// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

import (
	"cmp"
	"context"
	"fmt"
	"strconv"
	"strings"

	"unikraft.com/cloud/sdk/platform/group"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
	rescmd "unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/resource/patch"
	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/cli/internal/types"
)

type rootCmd struct {
	Get     getCmd     `cmd:"" help:"Inspect the current instance"`
	Help    helpCmd    `cmd:"" help:"Show available commands"`
	Edit    editCmd    `cmd:"" help:"Edit instance fields (env, args, memory, vcpus, tags)"`
	Volumes volumesCmd `cmd:"" help:"Manage volumes"`
	Mount   mountCmd   `cmd:"" help:"Attach a volume to this instance"`
	Unmount unmountCmd `cmd:"" help:"Detach a volume from this instance"`
	Start   startCmd   `cmd:"" help:"Start the instance"`
	Stop    stopCmd    `cmd:"" help:"Stop the instance"`
	Suspend suspendCmd `cmd:"" help:"Suspend the instance"`
	Restart restartCmd `cmd:"" help:"Restart the instance"`
	History historyCmd `cmd:"" help:"Show and manage command history"`
}

type helpCmd struct{}

func (c *helpCmd) Run(sctx *shellContext) error {
	sctx.builtins.Help(sctx.out)
	return nil
}

// renderField renders one of the instance's fields by path, reporting false
// when the instance does not carry it, or carries it empty.
func renderField(fields []resource.Field, path string) (string, bool) {
	found := resource.GetFieldByPathString(fields, path)
	if len(found) == 0 || found[0].IsEmpty() {
		return "", false
	}
	rendered, err := found[0].Render(value.RenderOpts{})
	if err != nil || strings.TrimSpace(rendered) == "" {
		return "", false
	}
	return rendered, true
}

// fieldByPath reads a field's value by path, and subValue reads a subfield's
// value off a field - the mount path of one of an instance's volumes, say. The
// zero value stands in for a field that isn't there, or doesn't hold what the
// caller expected.
func fieldByPath[T any](fields []resource.Field, path string) T {
	found := resource.GetFieldByPathString(fields, path)
	if len(found) == 0 {
		var zero T
		return zero
	}
	return asValue[T](found[0])
}

func subValue[T any](field resource.Field, name string) T {
	sub, ok := field.Get(name)
	if !ok {
		var zero T
		return zero
	}
	return asValue[T](sub)
}

func asValue[T any](field resource.Field) T {
	v, ok := field.Value.(T)
	if !ok {
		var zero T
		return zero
	}
	return v
}

type getCmd struct{}

func (c *getCmd) Run(sctx *shellContext) error {
	desc, err := sctx.resolve(instanceResource)
	if err != nil {
		return err
	}
	instance, err := sctx.instance()
	if err != nil {
		return err
	}
	rescmd.Printer{}.WithDefault(rescmd.PrinterTypeKeyValue).
		Print(sctx.ctx, sctx.out, nil, desc.Get, instance)
	return nil
}

type editCmd struct {
	Show   editShowCmd   `cmd:"" default:"1" hidden:""`
	Env    editEnvCmd    `cmd:"" help:"Set environment variable"`
	Args   editArgsCmd   `cmd:"" help:"Set arguments"`
	Memory editMemoryCmd `cmd:"" help:"Set memory (e.g. 128MiB)"`
	Vcpus  editVcpusCmd  `cmd:"" help:"Set vCPU count"`
	Tags   editTagsCmd   `cmd:"" help:"Add a tag"`
}

type editShowCmd struct{}

