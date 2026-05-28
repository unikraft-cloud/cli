// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"cmp"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/alecthomas/kong"
	"github.com/docker/go-units"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/kraftfile"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"

	"unikraft.com/cli/internal/builder"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/mirror"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/resource/patch"
	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/cli/internal/types"
	"unikraft.com/cli/internal/volimport"
)

type VolumesCmd struct {
	cmd.ResourceCmd[Volume]
	cmd.GettableResourceCmd[Volume]
	cmd.WaitableResourceCmd[Volume]
	cmd.ListableResourceCmd[Volume]
	cmd.BulkDeletableResourceCmd[Volume]

	Create   VolumeCreateCmd    `cmd:"" help:"Create a volume."`
	Edit     VolumeEditCmd      `cmd:"" help:"Edit a volume."`
	Template VolumeTemplatesCmd `cmd:"" group:"cmd-templates" help:"Manage volume templates." aliases:"templates" set:"name=volume-template" set:"names=volume-templates"`

	Clone  VolumesCloneCmd `cmd:"" help:"Clone a volume."`
	Import VolumeImportCmd `cmd:"" help:"Import data into a volume."`

	// Attach and Detach are implemented as resource-level operations rather than action commands to allow for multiple volumes to be attached or detached in a single command, which is a common use case when managing volumes. The attach and detach fields are specified as part of the volume resource, allowing for more flexible and efficient management of volume attachments.
	Attach VolumeAttachCmd `cmd:"" help:"Attach a volume."`
	Detach VolumeDetachCmd `cmd:"" help:"Detach a volume."`
}

// VolumeCreateCmd extends the generic resource create command with shortcut
// flags for commonly used volume fields. Each field tagged with
// `shortcut:"<path>"` is translated into a --set <path>=<value> entry before
// the standard create pipeline runs.
type VolumeCreateCmd struct {
	cmd.ResourceCreateCmd[Volume]

	Metro       string              `group:"flag-create" shortcut:"metro" help:"Metro to create in." placeholder:"metro" example:"fra,sfo,nyc"`
	Name        string              `group:"flag-create" shortcut:"name" short:"n" help:"Volume name." placeholder:"name"`
	Size        types.SizeMebibytes `group:"flag-create" shortcut:"size" help:"Volume size." placeholder:"size" example:"10GiB,100MiB"`
	Filesystem  string              `group:"flag-create" shortcut:"filesystem" help:"Volume filesystem." placeholder:"filesystem" example:"ext4"`
	QuotaPolicy string              `group:"flag-create" shortcut:"quota-policy" help:"Volume quota policy." placeholder:"quota-policy" example:"static,dynamic"`
}

func (c *VolumeCreateCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}

	return c.ResourceCreateCmd.Run(ctx, stdio, sandbox)
}

// VolumeAttachCmd extends the generic resource attach command with shortcut flags for
// commonly used attachable volume fields. Each field tagged with `shortcut:"<path>"` is
// translated into a --set <path>=<value> entry before the standard attach pipeline runs.
type VolumeAttachCmd struct {
	cmd.ResourceAttachCmd[Volume]

	AttachTo string `group:"flag-attach" name:"to" shortcut:"attach-to" help:"The instance the volume should be attached to." placeholder:"instance-name"`
	At       string `group:"flag-attach" name:"at" shortcut:"attach-at" help:"The path the volume should be mounted to." placeholder:"path" example:"/mnt"`
	ReadOnly bool   `group:"flag-attach" name:"readonly" shortcut:"attach-read-only" short:"r" help:"Mount the volume read-only."`
}

// Run applies any shortcut flags as --set arguments and then defers to the standard attach command implementation.
func (c *VolumeAttachCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}

	return c.ResourceAttachCmd.Run(ctx, stdio, sandbox)
}

// VolumeDetachCmd extends the generic resource detach command with shortcut flags for
// commonly used detachable volume fields. Each field tagged with `shortcut:"<path>"` is
// translated into a --set <path>=<value> entry before the standard detach pipeline runs.
type VolumeDetachCmd struct {
	cmd.ResourceDetachCmd[Volume]

	From string `group:"flag-detach" name:"from" shortcut:"detach-from" help:"The instance the volume should be detached from." placeholder:"instance-name"`
}

