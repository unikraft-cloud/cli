// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"unikraft.com/x/filters"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/patch"
	"unikraft.com/cli/internal/tui/watcher"
	xkong "unikraft.com/cli/internal/x/kong"
	"unikraft.com/cloud/sdk/platform/group"
)

type ResourceCmdInterface interface {
	Underlying() resource.Resource
}

type ResourceCmd[R resource.Resource] struct{}

var _ ResourceCmdInterface = (*ResourceCmd[resource.GettableResource])(nil)

func (cmd ResourceCmd[R]) Underlying() resource.Resource {
	var empty R
	return empty
}

type GettableResourceCmd[R resource.GettableResource] struct {
	Get ResourceGetCmd[R] `cmd:"" help:"Inspect a ${name}." aliases:"inspect,show"`
}
type WaitableResourceCmd[R resource.GettableResource] struct {
	Wait ResourceWaitCmd[R] `cmd:"" help:"Wait for ${names} to match a filter."`
}
type ListableResourceCmd[R resource.GettableListableResource] struct {
	List ResourceListCmd[R] `cmd:"" help:"List ${names}." aliases:"ls"`
}
type DeletableResourceCmd[R resource.DeletableResource] struct {
	Delete ResourceRemoveCmd[R] `cmd:"" help:"Remove a ${name}." aliases:"rm,remove"`
}

// BulkDeletableResourceCmd exposes a `delete` command which supports deleting
// resources by name as well as bulk deletion via `--all` and `--filter`.
//
// We keep this separate from DeletableResourceCmd so that resources which are
// deletable but not listable can still embed delete-by-name behavior.
type BulkDeletableResourceCmd[R interface {
	resource.DeletableResource
	resource.ListableResource
}] struct {
	Delete ResourceBulkRemoveCmd[R] `cmd:"" help:"Remove a ${name}." aliases:"rm,remove"`
}
type EditableResourceCmd[R resource.EditableResource] struct {
	Edit ResourceEditCmd[R] `cmd:"" help:"Edit a ${name}."`
}
type CreatableResourceCmd[R resource.CreatableResource] struct {
	Create ResourceCreateCmd[R] `cmd:"" help:"Create a ${name}."`
}

func (cmd ResourceCmd[R]) HelpSections() []kingkong.HelpSection {
	var r R

	fields, err := r.Fields(context.Background())
	if err != nil {
		panic(err)
	}
	fields, err = SelectFields(fields, true, resource.FieldVerbosityInvisible, nil)
	if err != nil {
		panic(err)
	}

	buf := &bytes.Buffer{}

	for _, field := range fields {
		paths := []string{}
		base := kingkong.DimmedColor(field.Name)
		paths = append(paths, base)
		for path := range resource.IterFields(field.Subfields) {
			parts := make([]string, 0, len(path)+1)
			parts = append(parts, base)
			for _, part := range path {
				if part == "*" {
					part = kingkong.Bold(part)
				}
				parts = append(parts, kingkong.DimmedMoreColor(part))
			}
			paths = append(paths, strings.Join(parts, kingkong.DimmedColor(".")))
		}
		fmt.Fprintln(buf, strings.Join(paths, kingkong.DimmedColor(", ")))
	}

	return []kingkong.HelpSection{
		{
			Title:   "Fields",
			Content: buf.String(),
		},
	}
}

func (cmd ResourceCmd[R]) Examples() []kingkong.Example {
	var r R
	if ep, ok := any(r).(ExampledResource); ok {
		return ep.Examples()[CmdTypeNone] // top-level
	}
	return nil
}

type FormatOpts struct {
	Field xkong.GreedyStrings `short:"f" help:"Specify which fields to include in the output."`

	Output Printer `short:"o" help:"Output format. One of: kv, table, json, yaml, raw, quiet, template."`
}

