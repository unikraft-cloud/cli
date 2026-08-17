// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

// stdinPump bridges the terminal's single stdin to two consumers: readline,
// which reads the prompt off it directly, and the remote command's stdin, which
// reads through readerFor so that a ^C cancels the read rather than leaving it
// stuck. Close stops the pumping goroutine, which is what lets the terminal
// leave raw mode promptly when the shell exits.

import (
	"context"
	"io"
	"sync"
)

type stdinPump struct {
	ch     chan []byte
	mu     sync.Mutex
	buf    []byte
	err    error
	closed chan struct{}
	once   sync.Once
}

func newStdinPump(r io.Reader) *stdinPump {
	p := &stdinPump{
		ch:     make(chan []byte, 64),
		closed: make(chan struct{}),
	}
	go p.run(r)
	return p
}

func (p *stdinPump) run(r io.Reader) {
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

// next returns the next chunk of input, from whatever a previous read left over
// or else from the pumping goroutine. Cancelling ctx ends the wait; pass
// context.Background() for a wait that only ends with input or EOF.
func (p *stdinPump) next(ctx context.Context) ([]byte, error) {
	p.mu.Lock()
	if len(p.buf) > 0 {
		chunk := p.buf
		p.buf = nil
		p.mu.Unlock()
		return chunk, nil
	}
	p.mu.Unlock()

	var (
		chunk []byte
		ok    bool
	)
	select {
	case chunk, ok = <-p.ch:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
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
}

// unread puts back what a caller could not fit in its buffer.
func (p *stdinPump) unread(chunk []byte) {
	p.mu.Lock()
	p.buf = append(chunk, p.buf...)
	p.mu.Unlock()
}

// Read makes the pump readline's stdin.
func (p *stdinPump) Read(b []byte) (int, error) {
	chunk, err := p.next(context.Background())
	if err != nil {
		return 0, err
	}
	n := copy(b, chunk)
	if n < len(chunk) {
		p.unread(chunk[n:])
	}
	return n, nil
}

// readerFor is the pump as the stdin of one foreground command: reads end when
// ctx is cancelled, so a ^C doesn't leave the command waiting on input.
func (p *stdinPump) readerFor(ctx context.Context) io.Reader {
	return &pumpReader{pump: p, ctx: ctx}
}

type pumpReader struct {
	pump *stdinPump
	ctx  context.Context
}

func (r *pumpReader) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	chunk, err := r.pump.next(r.ctx)
	if err != nil {
		return 0, err
	}
	n := copy(b, chunk)
	if n < len(chunk) {
		r.pump.unread(chunk[n:])
	}
	return n, nil
}

func (p *stdinPump) Close() error {
	p.once.Do(func() { close(p.closed) })
	return nil
}