// Run applies any shortcut flags as --set arguments and then defers to the standard detach command implementation.
func (c *VolumeDetachCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}

	return c.ResourceDetachCmd.Run(ctx, stdio, sandbox)
}

// VolumeEditCmd extends the generic resource edit command with shortcut
// flags for commonly used editable volume fields. Each field tagged with
// `shortcut:"<path>"` is translated into a --set <path>=<value> entry before
// the standard edit pipeline runs.
type VolumeEditCmd struct {
	cmd.ResourceEditCmd[Volume]

	Size        types.SizeMebibytes `group:"flag-edit" shortcut:"size" help:"Volume size." placeholder:"size" example:"20GiB,100MiB"`
	QuotaPolicy string              `group:"flag-edit" shortcut:"quota-policy" help:"Volume quota policy." placeholder:"quota-policy" example:"static,dynamic"`
}

func (c *VolumeEditCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}

	return c.ResourceEditCmd.Run(ctx, stdio, sandbox)
}

type VolumesCloneCmd struct {
	Source string `arg:"" completion-predictor:"resource-key-volume" help:"Name or UUID of the volume to clone."`

	Name string   `group:"flag-clone" shortcut:"name" short:"n" help:"New volume name." placeholder:"name"`
	Tags []string `group:"flag-clone" shortcut:"tags" help:"Volume tags." placeholder:"tag" example:"env=prod,team=platform"`

	cmd.SetArgs

	cmd.FormatOpts
}

func (VolumesCloneCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Clone a volume with a new name",
			Commands: []string{
				// "unikraft volume clone demo-volume --set name=demo-volume-clone",
				"unikraft volume clone demo-volume --name demo-volume-clone",
			},
		},
	}
}

