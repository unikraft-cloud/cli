// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package rollout_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"unikraft.com/cli/internal/rollout"
)

type recorder struct {
	calls       []string
	fail        []string
	cancelAfter string
	cancel      context.CancelFunc
}

type fakeSet struct {
	name string
	rec  *recorder
}

func (f *fakeSet) do(ctx context.Context, step string) error {
	step = f.name + "." + step
	call := step
	if ctx.Err() != nil {
		call += " cancelled"
	}
	f.rec.calls = append(f.rec.calls, call)
	if step == f.rec.cancelAfter {
		f.rec.cancel()
	}
	if slices.Contains(f.rec.fail, step) {
		return fmt.Errorf("%s failed", step)
	}
	return nil
}

func (f *fakeSet) Names() []string                  { return []string{f.name + "-0"} }
func (f *fakeSet) Start(ctx context.Context) error  { return f.do(ctx, "start") }
func (f *fakeSet) Stop(ctx context.Context) error   { return f.do(ctx, "stop") }
func (f *fakeSet) Up(ctx context.Context) error     { return f.do(ctx, "up") }
func (f *fakeSet) Delete(ctx context.Context) error { return f.do(ctx, "delete") }

// TestRollout pins the steps of each strategy and the steps of its undo.
func TestRollout(t *testing.T) {
	tests := []struct {
		name        string
		strategy    rollout.Strategy
		fail        []string
		cancelAfter string
		calls       []string
		done        bool
		err         string
	}{
		{
			name:     "rolling",
			strategy: rollout.Rolling{},
			calls:    []string{"create", "new.up", "old.delete"},
			done:     true,
		},
		{
			name:     "rolling holds the new set up",
			strategy: rollout.Rolling{HealthyAfter: time.Millisecond},
			calls:    []string{"create", "new.up", "new.up", "old.delete"},
			done:     true,
		},
		{
			name:     "rolling deletes a partial new set",
			strategy: rollout.Rolling{},
			fail:     []string{"create"},
			calls:    []string{"create", "new.delete"},
			err:      "create failed",
		},
		{
			name:     "rolling deletes a new set that is not up",
			strategy: rollout.Rolling{},
			fail:     []string{"new.up"},
			calls:    []string{"create", "new.up", "new.delete"},
			err:      "the new instances did not come up: new.up failed",
		},
		{
			name:     "rolling is done when the old set stays",
			strategy: rollout.Rolling{},
			fail:     []string{"old.delete"},
			calls:    []string{"create", "new.up", "old.delete"},
			done:     true,
			err:      "deleting the replaced ones failed: old.delete failed",
		},
		{
			name:        "rolling finishes the create before an interrupt",
			strategy:    rollout.Rolling{},
			cancelAfter: "create",
			calls:       []string{"create", "new.delete"},
			err:         "rollout interrupted while the new instances were created",
		},
		{
			name:        "rolling undoes an interrupt",
			strategy:    rollout.Rolling{HealthyAfter: time.Hour},
			cancelAfter: "new.up",
			calls:       []string{"create", "new.up", "new.delete"},
			err:         "rollout interrupted",
		},
		{
			name:     "replace",
			strategy: rollout.Replace{},
			calls:    []string{"create", "old.stop", "new.start", "new.up", "old.delete"},
			done:     true,
		},
		{
			name:     "replace restores an old set that does not stop",
			strategy: rollout.Replace{},
			fail:     []string{"old.stop"},
			calls:    []string{"create", "old.stop", "old.start", "old.up", "new.delete"},
			err:      "stopping the replaced instances: old.stop failed",
		},
		{
			name:     "replace restores the old set when the new set is not up",
			strategy: rollout.Replace{},
			fail:     []string{"new.up"},
			calls:    []string{"create", "old.stop", "new.start", "new.up", "new.stop", "old.start", "old.up", "new.delete"},
			err:      "the new instances did not come up",
		},
		{
			name:        "replace finishes the create before an interrupt",
			strategy:    rollout.Replace{},
			cancelAfter: "create",
			calls:       []string{"create", "new.delete"},
			err:         "rollout interrupted while the new instances were created",
		},
		{
			name:        "replace undoes an interrupt",
			strategy:    rollout.Replace{HealthyAfter: time.Hour},
			cancelAfter: "new.up",
			calls:       []string{"create", "old.stop", "new.start", "new.up", "new.stop", "old.start", "old.up", "new.delete"},
			err:         "rollout interrupted",
		},
		{
			name:     "replace keeps the new set when the restore fails",
			strategy: rollout.Replace{},
			fail:     []string{"new.up", "old.start"},
			calls:    []string{"create", "old.stop", "new.start", "new.up", "new.stop", "old.start"},
			err:      "the new instances new-0 are kept",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			rec := &recorder{fail: tt.fail, cancelAfter: tt.cancelAfter, cancel: cancel}

			done, err := tt.strategy.Rollout(ctx, &fakeSet{name: "old", rec: rec}, func(ctx context.Context) (rollout.Set, error) {
				if tt.cancelAfter == "create" {
					cancel()
				}
				call := "create"
				if ctx.Err() != nil {
					call += " cancelled"
				}
				rec.calls = append(rec.calls, call)
				if slices.Contains(rec.fail, "create") {
					return &fakeSet{name: "new", rec: rec}, fmt.Errorf("create failed")
				}
				return &fakeSet{name: "new", rec: rec}, nil
			})

			assert.Equal(t, tt.calls, rec.calls)
			assert.Equal(t, tt.done, done)
			if tt.err == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.err)
			}
		})
	}
}
