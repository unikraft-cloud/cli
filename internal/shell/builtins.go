// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

import (
	"fmt"
	"io"
	"maps"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/alecthomas/kong"
	"mvdan.cc/sh/v3/syntax"

	"unikraft.com/cli/internal/tabwriter"
)

// BuiltinSigil marks a line as a builtin. It is the only thing that does:
// a line that opens with it is answered here, and every other line is sent
// to the instance verbatim. That keeps the two namespaces apart without the
// shell having to shadow any of the instance's commands - `mount` is always
// the instance's, `:mount` is always ours.
const BuiltinSigil = ":"

// HasBuiltinSigil reports whether a line invokes a builtin. A bare ":" and a
// ": " opening are the POSIX null command rather than a sigil, and belong to
// the instance like any other command.
func HasBuiltinSigil(line string) bool {
	line = strings.TrimSpace(line)
	return len(line) > 1 && strings.HasPrefix(line, BuiltinSigil) && line[1] != ' '
}

// intrinsics are the builtins the shell driver answers from its own switch
// rather than through the kong grammar, because they change the session's
// state instead of running a command against the instance.
//
// These entries are description only - the driver's switch is what runs them
// - and exist so the parts derived from the grammar know they are there at
// all: the `help` menu, the set the painter colours, and tab completion. The
// split is the order they take in `help`, around the grammar's own commands.
var (
	intrinsicsHead = []BuiltinEntry{
		{Usage: ":cd <dir>", Name: "cd", Desc: "change the current remote directory"},
		{Usage: ":export <KEY=VALUE>", Name: "export", Desc: "set an environment variable for later commands"},
	}
	intrinsicsTail = []BuiltinEntry{
		{Usage: ":clear", Name: "clear", Desc: "clear the screen"},
		{Usage: ":exit", Name: "exit", Desc: "quit the shell"},
	}
)

// BuiltinEntry is one line of the `help` menu.
type BuiltinEntry struct {
	// Usage carries the arguments and any subcommand path
	// ("history rerun <index>").
	Usage string
	// Name is just the word that has to be recognised to colour the line as
	// a builtin.
	Name string
	Desc string
}

// Builtins is the set of commands the shell answers itself, derived from a
// kong grammar the caller supplies: the shell knows how to parse, complete
// and describe them, and nothing about what any of them do.
type Builtins struct {
	parser     *kong.Kong
	names      map[string]bool
	all        map[string]bool
	menu       []BuiltinEntry
	completion []CompletionNode
}

// NewBuiltins derives the builtins from grammar, a kong command struct, and
// the shell's own intrinsics.
func NewBuiltins(grammar any, out, errOut io.Writer) (*Builtins, error) {
	parser, err := kong.New(grammar,
		kong.Name(""),
		kong.NoDefaultHelp(),
		kong.Exit(func(int) {}),
		kong.Writers(out, errOut),
	)
	if err != nil {
		return nil, err
	}

	b := &Builtins{
		parser: parser,
		names:  make(map[string]bool),
		all:    make(map[string]bool),
	}

	b.menu = append(b.menu, intrinsicsHead...)
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
	b.menu = append(b.menu, intrinsicsTail...)

	// all is only ever consulted for the first word of a line, so it holds
	// the top-level names alone - "list" is a subcommand of volumes, not a
	// command the shell answers.
	maps.Copy(b.all, b.names)
	for _, entry := range slices.Concat(intrinsicsHead, intrinsicsTail) {
		b.all[entry.Name] = true
		b.completion = append(b.completion, CompletionNode{Name: entry.Name})
	}

	return b, nil
}

// Parse hands a builtin's words to the grammar. The caller runs the returned
// context, so a line that fails to parse and one that fails to run stay
// tellable apart.
func (b *Builtins) Parse(fields []string) (*kong.Context, error) {
	return b.parser.Parse(fields)
}

// HasCommand reports whether name is one the grammar can parse. The
// intrinsics are not: the driver answers those itself, and handing one to
// kong would only fail.
func (b *Builtins) HasCommand(name string) bool {
	return b != nil && b.names[name]
}

// IsBuiltin reports whether name is answered by the shell at all, the
// grammar's commands and the intrinsics alike.
func (b *Builtins) IsBuiltin(name string) bool {
	return b != nil && b.all[name]
}

// Completion is the builtin completion tree, for NewSandboxCompleter.
func (b *Builtins) Completion() []CompletionNode {
	return b.completion
}

