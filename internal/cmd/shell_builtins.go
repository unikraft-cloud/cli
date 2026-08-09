// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
	rescmd "unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/cli/internal/shell"
	"unikraft.com/cli/internal/tabwriter"
	"unikraft.com/cli/internal/types"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"
)

// ShellCmd is the root parser for the interactive shell.
type ShellCmd struct {
	Get     ShellGetCmd     `cmd:"" help:"Inspect the current instance"`
	Help    ShellHelpCmd    `cmd:"" help:"Show available commands"`
	Edit    ShellEditCmd    `cmd:"" help:"Edit instance fields (env, args, memory, vcpus, tags)"`
	Volumes ShellVolumesCmd `cmd:"" help:"Manage volumes"`
	Mount   ShellMountCmd   `cmd:"" help:"Attach a volume to this instance"`
	Unmount ShellUnmountCmd `cmd:"" help:"Detach a volume from this instance"`
	Start   ShellStartCmd   `cmd:"" help:"Start the instance"`
	Stop    ShellStopCmd    `cmd:"" help:"Stop the instance"`
	Suspend ShellSuspendCmd `cmd:"" help:"Suspend the instance"`
	Restart ShellRestartCmd `cmd:"" help:"Restart the instance"`
	History ShellHistoryCmd `cmd:"" help:"Show and manage command history"`
}

type ShellHelpCmd struct{}

func (c *ShellHelpCmd) Run(sctx *ShellContext) error {
	builtinHelp(sctx.Out)
	return nil
}

type ShellGetCmd struct{}

func (c *ShellGetCmd) Run(sctx *ShellContext) error {
	resources, err := Instance{}.Get(sctx.Ctx, []string{sctx.Key.String()})
	if err != nil {
		return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), err)
	}
	if len(resources) == 0 {
		return fmt.Errorf("%s", shell.ShellErrorStyle.Render("instance not found"))
	}
	rescmd.Printer{}.WithDefault(rescmd.PrinterTypeKeyValue).
		Print(sctx.Ctx, sctx.Out, nil, Instance{}, resources...)
	return nil
}

type ShellEditCmd struct {
	Show   ShellEditShowCmd   `cmd:"" default:"1" hidden:""`
	Env    ShellEditEnvCmd    `cmd:"" help:"Set environment variable"`
	Args   ShellEditArgsCmd   `cmd:"" help:"Set arguments"`
	Memory ShellEditMemoryCmd `cmd:"" help:"Set memory (e.g. 128MiB)"`
	Vcpus  ShellEditVcpusCmd  `cmd:"" help:"Set vCPU count"`
	Tags   ShellEditTagsCmd   `cmd:"" help:"Add a tag"`
}

type ShellEditShowCmd struct{}

func (c *ShellEditShowCmd) Run(sctx *ShellContext) error {
	resources, err := Instance{}.Get(sctx.Ctx, []string{sctx.Key.String()})
	if err != nil {
		return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), err)
	}
	if len(resources) == 0 {
		return fmt.Errorf("%s", shell.ShellErrorStyle.Render("instance not found"))
	}

	instance := resources[0].(Instance)
	if len(instance.Tags) > 0 {
		fmt.Fprintf(sctx.Out, "  %s %s\n", shell.ShellLabelStyle.Render("tags:"), shell.ShellValueStyle.Render(strings.Join(instance.Tags, ", ")))
	}
	if instance.Image.Reference != nil {
		fmt.Fprintf(sctx.Out, "  %s %s\n", shell.ShellLabelStyle.Render("image:"), shell.ShellValueStyle.Render(instance.Image.Reference.String()))
	}
	if len(instance.Runtime.Args) > 0 {
		fmt.Fprintf(sctx.Out, "  %s %s\n", shell.ShellLabelStyle.Render("args:"), shell.ShellValueStyle.Render(strings.Join(instance.Runtime.Args, " ")))
	}
	if len(instance.Runtime.Env) > 0 {
		fmt.Fprintf(sctx.Out, "  %s\n", shell.ShellLabelStyle.Render("env:"))
		for k, v := range instance.Runtime.Env {
			fmt.Fprintf(sctx.Out, "    %s=%s\n", shell.ShellDirStyle.Render(k), shell.ShellValueStyle.Render(v))
		}
	}
	if instance.Resources.Memory > 0 {
		memStr, _ := value.Render(instance.Resources.Memory, value.RenderOpts{})
		fmt.Fprintf(sctx.Out, "  %s %s\n", shell.ShellLabelStyle.Render("memory:"), shell.ShellValueStyle.Render(memStr))
	}
	if instance.Resources.VCPUs > 0 {
		fmt.Fprintf(sctx.Out, "  %s %s\n", shell.ShellLabelStyle.Render("vcpus:"), shell.ShellValueStyle.Render(strconv.Itoa(instance.Resources.VCPUs)))
	}
	return nil
}

