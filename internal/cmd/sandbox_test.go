// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseCopyPath pins how a copy specification is split into an instance
// target and a path. An empty target means the whole specification is a local
// path.
func TestParseCopyPath(t *testing.T) {
	for _, tt := range []struct {
		name   string
		spec   string
		target string
		path   string
	}{
		// Nothing to split: no separator at all.
		{"empty", "", "", ""},
		{"bare-name", "file.txt", "", "file.txt"},
		{"relative-path", "./a.txt", "", "./a.txt"},
		{"absolute-path", "/tmp/x", "", "/tmp/x"},

		// A plain target and the path after its separator.
		{"plain-target", "my-inst:/tmp/x", "my-inst", "/tmp/x"},
		{"relative-remote-path", "my-inst:relative/path", "my-inst", "relative/path"},
		{"single-character-target", "a:/tmp/x", "a", "/tmp/x"},
		{"metro-qualified-target", "fra0/my-inst:/tmp/x", "fra0/my-inst", "/tmp/x"},

		// The separator is the first colon that is not a prefix's own, so
		// later colons stay in the remote path.
		{"colon-in-remote-path", "my-inst:/tmp/a:b", "my-inst", "/tmp/a:b"},

		// A "name:" or "uuid:" prefix owns the colon it ends with.
		{"name-prefixed-target", "name:my-inst:/tmp/x", "name:my-inst", "/tmp/x"},
		{"uuid-prefixed-target", "uuid:abc123:/tmp/x", "uuid:abc123", "/tmp/x"},
		{"metro-and-name-prefixed", "fra0/name:my-inst:/tmp/x", "fra0/name:my-inst", "/tmp/x"},
		{"metro-and-uuid-prefixed", "fra0/uuid:abc:/tmp/x", "fra0/uuid:abc", "/tmp/x"},
		{"prefixed-colon-in-remote-path", "uuid:abc:/p:q", "uuid:abc", "/p:q"},

		// A target with no path keeps the separator, as "scp file host:" does.
		{"target-without-path", "my-inst:", "my-inst", ""},
		{"metro-qualified-without-path", "fra0/my-inst:", "fra0/my-inst", ""},
		{"name-prefixed-without-path", "name:my-inst:", "name:my-inst", ""},

		// A prefix with no second colon carries no path, so the whole
		// specification is a local one.
		{"name-prefix-without-path", "name:my-inst", "", "name:my-inst"},
		{"uuid-prefix-without-path", "uuid:abc123", "", "uuid:abc123"},
		{"bare-name-prefix", "name:", "", "name:"},

		// A specification that opens like a filesystem path is a local file
		// whose name happens to carry a colon.
		{"colon-in-relative-path", "./back:up.tar", "", "./back:up.tar"},
		{"colon-in-parent-path", "../up:x.txt", "", "../up:x.txt"},
		{"colon-in-home-path", "~/back:up.tar", "", "~/back:up.tar"},
		{"colon-in-absolute-path", "/tmp/a:b", "", "/tmp/a:b"},
		{"leading-separator", ":/tmp/x", "", ":/tmp/x"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			target, path := parseCopyPath(tt.spec)
			assert.Equal(t, tt.target, target, "target")
			assert.Equal(t, tt.path, path, "path")
		})
	}
}

func TestBuildExecCommand(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"single", []string{"ls"}, `'ls'`},
		{"multiple", []string{"echo", "hello"}, `'echo' 'hello'`},
		{"spaces", []string{"echo", "two words"}, `'echo' 'two words'`},
		{"glob", []string{"echo", "*"}, `'echo' '*'`},
		{"dollar", []string{"echo", "$HOME"}, `'echo' '$HOME'`},
		{"backtick", []string{"echo", "`id`"}, "'echo' '`id`'"},
		{"semicolon", []string{"echo", "; rm -rf /"}, `'echo' '; rm -rf /'`},
		{"single-quote", []string{"echo", "it's"}, `'echo' 'it'\''s'`},
		{"quote-and-dollar", []string{"echo", "it's $HOME"}, `'echo' 'it'\''s $HOME'`},
		{"backslash", []string{"echo", `back\slash`}, `'echo' 'back\slash'`},
		{"empty", []string{"echo", ""}, `'echo' ''`},
		{"newline", []string{"printf", "a\nb"}, "'printf' 'a\nb'"},
		{"tab", []string{"printf", "a\tb"}, "'printf' 'a\tb'"},
		{"control", []string{"printf", "x\x01y"}, "'printf' 'x\x01y'"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildExecCommand(tt.args)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.NotContains(t, got, "$'")
		})
	}

	t.Run("nul-byte", func(t *testing.T) {
		_, err := buildExecCommand([]string{"echo", "a\x00b"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "NUL byte")
	})
}