// Menu is the `help` menu's lines, in the order they are shown.
func (b *Builtins) Menu() []BuiltinEntry {
	return b.menu
}

// builtinMenu flattens a command and its subcommands into help lines. A
// command with a visible default subcommand describes itself with that
// subcommand's help, because that is what running the bare name does.
func builtinMenu(node *kong.Node, prefix string) []BuiltinEntry {
	// A top-level builtin carries the sigil, and its subcommands hang off
	// its usage: ":volumes" and then ":volumes create".
	usage := BuiltinSigil + node.Name
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

	entries := []BuiltinEntry{{Usage: usage + builtinArgs(described), Name: node.Name, Desc: desc}}
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

func builtinCompletion(node *kong.Node) CompletionNode {
	out := CompletionNode{Name: node.Name}
	for _, child := range node.Children {
		if child.Type != kong.CommandNode || child.Hidden {
			continue
		}
		out.Children = append(out.Children, builtinCompletion(child))
	}
	return out
}

// Help prints the list of available shell builtins. The names are styled, so
// it goes through the ANSI-aware tabwriter rather than a %-Ns pad, which
// would count escape sequences towards the column width.
func (b *Builtins) Help(out io.Writer) {
	fmt.Fprintln(out, ShellTitleStyle.Render("Builtins:"))
	fmt.Fprintln(out)

	tw := tabwriter.TabWriter(out)
	for _, entry := range b.menu {
		fmt.Fprintf(tw, "  %s\t%s\n", ShellValueStyle.Render(entry.Usage), ShellHintStyle.Render(entry.Desc))
	}
	// Nothing to do if the terminal write fails; every other line here
	// ignores write errors too.
	_ = tw.Flush()

	fmt.Fprintln(out)
	fmt.Fprintln(out, ShellKeyStyle.Render("  ctrl-d quit · ctrl-r history · tab autocomplete · ctrl-c cancel"))
	fmt.Fprintln(out)

	for _, line := range []string{
		"Builtins are the lines that open with '" + BuiltinSigil + "', and they turn green as you type them. Every other",
		"line goes to the instance exactly as written, so the shell never shadows its commands:",
		"",
		"  mount vol /mnt      runs the instance's mount",
		"  " + BuiltinSigil + "mount vol /mnt     attaches a volume to the instance",
		"",
		"A builtin has to be the whole line, because it runs here rather than on the instance -",
		"there is nothing on this side to pipe it into or chain it with:",
		"",
		"  ls && mount vol /mnt    goes to the instance whole; 'mount' is just its own command",
		"  " + BuiltinSigil + "mount vol /mnt && ls   is an error, and nothing runs",
	} {
		if line == "" {
			fmt.Fprintln(out)
			continue
		}
		fmt.Fprintln(out, ShellHintStyle.Render("  "+line))
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, ShellHintStyle.Render("  All command logs are kept in memory unless explicitly cleaned with '"+BuiltinSigil+"history clear' or '"+BuiltinSigil+"history delete'."))
}

// State is what a shell session carries between commands: where they run,
// what they run with, and whether the instance is up to run them at all.
type State struct {
	Dir     string
	Env     map[string]string
	Running bool
	Synced  bool
}

func NewState(initialDir string, initialEnv map[string]string, running bool) *State {
	dir := "/"
	if initialDir != "" {
		dir = initialDir
	}

	env := make(map[string]string)
	maps.Copy(env, initialEnv)

	return &State{
		Dir:     path.Clean(dir),
		Env:     env,
		Running: running,
	}
}

// ParseBuiltinLine splits a builtin line into its words, with the sigil
// stripped from the first. It reports false unless the line is one plain
// command: a builtin runs here rather than on the instance, so there is
// nothing on this side to pipe it into, redirect it to, or chain it with,
// and honouring half of `:mount vol /mnt && ls` would be worse than
// refusing all of it.
func ParseBuiltinLine(line string) ([]string, bool) {
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

	name, ok := strings.CutPrefix(fields[0], BuiltinSigil)
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

// ResolveDir resolves a `cd` target against the session's current directory.
func ResolveDir(cur, target string) string {
	if target == "" || target == "~" {
		return "/"
	}
	if strings.HasPrefix(target, "/") {
		return path.Clean(target)
	}
	return path.Clean(path.Join(cur, target))
}

// ParseAssignment splits a KEY=VALUE word, reporting false when it is not
// one: an empty or invalid name, or no "=" at all.
func ParseAssignment(s string) (key, val string, ok bool) {
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