type ShellEditEnvCmd struct {
	KeyValue []string `arg:"" name:"KEY=VALUE" help:"Environment variable to set"`
}

func (c *ShellEditEnvCmd) Run(sctx *ShellContext) error {
	fieldValue := strings.Join(c.KeyValue, " ")
	k, v, ok := parseAssignment(fieldValue)
	if !ok {
		return fmt.Errorf("%s edit env: expected KEY=VALUE", shell.ShellErrorStyle.Render("error:"))
	}
	fields := []resource.Field{{Name: "runtime", Subfields: []resource.Field{{Name: "env", Edit: &resource.Patch{Add: map[string]string{k: v}}}}}}
	return applyEditPatch(sctx, "env", fieldValue, fields)
}

type ShellEditArgsCmd struct {
	Arg []string `arg:"" name:"ARG" help:"Arguments to set"`
}

func (c *ShellEditArgsCmd) Run(sctx *ShellContext) error {
	fieldValue := strings.Join(c.Arg, " ")
	fields := []resource.Field{{Name: "runtime", Subfields: []resource.Field{{Name: "args", Edit: &resource.Patch{Set: InstanceArgs(strings.Fields(fieldValue))}}}}}
	return applyEditPatch(sctx, "args", fieldValue, fields)
}

type ShellEditMemoryCmd struct {
	Size string `arg:"" name:"SIZE" help:"Memory size (e.g. 128MiB)"`
}

func (c *ShellEditMemoryCmd) Run(sctx *ShellContext) error {
	var mem types.SizeMebibytes
	if err := mem.UnmarshalText([]byte(c.Size)); err != nil {
		return fmt.Errorf("%s invalid memory size: %v", shell.ShellErrorStyle.Render("error:"), err)
	}
	fields := []resource.Field{{Name: "resources", Subfields: []resource.Field{{Name: "memory", Edit: &resource.Patch{Set: mem}}}}}
	return applyEditPatch(sctx, "memory", c.Size, fields)
}

type ShellEditVcpusCmd struct {
	N int `arg:"" name:"N" help:"Number of vCPUs"`
}

func (c *ShellEditVcpusCmd) Run(sctx *ShellContext) error {
	if c.N < 1 {
		return fmt.Errorf("%s edit vcpus: expected a positive integer", shell.ShellErrorStyle.Render("error:"))
	}
	fields := []resource.Field{{Name: "resources", Subfields: []resource.Field{{Name: "vcpus", Edit: &resource.Patch{Set: c.N}}}}}
	return applyEditPatch(sctx, "vcpus", strconv.Itoa(c.N), fields)
}

type ShellEditTagsCmd struct {
	Tag []string `arg:"" name:"TAG" help:"Tag to add"`
}

func (c *ShellEditTagsCmd) Run(sctx *ShellContext) error {
	fieldValue := strings.Join(c.Tag, " ")
	fields := []resource.Field{{Name: "tags", Edit: &resource.Patch{Add: []string{fieldValue}}}}
	return applyEditPatch(sctx, "tags", fieldValue, fields)
}

