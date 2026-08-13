// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").

package cmd

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"
	"mvdan.cc/sh/v3/syntax"

	"unikraft.com/cloud/plugins/sandbox"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/multimetro"
)

type ExecOpts struct {
	Cmd []string `arg:"" name:"command" help:"Command to pass to the instance." placeholder:"cmd"`

	Plugin      string            `name:"plugin" help:"Plugin name from the instance to run the command onto"`
	Dir         string            `name:"dir" help:"Directory to execute the command from"`
	Env         map[string]string `name:"env"     help:"Environment variables to set (KEY=VALUE)" mapsep:","`
	TimeoutMsec int               `name:"cmd-timeout" help:"Timeout for waiting the result of the command"`

	// Raw sends the command line to the remote shell unquoted. Only the
	// interactive shell sets it - kong would otherwise expose it as an
	// undocumented --raw flag.
	Raw bool `kong:"-"`
}

type ExecSandboxInstanceCmd struct {
	Target string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instances to run the command on."`

	ExecOpts
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
				"unikraft instance exec my-instance --plugin sandbox --env DEBUG=true,PORT=8080 -- ./start.sh",
			},
		},
		{
			Description: "Run a command with a timeout on waiting for it to finish",
			Commands: []string{
				"unikraft instance exec my-instance --plugin sandbox --cmd-timeout 5000 -- long-running-task",
			},
		},
	}
}

func (c *ExecSandboxInstanceCmd) Run(ctx context.Context, stdio config.Stdio) error {
	target, err := resolveSandboxTarget(ctx, c.Target, c.Plugin, requireRunning)
	if err != nil {
		return err
	}

	opts := c.ExecOpts
	opts.Plugin = target.plugin

	in := stdio.Stdin
	fd, isFile := in.(interface{ Fd() uintptr })
	if in == nil || (isFile && term.IsTerminal(fd.Fd())) {
		in = strings.NewReader("")
	}

	return execSandboxInstance(ctx, stdio.Stdout, in, target.g, target.key, opts)
}

// sandboxTarget is an instance resolved and checked well enough to send
// plugin requests to.
type sandboxTarget struct {
	instance Instance
	key      multimetro.Key
	g        *group.Group[multimetro.MetroClient]
	plugin   string
}

type sandboxTargetOpt int

const (
	allowStopped sandboxTargetOpt = iota
	requireRunning
)

func resolveSandboxTarget(ctx context.Context, target, plugin string, opt sandboxTargetOpt) (sandboxTarget, error) {
	if plugin == "" {
		plugin = sandbox.PluginName
	}

	key := multimetro.ParseKey(target)

	resources, opErr := Instance{}.Get(ctx, []string{key.String()})
	if len(resources) == 0 {
		if opErr != nil {
			return sandboxTarget{}, opErr
		}
		return sandboxTarget{}, fmt.Errorf("instance %q not found", target)
	}

	instance, ok := resources[0].(Instance)
	if !ok {
		return sandboxTarget{}, fmt.Errorf("%q is not an instance", target)
	}

	if opt == requireRunning {
		if err := requireRunningInstance(instance); err != nil {
			return sandboxTarget{}, err
		}
	}

	if err := requirePlugin(instance, plugin); err != nil {
		return sandboxTarget{}, err
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return sandboxTarget{}, err
	}

	return sandboxTarget{
		instance: instance,
		key:      instance.Key().(multimetro.Key),
		g:        g,
		plugin:   plugin,
	}, nil
}

func requireRunningInstance(instance Instance) error {
	switch platform.InstanceState(instance.State) {
	case platform.InstanceStateRunning, platform.InstanceStateStandby:
		return nil
	default:
		return fmt.Errorf("instance %q is not running (state: %s); start it with \"unikraft instance start %s\"", instance.Name, string(instance.State), instance.Name)
	}
}

