// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Usage sums the token counts of the model requests of a run.
type Usage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

// Turn is one entry of a transcript: a system or user message, assistant
// text, a tool call or a tool result.
type Turn struct {
	Role    string `json:"role"`
	Name    string `json:"name,omitempty"`
	Content string `json:"content"`
}

// Row is the record of one run of a prompt, one line of results.jsonl.
type Row struct {
	PromptID   string             `json:"prompt_id"`
	Rep        int                `json:"rep"`
	Prompt     string             `json:"prompt"`
	Tags       []string           `json:"tags"`
	Model      string             `json:"model"`
	StopReason string             `json:"stop_reason"`
	Status     string             `json:"status"`
	Grade      map[string]float64 `json:"grade"`
	LatencyS   float64            `json:"latency_s"`
	ToolCalls  int                `json:"tool_calls"`
	Usage      Usage              `json:"usage"`
	Meta       map[string]any     `json:"meta,omitempty"`
}

// Recorder appends rows to results.jsonl in a directory and writes each
// transcript under traces/, numbering the runs of each prompt.
type Recorder struct {
	dir  string
	mu   sync.Mutex
	reps map[string]int
}

func NewRecorder(dir string) *Recorder {
	return &Recorder{dir: dir, reps: map[string]int{}}
}

// Record stores the row and its transcript, and gives the path of the
// transcript.
func (r *Recorder) Record(row Row, trace []Turn) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row.Rep = r.reps[row.PromptID]
	r.reps[row.PromptID]++

	line, err := json.Marshal(row)
	if err != nil {
		return "", err
	}
	results, err := os.OpenFile(filepath.Join(r.dir, "results.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	if _, err := results.Write(append(line, '\n')); err != nil {
		return "", err
	}
	if err := results.Close(); err != nil {
		return "", err
	}

	data, err := json.MarshalIndent(trace, "", "  ")
	if err != nil {
		return "", err
	}
	tracesDir := filepath.Join(r.dir, "traces")
	if err := os.MkdirAll(tracesDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(tracesDir, fmt.Sprintf("%s_rep%d.json", fileName(row.PromptID), row.Rep))
	return path, os.WriteFile(path, data, 0o644)
}

func fileName(id string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == ' ' {
			return '_'
		}
		return r
	}, id)
}
