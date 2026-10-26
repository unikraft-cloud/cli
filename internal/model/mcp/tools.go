// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcp

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/model"
	"unikraft.com/cli/internal/model/cmd"
)

// Options configures the tools registered for model types.
type Options struct {
	Partition *model.Partition
	ReadOnly  bool
	Resources []string
	Stderr    io.Writer
}

// Tools registers the tools of model types on an MCP server.
type Tools struct {
	srv  *mcp.Server
	opts Options

	resources map[string]bool

	partitionMu sync.Mutex
}

func New(srv *mcp.Server, opts Options) *Tools {
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	var resources map[string]bool
	if len(opts.Resources) > 0 {
		resources = make(map[string]bool, len(opts.Resources))
		for _, name := range opts.Resources {
			resources[NormalizeName(name)] = true
		}
	}
	return &Tools{srv: srv, opts: opts, resources: resources}
}

func (ts *Tools) enabled(name string, mutating bool) bool {
	name = NormalizeName(name)
	if mutating && ts.opts.ReadOnly {
		return false
	}
	return ts.resources == nil || ts.resources[name]
}

func (ts *Tools) AddTool[In any](typeName string, tool *mcp.Tool, structured bool, run func(context.Context, In, config.Stdio) error) error {
	if !ts.enabled(typeName, !tool.Annotations.ReadOnlyHint) {
		return nil
	}
	return ts.addCommandTool(tool, structured, run)
}

// AddTools registers every tool a fully capable resource type supports.
func (ts *Tools) AddTools[R interface {
	model.GettableListableModel
	model.CreatableModel
	model.EditableModel
	model.DeletableModel
}](ctx context.Context, opts ...ToolOption) error {
	return errors.Join(
		ts.AddListTool[R](ctx),
		ts.AddCreateTool[R](ctx, opts...),
		ts.AddEditTool[R](ctx, opts...),
		ts.AddDeleteTool[R](ctx),
	)
}

type listArgs struct {
	Keys   []string `json:"keys"`
	Filter []string `json:"filter"`
	Sort   []string `json:"sort"`
	Fields []string `json:"fields"`
}

func (ts *Tools) AddListTool[R model.GettableListableModel](ctx context.Context) error {
	typ, fields, err := fieldsOf[R](ctx)
	if err != nil {
		return err
	}
	return ts.AddTool(typ.Name, &mcp.Tool{
		Name:        toolName(typ.Name, "list"),
		Title:       "List " + typ.Names,
		Description: fmt.Sprintf("List all %s, or fetch specific ones by passing their keys. Narrow with filter and trim the output with fields; both take the field paths listed on those parameters. Returns one JSON object per %s.", typ.Names, typ.Name),
		InputSchema: listSchema(typ, fields),
		Annotations: ReadOnlyAnnotations(),
	}, true, func(ctx context.Context, args listArgs, stdio config.Stdio) error {
		return (&cmd.ModelListCmd[R]{
			Targets:    args.Keys,
			Filter:     args.Filter,
			Sort:       args.Sort,
			FormatOpts: JSONFormat(args.Fields...),
		}).Run(ctx, stdio, ts.opts.Partition)
	})
}

type getArgs struct {
	Keys   []string `json:"keys"`
	Fields []string `json:"fields"`
}

func (ts *Tools) AddGetTool[R model.GettableModel](ctx context.Context, description string) error {
	typ, fields, err := fieldsOf[R](ctx)
	if err != nil {
		return err
	}
	return ts.AddTool(typ.Name, &mcp.Tool{
		Name:        toolName(typ.Name, "get"),
		Title:       "Inspect " + typ.Names,
		Description: description,
		InputSchema: getSchema(typ, fields),
		Annotations: ReadOnlyAnnotations(),
	}, true, func(ctx context.Context, args getArgs, stdio config.Stdio) error {
		return (&cmd.ModelGetCmd[R]{
			Targets:    args.Keys,
			FormatOpts: JSONFormat(args.Fields...),
		}).Run(ctx, stdio, ts.opts.Partition)
	})
}

type createArgs struct {
	Set    map[string]jsontext.Value `json:"set"`
	DryRun bool                      `json:"dry_run"`
}

