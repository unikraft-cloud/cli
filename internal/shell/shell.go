// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/interp"
	"mvdan.cc/sh/v3/syntax"

	"github.com/reeflective/readline"

	"unikraft.com/x/log"
)

const (
	instanceRestartTimeout = 90 * time.Second
	instanceRestartPoll    = time.Second

	instanceProbeTimeout  = 3 * time.Second
	instanceProbeAttempts = 2
)

const environProbe = `env; printf 'HOME=%s\nPATH=%s\nUID=%s\nEUID=%s\nGID=%s\n' ` +
	`"${HOME:-/}" "$PATH" "$(id -u 2>/dev/null || echo 0)" ` +
	`"$(id -u 2>/dev/null || echo 0)" "$(id -g 2>/dev/null || echo 0)"`

var (
	errNotATerminal = errors.New("the shell needs a terminal; use -c '<command line>' to run commands without one")

	dirReset       = mustParse("cd /")
	interruptReset = mustParse(fmt.Sprintf("(exit %d)", StatusInterrupted))

	experimentalBanner = []string{
		"this shell is experimental",
		"no terminal, so vim, top and less will not run",
		"no job control, so no ctrl-z, bg or fg",
	}
)

// Streams are the three standard streams a single command is wired to.
type Streams struct {
	In       io.Reader
	Out, Err io.Writer
}

// console is where the session writes, serialised so two commands cannot interleave a line.
type console struct {
	Out, Err io.Writer
}

// SuspendFunc lends the shell a signal for as long as it holds the prompt.
type SuspendFunc func(ctx context.Context, sig ...os.Signal) (restore func())

// Transport is how the shell reaches the instance.
type Transport interface {
	Exec(ctx context.Context, streams Streams, dir string, env map[string]string, args []string) (int, error)
}

// Builtins handles the lines that open with ":".
type Builtins interface {
	Run(ctx context.Context, streams Streams, args []string) (int, error)
	Names() []string
}

// Restarts reports whether a builtin takes the instance down and up again.
type Restarts interface {
	Restarts(args []string) bool
}

type Config struct {
	Instance  string
	Transport Transport
	Builtins  Builtins

	Dir     string
	Env     map[string]string
	Command string

	SuspendSignals SuspendFunc
}

type session struct {
	cfg     Config
	console console

	runner *interp.Runner

	interactive bool

	commands []string

	tty *os.File

	editor *prompt

	restarted atomic.Bool
}

func (s *session) dir() string {
	return s.runner.Dir
}

func Run(ctx context.Context, cfg Config, streams Streams) error {
	if cfg.Transport == nil {
		return fmt.Errorf("no transport to the instance")
	}
	if cfg.Dir == "" {
		cfg.Dir = "/"
	}

	s := &session{cfg: cfg}
	if f, ok := streams.In.(*os.File); ok && term.IsTerminal(f.Fd()) {
		s.tty = f
	}
	s.interactive = cfg.Command == "" && s.tty != nil && isTTY(streams.Out)

	var terminal sync.Mutex
	s.console = console{
		Out: &lockedWriter{mu: &terminal, w: streams.Out},
		Err: &lockedWriter{mu: &terminal, w: streams.Err},
	}

	var cmdStdin io.Reader
	if s.tty == nil {
		cmdStdin = streams.In
	}

	env, err := s.environ(ctx)
	if err != nil {
		return err
	}

	runner, err := interp.New(
		interp.StdIO(cmdStdin, s.console.Out, s.console.Err),
		interp.Env(expand.ListEnviron(env...)),
		interp.Interactive(s.interactive),
		interp.ExecHandlers(s.route),
		interp.StatHandler(s.statHandler),
		interp.AccessHandler(s.accessHandler),
		interp.ReadDirHandler2(s.readDirHandler),
		interp.OpenHandler(s.open),
	)
	if err != nil {
		return err
	}

	runner.Dir = cfg.Dir
	runner.Reset()
	s.runner = runner

	switch {
	case cfg.Command != "":
		return s.runSource(ctx, strings.NewReader(cfg.Command))
	case s.interactive:
		return s.runInteractive(ctx)
	default:
		return errNotATerminal
	}
}

