// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package tunnel

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"

	"unikraft.com/x/log"
)

// tunnelConnection represents an accepted local connection being relayed to a
// remote host through the tunnel service.
type tunnelConnection struct {
	relay *tunnelRelay
	conn  net.Conn
}

// handle relays data between the local connection and the remote host.
func (c *tunnelConnection) handle(ctx context.Context, auth []byte, instance, instanceRaw string) {
	defer func() {
		if r := recover(); r != nil {
			log.G(ctx).Error().Interface("panic", r).Str("for", instanceRaw).Msg("recovered from panic while handling tunnel connection")
		}
	}()
	defer func() {
		c.conn.Close()
		log.G(ctx).Info().Str("for", instanceRaw).Msg("closed connection")
	}()

	rc, err := c.relay.dialRemote(ctx)
	if err != nil {
		log.G(ctx).Error().Err(err).Msg("failed to connect to remote host")
		return
	}
	defer rc.Close()

	// Force both ends closed as soon as ctx is cancelled (e.g. on Ctrl-C), so
	// this connection can't block the io.Copy calls below indefinitely and
	// Run/Close can proceed once every in-flight connection has drained.
	stop := context.AfterFunc(ctx, func() {
		c.conn.Close()
		rc.Close()
	})
	defer stop()

	log.G(ctx).Debug().
		Str("for", c.conn.RemoteAddr().String()).
		Str("from", rc.LocalAddr().String()).
		Str("to", rc.RemoteAddr().String()).
		Msg("opened connection")
	log.G(ctx).Info().Str("to", instanceRaw).Msg("accepted connection")

	_ = rc.SetDeadline(tunnelNoNetTimeout)
	_ = c.conn.SetDeadline(tunnelNoNetTimeout)

	defer func() {
		_ = c.conn.SetDeadline(tunnelImmediateNetCancel)
	}()

	if len(auth) > 0 {
		_, err = rc.Write(auth)
		if err != nil {
			log.G(ctx).Error().Err(err).Msg("failed to write auth to remote host")
			return
		}

		statusRaw := bytes.NewBuffer(nil)
		n, err := io.CopyN(statusRaw, rc, 2)
		if err != nil {
			log.G(ctx).Error().Err(err).Msg("failed to read auth status from remote host")
			return
		}
		if n != 2 {
			log.G(ctx).Error().Msg("invalid auth status from remote host")
			return
		}

		var status int16
		if err = binary.Read(statusRaw, binary.LittleEndian, &status); err != nil {
			log.G(ctx).Error().Err(err).Msg("failed to parse auth status from remote host")
			return
		}

		if status == 0 {
			log.G(ctx).Error().Msg("no available connections to remote host, try again later")
			return
		} else if status < 0 {
			log.G(ctx).Error().Msgf("internal tunnel error (C=%d), to view logs run:", status)
			log.G(ctx).Error().Msgf("    unikraft instance logs %s\n", instance)
			return
		}
	}

	writerDone := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.G(ctx).Error().Interface("panic", r).Msg("recovered from panic while copying tunnel data")
			}
		}()
		defer func() {
			_ = rc.SetDeadline(tunnelImmediateNetCancel)
			close(writerDone)
		}()
		_, werr := io.Copy(rc, c.conn)
		if werr != nil && !isNetClosedError(werr) && !isNetTimeoutError(werr) {
			log.G(ctx).Error().Err(werr).Msg("failed to copy data from client to remote host")
		}
	}()

	_, err = io.Copy(c.conn, rc)
	if err != nil {
		if !isNetTimeoutError(err) && !isNetClosedError(err) {
			log.G(ctx).Error().Err(err).Msg("failed to copy data from remote host to client")
		}
	} else {
		// Remote closed the connection cleanly; return to close our side.
		return
	}

	<-writerDone
}

func isNetTimeoutError(err error) bool {
	neterr, ok := errors.AsType[net.Error](err)
	return ok && neterr.Timeout()
}

func isNetClosedError(err error) bool {
	return errors.Is(err, net.ErrClosed) ||
		strings.Contains(err.Error(), "connection reset by peer")
}
