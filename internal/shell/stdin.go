// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

// StdinPump bridges a single io.Reader (the terminal stdin) to two
// consumers in the shell command:
//
//  1. ReadlineReader — feeds the readline prompt via the standard Read
//     method, so the user can type interactive commands.
//  2. CmdReader — feeds stdin of the remote-sandbox exec via the
//     context-aware ReadContext method, so that when a running command
//     is interrupted (^C) the read respects cmdCtx cancellation.
//
// Both consumers share the same underlying channel pumped by a single
// background goroutine.  The Close method signals that goroutine to
// stop, which is essential for timely terminal raw-mode restoration
// when the shell exits.
import (
	"context"
	"io"
	"sync"
)

type StdinPump struct {
	r      io.Reader
	ch     chan []byte
	mu     sync.Mutex
	buf    []byte
	err    error
	closed chan struct{}
	once   sync.Once
}

func NewStdinPump(r io.Reader) *StdinPump {
	p := &StdinPump{
		r:      r,
		ch:     make(chan []byte, 64),
		closed: make(chan struct{}),
	}
	go p.run(r)
	return p
}

func (p *StdinPump) run(r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			select {
			case p.ch <- chunk:
			case <-p.closed:
				return
			}
		}
		if err != nil {
			p.mu.Lock()
			p.err = err
			p.mu.Unlock()
			close(p.ch)
			return
		}
	}
}

func (p *StdinPump) Read(b []byte) (int, error) {
	p.mu.Lock()
	if len(p.buf) > 0 {
		n := copy(b, p.buf)
		p.buf = p.buf[n:]
		p.mu.Unlock()
		return n, nil
	}
	p.mu.Unlock()

	chunk, ok := <-p.ch
	if !ok {
		p.mu.Lock()
		err := p.err
		p.mu.Unlock()
		if err == nil {
			err = io.EOF
		}
		return 0, err
	}

	n := copy(b, chunk)
	if n < len(chunk) {
		p.mu.Lock()
		p.buf = chunk[n:]
		p.mu.Unlock()
	}
	return n, nil
}

func (p *StdinPump) ReadContext(ctx context.Context) ([]byte, error) {
	p.mu.Lock()
	if len(p.buf) > 0 {
		chunk := p.buf
		p.buf = nil
		p.mu.Unlock()
		return chunk, nil
	}
	p.mu.Unlock()

	select {
	case chunk, ok := <-p.ch:
		if !ok {
			p.mu.Lock()
			err := p.err
			p.mu.Unlock()
			if err == nil {
				err = io.EOF
			}
			return nil, err
		}
		return chunk, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *StdinPump) Close() error {
	p.once.Do(func() { close(p.closed) })
	return nil
}
