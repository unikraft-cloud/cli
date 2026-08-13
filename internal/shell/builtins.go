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

// builtinSigil is the only thing that marks a line as a builtin: a line opening
// with it is answered here, every other line goes to the instance verbatim, and
// so the shell never shadows the instance's own commands.
const builtinSigil = ":"

// hasBuiltinSigil reports whether a line invokes a builtin. A bare ":" and a
// ": " opening are the POSIX null command, and belong to the instance.
func hasBuiltinSigil(line string) bool {
	line = strings.TrimSpace(line)
	return len(line) > 1 && strings.HasPrefix(line, builtinSigil) && line[1] != ' '
}

// intrinsics are the builtins the driver's own switch runs, because they change
// session state rather than run something against the instance. These entries
// are description only, so that the parts derived from the kong grammar - the
// help menu, the painter, tab completion - know they exist. The split is their
// order in `help`, around the grammar's own commands.
var (
	intrinsicsHead = []builtinEntry{
		{Usage: ":cd <dir>", Name: "cd", Desc: "change the current remote directory"},
		{Usage: ":export <KEY=VALUE>", Name: "export", Desc: "set an environment variable for later commands"},
	}
	intrinsicsTail = []builtinEntry{
		{Usage: ":clear", Name: "clear", Desc: "clear the screen"},
		{Usage: ":exit", Name: "exit", Desc: "quit the shell"},
	}
)

// builtinEntry is one line of the `help` menu. Usage carries the arguments and
// any subcommand path ("history rerun <index>"); Name is just the word the
// painter has to recognise.
type builtinEntry struct {
	Usage string
	Name  string
	Desc  string
}

// builtins is the set of commands the shell answers itself, derived from a kong
// grammar the caller supplies: the shell knows how to parse, complete and
// describe them, and nothing about what any of them do.
type builtins struct {
	parser     *kong.Kong
	names      map[string]bool
	all        map[string]bool
	menu       []builtinEntry
	completion []completionNode
}

