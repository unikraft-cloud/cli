// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MakeNowJust/heredoc"
	"github.com/alecthomas/kong"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/cli/internal/sandbox"
	"unikraft.com/cli/internal/tui/selector"
	xio "unikraft.com/x/io"
	"unikraft.com/x/shell"
)

const copyPathSeparator = ":"

var sandboxKongVars = kong.Vars{
	"sandbox_plugin":       sandbox.PluginName,
	"sandbox_plugin_image": sandbox.DefaultImage,
}

type ExecSandboxInstanceCmd struct {
	Target string   `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instance to run the command on."`
	Cmd    []string `arg:"" name:"command" help:"Command to pass to the instance." placeholder:"cmd"`

	Plugin      string   `name:"plugin-name" default:"${sandbox_plugin}" help:"Name of the sandbox plugin to use." placeholder:"name"`
	PluginImage string   `name:"plugin-image" default:"${sandbox_plugin_image}" help:"Image of the sandbox plugin to attach when the instance has none." placeholder:"ref"`
	Dir         string   `name:"dir" short:"w" help:"Directory to execute the command from." placeholder:"dir"`
	Env         []string `name:"env" short:"e" sep:"none" help:"Environment variable." placeholder:"<key>=<value>" example:"DEBUG=true"`
}

func (ExecSandboxInstanceCmd) Help() string {
	return heredoc.Docf(`
		Execute a command on an instance which has the sandbox plugin
		loaded.

		The environment given with %[1]s--env%[1]s is the whole environment the
		command sees, not an addition to it.

		Standard input is forwarded to the command only when it is
		redirected from a file or a pipe. Run from a terminal, the command
		sees an empty standard input and cannot be typed at.
	`, "`")
}

func (cmd ExecSandboxInstanceCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Execute a command on an instance",
			Commands:    []string{"unikraft instance exec my-instance -- echo hello"},
		},
		{
			Description: "Execute a command in a specific working directory",
			Commands:    []string{"unikraft instance exec my-instance --dir /var/lib/app -- ls -la"},
		},
		{
			Description: "Execute a command with environment variables set",
			Commands:    []string{"unikraft instance exec my-instance -e DEBUG=true -- ./start.sh"},
		},
	}
}

func (c *ExecSandboxInstanceCmd) Run(ctx context.Context, stdio config.Stdio, partition *resource.Partition) error {
	if _, err := parseEnv(c.Env); err != nil {
		return err
	}

	target, err := resolveSandboxTarget(ctx, stdio, partition, c.Target, c.Plugin, c.PluginImage)
	if err != nil {
		return err
	}

	return c.runOn(ctx, target, stdio)
}

// runOn runs the command on target and reports what it ended as.
func (c *ExecSandboxInstanceCmd) runOn(ctx context.Context, target sandbox.Target, stdio config.Stdio) error {
	in := stdio.Stdin
	if in == nil || xio.IsTTYReader(in) {
		in = strings.NewReader("")
	}

	out := xio.Unwrap(stdio.Stdout)

	cmd := target.CommandArgs(ctx, c.Cmd)
	cmd.Dir = c.Dir
	cmd.Env = c.Env
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = out

	code, err := shell.ExitStatus(cmd.Run())
	if err != nil {
		return err
	}
	if code != 0 {
		return ExitStatus(code)
	}
	return nil
}

// parseEnv is the --env records as the environment they set, or which of them
// is not <key>=<value>.
func parseEnv(records []string) (map[string]string, error) {
	for _, record := range records {
		if !strings.Contains(record, "=") {
			return nil, fmt.Errorf("parsing --env: %q is not <key>=<value>", record)
		}
	}
	env, err := value.Parse[map[string]string](records)
	if err != nil {
		return nil, fmt.Errorf("parsing --env: %w", err)
	}
	return env, nil
}

