// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xstdio "unikraft.com/x/stdio"
)

func testStdio() xstdio.Stdio {
	return xstdio.Stdio{
		Stdin:  &bytes.Buffer{},
		Stdout: io.Discard,
		Stderr: io.Discard,
	}
}

func TestAPICmd_ResolveBody_PositionalKeyValue(t *testing.T) {
	c := &APICmd{Args: []string{"name=val", "count:=42"}}
	body, err := c.resolveBody(context.Background(), testStdio())
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, "val", got["name"])
	assert.InDelta(t, float64(42), got["count"], 0.01)
}

func TestAPICmd_ResolveBody_TopLevelArray(t *testing.T) {
	c := &APICmd{Args: []string{
		"[0][type]=platform",
		"[0][name]=desktop",
		"[1][type]=platform",
		"[1][name]=web",
	}}
	body, err := c.resolveBody(context.Background(), testStdio())
	require.NoError(t, err)

	var got []map[string]any
	require.NoError(t, json.Unmarshal(body, &got))
	require.Len(t, got, 2)
	assert.Equal(t, "desktop", got[0]["name"])
	assert.Equal(t, "web", got[1]["name"])
}

func TestAPICmd_ResolveBody_TopLevelArrayRejectsMixedShallowKey(t *testing.T) {
	// Regression guard: a plain key=value argument mixed in with top-level
	// array syntax must be rejected end-to-end through resolveBody, not
	// silently collapse the whole body down to the stray key's value.
	c := &APICmd{Args: []string{
		"[0][type]=platform",
		"name=value",
	}}
	_, err := c.resolveBody(context.Background(), testStdio())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot perform key-based access on array")
}

func TestAPICmd_ResolveBody_AtFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"from":"file"}`), 0o644))

	c := &APICmd{Args: []string{"@" + path}}
	body, err := c.resolveBody(context.Background(), testStdio())
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, "file", got["from"])
}

func TestAPICmd_ResolveBody_AtFileWithInlineArgs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"from":"file"}`), 0o644))

	c := &APICmd{Args: []string{"@" + path, "name=val"}}
	body, err := c.resolveBody(context.Background(), testStdio())
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, "file", got["from"])
	assert.Equal(t, "val", got["name"])
}

func TestAPICmd_ResolveBody_AtStdin(t *testing.T) {
	stdio := testStdio()
	stdio.Stdin = strings.NewReader(`{"from":"stdin"}`)

	c := &APICmd{Args: []string{"@-"}}
	body, err := c.resolveBody(context.Background(), stdio)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, "stdin", got["from"])
}

func TestAPICmd_ResolveBody_LegacyDataRawFallback(t *testing.T) {
	c := &APICmd{Data: "hello world"}
	body, err := c.resolveBody(context.Background(), testStdio())
	require.NoError(t, err)
	assert.Equal(t, []byte("hello world"), body)
}

func TestAPICmd_ResolveBody_DataWithArgsStillRequiresStructuredSyntax(t *testing.T) {
	c := &APICmd{Data: "foo:=invalid", Args: []string{"name=val"}}
	_, err := c.resolveBody(context.Background(), testStdio())
	require.Error(t, err)
}

func TestAPICmd_ResolveBody_NoSources(t *testing.T) {
	c := &APICmd{}
	body, err := c.resolveBody(context.Background(), testStdio())
	require.NoError(t, err)
	assert.Nil(t, body)
}