func (c *VolumesCloneCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	spec := patch.PatchSpec{
		Set: make(map[string][]string),
	}
	if err := c.Apply(&spec); err != nil {
		return err
	}
	req := platform.CloneVolumesRequestItem{}
	var unknownFields []string
	for key, values := range spec.Set {
		switch key {
		case "name":
			name, err := value.Parse[string](values)
			if err != nil {
				return err
			}
			if name != "" {
				req.VolName = new(name)
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
	req.Uuid = ptr.NilIfZero(volume.key.UUID)
	if req.Uuid == nil {
		req.Name = ptr.NilIfZero(volume.key.Name)
	}
	keys, opErr := group.CollectMetro(ctx, g, volume.Metro.Name, func(ctx context.Context, client multimetro.MetroClient) (multimetro.Keys, error) {
		log.G(ctx).Trace().Msg("cloning volume")
		resp, err := client.CloneVolumes(ctx, []platform.CloneVolumesRequestItem{req})
		if err != nil {
			return nil, err
		}
		if len(resp.Data.Volumes) == 0 {
			return nil, fmt.Errorf("no volumes cloned")
		}
		created := make(multimetro.Keys, 0, len(resp.Data.Volumes))
		for _, vol := range resp.Data.Volumes {
			created = append(created, multimetro.Key{
				Metro: client.Metro.Name,
				UUID:  vol.Uuid,
				Name:  vol.Name,
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
	MetroName LinkName[Metro] `mirror:"metro.name" field:"metro,short" create:"set,required"`
	Name      string          `mirror:"volume.name" field:",short" create:"set"`
	UUID      string          `mirror:"volume.uuid" field:",long"`

	Tags []string `mirror:"volume.tags"`

	State       types.VolumeState   `mirror:"volume.state" field:",short"`
	Size        types.SizeMebibytes `mirror:"volume.size_mb" field:",short" create:"set,required" edit:"set"`
	Filesystem  string              `mirror:"volume.filesystem" field:",long" create:"set"`
	QuotaPolicy string              `mirror:"volume.quota_policy" field:"quota-policy,long" create:"set" edit:"set"`
	Persistent  bool                `mirror:"volume.persistent" field:",long"`

	Timestamps struct {
		Created types.RelativeTime `mirror:"volume.created_at" field:",short"`
	}

	AttachedTo []struct {
		Link[Instance]
	} `mirror:"volume.attached_to"`

	MountedBy []struct {
		Link[Instance]
		ReadOnly bool `mirror:"read_only" field:",long"`
	} `mirror:"volume.mounted_by"`

	AttachTo       Link[Instance] `mirror:"volume.attach_to" field:",invisible" create:"set"`
	AttachAt       string         `mirror:"volume.attach_at" field:",invisible" create:"set"`
	AttachReadOnly bool           `mirror:"volume.attach_read_only" field:",invisible" create:"set"`

	DetachFrom Link[Instance] `mirror:"volume.detach_from" field:",invisible" create:"set"`

	Volume platform.Volume `field:"-" json:"volume"`
	Metro  *config.Metro   `field:"-" json:"metro"`

	key multimetro.Key
}

func (Volume) Type() resource.Type {
	return resource.Type{
		Name:  "volume",
		Names: "volumes",
	}
}

func (i Volume) Key() resource.Key {
	return i.key
}

func (i Volume) Raw() any {
	return i.Volume
}

func (i Volume) Fields(ctx context.Context) ([]resource.Field, error) {
	i.MetroName = LinkName[Metro](defaultMetro(ctx, string(i.MetroName)))
	return resource.FieldsFromStruct(i)
}

func (Volume) List(ctx context.Context) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return group.CollectAllSlices(ctx, g, func(ctx context.Context, c multimetro.MetroClient) ([]resource.Resource, error) {
		log.G(ctx).Trace().Msg("listing volumes")
		resp, err := c.GetVolumes(ctx, nil, platform.GetVolumesOpts{Details: new(true)})
		if err != nil {
			return nil, err
		}
		var results []resource.Resource
		var errs []error
		for _, volume := range resp.Data.Volumes {
			result, err := Volume{}.load(nil, volume, &c.Metro)
			if err != nil {
				errs = append(errs, err)
			}
			results = append(results, result)
		}
		return results, errors.Join(errs...)
	})
}

func (Volume) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	return group.CollectRefsSlices(ctx, g, multimetro.ParseKeys(keys).Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) ([]resource.Resource, group.Refs, error) {
		log.G(ctx).Trace().Msg("getting volumes")
		resp, err := c.GetVolumes(ctx, refs.NameOrUUIDs(), platform.GetVolumesOpts{Details: new(true)})
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, nil, err
		}
		var found []group.Ref
		var results []resource.Resource
		var errs []error
		for i, volume := range resp.Data.Volumes {
			if volume.Status == nil || *volume.Status != platform.ResponseStatusSuccess {
				continue
			}
			result, err := Volume{}.load(&refs[i], volume, &c.Metro)
			if err != nil {
				errs = append(errs, err)
				continue
			}

			found = append(found, group.Ref{
				Metro: c.Metro.Name,
				Name:  result.Name,
				UUID:  result.UUID,
			})
			results = append(results, result)
		}
		return results, found, errors.Join(errs...)
	})
}

func (Volume) load(ref *group.Ref, volume platform.Volume, metro *config.Metro) (Volume, error) {
	if ref == nil {
		ref = &group.Ref{
			Metro: metro.Name,
			Name:  volume.Name,
			UUID:  volume.Uuid,
		}
	} else {
		ref.Metro = cmp.Or(ref.Metro, metro.Name)
		ref.Name = cmp.Or(ref.Name, volume.Name)
		ref.UUID = cmp.Or(ref.UUID, volume.Uuid)
	}

	result := Volume{
		Volume: volume,
		Metro:  metro,
		key:    multimetro.Key(*ref),
	}
	err := mirror.Mirror(result, &result)
	if err != nil {
		return Volume{}, fmt.Errorf("could not mirror volume data: %w", err)
	}
	return result, nil
}

func (Volume) Delete(ctx context.Context, keys []string) error {
	parsedKeys := multimetro.ParseKeys(keys)

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, parsedKeys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		log.G(ctx).Trace().Msg("deleting volumes")
		resp, err := c.DeleteVolumes(ctx, refs.NameOrUUIDs())
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, err
		}
		var deleted []group.Ref
		for _, volume := range resp.Data.Volumes {
			if volume.Status != platform.ResponseStatusSuccess {
				continue
			}
			deleted = append(deleted, group.Ref{
				Metro: c.Metro.Name,
				Name:  volume.Name,
				UUID:  volume.Uuid,
			})
		}
		return deleted, nil
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
				metro = string(field.Create.Set.(LinkName[Metro]))
			case "size":
				size := field.Create.Set.(types.SizeMebibytes)
				sizeMb := uint64(size)
				req.SizeMb = &sizeMb
			case "filesystem":
				filesystem := field.Create.Set.(string)
				if filesystem != "" {
					data, err := json.Marshal(filesystem)
					if err != nil {
						return nil, err
					}
					// HACK: set on additional properties, since we need an updated SDK
					if req.AdditionalProperties == nil {
						req.AdditionalProperties = make(map[string]json.RawMessage)
					}
					req.AdditionalProperties["filesystem"] = data
				}
			case "quota-policy":
				quotaPolicy := field.Create.Set.(string)
				if quotaPolicy != "" {
					data, err := json.Marshal(quotaPolicy)
					if err != nil {
						return nil, err
					}
					// HACK: set on additional properties, since we need an updated SDK
					if req.AdditionalProperties == nil {
						req.AdditionalProperties = make(map[string]json.RawMessage)
					}
					req.AdditionalProperties["quota_policy"] = data
				}
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
				Metro: c.Metro.Name,
				UUID:  volume.Uuid,
				Name:  volume.Name,
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

// Attach attempts to attach the specified volume to an instance. The volume and instance can be identified by either name or UUID,
// but the instance must be specified in the attach fields rather than the key.
func (Volume) Attach(ctx context.Context, key string, fields []resource.Field) error {
	parsedKeys := multimetro.ParseKeys([]string{key})

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}

	return group.DoRefs(ctx, g, parsedKeys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		var (
			attachTo platform.NameOrUUID
			attachAt string
			readOnly *bool
		)
		for key, field := range resource.IterFields(fields) {
			if field.Create == nil || field.Create.Set == nil {
				continue
			}
			switch key.String() {
			case "attach-to":
				if instance, ok := field.Create.Set.(Link[Instance]); ok {
					attachTo = instance.Ref().NameOrUUID()
				}
			case "attach-read-only":
				if ro, ok := field.Create.Set.(bool); ok {
					readOnly = &ro
				}
			case "attach-at":
				if at, ok := field.Create.Set.(string); ok {
					attachAt = at
				}
			}
		}

		reqs := make([]platform.AttachVolumesRequestItem, 0, len(refs))
		for _, ref := range refs {
			req := platform.AttachVolumesRequestItem{
				AttachTo: attachTo,
				At:       attachAt,
				Readonly: readOnly,
			}
			if ref.UUID != "" {
				req.Uuid = &ref.UUID
			} else {
				req.Name = &ref.Name
			}
			reqs = append(reqs, req)
		}

		log.G(ctx).Trace().Msg("attaching volume")

		_, err := c.AttachVolumes(ctx, reqs)
		if err != nil {
			if platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
				return nil, nil
			}
			return nil, err
		}

		return refs, nil
	})
}

// Detach attempts to detach the specified volume from an instance. The volume can be identified by either name or UUID,
// but the instance must be specified in the detach fields rather than the key.
func (Volume) Detach(ctx context.Context, key string, fields []resource.Field) error {
	parsedKeys := multimetro.ParseKeys([]string{key})

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}

	return group.DoRefs(ctx, g, parsedKeys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		var from *platform.NameOrUUID
		for key, field := range resource.IterFields(fields) {
			if field.Create == nil || field.Create.Set == nil {
				continue
			}
			if key.String() == "detach-from" {
				if instance, ok := field.Create.Set.(Link[Instance]); ok {
					nou := instance.Ref().NameOrUUID()
					from = &nou
				}
			}
		}

		reqs := make([]platform.DetachVolumesRequestItem, 0, len(refs))
		for _, ref := range refs {
			req := platform.DetachVolumesRequestItem{From: from}
			if ref.UUID != "" {
				req.Uuid = &ref.UUID
			} else {
				req.Name = &ref.Name
			}
			reqs = append(reqs, req)
		}

		log.G(ctx).Trace().Msg("detaching volume")

		_, err := c.DetachVolumes(ctx, reqs)
		if err != nil {
			if platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
				return nil, nil
			}
			return nil, err
		}

		return refs, nil
	})
}