func (c *editShowCmd) Run(sctx *shellContext) error {
	instance, err := sctx.instance()
	if err != nil {
		return err
	}
	fields, err := instance.Fields(sctx.ctx)
	if err != nil {
		return wrapErr(err)
	}

	printLabelled := func(label, v string) {
		fmt.Fprintf(sctx.out, "  %s %s\n", labelStyle.Render(label+":"), valueStyle.Render(v))
	}

	if tags := fieldByPath[[]string](fields, "tags"); len(tags) > 0 {
		printLabelled("tags", strings.Join(tags, ", "))
	}
	if image, ok := renderField(fields, "image"); ok {
		printLabelled("image", image)
	}
	if args, ok := renderField(fields, "runtime.args"); ok {
		printLabelled("args", args)
	}
	if env := fieldByPath[map[string]string](fields, "runtime.env"); len(env) > 0 {
		fmt.Fprintf(sctx.out, "  %s\n", labelStyle.Render("env:"))
		for k, v := range env {
			fmt.Fprintf(sctx.out, "    %s=%s\n", dirStyle.Render(k), valueStyle.Render(v))
		}
	}
	for _, f := range []struct{ label, path string }{
		{"memory", "resources.memory"},
		{"vcpus", "resources.vcpus"},
	} {
		if v, ok := renderField(fields, f.path); ok {
			printLabelled(f.label, v)
		}
	}
	return nil
}

type editEnvCmd struct {
	KeyValue []string `arg:"" name:"KEY=VALUE" help:"Environment variable to set"`
}

func (c *editEnvCmd) Run(sctx *shellContext) error {
	fieldValue := strings.Join(c.KeyValue, " ")
	if _, _, ok := parseAssignment(fieldValue); !ok {
		return errf("edit env: expected KEY=VALUE")
	}
	return sctx.applyEditField("env", fieldValue, patch.PatchSpec{
		Add: map[string][]string{"runtime.env": {fieldValue}},
	})
}

type editArgsCmd struct {
	Arg []string `arg:"" name:"ARG" help:"Arguments to set"`
}

func (c *editArgsCmd) Run(sctx *shellContext) error {
	fieldValue := strings.Join(c.Arg, " ")
	return sctx.applyEditField("args", fieldValue, patch.PatchSpec{
		Set: map[string][]string{"runtime.args": {fieldValue}},
	})
}

type editMemoryCmd struct {
	Size string `arg:"" name:"SIZE" help:"Memory size (e.g. 128MiB)"`
}

func (c *editMemoryCmd) Run(sctx *shellContext) error {
	var mem types.SizeMebibytes
	if err := mem.UnmarshalText([]byte(c.Size)); err != nil {
		return errf("invalid memory size: %v", err)
	}
	return sctx.applyEditField("memory", c.Size, patch.PatchSpec{
		Set: map[string][]string{"resources.memory": {c.Size}},
	})
}

type editVcpusCmd struct {
	N int `arg:"" name:"N" help:"Number of vCPUs"`
}

func (c *editVcpusCmd) Run(sctx *shellContext) error {
	if c.N < 1 {
		return errf("edit vcpus: expected a positive integer")
	}
	return sctx.applyEditField("vcpus", strconv.Itoa(c.N), patch.PatchSpec{
		Set: map[string][]string{"resources.vcpus": {strconv.Itoa(c.N)}},
	})
}

type editTagsCmd struct {
	Tag []string `arg:"" name:"TAG" help:"Tag to add"`
}

func (c *editTagsCmd) Run(sctx *shellContext) error {
	fieldValue := strings.Join(c.Tag, " ")
	return sctx.applyEditField("tags", fieldValue, patch.PatchSpec{
		Add: map[string][]string{"tags": {fieldValue}},
	})
}

// applyEditField patches the instance and reports what changed, the way every
// `:edit` builtin does.
func (sctx *shellContext) applyEditField(fieldName, fieldValue string, spec patch.PatchSpec) error {
	if err := sctx.applyEdit(spec); err != nil {
		return err
	}
	fmt.Fprintf(sctx.out, "  %s %s=%s\n", valueStyle.Render("✓"), dirStyle.Render(fieldName), valueStyle.Render(fieldValue))
	printRestartHint(sctx.out)
	return nil
}

type volumesCmd struct {
	Mounted volumesMountedCmd `cmd:"" default:"1" help:"List volumes mounted on this instance"`
	List    volumesListCmd    `cmd:"" help:"List all available volumes"`
	Create  volumesCreateCmd  `cmd:"" help:"Create a new volume"`
}

