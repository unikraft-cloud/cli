// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
)

// AddTools registers every tool a fully capable resource type supports.
func (s *Server) AddTools[R interface {
	resource.GettableListableResource
	resource.CreatableResource
	resource.EditableResource
	resource.DeletableResource
}](ctx context.Context, opts ...ToolOption) error {
	return errors.Join(
		s.AddListTool[R](ctx),
		s.AddCreateTool[R](ctx, opts...),
		s.AddEditTool[R](ctx, opts...),
		s.AddDeleteTool[R](ctx),
	)
}

type listArgs struct {
	Keys   []string `json:"keys"`
	Filter []string `json:"filter"`
	Sort   []string `json:"sort"`
	Fields []string `json:"fields"`
}

func (s *Server) AddListTool[R resource.GettableListableResource](ctx context.Context) error {
	return s.resourceTool[R](ctx, false, func(typ resource.Type, fields []resource.Field) error {
		return s.addCommandTool(&mcp.Tool{
			Name:        toolName(typ.Name, "list"),
			Title:       "List " + typ.Names,
			Description: fmt.Sprintf("List all %s, or fetch specific ones by passing their keys. Narrow with filter and trim the output with fields; both take the field paths listed on those parameters. Returns one JSON object per %s.", typ.Names, typ.Name),
			InputSchema: listSchema(typ, fields),
			Annotations: readOnlyAnnotations(),
		}, true, func(ctx context.Context, args listArgs, stdio config.Stdio) error {
			return (&cmd.ResourceListCmd[R]{
				Targets:    args.Keys,
				Filter:     args.Filter,
				Sort:       args.Sort,
				FormatOpts: JSONFormat(args.Fields...),
			}).Run(ctx, stdio, s.opts.Partition)
		})
	})
}

type getArgs struct {
	Keys   []string `json:"keys"`
	Fields []string `json:"fields"`
}

func (s *Server) AddGetTool[R resource.GettableResource](ctx context.Context, description string) error {
	return s.resourceTool[R](ctx, false, func(typ resource.Type, fields []resource.Field) error {
		return s.addCommandTool(&mcp.Tool{
			Name:        toolName(typ.Name, "get"),
			Title:       "Inspect " + typ.Names,
			Description: description,
			InputSchema: getSchema(typ, fields),
			Annotations: readOnlyAnnotations(),
		}, true, func(ctx context.Context, args getArgs, stdio config.Stdio) error {
			return (&cmd.ResourceGetCmd[R]{
				Targets:    args.Keys,
				FormatOpts: JSONFormat(args.Fields...),
			}).Run(ctx, stdio, s.opts.Partition)
		})
	})
}

// ToolOption adjusts how a resource type's tools are registered.
type ToolOption func(*toolOptions)

type toolOptions struct {
	createDefaults map[string]any
	editNote       string
}

