// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").

package cmd

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/docker/go-units"

	"unikraft.com/cloud/plugins/sandbox"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/value"
	xio "unikraft.com/cli/internal/x/io"
)

type ExecOpts struct {
	Cmd []string `arg:"" name:"command" help:"Command to pass to the instance." placeholder:"cmd"`

	Plugin string   `name:"plugin" help:"Plugin name from the instance to run the command onto"`
	Dir    string   `name:"dir" help:"Directory to execute the command from"`
	Env    []string `name:"env" short:"e" sep:"none" help:"Environment variable." placeholder:"<key>=<value>" example:"DEBUG=true"`
}

func (o ExecOpts) env() (map[string]string, error) {
	env, err := value.Parse[map[string]string](o.Env)
	if err != nil {
		return nil, fmt.Errorf("parsing --env: %w", err)
	}
	return env, nil
}

type ExecSandboxInstanceCmd struct {
	Target string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instances to run the command on."`

	ExecOpts
}

func (cmd ExecSandboxInstanceCmd) Help() string {
	return "The environment given with --env is the whole environment the command sees. Standard input is forwarded to the command only when it is redirected from a file or a pipe; run from a terminal, the command sees an empty standard input and cannot be typed at."
}

func (cmd ExecSandboxInstanceCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Run a command on a sandbox instance",
			Commands: []string{
				"unikraft instance exec my-instance --plugin sandbox -- echo hello",
			},
		},
		{
			Description: "Run a command in a specific working directory",
			Commands: []string{
				"unikraft instance exec my-instance --plugin sandbox --dir /var/lib/app -- ls -la",
			},
		},
		{
			Description: "Run a command with environment variables set",
			Commands: []string{
				"unikraft instance exec my-instance --plugin sandbox -e DEBUG=true -e PORT=8080 -- ./start.sh",
			},
		},
	}
}

func (c *ExecSandboxInstanceCmd) Run(ctx context.Context, stdio config.Stdio, rsb *resource.Sandbox) error {
	target, err := resolveSandboxTarget(ctx, rsb, c.Target, c.Plugin)
	if err != nil {
		return err
	}

	in := stdio.Stdin
	fd, isFile := in.(interface{ Fd() uintptr })
	if in == nil || (isFile && term.IsTerminal(fd.Fd())) {
		in = strings.NewReader("")
	}

	return target.exec(ctx, xio.Unwrap(stdio.Stdout), in, c.ExecOpts)
}

type sandboxTarget struct {
	client   *sandbox.Client
	instance platform.Instance
	opts     []sandbox.Option

	plugin string
}

func resolveSandboxTarget(ctx context.Context, rsb *resource.Sandbox, target, plugin string) (sandboxTarget, error) {
	if plugin == "" {
		plugin = sandbox.PluginName
	}

	key := multimetro.ParseKey(target)

	gettable := rsb.WrapGettable(Instance{})
	resources, err := gettable.Get(ctx, []string{key.String()})
	if err != nil {
		return sandboxTarget{}, err
	}
	if len(resources) == 0 {
		return sandboxTarget{}, fmt.Errorf("instance %q not found", target)
	}
	if len(resources) > 1 {
		var keys []string
		for _, res := range resources {
			keys = append(keys, res.Key().String())
		}
		return sandboxTarget{}, fmt.Errorf("ambiguous instance: %s (found %v)", target, keys)
	}

	instance, ok := resources[0].(Instance)
	if !ok {
		return sandboxTarget{}, fmt.Errorf("%q is not an instance", target)
	}

	if !instance.State.IsRunning() {
		return sandboxTarget{}, fmt.Errorf("instance %q is not running (state: %s); start it with \"unikraft instance start %s\"", instance.Name, string(instance.State), instance.Name)
	}

	var loaded []string
	hasPlugin := false
	for _, p := range instance.Plugins {
		if p == nil || p.Name == "" {
			continue
		}
		if p.Name == plugin {
			hasPlugin = true
			break
		}
		loaded = append(loaded, p.Name)
	}
	if !hasPlugin {
		if len(loaded) == 0 {
			return sandboxTarget{}, fmt.Errorf("instance %q has no plugins loaded, so nothing answers to %q", instance.Name, plugin)
		}
		return sandboxTarget{}, fmt.Errorf("instance %q has no plugin named %q; it has: %s", instance.Name, plugin, strings.Join(loaded, ", "))
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return sandboxTarget{}, err
	}

	key = instance.Key().(multimetro.Key)
	return group.CollectMetro(ctx, g, key.Metro, func(ctx context.Context, c multimetro.MetroClient) (sandboxTarget, error) {
		sb := c.Sandbox(plugin)
		return sandboxTarget{
			client:   sb.Client,
			instance: multimetro.SandboxInstance(key),
			opts:     sb.Opts,
			plugin:   plugin,
		}, nil
	})
}

