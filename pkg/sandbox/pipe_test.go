// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package sandbox

import (
	"errors"
	"fmt"
	"io"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingWriter fails every write the same way.
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestDeadPipeSwallowsAGoneReader(t *testing.T) {
	for _, gone := range []error{syscall.EPIPE, io.ErrClosedPipe, fmt.Errorf("write: %w", syscall.EPIPE)} {
		t.Run(gone.Error(), func(t *testing.T) {
			closed := 0
			w := &deadPipe{out: failingWriter{err: gone}, closed: func() { closed++ }}

			n, err := w.Write([]byte("abc"))
			require.NoError(t, err, "a reader that left is not the command failing")
			assert.Equal(t, 3, n)

			n, err = w.Write([]byte("more"))
			require.NoError(t, err)
			assert.Equal(t, 4, n, "what nobody reads is taken all the same")
			assert.Equal(t, 1, closed, "the instance is told once")
		})
	}
}

func TestDeadPipeReportsOtherWriteErrors(t *testing.T) {
	full := errors.New("disk full")
	closed := 0
	w := &deadPipe{out: failingWriter{err: full}, closed: func() { closed++ }}

	_, err := w.Write([]byte("abc"))
	require.ErrorIs(t, err, full, "a writer that failed is the command's failure to report")
	assert.Zero(t, closed, "and not a reader that left")

	_, err = w.Write([]byte("again"))
	require.ErrorIs(t, err, full, "the pipe is not dead: the next write is tried too")
}
