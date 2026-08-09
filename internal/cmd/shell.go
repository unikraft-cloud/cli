// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"fmt"
	"io"
	"maps"
	"path"
	"strings"
	"time"

	"github.com/alecthomas/kong"
	"github.com/charmbracelet/x/ansi"
	"github.com/chzyer/readline"
	"mvdan.cc/sh/v3/syntax"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/interrupt"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/shell"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
)

type ShellSandboxInstanceCmd struct {
	Target string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instance to open a shell on."`

	Plugin string            `name:"plugin" help:"Plugin name from the instance to run commands on." default:"sandbox"`
	Dir    string            `name:"dir" help:"Initial working directory"`
	Env    map[string]string `name:"env" help:"Initial environment variables to set (KEY=VALUE)" mapsep:","`
}

func (cmd ShellSandboxInstanceCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Open an interactive shell session to a sandbox instance",
			Commands: []string{
				"unikraft instance shell my-instance",
			},
		},
	}
}

func (c *ShellSandboxInstanceCmd) Run(ctx context.Context, stdio config.Stdio) error {
	target, err := resolveSandboxTarget(ctx, c.Target, c.Plugin, allowStopped)
	if err != nil {
		return err
	}

	return runSandboxShell(ctx, target.g, target.instance, c.Plugin, c.Dir, c.Env, stdio)
}

type shellState struct {
	dir     string
	env     map[string]string
	running bool
	synced  bool
}

func newShellState(initialDir string, initialEnv map[string]string, running bool) *shellState {
	dir := "/"
	if initialDir != "" {
		dir = initialDir
	}

	env := make(map[string]string)
	maps.Copy(env, initialEnv)

	return &shellState{
		dir:     path.Clean(dir),
		env:     env,
		running: running,
	}
}

type shellBuiltins struct {
	parser *kong.Kong
	names  map[string]bool
}

func newShellBuiltins(out, errOut io.Writer) (*shellBuiltins, error) {
	parser, err := kong.New(&ShellCmd{},
		kong.Name(""),
		kong.NoDefaultHelp(),
		kong.Exit(func(int) {}),
		kong.Writers(out, errOut),
	)
	if err != nil {
		return nil, err
	}

	names := make(map[string]bool)
	for _, child := range parser.Model.Children {
		if child.Type != kong.CommandNode {
			continue
		}
		names[child.Name] = true
		for _, alias := range child.Aliases {
			names[alias] = true
		}
	}

	return &shellBuiltins{parser: parser, names: names}, nil
}

func (s *shellState) handleBuiltin(sctx *ShellContext, line string) bool {
	fields := shellFields(line)
	if len(fields) == 0 {
		return false
	}

	switch fields[0] {
	case "cd":
		target := "/"
		if len(fields) > 1 {
			target = strings.Trim(fields[1], `"'`)
		}
		newDir := resolveDir(s.dir, target)
		var buf strings.Builder
		opts := ExecOpts{
			Cmd:    []string{fmt.Sprintf("[ -d %s ] && echo OK", QuoteShellArg(newDir))},
			Raw:    true,
			Plugin: sctx.Plugin,
		}
		_ = execSandboxInstance(sctx.Ctx, &buf, nil, sctx.G, sctx.Key, opts)
		if strings.TrimSpace(buf.String()) != "OK" {
			fmt.Fprintf(sctx.Out, "cd: %s: No such file or directory\n", target)
			return true
		}
		s.dir = newDir
		return true

	case "export":
		for _, assign := range fields[1:] {
			if k, v, ok := parseAssignment(assign); ok {
				s.env[k] = v
			}
		}
		return true
	}

	if len(fields) == 1 {
		if k, v, ok := parseAssignment(fields[0]); ok {
			s.env[k] = v
			return true
		}
	}

	if !sctx.Builtins.names[fields[0]] {
		return false
	}

	kctx, err := sctx.Builtins.parser.Parse(fields)
	if err != nil {
		fmt.Fprintf(sctx.ErrOut, "%s %v\n", shell.ShellErrorStyle.Render("error:"), err)
		return true
	}

	if err := kctx.Run(sctx); err != nil {
		fmt.Fprintf(sctx.ErrOut, "%v\n", err)
	}

	return true
}

func shellFields(line string) []string {
	parser := syntax.NewParser()
	var fields []string
	for w, err := range parser.WordsSeq(strings.NewReader(line)) {
		if err != nil {
			return strings.Fields(line)
		}
		field, ok := literalWord(w)
		if !ok {
			return strings.Fields(line)
		}
		fields = append(fields, field)
	}
	return fields
}