func applyEditPatch(sctx *ShellContext, fieldName, fieldValue string, fields []resource.Field) error {
	patches, err := patchRequests(fields, instancePatchSpec)
	if err != nil {
		return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), err)
	}
	if err := instanceApplyPatches(sctx.Ctx, sctx.G, sctx.Key, patches); err != nil {
		return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), err)
	}
	fmt.Fprintf(sctx.Out, "  %s %s=%s\n", shell.ShellValueStyle.Render("✓"), shell.ShellDirStyle.Render(fieldName), shell.ShellValueStyle.Render(fieldValue))
	fmt.Fprintln(sctx.Out, shell.ShellHintStyle.Render("  Run 'restart' for changes to take effect."))
	return nil
}

type ShellVolumesCmd struct {
	Mounted ShellVolumesMountedCmd `cmd:"" default:"1" help:"List volumes mounted on this instance"`
	List    ShellVolumesListCmd    `cmd:"" help:"List all available volumes"`
	Create  ShellVolumesCreateCmd  `cmd:"" help:"Create a new volume"`
}

type ShellVolumesMountedCmd struct{}

func (c *ShellVolumesMountedCmd) Run(sctx *ShellContext) error {
	resources, err := Instance{}.Get(sctx.Ctx, []string{sctx.Key.String()})
	if err != nil {
		return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), err)
	}
	if len(resources) == 0 {
		return fmt.Errorf("%s", shell.ShellErrorStyle.Render("instance not found"))
	}

	instance := resources[0].(Instance)
	if len(instance.Volumes) == 0 {
		fmt.Fprintln(sctx.Out, shell.ShellHintStyle.Render("No volumes mounted."))
		return nil
	}

	for _, vol := range instance.Volumes {
		name := cmp.Or(vol.Name, vol.UUID)
		flags := ""
		if vol.Readonly {
			flags = " (ro)"
		}
		fmt.Fprintf(sctx.Out, "  %s %s → %s%s\n", shell.ShellLabelStyle.Render("■"), shell.ShellValueStyle.Render(name), shell.ShellDirStyle.Render(vol.At), flags)
	}
	return nil
}

type ShellVolumesListCmd struct{}

func (c *ShellVolumesListCmd) Run(sctx *ShellContext) error {
	volumes, err := Volume{}.List(sctx.Ctx)
	if err != nil {
		return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), err)
	}
	if len(volumes) == 0 {
		fmt.Fprintln(sctx.Out, shell.ShellHintStyle.Render("No volumes found."))
		return nil
	}
	for _, r := range volumes {
		vol := r.(Volume)
		sizeStr, _ := value.Render(vol.Size, value.RenderOpts{})
		attached := ""
		if len(vol.MountedBy) > 0 {
			names := make([]string, 0, len(vol.MountedBy))
			for _, m := range vol.MountedBy {
				names = append(names, cmp.Or(m.Name, m.UUID))
			}
			attached = fmt.Sprintf(" → %s", shell.ShellDirStyle.Render(strings.Join(names, ", ")))
		}
		fmt.Fprintf(sctx.Out, "  %s %-20s %s%s\n", shell.ShellLabelStyle.Render("■"), shell.ShellValueStyle.Render(vol.Name), shell.ShellHintStyle.Render(sizeStr), attached)
	}
	return nil
}

type ShellVolumesCreateCmd struct {
	Name       string   `arg:"" name:"name" help:"Volume name"`
	Size       string   `short:"s" name:"size" required:"" help:"Volume size (e.g. 10MiB)"`
	Filesystem string   `short:"f" name:"filesystem" help:"Filesystem type"`
	Tags       []string `short:"t" name:"tags" help:"Comma-separated tags"`
}

