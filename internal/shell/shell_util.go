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
	prompt := ShellPromptStyle.Render(instanceName) + " " + ShellDirStyle.Render(dir) + " ❯ "
	rl.SetPrompt(prompt)
}

type SandboxCompleter struct {
	mu       sync.Mutex
	builtins []readline.PrefixCompleterInterface
	remote   []readline.PrefixCompleterInterface
	tree     readline.AutoCompleter
}

func NewSandboxCompleter() *SandboxCompleter {
	c := &SandboxCompleter{
		builtins: []readline.PrefixCompleterInterface{
			readline.PcItem("history", readline.PcItem("list"), readline.PcItem("rerun"), readline.PcItem("clear"), readline.PcItem("delete")),
			readline.PcItem("exit"),
			readline.PcItem("clear"),
			readline.PcItem("cd"),
			readline.PcItem("volumes", readline.PcItem("mounted"), readline.PcItem("list"), readline.PcItem("create")),
			readline.PcItem("mount"),
			readline.PcItem("unmount"),
			readline.PcItem("edit", readline.PcItem("env"), readline.PcItem("args"), readline.PcItem("memory"), readline.PcItem("vcpus"), readline.PcItem("tags")),
			readline.PcItem("restart"),
			readline.PcItem("start"),
			readline.PcItem("stop"),
			readline.PcItem("suspend"),
			readline.PcItem("get"),
			readline.PcItem("help"),
			readline.PcItem("ls"),
			readline.PcItem("cat"),
			readline.PcItem("echo"),
			readline.PcItem("grep"),
			readline.PcItem("mkdir"),
			readline.PcItem("rm"),
		},
	}
	c.rebuild()
	return c
}

func (c *SandboxCompleter) rebuild() {
	items := append([]readline.PrefixCompleterInterface{}, c.builtins...)
	items = append(items, c.remote...)
	c.tree = readline.NewPrefixCompleter(items...)
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
	p.lastPass = []rune(highlightShellLine(s))
	return p.lastPass
}

func highlightShellLine(line string) string {
	p := syntax.NewParser(syntax.Variant(syntax.LangBash), syntax.KeepComments(true))
	f, err := p.Parse(strings.NewReader(line), "")
	if err != nil {
		return line
	}

	const (
		styleNone uint8 = iota
		styleCmd
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
					for i := cStart; i < cEnd; i++ {
						styles[i] = styleCmd
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
		fmt.Fprintf(out, "%4d  %s\n", i+1, highlightShellLine(entry.Cmd))
	}
}

func (h *HistoryCache) SyncFromRemote(ctx context.Context, g *group.Group[multimetro.MetroClient], key multimetro.Key, plugin string) {
	log.G(ctx).Debug().Str("instance", key.String()).Str("plugin", plugin).Msg("history: starting remote sync")
	_, err := group.CollectMetro(ctx, g, key.Metro, func(ctx context.Context, c multimetro.MetroClient) (struct{}, error) {
		resp, reqErr := c.Sandbox.ListInstanceCommands(ctx, key.Ref().UUID, plugin)
		if reqErr != nil {
			if strings.Contains(reqErr.Error(), "mime: no media type") {
				log.G(ctx).Debug().Err(reqErr).Msg("history: no media type on list, treating as empty")
				h.mu.Lock()
				h.ready = true
				h.mu.Unlock()
				return struct{}{}, nil
			}
			log.G(ctx).Debug().Err(reqErr).Msg("history: list instance commands failed")
			return struct{}{}, reqErr
		}

		log.G(ctx).Debug().Int("count", len(resp.Data.Commands)).Msg("history: fetched remote command list")
		remoteEntries := make([]HistoryEntry, 0, len(resp.Data.Commands))

		for i, cmdUUID := range resp.Data.Commands {
			if ctx.Err() != nil {
				log.G(ctx).Debug().Int("processed", i).Msg("history: context cancelled during inspect")
				return struct{}{}, ctx.Err()
			}

			inspectResp, inspectErr := c.Sandbox.InspectInstanceCommand(ctx, key.Ref().UUID, plugin, cmdUUID)
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

func (h *HistoryCache) Clear() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	n := len(h.entries)
	h.entries = h.entries[:0]
	return n
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

type ReadlineReader struct {
	S *StdinPump
}

func (r *ReadlineReader) Read(p []byte) (int, error) {
	return r.S.Read(p)
}

func (r *ReadlineReader) Fd() uintptr {
	if f, ok := r.S.r.(interface{ Fd() uintptr }); ok {
		return f.Fd()
	}
	return ^uintptr(0)
}

func (r *ReadlineReader) Close() error {
	return nil
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