// resolveSandboxTarget is the instance target names, ready to be addressed
// through plugin. An instance without the plugin is offered it first, when
// there is someone at the terminal to ask; image is what is attached then.
func resolveSandboxTarget(ctx context.Context, stdio config.Stdio, partition *resource.Partition, target, plugin, image string) (sandbox.Target, error) {
	instance, err := lookupInstance(ctx, partition, target)
	if err != nil {
		return sandbox.Target{}, err
	}

	if cause := missingPlugin(instance, plugin); cause != nil {
		if err := offerAttachPlugin(ctx, stdio, instance, plugin, image, cause); err != nil {
			return sandbox.Target{}, err
		}
		// Attached and started: see what the instance became.
		if instance, err = lookupInstance(ctx, partition, target); err != nil {
			return sandbox.Target{}, err
		}
		if err := missingPlugin(instance, plugin); err != nil {
			return sandbox.Target{}, fmt.Errorf("after attaching: %w", err)
		}
	}

	if !instance.State.IsRunning() {
		return sandbox.Target{}, fmt.Errorf("instance %q is not running (state: %s)", instance.Name, string(instance.State))
	}

	return sandboxTargetFor(ctx, instance, plugin)
}

// lookupInstance is the one instance target names.
func lookupInstance(ctx context.Context, partition *resource.Partition, target string) (Instance, error) {
	key := multimetro.ParseKey(target)

	gettable := partition.WrapGettable(Instance{})
	resources, err := gettable.Get(ctx, []string{key.String()})
	if err != nil {
		return Instance{}, err
	}
	if len(resources) == 0 {
		return Instance{}, fmt.Errorf("instance %q not found", target)
	}
	if len(resources) > 1 {
		var keys []string
		for _, res := range resources {
			keys = append(keys, res.Key().String())
		}
		return Instance{}, fmt.Errorf("ambiguous instance: %s (found %v)", target, keys)
	}

	instance, ok := resources[0].(Instance)
	if !ok {
		return Instance{}, fmt.Errorf("%q is not an instance", target)
	}
	return instance, nil
}

// missingPlugin is why instance cannot be addressed through plugin, or nil
// when it has the plugin loaded.
func missingPlugin(instance Instance, plugin string) error {
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
		return fmt.Errorf("instance %q has no plugins loaded", instance.Name)
	}
	return fmt.Errorf("instance %q has no plugin named %q; it has: %s", instance.Name, plugin, strings.Join(loaded, ", "))
}

// sandboxTargetFor is instance addressed through plugin, in its metro.
func sandboxTargetFor(ctx context.Context, instance Instance, plugin string) (sandbox.Target, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return sandbox.Target{}, err
	}

	metro := instance.Key().(multimetro.Key).Metro
	return group.CollectMetro(ctx, g, metro, func(ctx context.Context, c multimetro.MetroClient) (sandbox.Target, error) {
		sb := c.Sandbox(plugin)
		if sb.Client == nil {
			return sandbox.Target{}, fmt.Errorf("metro %q has no plugin client for %q", metro, plugin)
		}
		return sandbox.Target{
			Client:   sb.Client,
			Instance: instance.Instance,
			Plugin:   plugin,
			Opts:     sb.Opts,
		}, nil
	})
}

// attachHint is how plugin would be attached to instance by hand.
func attachHint(instance Instance, plugin, image string) string {
	return fmt.Sprintf("attach it with `unikraft instance edit %s --plugin name=%s,image=%s` while the instance is stopped",
		instance.Key().String(), plugin, image)
}

// offerAttachPlugin asks whether to attach the plugin instance is missing, and
// attaches image as it on a yes. cause is why the plugin is missing: it is the
// error when the question cannot be asked, or is answered no.
func offerAttachPlugin(ctx context.Context, stdio config.Stdio, instance Instance, plugin, image string, cause error) error {
	if slices.Contains(instance.Instance.Features, platform.InstanceFeatureDeleteOnStop) {
		// Attaching means stopping, and stopping this one deletes it.
		return fmt.Errorf("%w\nhint: the instance is deleted when it stops, so the plugin cannot be attached in place; recreate it with --plugin name=%s,image=%s", cause, plugin, image)
	}
	if !xio.IsTTYReader(stdio.Stdin) || !xio.IsTTY(stdio.Stderr) {
		return fmt.Errorf("%w\nhint: %s", cause, attachHint(instance, plugin, image))
	}

	verb := "start"
	if instance.State.IsRunning() {
		verb = "restart"
	}
	question := fmt.Sprintf("instance %q has no plugin %q; attach %s and %s the instance", instance.Name, plugin, image, verb)
	yes, err := selector.Confirm(ctx, stdio.Stdin, xio.Unwrap(stdio.Stderr), question)
	switch {
	case errors.Is(err, selector.ErrNoOptionSelected):
		yes = false
	case err != nil:
		return err
	}
	if !yes {
		return cause
	}
	return attachInstancePlugin(ctx, instance, &InstancePlugin{Name: plugin, Image: image})
}

