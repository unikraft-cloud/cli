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

type InstanceTemplatesCmd struct {
	cmd.ResourceCmd[InstanceTemplate]
	cmd.GettableResourceCmd[InstanceTemplate]
	cmd.ListableResourceCmd[InstanceTemplate]
	cmd.BulkDeletableResourceCmd[InstanceTemplate]

	Create InstanceTemplateCreateCmd `cmd:"" help:"Create an instance template."`
	Edit   InstanceTemplateEditCmd   `cmd:"" help:"Edit an instance template."`
}

// InstanceTemplateCreateCmd extends the generic resource create command with
// positional instance IDs.
type InstanceTemplateCreateCmd struct {
	cmd.ResourceCreateCmd[InstanceTemplate]

	Target string `arg:"" name:"instance" optional:"" completion-predictor:"resource-key-instance" help:"Instance to convert into a template."`
}

func (c *InstanceTemplateCreateCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	if c.Target != "" {
		c.Set = append(c.Set, map[string]string{"instance": c.Target})
	}
	return c.ResourceCreateCmd.Run(ctx, stdio, sandbox)
}

// InstanceTemplateEditCmd extends the generic resource edit command with
// shortcut flags for commonly used editable template fields.
type InstanceTemplateEditCmd struct {
	cmd.ResourceEditCmd[InstanceTemplate]

	Tag        []string `group:"flag-edit" shortcut:"tags" sep:"none" help:"Template tag." placeholder:"tag" example:"env-dev"`
	DeleteLock *bool    `group:"flag-edit" shortcut:"delete-lock" help:"Prevent deletion of the template."`
}

func (c *InstanceTemplateEditCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	return c.ResourceEditCmd.Run(ctx, stdio, sandbox)
}

type InstanceTemplate struct {
	Metro LinkName[Metro] `field:"metro,short"`
	Name  string          `mirror:"instance.name" field:",short"`
	UUID  string          `mirror:"instance.uuid" field:",long"`

	Tags       []string `mirror:"instance.tags" field:",long" edit:"set,add,del"`
	DeleteLock bool     `mirror:"instance.delete_lock" field:"delete-lock,long" edit:"set"`

	State types.InstanceState `mirror:"instance.state" field:",short"`
	Image types.ImageRef      `mirror:"instance.image" field:",short"`

	Runtime struct {
		Args InstanceArgs      `mirror:"instance.args" field:",short"`
		Env  map[string]string `mirror:"instance.env" field:",long"`
	}

	Resources struct {
		Memory types.SizeMebibytes `mirror:"instance.memory_mb" field:",short"`
		VCPUs  int                 `mirror:"instance.vcpus" field:"vcpus,short"`
	}

	Volumes []*InstanceVolume `mirror:"instance.volumes" field:",embed"`

	Timestamps struct {
		Created types.RelativeTime `mirror:"instance.created_at" field:",short"`
	}

	ScaleToZero InstanceScaleToZero `field:",embed" mirror:"instance.scale_to_zero"`

	Restart struct {
		Policy string `mirror:"instance.restart_policy"`
	}

	InstanceRef string `field:"instance,invisible,valueless" create:"set,required"`

	Instance platform.Instance `field:"-" json:"instance"`
	Profile  *config.Profile   `field:"-" json:"profile"`

	key multimetro.Key
}

func (InstanceTemplate) Type() resource.Type {
	return resource.Type{
		Name:  "instance-template",
		Names: "instance-templates",
	}
}

func (i InstanceTemplate) Key() resource.Key {
	return i.key
}

func (i InstanceTemplate) Raw() any {
	return i.Instance
}

func (i InstanceTemplate) Fields(ctx context.Context) ([]resource.Field, error) {
	return resource.FieldsFromStruct(i)
}

func (InstanceTemplate) List(ctx context.Context) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, err
	}
	return group.CollectAllSlices(ctx, g, func(ctx context.Context, c multimetro.MetroClient) ([]resource.Resource, error) {
		log.G(ctx).Trace().Msg("listing instance templates")
		resp, err := c.GetTemplateInstances(ctx, nil, platform.GetTemplateInstancesOpts{Details: new(true)})
		if err != nil {
			return nil, err
		}
		var results []resource.Resource
		var errs []error
		if resp == nil || resp.Data == nil {
			return nil, nil
		}
		for _, instance := range resp.Data.Instances {
			result, err := InstanceTemplate{}.load(nil, instance, &c.Metro, profile)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			results = append(results, result)
		}
		return results, errors.Join(errs...)
	})
}

