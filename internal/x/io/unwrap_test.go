// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package io_test

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"

	xio "unikraft.com/cli/internal/x/io"
)

// fakeFdWriter mimics an *os.File-like writer that exposes Fd(). It writes to
// an in-memory buffer so tests don't touch real file descriptors.
type fakeFdWriter struct {
	bytes.Buffer
	fd uintptr
}

func (w *fakeFdWriter) Fd() uintptr { return w.fd }

// fakeFdReader mimics an *os.File-like reader that exposes Fd(). It reads from
// an in-memory buffer so tests don't touch real file descriptors.
type fakeFdReader struct {
	bytes.Buffer
	fd uintptr
}

func (r *fakeFdReader) Fd() uintptr { return r.fd }

func TestUnwrap(t *testing.T) {
	t.Parallel()

	plain := &bytes.Buffer{}
	fdw := &fakeFdWriter{fd: 42}

	tests := []struct {
		name string
		in   io.Writer
		want io.Writer
	}{
		{
			name: "plain writer is returned unchanged",
			in:   plain,
			want: plain,
		},
		{
			name: "fd writer is returned unchanged",
			in:   fdw,
			want: fdw,
		},
		{
			name: "colorprofile.Writer is peeled to its Forward",
			in:   &colorprofile.Writer{Forward: fdw},
			want: fdw,
		},
		{
			name: "nested colorprofile.Writer is peeled fully",
			in: &colorprofile.Writer{
				Forward: &colorprofile.Writer{Forward: fdw},
			},
			want: fdw,
		},
		{
			name: "colorprofile.Writer wrapping a non-fd writer still peels",
			in:   &colorprofile.Writer{Forward: plain},
			want: plain,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := xio.Unwrap(tt.in)
			if got != tt.want {
				t.Errorf("Unwrap() = %T(%p), want %T(%p)", got, got, tt.want, tt.want)
			}
		})
	}
}

// TestIsTTY_SeesThroughWrappers is the regression test for the bug where
// colorprofile.Writer hid Fd() from TTY detection. The actual TTY status of
// the wrapped fd depends on how the test is run, but the invariant we care
// about is: IsTTY must produce the same result regardless of how many known
// wrappers are layered on top.
func TestIsTTY_SeesThroughWrappers(t *testing.T) {
	t.Parallel()

	// Use os.Stdin (or any real *os.File) so term.IsTerminal has a real fd to
	// inspect. The result varies by environment, but consistency across
	// wrappers is what we're asserting.
	base := os.Stdin
	want := xio.IsTTY(base)

	wrappers := []struct {
		name string
		w    io.Writer
	}{
		{"colorprofile.Writer", &colorprofile.Writer{Forward: base}},
		{"double-wrapped colorprofile.Writer", &colorprofile.Writer{
			Forward: &colorprofile.Writer{Forward: base},
		}},
	}

	for _, w := range wrappers {
		t.Run(w.name, func(t *testing.T) {
			t.Parallel()
			if got := xio.IsTTY(w.w); got != want {
				t.Errorf("IsTTY(%s) = %v, want %v (matching unwrapped base)", w.name, got, want)
			}
		})
	}
}

func TestIsTTY_NonFdWriter(t *testing.T) {
	t.Parallel()

	if xio.IsTTY(&bytes.Buffer{}) {
		t.Error("IsTTY(*bytes.Buffer) = true, want false")
	}
	if xio.IsTTY(&colorprofile.Writer{Forward: &bytes.Buffer{}}) {
		t.Error("IsTTY(colorprofile wrapping *bytes.Buffer) = true, want false")
	}
}

func TestTermWidth_HonorsCOLUMNS(t *testing.T) {
	// Not t.Parallel() because we mutate the COLUMNS env var.
	t.Setenv("COLUMNS", "123")

	tests := []struct {
		name string
		w    io.Writer
	}{
		{"plain writer", &bytes.Buffer{}},
		// Use a clearly invalid fd so the width query reliably falls through
		// to the COLUMNS env var instead of accidentally consulting a real
		// file descriptor (e.g. stdin) that may happen to be a TTY.
		{"fd writer", &fakeFdWriter{fd: ^uintptr(0)}},
		{"colorprofile-wrapped fd writer", &colorprofile.Writer{Forward: &fakeFdWriter{fd: ^uintptr(0)}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := xio.TermWidth(tt.w); got != 123 {
				t.Errorf("TermWidth(%s) = %d, want 123 (from COLUMNS)", tt.name, got)
			}
		})
	}
}

func TestIsTTYReader_NonFdReader(t *testing.T) {
	t.Parallel()

	if xio.IsTTYReader(&bytes.Buffer{}) {
		t.Error("IsTTYReader(*bytes.Buffer) = true, want false")
	}
	if xio.IsTTYReader(strings.NewReader("")) {
		t.Error("IsTTYReader(*strings.Reader) = true, want false")
	}
}

// TestIsTTYReader_MatchesWriterOnSameFd pins that the reader and writer forms
// agree for one underlying file descriptor: the actual TTY status varies by
// how the test is run, so agreement is the invariant worth asserting.
func TestIsTTYReader_MatchesWriterOnSameFd(t *testing.T) {
	t.Parallel()

	if got, want := xio.IsTTYReader(os.Stdin), xio.IsTTY(os.Stdin); got != want {
		t.Errorf("IsTTYReader(os.Stdin) = %v, IsTTY(os.Stdin) = %v; want equal", got, want)
	}
}

func TestIsTTYReader_NotATerminalFd(t *testing.T) {
	t.Parallel()

	if xio.IsTTYReader(&fakeFdReader{fd: ^uintptr(0)}) {
		t.Error("IsTTYReader(invalid fd) = true, want false")
	}
}