// attachInstancePlugin adds p to instance's plugins, stopping the instance
// first if it runs, and starts it.
func attachInstancePlugin(ctx context.Context, instance Instance, p *InstancePlugin) error {
	keys := multimetro.Keys{instance.Key().(multimetro.Key)}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}

	if instance.State.IsRunning() {
		if _, err := stopInstances(ctx, g, keys, StopOpts{DrainTimeout: -1}); err != nil {
			return fmt.Errorf("stopping instance %q: %w", instance.Name, err)
		}
	}

	prop, value, err := instancePatchSpec("plugins", patchOpAdd, []*InstancePlugin{p})
	if err != nil {
		return err
	}
	err = group.DoRefs(ctx, g, keys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		reqs := make([]platform.UpdateInstancesRequestItem, 0, len(refs))
		for _, ref := range refs {
			req := platform.UpdateInstancesRequestItem{
				Op:    platform.MutableInstanceOperationAdd,
				Prop:  prop,
				Value: new(value),
			}
			if ref.UUID != "" {
				req.Uuid = &ref.UUID
			} else {
				req.Name = &ref.Name
			}
			reqs = append(reqs, req)
		}
		log.G(ctx).Trace().Str("plugin", p.Name).Msg("attaching plugin")
		if _, err := c.UpdateInstances(ctx, reqs); err != nil {
			return nil, err
		}
		return refs, nil
	})
	if err != nil {
		return fmt.Errorf("attaching plugin %q: %w", p.Name, err)
	}

	if _, err := startInstances(ctx, g, keys); err != nil {
		return fmt.Errorf("starting instance %q: %w", instance.Name, err)
	}
	return nil
}

type WriteSandboxInstanceCmd struct {
	Target string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instance to write the file to."`
	Local  string `arg:"" name:"local" help:"Local file path to read from."`
	Remote string `arg:"" name:"remote" help:"Remote destination path on the instance."`

	Plugin      string `name:"plugin-name" default:"${sandbox_plugin}" help:"Name of the sandbox plugin to use." placeholder:"name"`
	PluginImage string `name:"plugin-image" default:"${sandbox_plugin_image}" help:"Image of the sandbox plugin to attach when the instance has none." placeholder:"ref"`
	Append      bool   `name:"append" help:"Append to the remote file instead of overwriting it."`
	Parents     bool   `name:"parents" short:"p" help:"Create missing parent directories on the remote path."`
}

func (WriteSandboxInstanceCmd) Help() string {
	return heredoc.Doc(`
		Write a single local file to an instance which has the sandbox
		plugin loaded.
	`)
}

func (cmd WriteSandboxInstanceCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Write a local file to an instance",
			Commands:    []string{"unikraft instance write my-instance ./config.json /etc/app.json"},
		},
		{
			Description: "Write a file, creating parent directories as needed",
			Commands:    []string{"unikraft instance write my-instance ./data.bin /var/lib/app.bin -p"},
		},
		{
			Description: "Append to a file on an instance",
			Commands:    []string{"unikraft instance write my-instance ./extra.log /var/log/app.log --append"},
		},
	}
}

func (c *WriteSandboxInstanceCmd) Run(ctx context.Context, stdio config.Stdio, partition *resource.Partition) error {
	if err := sandbox.StatLocalFile(c.Local, "written"); err != nil {
		return err
	}

	target, err := resolveSandboxTarget(ctx, stdio, partition, c.Target, c.Plugin, c.PluginImage)
	if err != nil {
		return err
	}

	opts := sandbox.UploadOpts{
		Local:   c.Local,
		Remote:  c.Remote,
		Append:  c.Append,
		Parents: c.Parents,
	}

	remote, err := target.Upload(ctx, opts)
	if err != nil {
		return err
	}

	log.G(ctx).Info().
		Str("source", c.Local).
		Str("remote", remote).
		Msg("file written")
	return nil
}

type ReadSandboxInstanceCmd struct {
	Target string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instance to read the file from."`
	Remote string `arg:"" name:"remote" help:"Remote file path to read."`
	Local  string `arg:"" name:"local" optional:"" help:"Local destination path. Defaults to the remote file's base name."`

	Plugin      string `name:"plugin-name" default:"${sandbox_plugin}" help:"Name of the sandbox plugin to use." placeholder:"name"`
	PluginImage string `name:"plugin-image" default:"${sandbox_plugin_image}" help:"Image of the sandbox plugin to attach when the instance has none." placeholder:"ref"`
	Force       bool   `name:"force" help:"Overwrite the local file if it already exists."`
	Parents     bool   `name:"parents" short:"p" help:"Create missing parent directories on the local path."`
}

