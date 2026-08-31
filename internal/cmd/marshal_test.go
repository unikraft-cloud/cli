// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cloud/sdk/platform"

	"unikraft.com/cli/internal/cmd"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/patch"
	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/cli/internal/types"
)

// TestJSONRoundTrip covers types that accept both a compact text form and a
// full object form from JSON/YAML (the `--load`/`--save` and visual editing
// paths).
//
// Any type implementing UnmarshalText needs its own UnmarshalJSON, because
// encoding/json will only ever feed a TextUnmarshaler a JSON *string* and
// rejects objects/arrays outright. Likewise a type implementing MarshalText
// needs its own MarshalJSON, or encoding/json collapses it to that compact
// string and silently drops every field the text form cannot express.
func TestJSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		object     string
		text       string
		into       func() any
		wantObject any
		wantText   any
		// marshalsToObject asserts the encoded form keeps all fields rather
		// than collapsing to a bare JSON string.
		marshalsToObject bool
	}{
		{
			name:   "InstanceService",
			object: `{"name":"my-group","domains":[{"fqdn":"example.com"}],"soft-limit":5,"hard-limit":10}`,
			text:   `"my-group"`,
			into:   func() any { return &cmd.InstanceService{} },
			wantObject: func() any {
				s := &cmd.InstanceService{
					Domains:   []cmd.Domain{{FQDN: "example.com"}},
					SoftLimit: 5,
					HardLimit: 10,
				}
				s.Name = "my-group"
				return s
			}(),
			wantText: func() any {
				s := &cmd.InstanceService{}
				s.Name = "my-group"
				return s
			}(),
			marshalsToObject: true,
		},
		{
			name:   "InstanceVolume",
			object: `{"name":"my-volume","at":"/data","readonly":true,"size":"1GiB"}`,
			text:   `"my-volume:/data:ro:size=1GiB"`,
			into:   func() any { return &cmd.InstanceVolume{} },
			wantObject: func() any {
				v := &cmd.InstanceVolume{At: "/data", Readonly: true, Size: 1024}
				v.Name = "my-volume"
				return v
			}(),
			wantText: func() any {
				v := &cmd.InstanceVolume{At: "/data", Readonly: true, Size: 1024}
				v.Name = "my-volume"
				return v
			}(),
			marshalsToObject: true,
		},
		{
			name:       "InstanceRom",
			object:     `{"name":"my-rom","image":"myuser/my-rom:latest","at":"/rom"}`,
			text:       `"image=myuser/my-rom:latest,at=/rom,name=my-rom"`,
			into:       func() any { return &cmd.InstanceRom{} },
			wantObject: &cmd.InstanceRom{Name: "my-rom", Image: "myuser/my-rom:latest", At: "/rom"},
			wantText:   &cmd.InstanceRom{Name: "my-rom", Image: "myuser/my-rom:latest", At: "/rom"},
		},
		{
			name:   "InstanceNetwork",
			object: `{"name":"eth1","mac":"aa:bb:cc:dd:ee:ff","tap-name":"tap0","ip":"10.0.0.5/24","autoconfig":false}`,
			text:   `"name=eth1,tap-name=tap0"`,
			into:   func() any { return &cmd.InstanceNetwork{} },
			wantObject: &cmd.InstanceNetwork{
				Name:       "eth1",
				MAC:        "aa:bb:cc:dd:ee:ff",
				TapName:    "tap0",
				IP:         "10.0.0.5/24",
				Autoconfig: new(false),
			},
			wantText:         &cmd.InstanceNetwork{Name: "eth1", TapName: "tap0"},
			marshalsToObject: true,
		},
		{
			name:   "InstanceNetworkRelay",
			object: `{"relay":{"name":"my-router-eth0","uuid":"9f8e-7d6c","dns":false}}`,
			text:   `"relay.name=my-router-eth0"`,
			into:   func() any { return &cmd.InstanceNetwork{} },
			wantObject: &cmd.InstanceNetwork{
				Relay: &cmd.InstanceNetworkRelay{
					Name: "my-router-eth0",
					UUID: "9f8e-7d6c",
					DNS:  new(false),
				},
			},
			wantText:         &cmd.InstanceNetwork{Relay: &cmd.InstanceNetworkRelay{Name: "my-router-eth0"}},
			marshalsToObject: true,
		},
		{
			name:   "InstanceScaleToZero",
			object: `{"policy":"on","stateful":true,"cooldown-time":500,"notify-time":100}`,
			text:   `"on"`,
			into:   func() any { return &cmd.InstanceScaleToZero{} },
			wantObject: &cmd.InstanceScaleToZero{
				Policy:       "on",
				Stateful:     true,
				CooldownTime: types.DurationMS(500),
				NotifyTime:   types.DurationMS(100),
			},
			wantText:         &cmd.InstanceScaleToZero{Policy: "on"},
			marshalsToObject: true,
		},
		{
			name:       "Service",
			object:     `{"source":443,"destination":8080,"handlers":["tls","http"]}`,
			text:       `"443:8080/tls+http"`,
			into:       func() any { return &cmd.Service{} },
			wantObject: &cmd.Service{Source: 443, Destination: 8080, Handlers: []platform.ConnectionHandler{"tls", "http"}},
			wantText:   &cmd.Service{Source: 443, Destination: 8080, Handlers: []platform.ConnectionHandler{"tls", "http"}},
			// Service.MarshalText renders "443:8080/tls+http"; MarshalJSON must win.
			marshalsToObject: true,
		},
		{
			name:       "Domain",
			object:     `{"fqdn":"example.com"}`,
			text:       `"example.com"`,
			into:       func() any { return &cmd.Domain{} },
			wantObject: &cmd.Domain{FQDN: "example.com"},
			wantText:   &cmd.Domain{FQDN: "example.com"},
		},
		{
			name:   "DomainWithCertificate",
			object: `{"fqdn":"example.com","certificate":"fra/my-cert"}`,
			text:   `"example.com"`,
			into:   func() any { return &cmd.Domain{} },
			wantObject: func() any {
				d := &cmd.Domain{FQDN: "example.com"}
				d.Certificate.Metro = "fra"
				d.Certificate.Name = "my-cert"
				return d
			}(),
			wantText: &cmd.Domain{FQDN: "example.com"},
		},
		{
			// Whitespace around a key belongs to neither the metro nor the name;
			// multimetro.ParseKey would otherwise keep it.
			name:   "TextLinkPadded",
			object: `{"name":"my-cert"}`,
			text:   `"  my-cert  "`,
			into:   func() any { return &cmd.TextLink[cmd.Certificate]{} },
			wantObject: func() any {
				l := &cmd.TextLink[cmd.Certificate]{}
				l.Name = "my-cert"
				return l
			}(),
			wantText: func() any {
				l := &cmd.TextLink[cmd.Certificate]{}
				l.Name = "my-cert"
				return l
			}(),
			marshalsToObject: true,
		},
		{
			// A certificate link keeps both name and uuid through JSON, and
			// accepts either form on the way in - same as InstanceService.
			name:   "TextLink",
			object: `{"metro":"fra","name":"my-cert","uuid":"9f8e-7d6c"}`,
			text:   `"fra/my-cert"`,
			into:   func() any { return &cmd.TextLink[cmd.Certificate]{} },
			wantObject: func() any {
				l := &cmd.TextLink[cmd.Certificate]{}
				l.Metro, l.Name, l.UUID = "fra", "my-cert", "9f8e-7d6c"
				return l
			}(),
			wantText: func() any {
				l := &cmd.TextLink[cmd.Certificate]{}
				l.Metro, l.Name = "fra", "my-cert"
				return l
			}(),
			marshalsToObject: true,
		},
		{
			name:       "InstanceArgs",
			object:     `["--verbose","--port=80"]`,
			text:       `"--verbose --port=80"`,
			into:       func() any { return &cmd.InstanceArgs{} },
			wantObject: &cmd.InstanceArgs{"--verbose", "--port=80"},
			wantText:   &cmd.InstanceArgs{"--verbose", "--port=80"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			t.Run("decode object", func(t *testing.T) {
				got := tt.into()
				require.NoError(t, json.Unmarshal([]byte(tt.object), got))
				assert.Equal(t, tt.wantObject, got)
			})

			t.Run("decode text", func(t *testing.T) {
				got := tt.into()
				require.NoError(t, json.Unmarshal([]byte(tt.text), got))
				assert.Equal(t, tt.wantText, got)
			})

			t.Run("encode round-trip", func(t *testing.T) {
				encoded, err := json.Marshal(tt.wantObject)
				require.NoError(t, err)
				if tt.marshalsToObject {
					assert.NotEqual(t, byte('"'), encoded[0],
						"encoded to a bare string, dropping fields: %s", encoded)
				}

				got := tt.into()
				require.NoError(t, json.Unmarshal(encoded, got))
				assert.Equal(t, tt.wantObject, got)
			})
		})
	}
}