const (
	sandboxLogPollInterval    = 100 * time.Millisecond
	sandboxLogPollMaxInterval = 1 * time.Second
)

func shQuote(arg string) string {
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

func buildExecCommand(cmdArgs []string) (string, error) {
	quoted := make([]string, 0, len(cmdArgs))
	for _, arg := range cmdArgs {
		if strings.ContainsRune(arg, 0) {
			return "", fmt.Errorf("cannot quote command argument %q: it contains a NUL byte", arg)
		}
		quoted = append(quoted, shQuote(arg))
	}
	return strings.Join(quoted, " "), nil
}

func (t sandboxTarget) exec(ctx context.Context, out io.Writer, in io.Reader, opts ExecOpts) error {
	log.G(ctx).Trace().Msg("executing command")

	cmdline, err := buildExecCommand(opts.Cmd)
	if err != nil {
		return err
	}

	req := sandbox.RunCommandRequest{
		Cmd: cmdline,
	}
	if opts.Dir != "" {
		req.Cwd = &opts.Dir
	}
	env, err := opts.env()
	if err != nil {
		return err
	}
	if len(env) > 0 {
		req.Env = &env
	}

	execResp, err := t.client.RunCommand(ctx, t.instance, &req, t.opts...)
	if err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}
	if execResp.Data == nil || execResp.Data.Uuid == "" {
		return fmt.Errorf("failed to start command: the %q plugin did not report a command UUID", t.plugin)
	}
	cmdUUID := execResp.Data.Uuid

	finished := false
	defer func() {
		if finished {
			return
		}
		signalCtx, cancelSignal := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancelSignal()
		if _, sigErr := t.client.SignalCommand(signalCtx, t.instance, cmdUUID, &sandbox.CommandSignalRequest{
			Signal: int(syscall.SIGINT),
		}, t.opts...); sigErr != nil {
			log.G(ctx).Debug().Err(sigErr).Str("cmd", cmdUUID).Msg("failed to signal remote command")
		}
	}()

	if in != nil {
		feedCtx, cancelFeed := context.WithCancel(ctx)
		defer cancelFeed()
		go t.feedStdin(feedCtx, cmdUUID, in)
	}

	logs := &commandLogs{target: t, cmdUUID: cmdUUID, out: out}

	log.G(ctx).Trace().
		Str("cmd", cmdUUID).
		Str("cmdline", cmdline).
		Msg("waiting for command")

	done := make(chan error, 1)
	go func() {
		_, err := t.client.WaitForCommand(ctx, t.instance, cmdUUID, t.opts...)
		done <- err
	}()

	poll := sandboxLogPollInterval

	for {
		select {
		case <-ctx.Done():
			log.G(ctx).Trace().Str("cmd", cmdUUID).Msg("context cancelled, exiting wait")
			return ctx.Err()

		case waitErr := <-done:
			if waitErr != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return fmt.Errorf("failed waiting for command: %w", waitErr)
			}

			if err := logs.drain(ctx); err != nil {
				return err
			}

			inspectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			inspectResp, err := t.client.GetCommandByUuid(inspectCtx, t.instance, cmdUUID, t.opts...)
			cancel()
			if err != nil {
				return fmt.Errorf("failed to inspect command: %w", err)
			}
			if inspectResp.Data != nil {
				log.G(ctx).Trace().
					Str("cmd", cmdUUID).
					Int32("exitcode", inspectResp.Data.Exitcode).
					Msg("command finished")
			}
			finished = true
			return nil

		case <-time.After(poll):
			n, err := logs.fetchAndPrint(ctx)
			if err != nil {
				return err
			}
			if n > 0 {
				poll = sandboxLogPollInterval
			} else {
				poll = min(poll*2, sandboxLogPollMaxInterval)
			}
		}
	}
}

type commandLogs struct {
	target  sandboxTarget
	cmdUUID string
	out     io.Writer

	stdoutOffset, stderrOffset       uint64
	stdoutAvailable, stderrAvailable uint64
}

