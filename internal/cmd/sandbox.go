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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/cloud/sdk/sandbox"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/multimetro"
)

type (
	SandboxInstanceCmd []string
	ExecOpts           struct {
		Cmd SandboxInstanceCmd `arg:"" name:"command" help:"Command to pass to the instance." placeholder:"cmd"`

		Plugin      string            `name:"plugin" help:"Plugin name from the instance to run the command onto" default:"sandbox"`
		Dir         string            `name:"dir" help:"Directory to execute the command from"`
		Env         map[string]string `name:"env"     help:"Environment variables to set (KEY=VALUE)" mapsep:","`
		TimeoutMsec int               `name:"cmd-timeout" help:"Timeout for waiting the result of the command"`
		Raw         bool
	}
)

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
	key := multimetro.ParseKey(c.Target)

	sandbox, opErr := Instance{}.Get(ctx, []string{key.String()})
	if opErr != nil && len(sandbox) == 0 {
		return opErr
	}

	instance := sandbox[0].(Instance)
	if err := requireRunningInstance(instance); err != nil {
		return err
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}

	resolvedKey := instance.Key().(multimetro.Key)

	return execSandboxInstance(ctx, stdio.Stdout, nil, g, resolvedKey, c.ExecOpts)
}

// instanceSandboxReady reports whether an instance's sandbox plugin is
// reachable: either fully running, or standby (scaled to zero, which wakes
// on the incoming request).
func instanceSandboxReady(instance Instance) bool {
	switch platform.InstanceState(instance.State) {
	case platform.InstanceStateRunning, platform.InstanceStateStandby:
		return true
	default:
		return false
	}
}

// requireRunningInstance returns an error if the instance is not in a state
// that can service sandbox requests (exec/write/read/mkdir). Sending these
// requests to a stopped instance results in an opaque 404 from the routing
// proxy rather than a useful error, since there's no live sandbox plugin to
// route to.
func requireRunningInstance(instance Instance) error {
	if instanceSandboxReady(instance) {
		return nil
	}
	return fmt.Errorf("instance %q is not running (state: %s); start it with \"unikraft instance start %s\"", instance.Name, string(instance.State), instance.Name)
}

// BuildExecCommand constructs the full shell command line from the exec options.
func BuildExecCommand(dir string, env map[string]string, cmdArgs []string, raw bool) string {
	var prefix string

	if dir != "" {
		escapedDir := "'" + strings.ReplaceAll(dir, "'", "'\\''") + "'"
		prefix += fmt.Sprintf("cd %s && ", escapedDir)
	}

	if len(env) > 0 {
		var envBuf strings.Builder
		envBuf.WriteString("env ")
		for k, v := range env {
			escapedVal := "'" + strings.ReplaceAll(v, "'", "'\\''") + "'"
			fmt.Fprintf(&envBuf, "%s=%s ", k, escapedVal)
		}
		prefix += envBuf.String()
	}

	if raw {
		return prefix + strings.Join(cmdArgs, " ")
	}

	var quotedCmd []string
	for _, arg := range cmdArgs {
		if strings.ContainsAny(arg, " \t\n*?[]{}()<>|&;\\\"'") || arg == "" {
			arg = "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
		}
		quotedCmd = append(quotedCmd, arg)
	}
	return prefix + strings.Join(quotedCmd, " ")
}

