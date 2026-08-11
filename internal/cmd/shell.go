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
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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

// shellIntrinsics are the builtins handleBuiltin answers from its own
// switch rather than through the kong grammar, because they change this
// shell's state instead of running a command against the instance.
//
// These entries are description only - the switch is what runs them - and
// exist so the parts derived from the grammar know they are there at all:
// the `help` menu, the set the painter colours, and tab completion. The
// split is the order they take in `help`, around the grammar's own
// commands.
var (
	shellIntrinsicsHead = []builtinEntry{
		{usage: ":cd <dir>", name: "cd", desc: "change the current remote directory"},
		{usage: ":export <KEY=VALUE>", name: "export", desc: "set an environment variable for later commands"},
	}
	shellIntrinsicsTail = []builtinEntry{
		{usage: ":clear", name: "clear", desc: "clear the screen"},
		{usage: ":exit", name: "exit", desc: "quit the shell"},
	}
)

// builtinEntry is one line of the `help` menu. usage carries the arguments
// and any subcommand path ("history rerun <index>"); name is just the word
// that has to be recognised to colour the line as a builtin.
type builtinEntry struct {
	usage string
	name  string
	desc  string
}

type shellBuiltins struct {
	parser     *kong.Kong
	names      map[string]bool
	all        map[string]bool
	menu       []builtinEntry
	completion []shell.CompletionNode
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

	b := &shellBuiltins{
		parser: parser,
		names:  make(map[string]bool),
		all:    make(map[string]bool),
	}

	b.menu = append(b.menu, shellIntrinsicsHead...)
	for _, child := range parser.Model.Children {
		if child.Type != kong.CommandNode || child.Hidden {
			continue
		}
		b.names[child.Name] = true
		for _, alias := range child.Aliases {
			b.names[alias] = true
		}
		b.menu = append(b.menu, builtinMenu(child, "")...)
		b.completion = append(b.completion, builtinCompletion(child))
	}
	b.menu = append(b.menu, shellIntrinsicsTail...)

	// all is only ever consulted for the first word of a line, so it holds
	// the top-level names alone - "list" is a subcommand of volumes, not a
	// command the shell answers.
	maps.Copy(b.all, b.names)
	for _, entry := range slices.Concat(shellIntrinsicsHead, shellIntrinsicsTail) {
		b.all[entry.name] = true
		b.completion = append(b.completion, shell.CompletionNode{Name: entry.name})
	}

	return b, nil
}

// builtinMenu flattens a command and its subcommands into help lines. A
// command with a visible default subcommand describes itself with that
// subcommand's help, because that is what running the bare name does.
func builtinMenu(node *kong.Node, prefix string) []builtinEntry {
	// A top-level builtin carries the sigil, and its subcommands hang off
	// its usage: ":volumes" and then ":volumes create".
	usage := shell.BuiltinSigil + node.Name
	if prefix != "" {
		usage = prefix + " " + node.Name
	}

	described := node
	if node.DefaultCmd != nil && !node.DefaultCmd.Hidden {
		described = node.DefaultCmd
	}
	desc := lowerFirst(described.Help)
	if described != node {
		desc = fmt.Sprintf("%s (alias for %s %s)", desc, usage, described.Name)
	}

	entries := []builtinEntry{{usage: usage + builtinArgs(described), name: node.Name, desc: desc}}
	for _, child := range node.Children {
		if child.Type != kong.CommandNode || child.Hidden {
			continue
		}
		entries = append(entries, builtinMenu(child, usage)...)
	}
	return entries
}

// lowerFirst puts a kong help string into the menu's sentence style. The
// grammar's help is capitalised because it also feeds the top-level CLI
// help, where that is the convention.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToLower(r)) + s[size:]
}

func builtinArgs(node *kong.Node) string {
	var sb strings.Builder
	for _, positional := range node.Positional {
		if positional.Required {
			fmt.Fprintf(&sb, " <%s>", positional.Name)
		} else {
			fmt.Fprintf(&sb, " [<%s>]", positional.Name)
		}
	}
	return sb.String()
}

func builtinCompletion(node *kong.Node) shell.CompletionNode {
	out := shell.CompletionNode{Name: node.Name}
	for _, child := range node.Children {
		if child.Type != kong.CommandNode || child.Hidden {
			continue
		}
		out.Children = append(out.Children, builtinCompletion(child))
	}
	return out
}