type ResourceListCmd[R resource.GettableListableResource] struct {
	Targets []string            `arg:"" name:"target" optional:"" completion-predictor:"resource-key-${name}" help:"Target ${names} to list."`
	Filter  []string            `help:"Filter output based on a field value." example:"name==my-instance,metro==fra" sep:"none"`
	Watch   *time.Duration      `short:"w" help:"Watch for changes and refresh output. Defaults to 2s." type:"optional" placeholder:"duration"`
	Sort    xkong.GreedyStrings `help:"Sort output by field values. Use - prefix for descending, + for ascending." example:"name,-timestamps.created-at"`

	FormatOpts

	// DefaultFilter is applied when listing.
	// HACK: not perfect, but lets other commands extend this one easily without
	// needing weirder runtime introspection
	DefaultFilter filters.Filter `kong:"-"`
}

func (cmd ResourceListCmd[R]) HelpSections() []kingkong.HelpSection {
	return ResourceCmd[R]{}.HelpSections()
}

func (cmd ResourceListCmd[R]) Examples() []kingkong.Example {
	var r R
	if ep, ok := any(r).(ExampledResource); ok {
		return ep.Examples()[CmdTypeList]
	}
	return nil
}

func (cmd *ResourceListCmd[R]) Run(ctx context.Context, stdio config.Stdio, partition *resource.Partition) error {
	filter, err := filters.ParseAll(cmd.Filter...)
	if err != nil {
		return err
	}
	if cmd.DefaultFilter != nil {
		filter = filters.All{cmd.DefaultFilter, filter}
	}
	ctx = resource.WithFilter(ctx, filter)

	sortSpecs, err := parseSortSpecs(cmd.Sort...)
	if err != nil {
		return err
	}

	var empty R
	render := func(out io.Writer) error {
		var resources []resource.Resource
		var opErr error
		if len(cmd.Targets) > 0 {
			r := partition.WrapGettable(empty)
			resources, opErr = r.Get(ctx, cmd.Targets)
		} else {
			r := partition.WrapListable(empty)
			resources, opErr = r.List(ctx)
		}
		if opErr != nil && len(resources) == 0 {
			return opErr
		}

		resources, filterErr := filterResources(ctx, resources, filter)
		if filterErr != nil {
			opErr = errors.Join(opErr, filterErr)
		}

		if len(sortSpecs) > 0 {
			resources, err = sortResources(ctx, resources, sortSpecs)
			if err != nil {
				return errors.Join(opErr, err)
			}
		}

		printErr := cmd.Output.
			WithDefault(PrinterTypeTable).
			Print(ctx, out, cmd.Field, empty, resources...)
		if printErr != nil {
			return errors.Join(opErr, printErr)
		}
		return opErr
	}

	if cmd.Watch != nil {
		watch := cmp.Or(*cmd.Watch, 2*time.Second)
		return watcher.WatchOutput(ctx, watch, stdio.Stdout, render)
	}
	return render(stdio.Stdout)
}

type ResourceGetCmd[R resource.GettableResource] struct {
	Targets []string       `arg:"" name:"target" optional:"" completion-predictor:"resource-key-${name}" help:"Target ${names} to get."`
	Watch   *time.Duration `short:"w" help:"Watch for changes and refresh output. Defaults to 2s." type:"optional" placeholder:"duration"`

	FormatOpts
}

func (cmd ResourceGetCmd[R]) HelpSections() []kingkong.HelpSection {
	return ResourceCmd[R]{}.HelpSections()
}

func (cmd ResourceGetCmd[R]) Examples() []kingkong.Example {
	var r R
	if ep, ok := any(r).(ExampledResource); ok {
		return ep.Examples()[CmdTypeGet]
	}
	return nil
}

