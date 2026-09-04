// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package sandbox

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"time"

	"github.com/docker/go-units"

	plugin "unikraft.com/cloud/plugins/sandbox"
	"unikraft.com/x/log"
)

const (
	logChunkSize    = 1 * units.MiB
	logFetchTimeout = 10 * time.Second
)

// logStream is a command's two output streams, read from the offset each has
// been read to. The plugin keeps the output; a poll asks for what follows what
// this stream has already delivered.
type logStream struct {
	target Target
	uuid   string

	stdout, stderr io.Writer

	stdoutOffset, stderrOffset       uint64
	stdoutAvailable, stderrAvailable uint64
}

func (l *logStream) drain(ctx context.Context) error {
	for {
		n, err := l.drainOnce(ctx)
		if err != nil {
			return err
		}
		if n == 0 || l.stdoutAvailable <= l.stdoutOffset && l.stderrAvailable <= l.stderrOffset {
			return nil
		}
	}
}

func (l *logStream) drainOnce(ctx context.Context) (uint64, error) {
	log.G(ctx).Trace().
		Str("cmd", l.uuid).
		Uint64("stdout_offset", l.stdoutOffset).
		Uint64("stderr_offset", l.stderrOffset).
		Msg("fetching logs")

	limit := uint64(logChunkSize)
	req := plugin.CommandLogsRequest{
		Stdout: plugin.CommandLogsRange{Offset: l.stdoutOffset, Limit: &limit},
		Stderr: plugin.CommandLogsRange{Offset: l.stderrOffset, Limit: &limit},
	}

	fetchCtx, cancel := context.WithTimeout(ctx, logFetchTimeout)
	defer cancel()
	resp, err := l.target.Client.GetCommandLogs(fetchCtx, l.target.Instance, l.uuid, &req, l.target.Opts...)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch logs: %w", err)
	}
	if resp.Data == nil {
		return 0, nil
	}

	log.G(ctx).Trace().
		Str("cmd", l.uuid).
		Uint64("stdout_offset", l.stdoutOffset).
		Uint64("stderr_offset", l.stderrOffset).
		Bool("has_stdout", resp.Data.Stdout != "").
		Bool("has_stderr", resp.Data.Stderr != "").
		Uint64("stdout_available", resp.Data.StdoutAvailable).
		Uint64("stderr_available", resp.Data.StderrAvailable).
		Msg("polled command logs")

	l.stdoutAvailable = resp.Data.StdoutAvailable
	l.stderrAvailable = resp.Data.StderrAvailable

	var written uint64
	outputs := []struct {
		data   string
		offset *uint64
		out    io.Writer
	}{
		{resp.Data.Stdout, &l.stdoutOffset, l.stdout},
		{resp.Data.Stderr, &l.stderrOffset, l.stderr},
	}
	for _, stream := range outputs {
		if stream.data == "" {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(stream.data)
		if err != nil {
			return written, fmt.Errorf("decoding command output: %w", err)
		}
		if stream.out != nil {
			if _, err := stream.out.Write(decoded); err != nil {
				return written, fmt.Errorf("writing command output: %w", err)
			}
		}
		*stream.offset += uint64(len(decoded))
		written += uint64(len(decoded))
	}
	return written, nil
}
