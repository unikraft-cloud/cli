// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package progdl

import (
	"context"
	"io"
	"sync"

	tea "charm.land/bubbletea/v2"

	imgprogress "unikraft.com/x/image-spec/progress"
	xio "unikraft.com/x/io"
	"unikraft.com/x/log"
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
	if prev, ok := t.descs[key]; ok {
		p.Current = max(p.Current, prev.Current)
	}
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
// It shows a progress bar on out if out is a terminal and debug logs are off.
func TrackImageProgress(ctx context.Context, out io.Writer, fn func(ctx context.Context) error) error {
	if !xio.IsTTY(out) || log.G(ctx).GetLevel() <= log.DebugLevel {
		return fn(ctx)
	}

	p := tea.NewProgram(New(),
		tea.WithOutput(xio.Unwrap(out)),
		tea.WithInput(nil),
		tea.WithoutSignalHandler(),
	)
	stop := context.AfterFunc(ctx, func() {
		p.Send(errMsg{err: ctx.Err()})
	})
	defer stop()

	tracker := &aggregatingTracker{
		descs:  make(map[string]imgprogress.Progress),
		sender: p,
	}

	fnErrCh := make(chan error, 1)
	go func() {
		err := fn(imgprogress.WithTracker(ctx, tracker))
		if err != nil {
			p.Send(errMsg{err: err})
		} else {
			p.Send(doneMsg{})
		}
		fnErrCh <- err
	}()

	if _, err := p.Run(); err != nil {
		log.G(ctx).Warn().Err(err).Msg("progress bar failed")
	}
	if err := <-fnErrCh; err != nil {
		return err
	}
	return ctx.Err()
}