func newToolOptions(opts []ToolOption) toolOptions {
	var o toolOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WithCreateDefault makes the create tool set the field at path to value
// unless the caller sets it.
func WithCreateDefault(path string, value any) ToolOption {
	return func(o *toolOptions) {
		if o.createDefaults == nil {
			o.createDefaults = map[string]any{}
		}
		o.createDefaults[path] = value
	}
}

// WithEditNote appends note to the edit tool's description.
func WithEditNote(note string) ToolOption {
	return func(o *toolOptions) {
		o.editNote = note
	}
}

type createArgs struct {
	Set    map[string]jsontext.Value `json:"set"`
	DryRun bool                      `json:"dry_run"`
}

func (s *Server) AddCreateTool[R resource.CreatableResource](ctx context.Context, opts ...ToolOption) error {
	return s.resourceTool[R](ctx, true, func(typ resource.Type, fields []resource.Field) error {
		name := toolName(typ.Name, "create")
		schema := createSchema(fields)
		defaults := make(map[string]jsontext.Value)
		for path, value := range newToolOptions(opts).createDefaults {
			prop, ok := schema.Properties["set"].Properties[path]
			if !ok {
				return fmt.Errorf("%s: cannot default %q: not a creatable field", name, path)
			}
			raw, err := json.Marshal(value)
			if err != nil {
				return fmt.Errorf("%s: cannot default %q: %w", name, path, err)
			}
			prop.Default = raw
			defaults[path] = raw
		}
		return s.addCommandTool(&mcp.Tool{
			Name:        name,
			Title:       "Create " + typ.Name,
			Description: createDescription(typ, fields, schema.Properties["set"], defaults),
			InputSchema: schema,
			Annotations: mutatingAnnotations(),
		}, true, func(ctx context.Context, args createArgs, stdio config.Stdio) error {
			if args.Set == nil {
				args.Set = map[string]jsontext.Value{}
			}
			for path, value := range defaults {
				if v, ok := args.Set[path]; !ok || v.Kind() == 'n' {
					args.Set[path] = value
				}
			}
			return (&cmd.ResourceCreateCmd[R]{
				Set:        patchArgs(args.Set),
				DryRun:     args.DryRun,
				FormatOpts: JSONFormat(),
			}).Run(ctx, stdio, s.opts.Partition)
		})
	})
}

type editArgs struct {
	Key    string                    `json:"key"`
	Set    map[string]jsontext.Value `json:"set"`
	Add    map[string]jsontext.Value `json:"add"`
	Del    map[string]jsontext.Value `json:"del"`
	DryRun bool                      `json:"dry_run"`
}

func (s *Server) AddEditTool[R resource.EditableResource](ctx context.Context, opts ...ToolOption) error {
	return s.resourceTool[R](ctx, true, func(typ resource.Type, fields []resource.Field) error {
		return s.addCommandTool(&mcp.Tool{
			Name:        toolName(typ.Name, "edit"),
			Title:       "Edit " + typ.Name,
			Description: editDescription(typ, newToolOptions(opts).editNote),
			InputSchema: editSchema(typ, fields),
			Annotations: mutatingAnnotations(),
		}, true, func(ctx context.Context, args editArgs, stdio config.Stdio) error {
			return (&cmd.ResourceEditCmd[R]{
				Target:     args.Key,
				Set:        patchArgs(args.Set),
				Add:        patchArgs(args.Add),
				Del:        patchArgs(args.Del),
				DryRun:     args.DryRun,
				FormatOpts: JSONFormat(),
			}).Run(ctx, stdio, s.opts.Partition)
		})
	})
}

type deleteArgs struct {
	Keys []string `json:"keys"`
}

func (s *Server) AddDeleteTool[R resource.DeletableResource](ctx context.Context) error {
	return s.resourceTool[R](ctx, true, func(typ resource.Type, _ []resource.Field) error {
		return s.addCommandTool(&mcp.Tool{
			Name:        toolName(typ.Name, "delete"),
			Title:       "Delete " + typ.Names,
			Description: fmt.Sprintf("Delete %[1]s by key. This cannot be undone. Returns the deleted %[1]s.", typ.Names),
			InputSchema: deleteSchema(typ),
			Annotations: destructiveAnnotations(),
		}, true, func(ctx context.Context, args deleteArgs, stdio config.Stdio) error {
			if len(args.Keys) == 0 {
				return fmt.Errorf("no %s specified", typ.Names)
			}
			return (&cmd.ResourceRemoveCmd[R]{
				Targets:    args.Keys,
				FormatOpts: JSONFormat(),
			}).Run(ctx, stdio, s.opts.Partition)
		})
	})
}

func (s *Server) resourceTool[R resource.GettableResource](ctx context.Context, mutating bool, add func(resource.Type, []resource.Field) error) error {
	var empty R
	typ := empty.Type()
	if !s.enabled(typ.Name, mutating) {
		return nil
	}
	fields, err := empty.Fields(ctx)
	if err != nil {
		return err
	}
	return add(typ, fields)
}

func (s *Server) addCommandTool[In any](tool *mcp.Tool, structured bool, run func(context.Context, In, config.Stdio) error) error {
	if tool.InputSchema == nil {
		schema, err := jsonschema.For[In](nil)
		if err != nil {
			return fmt.Errorf("%s: %w", tool.Name, err)
		}
		tool.InputSchema = schema
	}
	schema := tool.InputSchema.(*jsonschema.Schema)
	singleTypes(schema)
	s.srv.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.Params.Arguments
		if len(args) == 0 {
			args = []byte("{}")
		}
		ctx = log.WithLogger(ctx, new(log.G(ctx).With().Str("tool", req.Params.Name).Logger()))
		log.G(ctx).Trace().RawJSON("arguments", args).Msg("calling tool")

		var in In
		if err := unmarshalArgs(args, schema, &in); err != nil {
			log.G(ctx).Debug().Err(err).Msg("tool call rejected")
			var res mcp.CallToolResult
			res.SetError(err)
			return &res, nil
		}
		return s.capture(ctx, structured, func(ctx context.Context, stdio config.Stdio) error {
			return run(ctx, in, stdio)
		}), nil
	})
	return nil
}

func JSONFormat(fields ...string) cmd.FormatOpts {
	return cmd.FormatOpts{Field: fields, Output: cmd.Printer{Type: cmd.PrinterTypeJSON}}
}

func readOnlyAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true, DestructiveHint: new(false), IdempotentHint: true}
}

func mutatingAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{DestructiveHint: new(false)}
}

func destructiveAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{DestructiveHint: new(true), IdempotentHint: true}
}

func (s *Server) capture(ctx context.Context, structured bool, run func(context.Context, config.Stdio) error) *mcp.CallToolResult {
	start := time.Now()
	var out bytes.Buffer
	err := run(ctx, config.Stdio{Stdin: bytes.NewReader(nil), Stdout: &out, Stderr: s.opts.Stderr})
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

func unmarshalArgs(args []byte, schema *jsonschema.Schema, v any) error {
	var members map[string]jsontext.Value
	if err := json.Unmarshal(args, &members); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	for _, name := range slices.Sorted(maps.Keys(members)) {
		if _, ok := schema.Properties[name]; !ok {
			return fmt.Errorf("invalid arguments: unknown argument %q; valid arguments: %s", name, strings.Join(slices.Sorted(maps.Keys(schema.Properties)), ", "))
		}
	}
	for _, name := range schema.Required {
		if m, ok := members[name]; !ok || m.Kind() == 'n' {
			return fmt.Errorf("invalid arguments: %s is required", name)
		}
	}
	if err := json.Unmarshal(args, v); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}

func patchArgs(values map[string]jsontext.Value) []map[string]string {
	var args []map[string]string
	for _, path := range slices.Sorted(maps.Keys(values)) {
		for _, v := range patchValues(values[path]) {
			args = append(args, map[string]string{path: v})
		}
	}
	return args
}

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
