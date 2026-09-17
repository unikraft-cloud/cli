// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integ "unikraft.com/cli/internal/integration"
)

// TestHelp runs --help tests for all resource types.
func TestHelp(t *testing.T) {
	unikraftPath := integ.BuildUnikraft(t)
	t.Parallel()

	run := func(name string, fn func(*testing.T, string)) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fn(t, unikraftPath)
		})
	}

	run("general", generalHelpTests)
	run("auth", authHelpTests)
	run("instances", instancesHelpTests)
	run("volumes", volumesHelpTests)
	run("services", servicesHelpTests)
	run("certificates", certificatesHelpTests)
	run("images", imagesHelpTests)
	run("resources", resourceHelpTests)
	run("build", buildHelpTests)
	run("run", runHelpTests)
	run("config", configHelpTests)
	run("api", apiHelpTests)
}

// TestVersion checks that `unikraft version` output contains expected fields.
// Uses regexp instead of golden files because version output contains
// environment-specific values (Go version, OS/arch, build time).
func TestVersion(t *testing.T) {
	t.Parallel()
	unikraftPath := integ.BuildUnikraft(t)
	env := integ.NewTestEnv(t, unikraftPath)
	out := env.Run(t, []string{"unikraft", "version"})

	assert.Regexp(t, `version\s*:\s+\S+`, out)
	assert.Regexp(t, `commit\s*:\s+\S+`, out)
	assert.Regexp(t, `platform\s*:\s+\S+`, out)
	assert.Regexp(t, `go version\s*:\s+go\d+\.\d+`, out)
	assert.Regexp(t, `docs\s*:\s+https://`, out)
	assert.Regexp(t, `issues\s*:\s+https://`, out)
}

// generalHelpTests checks that top-level help and error output stays stable.
// Deterministic and offline.
func generalHelpTests(t *testing.T, unikraftPath string) {
	r := integ.NewTestEnv(t, unikraftPath)
	integ.Gild(t, cli(r),
		[]string{"unikraft"},
		[]string{"unikraft", "--help"},
		[]string{"unikraft", "invalid"},
		[]string{"unikraft", "--help", "--bad-flag"},
		[]string{"unikraft", "--help", "bad-arg"},
		[]string{"unikraft", "--log-level=fatal", "invalid"},
	)
}

func authHelpTests(t *testing.T, unikraftPath string) {
	r := integ.NewTestEnv(t, unikraftPath)
	integ.Gild(t, cli(r),
		[]string{"unikraft", "login", "--help"},
		[]string{"unikraft", "logout", "--help"},
		[]string{"unikraft", "profile", "--help"},
		[]string{"unikraft", "profile", "get", "--help"},
		[]string{"unikraft", "profile", "list", "--help"},
		[]string{"unikraft", "profile", "use", "--help"},
		[]string{"unikraft", "metro", "--help"},
		[]string{"unikraft", "metro", "get", "--help"},
		[]string{"unikraft", "metro", "list", "--help"},
		[]string{"unikraft", "quotas", "--help"},
	)
}

func instancesHelpTests(t *testing.T, unikraftPath string) {
	r := integ.NewTestEnv(t, unikraftPath)
	integ.Gild(t, cli(r),
		[]string{"unikraft", "instance", "--help"},
		[]string{"unikraft", "instance", "get", "--help"},
		[]string{"unikraft", "instance", "list", "--help"},
		[]string{"unikraft", "instance", "wait", "--help"},
		[]string{"unikraft", "instance", "create", "--help"},
		[]string{"unikraft", "instance", "new", "--help"},
		[]string{"unikraft", "instance", "run", "--help"},
		[]string{"unikraft", "instance", "edit", "--help"},
		[]string{"unikraft", "instance", "delete", "--help"},
		[]string{"unikraft", "instance", "template", "--help"},
		[]string{"unikraft", "instance", "template", "get", "--help"},
		[]string{"unikraft", "instance", "template", "list", "--help"},
		[]string{"unikraft", "instance", "template", "create", "--help"},
		[]string{"unikraft", "instance", "template", "edit", "--help"},
		[]string{"unikraft", "instance", "template", "delete", "--help"},
		[]string{"unikraft", "instance", "checkpoint", "--help"},
		[]string{"unikraft", "instance", "checkpoint", "get", "--help"},
		[]string{"unikraft", "instance", "checkpoint", "list", "--help"},
		[]string{"unikraft", "instance", "checkpoint", "wait", "--help"},
		[]string{"unikraft", "instance", "checkpoint", "create", "--help"},
		[]string{"unikraft", "instance", "checkpoint", "edit", "--help"},
		[]string{"unikraft", "instance", "checkpoint", "delete", "--help"},
		[]string{"unikraft", "instance", "checkpoint", "history", "--help"},
		[]string{"unikraft", "instance", "logs", "--help"},
		[]string{"unikraft", "instance", "start", "--help"},
		[]string{"unikraft", "instance", "stop", "--help"},
		[]string{"unikraft", "instance", "suspend", "--help"},
		[]string{"unikraft", "instance", "restart", "--help"},
		[]string{"unikraft", "instance", "tunnel", "--help"},
		[]string{"unikraft", "instance", "history", "--help"},
		[]string{"unikraft", "instance", "shell", "--help"},
		[]string{"unikraft", "instance", "exec", "--help"},
		[]string{"unikraft", "instance", "copy", "--help"},
		[]string{"unikraft", "instance", "write", "--help"},
		[]string{"unikraft", "instance", "read", "--help"},
	)
}