// handleBuiltin runs a sigil-carrying line, reporting whether it asked the
// shell to quit. The caller has already established the line is a builtin
// invocation, so there is no falling back to the instance from here: a line
// that opens with the sigil and doesn't name a builtin is a mistake, not a
// command to forward.
func (s *shellState) handleBuiltin(sctx *ShellContext, line string) (exit bool) {
	fields, ok := builtinFields(line)
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
			return false
		}
		s.dir = newDir
		return false

	case "export":
		for _, assign := range fields[1:] {
			if k, v, ok := parseAssignment(assign); ok {
				s.env[k] = v
			}
		}
		return false
	}

	if !sctx.Builtins.names[fields[0]] {
		fmt.Fprintf(sctx.ErrOut, "%s %s: not a builtin, and %s runs nothing on the instance - drop the %s to send it there\n",
			shell.ShellErrorStyle.Render("error:"), fields[0],
			shell.ShellValueStyle.Render(shell.BuiltinSigil+fields[0]),
			shell.ShellValueStyle.Render(shell.BuiltinSigil))
		return false
	}

	kctx, err := sctx.Builtins.parser.Parse(fields)
	if err != nil {
		fmt.Fprintf(sctx.ErrOut, "%s %v\n", shell.ShellErrorStyle.Render("error:"), err)
		return false
	}

	if err := kctx.Run(sctx); err != nil {
		fmt.Fprintf(sctx.ErrOut, "%v\n", err)
	}

	return false
}

// builtinFields splits a builtin line into its words, with the sigil
// stripped from the first. It reports false unless the line is one plain
// command: a builtin runs here rather than on the instance, so there is
// nothing on this side to pipe it into, redirect it to, or chain it with,
// and honouring half of `:mount vol /mnt && ls` would be worse than
// refusing all of it.
func builtinFields(line string) ([]string, bool) {
	parser := syntax.NewParser()
	f, err := parser.Parse(strings.NewReader(line), "")
	if err != nil || len(f.Stmts) != 1 {
		return nil, false
	}

	stmt := f.Stmts[0]
	if stmt.Background || stmt.Coprocess || stmt.Negated || len(stmt.Redirs) > 0 {
		return nil, false
	}

	// Anything richer than a plain command - a pipeline, an && or ||, a
	// subshell, a loop - parses to something other than a CallExpr.
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) == 0 || len(call.Assigns) > 0 {
		return nil, false
	}

	fields := make([]string, 0, len(call.Args))
	for _, arg := range call.Args {
		field, ok := literalWord(arg)
		if !ok {
			return nil, false
		}
		fields = append(fields, field)
	}

	name, ok := strings.CutPrefix(fields[0], shell.BuiltinSigil)
	if !ok || name == "" {
		return nil, false
	}
	fields[0] = name

	return fields, true
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
	ih := interrupt.FromContext(ctx)
	isRunning := requireRunningInstance(instance) == nil

	builtins, err := newShellBuiltins(stdio.Stdout, stdio.Stderr)
	if err != nil {
		return fmt.Errorf("failed to initialize shell parser: %w", err)
	}

	cache := &shell.HistoryCache{Builtins: builtins.all}
	state := newShellState(initialDir, initialEnv, isRunning)
	pump := shell.NewStdinPump(stdio.Stdin)
	defer pump.Close()

	completer := shell.NewSandboxCompleter(builtins.completion)
	painter := &shell.ShellPainter{Builtins: builtins.all}

	fmt.Fprintln(stdio.Stdout, shell.ShellTitleStyle.Render("▀▀▀ Unikraft Sandbox Shell"))
	fmt.Fprintf(stdio.Stdout, "  %s %s %s\n", shell.ShellLabelStyle.Render("■"), shell.ShellLabelStyle.Render("Target:"), shell.ShellValueStyle.Render(key.String()))
	fmt.Fprintf(stdio.Stdout, "  %s %s %s\n", shell.ShellLabelStyle.Render("■"), shell.ShellLabelStyle.Render("Plugin:"), shell.ShellValueStyle.Render(plugin))
	fmt.Fprintln(stdio.Stdout)
	fmt.Fprintln(stdio.Stdout, shell.ShellNoticeStyle.Render("⚠ EXPERIMENTAL: this shell does not support a PTY, so full-screen apps and job control won't work."))
	fmt.Fprintln(stdio.Stdout)
	builtinHelp(stdio.Stdout, builtins)
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

		_ = rl.SaveHistory(line)

		// Builtins stay out of the command cache: it mirrors what the
		// instance has run, and it is what `history rerun` replays back at
		// the instance.
		if shell.HasBuiltinSigil(line) {
			if state.handleBuiltin(sctx, line) {
				return nil
			}
			continue
		}

		cache.Append(line)

		if !state.running {
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
