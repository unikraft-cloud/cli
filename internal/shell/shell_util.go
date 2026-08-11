// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/chzyer/readline"
	"mvdan.cc/sh/v3/syntax"

	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cloud/sdk/platform/group"

	"unikraft.com/x/log"
)

func SetShellPrompt(rl *readline.Instance, instanceName, dir string) {
	prompt := shellPromptStyle.Render(instanceName) + " " + ShellDirStyle.Render(dir) + " ❯ "
	rl.SetPrompt(prompt)
}

// CompletionNode is one entry in the builtin completion tree. The shell
// package doesn't know which builtins exist - the caller derives them from
// the command grammar and passes them in - so this is the shape they arrive
// in, rather than readline's.
type CompletionNode struct {
	Name     string
	Children []CompletionNode
}

// fallbackRemoteCommands stand in for the instance's own commands until the
// real list arrives from SetRemoteCommands, which needs a round trip.
var fallbackRemoteCommands = []string{"ls", "cat", "echo", "grep", "mkdir", "rm"}

type SandboxCompleter struct {
	mu       sync.Mutex
	builtins []readline.PrefixCompleterInterface
	remote   []readline.PrefixCompleterInterface

	// The two namespaces don't overlap, so neither do their completions: a
	// bare word can only be the instance's, and a word behind the sigil can
	// only be a builtin.
	tree        readline.AutoCompleter
	builtinTree readline.AutoCompleter
}

func NewSandboxCompleter(builtins []CompletionNode) *SandboxCompleter {
	c := &SandboxCompleter{builtins: completionItems(builtins)}

	c.remote = make([]readline.PrefixCompleterInterface, 0, len(fallbackRemoteCommands))
	for _, cmd := range fallbackRemoteCommands {
		c.remote = append(c.remote, readline.PcItem(cmd))
	}

	c.rebuild()
	return c
}

func completionItems(nodes []CompletionNode) []readline.PrefixCompleterInterface {
	items := make([]readline.PrefixCompleterInterface, 0, len(nodes))
	for _, node := range nodes {
		items = append(items, readline.PcItem(node.Name, completionItems(node.Children)...))
	}
	return items
}

func (c *SandboxCompleter) rebuild() {
	c.tree = readline.NewPrefixCompleter(c.remote...)
	c.builtinTree = readline.NewPrefixCompleter(c.builtins...)
}

func (c *SandboxCompleter) SetRemoteCommands(cmds []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.remote = make([]readline.PrefixCompleterInterface, 0, len(cmds))
	for _, cmd := range cmds {
		c.remote = append(c.remote, readline.PcItem(cmd))
	}
	c.rebuild()
}

