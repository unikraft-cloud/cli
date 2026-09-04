// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package sandbox

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/docker/go-units"

	plugin "unikraft.com/cloud/plugins/sandbox"
)

const stdinChunkSize = 32 * units.KiB

// stdinPump forwards a reader to a command's standard input, a chunk per
// request, and closes that input once the reader is done.
type stdinPump struct {
	target Target
	uuid   string
	eof    *sync.Once
}

func (p *stdinPump) feed(ctx context.Context, in io.Reader, failed chan<- error) {
	report := func(err error) {
		if ctx.Err() != nil {
			return
		}
		select {
		case failed <- err:
		default:
		}
	}

	var sent uint64
	buf := make([]byte, stdinChunkSize)
	for {
		if ctx.Err() != nil {
			return
		}

		n, readErr := in.Read(buf)
		if n > 0 {
			if err := p.write(ctx, base64.StdEncoding.EncodeToString(buf[:n]), false); err != nil {
				report(fmt.Errorf("failed to send standard input to the command after %d bytes: %w", sent, err))
				return
			}
			sent += uint64(n)
		}

		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				report(fmt.Errorf("failed to read standard input after %d bytes: %w", sent, readErr))
				return
			}
			if ctx.Err() != nil {
				return
			}
			if err := p.close(ctx); err != nil {
				report(fmt.Errorf("failed to close the command's standard input after %d bytes: %w", sent, err))
			}
			return
		}
	}
}

func (p *stdinPump) close(ctx context.Context) error {
	var err error
	p.eof.Do(func() { err = p.write(ctx, "", true) })
	return err
}

func (p *stdinPump) write(ctx context.Context, data string, eof bool) error {
	req := plugin.CommandStdinRequest{Data: data, Eof: &eof}
	_, err := p.target.Client.WriteCommandStdin(ctx, p.target.Instance, p.uuid, &req, p.target.Opts...)
	return cmp.Or(p.target.notRunning(err), err)
}