func execSandboxInstance(ctx context.Context, out io.Writer, in io.Reader, g *group.Group[multimetro.MetroClient], key multimetro.Key, opts ExecOpts) error {
	log.G(ctx).Trace().Msg("executing command")

	_, err := group.CollectMetro(ctx, g, key.Metro, func(ctx context.Context, c multimetro.MetroClient) (struct{}, error) {
		instanceUUID := key.Ref().UUID
		pluginName := opts.Plugin

		cmdline := BuildExecCommand(opts.Dir, opts.Env, opts.Cmd, opts.Raw)

		req := sandbox.ExecInstanceCommandRequestBody{
			Cmd: cmdline,
		}

		execResp, err := c.Sandbox.ExecInstanceCommand(ctx, instanceUUID, pluginName, req)
		if err != nil {
			return struct{}{}, fmt.Errorf("failed to start command: %w", err)
		}
		cmdUUID := execResp.Data.Uuid

		// If we're leaving this closure because ctx was cancelled (Ctrl+C,
		// timeout, etc.), the remote command has no idea and keeps running -
		// signal it to stop. This must be a defer rather than living in the
		// select's ctx.Done() case below, since cancellation is just as
		// likely to be observed as a failure from an in-flight request
		// (fetchAndPrint/InspectInstanceCommand) as from that select case.
		defer func() {
			if ctx.Err() == nil {
				return
			}
			signalCtx, cancelSignal := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelSignal()
			if _, sigErr := c.Sandbox.SignalInstanceCommand(signalCtx, instanceUUID, pluginName, cmdUUID, sandbox.SignalInstanceCommandRequestBody{
				Signal: int(syscall.SIGINT),
			}); sigErr != nil {
				log.G(ctx).Debug().Err(sigErr).Str("cmd", cmdUUID).Msg("failed to signal remote command")
			}
		}()

		if in != nil {
			feedCtx, cancelFeed := context.WithCancel(ctx)
			defer cancelFeed()
			go feedSandboxStdin(feedCtx, c, instanceUUID, pluginName, cmdUUID, in)
		}

		var stdoutOffset, stderrOffset int64
		fetchAndPrint := func() error {
			log.G(ctx).Trace().
				Str("cmd", cmdUUID).
				Int64("stdout_offset", stdoutOffset).
				Int64("stderr_offset", stderrOffset).
				Msg("fetching logs")
			logsReq := sandbox.GetInstanceCommandLogsRequestBody{
				Stdout: &sandbox.BodyStreamRange{Offset: &stdoutOffset},
				Stderr: &sandbox.BodyStreamRange{Offset: &stderrOffset},
			}
			fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			logsResp, err := c.Sandbox.GetInstanceCommandLogs(fetchCtx, instanceUUID, pluginName, cmdUUID, logsReq)
			if err != nil {
				return fmt.Errorf("failed to fetch logs: %w", err)
			}
			log.G(ctx).Trace().
				Str("cmd", cmdUUID).
				Int64("stdout_offset", stdoutOffset).
				Int64("stderr_offset", stderrOffset).
				Bool("has_stdout", logsResp.Data.Stdout != "").
				Bool("has_stderr", logsResp.Data.Stderr != "").
				Interface("stdout_available", logsResp.Data.StdoutAvailable).
				Interface("stderr_available", logsResp.Data.StderrAvailable).
				Msg("polled command logs")
			if logsResp.Data.Stdout != "" {
				stdout, err := base64.StdEncoding.DecodeString(logsResp.Data.Stdout)
				if err != nil {
					stdout = []byte(logsResp.Data.Stdout)
				}
				fmt.Fprint(out, string(stdout))
				if logsResp.Data.StdoutAvailable != nil {
					stdoutOffset = int64(*logsResp.Data.StdoutAvailable)
				} else {
					stdoutOffset += int64(len(stdout))
				}
			}
			if logsResp.Data.Stderr != "" {
				stderr, err := base64.StdEncoding.DecodeString(logsResp.Data.Stderr)
				if err != nil {
					stderr = []byte(logsResp.Data.Stderr)
				}
				fmt.Fprint(out, string(stderr))
				if logsResp.Data.StderrAvailable != nil {
					stderrOffset = int64(*logsResp.Data.StderrAvailable)
				} else {
					stderrOffset += int64(len(stderr))
				}
			}
			return nil
		}

		log.G(ctx).Trace().
			Str("cmd", cmdUUID).
			Str("cmdline", cmdline).
			Msg("starting poll loop")

		var deadline time.Time
		if opts.TimeoutMsec > 0 {
			deadline = time.Now().Add(time.Duration(opts.TimeoutMsec) * time.Millisecond)
		}

		for {
			select {
			case <-ctx.Done():
				log.G(ctx).Trace().
					Str("cmd", cmdUUID).
					Msg("context cancelled, exiting poll loop")
				return struct{}{}, ctx.Err()
			case <-time.After(100 * time.Millisecond):
				if err := fetchAndPrint(); err != nil {
					return struct{}{}, err
				}

				inspectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				inspectResp, err := c.Sandbox.InspectInstanceCommand(inspectCtx, instanceUUID, pluginName, cmdUUID)
				cancel()
				if err != nil {
					return struct{}{}, fmt.Errorf("failed to inspect command: %w", err)
				}

				if inspectResp.Data.Exitcode != nil {
					log.G(ctx).Trace().
						Str("cmd", cmdUUID).
						Int32("exitcode", *inspectResp.Data.Exitcode).
						Msg("command finished, doing final fetch")
					if err := fetchAndPrint(); err != nil {
						return struct{}{}, err
					}
					return struct{}{}, nil
				}

				if !deadline.IsZero() && time.Now().After(deadline) {
					return struct{}{}, fmt.Errorf("timed out waiting for command to finish")
				}
			}
		}
	})
	return err
}

