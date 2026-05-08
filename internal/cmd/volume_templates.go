// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/alecthomas/kong"

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
	"unikraft.com/cli/internal/types"
)

type VolumeTemplatesCmd struct {
	cmd.ResourceCmd[VolumeTemplate]
	cmd.GettableResourceCmd[VolumeTemplate]
	cmd.ListableResourceCmd[VolumeTemplate]
	cmd.BulkDeletableResourceCmd[VolumeTemplate]

	Create VolumeTemplateCreateCmd `cmd:"" help:"Create a volume template."`
	Edit   VolumeTemplateEditCmd   `cmd:"" help:"Edit a volume template."`
}

// VolumeTemplateCreateCmd extends the generic resource create command with
// positional volume IDs.
type VolumeTemplateCreateCmd struct {
	cmd.ResourceCreateCmd[VolumeTemplate]

	Targets []string `arg:"" name:"volume" optional:"" completion-predictor:"resource-key-volume" help:"Volumes to convert into templates."`
}

func (c *VolumeTemplateCreateCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	if len(c.Targets) > 0 {
		c.Set = append(c.Set, map[string]string{"volumes": strings.Join(c.Targets, ",")})
	}
	return c.ResourceCreateCmd.Run(ctx, stdio, sandbox)
}

// VolumeTemplateEditCmd extends the generic resource edit command with
// shortcut flags for commonly used editable template fields.
type VolumeTemplateEditCmd struct {
	cmd.ResourceEditCmd[VolumeTemplate]

	Tags       []string `group:"flag-edit" shortcut:"tags" help:"Template tags." placeholder:"tag" example:"env-dev,team-platform"`
	DeleteLock *bool    `group:"flag-edit" shortcut:"delete-lock" help:"Prevent deletion of the template."`
}

func (c *VolumeTemplateEditCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	return c.ResourceEditCmd.Run(ctx, stdio, sandbox)
}

type VolumeTemplate struct {
	MetroName LinkName[Metro] `mirror:"metro.name" field:"metro,short"`
	Name      string          `mirror:"volume.name" field:",short"`
	UUID      string          `mirror:"volume.uuid" field:",long"`

	Tags       []string `mirror:"volume.tags" edit:"set,add,del"`
	DeleteLock bool     `mirror:"volume.delete_lock" field:"delete-lock,hidden" edit:"set"`

	State      types.VolumeState   `mirror:"volume.state" field:",short"`
	Size       types.SizeMebibytes `mirror:"volume.size_mb" field:",short"`
	Filesystem string              `mirror:"volume.filesystem" field:",long"`
	Persistent bool                `mirror:"volume.persistent" field:",long"`

	Timestamps struct {
		Created types.RelativeTime `mirror:"volume.created_at" field:",short"`
	}

	Volumes []string `field:"volumes,invisible,valueless" create:"set,required"`

	Volume  platform.Volume `field:"-" json:"volume"`
	Metro   *config.Metro   `field:"-" json:"metro"`
	Profile *config.Profile `field:"-" json:"profile"`

	key multimetro.Key
}

func (VolumeTemplate) Type() resource.Type {
	return resource.Type{
		Name:  "volume-template",
		Names: "volume-templates",
	}
}

func (v VolumeTemplate) Key() resource.Key {
	return v.key
}

func (v VolumeTemplate) Raw() any {
	return v.Volume
}

func (v VolumeTemplate) Fields(ctx context.Context) ([]resource.Field, error) {
	return resource.FieldsFromStruct(v)
}

func (VolumeTemplate) List(ctx context.Context) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, err
	}
	return group.CollectAllSlices(ctx, g, func(ctx context.Context, c multimetro.MetroClient) ([]resource.Resource, error) {
		log.G(ctx).Trace().Msg("listing volume templates")
		resp, err := c.GetTemplateVolumes(ctx, nil, platform.GetTemplateVolumesOpts{Details: new(true)})
		if err != nil {
			return nil, err
		}
		var results []resource.Resource
		var errs []error
		for _, volume := range resp.Data.Volumes {
			result, err := VolumeTemplate{}.load(nil, volume, &c.Metro, profile)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			results = append(results, result)
		}
		return results, errors.Join(errs...)
	})
}

