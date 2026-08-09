// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"unikraft.com/cli/internal/cmd"
)

func TestBuildExecCommand(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		env  map[string]string
		cmd  []string
		raw  bool
		want string
	}{
		{
			name: "simple command",
			cmd:  []string{"echo", "hello"},
			want: "echo hello",
		},
		{
			name: "command with spaces in arg",
			cmd:  []string{"echo", "hello world"},
			want: "echo 'hello world'",
		},
		{
			name: "command with empty arg",
			cmd:  []string{"echo", "", "end"},
			want: "echo '' end",
		},
		{
			name: "command with special characters",
			cmd:  []string{"echo", "a*b", "c?d"},
			want: "echo 'a*b' 'c?d'",
		},
		{
			name: "command with shell metacharacters",
			cmd:  []string{"echo", "a|b", "c;d"},
			want: "echo 'a|b' 'c;d'",
		},
		{
			name: "command with quotes in arg",
			cmd:  []string{"echo", `it's`},
			want: `echo "it's"`,
		},
		{
			name: "with dir",
			dir:  "/var/lib/app",
			cmd:  []string{"ls"},
			want: "cd /var/lib/app && ls",
		},
		{
			name: "with dir containing quotes",
			dir:  `/path/with's`,
			cmd:  []string{"ls"},
			want: `cd "/path/with's" && ls`,
		},
		{
			name: "with env",
			env:  map[string]string{"DEBUG": "true"},
			cmd:  []string{"./start.sh"},
			want: "env DEBUG=true ./start.sh",
		},
		{
			name: "with multiple env vars",
			env:  map[string]string{"A": "1"},
			cmd:  []string{"./start.sh"},
			want: "env A=1 ./start.sh",
		},
		{
			name: "with env containing quotes",
			env:  map[string]string{"MSG": "it's alive"},
			cmd:  []string{"echo"},
			want: `env MSG="it's alive" echo`,
		},
		{
			name: "with dir and env",
			dir:  "/app",
			env:  map[string]string{"ENV": "prod"},
			cmd:  []string{"./run"},
			want: "cd /app && env ENV=prod ./run",
		},
		{
			name: "raw mode preserves quoting",
			raw:  true,
			cmd:  []string{"echo", "hello world", "a*b"},
			want: "echo hello world a*b",
		},
		{
			name: "raw mode with dir",
			raw:  true,
			dir:  "/tmp",
			cmd:  []string{"ls -la"},
			want: "cd /tmp && ls -la",
		},
		{
			name: "arg with variable is not expanded remotely",
			cmd:  []string{"echo", "$HOME"},
			want: "echo '$HOME'",
		},
		{
			name: "arg with command substitution stays quoted",
			cmd:  []string{"echo", "$(id -u)"},
			want: "echo '$(id -u)'",
		},
		{
			name: "arg with backticks stays quoted",
			cmd:  []string{"echo", "`id -u`"},
			want: "echo '`id -u`'",
		},
		{
			name: "arg with tilde is not expanded remotely",
			cmd:  []string{"ls", "~root"},
			want: "ls '~root'",
		},
		{
			name: "env value with variable is not expanded remotely",
			env:  map[string]string{"P": "$PATH"},
			cmd:  []string{"env"},
			want: "env P='$PATH' env",
		},
		{
			name: "empty command",
			cmd:  []string{},
			want: "",
		},
		{
			name: "single arg",
			cmd:  []string{"pwd"},
			want: "pwd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cmd.BuildExecCommand(tt.dir, tt.env, tt.cmd, tt.raw)
			assert.Equal(t, tt.want, got)
		})
	}
}