func (l *commandLogs) pending() bool {
	return l.stdoutAvailable > l.stdoutOffset || l.stderrAvailable > l.stderrOffset
}

func (l *commandLogs) drain(ctx context.Context) error {
	for {
		n, err := l.fetchAndPrint(ctx)
		if err != nil {
			return err
		}
		if n == 0 || !l.pending() {
			return nil
		}
	}
}

func (l *commandLogs) fetchAndPrint(ctx context.Context) (uint64, error) {
	log.G(ctx).Trace().
		Str("cmd", l.cmdUUID).
		Uint64("stdout_offset", l.stdoutOffset).
		Uint64("stderr_offset", l.stderrOffset).
		Msg("fetching logs")

	req := sandbox.CommandLogsRequest{
		Stdout: sandbox.CommandLogsRange{Offset: l.stdoutOffset},
		Stderr: sandbox.CommandLogsRange{Offset: l.stderrOffset},
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, err := l.target.client.GetCommandLogs(fetchCtx, l.target.instance, l.cmdUUID, &req, l.target.opts...)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch logs: %w", err)
	}
	if resp.Data == nil {
		return 0, nil
	}

	log.G(ctx).Trace().
		Str("cmd", l.cmdUUID).
		Uint64("stdout_offset", l.stdoutOffset).
		Uint64("stderr_offset", l.stderrOffset).
		Bool("has_stdout", resp.Data.Stdout != "").
		Bool("has_stderr", resp.Data.Stderr != "").
		Uint64("stdout_available", resp.Data.StdoutAvailable).
		Uint64("stderr_available", resp.Data.StderrAvailable).
		Msg("polled command logs")

	l.stdoutAvailable = resp.Data.StdoutAvailable
	l.stderrAvailable = resp.Data.StderrAvailable

	var written uint64
	for _, stream := range []struct {
		data   string
		offset *uint64
	}{
		{resp.Data.Stdout, &l.stdoutOffset},
		{resp.Data.Stderr, &l.stderrOffset},
	} {
		if stream.data == "" {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(stream.data)
		if err != nil {
			return written, fmt.Errorf("decoding command output: %w", err)
		}
		if _, err := l.out.Write(decoded); err != nil {
			return written, fmt.Errorf("writing command output: %w", err)
		}
		*stream.offset += uint64(len(decoded))
		written += uint64(len(decoded))
	}
	return written, nil
}

// sandboxStdinChunkSize is the maximum number of bytes read from the local
// standard input per iteration, and so the most that a single stdin request
// carries to the command once base64-encoded.
const sandboxStdinChunkSize = 32 * 1024 // 32 KiB

func (t sandboxTarget) feedStdin(ctx context.Context, cmdUUID string, in io.Reader) {
	write := func(data string, eof bool) error {
		req := sandbox.CommandStdinRequest{Data: data, Eof: &eof}
		_, err := t.client.WriteCommandStdin(ctx, t.instance, cmdUUID, &req, t.opts...)
		return err
	}

	closeStdin := func() {
		if ctx.Err() != nil {
			return
		}
		if err := write("", true); err != nil {
			log.G(ctx).Warn().Err(err).Str("cmd", cmdUUID).Msg("failed to close the command's standard input")
		}
	}

	buf := make([]byte, sandboxStdinChunkSize)
	for {
		if ctx.Err() != nil {
			return
		}

		n, readErr := in.Read(buf)
		if n > 0 {
			if err := write(base64.StdEncoding.EncodeToString(buf[:n]), false); err != nil {
				if ctx.Err() == nil {
					log.G(ctx).Warn().Err(err).Str("cmd", cmdUUID).Msg("failed to send standard input to the command")
				}
				closeStdin()
				return
			}
		}

		if readErr != nil {
			if !errors.Is(readErr, io.EOF) && ctx.Err() == nil {
				log.G(ctx).Warn().Err(readErr).Str("cmd", cmdUUID).Msg("failed to read standard input; the command sees it truncated")
			}
			closeStdin()
			return
		}
	}
}

type WriteSandboxInstanceCmd struct {
	Target string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instance to write the file to."`
	Local  string `arg:"" name:"local" help:"Local file path to read from."`
	Remote string `arg:"" name:"remote" help:"Remote destination path on the instance."`

	Plugin  string `name:"plugin" help:"Plugin name from the instance to write the file to."`
	Append  bool   `name:"append" help:"Append to the remote file instead of overwriting it."`
	Parents bool   `name:"parents" short:"p" help:"Create parent directories on the remote path if they don't already exist."`
}

func (cmd WriteSandboxInstanceCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Write a local file to a sandbox instance",
			Commands: []string{
				"unikraft instance write my-instance ./config.json /etc/app/config.json",
			},
		},
		{
			Description: "Write a file, creating parent directories as needed",
			Commands: []string{
				"unikraft instance write my-instance ./data.bin /var/lib/app/data.bin --parents",
			},
		},
	}
}