func (c *ShellVolumesCreateCmd) Run(sctx *ShellContext) error {
	var sizeMb types.SizeMebibytes
	if err := sizeMb.UnmarshalText([]byte(c.Size)); err != nil {
		return fmt.Errorf("%s invalid size: %v", shell.ShellErrorStyle.Render("error:"), err)
	}
	metro := cmp.Or(sctx.Key.Metro, defaultMetro(sctx.Ctx, ""))
	req := platform.CreateVolumeRequest{
		Name:       &c.Name,
		SizeMb:     func() *uint64 { v := uint64(sizeMb); return &v }(),
		Tags:       c.Tags,
		Filesystem: ptr.NilIfZero(c.Filesystem),
	}

	keys, err := group.CollectMetro(sctx.Ctx, sctx.G, metro, func(ctx context.Context, client multimetro.MetroClient) (multimetro.Keys, error) {
		resp, err := client.CreateVolume(ctx, req)
		if err != nil {
			return nil, err
		}
		if resp == nil || resp.Data == nil || len(resp.Data.Volumes) == 0 {
			return nil, fmt.Errorf("no volumes created")
		}
		var created multimetro.Keys
		for _, volume := range resp.Data.Volumes {
			created = append(created, multimetro.Key{Metro: client.Metro.Name, UUID: volume.Uuid, Name: volume.Name})
		}
		return created, nil
	})
	if err != nil {
		return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), err)
	}

	sizeFmt, _ := value.Render(sizeMb, value.RenderOpts{})
	for _, k := range keys {
		fmt.Fprintf(sctx.Out, "  %s created %s (%s) [%s]\n", shell.ShellValueStyle.Render("✓"), shell.ShellValueStyle.Render(c.Name), shell.ShellHintStyle.Render(sizeFmt), shell.ShellHintStyle.Render(k.UUID))
	}
	return nil
}

type ShellMountCmd struct {
	Volume string `arg:"" name:"volume" help:"Volume name"`
	Path   string `arg:"" name:"path" help:"Mount path"`
	Mode   string `arg:"" name:"mode" optional:"" help:"Mount mode (ro)"`
}

func (c *ShellMountCmd) Run(sctx *ShellContext) error {
	vol := &InstanceVolume{At: c.Path, Readonly: c.Mode == "ro"}
	if err := vol.Link.UnmarshalText([]byte(c.Volume)); err != nil {
		return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), err)
	}
	fmt.Fprintf(sctx.Out, "  %s mounting %s → %s...\n", shell.ShellLabelStyle.Render("■"), shell.ShellValueStyle.Render(c.Volume), shell.ShellDirStyle.Render(c.Path))

	parsedKeys := multimetro.ParseKeys([]string{sctx.Key.String()})
	err := group.DoRefs(sctx.Ctx, sctx.G, parsedKeys.Refs(), func(ctx context.Context, client multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		var attachReqs []platform.AttachVolumesRequestItem
		for _, ref := range refs {
			req := platform.AttachVolumesRequestItem{
				AttachTo: platform.NameOrUUID{Uuid: ptr.NilIfZero(ref.UUID), Name: ptr.NilIfZero(ref.Name)},
				At:       vol.At,
				Uuid:     ptr.NilIfZero(vol.UUID),
				Name:     ptr.NilIfZero(vol.Name),
			}
			if vol.Readonly {
				req.Readonly = new(true)
			}
			attachReqs = append(attachReqs, req)
		}
		if len(attachReqs) > 0 {
			if _, err := client.AttachVolumes(ctx, attachReqs); err != nil {
				return nil, err
			}
		}
		return refs, nil
	})
	if err != nil {
		return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), err)
	}

	fmt.Fprintf(sctx.Out, "  %s mounted %s → %s\n", shell.ShellValueStyle.Render("✓"), shell.ShellValueStyle.Render(c.Volume), shell.ShellDirStyle.Render(c.Path))
	fmt.Fprintln(sctx.Out, shell.ShellHintStyle.Render("  Run 'restart' for changes to take effect."))
	return nil
}