// TestParseTextFields covers the --set path, which reaches these types
// through value.Parse rather than encoding/json. A nested reference such as
// certificate= resolves via that field's own UnmarshalText, so a type that
// only satisfies encoding/json is not enough here.
func TestParseTextFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  cmd.Domain
	}{
		{
			name:  "fqdn only",
			input: "fqdn=example.com",
			want:  cmd.Domain{FQDN: "example.com"},
		},
		{
			name:  "certificate by name",
			input: "fqdn=example.com,certificate=my-cert",
			want: func() cmd.Domain {
				d := cmd.Domain{FQDN: "example.com"}
				d.Certificate.Name = "my-cert"
				return d
			}(),
		},
		{
			name:  "certificate with metro",
			input: "name=demo,certificate=fra/my-cert",
			want: func() cmd.Domain {
				d := cmd.Domain{Name: "demo"}
				d.Certificate.Metro = "fra"
				d.Certificate.Name = "my-cert"
				return d
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := value.Parse[cmd.Domain]([]string{tt.input})
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestEditPatches covers the --set/--add/--del paths, where each flag carries
// one element verbatim and multi-value patches come from repeating it.
func TestEditPatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		res     resource.Resource
		spec    patch.PatchSpec
		want    map[string]string
		wantErr string
	}{
		{
			name: "volumes add repeated",
			res:  cmd.Instance{},
			spec: patch.PatchSpec{Add: map[string][]string{"volumes": {"a:/x", "b:/y:ro"}}},
			want: map[string]string{"volumes.add": "[a:/x, b:/y:ro]"},
		},
		{
			name: "volumes del by name",
			res:  cmd.Instance{},
			spec: patch.PatchSpec{Del: map[string][]string{"volumes": {"my-vol"}}},
			want: map[string]string{"volumes.del": `["my-vol"]`},
		},
		{
			// Roms delete by bare name, matching volumes.
			name: "roms del by name",
			res:  cmd.Instance{},
			spec: patch.PatchSpec{Del: map[string][]string{"roms": {"r1", "r2"}}},
			want: map[string]string{"roms.del": `["r1", "r2"]`},
		},
		{
			name: "env keeps commas verbatim",
			res:  cmd.Instance{},
			spec: patch.PatchSpec{Set: map[string][]string{"runtime.env": {"A=1", "B=x,y"}}},
			want: map[string]string{"runtime.env.set": "A=1, B=x,y"},
		},
		{
			name: "services set repeated",
			res:  cmd.ServiceGroup{},
			spec: patch.PatchSpec{Set: map[string][]string{"services": {"443:8080/tls", "80:80/http"}}},
			want: map[string]string{"services.set": "[443:8080/tls, 80:80/http]"},
		},
		{
			name: "domains add with certificate",
			res:  cmd.ServiceGroup{},
			spec: patch.PatchSpec{Add: map[string][]string{"domains": {"fqdn=a.com,certificate=my-cert"}}},
			want: map[string]string{"domains.add": "[fqdn=a.com, certificate=my-cert]"},
		},
		{
			// A comma-joined value must fail loudly rather than silently
			// becoming one mangled element.
			name:    "csv volumes rejected",
			res:     cmd.Instance{},
			spec:    patch.PatchSpec{Add: map[string][]string{"volumes": {"a:/x,b:/y"}}},
			wantErr: "invalid volume option",
		},
		{
			// An instance's service group is create-only; edit it through
			// the service resource instead.
			name:    "instance service not editable",
			res:     cmd.Instance{},
			spec:    patch.PatchSpec{Set: map[string][]string{"service.domains": {"x.com"}}},
			wantErr: "fields not settable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fields, err := tt.res.Fields(t.Context())
			require.NoError(t, err)

			patched, err := patch.PatchedFields(fields, tt.spec)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)

			got := map[string]string{}
			for path, f := range resource.IterFields(patched) {
				if f.Edit == nil {
					continue
				}
				for op, v := range map[string]any{"set": f.Edit.Set, "add": f.Edit.Add, "del": f.Edit.Del} {
					if v == nil {
						continue
					}
					s, err := value.Render(v, value.RenderOpts{})
					require.NoError(t, err)
					got[path.String()+"."+op] = s
				}
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestCreatePatches covers the create --set path, where a repeated flag
// carries one whole element each time. Network interfaces only exist here:
// /v1/instances has no patch property for them.
func TestCreatePatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		spec    map[string][]string
		want    []*cmd.InstanceNetwork
		wantErr string
	}{
		{
			name: "relay by name",
			spec: map[string][]string{"networks": {"relay.name=my-router-eth0"}},
			want: []*cmd.InstanceNetwork{
				{Relay: &cmd.InstanceNetworkRelay{Name: "my-router-eth0"}},
			},
		},
		{
			name: "relay with dns opt-out",
			spec: map[string][]string{"networks": {"relay.name=my-router-eth0,relay.dns=false"}},
			want: []*cmd.InstanceNetwork{
				{Relay: &cmd.InstanceNetworkRelay{Name: "my-router-eth0", DNS: new(false)}},
			},
		},
		{
			name:    "dns without a relay target rejected",
			spec:    map[string][]string{"networks": {"name=eth1,relay.dns=false"}},
			wantErr: "relay requires relay.name or relay.uuid",
		},
		{
			name: "relay by uuid",
			spec: map[string][]string{"networks": {"relay.uuid=c1d2e3f4-5678-90ab-cdef-1234567890ab"}},
			want: []*cmd.InstanceNetwork{
				{Relay: &cmd.InstanceNetworkRelay{UUID: "c1d2e3f4-5678-90ab-cdef-1234567890ab"}},
			},
		},
		{
			name: "repeated builds one interface each",
			spec: map[string][]string{"networks": {
				"relay.name=my-router-eth0",
				"name=eth1,tap-name=tap0,ip=10.0.0.5/24,mac=aa:bb:cc:dd:ee:ff,autoconfig=false",
			}},
			want: []*cmd.InstanceNetwork{
				{Relay: &cmd.InstanceNetworkRelay{Name: "my-router-eth0"}},
				{
					Name:       "eth1",
					TapName:    "tap0",
					IP:         "10.0.0.5/24",
					MAC:        "aa:bb:cc:dd:ee:ff",
					Autoconfig: new(false),
				},
			},
		},
		{
			name:    "unknown key rejected",
			spec:    map[string][]string{"networks": {"relay.name=r1,gateway=10.0.0.1"}},
			wantErr: "unknown fields: [gateway]",
		},
		{
			name:    "unknown nested key rejected",
			spec:    map[string][]string{"networks": {"relay.bogus=1"}},
			wantErr: "unknown fields: [relay.bogus]",
		},
		{
			// uuid and private-ip are reported by the API, never set.
			name:    "read-only keys rejected",
			spec:    map[string][]string{"networks": {"uuid=abc"}},
			wantErr: "unknown fields: [uuid]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fields, err := cmd.Instance{}.Fields(t.Context())
			require.NoError(t, err)

			patched, err := patch.PatchedFields(fields, patch.PatchSpec{
				Create: true,
				Set:    tt.spec,
			})
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)

			var got []*cmd.InstanceNetwork
			for path, f := range resource.IterFields(patched) {
				if path.String() != "networks" || f.Create == nil {
					continue
				}
				got = f.Create.Set.([]*cmd.InstanceNetwork)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestNetworkTextRoundTrip guards the compact form against the render/parse
// cycle a shortcut flag goes through: ApplyShortcutFlags renders --network
// back to a --set string, which PatchedFields then parses again. Anything the
// rendered form cannot express is silently dropped there.
func TestNetworkTextRoundTrip(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		"relay.name=my-router-eth0",
		"relay.name=my-router-eth0,relay.dns=false",
		"relay.name=my-router-eth0,relay.dns=true",
		"relay.uuid=c1d2e3f4-5678-90ab-cdef-1234567890ab",
		"name=eth1,tap-name=tap0,ip=10.0.0.5/24,mac=aa:bb:cc:dd:ee:ff,autoconfig=false",
	} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			want, err := value.Parse[cmd.InstanceNetwork]([]string{input})
			require.NoError(t, err)

			rendered, err := value.Render(&want, value.RenderOpts{})
			require.NoError(t, err)

			got, err := value.Parse[cmd.InstanceNetwork]([]string{rendered})
			require.NoError(t, err, "rendered as %q", rendered)
			assert.Equal(t, want, got, "rendered as %q", rendered)
		})
	}
}

// TestLinkCollection guards cross-resource links on list fields.
// resource.FieldsFromStruct harvests the resource.Link interface only from
// ANONYMOUS struct fields, so a list element must stay a struct embedding
// Link. As a bare Link its links vanish from the TUI, sandbox teardown and
// the JSON "links" output, while the rest of the field tree looks identical.
func TestLinkCollection(t *testing.T) {
	t.Parallel()

	vol := cmd.Volume{}
	vol.AttachedTo = append(vol.AttachedTo, struct {
		cmd.Link[cmd.Instance]
	}{Link: cmd.Link[cmd.Instance]{Name: "attached-inst"}})
	vol.MountedBy = append(vol.MountedBy, struct {
		cmd.Link[cmd.Instance]
		ReadOnly bool `mirror:"read_only" field:",long"`
	}{Link: cmd.Link[cmd.Instance]{Name: "mounted-inst"}})

	sg := cmd.ServiceGroup{}
	sg.Instances = append(sg.Instances, struct {
		cmd.Link[cmd.Instance]
	}{Link: cmd.Link[cmd.Instance]{Name: "member-inst"}})

	for _, tt := range []struct {
		res  resource.Resource
		path string
		want string
	}{
		{vol, "attached-to.0", "attached-inst"},
		{vol, "mounted-by.0", "mounted-inst"},
		{sg, "instances.0", "member-inst"},
	} {
		t.Run(tt.path, func(t *testing.T) {
			fields, err := tt.res.Fields(t.Context())
			require.NoError(t, err)

			var got []string
			for path, f := range resource.IterFields(fields) {
				if path.String() != tt.path {
					continue
				}
				for _, l := range f.Links {
					_, key, _ := l.Link()
					got = append(got, key.String())
				}
			}
			assert.Equal(t, []string{tt.want}, got, "link not collected for %s", tt.path)
		})
	}
}

// TestVisualNestedShortcuts guards the visual/--load path for shortcut flags
// that target a nested field, such as --publish (service.services) and
// --domain (service.domains). Their patches have to reach the editor at their
// own path: presented one level up they no longer match any field, and the
// reload fails outright or drops them.
func TestVisualNestedShortcuts(t *testing.T) {
	t.Parallel()

	fields, err := cmd.Instance{}.Fields(t.Context())
	require.NoError(t, err)

	pending, err := patch.PatchedFields(fields, patch.PatchSpec{
		Create: true,
		Set: map[string][]string{
			"metro":            {"fra"},
			"name":             {"my-instance"},
			"image":            {"nginx:latest"},
			"service":          {"my-group"},
			"service.domains":  {"a.com"},
			"service.services": {"443:8080/tls"},
		},
	})
	require.NoError(t, err)

	var shown []byte
	out, err := patch.Create(t.Context(), cmd.Instance{}, fields, pending,
		func(_ context.Context, data []byte) ([]byte, error) {
			shown = data
			return data, nil
		})
	require.NoError(t, err)

	assert.Equal(t, `# instance
image: nginx
metro: fra
name: my-instance
service:
  domains:
  - fqdn: a.com
  name: my-group
  services:
  - destination: 8080
    handlers:
    - tls
    source: 443
`, string(shown))

	got := map[string]string{}
	for path, f := range resource.IterFields(out) {
		if f.Create == nil || f.Create.Set == nil {
			continue
		}
		rendered, err := value.Render(f.Create.Set, value.RenderOpts{})
		require.NoError(t, err)
		got[path.String()] = rendered
	}
	assert.Equal(t, map[string]string{
		"metro":            "fra",
		"name":             "my-instance",
		"image":            "nginx",
		"service":          "my-group",
		"service.domains":  "[fqdn=a.com]",
		"service.services": "[443:8080/tls]",
	}, got)

	// Consuming the ancestor last must still leave unmatched keys behind for
	// the unknown-field check.
	_, err = patch.Create(t.Context(), cmd.Instance{}, fields, pending,
		func(_ context.Context, data []byte) ([]byte, error) {
			return append(data, []byte("bogus: 1\n")...), nil
		})
	require.ErrorContains(t, err, "unknown fields: [bogus]")
}
