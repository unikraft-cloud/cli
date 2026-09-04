// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package sandbox

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/docker/go-units"

	plugin "unikraft.com/cloud/plugins/sandbox"
)

const (
	stdinChunkSize  = 32 * units.KiB
	stdinEOFTimeout = 5 * time.Second
)

// stdinPump forwards a reader to a command's standard input, a chunk per
// request, and closes that input once the reader is done.
type stdinPump struct {
	target Target
	uuid   string
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
				p.close(ctx)
				return
			}
			sent += uint64(n)
		}

		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				report(fmt.Errorf("failed to read standard input after %d bytes: %w", sent, readErr))
				p.close(ctx)
				return
			}
			if ctx.Err() != nil {
				return
			}
			if err := p.write(ctx, "", true); err != nil {
				report(fmt.Errorf("failed to close the command's standard input after %d bytes: %w", sent, err))
			}
			return
		}
	}
}

func (p *stdinPump) close(ctx context.Context) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), stdinEOFTimeout)
	defer cancel()
	_ = p.write(ctx, "", true)
}

func (p *stdinPump) write(ctx context.Context, data string, eof bool) error {
	req := plugin.CommandStdinRequest{Data: data, Eof: &eof}
	_, err := p.target.Client.WriteCommandStdin(ctx, p.target.Instance, p.uuid, &req, p.target.Opts...)
	return err
}