func (ts *Tools) AddCreateTool[R model.CreatableModel](ctx context.Context, opts ...ToolOption) error {
	typ, fields, err := fieldsOf[R](ctx)
	if err != nil {
		return err
	}
	schema := createSchema(fields)
	defaults := make(map[string]jsontext.Value)
	for path, value := range newToolOptions(opts).createDefaults {
		prop, ok := schema.Properties["set"].Properties[path]
		if !ok {
			return fmt.Errorf("%s: cannot default %q: not a creatable field", toolName(typ.Name, "create"), path)
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("%s: cannot default %q: %w", toolName(typ.Name, "create"), path, err)
		}
		prop.Default = raw
		defaults[path] = raw
	}
	return ts.AddTool(typ.Name, &mcp.Tool{
		Name:        toolName(typ.Name, "create"),
		Title:       "Create " + typ.Name,
		Description: createDescription(typ, fields, schema.Properties["set"], defaults),
		InputSchema: schema,
		Annotations: MutatingAnnotations(),
	}, true, func(ctx context.Context, args createArgs, stdio config.Stdio) error {
		if args.Set == nil {
			args.Set = map[string]jsontext.Value{}
		}
		for path, value := range defaults {
			if v, ok := args.Set[path]; !ok || v.Kind() == 'n' {
				args.Set[path] = value
			}
		}
		return (&cmd.ModelCreateCmd[R]{
			Set:        patchArgs(args.Set),
			DryRun:     args.DryRun,
			FormatOpts: JSONFormat(),
		}).Run(ctx, stdio, ts.opts.Partition)
	})
}

type editArgs struct {
	Key    string                    `json:"key"`
	Set    map[string]jsontext.Value `json:"set"`
	Add    map[string]jsontext.Value `json:"add"`
	Del    map[string]jsontext.Value `json:"del"`
	DryRun bool                      `json:"dry_run"`
}

func (ts *Tools) AddEditTool[R model.EditableModel](ctx context.Context, opts ...ToolOption) error {
	typ, fields, err := fieldsOf[R](ctx)
	if err != nil {
		return err
	}
	return ts.AddTool(typ.Name, &mcp.Tool{
		Name:        toolName(typ.Name, "edit"),
		Title:       "Edit " + typ.Name,
		Description: editDescription(typ, newToolOptions(opts).editNote),
		InputSchema: editSchema(typ, fields),
		Annotations: MutatingAnnotations(),
	}, true, func(ctx context.Context, args editArgs, stdio config.Stdio) error {
		return (&cmd.ModelEditCmd[R]{
			Target:     args.Key,
			Set:        patchArgs(args.Set),
			Add:        patchArgs(args.Add),
			Del:        patchArgs(args.Del),
			DryRun:     args.DryRun,
			FormatOpts: JSONFormat(),
		}).Run(ctx, stdio, ts.opts.Partition)
	})
}

type deleteArgs struct {
	Keys []string `json:"keys"`
}

func (ts *Tools) AddDeleteTool[R model.DeletableModel](ctx context.Context) error {
	var empty R
	typ := empty.Type()
	return ts.AddTool(typ.Name, &mcp.Tool{
		Name:        toolName(typ.Name, "delete"),
		Title:       "Delete " + typ.Names,
		Description: fmt.Sprintf("Delete %[1]s by key. This cannot be undone. Returns the deleted %[1]s.", typ.Names),
		InputSchema: deleteSchema(typ),
		Annotations: destructiveAnnotations(),
	}, true, func(ctx context.Context, args deleteArgs, stdio config.Stdio) error {
		if len(args.Keys) == 0 {
			return fmt.Errorf("no %s specified", typ.Names)
		}
		return (&cmd.ModelRemoveCmd[R]{
			Targets:    args.Keys,
			FormatOpts: JSONFormat(),
		}).Run(ctx, stdio, ts.opts.Partition)
	})
}

func fieldsOf[R model.GettableModel](ctx context.Context) (model.Type, []model.Field, error) {
	var empty R
	fields, err := empty.Fields(ctx)
	return empty.Type(), fields, err
}

