// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package logfmt

import (
	"bytes"
	"io"
	"strings"

	"github.com/charmbracelet/x/ansi"

	xio "unikraft.com/x/io"
)

type wrappedWriter struct {
	out     io.Writer
	width   int
	pending []byte
}

func newWrappedWriter(out io.Writer, width int) io.Writer {
	return &wrappedWriter{out: out, width: width}
}

func newScreenWrappedWriter(out io.Writer) io.Writer {
	if !xio.IsTTY(out) {
		return out
	}
	return newWrappedWriter(out, xio.TermWidth(out))
}

func (w *wrappedWriter) Write(p []byte) (int, error) {
	if w.width < 1 {
		return w.out.Write(p)
	}

	w.pending = append(w.pending, p...)
	for {
		newline := bytes.IndexByte(w.pending, '\n')
		if newline == -1 {
			break
		}
		line := string(w.pending[:newline+1])
		if newline+1 >= len(w.pending) {
			w.pending = w.pending[:0]
		} else {
			w.pending = w.pending[newline+1:]
		}

		if err := w.writeWrappedLine(line); err != nil {
			return 0, err
		}
	}

	return len(p), nil
}

func (w *wrappedWriter) writeWrappedLine(line string) error {
	prefix, content := splitLogPrefix(line)
	wrapWidth := w.width
	if prefix != "" {
		wrapWidth = max(w.width-ansi.StringWidth(prefix), 1)
	}

	wrapped := ansi.Wrap(content, wrapWidth, " ")
	for line := range strings.Lines(wrapped) {
		if prefix != "" {
			line = prefix + line
		}
		if _, err := io.WriteString(w.out, line); err != nil {
			return err
		}
	}

	return nil
}

func splitLogPrefix(line string) (string, string) {
	ss := strings.SplitAfterN(line, " ", 2)
	if len(ss) < 2 {
		return "", line
	}
	if !strings.Contains(ss[0], LogLevelSymbol) {
		// this *shouldn't* happen, the log writer should always be prefixed by a
		// level but just in case, we avoid accidentally repeating the first word
		// of the line as a prefix
		return "", line
	}
	return ss[0], ss[1]
}
