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

// TestInstanceSandboxUnmarshalText pins the --sandbox flag's value grammar: the
// bare on/off words, and the comma-separated key=value form.
func TestInstanceSandboxUnmarshalText(t *testing.T) {
	for _, tt := range []struct {
		name string
		text string
		want InstanceSandbox
		err  string
	}{
		// Empty and bare-word forms.
		{name: "empty", text: ""},
		{name: "on", text: "on", want: InstanceSandbox{Persist: new(true)}},
		{name: "true", text: "true", want: InstanceSandbox{Persist: new(true)}},
		{name: "off", text: "off", want: InstanceSandbox{Persist: new(false)}},
		{name: "false", text: "false", want: InstanceSandbox{Persist: new(false)}},
		{name: "uppercase", text: "OFF", want: InstanceSandbox{Persist: new(false)}},

		// One key at a time.
		{name: "persist", text: "persist=false", want: InstanceSandbox{Persist: new(false)}},
		{name: "path", text: "path=/data", want: InstanceSandbox{Path: "/data"}},
		{name: "volume", text: "volume=my-vol", want: InstanceSandbox{Volume: "my-vol"}},
		{name: "size", text: "size=2GiB", want: InstanceSandbox{Size: 2048}},
		{name: "rom", text: "rom=foo/bar:1", want: InstanceSandbox{Rom: "foo/bar:1"}},
		{name: "plugin", text: "plugin=sbx", want: InstanceSandbox{Plugin: "sbx"}},

		// Several at once, in the form the flag's help advertises.
		{
			name: "several",
			text: "persist=true,path=/data,volume=v,size=1GiB,rom=r:1,plugin=p",
			want: InstanceSandbox{
				Persist: new(true),
				Path:    "/data",
				Volume:  "v",
				Size:    1024,
				Rom:     "r:1",
				Plugin:  "p",
			},
		},
		{name: "spaces around keys", text: "path = /data , size = 1GiB", want: InstanceSandbox{Path: "/data", Size: 1024}},

		// Rejections.
		{name: "unknown key", text: "bogus=1", err: "unknown fields"},
		{name: "bare word that is not on or off", text: "maybe", err: "unknown fields"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var got InstanceSandbox
			err := got.UnmarshalText([]byte(tt.text))
			if tt.err != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestInstanceSandboxSets covers what --sandbox actually expands to. The plugin
// config's persist_path and the volume's mount point must always be the same
// path: the plugin does not create the directory it is told to persist into.
func TestInstanceSandboxSets(t *testing.T) {
	const (
		path = defaultSandboxPath
		rom  = defaultSandboxRom
	)

	for _, tt := range []struct {
		name    string
		text    string
		plugins []InstancePlugin
		volumes []InstanceVolume
		want    []map[string]string
		err     string
	}{
		{
			// The headline case: bare --sandbox on a named instance.
			name: "defaults",
			want: []map[string]string{
				{"plugins": `name=sandbox,rom=` + rom + `,config={"persist_path":"` + path + `"}`},
				{"volumes": ":" + path + ":size=1GiB"},
			},
		},
		{
			name: "persist=false drops the volume and the config",
			text: "persist=false",
			want: []map[string]string{
				{"plugins": "name=sandbox,rom=" + rom},
			},
		},
		{
			name: "off is persist=false",
			text: "off",
			want: []map[string]string{
				{"plugins": "name=sandbox,rom=" + rom},
			},
		},
		{
			// path moves the mount point and persist_path together.
			name: "custom path",
			text: "path=/data",
			want: []map[string]string{
				{"plugins": `name=sandbox,rom=` + rom + `,config={"persist_path":"/data"}`},
				{"volumes": ":/data:size=1GiB"},
			},
		},
		{
			// A volume named without a size already exists, so it is attached
			// rather than created.
			name: "named volume is attached, not created",
			text: "volume=my-vol",
			want: []map[string]string{
				{"plugins": `name=sandbox,rom=` + rom + `,config={"persist_path":"` + path + `"}`},
				{"volumes": "my-vol:" + path},
			},
		},
		{
			// The platform takes either a reference or a description of a new
			// volume, never both.
			name: "named volume with a size",
			text: "volume=my-vol,size=5GiB",
			err:  "names a volume that already exists",
		},
		{
			name: "custom rom and plugin name",
			text: "rom=foo/bar:1,plugin=sbx",
			want: []map[string]string{
				{"plugins": `name=sbx,rom=foo/bar:1,config={"persist_path":"` + path + `"}`},
				{"volumes": ":" + path + ":size=1GiB"},
			},
		},
		{
			// A plugin the user asked for separately is only a conflict when it
			// wants the same name.
			name:    "unrelated plugin",
			plugins: []InstancePlugin{{Name: "other", Rom: "x/y:1"}},
			want: []map[string]string{
				{"plugins": `name=sandbox,rom=` + rom + `,config={"persist_path":"` + path + `"}`},
				{"volumes": ":" + path + ":size=1GiB"},
			},
		},
		{
			name:    "unrelated volume",
			volumes: []InstanceVolume{{At: "/mnt"}},
			want: []map[string]string{
				{"plugins": `name=sandbox,rom=` + rom + `,config={"persist_path":"` + path + `"}`},
				{"volumes": ":" + path + ":size=1GiB"},
			},
		},

		// Conflicts and bad input.
		{
			name:    "plugin name already taken",
			plugins: []InstancePlugin{{Name: "sandbox", Rom: "x/y:1"}},
			err:     `--plugin already loads a plugin named "sandbox"`,
		},
		{
			name:    "plugin name taken under a rename",
			text:    "plugin=sbx",
			plugins: []InstancePlugin{{Name: "sbx", Rom: "x/y:1"}},
			err:     `--plugin already loads a plugin named "sbx"`,
		},
		{
			name:    "volume already mounted at the sandbox path",
			volumes: []InstanceVolume{{At: defaultSandboxPath}},
			err:     "--volume already mounts at",
		},
		{
			name: "persist=false with a path",
			text: "persist=false,path=/data",
			err:  "leaves nothing to store",
		},
		{
			name: "persist=false with a size",
			text: "persist=false,size=2GiB",
			err:  "leaves nothing to store",
		},
		{
			name: "relative path",
			text: "path=data",
			err:  "must be absolute",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var s InstanceSandbox
			require.NoError(t, s.UnmarshalText([]byte(tt.text)))

			got, err := s.sets(tt.plugins, tt.volumes)
			if tt.err != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// The rendered entries are parsed a second time on their way into the create
// request, so the plugin's embedded JSON config has to survive a round trip.
func TestInstanceSandboxRoundTrip(t *testing.T) {
	var s InstanceSandbox
	sets, err := s.sets(nil, nil)
	require.NoError(t, err)

	var plugin InstancePlugin
	require.NoError(t, plugin.UnmarshalText([]byte(sets[0]["plugins"])))
	assert.Equal(t, "sandbox", plugin.Name)
	assert.Equal(t, defaultSandboxRom, plugin.Rom)
	assert.JSONEq(t, `{"persist_path":"`+defaultSandboxPath+`"}`, plugin.Config)

	var volume InstanceVolume
	require.NoError(t, volume.UnmarshalText([]byte(sets[1]["volumes"])))
	assert.Empty(t, volume.Name, "a volume being created is named by the platform")
	assert.Equal(t, defaultSandboxPath, volume.At)
	assert.Equal(t, plugin.Config, `{"persist_path":"`+volume.At+`"}`, "persist_path must be the volume's mount point")
}