type volumesMountedCmd struct{}

func (c *volumesMountedCmd) Run(sctx *shellContext) error {
	instance, err := sctx.instance()
	if err != nil {
		return err
	}
	fields, err := instance.Fields(sctx.ctx)
	if err != nil {
		return wrapErr(err)
	}

	var mounted []resource.Field
	if volumes := resource.GetFieldByPathString(fields, "volumes"); len(volumes) > 0 {
		mounted = volumes[0].Subfields
	}
	if len(mounted) == 0 {
		fmt.Fprintln(sctx.out, hintStyle.Render("No volumes mounted."))
		return nil
	}

	for _, vol := range mounted {
		name := cmp.Or(subValue[string](vol, "name"), subValue[string](vol, "uuid"))
		flags := ""
		if subValue[bool](vol, "readonly") {
			flags = " (ro)"
		}
		fmt.Fprintf(sctx.out, "  %s %s → %s%s\n", labelStyle.Render("■"), valueStyle.Render(name), dirStyle.Render(subValue[string](vol, "at")), flags)
	}
	return nil
}

type volumesListCmd struct{}

func (c *volumesListCmd) Run(sctx *shellContext) error {
	desc, err := sctx.resolve(volumeResource)
	if err != nil {
		return err
	}
	if desc.List == nil {
		return errf("volumes cannot be listed here")
	}

	volumes, err := desc.List.List(sctx.ctx)
	if err != nil {
		return wrapErr(err)
	}
	if len(volumes) == 0 {
		fmt.Fprintln(sctx.out, hintStyle.Render("No volumes found."))
		return nil
	}

	for _, vol := range volumes {
		fields, err := vol.Fields(sctx.ctx)
		if err != nil {
			return wrapErr(err)
		}
		name := fieldByPath[string](fields, "name")
		sizeStr, _ := renderField(fields, "size")

		attached := ""
		var names []string
		if mountedBy := resource.GetFieldByPathString(fields, "mounted-by"); len(mountedBy) > 0 {
			for _, m := range mountedBy[0].Subfields {
				names = append(names, cmp.Or(subValue[string](m, "name"), subValue[string](m, "uuid")))
			}
		}
		if len(names) > 0 {
			attached = fmt.Sprintf(" → %s", dirStyle.Render(strings.Join(names, ", ")))
		}
		fmt.Fprintf(sctx.out, "  %s %-20s %s%s\n", labelStyle.Render("■"), valueStyle.Render(name), hintStyle.Render(sizeStr), attached)
	}
	return nil
}

type volumesCreateCmd struct {
	Name       string   `arg:"" name:"name" help:"Volume name"`
	Size       string   `short:"s" name:"size" required:"" help:"Volume size (e.g. 10MiB)"`
	Filesystem string   `short:"f" name:"filesystem" help:"Filesystem type"`
	Tags       []string `short:"t" name:"tags" help:"Comma-separated tags"`
}