type ShellUnmountCmd struct {
	Volume string `arg:"" name:"volume" help:"Volume name"`
}

func (c *ShellUnmountCmd) Run(sctx *ShellContext) error {
	volKey := multimetro.ParseKey(c.Volume)
	fmt.Fprintf(sctx.Out, "  %s unmounting %s...\n", shell.ShellLabelStyle.Render("■"), shell.ShellValueStyle.Render(c.Volume))

	parsedKeys := multimetro.ParseKeys([]string{sctx.Key.String()})
	err := group.DoRefs(sctx.Ctx, sctx.G, parsedKeys.Refs(), func(ctx context.Context, client multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		var detachReqs []platform.DetachVolumesRequestItem
		for _, ref := range refs {
			detachReqs = append(detachReqs, platform.DetachVolumesRequestItem{
				From: &platform.NameOrUUID{Uuid: ptr.NilIfZero(ref.UUID), Name: ptr.NilIfZero(ref.Name)},
				Uuid: ptr.NilIfZero(volKey.UUID),
				Name: ptr.NilIfZero(volKey.Name),
			})
		}
		if len(detachReqs) > 0 {
			if _, err := client.DetachVolumes(ctx, detachReqs); err != nil {
				return nil, err
			}
		}
		return refs, nil
	})
	if err != nil {
		return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), err)
	}

	fmt.Fprintf(sctx.Out, "  %s unmounted %s\n", shell.ShellValueStyle.Render("✓"), shell.ShellValueStyle.Render(c.Volume))
	fmt.Fprintln(sctx.Out, shell.ShellHintStyle.Render("  Run 'restart' for changes to take effect."))
	return nil
}

type ShellStartCmd struct{}

func (c *ShellStartCmd) Run(sctx *ShellContext) error {
	parsedKeys := multimetro.ParseKeys([]string{sctx.Key.String()})
	before, opErr := Instance{}.Get(sctx.Ctx, parsedKeys.Strings())
	if opErr != nil && len(before) == 0 {
		return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), opErr)
	}

	started, startErr := startInstances(sctx.Ctx, sctx.G, parsedKeys)
	opErr = errors.Join(opErr, startErr)
	if len(started) == 0 {
		if opErr != nil {
			return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), opErr)
		}
		return nil
	}
	sctx.State.running = true
	sctx.startBackgroundSync()

	updated, getErr := Instance{}.Get(sctx.Ctx, started.Strings())
	opErr = errors.Join(opErr, getErr)
	if getErr != nil && len(updated) == 0 {
		if opErr != nil {
			return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), opErr)
		}
		return nil
	}

	keySet := make(map[string]struct{}, len(started))
	for _, k := range started {
		keySet[k.Canonical()] = struct{}{}
	}
	before = slices.DeleteFunc(slices.Clone(before), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().Canonical()]
		return !ok
	})
	updated = slices.DeleteFunc(slices.Clone(updated), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().Canonical()]
		return !ok
	})

	diffErr := rescmd.Diff(sctx.Ctx, sctx.Out, rescmd.FormatOpts{}, Instance{}, before, updated)
	if err := errors.Join(opErr, diffErr); err != nil {
		return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), err)
	}
	return nil
}

type ShellStopCmd struct{}