func (c *WriteSandboxInstanceCmd) Run(ctx context.Context, stdio config.Stdio, rsb *resource.Sandbox) error {
	info, err := os.Stat(c.Local)
	if err != nil {
		return fmt.Errorf("reading local file %q: %w", c.Local, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%q is a directory: only single files can be written", c.Local)
	}

	target, err := resolveSandboxTarget(ctx, rsb, c.Target, c.Plugin)
	if err != nil {
		return err
	}

	opts := uploadOpts{
		local:      c.Local,
		remote:     c.Remote,
		appendFile: c.Append,
		parents:    c.Parents,
	}

	remote, err := target.upload(ctx, opts)
	if err != nil {
		return err
	}

	log.G(ctx).Info().
		Str("source", c.Local).
		Str("remote", remote).
		Msg("file written")
	return nil
}

const sandboxMaxFileSize = 32 * units.MiB

func checkSandboxFileSize(name string, size int64) error {
	if size > sandboxMaxFileSize {
		return fmt.Errorf("%q is %d bytes, over the %s (%d bytes) limit for a single transfer: split it, or stream it with \"unikraft instance exec\"",
			name, size, units.BytesSize(sandboxMaxFileSize), int64(sandboxMaxFileSize))
	}
	return nil
}

type uploadOpts struct {
	local      string
	remote     string
	appendFile bool
	parents    bool
}

func (t sandboxTarget) upload(ctx context.Context, opts uploadOpts) (string, error) {
	info, err := os.Stat(opts.local)
	if err != nil {
		return "", fmt.Errorf("reading local file %q: %w", opts.local, err)
	}
	if err := checkSandboxFileSize(opts.local, info.Size()); err != nil {
		return "", err
	}

	data, err := os.ReadFile(opts.local)
	if err != nil {
		return "", fmt.Errorf("reading local file %q: %w", opts.local, err)
	}

	filename := filepath.Base(opts.local)

	if opts.appendFile {
		remote := opts.remote
		if strings.HasSuffix(remote, "/") {
			remote = path.Join(remote, filename)
		}
		if opts.parents {
			if err := t.mkdir(ctx, path.Dir(remote), true); err != nil {
				return "", fmt.Errorf("creating parent directories: %w", err)
			}
		}
		if err := t.writeFile(ctx, remote, data, true); err != nil {
			if !strings.Contains(err.Error(), "Is a directory") {
				return "", err
			}
			remote = path.Join(remote, filename)
			if err := t.writeFile(ctx, remote, data, true); err != nil {
				return "", err
			}
		}
		return remote, nil
	}

	if err := t.uploadFile(ctx, opts.remote, filename, data, opts.parents); err != nil {
		return "", err
	}

	remote := opts.remote
	if strings.HasSuffix(remote, "/") {
		remote = path.Join(remote, filename)
	}
	return remote, nil
}

func (t sandboxTarget) uploadFile(ctx context.Context, remotePath, filename string, data []byte, parents bool) error {
	log.G(ctx).Trace().Msg("uploading file")

	req := sandbox.UploadFileRequest{
		Path:     remotePath,
		Filename: filename,
		Parents:  parents,
		Encoding: sandbox.FileEncodingBase64,
		Data:     base64.StdEncoding.EncodeToString(data),
	}
	if _, err := t.client.UploadFile(ctx, t.instance, &req, t.opts...); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}

func (t sandboxTarget) writeFile(ctx context.Context, remotePath string, data []byte, appendFile bool) error {
	log.G(ctx).Trace().Msg("writing file")

	req := sandbox.WriteFileRequest{
		Path:     remotePath,
		Append:   appendFile,
		Encoding: sandbox.FileEncodingBase64,
		Data:     base64.StdEncoding.EncodeToString(data),
	}
	if _, err := t.client.WriteFile(ctx, t.instance, &req, t.opts...); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}

type ReadSandboxInstanceCmd struct {
	Target string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instance to read the file from."`
	Remote string `arg:"" name:"remote" help:"Remote file path to read."`
	Local  string `arg:"" name:"local" optional:"" help:"Local destination path to write the file to. Defaults to the remote file's base name."`

	Plugin  string `name:"plugin" help:"Plugin name from the instance to read the file from."`
	Force   bool   `name:"force" help:"Overwrite the local file if it already exists."`
	Parents bool   `name:"parents" short:"p" help:"Create parent directories of the local path if they don't already exist."`
}

func (cmd ReadSandboxInstanceCmd) Help() string {
	return "The file's permissions are not carried across: it is created locally with the usual permissions for a new file."
}

func (cmd ReadSandboxInstanceCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Read a file from a sandbox instance",
			Commands: []string{
				"unikraft instance read my-instance /etc/app/config.json ./config.json",
			},
		},
		{
			Description: "Read a file into the current directory",
			Commands: []string{
				"unikraft instance read my-instance /var/log/app.log",
			},
		},
	}
}