func feedSandboxStdin(ctx context.Context, c multimetro.MetroClient, instanceUUID, pluginName, cmdUUID string, in io.Reader) {
	if deadliner, ok := in.(interface{ SetReadDeadline(time.Time) error }); ok {
		stop := make(chan struct{})
		defer close(stop)
		go func() {
			select {
			case <-ctx.Done():
				_ = deadliner.SetReadDeadline(time.Now())
			case <-stop:
			}
		}()
		defer func() { _ = deadliner.SetReadDeadline(time.Time{}) }()
	}

	buf := make([]byte, 32*1024)
	for {
		if ctx.Err() != nil {
			return
		}

		n, readErr := in.Read(buf)
		if n > 0 {
			req := sandbox.FeedInstanceCommandStdinRequestBody{
				Data: base64.StdEncoding.EncodeToString(buf[:n]),
				Eof:  false,
			}
			if _, err := c.Sandbox.FeedInstanceCommandStdin(ctx, instanceUUID, pluginName, cmdUUID, req); err != nil {
				return
			}
		}

		if readErr != nil {
			if ctx.Err() != nil {
				return
			}
			eofReq := sandbox.FeedInstanceCommandStdinRequestBody{Eof: true}
			_, _ = c.Sandbox.FeedInstanceCommandStdin(ctx, instanceUUID, pluginName, cmdUUID, eofReq)
			return
		}
	}
}

type WriteSandboxInstanceCmd struct {
	Target string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instance to write the file to."`
	Local  string `arg:"" name:"local" help:"Local file path to read from." type:"existingfile"`
	Remote string `arg:"" name:"remote" help:"Remote destination path on the instance."`

	Plugin  string `name:"plugin" help:"Plugin name from the instance to write the file to." default:"sandbox"`
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
	key := multimetro.ParseKey(c.Target)

	sandbox, opErr := Instance{}.Get(ctx, []string{key.String()})
	if opErr != nil && len(sandbox) == 0 {
		return opErr
	}

	instance := sandbox[0].(Instance)
	if err := requireRunningInstance(instance); err != nil {
		return err
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}

	resolvedKey := instance.Key().(multimetro.Key)

	data, err := os.ReadFile(c.Local)
	if err != nil {
		return fmt.Errorf("reading local file %q: %w", c.Local, err)
	}

	if c.Parents {
		if err := mkdirSandboxInstance(ctx, g, resolvedKey, c.Plugin, filepath.Dir(c.Remote), true); err != nil {
			return fmt.Errorf("creating parent directories: %w", err)
		}
	}

	remote := c.Remote
	if err := writeSandboxFile(ctx, g, resolvedKey, c.Plugin, remote, data, c.Append); err != nil {
		if !strings.Contains(err.Error(), "Is a directory") {
			return err
		}
		remote = filepath.Join(remote, filepath.Base(c.Local))
		if err := writeSandboxFile(ctx, g, resolvedKey, c.Plugin, remote, data, c.Append); err != nil {
			return err
		}
	}

	log.G(ctx).Info().
		Str("source", c.Local).
		Str("remote", remote).
		Msg("file written")
	return nil
}

func writeSandboxFile(ctx context.Context, g *group.Group[multimetro.MetroClient], key multimetro.Key, plugin, remotePath string, data []byte, appendFile bool) error {
	log.G(ctx).Trace().Msg("writing file")

	_, err := group.CollectMetro(ctx, g, key.Metro, func(ctx context.Context, c multimetro.MetroClient) (struct{}, error) {
		req := sandbox.WriteInstanceFileRequestBody{
			Path:     remotePath,
			Append:   appendFile,
			Encoding: "base64",
			Data:     base64.StdEncoding.EncodeToString(data),
		}
		if _, err := c.Sandbox.WriteInstanceFile(ctx, key.Ref().UUID, plugin, req); err != nil {
			return struct{}{}, fmt.Errorf("failed to write file: %w", err)
		}
		return struct{}{}, nil
	})
	return err
}