func (s *session) runSource(ctx context.Context, src io.Reader) error {
	prog, err := syntax.NewParser().Parse(src, "")
	if err != nil {
		return err
	}
	return dropExitStatus(s.runner.Run(ctx, prog))
}

func (s *session) runInteractive(ctx context.Context) error {
	sigint, stop := captureInterrupts(ctx, s.cfg.SuspendSignals)
	defer stop()
	defer s.plainKeys()()

	fmt.Fprintln(s.console.Err, bannerStyle.Render(strings.Join(experimentalBanner, "\n")))

	s.editor = s.newPrompt(ctx)

	parser := syntax.NewParser()

	for {
		line, err := s.editor.readLine()
		switch {
		case errors.Is(err, readline.ErrInterrupt):
			fmt.Fprintln(s.console.Out, hintStyle.Render("^C"))
			continue
		case errors.Is(err, io.EOF):
			fmt.Fprintln(s.console.Out)
			return nil
		case err != nil:
			return err
		}

		prog, err := parser.Parse(strings.NewReader(line+"\n"), "")
		if err != nil {
			fmt.Fprintln(s.console.Err, errorStyle.Render(err.Error()))
			continue
		}

		for _, stmt := range prog.Stmts {
			interrupted, err := s.runStmt(ctx, sigint, stmt)
			if err != nil {
				fmt.Fprintln(s.console.Err, errorStyle.Render(err.Error()))
			}
			if s.restarted.Swap(false) {
				s.relocate(ctx)
			}
			if interrupted {
				break
			}
			if s.runner.Exited() {
				return nil
			}
		}
	}
}

func (s *session) runStmt(ctx context.Context, sigint <-chan os.Signal, stmt *syntax.Stmt) (bool, error) {
	for drained := false; !drained; {
		select {
		case <-sigint:
		default:
			drained = true
		}
	}

	stmtCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		interrupted atomic.Bool
		wg          sync.WaitGroup
		done        = make(chan struct{})
	)

	stop := sync.OnceFunc(func() {
		close(done)
		wg.Wait()
	})
	defer stop()

	wg.Go(func() {
		for {
			select {
			case <-sigint:
				interrupted.Store(true)
				cancel()
			case <-done:
				return
			}
		}
	})

	err := dropExitStatus(s.runner.Run(stmtCtx, stmt))
	stop()

	if !interrupted.Load() {
		return false, err
	}
	s.clearInterrupt(ctx)
	return true, nil
}

func (s *session) settle(ctx context.Context) bool {
	s.commands = nil

	fmt.Fprintln(s.console.Err, hintStyle.Render("the instance is restarting; waiting for it"))

	waitCtx, cancel := context.WithTimeout(ctx, instanceRestartTimeout)
	defer cancel()

	for !s.answers(waitCtx) {
		switch {
		case ctx.Err() != nil:
			fmt.Fprintln(s.console.Err, hintStyle.Render("stopped waiting for the instance"))
			return false
		case waitCtx.Err() != nil:
			fmt.Fprintln(s.console.Err, errorStyle.Render(
				`the instance has not come back; try ":get" or reconnect`))
			return true
		}
		select {
		case <-time.After(instanceRestartPoll):
		case <-waitCtx.Done():
		}
	}
	return true
}

func (s *session) relocate(ctx context.Context) {
	if s.dir() == probeDir {
		return
	}
	if _, err := s.stat(ctx, probeDir, s.dir(), true); err == nil {
		return
	}

	fmt.Fprintln(s.console.Err, hintStyle.Render(
		"the working directory did not survive the restart; moved to "+probeDir))
	_ = s.runner.Run(ctx, dirReset)
}

