// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/mirror"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/resource/patch"
	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/cli/internal/types"
)

type VolumesCmd struct {
	cmd.ResourceCmd[Volume]
	cmd.GettableResourceCmd[Volume]      `set:"name=volume" set:"names=volumes"`
	cmd.WaitableResourceCmd[Volume]      `set:"name=volume" set:"names=volumes"`
	cmd.ListableResourceCmd[Volume]      `set:"name=volume" set:"names=volumes"`
	cmd.BulkDeletableResourceCmd[Volume] `set:"name=volume" set:"names=volumes"`
	cmd.EditableResourceCmd[Volume]      `set:"name=volume" set:"names=volumes"`
	cmd.CreatableResourceCmd[Volume]     `set:"name=volume" set:"names=volumes"`

	Clone VolumesCloneCmd `cmd:"" help:"Clone a volume." set:"name=volume"`
}

type VolumesCloneCmd struct {
	Source string `arg:"" completion-predictor:"resource-key-volume" help:"Name or UUID of the volume to clone."`

	cmd.SetArgs

	cmd.FormatOpts
}

func (VolumesCloneCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Clone a volume with a new name",
			Commands:    []string{"unikraft volume clone demo-volume --set name=demo-volume-clone"},
		},
	}
}

func (c *VolumesCloneCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox) error {
	spec := patch.PatchSpec{
		Set: make(map[string][]string),
	}
	if err := c.Apply(&spec); err != nil {
		return err
	}
	req := platform.CloneVolumeByUUIDRequestBody{}
	var unknownFields []string
	for key, values := range spec.Set {
		switch key {
		case "name":
			name, err := value.Parse[string](values)
			if err != nil {
				return err
			}
			if name != "" {
				req.VolName = ptr.ToPtr(name)
			}
		case "tags":
			tags, err := value.Parse[[]string](values)
			if err != nil {
				return err
			}
			req.Tags = tags
		default:
			unknownFields = append(unknownFields, key)
		}
	}
	if len(unknownFields) > 0 {
		slices.Sort(unknownFields)
		return fmt.Errorf("unknown fields: %v", unknownFields)
	}

	gettable := sandbox.WrapGettable(Volume{})
	resources, err := gettable.Get(ctx, []string{c.Source})
	if err != nil {
		return err
	}
	if len(resources) == 0 {
		return fmt.Errorf("volume not found: %s", c.Source)
	}
	if len(resources) > 1 {
		var keys []string
		for _, res := range resources {
			keys = append(keys, res.Key().String())
		}
		return fmt.Errorf("ambiguous volume: %s (found %v)", c.Source, keys)
	}

	volume, ok := resources[0].(Volume)
	if !ok {
		return fmt.Errorf("unexpected resource type %T", resources[0])
	}
	if volume.UUID == "" {
		return fmt.Errorf("volume %q is missing a UUID", volume.Name)
	}
	if volume.Metro == nil {
		return fmt.Errorf("volume %q has no metro information", volume.Name)
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	keys, opErr := group.CollectMetro(ctx, g, volume.Metro.Name.String(), func(ctx context.Context, client multimetro.MetroClient) (multimetro.Keys, error) {
		log.G(ctx).Trace().Msg("cloning volume")
		resp, err := client.CloneVolumeByUUID(ctx, volume.UUID, req)
		if err != nil {
			return nil, err
		}
		if len(resp.Data.Volumes) == 0 {
			return nil, fmt.Errorf("no volumes cloned")
		}
		created := make(multimetro.Keys, 0, len(resp.Data.Volumes))
		for _, vol := range resp.Data.Volumes {
			created = append(created, multimetro.Key{
				Metro: client.Metro.Name.String(),
				UUID:  ptr.ZeroIfNil(vol.Uuid),
				Name:  ptr.ZeroIfNil(vol.Name),
			})
		}
		return created, nil
	})
	if opErr != nil && len(keys) == 0 {
		return opErr
	}

	results, getErr := Volume{}.Get(ctx, keys.Strings())
	if getErr != nil && len(results) == 0 {
		return errors.Join(opErr, getErr)
	}
	if sandbox != nil {
		for _, res := range results {
			if err := sandbox.Add(ctx, res); err != nil {
				return err
			}
		}
	}

	printErr := c.Output.
		WithDefault(cmd.PrinterTypeKeyValue).
		Print(ctx, stdio.Stdout, c.Field, Volume{}, results...)
	if printErr != nil {
		return errors.Join(opErr, getErr, printErr)
	}
	return errors.Join(opErr, getErr)
}

type Volume struct {
	MetroName string `mirror:"metro.name" field:"metro,short" create:"set,required"`
	Name      string `mirror:"volume.name" field:",short" create:"set"`
	UUID      string `mirror:"volume.uuid" field:",long"`

	Tags []string `mirror:"volume.tags"`

	State      types.VolumeState   `mirror:"volume.state" field:",short"`
	Size       types.SizeMebibytes `mirror:"volume.size_mb" field:",short" create:"set,required" edit:"set"`
	Persistent bool                `mirror:"volume.persistent" field:",long"`

	Timestamps struct {
		Created types.RelativeTime `mirror:"volume.created_at" field:",short"`
	}

	AttachedTo []struct {
		Name string `mirror:"name" field:",long"`
		UUID string `mirror:"uuid" field:",long"`
	} `mirror:"volume.attached_to"`

	MountedBy []struct {
		Name     string `mirror:"name" field:",long"`
		UUID     string `mirror:"uuid" field:",long"`
		ReadOnly bool   `mirror:"read_only" field:",long"`
	} `mirror:"volume.mounted_by"`

	Volume platform.Volume `field:"-" json:"volume"`
	Metro  *config.Metro   `field:"-" json:"metro"`
}

func (Volume) Type() resource.Type {
	return resource.Type{
		Name:  "volume",
		Names: "volumes",
	}
}

func (i Volume) key() multimetro.Key {
	return multimetro.Key{
		Metro: i.MetroName,
		Name:  i.Name,
		UUID:  i.UUID,
	}
}

func (i Volume) Key() resource.Key {
	return i.key()
}

func (i Volume) Raw() any {
	return i.Volume
}

func (i Volume) Fields() ([]resource.Field, error) {
	return resource.FieldsFromStruct(i)
}

func (Volume) List(ctx context.Context) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return group.CollectAllSlices(ctx, g, func(ctx context.Context, c multimetro.MetroClient) ([]resource.Resource, error) {
		log.G(ctx).Trace().Msg("listing volumes")
		resp, err := c.GetVolumes(ctx, nil, true)
		if err != nil {
			return nil, err
		}
		var results []resource.Resource
		for _, volume := range resp.Data.Volumes {
			result, err := Volume{}.load(volume, &c.Metro)
			if err != nil {
				return nil, err
			}
			results = append(results, result)
		}
		return results, nil
	})
}