func (c *ReadSandboxInstanceCmd) Run(ctx context.Context, stdio config.Stdio, rsb *resource.Sandbox) error {
	target, err := resolveSandboxTarget(ctx, rsb, c.Target, c.Plugin)
	if err != nil {
		return err
	}

	opts := downloadOpts{
		remote:  c.Remote,
		local:   c.Local,
		force:   c.Force,
		parents: c.Parents,
	}

	local, size, err := target.download(ctx, opts)
	if err != nil {
		return err
	}

	log.G(ctx).Info().
		Str("remote", c.Remote).
		Str("local", local).
		Int("size", size).
		Msg("file read")
	return nil
}

type downloadOpts struct {
	remote  string
	local   string
	force   bool
	parents bool
}

func (t sandboxTarget) download(ctx context.Context, opts downloadOpts) (string, int, error) {
	data, err := t.readFile(ctx, opts.remote)
	if err != nil {
		return "", 0, err
	}
	local := opts.local
	if local == "" {
		local = path.Base(opts.remote)
	} else if info, err := os.Stat(local); err == nil && info.IsDir() {
		local = filepath.Join(local, path.Base(opts.remote))
	} else if strings.HasSuffix(local, string(os.PathSeparator)) {
		local = filepath.Join(local, path.Base(opts.remote))
	}

	if opts.parents {
		if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
			return "", 0, fmt.Errorf("creating local parent directories for %q: %w", local, err)
		}
	}

	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if opts.force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC | syscall.O_NOFOLLOW
	}
	f, err := os.OpenFile(local, flags, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", 0, fmt.Errorf("local file %q already exists (use --force to overwrite)", local)
		}
		if errors.Is(err, syscall.ELOOP) {
			return "", 0, fmt.Errorf("local path %q is a symbolic link; refusing to write through it", local)
		}
		return "", 0, fmt.Errorf("writing local file %q: %w", local, err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return "", 0, fmt.Errorf("writing local file %q: %w", local, err)
	}
	if err := f.Close(); err != nil {
		return "", 0, fmt.Errorf("writing local file %q: %w", local, err)
	}

	return local, len(data), nil
}

func (t sandboxTarget) readFile(ctx context.Context, remotePath string) ([]byte, error) {
	log.G(ctx).Trace().Msg("reading file")

	req := sandbox.ReadFileRequest{
		Path: remotePath,
	}
	resp, err := t.client.ReadFile(ctx, t.instance, &req, t.opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("failed to read file: the %q plugin returned no contents", t.plugin)
	}

	contents, err := base64.StdEncoding.DecodeString(resp.Data.Contents)
	if err != nil {
		return nil, fmt.Errorf("decoding file contents: %w", err)
	}
	return contents, nil
}

const copyPathSeparator = ":"

func parseCopyPath(spec string) (target, filePath string) {
	i := strings.Index(spec, copyPathSeparator)
	if i < 0 {
		return "", spec
	}

	head := spec[:i+len(copyPathSeparator)]
	for _, prefix := range []string{multimetro.KeyNamePrefix, multimetro.KeyUUIDPrefix} {
		if head == prefix || strings.HasSuffix(head, multimetro.MetroKeySeparator+prefix) {
			j := strings.Index(spec[i+len(copyPathSeparator):], copyPathSeparator)
			if j < 0 {
				return "", spec
			}
			i += len(copyPathSeparator) + j
			break
		}
	}

	target, filePath = spec[:i], spec[i+len(copyPathSeparator):]

	if target == "" || filepath.IsAbs(target) || strings.HasPrefix(target, ".") || strings.HasPrefix(target, "~") {
		return "", spec
	}

	return target, filePath
}

