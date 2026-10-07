// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/alecthomas/kong"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/plugins/sandbox"
	xio "unikraft.com/x/io"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/tui/selector"
)

const (
	defaultPluginImage = "plugins/sandbox:latest"

	startHint  = "%[1]s it with `unikraft instance %[1]s %[2]s`"
	attachHint = "attach it with `unikraft instance edit %s --plugin name=%s,image=%s` while the instance is stopped"
)

const (
	attachDeclined = iota
	attachAndStart
	attachOnly
)

var sandboxKongVars = kong.Vars{
	"sandbox_plugin":       sandbox.PluginName,
	"sandbox_plugin_image": defaultPluginImage,
}

type SandboxPluginOpts struct {
	PluginName  string `name:"plugin-name" default:"${sandbox_plugin}" help:"Name of the sandbox plugin to use." placeholder:"name"`
	PluginImage string `name:"plugin-image" default:"${sandbox_plugin_image}" help:"Image of the sandbox plugin to attach when the instance has none." placeholder:"ref"`
}

func attachPlugin(ctx context.Context, stdio config.Stdio, partition *resource.Partition, instance Instance, opts SandboxPluginOpts) error {
	cause := missingPlugin(instance, opts.PluginName)
	if cause == nil {
		return nil
	}
	if update, ok := pendingPlugin(instance, opts.PluginName); ok {
		return tryLoadPlugin(ctx, stdio, instance, opts.PluginName, update)
	}
	return tryAttachPlugin(ctx, stdio, partition, instance, opts, cause)
}

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

func pendingPlugin(instance Instance, plugin string) (platform.InstancePendingUpdate, bool) {
	for _, update := range instance.Instance.Updates {
		if update.Prop != platform.MutableInstancePropertyPlugins {
			continue
		}
		switch update.Op {
		case platform.MutableInstanceOperationSet, platform.MutableInstanceOperationAdd:
		default:
			continue
		}
		plugins, _ := update.Value.([]any)
		if slices.ContainsFunc(plugins, func(p any) bool {
			fields, _ := p.(map[string]any)
			name, _ := fields["name"].(string)
			return name == plugin
		}) {
			return update, true
		}
	}
	return platform.InstancePendingUpdate{}, false
}

func tryAttachPlugin(ctx context.Context, stdio config.Stdio, partition *resource.Partition, instance Instance, opts SandboxPluginOpts, cause error) error {
	plugin, image := opts.PluginName, opts.PluginImage
	if slices.Contains(instance.Instance.Features, platform.InstanceFeatureDeleteOnStop) {
		return fmt.Errorf("%w\nhint: the instance is deleted when it stops, so the plugin cannot be attached in place; recreate it with --plugin name=%s,image=%s", cause, plugin, image)
	}
	if !xio.IsTTYReader(stdio.Stdin) || !xio.IsTTY(stdio.Stdout) {
		return fmt.Errorf("%w\nhint: %s", cause, fmt.Sprintf(attachHint, instance.Key().String(), plugin, image))
	}

	verb := "start"
	if instance.State.IsRunning() {
		verb = "restart"
	}
	question := fmt.Sprintf("instance %q has no plugin %q; attach %s", instance.Name, plugin, image)
	options := []string{
		"no",
		"attach it and " + verb + " the instance",
		"attach it and don't " + verb + " the instance",
	}
	picked, err := selector.Single(question, options...)
	switch {
	case errors.Is(err, selector.ErrNoOptionSelected):
		picked = options[attachDeclined]
	case err != nil:
		return err
	}

	switch slices.Index(options, picked) {
	case attachAndStart:
		return attachInstancePlugin(ctx, stdio, partition, instance, opts, true)
	case attachOnly:
		if err := attachInstancePlugin(ctx, stdio, partition, instance, opts, false); err != nil {
			return err
		}
		log.G(ctx).Info().Msgf("plugin %q attached to instance %q; %s to load it", plugin, instance.Name, fmt.Sprintf(startHint, verb, instance.Key().String()))
		return ExitStatus(1)
	default:
		return ExitStatus(1)
	}
}

func tryLoadPlugin(ctx context.Context, stdio config.Stdio, instance Instance, plugin string, update platform.InstancePendingUpdate) error {
	if update.Status == platform.InstancePendingUpdateStatusFailed {
		return fmt.Errorf("instance %q has plugin %q attached, but applying it failed: %s\nhint: detach it with `unikraft instance edit %s --del plugins=%s` and attach it again",
			instance.Name, plugin, ptr.ZeroIfNil(update.Error), instance.Key().String(), plugin)
	}

	verb := "start"
	if instance.State.IsRunning() {
		verb = "restart"
	}
	cause := fmt.Errorf("instance %q has plugin %q attached but not loaded yet", instance.Name, plugin)
	if !xio.IsTTYReader(stdio.Stdin) || !xio.IsTTY(stdio.Stdout) {
		return fmt.Errorf("%w\nhint: %s to load it", cause, fmt.Sprintf(startHint, verb, instance.Key().String()))
	}

	question := fmt.Sprintf("instance %q has plugin %q attached but not loaded yet; %s the instance to load it", instance.Name, plugin, verb)
	answer, err := selector.Single(question, "no", "yes")
	switch {
	case errors.Is(err, selector.ErrNoOptionSelected):
		answer = "no"
	case err != nil:
		return err
	}
	if answer != "yes" {
		return ExitStatus(1)
	}
	return restartInstance(ctx, stdio, instance)
}

func restartInstance(ctx context.Context, stdio config.Stdio, instance Instance) error {
	stdio.Stdout = io.Discard
	quiet := cmd.FormatOpts{Output: cmd.Printer{Type: cmd.PrinterTypeQuiet}}
	targets := []string{instance.Key().String()}

	if instance.State.IsRunning() {
		restart := InstancesRestartCmd{Targets: targets, DrainTimeout: -1, FormatOpts: quiet}
		return restart.Run(ctx, stdio)
	}
	start := InstancesStartCmd{Targets: targets, FormatOpts: quiet}
	return start.Run(ctx, stdio)
}

func attachInstancePlugin(ctx context.Context, stdio config.Stdio, partition *resource.Partition, instance Instance, opts SandboxPluginOpts, restart bool) error {
	stdio.Stdout = io.Discard
	edit := cmd.ResourceEditCmd[Instance]{
		Target: instance.Key().String(),
		Add:    []map[string]string{{"plugins": fmt.Sprintf("name=%s,image=%s", opts.PluginName, opts.PluginImage)}},
		Output: cmd.Printer{Type: cmd.PrinterTypeQuiet},
	}
	if err := edit.Run(ctx, stdio, partition); err != nil {
		return fmt.Errorf("attaching plugin %q: %w", opts.PluginName, err)
	}

	if restart {
		return restartInstance(ctx, stdio, instance)
	}
	return nil
}