func (VolumeTemplate) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, err
	}
	return group.CollectRefsSlices(ctx, g, multimetro.ParseKeys(keys).Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) ([]resource.Resource, group.Refs, error) {
		log.G(ctx).Trace().Msg("getting volume templates")
		resp, err := c.GetTemplateVolumes(ctx, refs.NameOrUUIDs(), platform.GetTemplateVolumesOpts{Details: new(true)})
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, nil, err
		}
		var found []group.Ref
		var results []resource.Resource
		var errs []error
		for i, volume := range resp.Data.Volumes {
			if volume.Status == nil || *volume.Status != platform.ResponseStatusSUCCESS {
				continue
			}
			result, err := VolumeTemplate{}.load(&refs[i], volume, &c.Metro, profile)
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

func (VolumeTemplate) load(ref *group.Ref, volume platform.Volume, metro *config.Metro, profile *config.Profile) (VolumeTemplate, error) {
	if ref == nil {
		ref = &group.Ref{
			Metro: metro.Name,
			Name:  ptr.ZeroIfNil(volume.Name),
			UUID:  ptr.ZeroIfNil(volume.Uuid),
		}
	} else {
		ref.Metro = cmp.Or(ref.Metro, metro.Name)
		ref.Name = cmp.Or(ref.Name, ptr.ZeroIfNil(volume.Name))
		ref.UUID = cmp.Or(ref.UUID, ptr.ZeroIfNil(volume.Uuid))
	}

	result := VolumeTemplate{
		Volume:  volume,
		Metro:   metro,
		Profile: profile,
		key:     multimetro.Key(*ref),
	}
	err := mirror.Mirror(result, &result)
	if err != nil {
		return VolumeTemplate{}, fmt.Errorf("could not mirror volume template data: %w", err)
	}
	return result, nil
}

func (VolumeTemplate) Delete(ctx context.Context, targets []resource.Resource) error {
	keys := make(multimetro.Keys, 0, len(targets))
	for _, target := range targets {
		template := target.(VolumeTemplate)
		keys = append(keys, template.key)
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, keys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		log.G(ctx).Trace().Msg("deleting volume templates")
		templates, err := c.DeleteTemplateVolumes(ctx, refs.NameOrUUIDs())
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, err
		}
		var deleted []group.Ref
		for _, template := range templates.Data.Volumes {
			status := ptr.ZeroIfNil(template.Status)
			if status != "" && status != platform.ResponseStatusSUCCESS {
				continue
			}
			deleted = append(deleted, group.Ref{
				Metro: c.Metro.Name,
				Name:  ptr.ZeroIfNil(template.Name),
				UUID:  ptr.ZeroIfNil(template.Uuid),
			})
		}
		return deleted, nil
	})
}

