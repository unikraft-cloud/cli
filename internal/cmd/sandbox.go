// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").

package cmd

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
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
	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/cli/internal/types"
)

const (
	// defaultSandboxRom is the plugin ROM that --sandbox loads by default.
	//
	// TODO: point this back at "plugins/sandbox:staging" once the sandbox API
	// changes being exercised here are published.
	defaultSandboxRom = "dragosgheorghioiu/sandbox-api"

	// defaultSandboxPath is where --sandbox mounts the volume backing the
	// plugin's command history. It is handed to the plugin verbatim as its
	// persist_path, and the two have to be the same path: the plugin refuses to
	// start if the directory is missing and never creates one itself, while a
	// mount point - but not a subdirectory of one - is created for us.
	defaultSandboxPath = "/ukp/plugins/sandbox/history"

	// defaultSandboxSize is the size of the volume --sandbox creates when it is
	// not pointed at an existing one.
	defaultSandboxSize = types.SizeMebibytes(1024)
)

// InstanceSandbox is the value of the --sandbox flag: a request to load the
// sandbox plugin onto a new instance, persistent unless asked otherwise.
//
// Parsed via value.Parse as comma-separated key=value pairs, with the bare
// words on/off (and true/false) as shorthand for persist=true/false:
//
//	on
//	path=/data,size=2GiB
//	persist=false
type InstanceSandbox struct {
	Persist *bool               `name:"persist"`
	Path    string              `name:"path"`
	Volume  string              `name:"volume"`
	Size    types.SizeMebibytes `name:"size"`
	Rom     string              `name:"rom"`
	Plugin  string              `name:"plugin"`
}

func (s *InstanceSandbox) UnmarshalText(data []byte) error {
	str := strings.TrimSpace(string(data))
	if str == "" {
		return nil
	}

	switch strings.ToLower(str) {
	case "on", "true":
		s.Persist = new(true)
		return nil
	case "off", "false":
		s.Persist = new(false)
		return nil
	}

	// The alias is what keeps value.Parse from calling this method again.
	type sandboxAlias InstanceSandbox
	parsed, err := value.Parse[sandboxAlias]([]string{str})
	if err != nil {
		return err
	}
	*s = InstanceSandbox(parsed)
	return nil
}

// persistent reports whether the sandbox should be given a volume to keep its
// command history in. Unset means yes.
func (s *InstanceSandbox) persistent() bool {
	return s.Persist == nil || *s.Persist
}

// sets renders the --set entries that --sandbox stands in for: a plugin, and,
// when persisting, a volume mounted at exactly the path the plugin is told to
// persist into. plugins and volumes are what the user asked for through
// --plugin and --volume, and are read only to report a conflict.
func (s *InstanceSandbox) sets(plugins []InstancePlugin, volumes []InstanceVolume) ([]map[string]string, error) {
	name := cmp.Or(s.Plugin, sandbox.PluginName)
	rom := cmp.Or(s.Rom, defaultSandboxRom)

	for _, p := range plugins {
		if p.Name == name {
			return nil, fmt.Errorf("--plugin already loads a plugin named %q; drop it, or rename the sandbox with --sandbox=plugin=<name>", name)
		}
	}

	if !s.persistent() {
		if s.Path != "" || s.Volume != "" || s.Size > 0 {
			return nil, fmt.Errorf("--sandbox=persist=false leaves nothing to store, so path=, volume= and size= cannot be given with it")
		}
		return []map[string]string{
			{"plugins": fmt.Sprintf("name=%s,rom=%s", name, rom)},
		}, nil
	}

	at := cmp.Or(s.Path, defaultSandboxPath)
	if !path.IsAbs(at) {
		return nil, fmt.Errorf("--sandbox path must be absolute, got %q", at)
	}
	for _, v := range volumes {
		if v.At == at {
			return nil, fmt.Errorf("--volume already mounts at %q, where the sandbox keeps its history; mount it elsewhere, or move the sandbox with --sandbox=path=<path>", at)
		}
	}

	volume, err := s.volume(at)
	if err != nil {
		return nil, err
	}

	// Marshalled rather than concatenated: the plugin reads this config as JSON
	// on its stdin and silently falls back to storing nothing at all when it
	// does not parse, so a path carrying a quote must not be able to break it.
	config, err := json.Marshal(struct {
		PersistPath string `json:"persist_path"`
	}{PersistPath: at})
	if err != nil {
		return nil, fmt.Errorf("encoding sandbox plugin config: %w", err)
	}

	return []map[string]string{
		{"plugins": fmt.Sprintf("name=%s,rom=%s,config=%s", name, rom, config)},
		{"volumes": volume},
	}, nil
}

