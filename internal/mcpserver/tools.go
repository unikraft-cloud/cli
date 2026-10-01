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
}](ctx context.Context) error {
	return errors.Join(
		s.AddListTool[R](ctx),
		s.AddCreateTool[R](ctx),
		s.AddEditTool[R](ctx),
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
	var empty R
	fields, err := empty.Fields(ctx)
	if err != nil {
		return err
	}
	typ := empty.Type()
	s.srv.AddTool(&mcp.Tool{
		Name:        toolName(typ.Name, "list"),
		Description: listDescription(typ, fields),
		InputSchema: listSchema(typ),
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args listArgs
		if err := unmarshalArgs(req, &args); err != nil {
			return rejected(ctx, req, err), nil
		}
		return s.capture(ctx, req, true, func(ctx context.Context, stdio config.Stdio) error {
			return (&cmd.ResourceListCmd[R]{
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

func (s *Server) AddGetTool[R resource.GettableResource](ctx context.Context) error {
	var empty R
	if _, err := empty.Fields(ctx); err != nil {
		return err
	}
	typ := empty.Type()
	s.srv.AddTool(&mcp.Tool{
		Name:        toolName(typ.Name, "get"),
		Description: getDescription(typ),
		InputSchema: getSchema(typ),
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args getArgs
		if err := unmarshalArgs(req, &args); err != nil {
			return rejected(ctx, req, err), nil
		}
		return s.capture(ctx, req, true, func(ctx context.Context, stdio config.Stdio) error {
			return (&cmd.ResourceGetCmd[R]{
				Targets:    args.Keys,
				FormatOpts: JSONFormat(args.Fields...),
			}).Run(ctx, stdio, s.opts.Partition)
		}), nil
	})
	return nil
}

type createArgs struct {
	Set    map[string]jsontext.Value `json:"set"`
	DryRun bool                      `json:"dry_run"`
}

func (s *Server) AddCreateTool[R resource.CreatableResource](ctx context.Context) error {
	if s.opts.ReadOnly {
		return nil
	}
	var empty R
	fields, err := empty.Fields(ctx)
	if err != nil {
		return err
	}
	typ := empty.Type()
	s.srv.AddTool(&mcp.Tool{
		Name:        toolName(typ.Name, "create"),
		Description: createDescription(typ),
		InputSchema: createSchema(fields),
		Annotations: mutatingAnnotations(false),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args createArgs
		if err := unmarshalArgs(req, &args); err != nil {
			return rejected(ctx, req, err), nil
		}
		return s.capture(ctx, req, true, func(ctx context.Context, stdio config.Stdio) error {
			return (&cmd.ResourceCreateCmd[R]{
				Set:        patchArgs(args.Set),
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

func (s *Server) AddEditTool[R resource.EditableResource](ctx context.Context) error {
	if s.opts.ReadOnly {
		return nil
	}
	var empty R
	fields, err := empty.Fields(ctx)
	if err != nil {
		return err
	}
	typ := empty.Type()
	s.srv.AddTool(&mcp.Tool{
		Name:        toolName(typ.Name, "edit"),
		Description: editDescription(typ),
		InputSchema: editSchema(typ, fields),
		Annotations: mutatingAnnotations(false),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args editArgs
		if err := unmarshalArgs(req, &args); err != nil {
			return rejected(ctx, req, err), nil
		}
		return s.capture(ctx, req, true, func(ctx context.Context, stdio config.Stdio) error {
			return (&cmd.ResourceEditCmd[R]{
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

func (s *Server) AddDeleteTool[R resource.DeletableResource](ctx context.Context) error {
	if s.opts.ReadOnly {
		return nil
	}
	var empty R
	if _, err := empty.Fields(ctx); err != nil {
		return err
	}
	typ := empty.Type()
	s.srv.AddTool(&mcp.Tool{
		Name:        toolName(typ.Name, "delete"),
		Description: deleteDescription(typ),
		InputSchema: deleteSchema(typ),
		Annotations: destructiveAnnotations(),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args deleteArgs
		if err := unmarshalArgs(req, &args); err != nil {
			return rejected(ctx, req, err), nil
		}
		if len(args.Keys) == 0 {
			return rejected(ctx, req, fmt.Errorf("no %s specified", typ.Names)), nil
		}
		return s.capture(ctx, req, true, func(ctx context.Context, stdio config.Stdio) error {
			return (&cmd.ResourceRemoveCmd[R]{
				Targets:    args.Keys,
				FormatOpts: JSONFormat(),
			}).Run(ctx, stdio, s.opts.Partition)
		}), nil
	})
	return nil
}

func (s *Server) addCommandTool[In any](tool *mcp.Tool, structured bool, run func(context.Context, In, config.Stdio) error) error {
	if tool.InputSchema == nil {
		schema, err := jsonschema.For[In](nil)
		if err != nil {
			return fmt.Errorf("%s: %w", tool.Name, err)
		}
		tool.InputSchema = schema
	}
	if schema, ok := tool.InputSchema.(*jsonschema.Schema); ok {
		singleTypes(schema)
	}
	s.srv.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var in In
		if err := unmarshalArgs(req, &in); err != nil {
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

func unmarshalArgs(req *mcp.CallToolRequest, v any) error {
	if len(req.Params.Arguments) == 0 {
		return nil
	}
	if err := json.Unmarshal(req.Params.Arguments, v); err != nil {
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