// newBuiltins derives the builtins from grammar, a kong command struct, and the
// shell's own intrinsics.
func newBuiltins(grammar any, out, errOut io.Writer) (*builtins, error) {
	parser, err := kong.New(grammar,
		kong.Name(""),
		kong.NoDefaultHelp(),
		kong.Exit(func(int) {}),
		kong.Writers(out, errOut),
	)
	if err != nil {
		return nil, err
	}

	b := &builtins{
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

	// all is only ever consulted for the first word of a line, so it holds the
	// top-level names alone - "list" is a subcommand of volumes, not a command
	// the shell answers.
	maps.Copy(b.all, b.names)
	for _, entry := range slices.Concat(intrinsicsHead, intrinsicsTail) {
		b.all[entry.Name] = true
		b.completion = append(b.completion, completionNode{Name: entry.Name})
	}

	return b, nil
}

// Parse hands a builtin's words to the grammar. The caller runs the returned
// context, so a line that fails to parse and one that fails to run stay
// tellable apart.
func (b *builtins) Parse(fields []string) (*kong.Context, error) {
	return b.parser.Parse(fields)
}

// HasCommand reports whether name is one the grammar can parse. The intrinsics
// are not: the driver answers those itself.
func (b *builtins) HasCommand(name string) bool {
	return b != nil && b.names[name]
}

// IsBuiltin reports whether name is answered by the shell at all, the grammar's
// commands and the intrinsics alike.
func (b *builtins) IsBuiltin(name string) bool {
	return b != nil && b.all[name]
}

// Completion is the builtin completion tree, for newCompleter.
func (b *builtins) Completion() []completionNode {
	return b.completion
}

// Menu is the `help` menu's lines, in the order they are shown.
func (b *builtins) Menu() []builtinEntry {
	return b.menu
}

// builtinMenu flattens a command and its subcommands into help lines. A command
// with a visible default subcommand describes itself with that subcommand's
// help, because that is what running the bare name does.
func builtinMenu(node *kong.Node, prefix string) []builtinEntry {
	// A top-level builtin carries the sigil, and its subcommands hang off its
	// usage: ":volumes" and then ":volumes create".
	usage := builtinSigil + node.Name
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

	entries := []builtinEntry{{Usage: usage + builtinArgs(described), Name: node.Name, Desc: desc}}
	for _, child := range node.Children {
		if child.Type != kong.CommandNode || child.Hidden {
			continue
		}
		entries = append(entries, builtinMenu(child, usage)...)
	}
	return entries
}

// lowerFirst puts a kong help string into the menu's sentence style: the
// grammar's help is capitalised because it also feeds the top-level CLI help.
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

func builtinCompletion(node *kong.Node) completionNode {
	out := completionNode{Name: node.Name}
	for _, child := range node.Children {
		if child.Type != kong.CommandNode || child.Hidden {
			continue
		}
		out.Children = append(out.Children, builtinCompletion(child))
	}
	return out
}

// Help prints the list of available shell builtins. The names are styled, so it
// goes through the ANSI-aware tabwriter rather than a %-Ns pad, which would
// count escape sequences towards the column width.
func (b *builtins) Help(out io.Writer) {
	fmt.Fprintln(out, titleStyle.Render("Builtins:"))
	fmt.Fprintln(out)

	tw := tabwriter.TabWriter(out)
	for _, entry := range b.menu {
		fmt.Fprintf(tw, "  %s\t%s\n", valueStyle.Render(entry.Usage), hintStyle.Render(entry.Desc))
	}
	// Nothing to do if the terminal write fails; every other line here ignores
	// write errors too.
	_ = tw.Flush()

	fmt.Fprintln(out)
	fmt.Fprintln(out, keyStyle.Render("  ctrl-d quit · ctrl-r history · tab autocomplete · ctrl-c cancel"))
	fmt.Fprintln(out)

	for _, line := range []string{
		"Builtins are the lines that open with '" + builtinSigil + "', and they turn green as you type them. Every other",
		"line goes to the instance exactly as written, so the shell never shadows its commands:",
		"",
		"  mount vol /mnt      runs the instance's mount",
		"  " + builtinSigil + "mount vol /mnt     attaches a volume to the instance",
		"",
		"A builtin has to be the whole line, because it runs here rather than on the instance -",
		"there is nothing on this side to pipe it into or chain it with:",
		"",
		"  ls && mount vol /mnt    goes to the instance whole; 'mount' is just its own command",
		"  " + builtinSigil + "mount vol /mnt && ls   is an error, and nothing runs",
	} {
		if line == "" {
			fmt.Fprintln(out)
			continue
		}
		fmt.Fprintln(out, hintStyle.Render("  "+line))
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, hintStyle.Render("  All command logs are kept in memory unless explicitly cleaned with '"+builtinSigil+"history clear' or '"+builtinSigil+"history delete'."))
}

// state is what a shell session carries between commands: where they run, what
// they run with, and whether the instance is up to run them at all.
type state struct {
	Dir     string
	Env     map[string]string
	Running bool
	Synced  bool
}

func newState(initialDir string, initialEnv map[string]string, running bool) *state {
	dir := "/"
	if initialDir != "" {
		dir = initialDir
	}

	env := make(map[string]string)
	maps.Copy(env, initialEnv)

	return &state{
		Dir:     path.Clean(dir),
		Env:     env,
		Running: running,
	}
}

// parseBuiltinLine splits a builtin line into its words, with the sigil
// stripped from the first. It reports false unless the line is one plain
// command: a builtin runs here rather than on the instance, so honouring half
// of `:mount vol /mnt && ls` would be worse than refusing all of it.
func parseBuiltinLine(line string) ([]string, bool) {
	f, err := parseShell(line)
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

	name, ok := strings.CutPrefix(fields[0], builtinSigil)
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

// resolveDir resolves a `cd` target against the session's current directory.
func resolveDir(cur, target string) string {
	if target == "" || target == "~" {
		return "/"
	}
	if strings.HasPrefix(target, "/") {
		return path.Clean(target)
	}
	return path.Clean(path.Join(cur, target))
}

// parseAssignment splits a KEY=VALUE word, reporting false when it is not one:
// an empty or invalid name, or no "=" at all.
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