// volume renders the volume holding the sandbox's history, in the grammar
// InstanceVolume parses back.
//
// A name is a reference to a volume that already exists, and a size asks for a
// new one; the platform takes exactly one of the two and names anything it
// creates itself, so the two are never sent together.
func (s *InstanceSandbox) volume(at string) (string, error) {
	vol := InstanceVolume{At: at}

	switch {
	case s.Volume == "":
		vol.Size = cmp.Or(s.Size, defaultSandboxSize)
	case s.Size > 0:
		return "", fmt.Errorf("--sandbox volume=%s names a volume that already exists, so size= cannot be given with it; drop size= to attach it, or drop volume= to have one created", s.Volume)
	default:
		link, err := ParseLink[Volume]([]byte(s.Volume))
		if err != nil {
			return "", err
		}
		vol.Link = link
	}

	text, err := vol.MarshalText()
	if err != nil {
		return "", err
	}
	return string(text), nil
}

type ExecOpts struct {
	Cmd []string `arg:"" name:"command" help:"Command to pass to the instance." placeholder:"cmd"`

	Plugin string            `name:"plugin" help:"Plugin name from the instance to run the command onto"`
	Dir    string            `name:"dir" help:"Directory to execute the command from"`
	Env    map[string]string `name:"env" short:"e" help:"Environment variables." placeholder:"<key>=<value>" example:"DEBUG=true,PORT=8080" mapsep:","`
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
	}
}

func (c *ExecSandboxInstanceCmd) Run(ctx context.Context, stdio config.Stdio) error {
	target, err := resolveSandboxTarget(ctx, c.Target, c.Plugin)
	if err != nil {
		return err
	}

	in := stdio.Stdin
	fd, isFile := in.(interface{ Fd() uintptr })
	if in == nil || (isFile && term.IsTerminal(fd.Fd())) {
		in = strings.NewReader("")
	}

	return execSandboxInstance(ctx, stdio.Stdout, in, target, c.ExecOpts)
}

type sandboxTarget struct {
	client   *sandbox.Client
	instance platform.Instance
	opts     []sandbox.Option

	plugin string
}

func resolveSandboxTarget(ctx context.Context, target, plugin string) (sandboxTarget, error) {
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

	if err := requireRunningInstance(instance); err != nil {
		return sandboxTarget{}, err
	}

	if err := requirePlugin(instance, plugin); err != nil {
		return sandboxTarget{}, err
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return sandboxTarget{}, err
	}

	key = instance.Key().(multimetro.Key)
	return group.CollectMetro(ctx, g, key.Metro, func(ctx context.Context, c multimetro.MetroClient) (sandboxTarget, error) {
		return sandboxTarget{
			client:   c.Sandbox,
			instance: multimetro.SandboxInstance(key),
			opts:     c.SandboxOpts(plugin),
			plugin:   plugin,
		}, nil
	})
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

func buildExecCommand(cmdArgs []string) (string, error) {
	quoted := make([]string, 0, len(cmdArgs))
	for _, arg := range cmdArgs {
		q, err := syntax.Quote(arg, syntax.LangBash)
		if err != nil {
			return "", fmt.Errorf("cannot quote command argument %q: %w", arg, err)
		}
		quoted = append(quoted, q)
	}
	return strings.Join(quoted, " "), nil
}

func execSandboxInstance(ctx context.Context, out io.Writer, in io.Reader, target sandboxTarget, opts ExecOpts) error {
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
	if len(opts.Env) > 0 {
		req.Env = &opts.Env
	}

	execResp, err := target.client.RunCommand(ctx, target.instance, &req, target.opts...)
	if err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}
	if execResp.Data == nil {
		return fmt.Errorf("failed to start command: the %q plugin did not report a command UUID", target.plugin)
	}
	cmdUUID := execResp.Data.Uuid

	defer func() {
		if ctx.Err() == nil {
			return
		}
		signalCtx, cancelSignal := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancelSignal()
		if _, sigErr := target.client.SignalCommand(signalCtx, target.instance, cmdUUID, &sandbox.CommandSignalRequest{
			Signal: int(syscall.SIGINT),
		}, target.opts...); sigErr != nil {
			log.G(ctx).Debug().Err(sigErr).Str("cmd", cmdUUID).Msg("failed to signal remote command")
		}
	}()

	if in != nil {
		feedCtx, cancelFeed := context.WithCancel(ctx)
		defer cancelFeed()
		go feedSandboxStdin(feedCtx, target, cmdUUID, in)
	}

	logs := &commandLogs{target: target, cmdUUID: cmdUUID, out: out}

	log.G(ctx).Trace().
		Str("cmd", cmdUUID).
		Str("cmdline", cmdline).
		Msg("waiting for command")

	done := make(chan error, 1)
	go func() {
		_, err := target.client.WaitForCommand(ctx, target.instance, cmdUUID, target.opts...)
		done <- err
	}()

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

			if err := logs.fetchAndPrint(ctx); err != nil {
				return err
			}

			inspectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			inspectResp, err := target.client.GetCommandByUuid(inspectCtx, target.instance, cmdUUID, target.opts...)
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
			return nil

		case <-time.After(sandboxLogPollInterval):
			if err := logs.fetchAndPrint(ctx); err != nil {
				return err
			}
		}
	}
}