func (InstanceTemplate) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, err
	}
	return group.CollectRefsSlices(ctx, g, multimetro.ParseKeys(keys).Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) ([]resource.Resource, group.Refs, error) {
		log.G(ctx).Trace().Msg("getting instance templates")
		resp, err := c.GetTemplateInstances(ctx, refs.NameOrUUIDs(), platform.GetTemplateInstancesOpts{Details: new(true)})
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, nil, err
		}
		var found []group.Ref
		var results []resource.Resource
		var errs []error
		if resp == nil || resp.Data == nil {
			return nil, nil, nil
		}
		for _, instance := range resp.Data.Instances {
			if instance.Status == nil || *instance.Status != platform.ResponseStatusSuccess {
				continue
			}
			matchedRef := matchRef(refs, instance.Name, instance.Uuid)
			result, err := InstanceTemplate{}.load(matchedRef, instance, &c.Metro, profile)
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

func (InstanceTemplate) load(ref *group.Ref, instance platform.Instance, metro *config.Metro, profile *config.Profile) (InstanceTemplate, error) {
	if ref == nil {
		ref = &group.Ref{
			Metro: metro.Name,
			Name:  instance.Name,
			UUID:  instance.Uuid,
		}
	} else {
		ref.Metro = cmp.Or(ref.Metro, metro.Name)
		ref.Name = cmp.Or(ref.Name, instance.Name)
		ref.UUID = cmp.Or(ref.UUID, instance.Uuid)
	}

	result := InstanceTemplate{
		Instance: instance,
		Metro:    LinkName[Metro](metro.Name),
		Profile:  profile,
		key:      multimetro.Key(*ref),
	}
	err := mirror.Mirror(result, &result)
	if err != nil {
		return InstanceTemplate{}, fmt.Errorf("could not mirror instance template data: %w", err)
	}
	return result, nil
}

func (InstanceTemplate) Delete(ctx context.Context, keys []string) error {
	parsedKeys := multimetro.ParseKeys(keys)

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, parsedKeys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		log.G(ctx).Trace().Msg("deleting instance templates")
		templates, err := c.DeleteTemplateInstances(ctx, refs.NameOrUUIDs())
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, err
		}
		var deleted []group.Ref
		for _, template := range templates.Data.Instances {
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

func (InstanceTemplate) Edit(ctx context.Context, key string, fields []resource.Field) error {
	parsedKeys := multimetro.ParseKeys([]string{key})
	patches, err := patchRequests(fields, instanceTemplatePatchSpec)
	if err != nil {
		return err
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, parsedKeys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		reqs := make([]platform.UpdateTemplateInstancesRequestItem, 0, len(refs)*len(patches))
		for _, ref := range refs {
			for _, patch := range patches {
				req := platform.UpdateTemplateInstancesRequestItem{
					Op:    platform.MutableTemplateInstanceOperation(patch.Op),
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
		log.G(ctx).Trace().Msg("updating instance template")
		_, err := c.UpdateTemplateInstances(ctx, reqs)
		if err != nil {
			if platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
				return nil, nil
			}
			return nil, err
		}
		return refs, nil
	})
}

func instanceTemplatePatchSpec(path string, op patchOp, value any) (platform.MutableTemplateInstanceProperty, any, error) {
	var zero platform.MutableTemplateInstanceProperty
	switch path {
	case "tags":
		return platform.MutableTemplateInstancePropertyTags, value.([]string), nil
	case "delete-lock":
		if value == nil {
			return zero, nil, nil
		}
		switch v := value.(type) {
		case bool:
			return platform.MutableTemplateInstancePropertyDeleteLock, v, nil
		case *bool:
			return platform.MutableTemplateInstancePropertyDeleteLock, *v, nil
		}
		return zero, nil, nil
	default:
		return zero, nil, nil
	}
}

func (InstanceTemplate) Create(ctx context.Context, fields []resource.Field) ([]resource.Resource, error) {
	var instance string
	for key, field := range resource.IterFields(fields) {
		if field.Create == nil || field.Create.Set == nil {
			continue
		}
		if key.String() == "instance" {
			instance = field.Create.Set.(string)
		}
	}
	if instance == "" {
		return nil, fmt.Errorf("no instance provided")
	}

	// First, get the instance to verify it exists and to fully resolve its key
	foundInstances, getErr := Instance{}.Get(ctx, []string{instance})
	if getErr != nil && len(foundInstances) == 0 {
		return nil, getErr
	}
	if len(foundInstances) == 0 {
		return nil, fmt.Errorf("no instance found")
	}

	inst := foundInstances[0].(Instance)
	if inst.key.Metro == "" {
		return nil, fmt.Errorf("instance key %q not fully resolved", inst.key.String())
	}
	ref := inst.key.Ref()
	refStr := cmp.Or(ref.Name, ref.UUID)

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	created, err := group.CollectMetro(ctx, g, inst.key.Metro, func(ctx context.Context, c multimetro.MetroClient) (multimetro.Keys, error) {
		var reqItem platform.CreateTemplateInstancesRequestItem
		if ref.Name != "" {
			reqItem.Name = new(ref.Name)
		} else {
			reqItem.Uuid = new(ref.UUID)
		}
		log.G(ctx).Trace().Str("ref", refStr).Msg("creating instance template")
		resp, err := c.CreateTemplateInstances(
			ctx,
			[]platform.CreateTemplateInstancesRequestItem{
				reqItem,
			},
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create template for %s: %w", refStr, err)
		}
		if resp == nil || resp.Data == nil || len(resp.Data.Instances) == 0 {
			return nil, fmt.Errorf("no template created for %s", refStr)
		}
		var created multimetro.Keys
		var errs []error
		for _, tmpl := range resp.Data.Instances {
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

	return InstanceTemplate{}.Get(ctx, created.Strings())
}

func (InstanceTemplate) Examples() map[cmd.CmdType][]kingkong.Example {
	return map[cmd.CmdType][]kingkong.Example{
		cmd.CmdTypeGet: {
			{
				Description: "Inspect an instance template by name or UUID",
				Commands:    []string{"unikraft instance template get demo-template"},
			},
		},
		cmd.CmdTypeList: {
			{
				Description: "List instance templates across metros",
				Commands:    []string{"unikraft instance template list"},
			},
		},
		cmd.CmdTypeCreate: {
			{
				Description: "Convert an instance into a template",
				Commands:    []string{"unikraft instance template create demo-instance"},
			},
		},
		cmd.CmdTypeEdit: {
			{
				Description: "Update template tags",
				Commands: []string{
					"unikraft instance template edit demo-template --set tags=env-dev",
				},
			},
		},
		cmd.CmdTypeDelete: {
			{
				Description: "Delete an instance template",
				Commands:    []string{"unikraft instance template delete demo-template"},
			},
		},
	}
}
