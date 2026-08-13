// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/chzyer/readline"
	"mvdan.cc/sh/v3/syntax"

	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cloud/sdk/platform/group"

	"unikraft.com/x/log"
)

func setPrompt(rl *readline.Instance, instanceName, dir string) {
	rl.SetPrompt(promptStyle.Render(instanceName) + " " + dirStyle.Render(dir) + " ❯ ")
}

// completionNode is one entry in the builtin completion tree. The caller
// derives the builtins from the command grammar, so this is the shape they
// arrive in rather than readline's.
type completionNode struct {
	Name     string
	Children []completionNode
}

// fallbackRemoteCommands stand in for the instance's own commands until the
// real list arrives from SetRemoteCommands, which needs a round trip.
var fallbackRemoteCommands = []string{"ls", "cat", "echo", "grep", "mkdir", "rm"}

type completer struct {
	mu       sync.Mutex
	builtins []readline.PrefixCompleterInterface
	remote   []readline.PrefixCompleterInterface

	// The two namespaces don't overlap, so neither do their completions: a bare
	// word can only be the instance's, and a word behind the sigil can only be
	// a builtin.
	tree        readline.AutoCompleter
	builtinTree readline.AutoCompleter
}

func newCompleter(builtins []completionNode) *completer {
	c := &completer{builtins: completionItems(builtins)}
	c.remote = completionItems(nodesFor(fallbackRemoteCommands))
	c.rebuild()
	return c
}

func nodesFor(names []string) []completionNode {
	nodes := make([]completionNode, 0, len(names))
	for _, name := range names {
		nodes = append(nodes, completionNode{Name: name})
	}
	return nodes
}

func completionItems(nodes []completionNode) []readline.PrefixCompleterInterface {
	items := make([]readline.PrefixCompleterInterface, 0, len(nodes))
	for _, node := range nodes {
		items = append(items, readline.PcItem(node.Name, completionItems(node.Children)...))
	}
	return items
}

func (c *completer) rebuild() {
	c.tree = readline.NewPrefixCompleter(c.remote...)
	c.builtinTree = readline.NewPrefixCompleter(c.builtins...)
}

func (c *completer) SetRemoteCommands(cmds []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.remote = completionItems(nodesFor(cmds))
	c.rebuild()
}