func (cmd *ResourceGetCmd[R]) Run(ctx context.Context, stdio config.Stdio, partition *resource.Partition) error {
	var empty R
	r := partition.WrapGettable(empty)

	render := func(out io.Writer) error {
		var resources []resource.Resource
		var opErr error
		if len(cmd.Targets) == 0 {
			def, ok := any(empty).(resource.DefaultResource)
			if !ok {
				return fmt.Errorf("parsing arguments: no %s specified", empty.Type().Names)
			}
			res, err := def.Default(ctx)
			if err != nil {
				return err
			}
			resources = []resource.Resource{res}
		} else {
			resources, opErr = r.Get(ctx, cmd.Targets)
			if opErr != nil && len(resources) == 0 {
				return opErr
			}
		}
		printErr := cmd.Output.
			WithDefault(PrinterTypeKeyValue).
			Print(ctx, out, cmd.Field, empty, resources...)
		if printErr != nil {
			return errors.Join(opErr, printErr)
		}
		return opErr
	}

	if cmd.Watch != nil {
		watch := cmp.Or(*cmd.Watch, 2*time.Second)
		return watcher.WatchOutput(ctx, watch, stdio.Stdout, render)
	}
	return render(stdio.Stdout)
}

type ResourceWaitCmd[R resource.GettableResource] struct {
	Targets []string `arg:"" name:"target" completion-predictor:"resource-key-${name}" help:"Target ${names} to wait for."`
	Until   []string `help:"Filter expression to wait for." example:"state==running,state!=stopped" sep:"none" required:"" aliases:"filter"`

	Interval time.Duration `long:"interval" default:"2s" help:"Polling interval."`

	FormatOpts
}

func (cmd ResourceWaitCmd[R]) HelpSections() []kingkong.HelpSection {
	return ResourceCmd[R]{}.HelpSections()
}

func (cmd ResourceWaitCmd[R]) Examples() []kingkong.Example {
	var r R
	if ep, ok := any(r).(ExampledResource); ok {
		return ep.Examples()[CmdTypeWait]
	}
	return nil
}