func (c *volumesCreateCmd) Run(sctx *shellContext) error {
	var sizeMb types.SizeMebibytes
	if err := sizeMb.UnmarshalText([]byte(c.Size)); err != nil {
		return errf("invalid size: %v", err)
	}

	desc, err := sctx.resolve(volumeResource)
	if err != nil {
		return err
	}
	creatable, ok := desc.Get.(resource.CreatableResource)
	if !ok {
		return errf("volumes cannot be created here")
	}

	// The volume goes to the instance's metro, so it can actually be mounted
	// on it. An empty metro leaves the resource's own default in place.
	spec := patch.PatchSpec{Create: true, Set: map[string][]string{
		"name": {c.Name},
		"size": {c.Size},
	}}
	if sctx.Key.Metro != "" {
		spec.Set["metro"] = []string{sctx.Key.Metro}
	}
	if c.Filesystem != "" {
		spec.Set["filesystem"] = []string{c.Filesystem}
	}
	if len(c.Tags) > 0 {
		spec.Set["tags"] = c.Tags
	}

	fields, err := desc.Get.Fields(sctx.ctx)
	if err != nil {
		return wrapErr(err)
	}
	patched, err := patch.PatchedFields(fields, spec)
	if err != nil {
		return wrapErr(err)
	}
	if err := patch.ValidateRequired(fields, patched, true); err != nil {
		return wrapErr(err)
	}

	created, err := creatable.Create(sctx.ctx, patched)
	if err != nil {
		return wrapErr(err)
	}

	sizeFmt, _ := value.Render(sizeMb, value.RenderOpts{})
	for _, vol := range created {
		uuid := vol.Key().String()
		if volFields, err := vol.Fields(sctx.ctx); err == nil {
			uuid = cmp.Or(fieldByPath[string](volFields, "uuid"), uuid)
		}
		fmt.Fprintf(sctx.out, "  %s created %s (%s) [%s]\n", valueStyle.Render("✓"), valueStyle.Render(c.Name), hintStyle.Render(sizeFmt), hintStyle.Render(uuid))
	}
	return nil
}

type mountCmd struct {
	Volume string `arg:"" name:"volume" help:"Volume name"`
	Path   string `arg:"" name:"path" help:"Mount path"`
	Mode   string `arg:"" name:"mode" optional:"" help:"Mount mode (ro)"`
}

func (c *mountCmd) Run(sctx *shellContext) error {
	fmt.Fprintf(sctx.out, "  %s mounting %s → %s...\n", labelStyle.Render("■"), valueStyle.Render(c.Volume), dirStyle.Render(c.Path))

	// The volume reads the way it does on `unikraft instance edit --add
	// volumes=`: NAME:PATH, with the mode as a further option.
	spec := c.Volume + ":" + c.Path
	if c.Mode == "ro" {
		spec += ":ro"
	}
	if err := sctx.applyEdit(patch.PatchSpec{Add: map[string][]string{"volumes": {spec}}}); err != nil {
		return err
	}

	fmt.Fprintf(sctx.out, "  %s mounted %s → %s\n", valueStyle.Render("✓"), valueStyle.Render(c.Volume), dirStyle.Render(c.Path))
	printRestartHint(sctx.out)
	return nil
}

type unmountCmd struct {
	Volume string `arg:"" name:"volume" help:"Volume name"`
}

func (c *unmountCmd) Run(sctx *shellContext) error {
	fmt.Fprintf(sctx.out, "  %s unmounting %s...\n", labelStyle.Render("■"), valueStyle.Render(c.Volume))

	if err := sctx.applyEdit(patch.PatchSpec{Del: map[string][]string{"volumes": {c.Volume}}}); err != nil {
		return err
	}

	fmt.Fprintf(sctx.out, "  %s unmounted %s\n", valueStyle.Render("✓"), valueStyle.Render(c.Volume))
	printRestartHint(sctx.out)
	return nil
}

// The lifecycle builtins are the CLI's own start/stop/suspend/restart commands
// pointed at this instance, so their output and semantics stay identical. They
// only add tracking of whether the instance is up.
func (sctx *shellContext) lifecycle(action string, run func(context.Context, config.Stdio) error) error {
	if run == nil {
		return errf("%s is not available here", action)
	}
	return wrapErr(run(sctx.ctx, sctx.stdio()))
}

type startCmd struct{}

func (c *startCmd) Run(sctx *shellContext) error {
	if err := sctx.lifecycle("start", sctx.Lifecycle.Start); err != nil {
		return err
	}
	sctx.state.Running = true
	sctx.startBackgroundSync()
	return nil
}

type stopCmd struct{}

func (c *stopCmd) Run(sctx *shellContext) error {
	if err := sctx.lifecycle("stop", sctx.Lifecycle.Stop); err != nil {
		return err
	}
	sctx.state.Running = false
	return nil
}

type suspendCmd struct{}