func literalWord(w *syntax.Word) (string, bool) {
	var sb strings.Builder
	for _, part := range w.Parts {
		if !literalWordPart(&sb, part) {
			return "", false
		}
	}
	return sb.String(), true
}

func literalWordPart(sb *strings.Builder, part syntax.WordPart) bool {
	switch p := part.(type) {
	case *syntax.Lit:
		sb.WriteString(p.Value)
	case *syntax.SglQuoted:
		sb.WriteString(p.Value)
	case *syntax.DblQuoted:
		for _, inner := range p.Parts {
			if !literalWordPart(sb, inner) {
				return false
			}
		}
	default:
		return false
	}
	return true
}

func resolveDir(cur, target string) string {
	if target == "" || target == "~" {
		return "/"
	}
	if strings.HasPrefix(target, "/") {
		return path.Clean(target)
	}
	return path.Clean(path.Join(cur, target))
}

func parseAssignment(s string) (key, val string, ok bool) {
	parts := strings.SplitN(s, "=", 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", "", false
	}
	key = parts[0]
	if !syntax.ValidName(key) {
		return "", "", false
	}
	val = strings.Trim(parts[1], `"'`)
	return key, val, true
}

func executeRemote(ctx context.Context, stdout io.Writer, stdin io.Reader, g *group.Group[multimetro.MetroClient], key multimetro.Key, plugin string, state *shellState, line string) error {
	return execSandboxInstance(ctx, stdout, stdin, g, key, ExecOpts{
		Cmd:    []string{line},
		Dir:    state.dir,
		Env:    state.env,
		Plugin: plugin,
		Raw:    true,
	})
}

func fetchRemoteAutocompleteCommands(ctx context.Context, g *group.Group[multimetro.MetroClient], key multimetro.Key, plugin string, completer *shell.SandboxCompleter) {
	log.G(ctx).Debug().Msg("shell: fetching remote autocomplete commands")
	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var buf strings.Builder
	opts := ExecOpts{
		Cmd:    []string{"sh -c 'IFS=:; for d in $PATH; do ls -1 \"$d\" 2>/dev/null; done | sort -u'"},
		Raw:    true,
		Plugin: plugin,
	}

	err := execSandboxInstance(fetchCtx, &buf, nil, g, key, opts)
	if err != nil {
		log.G(ctx).Debug().Err(err).Msg("shell: remote autocomplete exec failed")
		return
	}

	raw := buf.String()
	log.G(ctx).Debug().Int("raw_len", len(raw)).Msg("shell: remote autocomplete raw output")

	lines := strings.Split(raw, "\n")
	var cmds []string
	seen := make(map[string]bool)

	for _, line := range lines {
		cmd := strings.TrimSpace(line)
		if cmd != "" && !strings.ContainsAny(cmd, " \t/") && !seen[cmd] {
			cmds = append(cmds, cmd)
			seen[cmd] = true
		}
	}

	log.G(ctx).Debug().Int("commands", len(cmds)).Msg("shell: remote autocomplete filtered commands")

	if len(cmds) > 0 {
		completer.SetRemoteCommands(cmds)
		log.G(ctx).Debug().Msg("shell: remote autocomplete commands set")
	}
}

type ShellContext struct {
	Ctx       context.Context
	Out       io.Writer
	ErrOut    io.Writer
	G         *group.Group[multimetro.MetroClient]
	Key       multimetro.Key
	Plugin    string
	State     *shellState
	Cache     *shell.HistoryCache
	Completer *shell.SandboxCompleter
	Builtins  *shellBuiltins
}

func (sctx *ShellContext) startBackgroundSync() {
	if sctx.State.synced || !sctx.State.running {
		return
	}
	sctx.State.synced = true
	go sctx.Cache.SyncFromRemote(sctx.Ctx, sctx.G, sctx.Key, sctx.Plugin)
	go fetchRemoteAutocompleteCommands(sctx.Ctx, sctx.G, sctx.Key, sctx.Plugin, sctx.Completer)
}