func (Volume) Edit(ctx context.Context, key string, fields []resource.Field) error {
	parsedKeys := multimetro.ParseKeys([]string{key})
	patches, err := patchRequests(fields, volumePatchSpec)
	if err != nil {
		return err
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, parsedKeys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		reqs := make([]platform.UpdateVolumesRequestItem, 0, len(refs)*len(patches))
		for _, ref := range refs {
			for _, patch := range patches {
				req := platform.UpdateVolumesRequestItem{
					Op:    platform.MutableVolumeOperation(patch.Op),
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
		log.G(ctx).Trace().Msg("updating volume")
		_, err := c.UpdateVolumes(ctx, reqs)
		if err != nil {
			if platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
				return nil, nil
			}
			return nil, err
		}
		return refs, nil
	})
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
					// `unikraft volume create \
					//   --set name=demo-volume \
					//   --set size=10 \
					//   --set metro=fra`,
					`unikraft volume create \
	  --name demo-volume \
	  --size 10 \
	  --metro fra`,
				},
			},
		},
		cmd.CmdTypeEdit: {
			{
				Description: "Resize a volume",
				Commands: []string{
					// "unikraft volume edit demo-volume --set size=20",
					"unikraft volume edit demo-volume --size 20",
				},
			},
		},
		cmd.CmdTypeDelete: {
			{
				Description: "Delete a volume by name or UUID",
				Commands:    []string{"unikraft volume delete demo-volume"},
			},
		},
		cmd.CmdTypeAttach: {
			{
				Description: "Attach a volume",
				Commands: []string{
					`unikraft volume attach demo-volume \
	  --at /mnt \
	  --readonly \
	  --to instance-name`,
				},
			},
		},
		cmd.CmdTypeDetach: {
			{
				Description: "Detach a volume",
				Commands: []string{
					`unikraft volume detach demo-volume \
	  --from instance-name`,
				},
			},
		},
	}
}

