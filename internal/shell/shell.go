// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/chzyer/readline"

	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/interrupt"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/patch"
)

// The resource types the builtins work with. They are resolved from the
// registry by name, so the shell never names a concrete resource type.
const (
	instanceResource = "instance"
	volumeResource   = "volume"
)

// ExecFunc runs a command line on the instance the shell is attached to. The
// shell owns no transport of its own: the caller supplies the same one that
// `unikraft instance exec` runs on.
type ExecFunc func(ctx context.Context, out io.Writer, in io.Reader, dir string, env map[string]string, line string) error

// Lifecycle points the lifecycle builtins at the CLI's own start, stop, suspend
// and restart commands, so their output and semantics stay identical to running
// them from outside the shell.
type Lifecycle struct {
	Start   func(ctx context.Context, stdio config.Stdio) error
	Stop    func(ctx context.Context, stdio config.Stdio) error
	Suspend func(ctx context.Context, stdio config.Stdio) error
	Restart func(ctx context.Context, stdio config.Stdio) error
}

// Config is what the shell cannot work out for itself. Registry carries the
// resources the builtins read, edit and create, which the shell only ever sees
// through the interfaces in internal/resource - the way internal/resource/tui
// does - so it stays independent of the command package implementing them.
type Config struct {
	Registry *resource.Registry

	Group  *group.Group[multimetro.MetroClient]
	Key    multimetro.Key
	Plugin string

	// Running reports whether the instance was up when the shell opened.
	Running bool

	Dir string
	Env map[string]string

	Exec      ExecFunc
	Lifecycle Lifecycle
}

// shellContext is one open session: the config it was opened with, plus the
// state and machinery that only lives as long as the session does.
type shellContext struct {
	Config

	ctx       context.Context
	out       io.Writer
	errOut    io.Writer
	state     *state
	cache     *historyCache
	completer *completer
	builtins  *builtins
}

// The lifecycle and edit builtins report through the CLI's own stdio, so their
// output lands where the rest of the shell's does.
func (sctx *shellContext) stdio() config.Stdio {
	return config.Stdio{Stdout: sctx.out, Stderr: sctx.errOut}
}

// errf formats a shell error with the "error:" prefix the shell reports with.
func errf(format string, a ...any) error {
	return fmt.Errorf("%s %s", errorStyle.Render("error:"), fmt.Sprintf(format, a...))
}

// printRestartHint says that an edit only lands on the next boot.
func printRestartHint(out io.Writer) {
	fmt.Fprintln(out, hintStyle.Render("  Run '"+builtinSigil+"restart' for changes to take effect."))
}

// wrapErr gives an error the shell's error styling, passing nil through.
func wrapErr(err error) error {
	if err == nil {
		return nil
	}
	return errf("%v", err)
}

// resolve looks a resource type up in the registry the shell was given.
func (sctx *shellContext) resolve(name string) (*resource.ResourceDescriptor, error) {
	if sctx.Registry == nil {
		return nil, errf("no resources available")
	}
	desc, ok := sctx.Registry.Resolve(name)
	if !ok {
		return nil, errf("unknown resource: %s", name)
	}
	return desc, nil
}

// instance re-reads this shell's instance. The shell is long-lived and start,
// stop and edit all mutate the instance underneath it, so this always goes to
// the API rather than holding on to a copy.
func (sctx *shellContext) instance() (resource.Resource, error) {
	desc, err := sctx.resolve(instanceResource)
	if err != nil {
		return nil, err
	}
	if desc.Get == nil {
		return nil, errf("instances cannot be read here")
	}

	resources, err := desc.Get.Get(sctx.ctx, []string{sctx.Key.String()})
	if err != nil {
		return nil, wrapErr(err)
	}
	if len(resources) == 0 {
		return nil, errf("instance not found")
	}
	return resources[0], nil
}

// applyEdit patches this shell's instance. The patch is the field paths and
// unparsed values the user typed: the resource's own field templates decide
// what they parse into, the way `unikraft instance edit` does.
func (sctx *shellContext) applyEdit(spec patch.PatchSpec) error {
	desc, err := sctx.resolve(instanceResource)
	if err != nil {
		return err
	}
	editable, ok := desc.Get.(resource.EditableResource)
	if !ok {
		return errf("instances cannot be edited here")
	}

	res, err := sctx.instance()
	if err != nil {
		return err
	}
	fields, err := res.Fields(sctx.ctx)
	if err != nil {
		return wrapErr(err)
	}
	patched, err := patch.PatchedFields(fields, spec)
	if err != nil {
		return wrapErr(err)
	}
	if len(patched) == 0 {
		return nil
	}
	return wrapErr(editable.Edit(sctx.ctx, res.Key().String(), patched))
}

