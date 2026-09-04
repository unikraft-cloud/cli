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
	"unikraft.com/cli/internal/timeouts"
	"unikraft.com/cli/internal/types"
)

type InstanceTemplatesCmd struct {
	cmd.ResourceCmd[InstanceTemplate]
	cmd.GettableResourceCmd[InstanceTemplate]
	cmd.ListableResourceCmd[InstanceTemplate]
	cmd.BulkDeletableResourceCmd[InstanceTemplate]

	Create cmd.ResourceCreateCmd[InstanceTemplate] `cmd:"" help:"Create an instance template."`
	Edit   cmd.ResourceEditCmd[InstanceTemplate]   `cmd:"" help:"Edit an instance template."`
}

type InstanceTemplate struct {
	Metro LinkName[Metro] `field:"metro,short"`
	Name  string          `mirror:"instance.name" field:",short"`
	UUID  string          `mirror:"instance.uuid" field:",long"`

	Tags        []string          `mirror:"instance.tags" field:",long" edit:"set,add,del" flag:"tag" sep:"none" help:"Template tag." placeholder:"tag" example:"env-dev"`
	Annotations map[string]string `mirror:"instance.annotations" field:",long"`
	DeleteLock  bool              `mirror:"instance.delete_lock" field:"delete-lock,long" edit:"set" flag:"delete-lock" help:"Prevent deletion of the template."`

	Autokill Autokill `field:",embed" mirror:"instance.template_autokill" create:"set" edit:"set" flag:"autokill" help:"Autokill options.\n  time: time without a clone before the template is deleted" placeholder:"<key>=<value>" example:"time=24h"`

	State types.InstanceState             `mirror:"instance.state" field:",short"`
	Image types.ImageRef[reference.Named] `mirror:"instance.image" field:",short"`
	Type_ *platform.InstanceType          `mirror:"instance.type" field:"type,long"`

	Runtime struct {
		Args InstanceArgs      `mirror:"instance.args" field:",short"`
		Env  map[string]string `mirror:"instance.env" field:",long"`
	}

	Resources struct {
		Memory types.SizeMebibytes `mirror:"instance.memory_mb" field:",short"`
		VCPUs  int                 `mirror:"instance.vcpus" field:"vcpus,short"`
	}

	Volumes []InstanceTemplateVolume `mirror:"instance.volumes" field:",embed"`

	Snapshot struct {
		UUID string `mirror:"instance.snapshot.uuid" field:",long"`
	}

	Timestamps struct {
		Created types.RelativeTime `mirror:"instance.created_at" field:",short"`
	}

	Timing struct {
		BootTime types.DurationUS `mirror:"instance.boot_time_us" field:",long"`
		NetTime  types.DurationUS `mirror:"instance.net_time_us"`
	}

	ScaleToZero InstanceScaleToZero `field:",embed" mirror:"instance.scale_to_zero"`

	Restart struct {
		Policy string `mirror:"instance.restart_policy"`
	}

	InstanceRef string `field:"instance,invisible,valueless" create:"set,required" flag-arg:"instance" completion-predictor:"resource-key-instance" help:"Instance to convert into a template."`

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
	case "autokill":
		autokill := value.(Autokill)
		req := map[string]any{}
		if autokill.TimeMs > 0 {
			req["time_ms"] = uint64(autokill.TimeMs)
		}
		return platform.MutableTemplateInstancePropertyAutokill, req, nil
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
	var autokill Autokill
	for key, field := range resource.IterFields(fields) {
		if field.Create == nil || field.Create.Set == nil {
			continue
		}
		switch key.String() {
		case "instance":
			instance = field.Create.Set.(string)
		case "autokill":
			autokill = field.Create.Set.(Autokill)
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

	createdData := make(map[string]createdResource[platform.Instance])
	created, err := group.CollectMetro(ctx, g, inst.key.Metro, func(ctx context.Context, c multimetro.MetroClient) (multimetro.Keys, error) {
		var reqItem platform.CreateTemplateInstancesRequestItem
		if ref.Name != "" {
			reqItem.Name = new(ref.Name)
		} else {
			reqItem.Uuid = new(ref.UUID)
		}
		if autokill.TimeMs > 0 {
			t := uint64(autokill.TimeMs)
			reqItem.Autokill = &platform.ItemAutokill{TimeMs: &t}
		}
		log.G(ctx).Trace().Str("ref", refStr).Msg("creating instance template")
		reqItem.TimeoutS = new(int64(-1))
		resp, err := timeouts.TryWithFallback(
			ctx,
			[]platform.CreateTemplateInstancesRequestItem{reqItem},
			c.CreateTemplateInstances,
		)
		if _, err = timeouts.Tolerate(ctx, resp, err, "template create did not complete"); err != nil {
			return nil, fmt.Errorf("failed to create template for %s: %w", refStr, err)
		}
		if resp == nil || resp.Data == nil || len(resp.Data.Instances) == 0 {
			return nil, fmt.Errorf("no template created for %s", refStr)
		}
		var created multimetro.Keys
		var errs []error
		for _, tmpl := range resp.Data.Instances {
			name := cmp.Or(tmpl.Name, tmpl.Uuid)
			status := tmpl.Status
			failed := status != "" && status != platform.ResponseStatusSuccess
			// A UUID means the template exists
			if tmpl.Uuid == "" && failed {
				message := ptr.ZeroIfNil(tmpl.Message)
				if message == "" {
					message = "unknown error"
				}
				errs = append(errs, fmt.Errorf("template create failed for %s: %s", name, message))
				continue
			}
			if failed {
				log.G(ctx).Warn().
					Str("template", name).
					Str("state", string(tmpl.State)).
					Str("reason", ptr.ZeroIfNil(tmpl.Message)).
					Msg("template has not reached the template state yet")
			}
			key := multimetro.Key{
				Metro: c.Metro.Name,
				UUID:  tmpl.Uuid,
				Name:  tmpl.Name,
			}
			createdData[key.String()] = createdResource[platform.Instance]{
				data: platform.Instance{
					Uuid:  tmpl.Uuid,
					Name:  tmpl.Name,
					State: tmpl.State,
				},
				metro: c.Metro,
			}
			created = append(created, key)
		}
		return created, errors.Join(errs...)
	})
	if err != nil && len(created) == 0 {
		return nil, err
	}
	if len(created) == 0 {
		return nil, fmt.Errorf("no template created for %s", refStr)
	}

	var errs []error
	if err != nil {
		errs = append(errs, err)
	}

	results, err := InstanceTemplate{}.Get(ctx, created.Strings())
	if notFound, ok := errors.AsType[group.ErrRefNotFound](err); ok {
		recovered, missing := recoverCreated(ctx, notFound.Refs, createdData, InstanceTemplate{}.load)
		results = append(results, recovered...)
		if len(missing) > 0 {
			errs = append(errs, group.ErrRefNotFound{Refs: missing})
		}
	} else if err != nil {
		errs = append(errs, err)
	}
	return results, errors.Join(errs...)
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
