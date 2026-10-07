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

	"unikraft.com/cloud/sdk/platform"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/types"
)

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

// TestTryAttachPluginWithoutTTY pins that with nobody at a terminal the
// question is not asked: the command fails as before, pointing at how to
// attach the plugin by hand, and nothing is touched.
func TestTryAttachPluginWithoutTTY(t *testing.T) {
	cause := errors.New(`instance "my-inst" has no plugins loaded`)
	opts := SandboxPluginOpts{PluginName: "sandbox", PluginImage: "plugins/sandbox:latest"}
	instance := Instance{
		Name:  "my-inst",
		State: types.InstanceState(platform.InstanceStateRunning),
		key:   multimetro.Key{Metro: "fra", Name: "my-inst"},
	}

	t.Run("hint", func(t *testing.T) {
		// A yes on a pipe is not an answer: it was not given to a question.
		stdio := config.Stdio{Stdin: strings.NewReader("y\n"), Stdout: &bytes.Buffer{}}

		err := tryAttachPlugin(t.Context(), stdio, nil, instance, opts, cause)

		require.ErrorIs(t, err, cause)
		assert.Equal(t, cause.Error()+"\nhint: attach it with `unikraft instance edit fra/my-inst --plugin name=sandbox,image=plugins/sandbox:latest` while the instance is stopped", err.Error())
	})

	t.Run("delete-on-stop", func(t *testing.T) {
		doomed := instance
		doomed.Instance.Features = []platform.InstanceFeature{platform.InstanceFeatureDeleteOnStop}
		stdio := config.Stdio{Stdin: strings.NewReader("y\n"), Stdout: &bytes.Buffer{}}

		err := tryAttachPlugin(t.Context(), stdio, nil, doomed, opts, cause)

		require.ErrorIs(t, err, cause)
		assert.Contains(t, err.Error(), "deleted when it stops")
		assert.Contains(t, err.Error(), "--plugin name=sandbox,image=plugins/sandbox:latest")
	})
}

// TestPendingPlugin pins how an attached-but-not-loaded plugin is recognised:
// a queued set or add of the plugins property naming it. A queued removal, or
// a queued change to anything else, is not it.
func TestPendingPlugin(t *testing.T) {
	sandbox := []any{map[string]any{"name": "sandbox", "image": "plugins/sandbox:staging"}}
	for _, tt := range []struct {
		name    string
		updates []platform.InstancePendingUpdate
		want    bool
	}{
		{"none", nil, false},
		{"add", []platform.InstancePendingUpdate{{Prop: platform.MutableInstancePropertyPlugins, Op: platform.MutableInstanceOperationAdd, Value: sandbox}}, true},
		{"set", []platform.InstancePendingUpdate{{Prop: platform.MutableInstancePropertyPlugins, Op: platform.MutableInstanceOperationSet, Value: sandbox}}, true},
		{"del", []platform.InstancePendingUpdate{{Prop: platform.MutableInstancePropertyPlugins, Op: platform.MutableInstanceOperationDel, Value: []any{"sandbox"}}}, false},
		{"another-plugin", []platform.InstancePendingUpdate{{Prop: platform.MutableInstancePropertyPlugins, Op: platform.MutableInstanceOperationAdd, Value: []any{map[string]any{"name": "other"}}}}, false},
		{"another-property", []platform.InstancePendingUpdate{{Prop: platform.MutableInstancePropertyRoms, Op: platform.MutableInstanceOperationAdd, Value: sandbox}}, false},
		{"malformed-value", []platform.InstancePendingUpdate{{Prop: platform.MutableInstancePropertyPlugins, Op: platform.MutableInstanceOperationAdd, Value: "sandbox"}}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			instance := Instance{Name: "my-inst"}
			instance.Instance.Updates = tt.updates

			_, ok := pendingPlugin(instance, "sandbox")

			assert.Equal(t, tt.want, ok)
		})
	}
}

// TestTryLoadPluginWithoutTTY pins that a plugin attached but not loaded is
// not attached a second time: on a pipe the command fails saying so, pointing
// at the restart that loads it, or at what the platform failed on.
func TestTryLoadPluginWithoutTTY(t *testing.T) {
	pending := platform.InstancePendingUpdate{
		Prop:   platform.MutableInstancePropertyPlugins,
		Op:     platform.MutableInstanceOperationAdd,
		Value:  []any{map[string]any{"name": "sandbox"}},
		Status: platform.InstancePendingUpdateStatusPending,
	}
	instance := Instance{
		Name:  "my-inst",
		State: types.InstanceState(platform.InstanceStateRunning),
		key:   multimetro.Key{Metro: "fra", Name: "my-inst"},
	}

	t.Run("running", func(t *testing.T) {
		stdio := config.Stdio{Stdin: strings.NewReader("y\n"), Stdout: &bytes.Buffer{}}

		err := tryLoadPlugin(t.Context(), stdio, instance, "sandbox", pending)

		require.Error(t, err)
		assert.Equal(t, `instance "my-inst" has plugin "sandbox" attached but not loaded yet`+"\nhint: restart it with `unikraft instance restart fra/my-inst` to load it", err.Error())
	})

	t.Run("stopped", func(t *testing.T) {
		stopped := instance
		stopped.State = types.InstanceState(platform.InstanceStateStopped)
		stdio := config.Stdio{Stdin: strings.NewReader("y\n"), Stdout: &bytes.Buffer{}}

		err := tryLoadPlugin(t.Context(), stdio, stopped, "sandbox", pending)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "hint: start it with `unikraft instance start fra/my-inst` to load it")
	})

	t.Run("failed", func(t *testing.T) {
		failed := pending
		failed.Status = platform.InstancePendingUpdateStatusFailed
		failed.Error = new("image not found")
		stdio := config.Stdio{Stdin: strings.NewReader("y\n"), Stdout: &bytes.Buffer{}}

		err := tryLoadPlugin(t.Context(), stdio, instance, "sandbox", failed)

		require.Error(t, err)
		assert.Equal(t, `instance "my-inst" has plugin "sandbox" attached, but applying it failed: image not found`+"\nhint: detach it with `unikraft instance edit fra/my-inst --del plugins=sandbox` and attach it again", err.Error())
	})
}