func (ReadSandboxInstanceCmd) Help() string {
	return heredoc.Doc(`
		Read a single file from an instance which has the sandbox plugin
		loaded.

		The local file is always written as a regular file. Permissions are
		not carried across from the instance.
	`)
}

func (cmd ReadSandboxInstanceCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Read a file from an instance",
			Commands:    []string{"unikraft instance read my-instance /etc/app.json ./config.json"},
		},
		{
			Description: "Read a file into the current directory",
			Commands:    []string{"unikraft instance read my-instance /var/log/app.log"},
		},
		{
			Description: "Read a file, overwriting the local file if it exists",
			Commands:    []string{"unikraft instance read my-instance /var/log/app.log --force"},
		},
	}
}

func (c *ReadSandboxInstanceCmd) Run(ctx context.Context, stdio config.Stdio, partition *resource.Partition) error {
	target, err := resolveSandboxTarget(ctx, stdio, partition, c.Target, c.Plugin, c.PluginImage)
	if err != nil {
		return err
	}

	opts := sandbox.DownloadOpts{
		Remote:  c.Remote,
		Local:   c.Local,
		Force:   c.Force,
		Parents: c.Parents,
	}

	local, size, err := target.Download(ctx, opts)
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

	Plugin      string `name:"plugin-name" default:"${sandbox_plugin}" help:"Name of the sandbox plugin to use." placeholder:"name"`
	PluginImage string `name:"plugin-image" default:"${sandbox_plugin_image}" help:"Image of the sandbox plugin to attach when the instance has none." placeholder:"ref"`
	Force       bool   `name:"force" help:"Overwrite the local file if it already exists."`
	Parents     bool   `name:"parents" short:"p" help:"Create missing parent directories on the destination path."`
}

func (CopySandboxInstanceCmd) Help() string {
	return heredoc.Docf(`
		Copy a single file to or from an instance which has the sandbox
		plugin loaded.

		A path on an instance is written as %[1]s<instance>:<path>%[1]s, optionally
		prefixed with a metro as %[1]s<metro>/<instance>:<path>%[1]s. Exactly one of
		the two paths may name an instance.

		Copying to an instance always overwrites the remote file. Copying
		from one writes a regular local file, without the permissions it
		had on the instance.
	`, "`")
}

func (cmd CopySandboxInstanceCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Copy a local file to an instance",
			Commands:    []string{"unikraft instance copy ./config.json my-instance:/etc/app.json"},
		},
		{
			Description: "Copy a file off an instance",
			Commands:    []string{"unikraft instance copy my-instance:/var/log/app.log ./app.log"},
		},
		{
			Description: "Copy a file into a directory, keeping its name",
			Commands:    []string{"unikraft instance copy my-instance:/var/log/app.log ./logs/"},
		},
		{
			Description: "Copy a file to an instance in a specific metro",
			Commands:    []string{"unikraft instance copy ./data.bin fra/my-instance:/var/lib/app.bin"},
		},
	}
}

func (c *CopySandboxInstanceCmd) Run(ctx context.Context, stdio config.Stdio, partition *resource.Partition) error {
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

		if err := sandbox.StatLocalFile(srcPath, "copied"); err != nil {
			return err
		}

		target, err := resolveSandboxTarget(ctx, stdio, partition, dstTarget, c.Plugin, c.PluginImage)
		if err != nil {
			return err
		}

		opts := sandbox.UploadOpts{
			Local:   srcPath,
			Remote:  dstPath,
			Parents: c.Parents,
		}

		remote, err := target.Upload(ctx, opts)
		if err != nil {
			return err
		}

		log.G(ctx).Info().
			Str("source", srcPath).
			Str("remote", remote).
			Msg("file written")
		return nil

	default:
		target, err := resolveSandboxTarget(ctx, stdio, partition, srcTarget, c.Plugin, c.PluginImage)
		if err != nil {
			return err
		}

		opts := sandbox.DownloadOpts{
			Remote:  srcPath,
			Local:   dstPath,
			Force:   c.Force,
			Parents: c.Parents,
		}

		local, size, err := target.Download(ctx, opts)
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
