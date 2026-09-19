// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugin "unikraft.com/cloud/plugins/sandbox"
	"unikraft.com/cloud/sdk/platform"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/sandbox"
	"unikraft.com/cli/internal/types"
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

// TestSandboxPluginDefault pins that the plugin every sandbox command
// addresses defaults to the plugin's own name, and the image it attaches when
// missing to the plugin's default image, both filled in by the parser.
func TestSandboxPluginDefault(t *testing.T) {
	for _, tt := range []struct {
		name  string
		args  []string
		flags func(*UnikraftCLI) (plugin, image string)
	}{
		{
			name: "shell",
			args: []string{"instance", "shell", "my-inst"},
			flags: func(cli *UnikraftCLI) (string, string) {
				return cli.Instances.Shell.Plugin, cli.Instances.Shell.PluginImage
			},
		},
		{
			name: "exec",
			args: []string{"instance", "exec", "my-inst", "--", "echo", "hi"},
			flags: func(cli *UnikraftCLI) (string, string) {
				return cli.Instances.Exec.Plugin, cli.Instances.Exec.PluginImage
			},
		},
		{
			name: "copy",
			args: []string{"instance", "copy", "./a.txt", "my-inst:/tmp/a.txt"},
			flags: func(cli *UnikraftCLI) (string, string) {
				return cli.Instances.Copy.Plugin, cli.Instances.Copy.PluginImage
			},
		},
		{
			name: "write",
			args: []string{"instance", "write", "my-inst", "./a.txt", "/tmp/a.txt"},
			flags: func(cli *UnikraftCLI) (string, string) {
				return cli.Instances.Write.Plugin, cli.Instances.Write.PluginImage
			},
		},
		{
			name: "read",
			args: []string{"instance", "read", "my-inst", "/tmp/a.txt"},
			flags: func(cli *UnikraftCLI) (string, string) {
				return cli.Instances.Read.Plugin, cli.Instances.Read.PluginImage
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var cli UnikraftCLI
			parser, err := NewParser(&cli)
			require.NoError(t, err)

			_, err = parser.Parse(tt.args)
			require.NoError(t, err)
			name, image := tt.flags(&cli)
			assert.Equal(t, plugin.PluginName, name)
			assert.Equal(t, sandbox.DefaultImage, image)
		})
	}
}

// TestMissingPlugin pins what an instance is told about the plugin it lacks:
// nothing when it has it, otherwise which plugins it does have, if any.
func TestMissingPlugin(t *testing.T) {
	for _, tt := range []struct {
		name    string
		plugins []*InstancePlugin
		want    string
	}{
		{"none", nil, `instance "my-inst" has no plugins loaded`},
		{"present", []*InstancePlugin{{Name: "sandbox"}}, ""},
		{"present-among-others", []*InstancePlugin{{Name: "a"}, {Name: "sandbox"}, {Name: "b"}}, ""},
		{"others", []*InstancePlugin{{Name: "a"}, {Name: "b"}}, `instance "my-inst" has no plugin named "sandbox"; it has: a, b`},
		{"blank-entries-are-nothing", []*InstancePlugin{nil, {Name: ""}}, `instance "my-inst" has no plugins loaded`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := missingPlugin(Instance{Name: "my-inst", Plugins: tt.plugins}, "sandbox")

			if tt.want == "" {
				assert.NoError(t, err)
				return
			}
			assert.EqualError(t, err, tt.want)
		})
	}
}

// TestAttachHint pins the command the user is pointed at, complete with the
// metro the instance lives in.
func TestAttachHint(t *testing.T) {
	instance := Instance{Name: "my-inst", key: multimetro.Key{Metro: "fra", Name: "my-inst"}}

	hint := attachHint(instance, "sandbox", "plugins/sandbox:latest")

	assert.Equal(t, "attach it with `unikraft instance edit fra/my-inst --plugin name=sandbox,image=plugins/sandbox:latest` while the instance is stopped", hint)
}

// TestOfferAttachPluginWithoutTTY pins that with nobody at a terminal the
// question is not asked: the command fails as before, pointing at how to
// attach the plugin by hand, and nothing is touched.
func TestOfferAttachPluginWithoutTTY(t *testing.T) {
	cause := errors.New(`instance "my-inst" has no plugins loaded`)
	instance := Instance{
		Name:  "my-inst",
		State: types.InstanceState(platform.InstanceStateRunning),
		key:   multimetro.Key{Metro: "fra", Name: "my-inst"},
	}

	t.Run("hint", func(t *testing.T) {
		var stderr bytes.Buffer
		// A yes on a pipe is not an answer: it was not given to a question.
		stdio := config.Stdio{Stdin: strings.NewReader("y\n"), Stderr: &stderr}

		err := offerAttachPlugin(t.Context(), stdio, instance, "sandbox", "plugins/sandbox:latest", cause)

		require.ErrorIs(t, err, cause)
		assert.Equal(t, cause.Error()+"\nhint: attach it with `unikraft instance edit fra/my-inst --plugin name=sandbox,image=plugins/sandbox:latest` while the instance is stopped", err.Error())
		assert.Empty(t, stderr.String(), "no question was asked")
	})

	t.Run("delete-on-stop", func(t *testing.T) {
		doomed := instance
		doomed.Instance.Features = []platform.InstanceFeature{platform.InstanceFeatureDeleteOnStop}
		var stderr bytes.Buffer
		stdio := config.Stdio{Stdin: strings.NewReader("y\n"), Stderr: &stderr}

		err := offerAttachPlugin(t.Context(), stdio, doomed, "sandbox", "plugins/sandbox:latest", cause)

		require.ErrorIs(t, err, cause)
		assert.Contains(t, err.Error(), "deleted when it stops")
		assert.Contains(t, err.Error(), "--plugin name=sandbox,image=plugins/sandbox:latest")
		assert.Empty(t, stderr.String(), "no question was asked")
	})
}
