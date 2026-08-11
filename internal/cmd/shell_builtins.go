// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"unikraft.com/cli/internal/config"
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
	builtinHelp(sctx.Out, sctx.Builtins)
	return nil
}

// instance re-reads this shell's instance. The shell is long-lived and the
// instance changes underneath it - start, stop and edit all mutate it - so
// this always goes to the API rather than holding on to a copy.
func (sctx *ShellContext) instance() (Instance, error) {
	resources, err := Instance{}.Get(sctx.Ctx, []string{sctx.Key.String()})
	if err != nil {
		return Instance{}, shellErr(err)
	}
	if len(resources) == 0 {
		return Instance{}, fmt.Errorf("%s", shell.ShellErrorStyle.Render("instance not found"))
	}
	instance, ok := resources[0].(Instance)
	if !ok {
		return Instance{}, fmt.Errorf("%s", shell.ShellErrorStyle.Render("instance not found"))
	}
	return instance, nil
}

type ShellGetCmd struct{}

func (c *ShellGetCmd) Run(sctx *ShellContext) error {
	instance, err := sctx.instance()
	if err != nil {
		return err
	}
	rescmd.Printer{}.WithDefault(rescmd.PrinterTypeKeyValue).
		Print(sctx.Ctx, sctx.Out, nil, Instance{}, instance)
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
	instance, err := sctx.instance()
	if err != nil {
		return err
	}

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
		return shellErr(err)
	}
	if err := instanceApplyPatches(sctx.Ctx, sctx.G, sctx.Key, patches); err != nil {
		return shellErr(err)
	}
	fmt.Fprintf(sctx.Out, "  %s %s=%s\n", shell.ShellValueStyle.Render("✓"), shell.ShellDirStyle.Render(fieldName), shell.ShellValueStyle.Render(fieldValue))
	fmt.Fprintln(sctx.Out, shell.ShellHintStyle.Render("  Run '"+shell.BuiltinSigil+"restart' for changes to take effect."))
	return nil
}

type ShellVolumesCmd struct {
	Mounted ShellVolumesMountedCmd `cmd:"" default:"1" help:"List volumes mounted on this instance"`
	List    ShellVolumesListCmd    `cmd:"" help:"List all available volumes"`
	Create  ShellVolumesCreateCmd  `cmd:"" help:"Create a new volume"`
}

type ShellVolumesMountedCmd struct{}

func (c *ShellVolumesMountedCmd) Run(sctx *ShellContext) error {
	instance, err := sctx.instance()
	if err != nil {
		return err
	}

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
		return shellErr(err)
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
		return shellErr(err)
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
		return shellErr(err)
	}
	fmt.Fprintf(sctx.Out, "  %s mounting %s → %s...\n", shell.ShellLabelStyle.Render("■"), shell.ShellValueStyle.Render(c.Volume), shell.ShellDirStyle.Render(c.Path))

	// UUID only: sctx.Key carries both a UUID and a name, and the volume
	// attach/detach API rejects a reference that specifies the two.
	parsedKeys := multimetro.Keys{{Metro: sctx.Key.Metro, UUID: sctx.Key.UUID}}
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
		return shellErr(err)
	}

	fmt.Fprintf(sctx.Out, "  %s mounted %s → %s\n", shell.ShellValueStyle.Render("✓"), shell.ShellValueStyle.Render(c.Volume), shell.ShellDirStyle.Render(c.Path))
	fmt.Fprintln(sctx.Out, shell.ShellHintStyle.Render("  Run '"+shell.BuiltinSigil+"restart' for changes to take effect."))
	return nil
}

type ShellUnmountCmd struct {
	Volume string `arg:"" name:"volume" help:"Volume name"`
}

func (c *ShellUnmountCmd) Run(sctx *ShellContext) error {
	volKey := multimetro.ParseKey(c.Volume)
	fmt.Fprintf(sctx.Out, "  %s unmounting %s...\n", shell.ShellLabelStyle.Render("■"), shell.ShellValueStyle.Render(c.Volume))

	// UUID only: sctx.Key carries both a UUID and a name, and the volume
	// attach/detach API rejects a reference that specifies the two.
	parsedKeys := multimetro.Keys{{Metro: sctx.Key.Metro, UUID: sctx.Key.UUID}}
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
		return shellErr(err)
	}

	fmt.Fprintf(sctx.Out, "  %s unmounted %s\n", shell.ShellValueStyle.Render("✓"), shell.ShellValueStyle.Render(c.Volume))
	fmt.Fprintln(sctx.Out, shell.ShellHintStyle.Render("  Run '"+shell.BuiltinSigil+"restart' for changes to take effect."))
	return nil
}

