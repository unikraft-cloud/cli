// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package progdl

import (
	"context"
	"errors"
	"io"

	tea "charm.land/bubbletea/v2"

	xio "unikraft.com/x/io"
)

// ErrDownloadInterrupted is returned when the download is interrupted by the user.
var ErrDownloadInterrupted = errors.New("download interrupted")

// DownloadWithProgress downloads from reader to writer with a progress bar.
// If out is not a terminal, it falls back to a simple download without progress display.
func DownloadWithProgress(ctx context.Context, out io.Writer, reader io.ReadCloser, writer io.Writer, totalSize int64) error {
	// Create a cancellable context so we can stop the download when TUI exits
	downloadCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Wrap reader to respect context cancellation
	ctxReader := newContextReader(downloadCtx, reader)

	if !xio.IsTTY(out) {
		// Fallback: just copy without progress display
		_, err := io.Copy(writer, ctxReader)
		return err
	}

	// Create channels for coordination
	progressCh := make(chan float64, 100)
	doneCh := make(chan error, 1)

	// Create progress writer
	pw := &progressWriter{
		total: totalSize,
		file:  writer,
		onProgress: func(pct float64) {
			select {
			case progressCh <- pct:
			default:
			}
		},
	}

	// Create and run the bubbletea program
	m := New()
	p := tea.NewProgram(m,
		tea.WithOutput(xio.Unwrap(out)),
		tea.WithContext(ctx),
	)

	// Start download in background
	go func() {
		_, err := io.Copy(pw, ctxReader)
		// Signal completion to TUI
		if err != nil {
			p.Send(errMsg{err: err})
		} else {
			p.Send(doneMsg{})
		}
		doneCh <- err
	}()

	// Send progress updates to the program
	go func() {
		for {
			select {
			case pct := <-progressCh:
				p.Send(progressMsg(pct))
			case <-downloadCtx.Done():
				return
			}
		}
	}()

	_, err := p.Run()

	// Cancel the download context to stop the download goroutine
	cancel()

	// Wait for download goroutine to finish
	downloadErr := <-doneCh

	// Check if parent context was cancelled (user interrupted via signal).
	// With tea.WithContext(ctx), a cancelled context can cause p.Run to return
	// a non-nil error; we normalize that to ErrDownloadInterrupted.
	if ctx.Err() != nil {
		return ErrDownloadInterrupted
	}

	// If the Bubble Tea program returned an error for reasons other than
	// context cancellation, surface it.
	if err != nil {
		return err
	}

	// Check if download had an error
	if downloadErr != nil {
		// If the download context was cancelled (by us after TUI exit), report as interrupted
		if downloadErr == context.Canceled {
			return ErrDownloadInterrupted
		}
		return downloadErr
	}

	return nil
}
