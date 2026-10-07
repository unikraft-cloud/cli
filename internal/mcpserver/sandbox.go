// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcpserver

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"unikraft.com/cli/internal/config"
)

type SandboxCommands interface {
	Exec(ctx context.Context, stdio config.Stdio, instance string, opts SandboxExecOptions) (int, error)
	ReadFile(ctx context.Context, instance, path string) ([]byte, error)
	WriteFile(ctx context.Context, instance, path string, data []byte, opts SandboxWriteOptions) error
}

type SandboxExecOptions struct {
	Args    []string
	Cmdline string
	Dir     string
	Env     map[string]string
	Stdin   string
}

type SandboxWriteOptions struct {
	Append  bool
	Parents bool
}

type sandboxExecArgs struct {
	Key       string            `json:"key" jsonschema:"The sandbox instance: a name, UUID or metro/name key."`
	Command   jsontext.Value    `json:"command"`
	Dir       string            `json:"dir,omitzero" jsonschema:"Directory to run the command in."`
	Env       map[string]string `json:"env,omitzero" jsonschema:"The whole environment the command sees, not an addition to it."`
	Stdin     string            `json:"stdin,omitzero" jsonschema:"Text fed to the command's standard input."`
	TimeoutMS int64             `json:"timeout_ms,omitzero" jsonschema:"Milliseconds to wait before the command is stopped. Defaults to the server's timeout."`
}

type sandboxReadArgs struct {
	Key  string `json:"key" jsonschema:"The sandbox instance: a name, UUID or metro/name key."`
	Path string `json:"path" jsonschema:"Absolute path of the file inside the sandbox."`
}

type sandboxWriteArgs struct {
	Key     string `json:"key" jsonschema:"The sandbox instance: a name, UUID or metro/name key."`
	Path    string `json:"path" jsonschema:"Absolute path of the file inside the sandbox."`
	Content string `json:"content" jsonschema:"The text to write."`
	Append  bool   `json:"append,omitzero" jsonschema:"Append to the file instead of replacing it."`
	Parents bool   `json:"parents,omitzero" jsonschema:"Create the missing parent directories."`
}

var errExecTimeout = errors.New("sandbox command timed out")

func (s *Server) AddSandboxTools(commands SandboxCommands) error {
	if !s.enabled("sandbox", false) {
		return nil
	}
	err := s.addCommandTool(&mcp.Tool{
		Name:        "sandbox_read",
		Title:       "Read sandbox file",
		Description: "Read a text file from a running sandbox instance. Returns its content.",
		Annotations: readOnlyAnnotations(),
	}, false, func(ctx context.Context, in sandboxReadArgs, stdio config.Stdio) error {
		data, err := commands.ReadFile(ctx, in.Key, in.Path)
		if err != nil {
			return err
		}
		if !utf8.Valid(data) {
			return fmt.Errorf("%s is not UTF-8 text; read it with the unikraft instance read CLI command", in.Path)
		}
		_, err = stdio.Stdout.Write(data)
		return err
	})
	if err != nil || s.opts.ReadOnly {
		return err
	}

	exec, err := jsonschema.For[sandboxExecArgs](nil)
	if err != nil {
		return err
	}
	exec.Properties["command"] = &jsonschema.Schema{
		AnyOf:       []*jsonschema.Schema{{Type: "array", Items: &jsonschema.Schema{Type: "string"}}, {Type: "string"}},
		Description: "The program and its arguments as an array of strings, or one string for the shell to interpret, so that pipes and redirections work.",
	}
	return errors.Join(
		s.addCommandTool(&mcp.Tool{
			Name:        "sandbox_exec",
			Title:       "Run in sandbox",
			Description: "Run a command inside a running sandbox: an instance with the sandbox plugin loaded, which instance_create makes when plugins holds the plugin. Returns the command's combined output; a non-zero exit status is reported as an error after the output.",
			InputSchema: exec,
			Annotations: mutatingAnnotations(),
		}, false, func(ctx context.Context, in sandboxExecArgs, stdio config.Stdio) error {
			opts := SandboxExecOptions{Dir: in.Dir, Env: in.Env, Stdin: in.Stdin}
			switch in.Command.Kind() {
			case '[':
				if err := json.Unmarshal(in.Command, &opts.Args); err != nil {
					return fmt.Errorf("invalid command: %w", err)
				}
			case '"':
				if err := json.Unmarshal(in.Command, &opts.Cmdline); err != nil {
					return fmt.Errorf("invalid command: %w", err)
				}
			default:
				return errors.New("invalid command: give an array of strings or one command line")
			}
			if len(opts.Args) == 0 && opts.Cmdline == "" {
				return errors.New("invalid command: it is empty")
			}
			timeout := time.Duration(in.TimeoutMS) * time.Millisecond
			if timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeoutCause(ctx, timeout, errExecTimeout)
				defer cancel()
			}
			code, err := commands.Exec(ctx, stdio, in.Key, opts)
			if errors.Is(context.Cause(ctx), errExecTimeout) {
				return fmt.Errorf("command timed out after %s", timeout)
			}
			if err != nil {
				return err
			}
			if code != 0 {
				return fmt.Errorf("exit status %d", code)
			}
			return nil
		}),
		s.addCommandTool(&mcp.Tool{
			Name:        "sandbox_write",
			Title:       "Write sandbox file",
			Description: "Write a text file inside a running sandbox instance, replacing it unless append is set. Returns a confirmation.",
			Annotations: mutatingAnnotations(),
		}, false, func(ctx context.Context, in sandboxWriteArgs, stdio config.Stdio) error {
			if err := commands.WriteFile(ctx, in.Key, in.Path, []byte(in.Content), SandboxWriteOptions{Append: in.Append, Parents: in.Parents}); err != nil {
				return err
			}
			verb := "written"
			if in.Append {
				verb = "appended"
			}
			_, err := fmt.Fprintf(stdio.Stdout, "%d bytes %s to %s\n", len(in.Content), verb, in.Path)
			return err
		}),
	)
}