func (s *session) answers(ctx context.Context) bool {
	probeCtx, cancel := context.WithTimeout(ctx, instanceProbeTimeout)
	defer cancel()

	_, err := s.script(probeCtx, `:`)
	return err == nil
}

func mustParse(src string) *syntax.File {
	prog, err := syntax.NewParser().Parse(strings.NewReader(src), "")
	if err != nil {
		panic(err)
	}
	return prog
}

func (s *session) clearInterrupt(ctx context.Context) {
	_ = s.runner.Run(ctx, interruptReset)
}

func (s *session) prompt(continuation bool) string {
	if continuation {
		return continuationStyle.Render("> ")
	}
	return promptStyle.Render(s.cfg.Instance) +
		promptDirStyle.Render(":"+s.dir()) +
		promptStyle.Render("$ ")
}

func (s *session) plainKeys() func() {
	fmt.Fprint(s.console.Out, ansi.DisableKittyKeyboard, ansi.ResetModifyOtherKeys)
	return func() {
		fmt.Fprint(s.console.Out, ansi.PopKittyKeyboard(1), ansi.ResetModifyOtherKeys)
	}
}

func captureInterrupts(ctx context.Context, suspend SuspendFunc) (<-chan os.Signal, func()) {
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, syscall.SIGINT)

	restore := func() {}
	if suspend != nil {
		restore = suspend(ctx, syscall.SIGINT)
	}

	return ch, func() {
		signal.Stop(ch)
		restore()
	}
}

func unwrap(w io.Writer) io.Writer {
	for {
		wrapped, ok := w.(*colorprofile.Writer)
		if !ok {
			return w
		}
		w = wrapped.Forward
	}
}

func isTTY(w io.Writer) bool {
	fd, ok := unwrap(w).(interface{ Fd() uintptr })
	return ok && term.IsTerminal(fd.Fd())
}

func dropExitStatus(err error) error {
	if _, ok := errors.AsType[interp.ExitStatus](err); ok {
		return nil
	}
	return err
}

func (s *session) environ(ctx context.Context) ([]string, error) {
	out, err := s.probe(ctx)
	if err != nil {
		log.G(ctx).Debug().Err(err).Msg("could not read the instance environment")

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("could not reach %s: %w", cmp.Or(s.cfg.Instance, "the instance"), err)
	}

	vars := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
		if name, value, ok := strings.Cut(line, "="); ok && isEnvName(name) {
			vars[name] = value
		}
	}
	maps.Copy(vars, s.cfg.Env)

	for name, fallback := range map[string]string{
		"HOME": "/", "PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"UID": "0", "EUID": "0", "GID": "0",
	} {
		if vars[name] == "" {
			vars[name] = fallback
		}
	}

	env := make([]string, 0, len(vars))
	for name, value := range vars {
		env = append(env, name+"="+value)
	}
	slices.Sort(env)
	return env, nil
}

func (s *session) probe(ctx context.Context) (string, error) {
	var err error
	for range instanceProbeAttempts {
		var out bytes.Buffer

		probeCtx, cancel := context.WithTimeout(ctx, instanceProbeTimeout)
		probeCtx = Detached(probeCtx)

		_, err = s.cfg.Transport.Exec(probeCtx, Streams{Out: &out, Err: io.Discard}, s.cfg.Dir, nil,
			[]string{"sh", "-c", environProbe})
		cancel()

		switch {
		case err == nil:
			return out.String(), nil
		case ctx.Err() != nil:
			return "", err
		}
	}
	return "", err
}

func isEnvName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

func (s *session) builtinNames() []string {
	names := slices.Clone(sessionBuiltinNames)
	if s.cfg.Builtins != nil {
		names = append(names, s.cfg.Builtins.Names()...)
	}
	slices.Sort(names)
	return names
}

// lockedWriter serialises writes from commands running at the same time.
type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
