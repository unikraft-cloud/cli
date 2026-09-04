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

	Create cmd.ResourceCreateCmd[VolumeTemplate] `cmd:"" help:"Create a volume template."`
	Edit   cmd.ResourceEditCmd[VolumeTemplate]   `cmd:"" help:"Edit a volume template."`
}

type VolumeTemplate struct {
	Metro LinkName[Metro] `field:"metro,short"`
	Name  string          `mirror:"volume.name" field:",short"`
	UUID  string          `mirror:"volume.uuid" field:",long"`

	Tags       []string `mirror:"volume.tags" field:",long" edit:"set,add,del" flag:"tag" sep:"none" help:"Template tag." placeholder:"tag" example:"env-dev"`
	DeleteLock bool     `mirror:"volume.delete_lock" field:"delete-lock,long" edit:"set" flag:"delete-lock" help:"Prevent deletion of the template."`

	State      types.VolumeState   `mirror:"volume.state" field:",short"`
	Size       types.SizeMebibytes `mirror:"volume.size_mb" field:",short"`
	Filesystem string              `mirror:"volume.filesystem" field:",long"`
	Persistent bool                `mirror:"volume.persistent" field:",long"`

	Timestamps struct {
		Created types.RelativeTime `mirror:"volume.created_at" field:",short"`
	}

	VolumeRef string `field:"volume,invisible,valueless" create:"set,required" flag-arg:"volume" completion-predictor:"resource-key-volume" help:"Volume to convert into a template."`

	Volume  platform.Volume `field:"-" json:"volume"`
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
		if resp == nil || resp.Data == nil {
			return nil, nil
		}
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
		if resp == nil || resp.Data == nil {
			return nil, nil, nil
		}
		for _, volume := range resp.Data.Volumes {
			if volume.Status == nil || *volume.Status != platform.ResponseStatusSuccess {
				continue
			}
			matchedRef := matchRef(refs, volume.Name, volume.Uuid)
			result, err := VolumeTemplate{}.load(matchedRef, volume, &c.Metro, profile)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if matchedRef != nil {
				found = append(found, *matchedRef)
			} else {
				found = append(found, group.Ref{Metro: c.Metro.Name, Name: result.Name, UUID: result.UUID})
			}
			results = append(results, result)
		}
		return results, found, errors.Join(errs...)
	})
}

func (VolumeTemplate) load(ref *group.Ref, volume platform.Volume, metro *config.Metro, profile *config.Profile) (VolumeTemplate, error) {
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

	result := VolumeTemplate{
		Volume:  volume,
		Metro:   LinkName[Metro](metro.Name),
		Profile: profile,
		key:     multimetro.Key(*ref),
	}
	err := mirror.Mirror(result, &result)
	if err != nil {
		return VolumeTemplate{}, fmt.Errorf("could not mirror volume template data: %w", err)
	}
	return result, nil
}

func (VolumeTemplate) Delete(ctx context.Context, keys []string) error {
	parsedKeys := multimetro.ParseKeys(keys)

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, parsedKeys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		log.G(ctx).Trace().Msg("deleting volume templates")
		templates, err := c.DeleteTemplateVolumes(ctx, refs.NameOrUUIDs())
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, err
		}
		var deleted []group.Ref
		for _, template := range templates.Data.Volumes {
			status := template.Status
			if status != "" && status != platform.ResponseStatusSuccess {
				continue
			}
			deleted = append(deleted, group.Ref{
				Metro: c.Metro.Name,
				Name:  template.Name,
				UUID:  template.Uuid,
			})
		}
		return deleted, nil
	})
}