func (c *completer) Do(line []rune, pos int) ([][]rune, int) {
	if len(line) == 0 {
		return nil, 0
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Dropping the sigil before delegating keeps the builtin completer free of
	// sigil-prefixed duplicates: readline reports the offset as a length of the
	// word already typed, not an index into the line, so shifting the whole
	// line by one leaves the result unchanged.
	if strings.HasPrefix(string(line), builtinSigil) {
		// pos is the cursor, which readline can place before the sigil.
		if c.builtinTree == nil || pos < 1 {
			return nil, 0
		}
		return c.builtinTree.Do(line[1:], pos-1)
	}

	if c.tree == nil {
		return nil, 0
	}
	return c.tree.Do(line, pos)
}

// painter syntax-highlights the line being edited. Readline calls Paint on
// every redraw, including redraws that don't change the text, so the last
// result is memoised to avoid re-parsing the line each time.
type painter struct {
	// builtins names the commands this shell answers itself, so they can be
	// told apart from the instance's as they're typed.
	builtins *builtins

	mu       sync.Mutex
	lastLine string
	lastPass []rune
}

func (p *painter) Paint(line []rune, pos int) []rune {
	s := string(line)

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.lastPass != nil && p.lastLine == s {
		return p.lastPass
	}

	p.lastLine = s
	p.lastPass = []rune(highlightLine(s, p.builtins))
	return p.lastPass
}

// isBuiltinWord reports whether the command word spanning [start,end) will be
// answered here rather than by the instance. Only the first word of a line can
// be, because the shell dispatches on the whole line: the ":mount" in
// `ls && :mount x` is the instance's problem.
func isBuiltinWord(line string, b *builtins, start, end uint) bool {
	if strings.TrimSpace(line[:start]) != "" {
		return false
	}
	name, ok := strings.CutPrefix(line[start:end], builtinSigil)
	return ok && b.IsBuiltin(name)
}

// shellParsers hands out bash parsers. Building one per call is wasted work on
// a path that runs on every painter cache miss and every builtin line; a parser
// is reusable but not safe to share concurrently, hence the pool.
var shellParsers = sync.Pool{
	New: func() any {
		return syntax.NewParser(syntax.Variant(syntax.LangBash), syntax.KeepComments(true))
	},
}

func parseShell(line string) (*syntax.File, error) {
	p := shellParsers.Get().(*syntax.Parser)
	defer shellParsers.Put(p)
	return p.Parse(strings.NewReader(line), "")
}

const (
	styleNone uint8 = iota
	styleCmd
	styleBuiltin
	styleStr
	styleVar
	styleOpt
	styleComment
	styleOp
)

// highlightStyles maps a style ID to its rendering. The zero style at styleNone
// renders its input unchanged, so plain runs need no special case.
var highlightStyles = [...]lipgloss.Style{
	styleNone:    {},
	styleCmd:     highlightCmdStyle,
	styleBuiltin: highlightBuiltinStyle,
	styleStr:     highlightStrStyle,
	styleVar:     highlightVarStyle,
	styleOpt:     highlightOptStyle,
	styleComment: highlightCommentStyle,
	styleOp:      highlightOpStyle,
}

func highlightLine(line string, b *builtins) string {
	f, err := parseShell(line)
	if err != nil {
		return line
	}

	styles := make([]uint8, len(line))

	syntax.Walk(f, func(node syntax.Node) bool {
		if node == nil {
			return true
		}

		start := node.Pos().Offset()
		end := node.End().Offset()

		if start >= uint(len(line)) || end > uint(len(line)) {
			return true
		}

		var styleID uint8
		switch n := node.(type) {
		case *syntax.CallExpr:
			if len(n.Args) > 0 {
				cStart := n.Args[0].Pos().Offset()
				cEnd := n.Args[0].End().Offset()
				if cStart < uint(len(line)) && cEnd <= uint(len(line)) {
					cmdStyle := styleCmd
					if isBuiltinWord(line, b, cStart, cEnd) {
						cmdStyle = styleBuiltin
					}
					for i := cStart; i < cEnd; i++ {
						styles[i] = cmdStyle
					}
				}
			}
		case *syntax.SglQuoted, *syntax.DblQuoted:
			styleID = styleStr
		case *syntax.ParamExp:
			styleID = styleVar
		case *syntax.Comment:
			styleID = styleComment
		case *syntax.Word:
			if strings.HasPrefix(line[start:end], "-") {
				styleID = styleOpt
			}
		case *syntax.Redirect, *syntax.BinaryCmd:
			styleID = styleOp
		case *syntax.Assign:
			if n.Name != nil {
				aStart := n.Name.Pos().Offset()
				aEnd := n.Name.End().Offset()
				if aStart < uint(len(line)) && aEnd <= uint(len(line)) {
					for i := aStart; i < aEnd; i++ {
						styles[i] = styleVar
					}
				}
			}
		}

		if styleID != styleNone {
			for i := start; i < end; i++ {
				styles[i] = styleID
			}
		}
		return true
	})

	var sb strings.Builder
	for i := 0; i < len(line); {
		j := i
		for j < len(line) && styles[j] == styles[i] {
			j++
		}
		sb.WriteString(highlightStyles[styles[i]].Render(line[i:j]))
		i = j
	}
	return sb.String()
}

type historyEntry struct {
	Cmd  string
	UUID string
}

type historyCache struct {
	// builtins is the set the painter highlights against, so a listed entry is
	// coloured the same way it was when it was typed.
	builtins *builtins

	mu       sync.Mutex
	entries  []historyEntry
	ready    bool
	err      error
	OnSynced func(entries []historyEntry)
}

func (h *historyCache) Append(cmd string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries = append(h.entries, historyEntry{Cmd: cmd})
}

func (h *historyCache) Print(out io.Writer) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.ready {
		fmt.Fprintln(out, hintStyle.Render("History is currently syncing from the remote sandbox... Showing session history only:"))
	} else if h.err != nil {
		fmt.Fprintf(out, "%s %v\n", errorStyle.Render("Failed to sync remote history:"), h.err)
	}

	for i, entry := range h.entries {
		fmt.Fprintf(out, "%4d  %s\n", i+1, highlightLine(entry.Cmd, h.builtins))
	}
}

