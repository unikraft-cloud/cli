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
	"github.com/distribution/reference"

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

	Targets []string `arg:"" name:"instance" optional:"" completion-predictor:"resource-key-instance" help:"Instances to convert into templates."`
}

func (c *InstanceTemplateCreateCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	if len(c.Targets) > 0 {
		c.Set = append(c.Set, map[string]string{"instances": strings.Join(c.Targets, ",")})
	}
	return c.ResourceCreateCmd.Run(ctx, stdio, sandbox)
}

// InstanceTemplateEditCmd extends the generic resource edit command with
// shortcut flags for commonly used editable template fields.
type InstanceTemplateEditCmd struct {
	cmd.ResourceEditCmd[InstanceTemplate]

	Tags       []string `group:"flag-edit" shortcut:"tags" help:"Template tags." placeholder:"tag" example:"env-dev,team-platform"`
	DeleteLock *bool    `group:"flag-edit" shortcut:"delete-lock" help:"Prevent deletion of the template."`
}

func (c *InstanceTemplateEditCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	return c.ResourceEditCmd.Run(ctx, stdio, sandbox)
}

type InstanceTemplate struct {
	MetroName LinkName[Metro] `mirror:"metro.name" field:"metro,short"`
	Name      string          `mirror:"instance.name" field:",short"`
	UUID      string          `mirror:"instance.uuid" field:",long"`

	Tags       []string `mirror:"instance.tags" edit:"set,add,del"`
	DeleteLock bool     `mirror:"instance.delete_lock" field:"delete-lock,hidden" edit:"set"`

	State types.InstanceState             `mirror:"instance.state" field:",short"`
	Image types.ImageRef[reference.Named] `mirror:"instance.image" field:",short"`

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

	Instances []string `field:"instances,invisible,valueless" create:"set,required"`

	Instance platform.Instance `field:"-" json:"instance"`
	Metro    *config.Metro     `field:"-" json:"metro"`
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
		for i, instance := range resp.Data.Instances {
			if instance.Status == nil || *instance.Status != platform.ResponseStatusSUCCESS {
				continue
			}
			result, err := InstanceTemplate{}.load(&refs[i], instance, &c.Metro, profile)
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

func (InstanceTemplate) load(ref *group.Ref, instance platform.Instance, metro *config.Metro, profile *config.Profile) (InstanceTemplate, error) {
	if ref == nil {
		ref = &group.Ref{
			Metro: metro.Name,
			Name:  ptr.ZeroIfNil(instance.Name),
			UUID:  ptr.ZeroIfNil(instance.Uuid),
		}
	} else {
		ref.Metro = cmp.Or(ref.Metro, metro.Name)
		ref.Name = cmp.Or(ref.Name, ptr.ZeroIfNil(instance.Name))
		ref.UUID = cmp.Or(ref.UUID, ptr.ZeroIfNil(instance.Uuid))
	}

	result := InstanceTemplate{
		Instance: instance,
		Metro:    metro,
		Profile:  profile,
		key:      multimetro.Key(*ref),
	}
	err := mirror.Mirror(result, &result)
	if err != nil {
		return InstanceTemplate{}, fmt.Errorf("could not mirror instance template data: %w", err)
	}
	return result, nil
}

func (InstanceTemplate) Delete(ctx context.Context, targets []resource.Resource) error {
	keys := make(multimetro.Keys, 0, len(targets))
	for _, target := range targets {
		template := target.(InstanceTemplate)
		keys = append(keys, template.key)
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, keys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		log.G(ctx).Trace().Msg("deleting instance templates")
		templates, err := c.DeleteTemplateInstances(ctx, refs.NameOrUUIDs())
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, err
		}
		var deleted []group.Ref
		for _, template := range templates.Data.Instances {
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

func (InstanceTemplate) Edit(ctx context.Context, target resource.Resource, fields []resource.Field) (resource.Resource, error) {
	template := target.(InstanceTemplate)
	patches := patchRequests(fields, instanceTemplatePatchSpec)
	reqs := make([]platform.UpdateTemplateInstancesRequestItem, 0, len(patches))
	for _, patch := range patches {
		reqs = append(reqs, platform.UpdateTemplateInstancesRequestItem{
			Uuid:  &template.UUID,
			Op:    platform.UpdateTemplateInstancesRequestItemOp(patch.Op),
			Prop:  patch.Prop,
			Value: new(patch.Value),
		})
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	err = group.DoMetro(ctx, g, template.key.Metro, func(ctx context.Context, c multimetro.MetroClient) error {
		log.G(ctx).Trace().Msg("updating instance template")
		_, err := c.UpdateTemplateInstances(ctx, reqs)
		return err
	})
	if err != nil {
		return nil, err
	}
	results, err := InstanceTemplate{}.Get(ctx, []string{template.Key().String()})
	if err != nil {
		return nil, err
	}
	return results[0], nil
}

func instanceTemplatePatchSpec(path string, op patchOp, value any) (platform.UpdateTemplateInstancesRequestItemProp, any) {
	var zero platform.UpdateTemplateInstancesRequestItemProp
	switch path {
	case "tags":
		return platform.UpdateTemplateInstancesRequestItemPropTags, value.([]string)
	case "delete-lock":
		if value == nil {
			return zero, nil
		}
		switch v := value.(type) {
		case bool:
			return platform.UpdateTemplateInstancesRequestItemPropDelete_lock, v
		case *bool:
			return platform.UpdateTemplateInstancesRequestItemPropDelete_lock, *v
		}
		return zero, nil
	default:
		return zero, nil
	}
}

func (InstanceTemplate) Create(ctx context.Context, fields []resource.Field) ([]resource.Resource, error) {
	var instances []string
	for key, field := range resource.IterFields(fields) {
		if field.Create == nil || field.Create.Set == nil {
			continue
		}
		if key.String() == "instances" {
			instances = field.Create.Set.([]string)
		}
	}
	if len(instances) == 0 {
		return nil, fmt.Errorf("no instances provided")
	}

	// First, get the instances to verify they exist and to fully resolve their keys
	foundInstances, getErr := Instance{}.Get(ctx, instances)
	if getErr != nil && len(foundInstances) == 0 {
		return nil, getErr
	}
	if len(foundInstances) == 0 {
		return nil, fmt.Errorf("no instances found")
	}

	// Build refs grouped by metro from the found instances
	refsByMetro := make(map[string][]group.Ref)
	for _, res := range foundInstances {
		inst := res.(Instance)
		if inst.key.Metro == "" {
			return nil, fmt.Errorf("instance key %q not fully resolved", inst.key.String())
		}
		refsByMetro[inst.key.Metro] = append(refsByMetro[inst.key.Metro], inst.key.Ref())
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
				log.G(ctx).Trace().Str("ref", refStr).Msg("creating instance template")
				resp, err := c.CreateTemplateInstances(ctx, []platform.NameOrUUID{ref.NameOrUUID()})
				if err != nil {
					errs = append(errs, fmt.Errorf("failed to create template for %s: %w", refStr, err))
					continue
				}
				if len(resp.Data.Instances) == 0 {
					errs = append(errs, fmt.Errorf("no template created for %s", refStr))
					continue
				}
				for _, tmpl := range resp.Data.Instances {
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

	results, err := InstanceTemplate{}.Get(ctx, created.Strings())
	if err != nil {
		errs = append(errs, err)
	}
	return results, errors.Join(errs...)
}

func (InstanceTemplate) Examples() map[cmd.CmdType][]kingkong.Example {
	return map[cmd.CmdType][]kingkong.Example{
		cmd.CmdTypeGet: {
			{
				Description: "Inspect an instance template by name",
				Commands:    []string{"unikraft instance template get demo-template"},
			},
			{
				Description: "Inspect an instance template by UUID",
				Commands:    []string{"unikraft instance template get d4f0c132-a70e-4777-9631-7dfe08a1c15d"},
			},
			{
				Description: "Show template details in JSON format",
				Commands:    []string{"unikraft instance template get demo-template -o json"},
			},
		},
		cmd.CmdTypeList: {
			{
				Description: "List instance templates across metros",
				Commands:    []string{"unikraft instance template list"},
			},
			{
				Description: "List templates in table format",
				Commands:    []string{"unikraft instance template list -o table"},
			},
		},
		cmd.CmdTypeCreate: {
			{
				Description: "Convert a stopped instance into a template",
				Commands:    []string{"unikraft instance template create demo-instance"},
			},
			{
				Description: "Convert multiple instances into templates",
				Commands:    []string{"unikraft instance template create instance-1 instance-2"},
			},
		},
		cmd.CmdTypeEdit: {
			{
				Description: "Update template tags",
				Commands: []string{
					"unikraft instance template edit demo-template --tags env-prod,team-platform",
				},
			},
			{
				Description: "Lock a template to prevent deletion",
				Commands: []string{
					"unikraft instance template edit demo-template --delete-lock",
				},
			},
		},
		cmd.CmdTypeDelete: {
			{
				Description: "Delete an instance template",
				Commands:    []string{"unikraft instance template delete demo-template"},
			},
			{
				Description: "Delete all instance templates (with confirmation)",
				Commands:    []string{"unikraft instance template delete --all"},
			},
		},
	}
}