func (VolumeTemplate) Edit(ctx context.Context, key string, fields []resource.Field) error {
	parsedKeys := multimetro.ParseKeys([]string{key})
	patches, err := patchRequests(fields, volumeTemplatePatchSpec)
	if err != nil {
		return err
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, parsedKeys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		reqs := make([]platform.UpdateTemplateVolumesRequestItem, 0, len(refs)*len(patches))
		for _, ref := range refs {
			for _, patch := range patches {
				req := platform.UpdateTemplateVolumesRequestItem{
					Op:    platform.MutableTemplateVolumeOperation(patch.Op),
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
		log.G(ctx).Trace().Msg("updating volume template")
		_, err := c.UpdateTemplateVolumes(ctx, reqs)
		if err != nil {
			if platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
				return nil, nil
			}
			return nil, err
		}
		return refs, nil
	})
}

func volumeTemplatePatchSpec(path string, op patchOp, value any) (platform.MutableTemplateVolumeProperty, any, error) {
	var zero platform.MutableTemplateVolumeProperty
	switch path {
	case "tags":
		return platform.MutableTemplateVolumePropertyTags, value.([]string), nil
	case "delete-lock":
		if value == nil {
			return zero, nil, nil
		}
		switch v := value.(type) {
		case bool:
			return platform.MutableTemplateVolumePropertyDeleteLock, v, nil
		case *bool:
			return platform.MutableTemplateVolumePropertyDeleteLock, *v, nil
		}
		return zero, nil, nil
	default:
		return zero, nil, nil
	}
}

func (VolumeTemplate) Create(ctx context.Context, fields []resource.Field) ([]resource.Resource, error) {
	var volume string
	for key, field := range resource.IterFields(fields) {
		if field.Create == nil || field.Create.Set == nil {
			continue
		}
		if key.String() == "volume" {
			volume = field.Create.Set.(string)
		}
	}
	if volume == "" {
		return nil, fmt.Errorf("no volume provided")
	}

	// First, get the volume to verify it exists and to fully resolve its key
	foundVolumes, getErr := Volume{}.Get(ctx, []string{volume})
	if getErr != nil && len(foundVolumes) == 0 {
		return nil, getErr
	}
	if len(foundVolumes) == 0 {
		return nil, fmt.Errorf("no volume found")
	}

	vol := foundVolumes[0].(Volume)
	if vol.key.Metro == "" {
		return nil, fmt.Errorf("volume key %q not fully resolved", vol.key.String())
	}
	ref := vol.key.Ref()
	refStr := cmp.Or(ref.Name, ref.UUID)

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	created, err := group.CollectMetro(ctx, g, vol.key.Metro, func(ctx context.Context, c multimetro.MetroClient) (multimetro.Keys, error) {
		log.G(ctx).Trace().Str("ref", refStr).Msg("creating volume template")
		resp, err := c.CreateTemplateVolume(ctx, []platform.NameOrUUID{ref.NameOrUUID()})
		if err != nil {
			return nil, fmt.Errorf("failed to create template for %s: %w", refStr, err)
		}
		if resp == nil || resp.Data == nil || len(resp.Data.Volumes) == 0 {
			return nil, fmt.Errorf("no template created for %s", refStr)
		}
		var created multimetro.Keys
		var errs []error
		for _, tmpl := range resp.Data.Volumes {
			status := tmpl.Status
			if status != "" && status != platform.ResponseStatusSuccess {
				name := cmp.Or(tmpl.Name, tmpl.Uuid)
				message := ptr.ZeroIfNil(tmpl.Message)
				if message == "" {
					message = "unknown error"
				}
				errs = append(errs, fmt.Errorf("template create failed for %s: %s", name, message))
				continue
			}
			created = append(created, multimetro.Key{
				Metro: c.Metro.Name,
				UUID:  tmpl.Uuid,
				Name:  tmpl.Name,
			})
		}
		return created, errors.Join(errs...)
	})
	if err != nil {
		return nil, err
	}
	if len(created) == 0 {
		return nil, fmt.Errorf("no template created for %s", refStr)
	}

	return VolumeTemplate{}.Get(ctx, created.Strings())
}

func (VolumeTemplate) Examples() map[cmd.CmdType][]kingkong.Example {
	return map[cmd.CmdType][]kingkong.Example{
		cmd.CmdTypeGet: {
			{
				Description: "Inspect a volume template by name or UUID",
				Commands:    []string{"unikraft volume template get demo-template"},
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
		},
		cmd.CmdTypeEdit: {
			{
				Description: "Update template tags",
				Commands: []string{
					"unikraft volume template edit demo-template --set tags=env-dev",
				},
			},
		},
		cmd.CmdTypeDelete: {
			{
				Description: "Delete a volume template",
				Commands:    []string{"unikraft volume template delete demo-template"},
			},
		},
	}
}
