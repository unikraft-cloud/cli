// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver

import (
	"bytes"
	"cmp"
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
	"unikraft.com/cli/internal/model"
	"unikraft.com/cli/internal/model/cmd"
)

// AddTools registers every tool a fully capable resource type supports.
func (s *Server) AddTools[R interface {
	model.GettableListableModel
	model.CreatableModel
	model.EditableModel
	model.DeletableModel
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

func (s *Server) AddListTool[R model.GettableListableModel](ctx context.Context) error {
	var empty R
	typ := empty.Type()
	if !s.enabled(typ.Name) {
		return nil
	}
	fields, err := empty.Fields(ctx)
	if err != nil {
		return err
	}
	schema := listSchema(typ, fields)
	s.srv.AddTool(&mcp.Tool{
		Name:        toolName(typ.Name, "list"),
		Title:       "List " + typ.Names,
		Description: listDescription(typ),
		InputSchema: schema,
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args listArgs
		if err := unmarshalArgs(req, schema, &args); err != nil {
			return rejected(ctx, req, err), nil
		}
		return s.capture(ctx, req, true, func(ctx context.Context, stdio config.Stdio) error {
			return (&cmd.ModelListCmd[R]{
				Targets:    args.Keys,
				Filter:     args.Filter,
				Sort:       args.Sort,
				FormatOpts: JSONFormat(args.Fields...),
			}).Run(ctx, stdio, s.opts.Partition)
		}), nil
	})
	return nil
}

type getArgs struct {
	Keys   []string `json:"keys"`
	Fields []string `json:"fields"`
}

// AddGetTool registers a get tool; an empty description selects the generic one.
func (s *Server) AddGetTool[R model.GettableModel](ctx context.Context, description string) error {
	var empty R
	typ := empty.Type()
	if !s.enabled(typ.Name) {
		return nil
	}
	fields, err := empty.Fields(ctx)
	if err != nil {
		return err
	}
	schema := getSchema(typ, fields)
	s.srv.AddTool(&mcp.Tool{
		Name:        toolName(typ.Name, "get"),
		Title:       "Inspect " + typ.Names,
		Description: cmp.Or(description, getDescription(typ)),
		InputSchema: schema,
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args getArgs
		if err := unmarshalArgs(req, schema, &args); err != nil {
			return rejected(ctx, req, err), nil
		}
		return s.capture(ctx, req, true, func(ctx context.Context, stdio config.Stdio) error {
			return (&cmd.ModelGetCmd[R]{
				Targets:    args.Keys,
				FormatOpts: JSONFormat(args.Fields...),
			}).Run(ctx, stdio, s.opts.Partition)
		}), nil
	})
	return nil
}

// ToolOption adjusts how a resource type's tools are registered.
type ToolOption func(*toolOptions)

type toolOptions struct {
	createDefaults map[string]any
	editNote       string
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

func (s *Server) AddCreateTool[R model.CreatableModel](ctx context.Context, opts ...ToolOption) error {
	var empty R
	typ := empty.Type()
	if !s.enabled(typ.Name) || s.opts.ReadOnly {
		return nil
	}
	fields, err := empty.Fields(ctx)
	if err != nil {
		return err
	}
	schema := createSchema(fields)
	var o toolOptions
	for _, opt := range opts {
		opt(&o)
	}
	defaults := make(map[string]jsontext.Value, len(o.createDefaults))
	for path, value := range o.createDefaults {
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
	s.srv.AddTool(&mcp.Tool{
		Name:        toolName(typ.Name, "create"),
		Title:       "Create " + typ.Name,
		Description: createDescription(typ, fields, schema.Properties["set"], defaults),
		InputSchema: schema,
		Annotations: mutatingAnnotations(false),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args createArgs
		if err := unmarshalArgs(req, schema, &args); err != nil {
			return rejected(ctx, req, err), nil
		}
		set := args.Set
		for path, value := range defaults {
			if v, ok := set[path]; !ok || v.Kind() == 'n' {
				if set == nil {
					set = map[string]jsontext.Value{}
				}
				set[path] = value
			}
		}
		return s.capture(ctx, req, true, func(ctx context.Context, stdio config.Stdio) error {
			return (&cmd.ModelCreateCmd[R]{
				Set:        patchArgs(set),
				DryRun:     args.DryRun,
				FormatOpts: JSONFormat(),
			}).Run(ctx, stdio, s.opts.Partition)
		}), nil
	})
	return nil
}

type editArgs struct {
	Key    string                    `json:"key"`
	Set    map[string]jsontext.Value `json:"set"`
	Add    map[string]jsontext.Value `json:"add"`
	Del    map[string]jsontext.Value `json:"del"`
	DryRun bool                      `json:"dry_run"`
}

func (s *Server) AddEditTool[R model.EditableModel](ctx context.Context, opts ...ToolOption) error {
	var empty R
	typ := empty.Type()
	if !s.enabled(typ.Name) || s.opts.ReadOnly {
		return nil
	}
	fields, err := empty.Fields(ctx)
	if err != nil {
		return err
	}
	schema := editSchema(typ, fields)
	var o toolOptions
	for _, opt := range opts {
		opt(&o)
	}
	s.srv.AddTool(&mcp.Tool{
		Name:        toolName(typ.Name, "edit"),
		Title:       "Edit " + typ.Name,
		Description: editDescription(typ, o.editNote),
		InputSchema: schema,
		Annotations: mutatingAnnotations(false),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args editArgs
		if err := unmarshalArgs(req, schema, &args); err != nil {
			return rejected(ctx, req, err), nil
		}
		return s.capture(ctx, req, true, func(ctx context.Context, stdio config.Stdio) error {
			return (&cmd.ModelEditCmd[R]{
				Target:     args.Key,
				Set:        patchArgs(args.Set),
				Add:        patchArgs(args.Add),
				Del:        patchArgs(args.Del),
				DryRun:     args.DryRun,
				FormatOpts: JSONFormat(),
			}).Run(ctx, stdio, s.opts.Partition)
		}), nil
	})
	return nil
}

type deleteArgs struct {
	Keys []string `json:"keys"`
}

func (s *Server) AddDeleteTool[R model.DeletableModel](ctx context.Context) error {
	var empty R
	typ := empty.Type()
	if !s.enabled(typ.Name) || s.opts.ReadOnly {
		return nil
	}
	if _, err := empty.Fields(ctx); err != nil {
		return err
	}
	schema := deleteSchema(typ)
	s.srv.AddTool(&mcp.Tool{
		Name:        toolName(typ.Name, "delete"),
		Title:       "Delete " + typ.Names,
		Description: deleteDescription(typ),
		InputSchema: schema,
		Annotations: destructiveAnnotations(),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args deleteArgs
		if err := unmarshalArgs(req, schema, &args); err != nil {
			return rejected(ctx, req, err), nil
		}
		if len(args.Keys) == 0 {
			return rejected(ctx, req, fmt.Errorf("no %s specified", typ.Names)), nil
		}
		return s.capture(ctx, req, true, func(ctx context.Context, stdio config.Stdio) error {
			return (&cmd.ModelRemoveCmd[R]{
				Targets:    args.Keys,
				FormatOpts: JSONFormat(),
			}).Run(ctx, stdio, s.opts.Partition)
		}), nil
	})
	return nil
}

func (s *Server) addCommandTool[In any](tool *mcp.Tool, structured bool, run func(context.Context, In, config.Stdio) error) error {
	schema, ok := tool.InputSchema.(*jsonschema.Schema)
	if tool.InputSchema == nil {
		var err error
		if schema, err = jsonschema.For[In](nil); err != nil {
			return fmt.Errorf("%s: %w", tool.Name, err)
		}
		tool.InputSchema = schema
	} else if !ok {
		return fmt.Errorf("%s: input schema must be a *jsonschema.Schema", tool.Name)
	}
	singleTypes(schema)
	s.srv.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var in In
		if err := unmarshalArgs(req, schema, &in); err != nil {
			return rejected(ctx, req, err), nil
		}
		return s.capture(ctx, req, structured, func(ctx context.Context, stdio config.Stdio) error {
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

func mutatingAnnotations(idempotent bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{DestructiveHint: new(false), IdempotentHint: idempotent}
}

func destructiveAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{DestructiveHint: new(true), IdempotentHint: true}
}

func (s *Server) capture(ctx context.Context, req *mcp.CallToolRequest, structured bool, run func(context.Context, config.Stdio) error) *mcp.CallToolResult {
	ctx = log.WithLogger(ctx, new(log.G(ctx).With().Str("tool", req.Params.Name).Logger()))
	args := []byte(req.Params.Arguments)
	if len(args) == 0 {
		args = []byte("{}")
	}
	log.G(ctx).Trace().RawJSON("arguments", args).Msg("calling tool")

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

func rejected(ctx context.Context, req *mcp.CallToolRequest, err error) *mcp.CallToolResult {
	log.G(ctx).Debug().Str("tool", req.Params.Name).Err(err).Msg("tool call rejected")
	return errorResult(err)
}

func errorResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		IsError: true,
	}
}

func unmarshalArgs(req *mcp.CallToolRequest, schema *jsonschema.Schema, v any) error {
	args := req.Params.Arguments
	if len(args) == 0 {
		args = []byte("{}")
	}
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