func runSandboxShell(ctx context.Context, g *group.Group[multimetro.MetroClient], instance Instance, plugin, initialDir string, initialEnv map[string]string, stdio config.Stdio) error {
	key := instance.Key().(multimetro.Key)
	cache := &shell.HistoryCache{}
	ih := interrupt.FromContext(ctx)
	isRunning := requireRunningInstance(instance) == nil

	builtins, err := newShellBuiltins(stdio.Stdout, stdio.Stderr)
	if err != nil {
		return fmt.Errorf("failed to initialize shell parser: %w", err)
	}

	state := newShellState(initialDir, initialEnv, isRunning)
	pump := shell.NewStdinPump(stdio.Stdin)
	defer pump.Close()

	completer := shell.NewSandboxCompleter()
	painter := &shell.ShellPainter{}

	fmt.Fprintln(stdio.Stdout, shell.ShellTitleStyle.Render("▀▀▀ Unikraft Sandbox Shell"))
	fmt.Fprintf(stdio.Stdout, "  %s %s %s\n", shell.ShellLabelStyle.Render("■"), shell.ShellLabelStyle.Render("Target:"), shell.ShellValueStyle.Render(key.String()))
	fmt.Fprintf(stdio.Stdout, "  %s %s %s\n", shell.ShellLabelStyle.Render("■"), shell.ShellLabelStyle.Render("Plugin:"), shell.ShellValueStyle.Render(plugin))
	fmt.Fprintln(stdio.Stdout)
	fmt.Fprintln(stdio.Stdout, shell.ShellNoticeStyle.Render("⚠ EXPERIMENTAL: this shell does not support a PTY, so full-screen apps and job control won't work."))
	fmt.Fprintln(stdio.Stdout)
	builtinHelp(stdio.Stdout)
	fmt.Fprintln(stdio.Stdout)

	term := shell.NewTerminalWriter(stdio.Stdout)
	defer func() { _ = term.Flush() }()

	rl, err := readline.NewEx(&readline.Config{
		Stdin:             pump,
		Stdout:            term,
		Painter:           painter,
		AutoComplete:      completer,
		InterruptPrompt:   "^C",
		EOFPrompt:         "exit",
		HistorySearchFold: true,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize readline: %w", err)
	}
	defer rl.Close()

	sctx := &ShellContext{
		Ctx:       ctx,
		Out:       stdio.Stdout,
		ErrOut:    stdio.Stderr,
		G:         g,
		Key:       key,
		Plugin:    plugin,
		State:     state,
		Cache:     cache,
		Completer: completer,
		Builtins:  builtins,
	}

	cache.OnSynced = func(entries []shell.HistoryEntry) {
		for _, entry := range entries {
			_ = rl.SaveHistory(entry.Cmd)
		}
	}
	sctx.startBackgroundSync()

	for {
		shell.SetShellPrompt(rl, key.String(), state.dir)

		line, err := rl.Readline()

		_ = term.Flush()

		if err != nil {
			if err == readline.ErrInterrupt {
				continue
			}
			break
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if line == "exit" {
			return nil
		}

		if line == "clear" {
			fmt.Fprint(stdio.Stdout, ansi.CursorHomePosition+ansi.EraseEntireScreen)
			_ = rl.SaveHistory(line)
			continue
		}

		_ = rl.SaveHistory(line)
		cache.Append(line)

		if state.handleBuiltin(sctx, line) {
			continue
		}

		if !state.running {
			log.G(ctx).Debug().Str("cmd", line).Msg("shell: skipping command, instance not running")
			fmt.Fprintf(stdio.Stdout, "%s instance is not running. Use 'start' to start it.\n", shell.ShellErrorStyle.Render("error:"))
			continue
		}

		cmdCtx, cancelCmd := context.WithCancel(ctx)
		var restore func()
		if ih != nil {
			restore = ih.Set(cancelCmd)
		}

		cmdIn := &shell.CmdReader{S: pump, Ctx: cmdCtx}

		log.G(ctx).Debug().Str("cmd", line).Str("dir", state.dir).Msg("shell: executing remote command")
		runErr := executeRemote(cmdCtx, stdio.Stdout, cmdIn, g, key, plugin, state, line)
		wasInterrupted := cmdCtx.Err() != nil

		if restore != nil {
			restore()
		}
		cancelCmd()

		if runErr != nil {
			if wasInterrupted {
				log.G(ctx).Debug().Str("cmd", line).Msg("shell: command interrupted")
				fmt.Fprintf(stdio.Stdout, "\n%s\n", shell.ShellErrorStyle.Render("Interrupt: ^C"))
			} else {
				log.G(ctx).Debug().Err(runErr).Str("cmd", line).Msg("shell: command failed")
				fmt.Fprintln(stdio.Stderr, shell.ShellErrorStyle.Render("error: ")+runErr.Error())
			}
		} else {
			log.G(ctx).Debug().Str("cmd", line).Msg("shell: command completed")
		}
	}

	return nil
}
