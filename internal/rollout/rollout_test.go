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

	"github.com/stretchr/testify/assert"

	"unikraft.com/cli/internal/rollout"
)

type recorder struct {
	calls []string
	fail  []string
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
	if slices.Contains(f.rec.fail, step) {
		return fmt.Errorf("%s failed", step)
	}
	return nil
}

func (f *fakeSet) Names() []string                  { return []string{f.name + "-0"} }
func (f *fakeSet) Up(ctx context.Context) error     { return f.do(ctx, "up") }
func (f *fakeSet) Delete(ctx context.Context) error { return f.do(ctx, "delete") }

// TestRollout pins the steps of each strategy and the steps of its undo.
func TestRollout(t *testing.T) {
	tests := []struct {
		name        string
		fail        []string
		cancelAfter string
		calls       []string
		done        bool
		err         string
	}{
		{
			name:  "rolling",
			calls: []string{"create", "new.up", "old.delete"},
			done:  true,
		},
		{
			name:  "rolling deletes a partial new set",
			fail:  []string{"create"},
			calls: []string{"create", "new.delete"},
			err:   "create failed",
		},
		{
			name:  "rolling deletes a new set that is not up",
			fail:  []string{"new.up"},
			calls: []string{"create", "new.up", "new.delete"},
			err:   "the new instances did not come up: new.up failed",
		},
		{
			name:  "rolling is done when the old set stays",
			fail:  []string{"old.delete"},
			calls: []string{"create", "new.up", "old.delete"},
			done:  true,
			err:   "deleting the replaced ones failed: old.delete failed",
		},
		{
			name:        "rolling finishes the create before an interrupt",
			cancelAfter: "create",
			calls:       []string{"create", "new.delete"},
			err:         "rollout interrupted while the new instances were created",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			rec := &recorder{fail: tt.fail}

			done, err := rollout.Rolling{}.Rollout(ctx, &fakeSet{name: "old", rec: rec}, func(ctx context.Context) (rollout.Set, error) {
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