// execRemote sends a command line to the instance from the shell's current
// working directory and environment.
func (sctx *shellContext) execRemote(ctx context.Context, out io.Writer, in io.Reader, line string) error {
	return sctx.Exec(ctx, out, in, sctx.state.Dir, sctx.state.Env, line)
}

// handleBuiltin runs a sigil-carrying line, reporting whether it asked the
// shell to quit. A line that opens with the sigil and doesn't name a builtin is
// a mistake, not a command to forward to the instance.
func (sctx *shellContext) handleBuiltin(line string) (exit bool) {
	fields, ok := parseBuiltinLine(line)
	if !ok {
		fmt.Fprintf(sctx.errOut, "%s builtins run here rather than on the instance, so they can't be redirected, piped or chained - %s must be the whole line\n",
			errorStyle.Render("error:"), valueStyle.Render(strings.TrimSpace(line)))
		return false
	}

	switch fields[0] {
	case "exit":
		return true

	case "clear":
		fmt.Fprint(sctx.out, ansi.CursorHomePosition+ansi.EraseEntireScreen)
		return false

	case "cd":
		target := "/"
		if len(fields) > 1 {
			target = strings.Trim(fields[1], `"'`)
		}
		newDir := resolveDir(sctx.state.Dir, target)
		var buf strings.Builder
		// The probe runs from the candidate directory, so a directory that
		// isn't there fails the injected `cd` and never echoes.
		_ = sctx.Exec(sctx.ctx, &buf, nil, newDir, nil, "echo OK")
		if strings.TrimSpace(buf.String()) != "OK" {
			fmt.Fprintf(sctx.out, "cd: %s: No such file or directory\n", target)
			return false
		}
		sctx.state.Dir = newDir
		return false

	case "export":
		for _, assign := range fields[1:] {
			if k, v, ok := parseAssignment(assign); ok {
				sctx.state.Env[k] = v
			}
		}
		return false
	}

	if !sctx.builtins.HasCommand(fields[0]) {
		fmt.Fprintf(sctx.errOut, "%s %s: not a builtin, and %s runs nothing on the instance - drop the %s to send it there\n",
			errorStyle.Render("error:"), fields[0],
			valueStyle.Render(builtinSigil+fields[0]),
			valueStyle.Render(builtinSigil))
		return false
	}

	kctx, err := sctx.builtins.Parse(fields)
	if err != nil {
		fmt.Fprintf(sctx.errOut, "%s %v\n", errorStyle.Render("error:"), err)
		return false
	}

	if err := kctx.Run(sctx); err != nil {
		fmt.Fprintf(sctx.errOut, "%v\n", err)
	}

	return false
}

func fetchRemoteAutocompleteCommands(ctx context.Context, exec ExecFunc, c *completer) {
	log.G(ctx).Debug().Msg("shell: fetching remote autocomplete commands")
	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var buf strings.Builder
	err := exec(fetchCtx, &buf, nil, "", nil, "sh -c 'IFS=:; for d in $PATH; do ls -1 \"$d\" 2>/dev/null; done | sort -u'")
	if err != nil {
		log.G(ctx).Debug().Err(err).Msg("shell: remote autocomplete exec failed")
		return
	}

	var cmds []string
	seen := make(map[string]bool)
	for line := range strings.SplitSeq(buf.String(), "\n") {
		cmd := strings.TrimSpace(line)
		if cmd != "" && !strings.ContainsAny(cmd, " \t/") && !seen[cmd] {
			cmds = append(cmds, cmd)
			seen[cmd] = true
		}
	}

	log.G(ctx).Debug().Int("commands", len(cmds)).Msg("shell: remote autocomplete filtered commands")

	if len(cmds) > 0 {
		c.SetRemoteCommands(cmds)
	}
}

func (sctx *shellContext) startBackgroundSync() {
	if sctx.state.Synced || !sctx.state.Running {
		return
	}
	sctx.state.Synced = true
	go sctx.cache.SyncFromRemote(sctx.ctx, sctx.Group, sctx.Key, sctx.Plugin)
	go fetchRemoteAutocompleteCommands(sctx.ctx, sctx.Exec, sctx.completer)
}