func (ts *Tools) addCommandTool[In any](tool *mcp.Tool, structured bool, run func(context.Context, In, config.Stdio) error) error {
	var schema *jsonschema.Schema
	if tool.InputSchema == nil {
		var err error
		if schema, err = jsonschema.For[In](nil); err != nil {
			return fmt.Errorf("%s: %w", tool.Name, err)
		}
	} else {
		schema = tool.InputSchema.(*jsonschema.Schema).CloneSchemas()
	}
	// For types nil-able fields as ["null", T]; clients such as the MCP
	// Inspector reject array-valued types.
	singleTypes(schema)
	tool = new(*tool)
	tool.InputSchema = schema
	check := schema.CloneSchemas()
	// The command checks the fields required in set once it has filled in
	// defaults such as the profile's metro.
	for _, prop := range check.Properties {
		prop.Required = nil
	}
	resolved, err := check.Resolve(nil)
	if err != nil {
		return fmt.Errorf("%s: %w", tool.Name, err)
	}
	ts.srv.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.Params.Arguments
		if len(args) == 0 {
			args = []byte("{}")
		}
		ctx = log.WithLogger(ctx, new(log.G(ctx).With().Str("tool", req.Params.Name).Logger()))
		log.G(ctx).Trace().RawJSON("arguments", args).Msg("calling tool")

		var instance any
		err := json.Unmarshal(args, &instance)
		if err == nil {
			err = resolved.Validate(withoutNulls(instance))
		}
		var in In
		if err == nil {
			err = json.Unmarshal(args, &in)
		}
		if err != nil {
			err = fmt.Errorf("invalid arguments: %w", err)
			log.G(ctx).Debug().Err(err).Msg("tool call rejected")
			var res mcp.CallToolResult
			res.SetError(err)
			return &res, nil
		}
		return ts.capture(ctx, structured, func(ctx context.Context, stdio config.Stdio) error {
			return run(ctx, in, stdio)
		}), nil
	})
	return nil
}

func JSONFormat(fields ...string) cmd.FormatOpts {
	return cmd.FormatOpts{Field: fields, Output: cmd.Printer{Type: cmd.PrinterTypeJSON}}
}

func ReadOnlyAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(false), IdempotentHint: true}
}

func MutatingAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{DestructiveHint: new(false)}
}

func destructiveAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{DestructiveHint: new(true), IdempotentHint: true}
}

func (ts *Tools) capture(ctx context.Context, structured bool, run func(context.Context, config.Stdio) error) *mcp.CallToolResult {
	start := time.Now()
	var out bytes.Buffer
	err := ts.syncPartition(func() error {
		return run(ctx, config.Stdio{Stdin: bytes.NewReader(nil), Stdout: &out, Stderr: ts.opts.Stderr})
	})
	msg := "tool call complete"
	if err != nil {
		msg = "tool call failed"
	}
	log.G(ctx).Debug().
		Dur("took", time.Since(start)).
		Int("output_bytes", out.Len()).
		Err(err).
		Msg(msg)

	res := &mcp.CallToolResult{Content: []mcp.Content{}}
	if out.Len() > 0 {
		res.Content = append(res.Content, &mcp.TextContent{Text: out.String()})
		var parsed any
		if structured && json.Unmarshal(out.Bytes(), &parsed) == nil {
			res.StructuredContent = map[string]any{"resources": parsed}
		}
	}
	if err != nil {
		res.Content = append(res.Content, &mcp.TextContent{Text: err.Error()})
		res.IsError = true
	}
	return res
}

func withoutNulls(v any) any {
	if m, ok := v.(map[string]any); ok {
		for k, e := range m {
			if e == nil {
				delete(m, k)
			} else {
				withoutNulls(e)
			}
		}
	}
	return v
}

// patchArgs turns set, add or del arguments into the path=value pairs that
// the --set, --add and --del flags give, one per array element, in path order.
func patchArgs(values map[string]jsontext.Value) []map[string]string {
	var args []map[string]string
	for _, path := range slices.Sorted(maps.Keys(values)) {
		for _, v := range patchValues(values[path]) {
			args = append(args, map[string]string{path: v})
		}
	}
	return args
}

// patchValues gives the CLI values of one argument: none for null, one per
// array element, and strings unquoted.
func patchValues(v jsontext.Value) []string {
	switch v.Kind() {
	case 'n':
		return nil
	case '[':
		var items []jsontext.Value
		if err := json.Unmarshal(v, &items); err == nil {
			out := make([]string, 0, len(items))
			for _, item := range items {
				out = append(out, scalarString(item))
			}
			return out
		}
	}
	return []string{scalarString(v)}
}

func scalarString(v jsontext.Value) string {
	if v.Kind() == '"' {
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			return s
		}
	}
	return string(bytes.TrimSpace(v))
}
