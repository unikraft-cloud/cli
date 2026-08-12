// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/chzyer/readline"

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

	Plugin string            `name:"plugin" help:"Plugin name from the instance to run commands on."`
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

	return runSandboxShell(ctx, target.g, target.instance, target.plugin, c.Dir, c.Env, stdio)
}

// handleBuiltin runs a sigil-carrying line, reporting whether it asked the
// shell to quit. The caller has already established the line is a builtin
// invocation, so there is no falling back to the instance from here: a line
// that opens with the sigil and doesn't name a builtin is a mistake, not a
// command to forward.
func (sctx *ShellContext) handleBuiltin(line string) (exit bool) {
	fields, ok := shell.ParseBuiltinLine(line)
	if !ok {
		fmt.Fprintf(sctx.ErrOut, "%s builtins run here rather than on the instance, so they can't be redirected, piped or chained - %s must be the whole line\n",
			shell.ShellErrorStyle.Render("error:"), shell.ShellValueStyle.Render(strings.TrimSpace(line)))
		return false
	}

	switch fields[0] {
	case "exit":
		return true

	case "clear":
		fmt.Fprint(sctx.Out, ansi.CursorHomePosition+ansi.EraseEntireScreen)
		return false

	case "cd":
		target := "/"
		if len(fields) > 1 {
			target = strings.Trim(fields[1], `"'`)
		}
		newDir := shell.ResolveDir(sctx.State.Dir, target)
		var buf strings.Builder
		opts := ExecOpts{
			Cmd:    []string{fmt.Sprintf("[ -d %s ] && echo OK", QuoteShellArg(newDir))},
			Raw:    true,
			Plugin: sctx.Plugin,
		}
		_ = execSandboxInstance(sctx.Ctx, &buf, nil, sctx.G, sctx.Key, opts)
		if strings.TrimSpace(buf.String()) != "OK" {
			fmt.Fprintf(sctx.Out, "cd: %s: No such file or directory\n", target)
			return false
		}
		sctx.State.Dir = newDir
		return false

	case "export":
		for _, assign := range fields[1:] {
			if k, v, ok := shell.ParseAssignment(assign); ok {
				sctx.State.Env[k] = v
			}
		}
		return false
	}

	if !sctx.Builtins.HasCommand(fields[0]) {
		fmt.Fprintf(sctx.ErrOut, "%s %s: not a builtin, and %s runs nothing on the instance - drop the %s to send it there\n",
			shell.ShellErrorStyle.Render("error:"), fields[0],
			shell.ShellValueStyle.Render(shell.BuiltinSigil+fields[0]),
			shell.ShellValueStyle.Render(shell.BuiltinSigil))
		return false
	}

	kctx, err := sctx.Builtins.Parse(fields)
	if err != nil {
		fmt.Fprintf(sctx.ErrOut, "%s %v\n", shell.ShellErrorStyle.Render("error:"), err)
		return false
	}

	if err := kctx.Run(sctx); err != nil {
		fmt.Fprintf(sctx.ErrOut, "%v\n", err)
	}

	return false
}

func executeRemote(ctx context.Context, stdout io.Writer, stdin io.Reader, g *group.Group[multimetro.MetroClient], key multimetro.Key, plugin string, state *shell.State, line string) error {
	return execSandboxInstance(ctx, stdout, stdin, g, key, ExecOpts{
		Cmd:    []string{line},
		Dir:    state.Dir,
		Env:    state.Env,
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
	State     *shell.State
	Cache     *shell.HistoryCache
	Completer *shell.SandboxCompleter
	Builtins  *shell.Builtins
}

func (sctx *ShellContext) startBackgroundSync() {
	if sctx.State.Synced || !sctx.State.Running {
		return
	}
	sctx.State.Synced = true
	go sctx.Cache.SyncFromRemote(sctx.Ctx, sctx.G, sctx.Key, sctx.Plugin)
	go fetchRemoteAutocompleteCommands(sctx.Ctx, sctx.G, sctx.Key, sctx.Plugin, sctx.Completer)
}

func runSandboxShell(ctx context.Context, g *group.Group[multimetro.MetroClient], instance Instance, plugin, initialDir string, initialEnv map[string]string, stdio config.Stdio) error {
	key := instance.Key().(multimetro.Key)
	ih := interrupt.FromContext(ctx)
	isRunning := requireRunningInstance(instance) == nil

	builtins, err := shell.NewBuiltins(&ShellCmd{}, stdio.Stdout, stdio.Stderr)
	if err != nil {
		return fmt.Errorf("failed to initialize shell parser: %w", err)
	}

	cache := &shell.HistoryCache{Builtins: builtins}
	state := shell.NewState(initialDir, initialEnv, isRunning)
	pump := shell.NewStdinPump(stdio.Stdin)
	defer pump.Close()

	completer := shell.NewSandboxCompleter(builtins.Completion())
	painter := &shell.ShellPainter{Builtins: builtins}

	fmt.Fprintln(stdio.Stdout, shell.ShellTitleStyle.Render("▀▀▀ Unikraft Sandbox Shell"))
	fmt.Fprintf(stdio.Stdout, "  %s %s %s\n", shell.ShellLabelStyle.Render("■"), shell.ShellLabelStyle.Render("Target:"), shell.ShellValueStyle.Render(key.String()))
	fmt.Fprintf(stdio.Stdout, "  %s %s %s\n", shell.ShellLabelStyle.Render("■"), shell.ShellLabelStyle.Render("Plugin:"), shell.ShellValueStyle.Render(plugin))
	fmt.Fprintln(stdio.Stdout)
	fmt.Fprintln(stdio.Stdout, shell.ShellNoticeStyle.Render("⚠ EXPERIMENTAL: this shell does not support a PTY, so full-screen apps and job control won't work."))
	fmt.Fprintln(stdio.Stdout)
	builtins.Help(stdio.Stdout)
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
		shell.SetShellPrompt(rl, key.String(), state.Dir)

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

		_ = rl.SaveHistory(line)

		// Builtins stay out of the command cache: it mirrors what the
		// instance has run, and it is what `history rerun` replays back at
		// the instance.
		if shell.HasBuiltinSigil(line) {
			if sctx.handleBuiltin(line) {
				return nil
			}
			continue
		}

		cache.Append(line)

		if !state.Running {
			log.G(ctx).Debug().Str("cmd", line).Msg("shell: skipping command, instance not running")
			fmt.Fprintf(stdio.Stdout, "%s instance is not running. Use '%sstart' to start it.\n",
				shell.ShellErrorStyle.Render("error:"), shell.BuiltinSigil)
			continue
		}

		cmdCtx, cancelCmd := context.WithCancel(ctx)
		var restore func()
		if ih != nil {
			restore = ih.Set(cancelCmd)
		}

		cmdIn := &shell.CmdReader{S: pump, Ctx: cmdCtx}

		log.G(ctx).Debug().Str("cmd", line).Str("dir", state.Dir).Msg("shell: executing remote command")
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
