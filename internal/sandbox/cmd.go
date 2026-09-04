// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"syscall"
	"time"

	plugin "unikraft.com/cloud/plugins/sandbox"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/x/log"
)

const (
	pollInterval    = 100 * time.Millisecond
	pollMaxInterval = 1 * time.Second
	pollMaxFailures = 3
	signalTimeout   = 5 * time.Second
)

const PluginName = plugin.PluginName

type ExitError struct {
	UUID string
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("command %s exited with status %d", e.UUID, e.Code)
}

func (e *ExitError) ExitCode() int { return e.Code }

type Target struct {
	Client   *plugin.Client
	Instance platform.Instance
	Plugin   string
	Opts     []plugin.Option
}

func (t Target) Command(ctx context.Context, name string, args ...string) *Cmd {
	return t.CommandArgs(ctx, append([]string{name}, args...))
}

func (t Target) CommandArgs(ctx context.Context, args []string) *Cmd {
	c := &Cmd{Args: args, ctx: ctx, target: t}
	if len(args) == 0 {
		c.Err = errors.New("sandbox: no command given")
	}
	return c
}

func (t Target) CommandLine(ctx context.Context, cmdline string) *Cmd {
	c := &Cmd{Cmdline: cmdline, ctx: ctx, target: t}
	if cmdline == "" {
		c.Err = errors.New("sandbox: no command given")
	}
	return c
}

type Cmd struct {
	Args    []string
	Cmdline string

	Dir string
	Env map[string]string

	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Cancel         func() error
	WaitDelay      time.Duration
	Err            error
	UUID           string
	ExitCode       int

	ctx    context.Context
	target Target

	waitCtx  context.Context
	stopWait context.CancelFunc

	stopStdin context.CancelFunc
	stdinErr  chan error

	logs   *logStream
	done   chan error
	closed bool

	waited bool
}

func (c *Cmd) commandLine() (string, error) {
	if len(c.Args) == 0 {
		if c.Cmdline == "" {
			return "", errors.New("sandbox: no command given")
		}
		return c.Cmdline, nil
	}
	if c.Cmdline != "" {
		return "", errors.New("sandbox: only one of Args and Cmdline may be given")
	}
	// HACK: this will be removed after the plugin api will be able to
	// take parsed args instead of a single string
	return Quote(c.Args)
}

func (c *Cmd) Start() error {
	if c.Err != nil {
		return c.Err
	}
	if c.UUID != "" {
		return errors.New("sandbox: command already started")
	}

	log.G(c.ctx).Trace().Msg("executing command")

	cmdline, err := c.commandLine()
	if err != nil {
		return err
	}
	c.Cmdline = cmdline

	req := plugin.RunCommandRequest{Cmd: cmdline}
	if c.Dir != "" {
		req.Cwd = &c.Dir
	}
	if len(c.Env) > 0 {
		env := c.Env
		req.Env = &env
	}

	resp, err := c.target.Client.RunCommand(c.ctx, c.target.Instance, &req, c.target.Opts...)
	if err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}
	if resp.Data == nil || resp.Data.Uuid == "" {
		return fmt.Errorf("failed to start command: the %q plugin did not report a command UUID", c.target.Plugin)
	}
	c.UUID = resp.Data.Uuid

	c.waitCtx, c.stopWait = context.WithCancel(context.WithoutCancel(c.ctx))

	c.stdinErr = make(chan error, 1)
	if c.Stdin != nil {
		feedCtx, cancelFeed := context.WithCancel(c.ctx)
		c.stopStdin = cancelFeed
		go (&stdinPump{target: c.target, uuid: c.UUID}).feed(feedCtx, c.Stdin, c.stdinErr)
	}

	c.logs = &logStream{
		target: c.target,
		uuid:   c.UUID,
		stdout: c.Stdout,
		stderr: c.Stderr,
	}

	log.G(c.ctx).Trace().
		Str("cmd", c.UUID).
		Str("cmdline", cmdline).
		Msg("waiting for command")

	c.done = make(chan error, 1)
	go c.stream()

	return nil
}

