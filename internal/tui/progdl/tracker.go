// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package progdl

import (
	"context"
	"io"
	"sync"

	tea "charm.land/bubbletea/v2"
	xio "unikraft.com/cli/internal/x/io"
	imgprogress "unikraft.com/x/image-spec/progress"
)

type msgSender interface {
	Send(tea.Msg)
}

// aggregatingTracker aggregates per-descriptor progress into a single 0–1
// fraction and forwards it to bubbletea as progressMsg messages.
type aggregatingTracker struct {
	mu     sync.Mutex
	descs  map[string]imgprogress.Progress // keyed by descriptor digest
	sender msgSender
}

func (t *aggregatingTracker) Update(p imgprogress.Progress) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := p.Descriptor.Digest.String()
	t.descs[key] = p

	var totalBytes, currentBytes int64
	for _, d := range t.descs {
		totalBytes += d.Total
		currentBytes += d.Current
	}

	if totalBytes > 0 {
		t.sender.Send(progressMsg(float64(currentBytes) / float64(totalBytes)))
	}
}

// TrackImageProgress runs fn with an image progress tracker attached to ctx.
// A progress bar is displayed on out while fn executes.
func TrackImageProgress(ctx context.Context, out io.Writer, fn func(ctx context.Context) error) error {
	if !xio.IsTTY(out) {
		return fn(ctx)
	}

	fnCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	m := New()
	p := tea.NewProgram(m,
		tea.WithOutput(xio.Unwrap(out)),
		tea.WithContext(fnCtx),
	)

	tracker := &aggregatingTracker{
		descs:  make(map[string]imgprogress.Progress),
		sender: p,
	}

	trackerCtx := imgprogress.WithTracker(fnCtx, tracker)

	fnErrCh := make(chan error, 1)
	go func() {
		err := fn(trackerCtx)
		if err != nil {
			p.Send(errMsg{err: err})
		} else {
			p.Send(doneMsg{})
		}
		fnErrCh <- err
	}()

	_, teaErr := p.Run()
	cancel()

	fnErr := <-fnErrCh

	if ctx.Err() != nil {
		return ctx.Err()
	}
	if fnErr != nil {
		return fnErr
	}
	return teaErr
}