func (c *ShellStopCmd) Run(sctx *ShellContext) error {
	parsedKeys := multimetro.ParseKeys([]string{sctx.Key.String()})
	before, opErr := Instance{}.Get(sctx.Ctx, parsedKeys.Strings())
	if opErr != nil && len(before) == 0 {
		return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), opErr)
	}

	stopped, stopErr := stopInstances(sctx.Ctx, sctx.G, parsedKeys, StopOpts{DrainTimeout: -1})
	opErr = errors.Join(opErr, stopErr)
	if len(stopped) == 0 {
		if opErr != nil {
			return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), opErr)
		}
		return nil
	}
	sctx.State.running = false

	updated, getErr := Instance{}.Get(sctx.Ctx, stopped.Strings())
	opErr = errors.Join(opErr, getErr)
	if getErr != nil && len(updated) == 0 {
		if opErr != nil {
			return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), opErr)
		}
		return nil
	}

	keySet := make(map[string]struct{}, len(stopped))
	for _, k := range stopped {
		keySet[k.Canonical()] = struct{}{}
	}
	before = slices.DeleteFunc(slices.Clone(before), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().Canonical()]
		return !ok
	})
	updated = slices.DeleteFunc(slices.Clone(updated), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().Canonical()]
		return !ok
	})

	diffErr := rescmd.Diff(sctx.Ctx, sctx.Out, rescmd.FormatOpts{}, Instance{}, before, updated)
	if err := errors.Join(opErr, diffErr); err != nil {
		return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), err)
	}
	return nil
}

type ShellSuspendCmd struct{}

func (c *ShellSuspendCmd) Run(sctx *ShellContext) error {
	parsedKeys := multimetro.ParseKeys([]string{sctx.Key.String()})
	before, opErr := Instance{}.Get(sctx.Ctx, parsedKeys.Strings())
	if opErr != nil && len(before) == 0 {
		return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), opErr)
	}

	suspended, suspendErr := suspendInstances(sctx.Ctx, sctx.G, parsedKeys, -1)
	opErr = errors.Join(opErr, suspendErr)
	if len(suspended) == 0 {
		if opErr != nil {
			return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), opErr)
		}
		return nil
	}
	sctx.State.running = false

	updated, getErr := Instance{}.Get(sctx.Ctx, suspended.Strings())
	opErr = errors.Join(opErr, getErr)
	if getErr != nil && len(updated) == 0 {
		if opErr != nil {
			return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), opErr)
		}
		return nil
	}

	keySet := make(map[string]struct{}, len(suspended))
	for _, k := range suspended {
		keySet[k.Canonical()] = struct{}{}
	}
	before = slices.DeleteFunc(slices.Clone(before), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().Canonical()]
		return !ok
	})
	updated = slices.DeleteFunc(slices.Clone(updated), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().Canonical()]
		return !ok
	})

	diffErr := rescmd.Diff(sctx.Ctx, sctx.Out, rescmd.FormatOpts{}, Instance{}, before, updated)
	if err := errors.Join(opErr, diffErr); err != nil {
		return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), err)
	}
	return nil
}

type ShellRestartCmd struct{}

func (c *ShellRestartCmd) Run(sctx *ShellContext) error {
	parsedKeys := multimetro.ParseKeys([]string{sctx.Key.String()})
	before, opErr := Instance{}.Get(sctx.Ctx, parsedKeys.Strings())
	if opErr != nil && len(before) == 0 {
		return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), opErr)
	}

	stopped, stopErr := stopInstances(sctx.Ctx, sctx.G, parsedKeys, StopOpts{DrainTimeout: -1})
	opErr = errors.Join(opErr, stopErr)
	if len(stopped) == 0 {
		if opErr != nil {
			return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), opErr)
		}
		return nil
	}

	started, startErr := startInstances(sctx.Ctx, sctx.G, stopped)
	opErr = errors.Join(opErr, startErr)
	if len(started) == 0 {
		if opErr != nil {
			return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), opErr)
		}
		return nil
	}
	sctx.State.running = true
	sctx.startBackgroundSync()

	updated, getErr := Instance{}.Get(sctx.Ctx, started.Strings())
	opErr = errors.Join(opErr, getErr)
	if getErr != nil && len(updated) == 0 {
		if opErr != nil {
			return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), opErr)
		}
		return nil
	}

	keySet := make(map[string]struct{}, len(started))
	for _, k := range started {
		keySet[k.Canonical()] = struct{}{}
	}
	before = slices.DeleteFunc(slices.Clone(before), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().Canonical()]
		return !ok
	})
	updated = slices.DeleteFunc(slices.Clone(updated), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().Canonical()]
		return !ok
	})

	diffErr := rescmd.Diff(sctx.Ctx, sctx.Out, rescmd.FormatOpts{}, Instance{}, before, updated)
	if err := errors.Join(opErr, diffErr); err != nil {
		return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), err)
	}
	return nil
}