// shellErr gives an error the shell's error styling, passing nil through.
func shellErr(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s %v", shell.ShellErrorStyle.Render("error:"), err)
}

// The lifecycle builtins are the CLI's own start/stop/suspend/restart
// commands pointed at this shell's instance, so the diff output and the
// semantics stay identical to running them from outside. They only add
// tracking of whether the instance is up, which the shell needs to decide
// whether a command can be sent at all.
func (sctx *ShellContext) stdio() config.Stdio {
	return config.Stdio{Stdout: sctx.Out, Stderr: sctx.ErrOut}
}

type ShellStartCmd struct{}

func (c *ShellStartCmd) Run(sctx *ShellContext) error {
	cmd := InstancesStartCmd{Targets: []string{sctx.Key.String()}}
	if err := cmd.Run(sctx.Ctx, sctx.stdio()); err != nil {
		return shellErr(err)
	}
	sctx.State.running = true
	sctx.startBackgroundSync()
	return nil
}

type ShellStopCmd struct{}

func (c *ShellStopCmd) Run(sctx *ShellContext) error {
	cmd := InstancesStopCmd{
		Targets:  []string{sctx.Key.String()},
		StopOpts: StopOpts{DrainTimeout: -1},
	}
	if err := cmd.Run(sctx.Ctx, sctx.stdio()); err != nil {
		return shellErr(err)
	}
	sctx.State.running = false
	return nil
}

type ShellSuspendCmd struct{}

func (c *ShellSuspendCmd) Run(sctx *ShellContext) error {
	cmd := InstancesSuspendCmd{Targets: []string{sctx.Key.String()}, DrainTimeout: -1}
	if err := cmd.Run(sctx.Ctx, sctx.stdio()); err != nil {
		return shellErr(err)
	}
	sctx.State.running = false
	return nil
}

type ShellRestartCmd struct{}

func (c *ShellRestartCmd) Run(sctx *ShellContext) error {
	cmd := InstancesRestartCmd{
		Targets:  []string{sctx.Key.String()},
		StopOpts: StopOpts{DrainTimeout: -1},
	}
	if err := cmd.Run(sctx.Ctx, sctx.stdio()); err != nil {
		return shellErr(err)
	}
	sctx.State.running = true
	sctx.startBackgroundSync()
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
	if err := executeRemote(sctx.Ctx, sctx.Out, nil, sctx.G, sctx.Key, sctx.Plugin, sctx.State, cmd); err != nil {
		return shellErr(err)
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

// builtinHelp prints the list of available shell builtins. The names are
// styled, so it goes through the ANSI-aware tabwriter rather than a %-Ns
// pad, which would count escape sequences towards the column width.
func builtinHelp(out io.Writer, builtins *shellBuiltins) {
	fmt.Fprintln(out, shell.ShellTitleStyle.Render("Builtins:"))
	fmt.Fprintln(out)

	tw := tabwriter.TabWriter(out)
	for _, b := range builtins.menu {
		fmt.Fprintf(tw, "  %s\t%s\n", shell.ShellValueStyle.Render(b.usage), shell.ShellHintStyle.Render(b.desc))
	}
	// Nothing to do if the terminal write fails; every other line here
	// ignores write errors too.
	_ = tw.Flush()

	fmt.Fprintln(out)
	fmt.Fprintln(out, shell.ShellKeyStyle.Render("  ctrl-d quit · ctrl-r history · tab autocomplete · ctrl-c cancel"))
	fmt.Fprintln(out)

	sigil := shell.BuiltinSigil
	for _, line := range []string{
		"Builtins are the lines that open with '" + sigil + "', and they turn green as you type them. Every other",
		"line goes to the instance exactly as written, so the shell never shadows its commands:",
		"",
		"  mount vol /mnt      runs the instance's mount",
		"  " + sigil + "mount vol /mnt     attaches a volume to the instance",
		"",
		"A builtin has to be the whole line, because it runs here rather than on the instance -",
		"there is nothing on this side to pipe it into or chain it with:",
		"",
		"  ls && mount vol /mnt    goes to the instance whole; 'mount' is just its own command",
		"  " + sigil + "mount vol /mnt && ls   is an error, and nothing runs",
	} {
		if line == "" {
			fmt.Fprintln(out)
			continue
		}
		fmt.Fprintln(out, shell.ShellHintStyle.Render("  "+line))
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, shell.ShellHintStyle.Render("  All command logs are kept in memory unless explicitly cleaned with '"+sigil+"history clear' or '"+sigil+"history delete'."))
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