func volumePatchSpec(path string, _ patchOp, value any) (platform.MutableVolumeProperty, any, error) {
	var zero platform.MutableVolumeProperty
	switch path {
	case "size":
		return platform.MutableVolumePropertySize_mb, int64(value.(types.SizeMebibytes)), nil
	case "quota-policy":
		return platform.MutableVolumePropertyQuota_policy, value.(string), nil
	default:
		return zero, nil, nil
	}
}

// VolumeImportCmd imports data from a local source into a volume by
// temporarily spinning up a volimport instance, streaming a CPIO archive
// over TLS, and cleaning up the instance afterwards.
type VolumeImportCmd struct {
	Volume  string `arg:"" completion-predictor:"resource-key-volume" help:"Name or UUID of the volume to import into." create:"set,required"`
	Source  string `short:"s" help:"Data source: local directory, CPIO archive, or Dockerfile." placeholder:"path" create:"set,required"`
	Force   bool   `short:"f" help:"Force import even if the data might exceed volume capacity."`
	Port    int    `short:"p" default:"42069" help:"Port to connect to the volume import service on." placeholder:"port" hidden:"true"`
	Image   string `default:"official/utils/volimport:1.0" help:"Volume import image to use." placeholder:"image" hidden:"true"`
	Timeout uint64 `short:"t" default:"10" help:"Inactivity timeout in seconds for the import service." placeholder:"seconds" hidden:"true"`
}

func (VolumeImportCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Import the current directory into a volume",
			Commands:    []string{"unikraft volume import my-volume --source ."},
		},
		{
			Description: "Import a local directory into a volume",
			Commands:    []string{"unikraft volume import my-volume --source ./data"},
		},
		{
			Description: "Import a CPIO archive into a volume",
			Commands:    []string{"unikraft volume import my-volume --source rootfs.cpio"},
		},
		{
			Description: "Import a Dockerfile context into a volume",
			Commands:    []string{"unikraft volume import my-volume --source ./Dockerfile"},
		},
	}
}