func (c *suspendCmd) Run(sctx *shellContext) error {
	if err := sctx.lifecycle("suspend", sctx.Lifecycle.Suspend); err != nil {
		return err
	}
	sctx.state.Running = false
	return nil
}

type restartCmd struct{}

func (c *restartCmd) Run(sctx *shellContext) error {
	if err := sctx.lifecycle("restart", sctx.Lifecycle.Restart); err != nil {
		return err
	}
	sctx.state.Running = true
	sctx.startBackgroundSync()
	return nil
}

type historyCmd struct {
	List   historyListCmd   `cmd:"" default:"1" help:"Show command history"`
	Rerun  historyRerunCmd  `cmd:"" help:"Re-execute a specific history entry"`
	Clear  historyClearCmd  `cmd:"" help:"Clear all history"`
	Delete historyDeleteCmd `cmd:"" help:"Delete a specific history entry"`
}

type historyListCmd struct{}

func (c *historyListCmd) Run(sctx *shellContext) error {
	sctx.cache.Print(sctx.out)
	return nil
}

type historyRerunCmd struct {
	Index int `arg:"" name:"index" help:"History index to rerun"`
}

func (c *historyRerunCmd) Run(sctx *shellContext) error {
	cmd, ok := sctx.cache.Get(c.Index)
	if !ok {
		return errf("history: event not found: %d", c.Index)
	}

	fmt.Fprintln(sctx.out, cmd)
	if err := sctx.execRemote(sctx.ctx, sctx.out, nil, cmd); err != nil {
		return wrapErr(err)
	}
	return nil
}

type historyClearCmd struct{}

func (c *historyClearCmd) Run(sctx *shellContext) error {
	sctx.cache.Clear()
	fmt.Fprintln(sctx.out, hintStyle.Render("Clearing remote history..."))
	_, _ = group.CollectMetro(sctx.ctx, sctx.Group, sctx.Key.Metro, func(ctx context.Context, client multimetro.MetroClient) (struct{}, error) {
		instance := multimetro.SandboxInstance(sctx.Key)
		callOpts := client.SandboxOpts(sctx.Plugin)

		resp, listErr := client.Sandbox.ListCommands(ctx, instance, callOpts...)
		if listErr != nil {
			return struct{}{}, listErr
		}
		if resp.Data == nil {
			return struct{}{}, nil
		}
		for _, cmdUUID := range resp.Data.Commands {
			_, _ = client.Sandbox.DeleteCommandByUuid(ctx, instance, cmdUUID, callOpts...)
		}
		return struct{}{}, nil
	})
	fmt.Fprintf(sctx.out, "History cleared\n")
	return nil
}

type historyDeleteCmd struct {
	Index int `arg:"" name:"index" help:"History index to delete"`
}

func (c *historyDeleteCmd) Run(sctx *shellContext) error {
	cmd, cmdUUID, ok := sctx.cache.Delete(c.Index)
	if !ok {
		return errf("history: event not found: %d", c.Index)
	}
	if cmdUUID != "" {
		fmt.Fprintf(sctx.out, "%s %s\n", hintStyle.Render("Deleting remote entry..."), hintStyle.Render(cmdUUID))
		_, delErr := group.CollectMetro(sctx.ctx, sctx.Group, sctx.Key.Metro, func(ctx context.Context, client multimetro.MetroClient) (struct{}, error) {
			resp, err := client.Sandbox.DeleteCommandByUuid(ctx, multimetro.SandboxInstance(sctx.Key), cmdUUID, client.SandboxOpts(sctx.Plugin)...)
			if err != nil {
				return struct{}{}, err
			}
			if !resp.IsSuccess() {
				return struct{}{}, fmt.Errorf("%s", resp.Message)
			}
			return struct{}{}, nil
		})
		if delErr != nil {
			fmt.Fprintln(sctx.errOut, errf("failed to delete remote command %s: %v", cmdUUID, delErr))
		}
	}
	fmt.Fprintf(sctx.out, "Removed: %s\n", cmd)
	return nil
}