func volumesHelpTests(t *testing.T, unikraftPath string) {
	r := integ.NewTestEnv(t, unikraftPath)
	integ.Gild(t, cli(r),
		[]string{"unikraft", "volume", "--help"},
		[]string{"unikraft", "volume", "get", "--help"},
		[]string{"unikraft", "volume", "list", "--help"},
		[]string{"unikraft", "volume", "wait", "--help"},
		[]string{"unikraft", "volume", "create", "--help"},
		[]string{"unikraft", "volume", "clone", "--help"},
		[]string{"unikraft", "volume", "attach", "--help"},
		[]string{"unikraft", "volume", "detach", "--help"},
		[]string{"unikraft", "volume", "import", "--help"},
		[]string{"unikraft", "volume", "edit", "--help"},
		[]string{"unikraft", "volume", "delete", "--help"},
		[]string{"unikraft", "volume", "template", "--help"},
		[]string{"unikraft", "volume", "template", "get", "--help"},
		[]string{"unikraft", "volume", "template", "list", "--help"},
		[]string{"unikraft", "volume", "template", "create", "--help"},
		[]string{"unikraft", "volume", "template", "edit", "--help"},
		[]string{"unikraft", "volume", "template", "delete", "--help"},
	)
}

func servicesHelpTests(t *testing.T, unikraftPath string) {
	r := integ.NewTestEnv(t, unikraftPath)
	integ.Gild(t, cli(r),
		[]string{"unikraft", "service", "--help"},
		[]string{"unikraft", "service", "get", "--help"},
		[]string{"unikraft", "service", "list", "--help"},
		[]string{"unikraft", "service", "wait", "--help"},
		[]string{"unikraft", "service", "create", "--help"},
		[]string{"unikraft", "service", "edit", "--help"},
		[]string{"unikraft", "service", "delete", "--help"},
	)
}

func certificatesHelpTests(t *testing.T, unikraftPath string) {
	r := integ.NewTestEnv(t, unikraftPath)
	integ.Gild(t, cli(r),
		[]string{"unikraft", "certificate", "--help"},
		[]string{"unikraft", "certificate", "get", "--help"},
		[]string{"unikraft", "certificate", "list", "--help"},
		[]string{"unikraft", "certificate", "wait", "--help"},
		[]string{"unikraft", "certificate", "create", "--help"},
		[]string{"unikraft", "certificate", "delete", "--help"},
	)
}

func imagesHelpTests(t *testing.T, unikraftPath string) {
	r := integ.NewTestEnv(t, unikraftPath)
	integ.Gild(t, cli(r),
		[]string{"unikraft", "image", "--help"},
		[]string{"unikraft", "image", "build", "--help"},
		[]string{"unikraft", "image", "get", "--help"},
		[]string{"unikraft", "image", "list", "--help"},
		[]string{"unikraft", "image", "copy", "--help"},
	)
}

func resourceHelpTests(t *testing.T, unikraftPath string) {
	r := integ.NewTestEnv(t, unikraftPath)
	integ.Gild(t, cli(r),
		[]string{"unikraft", "resource", "--help"},
		[]string{"unikraft", "resource", "delete", "--help"},
	)
}

func buildHelpTests(t *testing.T, unikraftPath string) {
	r := integ.NewTestEnv(t, unikraftPath)
	integ.Gild(t, cli(r),
		[]string{"unikraft", "build", "--help"},
	)
}

func runHelpTests(t *testing.T, unikraftPath string) {
	r := integ.NewTestEnv(t, unikraftPath)
	integ.Gild(t, cli(r),
		[]string{"unikraft", "run", "--help"},
	)
}

func configHelpTests(t *testing.T, unikraftPath string) {
	r := integ.NewTestEnv(t, unikraftPath)
	integ.Gild(t, cli(r),
		[]string{"unikraft", "config", "--help"},
		[]string{"unikraft", "config", "get", "--help"},
	)
}

func apiHelpTests(t *testing.T, unikraftPath string) {
	r := integ.NewTestEnv(t, unikraftPath)
	integ.Gild(t, cli(r),
		[]string{"unikraft", "api", "--help"},
	)
}

// cli returns a callback that runs a CLI command and formats its output for
// golden comparison. It tolerates command failures and records the exit code.
func cli(env *integ.TestEnv) func(*testing.T, []string) string {
	return func(t *testing.T, args []string) string {
		t.Helper()
		out, err := env.RunRaw(t, args)

		var exitCode int
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			exitCode = exitErr.ExitCode()
		} else if err != nil {
			require.NoError(t, err, "command %q failed: %s", strings.Join(args, " "), out)
		}

		formatted := make([]string, 0, len(args))
		for _, arg := range args {
			formatted = append(formatted, quoteArg(arg))
		}

		var result strings.Builder
		result.WriteString("$ " + strings.Join(formatted, " ") + "\n")
		if normalized := normalizeOutput(out); normalized != "" {
			result.WriteString("\n" + normalized + "\n")
		}
		if exitCode != 0 {
			result.WriteString("\nexit code: " + strconv.Itoa(exitCode) + "\n")
		}
		return result.String()
	}
}

func quoteArg(arg string) string {
	if arg == "" {
		return "''"
	}
	if strings.ContainsAny(arg, " \t\n{}()") {
		return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
	}
	return arg
}

func normalizeOutput(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = ansi.Strip(s)
	s = strings.TrimRightFunc(s, unicode.IsSpace)
	return s
}