func (h *historyCache) SyncFromRemote(ctx context.Context, g *group.Group[multimetro.MetroClient], key multimetro.Key, plugin string) {
	log.G(ctx).Debug().Str("instance", key.String()).Str("plugin", plugin).Msg("history: starting remote sync")
	_, err := group.CollectMetro(ctx, g, key.Metro, func(ctx context.Context, c multimetro.MetroClient) (struct{}, error) {
		instance := multimetro.SandboxInstance(key)
		callOpts := c.SandboxOpts(plugin)

		resp, reqErr := c.Sandbox.ListCommands(ctx, instance, callOpts...)
		if reqErr != nil {
			// A 404 means nothing answers to that plugin name. This runs in the
			// background at startup, so the first command the user runs reports
			// it properly instead (see wrapSandboxErr in internal/cmd).
			if strings.Contains(reqErr.Error(), "mime: no media type") ||
				strings.Contains(reqErr.Error(), "request failed: 404") {
				log.G(ctx).Debug().Err(reqErr).Msg("history: list unavailable, treating as empty")
				h.mu.Lock()
				h.ready = true
				h.mu.Unlock()
				return struct{}{}, nil
			}
			log.G(ctx).Debug().Err(reqErr).Msg("history: list instance commands failed")
			return struct{}{}, reqErr
		}

		var commands []string
		if resp.Data != nil {
			commands = resp.Data.Commands
		}

		log.G(ctx).Debug().Int("count", len(commands)).Msg("history: fetched remote command list")
		remoteEntries := make([]historyEntry, 0, len(commands))

		for i, cmdUUID := range commands {
			if ctx.Err() != nil {
				log.G(ctx).Debug().Int("processed", i).Msg("history: context cancelled during inspect")
				return struct{}{}, ctx.Err()
			}

			inspectResp, inspectErr := c.Sandbox.GetCommandByUuid(ctx, instance, cmdUUID, callOpts...)
			if inspectErr != nil {
				log.G(ctx).Debug().Err(inspectErr).Str("cmd_uuid", cmdUUID).Msg("history: inspect command failed, skipping")
				continue
			}

			if inspectResp.Data != nil && inspectResp.Data.Cmdline != "" {
				cleaned := cleanRemoteHistory(inspectResp.Data.Cmdline)
				if cleaned != "" {
					remoteEntries = append(remoteEntries, historyEntry{Cmd: cleaned, UUID: cmdUUID})
				}
			}
		}

		log.G(ctx).Debug().Int("entries", len(remoteEntries)).Msg("history: remote sync entries collected")

		h.mu.Lock()
		h.entries = append(remoteEntries, h.entries...)
		h.ready = true
		cb := h.OnSynced
		entries := h.entries
		h.mu.Unlock()

		log.G(ctx).Debug().Int("total_entries", len(entries)).Msg("history: remote sync complete")

		if cb != nil {
			cb(entries)
		}

		return struct{}{}, nil
	})
	if err != nil {
		// Not reported here: this runs in the background and would land over
		// the prompt. Print surfaces h.err the next time `:history` is run.
		h.mu.Lock()
		h.err = err
		h.ready = true
		h.mu.Unlock()
		log.G(ctx).Debug().Err(err).Msg("history: remote sync error")
	}
}

func (h *historyCache) Get(n int) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	idx := n - 1
	if idx >= 0 && idx < len(h.entries) {
		return h.entries[idx].Cmd, true
	}
	return "", false
}

func (h *historyCache) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.entries = h.entries[:0]
}

func (h *historyCache) Delete(n int) (string, string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	idx := n - 1
	if idx < 0 || idx >= len(h.entries) {
		return "", "", false
	}

	entry := h.entries[idx]
	h.entries = append(h.entries[:idx], h.entries[idx+1:]...)
	return entry.Cmd, entry.UUID, true
}

var injectedWrapperRx = regexp.MustCompile(`^(?:cd '.*?' && )?(?:env (?:[a-zA-Z0-9_]+='.*?' )*)?`)

// cleanRemoteHistory strips the cd/env prefix buildExecCommand injects, and
// drops the shell's own autocomplete probe.
func cleanRemoteHistory(cmd string) string {
	if strings.HasPrefix(cmd, "sh -c 'IFS=:") {
		return ""
	}
	return strings.TrimSpace(injectedWrapperRx.ReplaceAllString(cmd, ""))
}