// Run opens an interactive shell on the instance described by cfg and returns
// when the user leaves it.
func Run(ctx context.Context, cfg Config, stdio config.Stdio) error {
	ih := interrupt.FromContext(ctx)

	b, err := newBuiltins(&rootCmd{}, stdio.Stdout, stdio.Stderr)
	if err != nil {
		return fmt.Errorf("failed to initialize shell parser: %w", err)
	}

	pump := newStdinPump(stdio.Stdin)
	defer pump.Close()

	sctx := &shellContext{
		Config:    cfg,
		ctx:       ctx,
		out:       stdio.Stdout,
		errOut:    stdio.Stderr,
		state:     newState(cfg.Dir, cfg.Env, cfg.Running),
		cache:     &historyCache{builtins: b},
		completer: newCompleter(b.Completion()),
		builtins:  b,
	}

	fmt.Fprintln(stdio.Stdout, titleStyle.Render("▀▀▀ Unikraft Sandbox Shell"))
	fmt.Fprintf(stdio.Stdout, "  %s %s %s\n", labelStyle.Render("■"), labelStyle.Render("Target:"), valueStyle.Render(cfg.Key.String()))
	fmt.Fprintf(stdio.Stdout, "  %s %s %s\n", labelStyle.Render("■"), labelStyle.Render("Plugin:"), valueStyle.Render(cfg.Plugin))
	fmt.Fprintln(stdio.Stdout)
	fmt.Fprintln(stdio.Stdout, noticeStyle.Render("⚠ EXPERIMENTAL: this shell does not support a PTY, so full-screen apps and job control won't work."))
	fmt.Fprintln(stdio.Stdout)
	b.Help(stdio.Stdout)
	fmt.Fprintln(stdio.Stdout)

	term := newTerminalWriter(stdio.Stdout)
	defer func() { _ = term.Flush() }()

	rl, err := readline.NewEx(&readline.Config{
		Stdin:             pump,
		Stdout:            term,
		Painter:           &painter{builtins: b},
		AutoComplete:      sctx.completer,
		InterruptPrompt:   "^C",
		EOFPrompt:         "exit",
		HistorySearchFold: true,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize readline: %w", err)
	}
	defer rl.Close()

	sctx.cache.OnSynced = func(entries []historyEntry) {
		for _, entry := range entries {
			_ = rl.SaveHistory(entry.Cmd)
		}
	}
	sctx.startBackgroundSync()

	for {
		setPrompt(rl, cfg.Key.String(), sctx.state.Dir)

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

		// Builtins stay out of the command cache: it mirrors what the instance
		// has run, and is what `history rerun` replays back at the instance.
		if hasBuiltinSigil(line) {
			if sctx.handleBuiltin(line) {
				return nil
			}
			continue
		}

		sctx.cache.Append(line)

		if !sctx.state.Running {
			log.G(ctx).Debug().Str("cmd", line).Msg("shell: skipping command, instance not running")
			fmt.Fprintf(stdio.Stdout, "%s instance is not running. Use '%sstart' to start it.\n",
				errorStyle.Render("error:"), builtinSigil)
			continue
		}

		cmdCtx, cancelCmd := context.WithCancel(ctx)
		var restore func()
		if ih != nil {
			restore = ih.Set(cancelCmd)
		}

		log.G(ctx).Debug().Str("cmd", line).Str("dir", sctx.state.Dir).Msg("shell: executing remote command")
		runErr := sctx.execRemote(cmdCtx, stdio.Stdout, pump.readerFor(cmdCtx), line)
		wasInterrupted := cmdCtx.Err() != nil

		if restore != nil {
			restore()
		}
		cancelCmd()

		switch {
		case runErr == nil:
			log.G(ctx).Debug().Str("cmd", line).Msg("shell: command completed")
		case wasInterrupted:
			log.G(ctx).Debug().Str("cmd", line).Msg("shell: command interrupted")
			fmt.Fprintf(stdio.Stdout, "\n%s\n", errorStyle.Render("Interrupt: ^C"))
		default:
			log.G(ctx).Debug().Err(runErr).Str("cmd", line).Msg("shell: command failed")
			fmt.Fprintln(stdio.Stderr, errorStyle.Render("error: ")+runErr.Error())
		}
	}

	return nil
}