type commandLogs struct {
	target  sandboxTarget
	cmdUUID string
	out     io.Writer

	stdoutOffset, stderrOffset uint64
}

func (l *commandLogs) fetchAndPrint(ctx context.Context) error {
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
		return fmt.Errorf("failed to fetch logs: %w", err)
	}
	if resp.Data == nil {
		return nil
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

	for _, stream := range []struct {
		data      string
		available uint64
		offset    *uint64
	}{
		{resp.Data.Stdout, resp.Data.StdoutAvailable, &l.stdoutOffset},
		{resp.Data.Stderr, resp.Data.StderrAvailable, &l.stderrOffset},
	} {
		if stream.data == "" {
			continue
		}
		decoded := decodeSandboxPayload(stream.data)
		fmt.Fprint(l.out, string(decoded))
		if stream.available > 0 {
			*stream.offset = stream.available
		} else {
			*stream.offset += uint64(len(decoded))
		}
	}
	return nil
}

func feedSandboxStdin(ctx context.Context, target sandboxTarget, cmdUUID string, in io.Reader) {
	write := func(data string, eof bool) error {
		req := sandbox.CommandStdinRequest{Data: data, Eof: &eof}
		_, err := target.client.WriteCommandStdin(ctx, target.instance, cmdUUID, &req, target.opts...)
		return err
	}

	buf := make([]byte, 32*1024)
	for {
		if ctx.Err() != nil {
			return
		}

		n, readErr := in.Read(buf)
		if n > 0 {
			if err := write(base64.StdEncoding.EncodeToString(buf[:n]), false); err != nil {
				log.G(ctx).Warn().Err(err).Str("cmd", cmdUUID).Msg("failed to send standard input to the command")
				return
			}
		}

		if readErr != nil {
			if ctx.Err() != nil {
				return
			}
			if err := write("", true); err != nil {
				log.G(ctx).Warn().Err(err).Str("cmd", cmdUUID).Msg("failed to close the command's standard input")
			}
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
	info, err := os.Stat(c.Local)
	if err != nil {
		return fmt.Errorf("reading local file %q: %w", c.Local, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%q is a directory: only single files can be written", c.Local)
	}

	target, err := resolveSandboxTarget(ctx, c.Target, c.Plugin)
	if err != nil {
		return err
	}

	opts := uploadOpts{
		local:      c.Local,
		remote:     c.Remote,
		appendFile: c.Append,
		parents:    c.Parents,
	}

	remote, err := uploadSandboxFile(ctx, target, opts)
	if err != nil {
		return err
	}

	log.G(ctx).Info().
		Str("source", c.Local).
		Str("remote", remote).
		Msg("file written")
	return nil
}

type uploadOpts struct {
	local      string
	remote     string
	appendFile bool
	parents    bool
}

func uploadSandboxFile(ctx context.Context, target sandboxTarget, opts uploadOpts) (string, error) {
	data, err := os.ReadFile(opts.local)
	if err != nil {
		return "", fmt.Errorf("reading local file %q: %w", opts.local, err)
	}

	if opts.parents {
		if err := mkdirSandboxInstance(ctx, target, path.Dir(opts.remote), true); err != nil {
			return "", fmt.Errorf("creating parent directories: %w", err)
		}
	}

	remote := opts.remote
	if err := writeSandboxFile(ctx, target, remote, data, opts.appendFile); err != nil {
		if !strings.Contains(err.Error(), "Is a directory") {
			return "", err
		}
		remote = path.Join(remote, filepath.Base(opts.local))
		if err := writeSandboxFile(ctx, target, remote, data, opts.appendFile); err != nil {
			return "", err
		}
	}

	return remote, nil
}

func writeSandboxFile(ctx context.Context, target sandboxTarget, remotePath string, data []byte, appendFile bool) error {
	log.G(ctx).Trace().Msg("writing file")

	req := sandbox.WriteFileRequest{
		Path:     remotePath,
		Append:   appendFile,
		Encoding: sandbox.FileEncodingBase64,
		Data:     base64.StdEncoding.EncodeToString(data),
	}
	if _, err := target.client.WriteFile(ctx, target.instance, &req, target.opts...); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
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
	target, err := resolveSandboxTarget(ctx, c.Target, c.Plugin)
	if err != nil {
		return err
	}

	opts := downloadOpts{
		remote: c.Remote,
		local:  c.Local,
		force:  c.Force,
	}

	local, size, err := downloadSandboxFile(ctx, target, opts)
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
	remote string
	local  string
	force  bool
}

func downloadSandboxFile(ctx context.Context, target sandboxTarget, opts downloadOpts) (string, int, error) {
	data, err := readSandboxFile(ctx, target, opts.remote)
	if err != nil {
		return "", 0, err
	}

	local := opts.local
	if local == "" {
		local = path.Base(opts.remote)
	} else if info, err := os.Stat(local); err == nil && info.IsDir() {
		local = filepath.Join(local, path.Base(opts.remote))
	}

	if !opts.force {
		if _, err := os.Stat(local); err == nil {
			return "", 0, fmt.Errorf("local file %q already exists (use --force to overwrite)", local)
		}
	}

	if err := os.WriteFile(local, data, 0o644); err != nil {
		return "", 0, fmt.Errorf("writing local file %q: %w", local, err)
	}

	return local, len(data), nil
}

func readSandboxFile(ctx context.Context, target sandboxTarget, remotePath string) ([]byte, error) {
	log.G(ctx).Trace().Msg("reading file")

	req := sandbox.ReadFileRequest{
		Path: remotePath,
	}
	resp, err := target.client.ReadFile(ctx, target.instance, &req, target.opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("failed to read file: the %q plugin returned no contents", target.plugin)
	}

	return decodeSandboxPayload(resp.Data.Contents), nil
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

		target, err := resolveSandboxTarget(ctx, dstTarget, c.Plugin)
		if err != nil {
			return err
		}

		opts := uploadOpts{
			local:   srcPath,
			remote:  dstPath,
			parents: c.Parents,
		}

		remote, err := uploadSandboxFile(ctx, target, opts)
		if err != nil {
			return err
		}

		log.G(ctx).Info().
			Str("source", srcPath).
			Str("remote", remote).
			Msg("file written")
		return nil

	default:
		target, err := resolveSandboxTarget(ctx, srcTarget, c.Plugin)
		if err != nil {
			return err
		}

		opts := downloadOpts{
			remote: srcPath,
			local:  dstPath,
			force:  c.Force,
		}

		local, size, err := downloadSandboxFile(ctx, target, opts)
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
	target, err := resolveSandboxTarget(ctx, c.Target, c.Plugin)
	if err != nil {
		return err
	}
	if err := mkdirSandboxInstance(ctx, target, c.Path, c.Parents); err != nil {
		return err
	}

	fmt.Fprintf(stdio.Stdout, "created directory %q\n", c.Path)
	return nil
}

func mkdirSandboxInstance(ctx context.Context, target sandboxTarget, path string, parents bool) error {
	log.G(ctx).Trace().Msg("creating directory")

	req := sandbox.MkdirRequest{
		Path:    path,
		Parents: parents,
	}
	if _, err := target.client.CreateDirectory(ctx, target.instance, &req, target.opts...); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	return nil
}