func (c *SandboxCompleter) Do(line []rune, pos int) ([][]rune, int) {
	if len(line) == 0 {
		return nil, 0
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Dropping the sigil before delegating keeps the builtin completer free
	// of sigil-prefixed duplicates: readline reports the offset as a length
	// of the word already typed, not an index into the line, so shifting the
	// whole line by one leaves the result unchanged.
	if strings.HasPrefix(string(line), BuiltinSigil) {
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

// ShellPainter syntax-highlights the line being edited. Readline calls Paint
// on every redraw, including redraws that don't change the text (cursor
// movement, history search), so the last result is memoised to avoid
// re-parsing the line each time.
type ShellPainter struct {
	// Builtins names the commands this shell answers itself, so they can be
	// told apart from the instance's commands as they're typed - the point
	// at which knowing still lets you change your mind.
	Builtins *Builtins

	mu       sync.Mutex
	lastLine string
	lastPass []rune
}

func (p *ShellPainter) Paint(line []rune, pos int) []rune {
	s := string(line)

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.lastPass != nil && p.lastLine == s {
		return p.lastPass
	}

	p.lastLine = s
	p.lastPass = []rune(highlightShellLine(s, p.Builtins))
	return p.lastPass
}

// isBuiltinWord reports whether the command word spanning [start,end) will
// be answered here rather than by the instance. Only the first word of a
// line can be: the sigil is what the shell dispatches on, and it dispatches
// on the whole line, so the ":mount" in `ls && :mount x` is the instance's
// problem and isn't coloured as ours.
func isBuiltinWord(line string, builtins *Builtins, start, end uint) bool {
	if strings.TrimSpace(line[:start]) != "" {
		return false
	}
	name, ok := strings.CutPrefix(line[start:end], BuiltinSigil)
	return ok && builtins.IsBuiltin(name)
}

// highlightParsers hands out bash parsers for highlightShellLine. Building
// one per call is wasted work on a path that runs on every painter cache
// miss and every history line printed; a parser is reusable but not safe to
// share concurrently, hence the pool.
var highlightParsers = sync.Pool{
	New: func() any {
		return syntax.NewParser(syntax.Variant(syntax.LangBash), syntax.KeepComments(true))
	},
}

func highlightShellLine(line string, builtins *Builtins) string {
	p := highlightParsers.Get().(*syntax.Parser)
	defer highlightParsers.Put(p)

	f, err := p.Parse(strings.NewReader(line), "")
	if err != nil {
		return line
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
					if isBuiltinWord(line, builtins, cStart, cEnd) {
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
			word := line[start:end]
			if strings.HasPrefix(word, "-") {
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
	var currentStyleID uint8
	var currentChunk []byte

	flush := func() {
		if len(currentChunk) > 0 {
			strChunk := string(currentChunk)
			switch currentStyleID {
			case styleCmd:
				sb.WriteString(shellHighlightCmdStyle.Render(strChunk))
			case styleBuiltin:
				sb.WriteString(shellHighlightBuiltinStyle.Render(strChunk))
			case styleStr:
				sb.WriteString(shellHighlightStrStyle.Render(strChunk))
			case styleVar:
				sb.WriteString(shellHighlightVarStyle.Render(strChunk))
			case styleOpt:
				sb.WriteString(shellHighlightOptStyle.Render(strChunk))
			case styleComment:
				sb.WriteString(shellHighlightCommentStyle.Render(strChunk))
			case styleOp:
				sb.WriteString(shellHighlightOpStyle.Render(strChunk))
			default:
				sb.Write(currentChunk)
			}
			currentChunk = nil
		}
	}

	for i := 0; i < len(line); i++ {
		if styles[i] != currentStyleID {
			flush()
			currentStyleID = styles[i]
		}
		currentChunk = append(currentChunk, line[i])
	}
	flush()

	return sb.String()
}

type HistoryEntry struct {
	Cmd  string
	UUID string
}

type HistoryCache struct {
	// Builtins is the set ShellPainter highlights against, so a listed
	// entry is coloured the same way it was when it was typed.
	Builtins *Builtins

	mu       sync.Mutex
	entries  []HistoryEntry
	ready    bool
	err      error
	OnSynced func(entries []HistoryEntry)
}

func (h *HistoryCache) Append(cmd string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries = append(h.entries, HistoryEntry{Cmd: cmd})
}

func (h *HistoryCache) Print(out io.Writer) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.ready {
		fmt.Fprintln(out, ShellHintStyle.Render("History is currently syncing from the remote sandbox... Showing session history only:"))
	} else if h.err != nil {
		fmt.Fprintf(out, "%s %v\n", ShellErrorStyle.Render("Failed to sync remote history:"), h.err)
	}

	for i, entry := range h.entries {
		fmt.Fprintf(out, "%4d  %s\n", i+1, highlightShellLine(entry.Cmd, h.Builtins))
	}
}

func (h *HistoryCache) SyncFromRemote(ctx context.Context, g *group.Group[multimetro.MetroClient], key multimetro.Key, plugin string) {
	log.G(ctx).Debug().Str("instance", key.String()).Str("plugin", plugin).Msg("history: starting remote sync")
	_, err := group.CollectMetro(ctx, g, key.Metro, func(ctx context.Context, c multimetro.MetroClient) (struct{}, error) {
		instance := multimetro.SandboxInstance(key)
		callOpts := c.SandboxOpts(plugin)

		resp, reqErr := c.Sandbox.ListCommands(ctx, instance, callOpts...)
		if reqErr != nil {
			// A 404 means there's no plugin of that name answering on the
			// instance. Syncing history runs in the background at startup, so
			// saying so here would just dump an error over the prompt before
			// the user has typed anything - the first command they run
			// reports it properly (see pluginError in internal/cmd).
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
		remoteEntries := make([]HistoryEntry, 0, len(commands))

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
					remoteEntries = append(remoteEntries, HistoryEntry{Cmd: cleaned, UUID: cmdUUID})
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
		h.mu.Lock()
		h.err = err
		h.ready = true
		h.mu.Unlock()
		log.G(ctx).Debug().Err(err).Msg("history: remote sync error")
		fmt.Fprintf(os.Stderr, "%s failed to sync remote history: %v\n", ShellErrorStyle.Render("shell:"), err)
	}
}

func (h *HistoryCache) Get(n int) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	idx := n - 1
	if idx >= 0 && idx < len(h.entries) {
		return h.entries[idx].Cmd, true
	}
	return "", false
}

func (h *HistoryCache) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.entries = h.entries[:0]
}

func (h *HistoryCache) Delete(n int) (string, string, bool) {
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

func cleanRemoteHistory(cmd string) string {
	if strings.HasPrefix(cmd, "sh -c 'IFS=:") {
		return ""
	}

	cmd = injectedWrapperRx.ReplaceAllString(cmd, "")

	return strings.TrimSpace(cmd)
}

type CmdReader struct {
	S   *StdinPump
	Ctx context.Context
}

func (r *CmdReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	chunk, err := r.S.ReadContext(r.Ctx)
	if err != nil {
		return 0, err
	}

	n := copy(p, chunk)
	if n < len(chunk) {
		r.S.mu.Lock()
		r.S.buf = append(chunk[n:], r.S.buf...)
		r.S.mu.Unlock()
	}
	return n, nil
}