func requirePlugin(instance Instance, plugin string) error {
	var loaded []string
	for _, p := range instance.Plugins {
		if p == nil || p.Name == "" {
			continue
		}
		if p.Name == plugin {
			return nil
		}
		loaded = append(loaded, p.Name)
	}

	if len(loaded) == 0 {
		return fmt.Errorf("instance %q has no plugins loaded, so nothing answers to %q", instance.Name, plugin)
	}
	return fmt.Errorf("instance %q has no plugin named %q; it has: %s", instance.Name, plugin, strings.Join(loaded, ", "))
}

const sandboxLogPollInterval = 100 * time.Millisecond

func decodeSandboxPayload(s string) []byte {
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return []byte(s)
	}
	return decoded
}

func wrapSandboxErr(ctx context.Context, key multimetro.Key, plugin, msg string, err error) error {
	if err != nil && strings.Contains(err.Error(), "request failed: 404") {
		log.G(ctx).Debug().Err(err).Str("plugin", plugin).Msg("plugin endpoint returned 404")
		return fmt.Errorf("instance %q is not serving its %q plugin: the plugin may have failed to start", key.String(), plugin)
	}
	return fmt.Errorf("%s: %w", msg, err)
}

func quoteShellArg(s string) string {
	if quoted, err := syntax.Quote(s, syntax.LangBash); err == nil {
		return quoted
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func BuildExecCommand(dir string, env map[string]string, cmdArgs []string, raw bool) string {
	var prefix string

	if dir != "" {
		prefix += fmt.Sprintf("cd %s && ", quoteShellArg(dir))
	}

	if len(env) > 0 {
		var envBuf strings.Builder
		envBuf.WriteString("env ")
		for k, v := range env {
			fmt.Fprintf(&envBuf, "%s=%s ", k, quoteShellArg(v))
		}
		prefix += envBuf.String()
	}

	if raw {
		return prefix + strings.Join(cmdArgs, " ")
	}

	quotedCmd := make([]string, 0, len(cmdArgs))
	for _, arg := range cmdArgs {
		quotedCmd = append(quotedCmd, quoteShellArg(arg))
	}
	return prefix + strings.Join(quotedCmd, " ")
}

func execSandboxInstance(ctx context.Context, out io.Writer, in io.Reader, g *group.Group[multimetro.MetroClient], key multimetro.Key, opts ExecOpts) error {
	log.G(ctx).Trace().Msg("executing command")

	_, err := group.CollectMetro(ctx, g, key.Metro, func(ctx context.Context, c multimetro.MetroClient) (struct{}, error) {
		instance := multimetro.SandboxInstance(key)
		pluginName := opts.Plugin
		callOpts := c.SandboxOpts(pluginName)

		cmdline := BuildExecCommand(opts.Dir, opts.Env, opts.Cmd, opts.Raw)

		req := sandbox.RunCommandRequest{
			Cmd: cmdline,
		}

		execResp, err := c.Sandbox.RunCommand(ctx, instance, &req, callOpts...)
		if err != nil {
			return struct{}{}, wrapSandboxErr(ctx, key, pluginName, "failed to start command", err)
		}
		if execResp.Data == nil {
			return struct{}{}, fmt.Errorf("failed to start command: the %q plugin did not report a command UUID", pluginName)
		}
		cmdUUID := execResp.Data.Uuid

		defer func() {
			if ctx.Err() == nil {
				return
			}
			signalCtx, cancelSignal := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelSignal()
			if _, sigErr := c.Sandbox.SignalCommand(signalCtx, instance, cmdUUID, &sandbox.CommandSignalRequest{
				Signal: int(syscall.SIGINT),
			}, callOpts...); sigErr != nil {
				log.G(ctx).Debug().Err(sigErr).Str("cmd", cmdUUID).Msg("failed to signal remote command")
			}
		}()

		if in != nil {
			feedCtx, cancelFeed := context.WithCancel(ctx)
			defer cancelFeed()
			go feedSandboxStdin(feedCtx, c, instance, pluginName, cmdUUID, in)
		}

		var stdoutOffset, stderrOffset uint64
		fetchAndPrint := func() error {
			log.G(ctx).Trace().
				Str("cmd", cmdUUID).
				Uint64("stdout_offset", stdoutOffset).
				Uint64("stderr_offset", stderrOffset).
				Msg("fetching logs")
			logsReq := sandbox.CommandLogsRequest{
				Stdout: sandbox.CommandLogsRange{Offset: stdoutOffset},
				Stderr: sandbox.CommandLogsRange{Offset: stderrOffset},
			}
			fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			logsResp, err := c.Sandbox.GetCommandLogs(fetchCtx, instance, cmdUUID, &logsReq, callOpts...)
			if err != nil {
				return wrapSandboxErr(ctx, key, pluginName, "failed to fetch logs", err)
			}
			if logsResp.Data == nil {
				return nil
			}
			log.G(ctx).Trace().
				Str("cmd", cmdUUID).
				Uint64("stdout_offset", stdoutOffset).
				Uint64("stderr_offset", stderrOffset).
				Bool("has_stdout", logsResp.Data.Stdout != "").
				Bool("has_stderr", logsResp.Data.Stderr != "").
				Uint64("stdout_available", logsResp.Data.StdoutAvailable).
				Uint64("stderr_available", logsResp.Data.StderrAvailable).
				Msg("polled command logs")
			for _, stream := range []struct {
				data      string
				available uint64
				offset    *uint64
			}{
				{logsResp.Data.Stdout, logsResp.Data.StdoutAvailable, &stdoutOffset},
				{logsResp.Data.Stderr, logsResp.Data.StderrAvailable, &stderrOffset},
			} {
				if stream.data == "" {
					continue
				}
				decoded := decodeSandboxPayload(stream.data)
				fmt.Fprint(out, string(decoded))
				// The plugin reports how much of the stream it has produced,
				// which is where the next poll picks up. One that doesn't
				// leaves the offset to advance by what was read.
				if stream.available > 0 {
					*stream.offset = stream.available
				} else {
					*stream.offset += uint64(len(decoded))
				}
			}
			return nil
		}

		log.G(ctx).Trace().
			Str("cmd", cmdUUID).
			Str("cmdline", cmdline).
			Msg("waiting for command")

		waitCtx := ctx
		if opts.TimeoutMsec > 0 {
			var cancelWait context.CancelFunc
			waitCtx, cancelWait = context.WithTimeout(ctx, time.Duration(opts.TimeoutMsec)*time.Millisecond)
			defer cancelWait()
		}

		done := make(chan error, 1)
		go func() {
			_, err := c.Sandbox.WaitForCommand(waitCtx, instance, cmdUUID, callOpts...)
			done <- err
		}()

		for {
			select {
			case <-ctx.Done():
				log.G(ctx).Trace().Str("cmd", cmdUUID).Msg("context cancelled, exiting wait")
				return struct{}{}, ctx.Err()

			case waitErr := <-done:
				if waitErr != nil {
					if ctx.Err() == nil && waitCtx.Err() == context.DeadlineExceeded {
						return struct{}{}, fmt.Errorf("timed out waiting for command to finish")
					}
					return struct{}{}, wrapSandboxErr(ctx, key, pluginName, "failed waiting for command", waitErr)
				}

				if err := fetchAndPrint(); err != nil {
					return struct{}{}, err
				}

				inspectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				inspectResp, err := c.Sandbox.GetCommandByUuid(inspectCtx, instance, cmdUUID, callOpts...)
				cancel()
				if err != nil {
					return struct{}{}, wrapSandboxErr(ctx, key, pluginName, "failed to inspect command", err)
				}
				if inspectResp.Data != nil {
					log.G(ctx).Trace().
						Str("cmd", cmdUUID).
						Int32("exitcode", inspectResp.Data.Exitcode).
						Msg("command finished")
				}
				return struct{}{}, nil

			case <-time.After(sandboxLogPollInterval):
				if err := fetchAndPrint(); err != nil {
					return struct{}{}, err
				}
			}
		}
	})
	return err
}

func feedSandboxStdin(ctx context.Context, c multimetro.MetroClient, instance platform.Instance, pluginName, cmdUUID string, in io.Reader) {
	callOpts := c.SandboxOpts(pluginName)

	buf := make([]byte, 32*1024)
	for {
		if ctx.Err() != nil {
			return
		}

		n, readErr := in.Read(buf)
		if n > 0 {
			req := sandbox.CommandStdinRequest{
				Data: base64.StdEncoding.EncodeToString(buf[:n]),
				Eof:  false,
			}
			if _, err := c.Sandbox.WriteCommandStdin(ctx, instance, cmdUUID, &req, callOpts...); err != nil {
				return
			}
		}

		if readErr != nil {
			if ctx.Err() != nil {
				return
			}
			eofReq := sandbox.CommandStdinRequest{Eof: true}
			_, _ = c.Sandbox.WriteCommandStdin(ctx, instance, cmdUUID, &eofReq, callOpts...)
			return
		}
	}
}

type WriteSandboxInstanceCmd struct {
	Target string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instance to write the file to."`
	Local  string `arg:"" name:"local" help:"Local file path to read from." type:"existingfile"`
	Remote string `arg:"" name:"remote" help:"Remote destination path on the instance."`

	Plugin  string `name:"plugin" help:"Plugin name from the instance to write the file to."`
	Append  bool   `name:"append" help:"Append to the remote file instead of overwriting it."`
	Parents bool   `name:"parents" help:"Create parent directories on the remote path if they don't already exist."`
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

func (c *WriteSandboxInstanceCmd) Run(ctx context.Context, stdio config.Stdio) error {
	target, err := resolveSandboxTarget(ctx, c.Target, c.Plugin, requireRunning)
	if err != nil {
		return err
	}

	remote, err := uploadSandboxFile(ctx, target.g, target.key, target.plugin, c.Local, c.Remote, c.Append, c.Parents)
	if err != nil {
		return err
	}

	log.G(ctx).Info().
		Str("source", c.Local).
		Str("remote", remote).
		Msg("file written")
	return nil
}

// uploadSandboxFile copies a local file to remotePath and returns the remote
// path it ended up at: a remotePath naming a directory is written into under
// the local base name, the way scp resolves one.
func uploadSandboxFile(ctx context.Context, g *group.Group[multimetro.MetroClient], key multimetro.Key, plugin, local, remotePath string, appendFile, parents bool) (string, error) {
	data, err := os.ReadFile(local)
	if err != nil {
		return "", fmt.Errorf("reading local file %q: %w", local, err)
	}

	if parents {
		if err := mkdirSandboxInstance(ctx, g, key, plugin, path.Dir(remotePath), true); err != nil {
			return "", fmt.Errorf("creating parent directories: %w", err)
		}
	}

	if err := writeSandboxFile(ctx, g, key, plugin, remotePath, data, appendFile); err != nil {
		if !strings.Contains(err.Error(), "Is a directory") {
			return "", err
		}
		remotePath = path.Join(remotePath, filepath.Base(local))
		if err := writeSandboxFile(ctx, g, key, plugin, remotePath, data, appendFile); err != nil {
			return "", err
		}
	}

	return remotePath, nil
}

func writeSandboxFile(ctx context.Context, g *group.Group[multimetro.MetroClient], key multimetro.Key, plugin, remotePath string, data []byte, appendFile bool) error {
	log.G(ctx).Trace().Msg("writing file")

	_, err := group.CollectMetro(ctx, g, key.Metro, func(ctx context.Context, c multimetro.MetroClient) (struct{}, error) {
		req := sandbox.WriteFileRequest{
			Path:     remotePath,
			Append:   appendFile,
			Encoding: sandbox.FileEncodingBase64,
			Data:     base64.StdEncoding.EncodeToString(data),
		}
		if _, err := c.Sandbox.WriteFile(ctx, multimetro.SandboxInstance(key), &req, c.SandboxOpts(plugin)...); err != nil {
			return struct{}{}, wrapSandboxErr(ctx, key, plugin, "failed to write file", err)
		}
		return struct{}{}, nil
	})
	return err
}

type ReadSandboxInstanceCmd struct {
	Target string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instance to read the file from."`
	Remote string `arg:"" name:"remote" help:"Remote file path to read."`
	Local  string `arg:"" name:"local" optional:"" help:"Local destination path to write the file to. Defaults to the remote file's base name."`

	Plugin string `name:"plugin" help:"Plugin name from the instance to read the file from."`
	Force  bool   `name:"force" help:"Overwrite the local file if it already exists."`
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

func (c *ReadSandboxInstanceCmd) Run(ctx context.Context, stdio config.Stdio) error {
	target, err := resolveSandboxTarget(ctx, c.Target, c.Plugin, requireRunning)
	if err != nil {
		return err
	}

	local, size, err := downloadSandboxFile(ctx, target.g, target.key, target.plugin, c.Remote, c.Local, c.Force)
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

// downloadSandboxFile copies remotePath off the instance and returns the local
// path written and its size. An empty local path takes the remote base name,
// and one naming a directory is written into. An existing file is only
// overwritten when force is set.
func downloadSandboxFile(ctx context.Context, g *group.Group[multimetro.MetroClient], key multimetro.Key, plugin, remotePath, local string, force bool) (string, int, error) {
	data, err := readSandboxFile(ctx, g, key, plugin, remotePath)
	if err != nil {
		return "", 0, err
	}

	if local == "" {
		local = path.Base(remotePath)
	} else if info, err := os.Stat(local); err == nil && info.IsDir() {
		local = filepath.Join(local, path.Base(remotePath))
	}

	if !force {
		if _, err := os.Stat(local); err == nil {
			return "", 0, fmt.Errorf("local file %q already exists (use --force to overwrite)", local)
		}
	}

	if err := os.WriteFile(local, data, 0o644); err != nil {
		return "", 0, fmt.Errorf("writing local file %q: %w", local, err)
	}

	return local, len(data), nil
}

func readSandboxFile(ctx context.Context, g *group.Group[multimetro.MetroClient], key multimetro.Key, plugin, remotePath string) ([]byte, error) {
	log.G(ctx).Trace().Msg("reading file")

	return group.CollectMetro(ctx, g, key.Metro, func(ctx context.Context, c multimetro.MetroClient) ([]byte, error) {
		req := sandbox.ReadFileRequest{
			Path: remotePath,
		}
		resp, err := c.Sandbox.ReadFile(ctx, multimetro.SandboxInstance(key), &req, c.SandboxOpts(plugin)...)
		if err != nil {
			return nil, wrapSandboxErr(ctx, key, plugin, "failed to read file", err)
		}
		if resp.Data == nil {
			return nil, fmt.Errorf("failed to read file: the %q plugin returned no contents", plugin)
		}

		return decodeSandboxPayload(resp.Data.Contents), nil
	})
}

// copyPathSeparator separates an instance target from a path, the way scp
// separates a host from one.
const copyPathSeparator = ":"

// parseCopyPath splits a copy specification into an instance target and a path,
// returning an empty target for a local file. A target keeps its "name:" or
// "uuid:" prefix and its "<metro>/" qualifier, so the colon those end with is
// not mistaken for the separator. A local path whose first element carries a
// colon has to be written "./back:up.tar", as it does for scp.
func parseCopyPath(spec string) (target, filePath string) {
	i := strings.Index(spec, copyPathSeparator)
	if i < 0 {
		return "", spec
	}

	// A "name:" or "uuid:" prefix owns the colon it ends with, so the separator
	// is the colon after it. Without one it is a local path: an instance with
	// no path of its own still keeps the separator, as "<instance>:".
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

	// A target that opens like a filesystem path is a local file whose name
	// happens to carry a colon.
	if target == "" || strings.HasPrefix(target, "/") || strings.HasPrefix(target, ".") || strings.HasPrefix(target, "~") {
		return "", spec
	}

	return target, filePath
}

type CopySandboxInstanceCmd struct {
	Source      string `arg:"" name:"source" help:"File to copy from, either a local path or <instance>:<path>."`
	Destination string `arg:"" name:"destination" help:"Where to copy it to, either a local path or <instance>:<path>."`

	Plugin  string `name:"plugin" help:"Plugin name from the instance to copy through."`
	Force   bool   `name:"force" help:"Overwrite the local file if it already exists."`
	Parents bool   `name:"parents" help:"Create parent directories on the remote path if they don't already exist."`
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

func (c *CopySandboxInstanceCmd) Run(ctx context.Context, stdio config.Stdio) error {
	srcTarget, srcPath := parseCopyPath(c.Source)
	dstTarget, dstPath := parseCopyPath(c.Destination)

	switch {
	case srcTarget != "" && dstTarget != "":
		return fmt.Errorf("cannot copy from one instance to another: copy %q to a local path first", c.Source)

	case srcTarget == "" && dstTarget == "":
		return fmt.Errorf("neither %q nor %q names an instance: a path on an instance is written as <instance>%s<path>", c.Source, c.Destination, copyPathSeparator)

	case dstTarget != "":
		// A destination naming no path leaves the file wherever the plugin
		// resolves a relative one to, as "scp file host:" does.
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

		target, err := resolveSandboxTarget(ctx, dstTarget, c.Plugin, requireRunning)
		if err != nil {
			return err
		}

		remote, err := uploadSandboxFile(ctx, target.g, target.key, target.plugin, srcPath, dstPath, false, c.Parents)
		if err != nil {
			return err
		}

		log.G(ctx).Info().
			Str("source", srcPath).
			Str("remote", remote).
			Msg("file written")
		return nil

	default:
		target, err := resolveSandboxTarget(ctx, srcTarget, c.Plugin, requireRunning)
		if err != nil {
			return err
		}

		local, size, err := downloadSandboxFile(ctx, target.g, target.key, target.plugin, srcPath, dstPath, c.Force)
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

type MkdirSandboxInstanceCmd struct {
	Target string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instance to create the directory on."`
	Path   string `arg:"" name:"path" help:"Remote directory path to create."`

	Plugin  string `name:"plugin" help:"Plugin name from the instance to create the directory on."`
	Parents bool   `name:"parents" short:"p" help:"Create parent directories as needed."`
}

func (cmd MkdirSandboxInstanceCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Create a directory on a sandbox instance",
			Commands: []string{
				"unikraft instance mkdir my-instance /var/lib/app",
			},
		},
		{
			Description: "Create a nested directory, including any missing parents",
			Commands: []string{
				"unikraft instance mkdir my-instance /var/lib/app/data --parents",
			},
		},
	}
}

func (c *MkdirSandboxInstanceCmd) Run(ctx context.Context, stdio config.Stdio) error {
	target, err := resolveSandboxTarget(ctx, c.Target, c.Plugin, requireRunning)
	if err != nil {
		return err
	}
	if err := mkdirSandboxInstance(ctx, target.g, target.key, target.plugin, c.Path, c.Parents); err != nil {
		return err
	}

	fmt.Fprintf(stdio.Stdout, "created directory %q\n", c.Path)
	return nil
}

func mkdirSandboxInstance(ctx context.Context, g *group.Group[multimetro.MetroClient], key multimetro.Key, plugin, path string, parents bool) error {
	log.G(ctx).Trace().Msg("creating directory")

	_, err := group.CollectMetro(ctx, g, key.Metro, func(ctx context.Context, c multimetro.MetroClient) (struct{}, error) {
		req := sandbox.MkdirRequest{
			Path:    path,
			Parents: parents,
		}
		if _, err := c.Sandbox.CreateDirectory(ctx, multimetro.SandboxInstance(key), &req, c.SandboxOpts(plugin)...); err != nil {
			return struct{}{}, wrapSandboxErr(ctx, key, plugin, "failed to create directory", err)
		}
		return struct{}{}, nil
	})
	return err
}