func (VolumeTemplate) Edit(ctx context.Context, target resource.Resource, fields []resource.Field) (resource.Resource, error) {
	template := target.(VolumeTemplate)
	patches := patchRequests(fields, volumeTemplatePatchSpec)
	reqs := make([]platform.UpdateTemplateVolumesRequestItem, 0, len(patches))
	for _, patch := range patches {
		reqs = append(reqs, platform.UpdateTemplateVolumesRequestItem{
			Uuid:  &template.UUID,
			Op:    platform.UpdateTemplateVolumesRequestItemOp(patch.Op),
			Prop:  patch.Prop,
			Value: new(patch.Value),
		})
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	err = group.DoMetro(ctx, g, template.key.Metro, func(ctx context.Context, c multimetro.MetroClient) error {
		log.G(ctx).Trace().Msg("updating volume template")
		_, err := c.UpdateTemplateVolumes(ctx, reqs)
		return err
	})
	if err != nil {
		return nil, err
	}
	results, err := VolumeTemplate{}.Get(ctx, []string{template.Key().String()})
	if err != nil {
		return nil, err
	}
	return results[0], nil
}

func volumeTemplatePatchSpec(path string, op patchOp, value any) (platform.UpdateTemplateVolumesRequestItemProp, any) {
	var zero platform.UpdateTemplateVolumesRequestItemProp
	switch path {
	case "tags":
		return platform.UpdateTemplateVolumesRequestItemPropTags, value.([]string)
	case "delete-lock":
		if value == nil {
			return zero, nil
		}
		switch v := value.(type) {
		case bool:
			return platform.UpdateTemplateVolumesRequestItemPropDelete_lock, v
		case *bool:
			return platform.UpdateTemplateVolumesRequestItemPropDelete_lock, *v
		}
		return zero, nil
	default:
		return zero, nil
	}
}

func (VolumeTemplate) Create(ctx context.Context, fields []resource.Field) ([]resource.Resource, error) {
	var volumes []string
	for key, field := range resource.IterFields(fields) {
		if field.Create == nil || field.Create.Set == nil {
			continue
		}
		if key.String() == "volumes" {
			volumes = field.Create.Set.([]string)
		}
	}
	if len(volumes) == 0 {
		return nil, fmt.Errorf("no volumes provided")
	}

	// First, get the volumes to verify they exist and to fully resolve their keys
	foundVolumes, getErr := Volume{}.Get(ctx, volumes)
	if getErr != nil && len(foundVolumes) == 0 {
		return nil, getErr
	}
	if len(foundVolumes) == 0 {
		return nil, fmt.Errorf("no volumes found")
	}

	// Build refs grouped by metro from the found volumes
	refsByMetro := make(map[string][]group.Ref)
	for _, res := range foundVolumes {
		vol := res.(Volume)
		if vol.key.Metro == "" {
			return nil, fmt.Errorf("volume key %q not fully resolved", vol.key.String())
		}
		refsByMetro[vol.key.Metro] = append(refsByMetro[vol.key.Metro], vol.key.Ref())
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	var created multimetro.Keys
	var errs []error
	for metroName, refs := range refsByMetro {
		keys, err := group.CollectMetro(ctx, g, metroName, func(ctx context.Context, c multimetro.MetroClient) (multimetro.Keys, error) {
			var created multimetro.Keys
			var errs []error
			// Create templates one at a time since the platform API only accepts single operations
			for _, ref := range refs {
				refStr := cmp.Or(ref.Name, ref.UUID)
				log.G(ctx).Trace().Str("ref", refStr).Msg("creating volume template")
				resp, err := c.CreateTemplateVolume(ctx, []platform.NameOrUUID{ref.NameOrUUID()})
				if err != nil {
					errs = append(errs, fmt.Errorf("failed to create template for %s: %w", refStr, err))
					continue
				}
				if len(resp.Data.Volumes) == 0 {
					errs = append(errs, fmt.Errorf("no template created for %s", refStr))
					continue
				}
				for _, tmpl := range resp.Data.Volumes {
					status := ptr.ZeroIfNil(tmpl.Status)
					if status != "" && status != platform.ResponseStatusSUCCESS {
						name := cmp.Or(ptr.ZeroIfNil(tmpl.Name), ptr.ZeroIfNil(tmpl.Uuid))
						message := ptr.ZeroIfNil(tmpl.Message)
						if message == "" {
							message = "unknown error"
						}
						errs = append(errs, fmt.Errorf("template create failed for %s: %s", name, message))
						continue
					}
					created = append(created, multimetro.Key{
						Metro: c.Metro.Name,
						UUID:  ptr.ZeroIfNil(tmpl.Uuid),
						Name:  ptr.ZeroIfNil(tmpl.Name),
					})
				}
			}
			return created, errors.Join(errs...)
		})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		created = append(created, keys...)
	}

	if len(created) == 0 {
		return nil, errors.Join(errs...)
	}

	results, err := VolumeTemplate{}.Get(ctx, created.Strings())
	if err != nil {
		errs = append(errs, err)
	}
	return results, errors.Join(errs...)
}

func (VolumeTemplate) Examples() map[cmd.CmdType][]kingkong.Example {
	return map[cmd.CmdType][]kingkong.Example{
		cmd.CmdTypeGet: {
			{
				Description: "Inspect a volume template by name",
				Commands:    []string{"unikraft volume template get demo-template"},
			},
			{
				Description: "Inspect a volume template by UUID",
				Commands:    []string{"unikraft volume template get d4f0c132-a70e-4777-9631-7dfe08a1c15d"},
			},
			{
				Description: "Show template details in JSON format",
				Commands:    []string{"unikraft volume template get demo-template -o json"},
			},
		},
		cmd.CmdTypeList: {
			{
				Description: "List volume templates across metros",
				Commands:    []string{"unikraft volume template list"},
			},
		},
		cmd.CmdTypeCreate: {
			{
				Description: "Convert a volume into a template",
				Commands:    []string{"unikraft volume template create demo-volume"},
			},
			{
				Description: "Convert multiple volumes into templates",
				Commands:    []string{"unikraft volume template create vol-1 vol-2"},
			},
		},
		cmd.CmdTypeEdit: {
			{
				Description: "Update template tags",
				Commands: []string{
					"unikraft volume template edit demo-template --tags env-prod,team-data",
				},
			},
			{
				Description: "Lock a template to prevent deletion",
				Commands: []string{
					"unikraft volume template edit demo-template --delete-lock",
				},
			},
		},
		cmd.CmdTypeDelete: {
			{
				Description: "Delete a volume template",
				Commands:    []string{"unikraft volume template delete demo-template"},
			},
			{
				Description: "Delete all volume templates (with confirmation)",
				Commands:    []string{"unikraft volume template delete --all"},
			},
		},
	}
}
