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
	key := multimetro.ParseKey(c.Target)

	sandbox, opErr := Instance{}.Get(ctx, []string{key.String()})
	if opErr != nil && len(sandbox) == 0 {
		return opErr
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}

	instance := sandbox[0].(Instance)
	resolvedKey := instance.Key().(multimetro.Key)

	return runSandboxShell(ctx, g, resolvedKey, c.Plugin, c.Dir, c.Env, stdio)
}

// shellState tracks the client-side notion of "current directory" and
// exported environment variables between remote invocations. There is no
// persistent remote shell process backing this session — every command is
// a discrete remote exec — so cd/env assignments have to be remembered
// here and re-applied (via ExecOpts.Dir/Env) on the next call.
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

var knownBuiltins = map[string]bool{
	"get": true, "help": true, "restart": true, "start": true,
	"stop": true, "suspend": true, "mount": true, "unmount": true,
	"edit": true, "volumes": true, "history": true,
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
		escapedDir := strings.ReplaceAll(newDir, "'", "'\\''")
		var buf strings.Builder
		opts := ExecOpts{
			Cmd:    []string{fmt.Sprintf("[ -d '%s' ] && echo OK", escapedDir)},
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

	if !knownBuiltins[fields[0]] {
		return false
	}

	var cmd ShellCmd
	parser, err := kong.New(&cmd,
		kong.Name(""),
		kong.NoDefaultHelp(),
		kong.Exit(func(int) {}),
		kong.Writers(sctx.Out, sctx.ErrOut),
	)
	if err != nil {
		fmt.Fprintf(sctx.ErrOut, "%s failed to initialize shell parser: %v\n", shell.ShellErrorStyle.Render("error:"), err)
		return true
	}

	kctx, err := parser.Parse(fields)
	if err != nil {
		fmt.Fprintf(sctx.ErrOut, "%s %v\n", shell.ShellErrorStyle.Render("error:"), err)
		return true
	}

	if err := kctx.Run(sctx); err != nil {
		fmt.Fprintf(sctx.ErrOut, "%v\n", err)
	}

	return true
}

// shellFields splits line into shell-style words, honoring quoting so that
// e.g. `cd "my dir"` keeps "my dir" as a single argument instead of being
// split on the space. Constructs that can't be reduced to plain text
// (variables, substitutions, globs, ...) fall back to naive whitespace
// splitting - this is only used to detect and parse local builtins, and
// anything else is sent to the remote shell unparsed regardless.
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

// executeRemote sends the raw command line straight to the sandbox,
// unparsed on the client — quoting, pipes, redirects, and variable
// expansion are all resolved by the remote shell, not locally.
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
	fetchCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var buf strings.Builder
	opts := ExecOpts{
		Cmd:         []string{"sh -c 'IFS=:; for d in $PATH; do ls -1 \"$d\" 2>/dev/null; done | sort -u'"},
		Raw:         true,
		Plugin:      plugin,
		TimeoutMsec: 10000,
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
	RL        *readline.Instance
}

// startBackgroundSync kicks off history and autocomplete syncing against the
// remote instance. It is a no-op if already started, or if the instance
// isn't running yet - both would just fail against a stopped instance, so
// this is called once at shell startup if the instance is already running,
// and again from the start/restart builtins once it comes up.
func (sctx *ShellContext) startBackgroundSync() {
	if sctx.State.synced || !sctx.State.running {
		return
	}
	sctx.State.synced = true
	go sctx.Cache.SyncFromRemote(sctx.Ctx, sctx.G, sctx.Key, sctx.Plugin)
	go fetchRemoteAutocompleteCommands(sctx.Ctx, sctx.G, sctx.Key, sctx.Plugin, sctx.Completer)
}

func runSandboxShell(ctx context.Context, g *group.Group[multimetro.MetroClient], key multimetro.Key, plugin, initialDir string, initialEnv map[string]string, stdio config.Stdio) error {
	cache := &shell.HistoryCache{}
	ih := interrupt.FromContext(ctx)

	isRunning := false
	resources, err := Instance{}.Get(ctx, []string{key.String()})
	if err == nil && len(resources) > 0 {
		isRunning = instanceSandboxReady(resources[0].(Instance))
	}

	state := newShellState(initialDir, initialEnv, isRunning)
	pump := shell.NewStdinPump(stdio.Stdin)
	defer pump.Close()

	completer := shell.NewSandboxCompleter()
	readlineReader := &shell.ReadlineReader{S: pump}
	painter := &shell.ShellPainter{}

	fmt.Fprintln(stdio.Stdout, shell.ShellTitleStyle.Render("▀▀▀ Unikraft Sandbox Shell"))
	fmt.Fprintf(stdio.Stdout, "  %s %s %s\n", shell.ShellLabelStyle.Render("■"), shell.ShellLabelStyle.Render("Target:"), shell.ShellValueStyle.Render(key.String()))
	fmt.Fprintf(stdio.Stdout, "  %s %s %s\n", shell.ShellLabelStyle.Render("■"), shell.ShellLabelStyle.Render("Plugin:"), shell.ShellValueStyle.Render(plugin))
	fmt.Fprintln(stdio.Stdout)
	fmt.Fprintln(stdio.Stdout, shell.ShellNoticeStyle.Render("⚠ EXPERIMENTAL: this shell does not support a PTY, so full-screen apps and job control won't work."))
	fmt.Fprintln(stdio.Stdout)
	builtinHelp(stdio.Stdout)
	fmt.Fprintln(stdio.Stdout)

	// Readline blanks and redraws the prompt line on every keystroke, in two
	// separate writes; term coalesces them so the blank never reaches the
	// screen. See shell.TerminalWriter.
	term := shell.NewTerminalWriter(stdio.Stdout)
	defer func() { _ = term.Flush() }()

	rl, err := readline.NewEx(&readline.Config{
		Stdin:             readlineReader,
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
		RL:        rl,
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

		// Take the terminal back from readline before anything below writes
		// to stdio.Stdout directly.
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
			print("\033[H\033[2J")
			_ = rl.SaveHistory(line)
			continue
		}

		_ = rl.SaveHistory(line)
		cache.Append(line)

		// Pass execution to the local handler; if it processes it, skip remote execution
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