type CopySandboxInstanceCmd struct {
	Source      string `arg:"" name:"source" help:"File to copy from, either a local path or <instance>:<path>."`
	Destination string `arg:"" name:"destination" help:"Where to copy it to, either a local path or <instance>:<path>."`

	Plugin  string `name:"plugin" help:"Plugin name from the instance to copy through."`
	Force   bool   `name:"force" help:"Overwrite the local file if it already exists. Copying to an instance always overwrites the remote file."`
	Parents bool   `name:"parents" short:"p" help:"Create parent directories of the destination if they don't already exist, on whichever side it is on."`
}

func (cmd CopySandboxInstanceCmd) Help() string {
	return "Only single files are copied, and their permissions are not carried across: a file copied off an instance is created with the usual permissions for a new file."
}

func (cmd CopySandboxInstanceCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Copy a local file to a sandbox instance",
			Commands: []string{
				"unikraft instance copy ./config.json my-instance:/etc/app/config.json",
			},
		},
		{
			Description: "Copy a file off a sandbox instance",
			Commands: []string{
				"unikraft instance copy my-instance:/var/log/app.log ./app.log",
			},
		},
		{
			Description: "Copy a file into a directory, keeping its name",
			Commands: []string{
				"unikraft instance copy my-instance:/var/log/app.log ./logs/",
			},
		},
		{
			Description: "Copy a file to an instance in a specific metro, creating parent directories as needed",
			Commands: []string{
				"unikraft instance copy ./data.bin fra0/my-instance:/var/lib/app/data.bin --parents",
			},
		},
	}
}

func (c *CopySandboxInstanceCmd) Run(ctx context.Context, stdio config.Stdio, rsb *resource.Sandbox) error {
	srcTarget, srcPath := parseCopyPath(c.Source)
	dstTarget, dstPath := parseCopyPath(c.Destination)

	switch {
	case srcTarget != "" && dstTarget != "":
		return fmt.Errorf("cannot copy from one instance to another: copy %q to a local path first", c.Source)

	case srcTarget == "" && dstTarget == "":
		return fmt.Errorf("neither %q nor %q names an instance: a path on an instance is written as <instance>%s<path>", c.Source, c.Destination, copyPathSeparator)

	case dstTarget != "":
		if dstPath == "" {
			dstPath = filepath.Base(srcPath)
		}

		info, err := os.Stat(srcPath)
		if err != nil {
			return fmt.Errorf("reading local file %q: %w", srcPath, err)
		}
		if info.IsDir() {
			return fmt.Errorf("%q is a directory: only single files can be copied", srcPath)
		}

		target, err := resolveSandboxTarget(ctx, rsb, dstTarget, c.Plugin)
		if err != nil {
			return err
		}

		opts := uploadOpts{
			local:   srcPath,
			remote:  dstPath,
			parents: c.Parents,
		}

		remote, err := target.upload(ctx, opts)
		if err != nil {
			return err
		}

		log.G(ctx).Info().
			Str("source", srcPath).
			Str("remote", remote).
			Msg("file written")
		return nil

	default:
		target, err := resolveSandboxTarget(ctx, rsb, srcTarget, c.Plugin)
		if err != nil {
			return err
		}

		opts := downloadOpts{
			remote:  srcPath,
			local:   dstPath,
			force:   c.Force,
			parents: c.Parents,
		}

		local, size, err := target.download(ctx, opts)
		if err != nil {
			return err
		}

		log.G(ctx).Info().
			Str("remote", srcPath).
			Str("local", local).
			Int("size", size).
			Msg("file read")
		return nil
	}
}

func (t sandboxTarget) mkdir(ctx context.Context, path string, parents bool) error {
	log.G(ctx).Trace().Msg("creating directory")

	req := sandbox.MkdirRequest{
		Path:    path,
		Parents: parents,
	}
	if _, err := t.client.CreateDirectory(ctx, t.instance, &req, t.opts...); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	return nil
}