type ShellHistoryCmd struct {
	List   ShellHistoryListCmd   `cmd:"" default:"1" help:"Show command history"`
	Rerun  ShellHistoryRerunCmd  `cmd:"" help:"Re-execute a specific history entry"`
	Clear  ShellHistoryClearCmd  `cmd:"" help:"Clear all history"`
	Delete ShellHistoryDeleteCmd `cmd:"" help:"Delete a specific history entry"`
}

type ShellHistoryListCmd struct{}

func (c *ShellHistoryListCmd) Run(sctx *ShellContext) error {
	sctx.Cache.Print(sctx.Out)
	return nil
}

type ShellHistoryRerunCmd struct {
	Index int `arg:"" name:"index" help:"History index to rerun"`
}

func (c *ShellHistoryRerunCmd) Run(sctx *ShellContext) error {
	cmd, ok := sctx.Cache.Get(c.Index)
	if !ok {
		return fmt.Errorf("%s history: event not found: %d", shell.ShellErrorStyle.Render("error:"), c.Index)
	}

	fmt.Fprintln(sctx.Out, cmd)
	if err := executeRemote(sctx.Ctx, sctx.Out, strings.NewReader(""), sctx.G, sctx.Key, sctx.Plugin, sctx.State, cmd); err != nil {
		return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), err)
	}
	return nil
}

type ShellHistoryClearCmd struct{}

func (c *ShellHistoryClearCmd) Run(sctx *ShellContext) error {
	sctx.Cache.Clear()
	fmt.Fprintln(sctx.Out, shell.ShellHintStyle.Render("Clearing remote history..."))
	_, _ = group.CollectMetro(sctx.Ctx, sctx.G, sctx.Key.Metro, func(ctx context.Context, client multimetro.MetroClient) (struct{}, error) {
		resp, listErr := client.Sandbox.ListInstanceCommands(ctx, sctx.Key.Ref().UUID, sctx.Plugin)
		if listErr != nil {
			return struct{}{}, listErr
		}
		for _, cmdUUID := range resp.Data.Commands {
			_, _ = client.Sandbox.DeleteInstanceCommand(ctx, sctx.Key.Ref().UUID, sctx.Plugin, cmdUUID)
		}
		return struct{}{}, nil
	})
	fmt.Fprintf(sctx.Out, "History cleared\n")
	return nil
}

type ShellHistoryDeleteCmd struct {
	Index int `arg:"" name:"index" help:"History index to delete"`
}

func (c *ShellHistoryDeleteCmd) Run(sctx *ShellContext) error {
	cmd, cmdUUID, ok := sctx.Cache.Delete(c.Index)
	if !ok {
		return fmt.Errorf("%s history: event not found: %d", shell.ShellErrorStyle.Render("error:"), c.Index)
	}
	if cmdUUID != "" {
		fmt.Fprintf(sctx.Out, "%s %s\n", shell.ShellHintStyle.Render("Deleting remote entry..."), shell.ShellHintStyle.Render(cmdUUID))
		_, delErr := group.CollectMetro(sctx.Ctx, sctx.G, sctx.Key.Metro, func(ctx context.Context, client multimetro.MetroClient) (struct{}, error) {
			resp, err := client.Sandbox.DeleteInstanceCommand(ctx, sctx.Key.Ref().UUID, sctx.Plugin, cmdUUID)
			if err != nil {
				return struct{}{}, err
			}
			if resp.Status != "success" {
				return struct{}{}, fmt.Errorf("%s", resp.Message)
			}
			return struct{}{}, nil
		})
		if delErr != nil {
			fmt.Fprintf(sctx.ErrOut, "%s failed to delete remote command %s: %v\n", shell.ShellErrorStyle.Render("error:"), cmdUUID, delErr)
		}
	}
	fmt.Fprintf(sctx.Out, "Removed: %s\n", cmd)
	return nil
}

