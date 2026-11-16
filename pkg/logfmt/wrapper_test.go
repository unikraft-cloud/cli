// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package logfmt

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWrappedWriterWrapsPlainLine(t *testing.T) {
	buffer := &bytes.Buffer{}
	writer := newWrappedWriter(buffer, 6)

	_, err := writer.Write([]byte("hello world\n"))
	require.NoError(t, err)

	require.Equal(t, "hello\nworld\n", buffer.String())
}

func TestWrappedWriterWrapsLongWord(t *testing.T) {
	buffer := &bytes.Buffer{}
	writer := newWrappedWriter(buffer, 4)

	_, err := writer.Write([]byte("abcdefgh\n"))
	require.NoError(t, err)

	require.Equal(t, "abcd\nefgh\n", buffer.String())
}

func TestWrappedWriterWrapsLogLine(t *testing.T) {
	buffer := &bytes.Buffer{}
	writer := newWrappedWriter(buffer, 10)

	_, err := writer.Write([]byte(LogLevelSymbol + " hello world from unikraft\n"))
	require.NoError(t, err)

	expected := "" +
		LogLevelSymbol + " hello\n" +
		LogLevelSymbol + " world\n" +
		LogLevelSymbol + " from\n" +
		LogLevelSymbol + " unikraft\n"
	require.Equal(t, expected, buffer.String())
}

func TestWrappedWriterRepeatsLogPrefix(t *testing.T) {
	buffer := &bytes.Buffer{}
	writer := newWrappedWriter(buffer, 8)

	prefix := "\x1b[31m" + LogLevelSymbol + "\x1b[0m "
	_, err := writer.Write([]byte(prefix + "hello world\n"))
	require.NoError(t, err)

	expected := prefix + "hello\n" + prefix + "world\n"
	require.Equal(t, expected, buffer.String())
}