func (Volume) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	return group.CollectRefsSlices(ctx, g, multimetro.ParseKeys(keys).Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) ([]resource.Resource, group.Refs, error) {
		log.G(ctx).Trace().Msg("getting volumes")
		resp, err := c.GetVolumes(ctx, refs.NameOrUUIDs(), true)
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, nil, err
		}
		var found []group.Ref
		var results []resource.Resource
		for _, volume := range resp.Data.Volumes {
			if volume.Status == nil || *volume.Status != platform.ResponseStatusSUCCESS {
				continue
			}
			result, err := Volume{}.load(volume, &c.Metro)
			if err != nil {
				return nil, nil, err
			}
			found = append(found, group.Ref{
				Metro: c.Metro.Name.String(),
				Name:  result.Name,
				UUID:  result.UUID,
			})
			results = append(results, result)
		}
		return results, found, nil
	})
}

func (Volume) load(volume platform.Volume, metro *config.Metro) (Volume, error) {
	result := Volume{
		Volume: volume,
		Metro:  metro,
	}
	err := mirror.Mirror(result, &result)
	if err != nil {
		return Volume{}, fmt.Errorf("could not mirror volume data: %w", err)
	}
	return result, nil
}

func (Volume) Delete(ctx context.Context, targets []resource.Resource) error {
	keys := make(multimetro.Keys, 0, len(targets))
	for _, target := range targets {
		volume := target.(Volume)
		keys = append(keys, volume.key())
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, keys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		log.G(ctx).Trace().Msg("deleting volumes")
		for _, key := range refs {
			_, err := c.DeleteVolumeByUUID(ctx, key.UUID)
			if err != nil {
				return nil, err
			}
		}
		return refs, nil
	})
}