type ReadSandboxInstanceCmd struct {
	Target string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instance to read the file from."`
	Remote string `arg:"" name:"remote" help:"Remote file path to read."`
	Local  string `arg:"" name:"local" optional:"" help:"Local destination path to write the file to. Defaults to the remote file's base name."`

	Plugin string `name:"plugin" help:"Plugin name from the instance to read the file from." default:"sandbox"`
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
	key := multimetro.ParseKey(c.Target)

	sandbox, opErr := Instance{}.Get(ctx, []string{key.String()})
	if opErr != nil && len(sandbox) == 0 {
		return opErr
	}

	instance := sandbox[0].(Instance)
	if err := requireRunningInstance(instance); err != nil {
		return err
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}

	resolvedKey := instance.Key().(multimetro.Key)

	data, err := readSandboxFile(ctx, g, resolvedKey, c.Plugin, c.Remote)
	if err != nil {
		return err
	}

	local := c.Local
	if local == "" {
		local = filepath.Base(c.Remote)
	} else if info, err := os.Stat(local); err == nil && info.IsDir() {
		local = filepath.Join(local, filepath.Base(c.Remote))
	}

	if !c.Force {
		if _, err := os.Stat(local); err == nil {
			return fmt.Errorf("local file %q already exists (use --force to overwrite)", local)
		}
	}

	if err := os.WriteFile(local, data, 0o644); err != nil {
		return fmt.Errorf("writing local file %q: %w", local, err)
	}

	log.G(ctx).Info().
		Str("remote", c.Remote).
		Str("local", local).
		Int("size", len(data)).
		Msg("file read")
	return nil
}

func readSandboxFile(ctx context.Context, g *group.Group[multimetro.MetroClient], key multimetro.Key, plugin, remotePath string) ([]byte, error) {
	log.G(ctx).Trace().Msg("reading file")

	return group.CollectMetro(ctx, g, key.Metro, func(ctx context.Context, c multimetro.MetroClient) ([]byte, error) {
		req := sandbox.ReadInstanceFileRequestBody{
			Path: remotePath,
		}
		resp, err := c.Sandbox.ReadInstanceFile(ctx, key.Ref().UUID, plugin, req)
		if err != nil {
			return nil, fmt.Errorf("failed to read file: %w", err)
		}

		data, err := base64.StdEncoding.DecodeString(resp.Data.Contents)
		if err != nil {
			return []byte(resp.Data.Contents), nil
		}
		return data, nil
	})
}

type MkdirSandboxInstanceCmd struct {
	Target string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instance to create the directory on."`
	Path   string `arg:"" name:"path" help:"Remote directory path to create."`

	Plugin  string `name:"plugin" help:"Plugin name from the instance to create the directory on." default:"sandbox"`
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
	key := multimetro.ParseKey(c.Target)

	sandbox, opErr := Instance{}.Get(ctx, []string{key.String()})
	if opErr != nil && len(sandbox) == 0 {
		return opErr
	}

	instance := sandbox[0].(Instance)
	if err := requireRunningInstance(instance); err != nil {
		return err
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}

	resolvedKey := instance.Key().(multimetro.Key)

	if err := mkdirSandboxInstance(ctx, g, resolvedKey, c.Plugin, c.Path, c.Parents); err != nil {
		return err
	}

	fmt.Fprintf(stdio.Stdout, "created directory %q\n", c.Path)
	return nil
}

func mkdirSandboxInstance(ctx context.Context, g *group.Group[multimetro.MetroClient], key multimetro.Key, plugin, path string, parents bool) error {
	log.G(ctx).Trace().Msg("creating directory")

	_, err := group.CollectMetro(ctx, g, key.Metro, func(ctx context.Context, c multimetro.MetroClient) (struct{}, error) {
		req := sandbox.MakeInstanceDirectoryRequestBody{
			Path:    path,
			Parents: parents,
		}
		if _, err := c.Sandbox.MakeInstanceDirectory(ctx, key.Ref().UUID, plugin, req); err != nil {
			return struct{}{}, fmt.Errorf("failed to create directory: %w", err)
		}
		return struct{}{}, nil
	})
	return err
}