// shellBuiltinHelp is the list of builtins shown by `help`, in display order.
//
// Some of these are handled by ShellCmd and some by the shell loop itself
// (cd, clear, exit), so the list can't be derived from the kong grammar
// alone - it's the user-facing menu, not the parser's command set.
var shellBuiltinHelp = []struct{ name, desc string }{
	{"cd", "change the current remote directory"},
	{"get", "inspect the current instance"},
	{"edit", "edit instance fields (env, args, memory, vcpus, tags)"},
	{"volumes", "list volumes mounted on this instance (alias for volumes mounted)"},
	{"volumes mounted", "list volumes mounted on this instance"},
	{"volumes list", "list all available volumes"},
	{"volumes create", "create a new volume"},
	{"mount", "attach a volume to this instance"},
	{"unmount", "detach a volume from this instance"},
	{"start", "start the instance"},
	{"stop", "stop the instance"},
	{"suspend", "suspend the instance"},
	{"restart", "restart the instance"},
	{"history", "show command history (alias for history list)"},
	{"history list", "show command history"},
	{"history rerun <n>", "re-execute history entry N"},
	{"history clear", "clear all history entries"},
	{"history delete <n>", "delete history entry N"},
	{"clear", "clear the screen"},
	{"exit", "quit the shell"},
}

// builtinHelp prints the list of available shell builtins. The names are
// styled, so it goes through the ANSI-aware tabwriter rather than a %-Ns
// pad, which would count escape sequences towards the column width.
func builtinHelp(out io.Writer) {
	fmt.Fprintln(out, shell.ShellTitleStyle.Render("Builtins:"))
	fmt.Fprintln(out)

	tw := tabwriter.TabWriter(out)
	for _, b := range shellBuiltinHelp {
		fmt.Fprintf(tw, "  %s\t%s\n", shell.ShellValueStyle.Render(b.name), shell.ShellHintStyle.Render(b.desc))
	}
	// Nothing to do if the terminal write fails; every other line here
	// ignores write errors too.
	_ = tw.Flush()

	fmt.Fprintln(out)
	fmt.Fprintln(out, shell.ShellKeyStyle.Render("  ctrl-d quit · ctrl-r history · tab autocomplete · ctrl-c cancel"))
	fmt.Fprintln(out)
	fmt.Fprintln(out, shell.ShellHintStyle.Render("  All command logs are kept in memory unless explicitly cleaned with 'history clear' or 'history delete'."))
}

// instanceApplyPatches sends a set of patch operations to the instance API.
func instanceApplyPatches(ctx context.Context, g *group.Group[multimetro.MetroClient], key multimetro.Key, patches []patchReq[platform.MutableInstanceProperty]) error {
	if len(patches) == 0 {
		return nil
	}
	parsedKeys := multimetro.ParseKeys([]string{key.String()})
	return group.DoRefs(ctx, g, parsedKeys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		reqs := make([]platform.UpdateInstancesRequestItem, 0, len(refs)*len(patches))
		for _, ref := range refs {
			for _, patch := range patches {
				req := platform.UpdateInstancesRequestItem{
					Op:    platform.MutableInstanceOperation(patch.Op),
					Prop:  patch.Prop,
					Value: new(patch.Value),
				}
				if ref.UUID != "" {
					req.Uuid = &ref.UUID
				} else {
					req.Name = &ref.Name
				}
				reqs = append(reqs, req)
			}
		}
		log.G(ctx).Trace().Msg("updating instance")
		_, err := c.UpdateInstances(ctx, reqs)
		if err != nil {
			if platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
				return nil, nil
			}
			return nil, err
		}
		return refs, nil
	})
}