func (Volume) Create(ctx context.Context, fields []resource.Field) ([]resource.Resource, error) {
	var req platform.CreateVolumeRequest
	var metro string
	for key, field := range resource.IterFields(fields) {
		if field.Create.Set != nil {
			switch key.String() {
			case "name":
				name := field.Create.Set.(string)
				req.Name = &name
			case "metro":
				metro = field.Create.Set.(string)
			case "size":
				size := field.Create.Set.(types.SizeMebibytes)
				req.SizeMb = uint64(size)
			}
		}
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	keys, err := group.CollectMetro(ctx, g, metro, func(ctx context.Context, c multimetro.MetroClient) (multimetro.Keys, error) {
		log.G(ctx).Trace().Msg("creating volume")
		resp, err := c.CreateVolume(ctx, req)
		if err != nil {
			return nil, err
		}
		if len(resp.Data.Volumes) == 0 {
			return nil, fmt.Errorf("no volumes created")
		}
		created := make(multimetro.Keys, 0, len(resp.Data.Volumes))
		for _, volume := range resp.Data.Volumes {
			key := multimetro.Key{
				Metro: c.Metro.Name.String(),
				UUID:  ptr.ZeroIfNil(volume.Uuid),
				Name:  ptr.ZeroIfNil(volume.Name),
			}
			created = append(created, key)
		}
		return created, nil
	})
	if err != nil {
		return nil, err
	}
	results, err := Volume{}.Get(ctx, keys.Strings())
	if err != nil {
		return nil, err
	}
	return results, nil
}

func (Volume) Edit(ctx context.Context, target resource.Resource, fields []resource.Field) (resource.Resource, error) {
	volume := target.(Volume)
	patches := patchRequests(fields, volumePatchSpec)
	reqs := make([]platform.UpdateVolumesRequestItem, 0, len(patches))
	for _, patch := range patches {
		reqs = append(reqs, platform.UpdateVolumesRequestItem{
			Uuid:  &volume.UUID,
			Op:    platform.UpdateVolumesRequestItemOp(patch.Op),
			Prop:  patch.Prop,
			Value: platform.Ptr(patch.Value),
		})
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	err = group.DoMetro(ctx, g, volume.key().Metro, func(ctx context.Context, c multimetro.MetroClient) error {
		log.G(ctx).Trace().Msg("updating volume")
		_, err := c.UpdateVolumes(ctx, reqs)
		return err
	})
	if err != nil {
		return nil, err
	}
	results, err := Volume{}.Get(ctx, []string{volume.Key().String()})
	if err != nil {
		return nil, err
	}
	return results[0], nil
}

func (Volume) Examples() map[cmd.CmdType][]kingkong.Example {
	return map[cmd.CmdType][]kingkong.Example{
		cmd.CmdTypeGet: {
			{
				Description: "Inspect a volume by name or UUID",
				Commands:    []string{"unikraft volume get demo-volume"},
			},
		},
		cmd.CmdTypeList: {
			{
				Description: "List all volumes",
				Commands:    []string{"unikraft volume list"},
			},
		},
		cmd.CmdTypeCreate: {
			{
				Description: "Create a new volume",
				Commands: []string{
					`unikraft volume create \
  --set name=demo-volume \
  --set size=10 \
  --set metro=fra`,
				},
			},
		},
		cmd.CmdTypeEdit: {
			{
				Description: "Resize a volume",
				Commands:    []string{"unikraft volume edit demo-volume --set size=20"},
			},
		},
		cmd.CmdTypeDelete: {
			{
				Description: "Delete a volume by name or UUID",
				Commands:    []string{"unikraft volume delete demo-volume"},
			},
		},
	}
}

func volumePatchSpec(path string, _ patchOp, value any) (platform.UpdateVolumesRequestItemProp, any) {
	var zero platform.UpdateVolumesRequestItemProp
	switch path {
	case "size":
		return platform.UpdateVolumesRequestItemPropSize_mb, int64(value.(types.SizeMebibytes))
	default:
		return zero, nil
	}
}
