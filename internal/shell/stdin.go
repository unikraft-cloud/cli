// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/muesli/cancelreader"
)

func terminalStdin(ctx context.Context, tty *os.File) io.Reader {
	reader, err := cancelreader.NewReader(tty)
	if err != nil {
		return tty
	}

	lent := &lentTerminal{reader: reader}
	go func() {
		<-ctx.Done()
		lent.release()
	}()

	return lent
}

type lentTerminal struct {
	reader cancelreader.CancelReader

	mu   sync.RWMutex
	done bool
}

func (l *lentTerminal) Read(b []byte) (int, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.done {
		return 0, io.EOF
	}

	n, err := l.reader.Read(b)
	if errors.Is(err, cancelreader.ErrCanceled) {
		return n, io.EOF
	}
	return n, err
}

func (l *lentTerminal) release() {
	l.reader.Cancel()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.done = true
	_ = l.reader.Close()
}
