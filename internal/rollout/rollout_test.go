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
	calls    []string
	count    map[string]int
	fail     []string
	cancelAt string
	cancel   context.CancelFunc
	created  int
}

type fakeSet struct {
	name   string
	lo, hi int
	rec    *recorder
}

func (f *fakeSet) do(ctx context.Context, step string) error {
	step = fmt.Sprintf("%s[%d:%d].%s", f.name, f.lo, f.hi, step)
	if step == f.rec.cancelAt {
		f.rec.cancel()
	}
	call := step
	if ctx.Err() != nil {
		call += " cancelled"
	}
	f.rec.calls = append(f.rec.calls, call)
	f.rec.count[step]++
	if slices.Contains(f.rec.fail, step) || slices.Contains(f.rec.fail, fmt.Sprintf("%s#%d", step, f.rec.count[step])) {
		return fmt.Errorf("%s failed", step)
	}
	return nil
}

func (f *fakeSet) Len() int { return f.hi - f.lo }

func (f *fakeSet) Take(n int) (rollout.Set, rollout.Set) {
	mid := min(f.lo+n, f.hi)
	return &fakeSet{name: f.name, lo: f.lo, hi: mid, rec: f.rec}, &fakeSet{name: f.name, lo: mid, hi: f.hi, rec: f.rec}
}

func (f *fakeSet) Names() []string {
	var names []string
	for i := f.lo; i < f.hi; i++ {
		names = append(names, fmt.Sprintf("%s-%d", f.name, i))
	}
	return names
}

func (f *fakeSet) Start(ctx context.Context) error  { return f.do(ctx, "start") }
func (f *fakeSet) Stop(ctx context.Context) error   { return f.do(ctx, "stop") }
func (f *fakeSet) Up(ctx context.Context) error     { return f.do(ctx, "up") }
func (f *fakeSet) Delete(ctx context.Context) error { return f.do(ctx, "delete") }

// TestApply pins the steps of each strategy and the steps of its undo.
func TestApply(t *testing.T) {
	tests := []struct {
		name     string
		strategy rollout.Strategy
		fail     []string
		cancelAt string
		calls    []string
		done     bool
		err      string
	}{
		{
			name:     "rolling",
			strategy: rollout.Rolling{},
			calls: []string{
				"new[0:1].create", "new[0:1].start", "new[0:1].up", "old[0:1].stop",
				"new[1:2].create", "new[1:2].start", "new[1:2].up", "new[0:1].up", "old[1:2].stop",
				"old[0:2].delete",
			},
			done: true,
		},
		{
			name:     "rolling in one batch holds the new set up",
			strategy: rollout.Rolling{Batch: 5, HealthyAfter: time.Millisecond},
			calls:    []string{"new[0:2].create", "new[0:2].start", "new[0:2].up", "new[0:2].up", "old[0:2].stop", "old[0:2].delete"},
			done:     true,
		},
		{
			name:     "rolling deletes a partial new set",
			strategy: rollout.Rolling{},
			fail:     []string{"new[0:1].create"},
			calls:    []string{"new[0:1].create", "new[0:1].delete"},
			err:      "new[0:1].create failed",
		},
		{
			name:     "rolling starts the stopped batches again when a batch is not up",
			strategy: rollout.Rolling{},
			fail:     []string{"new[1:2].up"},
			calls: []string{
				"new[0:1].create", "new[0:1].start", "new[0:1].up", "old[0:1].stop",
				"new[1:2].create", "new[1:2].start", "new[1:2].up",
				"old[0:1].start", "old[0:1].up", "new[0:1].delete", "new[1:2].delete",
			},
			err: "the new instances did not come up: new[1:2].up failed",
		},
		{
			name:     "rolling starts the stopped batches again when an earlier batch goes down",
			strategy: rollout.Rolling{},
			fail:     []string{"new[0:1].up#2"},
			calls: []string{
				"new[0:1].create", "new[0:1].start", "new[0:1].up", "old[0:1].stop",
				"new[1:2].create", "new[1:2].start", "new[1:2].up", "new[0:1].up",
				"old[0:1].start", "old[0:1].up", "new[0:1].delete", "new[1:2].delete",
			},
			err: "the new instances did not stay up: new[0:1].up failed",
		},
		{
			name:     "rolling starts the kept batch that did not run when the restore fails",
			strategy: rollout.Rolling{},
			fail:     []string{"old[0:1].start"},
			cancelAt: "new[1:2].create",
			calls: []string{
				"new[0:1].create", "new[0:1].start", "new[0:1].up", "old[0:1].stop",
				"new[1:2].create", "old[0:1].start", "new[1:2].start",
			},
			err: "the new instances new-0 new-1 are kept",
		},
		{
			name:     "rolling is done when the old set stays",
			strategy: rollout.Rolling{Batch: 2},
			fail:     []string{"old[0:2].delete"},
			calls:    []string{"new[0:2].create", "new[0:2].start", "new[0:2].up", "old[0:2].stop", "old[0:2].delete"},
			done:     true,
			err:      "deleting the replaced ones failed: old[0:2].delete failed",
		},
		{
			name:     "rolling finishes the create before an interrupt",
			strategy: rollout.Rolling{},
			cancelAt: "new[0:1].create",
			calls:    []string{"new[0:1].create", "new[0:1].delete"},
			err:      "rollout interrupted while the new instances were created",
		},
		{
			name:     "rolling undoes an interrupt",
			strategy: rollout.Rolling{HealthyAfter: time.Hour},
			cancelAt: "new[0:1].up",
			calls:    []string{"new[0:1].create", "new[0:1].start", "new[0:1].up cancelled", "new[0:1].delete"},
			err:      "rollout interrupted while the new instances were up",
		},
		{
			name:     "recreate",
			strategy: rollout.Recreate{},
			calls:    []string{"new[0:2].create", "old[0:2].stop", "new[0:2].start", "new[0:2].up", "old[0:2].delete"},
			done:     true,
		},
		{
			name:     "recreate restores an old set that does not stop",
			strategy: rollout.Recreate{},
			fail:     []string{"old[0:2].stop"},
			calls:    []string{"new[0:2].create", "old[0:2].stop", "old[0:2].start", "old[0:2].up", "new[0:2].delete"},
			err:      "stopping the replaced instances: old[0:2].stop failed",
		},
		{
			name:     "recreate restores the old set when the new set is not up",
			strategy: rollout.Recreate{},
			fail:     []string{"new[0:2].up"},
			calls: []string{
				"new[0:2].create", "old[0:2].stop", "new[0:2].start", "new[0:2].up",
				"new[0:2].stop", "old[0:2].start", "old[0:2].up", "new[0:2].delete",
			},
			err: "the new instances did not come up",
		},
		{
			name:     "recreate keeps the new set running when the restore fails",
			strategy: rollout.Recreate{},
			fail:     []string{"new[0:2].up", "old[0:2].start"},
			calls: []string{
				"new[0:2].create", "old[0:2].stop", "new[0:2].start", "new[0:2].up",
				"new[0:2].stop", "old[0:2].start", "new[0:2].start",
			},
			err: "the new instances new-0 new-1 are kept",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			rec := &recorder{count: map[string]int{}, fail: tt.fail, cancelAt: tt.cancelAt, cancel: cancel}

			done, err := tt.strategy.Apply(ctx, &fakeSet{name: "old", hi: 2, rec: rec}, func(ctx context.Context, n int) (rollout.Set, error) {
				set := &fakeSet{name: "new", lo: rec.created, hi: rec.created + n, rec: rec}
				rec.created += n
				return set, set.do(ctx, "create")
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