func (c *Cmd) stream() {
	ended := make(chan error, 1)
	go func() {
		_, err := c.target.Client.WaitForCommand(c.waitCtx, c.target.Instance, c.UUID, c.target.Opts...)
		ended <- err
	}()

	poll := pollInterval
	failures := 0
	timer := time.NewTimer(poll)
	defer timer.Stop()

	for {
		select {
		case <-c.waitCtx.Done():
			c.done <- c.waitCtx.Err()
			return

		case err := <-ended:
			if err != nil {
				if c.waitCtx.Err() != nil {
					c.done <- c.waitCtx.Err()
					return
				}
				c.done <- fmt.Errorf("failed waiting for command: %w", err)
				return
			}
			if err := c.logs.drain(c.waitCtx); err != nil {
				c.done <- err
				return
			}
			c.done <- c.exitStatus()
			return

		case <-timer.C:
			n, err := c.logs.drainOnce(c.waitCtx)
			if err != nil {
				if c.waitCtx.Err() != nil {
					c.done <- c.waitCtx.Err()
					return
				}
				failures++
				if failures >= pollMaxFailures {
					c.done <- err
					return
				}
				log.G(c.ctx).Debug().
					Err(err).
					Str("cmd", c.UUID).
					Int("failures", failures).
					Msg("failed to fetch logs, retrying")
				poll = min(poll*2, pollMaxInterval)
				timer.Reset(poll)
				continue
			}
			failures = 0
			if n > 0 {
				poll = pollInterval
			} else {
				poll = min(poll*2, pollMaxInterval)
			}
			timer.Reset(poll)
		}
	}
}

func (c *Cmd) exitStatus() error {
	resp, err := c.target.Client.GetCommandByUuid(c.waitCtx, c.target.Instance, c.UUID, c.target.Opts...)
	if err != nil {
		return fmt.Errorf("failed to read the exit status of command %s: %w", c.UUID, err)
	}
	if resp.Data == nil {
		return fmt.Errorf("failed to read the exit status of command %s: the %q plugin reported no state for it", c.UUID, c.target.Plugin)
	}

	c.ExitCode = int(resp.Data.Exitcode)
	log.G(c.ctx).Trace().
		Str("cmd", c.UUID).
		Int("exit_code", c.ExitCode).
		Msg("command ended")
	if c.ExitCode != 0 {
		return &ExitError{UUID: c.UUID, Code: c.ExitCode}
	}
	return nil
}

func (c *Cmd) Wait() error {
	if c.UUID == "" {
		return errors.New("sandbox: command not started")
	}
	if c.waited {
		return errors.New("sandbox: Wait was already called")
	}
	c.waited = true

	defer c.stop()
	if c.stopStdin != nil {
		defer c.stopStdin()
	}

	cancelled := c.ctx.Done()
	var expired <-chan time.Time

	for {
		select {
		case <-cancelled:
			cancelled = nil
			log.G(c.ctx).Trace().Str("cmd", c.UUID).Msg("context cancelled, interrupting command")

			if err := c.cancel(); err != nil {
				return err
			}
			if c.WaitDelay > 0 {
				delay := time.NewTimer(c.WaitDelay)
				defer delay.Stop()
				expired = delay.C
			}

		case <-expired:
			return c.interrupted()

		case stdinErr := <-c.stdinErr:
			log.G(c.ctx).Warn().Err(stdinErr).Str("cmd", c.UUID).Msg("standard input failed")
			continue

		case err := <-c.done:
			c.closed = true
			if err != nil {
				_, exited := errors.AsType[*ExitError](err)
				if c.ctx.Err() != nil && !exited {
					return c.ctx.Err()
				}
				return err
			}
			return nil
		}
	}
}

func (c *Cmd) stop() {
	c.stopWait()
	if !c.closed {
		<-c.done
		c.closed = true
	}
}

func (c *Cmd) Run() error {
	if err := c.Start(); err != nil {
		return err
	}
	return c.Wait()
}

func (c *Cmd) Signal(ctx context.Context, sig syscall.Signal) error {
	if c.UUID == "" {
		return errors.New("sandbox: command not started")
	}
	req := plugin.CommandSignalRequest{Signal: int(sig)}
	_, err := c.target.Client.SignalCommand(ctx, c.target.Instance, c.UUID, &req, c.target.Opts...)
	return err
}

func (c *Cmd) cancel() error {
	if c.Cancel != nil {
		return c.Cancel()
	}

	signalCtx, cancel := context.WithTimeout(c.waitCtx, signalTimeout)
	defer cancel()
	if err := c.Signal(signalCtx, syscall.SIGINT); err != nil {
		log.G(c.ctx).Debug().Err(err).Str("cmd", c.UUID).Msg("failed to signal remote command")
	}
	return nil
}

func (c *Cmd) interrupted() error {
	return fmt.Errorf("command %s is still running: %w", c.UUID, c.ctx.Err())
}

// HACK: this will be removed once the sandbox plugin will accept parsed args
func Quote(args []string) (string, error) {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		if strings.ContainsRune(arg, 0) {
			return "", fmt.Errorf("cannot quote command argument %q: it contains a NUL byte", arg)
		}
		quoted = append(quoted, "'"+strings.ReplaceAll(arg, "'", `'\''`)+"'")
	}
	return strings.Join(quoted, " "), nil
}