func (cmd *ResourceWaitCmd[R]) Run(ctx context.Context, stdio config.Stdio, partition *resource.Partition) error {
	var empty R
	if len(cmd.Targets) == 0 {
		return fmt.Errorf("no %s specified", empty.Type().Names)
	}
	r := partition.WrapGettable(empty)

	filter, err := filters.ParseAll(cmd.Until...)
	if err != nil {
		return err
	}
	ctx = resource.WithFilter(ctx, filter)

	ticker := time.NewTicker(cmd.Interval)
	defer ticker.Stop()

	passing := map[string]bool{}
	for {
		resources, err := r.Get(ctx, cmd.Targets)
		if err != nil {
			return err
		}

		filtered, err := filterResources(ctx, resources, filter)
		if err != nil {
			return err
		}
		if len(filtered) == len(resources) {
			log.G(ctx).Debug().
				Strs("resources", cmd.Targets).
				Msg("all resources match the specified conditions")

			return cmd.Output.
				WithDefault(PrinterTypeKeyValue).
				Print(ctx, stdio.Stdout, []string(cmd.Field), empty, filtered...)
		}
		log.G(ctx).Debug().
			Strs("resources", cmd.Targets).
			Int("matching", len(filtered)).
			Int("total", len(resources)).
			Msg("not all resources match the specified conditions yet")

		passed := passing
		passing = map[string]bool{}
		for _, res := range filtered {
			key := res.Key().String()
			passing[key] = true
			if ok := passed[key]; !ok {
				log.G(ctx).Info().Str("resource", key).
					Msg("resource now matches the specified conditions")
			}
		}
		for _, res := range resources {
			key := res.Key().String()
			if _, ok := passing[key]; ok {
				continue
			}
			passing[key] = false
			if ok := passed[key]; ok {
				log.G(ctx).Info().Str("resource", key).
					Msg("resource no longer matches the specified conditions")
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func filterResources(ctx context.Context, resources []resource.Resource, filter filters.Filter) (filtered []resource.Resource, rerr error) {
	if filter == nil {
		return resources, nil
	}

	// Extract field paths needed by the filter
	filterKeys := filters.Keys(filter)
	if len(filterKeys) == 0 {
		return resources, nil
	}
	filterPaths := make([]resource.FieldPath, len(filterKeys))
	for i, key := range filterKeys {
		filterPaths[i] = resource.FieldPath(key)
	}

	resolved, err := resolveResources(ctx, resources, filterPaths)
	if err != nil {
		return nil, err
	}

	seenPaths := make(map[string]bool)
	for _, res := range resolved {
		fields, _ := res.Fields(ctx)
		matched, err := filter.Match(newFieldAdaptor(fields))
		if err != nil {
			if fieldErr, ok := errors.AsType[*filters.FieldNotFoundError](err); ok {
				// Deduplicate field-not-found errors by path
				pathKey := strings.Join(fieldErr.Path, ".")
				if !seenPaths[pathKey] {
					seenPaths[pathKey] = true
					rerr = errors.Join(rerr, err)
				}
				continue
			}
			return nil, err
		}
		if matched {
			filtered = append(filtered, res)
		}
	}
	return filtered, rerr
}

type ResourceRemoveCmd[R resource.DeletableResource] struct {
	Targets []string `arg:"" name:"target" completion-predictor:"resource-key-${name}" help:"Target ${names} to remove."`

	FormatOpts
}

func (cmd ResourceRemoveCmd[R]) HelpSections() []kingkong.HelpSection {
	return ResourceCmd[R]{}.HelpSections()
}

func (cmd ResourceRemoveCmd[R]) Examples() []kingkong.Example {
	var r R
	if ep, ok := any(r).(ExampledResource); ok {
		return ep.Examples()[CmdTypeDelete]
	}
	return nil
}

func (cmd *ResourceRemoveCmd[R]) Run(ctx context.Context, stdio config.Stdio, partition *resource.Partition) error {
	var empty R
	r := partition.WrapDeletable(empty)

	// Get resources for display purposes.
	resources, getErr := r.Get(ctx, cmd.Targets)
	if getErr != nil && len(resources) == 0 {
		return getErr
	}

	// Delete using keys directly.
	deleteErr := error(nil)
	if len(cmd.Targets) > 0 {
		deleteErr = r.Delete(ctx, cmd.Targets)
	}

	toPrint := resources
	if deleteErr != nil {
		if notFound, ok := errors.AsType[group.ErrRefNotFound](deleteErr); ok {
			missing := make(map[string]struct{}, len(notFound.Refs))
			for _, ref := range notFound.Refs {
				missing[multimetro.Key(ref).String()] = struct{}{}
			}
			toPrint = slices.DeleteFunc(slices.Clone(resources), func(r resource.Resource) bool {
				_, ok := missing[r.Key().String()]
				return ok
			})
		} else {
			// Unknown error shape: avoid claiming success.
			toPrint = nil
		}
	}

	printErr := cmd.Output.
		WithDefault(PrinterTypeQuiet).
		Print(ctx, stdio.Stdout, cmd.Field, empty, toPrint...)
	if printErr != nil {
		return errors.Join(getErr, deleteErr, printErr)
	}
	return errors.Join(getErr, deleteErr)
}

type ResourceBulkRemoveCmd[R interface {
	resource.DeletableResource
	resource.ListableResource
}] struct {
	Targets []string `arg:"" name:"target" optional:"" completion-predictor:"resource-key-${name}" help:"Target ${names} to remove."`

	All    bool     `xor:"select" help:"Remove all ${names}. Prompts for confirmation."`
	Filter []string `xor:"select" help:"Filter ${names} to remove. Prompts for confirmation." example:"name==my-instance,metro==fra" sep:"none"`
	Force  bool     `help:"Do not prompt for confirmation when using --all or --filter."`

	FormatOpts
}

func (cmd ResourceBulkRemoveCmd[R]) HelpSections() []kingkong.HelpSection {
	return ResourceCmd[R]{}.HelpSections()
}

func (cmd ResourceBulkRemoveCmd[R]) Examples() []kingkong.Example {
	var r R
	if ep, ok := any(r).(ExampledResource); ok {
		return ep.Examples()[CmdTypeDelete]
	}
	return nil
}

func (cmd *ResourceBulkRemoveCmd[R]) Run(ctx context.Context, stdio config.Stdio, partition *resource.Partition) error {
	var empty R
	var resources []resource.Resource
	if cmd.All || len(cmd.Filter) > 0 {
		if len(cmd.Targets) > 0 {
			// would be nice if xor groups could enforce this
			return fmt.Errorf("cannot specify names when using --all or --filter")
		}

		filter, err := filters.ParseAll(cmd.Filter...)
		if err != nil {
			return err
		}

		r := partition.WrapListable(empty)
		resources, err = r.List(ctx)
		if err != nil {
			return err
		}

		if filter != nil {
			resources, err = filterResources(ctx, resources, filter)
			if err != nil {
				return err
			}
			resources = unwrapResources(resources)
		}

		if !cmd.Force && len(resources) > 0 {
			log.G(ctx).Warn().
				Int("count", len(resources)).
				Msg("resources will be deleted")

			err = cmd.Output.
				WithDefault(PrinterTypeTable).
				Print(ctx, stdio.Stdout, []string(cmd.Field), empty, resources...)
			if err != nil {
				return err
			}

			fmt.Fprintf(stdio.Stdout, "\nType \"yes\" to confirm deletion: ")

			inputCh := make(chan string, 1)
			errCh := make(chan error, 1)
			go func() {
				reader := bufio.NewReader(stdio.Stdin)
				response, err := reader.ReadString('\n')
				if err != nil && err != io.EOF {
					errCh <- fmt.Errorf("failed to read confirmation: %w", err)
					return
				}
				inputCh <- strings.TrimSpace(response)
			}()

			select {
			case <-ctx.Done():
				return ctx.Err()
			case err := <-errCh:
				return err
			case response := <-inputCh:
				switch strings.ToLower(response) {
				case "y", "yes":
				default:
					return fmt.Errorf("deletion cancelled")
				}
			}
		}

		dr := partition.WrapDeletable(empty)
		keys := make([]string, len(resources))
		for i, res := range resources {
			keys[i] = res.Key().String()
		}
		err = dr.Delete(ctx, keys)
		if err != nil {
			return err
		}
		return nil
	} else if len(cmd.Targets) > 0 {
		r := partition.WrapDeletable(empty)

		// Get resources for display purposes.
		resources, err := r.Get(ctx, cmd.Targets)
		if err != nil {
			return err
		}

		err = r.Delete(ctx, cmd.Targets)
		if err != nil {
			return err
		}
		return cmd.Output.
			WithDefault(PrinterTypeQuiet).
			Print(ctx, stdio.Stdout, []string(cmd.Field), empty, resources...)
	} else {
		return fmt.Errorf("no resources specified for deletion")
	}
}

type ResourceEditCmd[R resource.EditableResource] struct {
	Target string `arg:"" name:"target" completion-predictor:"resource-key-${name}" help:"Target ${name} to edit."`

	SetArgs
	AddArgs
	DelArgs

	Visual bool   `xor:"edit-mode" help:"Open an editor to modify fields visually."`
	Cmd    string `xor:"edit-mode" help:"Run a command to edit fields (receives YAML on stdin, outputs edited YAML on stdout)."`
	Load   []byte `xor:"edit-mode" collapse:"file-mode" type:"filecontent" help:"Load fields from a YAML file."`
	Save   string `xor:"edit-mode" collapse:"file-mode" placeholder:"FILE" help:"Save editable fields as YAML to a file (use - for stdout)."`

	DryRun bool `help:"Print patches without applying them."`

	FormatOpts
}

func (cmd ResourceEditCmd[R]) HelpSections() []kingkong.HelpSection {
	return ResourceCmd[R]{}.HelpSections()
}

func (cmd ResourceEditCmd[R]) Examples() []kingkong.Example {
	var r R
	if ep, ok := any(r).(ExampledResource); ok {
		return ep.Examples()[CmdTypeEdit]
	}
	return nil
}

func (cmd *ResourceEditCmd[R]) toPatchSpec() (patch.PatchSpec, error) {
	spec := patch.PatchSpec{
		Set: make(map[string][]string),
		Add: make(map[string][]string),
		Del: make(map[string][]string),
	}
	if err := cmd.SetArgs.Apply(&spec); err != nil {
		return spec, err
	}
	if err := cmd.AddArgs.Apply(&spec); err != nil {
		return spec, err
	}
	if err := cmd.DelArgs.Apply(&spec); err != nil {
		return spec, err
	}
	return spec, nil
}

func (cmd *ResourceEditCmd[R]) Run(ctx context.Context, stdio config.Stdio, partition *resource.Partition) error {
	spec, err := cmd.toPatchSpec()
	if err != nil {
		return err
	}

	var empty R
	r := partition.WrapEditable(empty)

	var res resource.Resource
	if cmd.Target == "" {
		def, ok := any(empty).(resource.DefaultResource)
		if !ok {
			return fmt.Errorf("parsing arguments: no %s specified", empty.Type().Names)
		}
		res, err = def.Default(ctx)
		if err != nil {
			return err
		}
	} else {
		resources, err := r.Get(ctx, []string{cmd.Target})
		if err != nil {
			return err
		}
		if len(resources) == 0 {
			return fmt.Errorf("resource not found: %s", cmd.Target)
		}
		if len(resources) > 1 {
			var keys []string
			for _, res := range resources {
				keys = append(keys, res.Key().String())
			}
			return fmt.Errorf("ambiguous resource name: %s (found %v)", cmd.Target, keys)
		}
		res = resources[0]
	}

	allFields := make(map[string]int)
	for k := range spec.Set {
		allFields[k]++
	}
	for k := range spec.Add {
		allFields[k]++
	}
	for k := range spec.Del {
		allFields[k]++
	}
	for k, count := range allFields {
		if count > 1 {
			return fmt.Errorf("field %s has multiple patch operations", k)
		}
	}

	fields, err := res.Fields(ctx)
	if err != nil {
		return fmt.Errorf("failed to get fields: %w", err)
	}
	patched, err := patch.PatchedFields(fields, spec)
	if err != nil {
		return err
	}

	// Handle --save: write YAML to file and exit
	if cmd.Save != "" {
		return saveYAML(cmd.Save, stdio, res, fields, patched, false)
	}

	// Handle different editing modes (mutually exclusive via xor:"edit-mode" tag)
	var editor patch.EditorFunc
	switch {
	case cmd.Visual:
		editor, err = patch.VisualCommandEditorFunc()
		if err != nil {
			return err
		}
	case cmd.Cmd != "":
		editor = patch.CommandEditorFunc(cmd.Cmd)
	case len(cmd.Load) > 0:
		editor = patch.ContentEditorFunc(cmd.Load)
	}
	if editor != nil {
		patched, err = patch.Edit(ctx, res, fields, patched, editor)
		if err != nil {
			return err
		}
	}
	patched = patch.FilterEditFields(patched)

	if cmd.DryRun {
		return PrintPatches(stdio.Stdout, patched, false)
	}

	updated := []resource.Resource{res}
	if len(patched) > 0 {
		editKey := res.Key().String()
		if err := r.Edit(ctx, editKey, patched); err != nil {
			return err
		}
		// Re-fetch the resource to get the updated state.
		getKey := cmd.Target
		if getKey == "" {
			getKey = res.Key().String()
		}
		results, err := r.Get(ctx, []string{getKey})
		if err != nil {
			return err
		}
		if len(results) > 0 {
			updated = results[:1]
		}
	} else {
		log.G(ctx).Warn().
			Str("resource", res.Key().String()).
			Msg("no edits made")
	}
	return Diff(ctx, stdio.Stdout, cmd.FormatOpts, empty, []resource.Resource{res}, updated)
}

type ResourceCreateCmd[R resource.CreatableResource] struct {
	SetArgs

	Visual bool   `xor:"edit-mode" help:"Open an editor to modify fields visually."`
	Cmd    string `xor:"edit-mode" help:"Run a command to edit fields (receives YAML on stdin, outputs edited YAML on stdout)."`
	Load   []byte `xor:"edit-mode" collapse:"file-mode" type:"filecontent" help:"Load fields from a YAML file."`
	Save   string `xor:"edit-mode" collapse:"file-mode" placeholder:"FILE" help:"Save creatable fields as YAML to a file (use - for stdout)."`

	DryRun bool `help:"Print patches without applying them."`

	FormatOpts
}

func (cmd ResourceCreateCmd[R]) HelpSections() []kingkong.HelpSection {
	return ResourceCmd[R]{}.HelpSections()
}

func (cmd ResourceCreateCmd[R]) Examples() []kingkong.Example {
	var r R
	if ep, ok := any(r).(ExampledResource); ok {
		return ep.Examples()[CmdTypeCreate]
	}
	return nil
}

func (cmd *ResourceCreateCmd[R]) toPatchSpec() (patch.PatchSpec, error) {
	spec := patch.PatchSpec{
		Create: true,
		Set:    make(map[string][]string),
	}
	if err := cmd.Apply(&spec); err != nil {
		return spec, err
	}
	return spec, nil
}

func (cmd *ResourceCreateCmd[R]) Run(ctx context.Context, stdio config.Stdio, partition *resource.Partition) error {
	_, err := cmd.RunResources(ctx, stdio, partition)
	return err
}

func (cmd *ResourceCreateCmd[R]) RunResources(ctx context.Context, stdio config.Stdio, partition *resource.Partition) ([]resource.Resource, error) {
	spec, err := cmd.toPatchSpec()
	if err != nil {
		return nil, err
	}

	var empty R
	r := partition.WrapCreatable(empty)
	fieldsResource := resource.Resource(empty)
	if typed, ok := any(empty).(interface {
		WithType(string) resource.Resource
	}); ok {
		if values := spec.Set["type"]; len(values) > 0 {
			fieldsResource = typed.WithType(values[0])
		}
	}
	fields, err := fieldsResource.Fields(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get fields: %w", err)
	}
	patched, err := patch.PatchedFields(fields, spec)
	if err != nil {
		return nil, err
	}

	// Handle --save: write YAML to file and exit
	if cmd.Save != "" {
		return nil, saveYAML(cmd.Save, stdio, empty, fields, patched, true)
	}

	// Handle different editing modes (mutually exclusive via xor:"edit-mode" tag)
	var editor patch.EditorFunc
	switch {
	case cmd.Visual:
		editor, err = patch.VisualCommandEditorFunc()
		if err != nil {
			return nil, err
		}
	case cmd.Cmd != "":
		editor = patch.CommandEditorFunc(cmd.Cmd)
	case len(cmd.Load) > 0:
		editor = patch.ContentEditorFunc(cmd.Load)
	}
	if editor != nil {
		patched, err = patch.Create(ctx, empty, fields, patched, editor)
		if err != nil {
			return nil, err
		}
	}

	patchedFields := patch.FilterCreateFields(patched)

	// Validate required fields before printing/applying
	if err := patch.ValidateRequired(fields, patched, true); err != nil {
		return nil, err
	}

	if cmd.DryRun {
		return nil, PrintPatches(stdio.Stdout, patchedFields, true)
	}

	resources, opErr := r.Create(ctx, patchedFields)
	if opErr != nil && len(resources) == 0 {
		return nil, opErr
	}
	printErr := cmd.Output.
		WithDefault(PrinterTypeKeyValue).
		Print(ctx, stdio.Stdout, cmd.Field, empty, resources...)
	if printErr != nil {
		return resources, errors.Join(opErr, printErr)
	}
	return resources, opErr
}

// saveYAML writes YAML to a file or stdout (if filename is "-").
func saveYAML(filename string, stdio config.Stdio, res resource.Resource, fields, patches []resource.Field, create bool) error {
	var w io.Writer
	if filename == "-" {
		w = stdio.Stdout
	} else {
		f, err := os.Create(filename)
		if err != nil {
			return fmt.Errorf("failed to create file: %w", err)
		}
		defer f.Close()
		w = f
	}
	return patch.SaveYAML(res, fields, patches, w, create)
}