func (c *VolumeImportCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox) error {
	if c.Source == "" {
		return fmt.Errorf("source path is required")
	}

	// Resolve source to an absolute path.
	abs, err := filepath.Abs(c.Source)
	if err != nil {
		return fmt.Errorf("resolving source path: %w", err)
	}
	c.Source = abs

	if c.Port < 1024 || c.Port > 65535 {
		return fmt.Errorf("port must be between 1024 and 65535")
	}

	// Resolve the target volume.
	gettable := sandbox.WrapGettable(Volume{})
	resources, err := gettable.Get(ctx, []string{c.Volume})
	if err != nil {
		return err
	}
	if len(resources) == 0 {
		return fmt.Errorf("volume not found: %s", c.Volume)
	}
	if len(resources) > 1 {
		keys := make([]string, 0, len(resources))
		for _, res := range resources {
			keys = append(keys, res.Key().String())
		}
		return fmt.Errorf("ambiguous volume: %s (found %v)", c.Volume, keys)
	}
	vol, ok := resources[0].(Volume)
	if !ok {
		return fmt.Errorf("unexpected resource type %T", resources[0])
	}

	sourceType, err := builder.DetectSourceType(c.Source)
	if err != nil {
		return fmt.Errorf("detecting source type for %q: %w", c.Source, err)
	}
	importOpts := &builder.BuildOpts{
		Rootfs: builder.FSOpts{
			Path: c.Source,
			Type: sourceType,
			// Set format to CPIO as volimport expects a CPIO archive
			Format: kraftfile.FsTypeCpio,
		},
		// Set platform as volimport makes sense only on UnikraftCloud
		Platform: []ocispec.Platform{builder.DefaultPlatform},
	}

	// Build an import archive from the data source.
	log.G(ctx).Trace().Str("source", c.Source).Msg("packaging source as import archive")
	images, err := builder.BuildRootfs(ctx, *importOpts)
	if err != nil {
		return fmt.Errorf("packaging source as import archive: %w", err)
	}
	if len(images) == 0 {
		return fmt.Errorf("no images were built from the provided source")
	}
	if len(images) > 1 {
		return fmt.Errorf("multiple images were built from the provided source; expected exactly one")
	}
	initrd := images[0].Initrd
	if initrd == nil {
		return fmt.Errorf("built image has no initrd")
	}
	defer func() {
		if err := initrd.Cleanup(); err != nil {
			log.G(ctx).Error().Err(err).Msg("cleaning up initrd")
		}
	}()

	cpioReader, cpioSize, err := initrd.Open(ctx)
	if err != nil {
		return fmt.Errorf("opening import archive: %w", err)
	}
	defer cpioReader.Close()

	authStr, err := volimport.GenRandAuth()
	if err != nil {
		return fmt.Errorf("generating authentication token: %w", err)
	}

	// Spawn a temporary volimport instance in the volume's metro.
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	var instUUID, instFQDN string
	if err := group.DoMetro(ctx, g, vol.Metro.Name, func(ctx context.Context, mc multimetro.MetroClient) error {
		var merr error
		instUUID, instFQDN, merr = volimport.Start(ctx, mc, c.Image, vol.UUID, authStr, c.Timeout, c.Port)
		return merr
	}); err != nil {
		return fmt.Errorf("spawning volume data import instance: %w", err)
	}

	defer func() {
		if err := group.DoMetro(ctx, g, vol.Metro.Name, func(ctx context.Context, mc multimetro.MetroClient) error {
			return volimport.Terminate(ctx, mc, instUUID)
		}); err != nil {
			log.G(ctx).Error().Err(err).Msg("terminating volume data import instance")
		}
	}()

	// Open a TLS connection to the instance and stream the CPIO archive.
	instAddr := instFQDN + ":" + strconv.Itoa(c.Port)
	log.G(ctx).Info().
		Str("size", units.BytesSize(float64(cpioSize))).
		Str("volume", c.Volume).
		Msg("importing data into volume")

	conn, err := tls.Dial("tcp", instAddr, &tls.Config{
		InsecureSkipVerify: ptr.ZeroIfNil(vol.Metro.Insecure),
	})
	if err != nil {
		return fmt.Errorf("connecting to volume import service at %s: %w", instAddr, err)
	}
	defer conn.Close()

	freeSpace, totalSpace, err := volimport.Copy(ctx, conn, authStr, cpioReader, c.Force, uint64(cpioSize))
	if err != nil {
		return fmt.Errorf("importing data: %w", err)
	}

	log.G(ctx).Info().
		Str("volume", c.Volume).
		Str("free", units.BytesSize(float64(freeSpace))).
		Str("total", units.BytesSize(float64(totalSpace))).
		Msg("import complete")

	// Wait for the import instance to stop; it auto-deletes via delete-on-stop.
	return group.DoMetro(ctx, g, vol.Metro.Name, func(ctx context.Context, mc multimetro.MetroClient) error {
		return volimport.Wait(ctx, mc, instUUID)
	})
}
