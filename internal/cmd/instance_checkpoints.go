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
	"io"

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

type InstanceCheckpointsCmd struct {
	cmd.ResourceCmd[InstanceCheckpoint]
	cmd.GettableResourceCmd[InstanceCheckpoint]
	cmd.ListableResourceCmd[InstanceCheckpoint]
	cmd.WaitableResourceCmd[InstanceCheckpoint]
	cmd.BulkDeletableResourceCmd[InstanceCheckpoint]

	Create  cmd.ResourceCreateCmd[InstanceCheckpoint] `cmd:"" help:"Create a checkpoint from an instance."`
	Edit    cmd.ResourceEditCmd[InstanceCheckpoint]   `cmd:"" help:"Edit an instance checkpoint."`
	History InstanceCheckpointHistoryCmd              `cmd:"" help:"Show the history of a checkpoint."`
}

type InstanceCheckpoint struct {
	MetroName LinkName[Metro] `mirror:"metro.name" field:"metro,short"`
	Name      string          `mirror:"instance.name" field:",short"`
	UUID      string          `mirror:"instance.uuid" field:",long"`

	Tags        []string          `mirror:"instance.tags" field:",long" edit:"set,add,del" flag:"tag" sep:"none" help:"Checkpoint tag." placeholder:"tag" example:"env-dev"`
	Annotations map[string]string `mirror:"instance.annotations" field:",long"`
	DeleteLock  bool              `mirror:"instance.delete_lock" field:"delete-lock,long" edit:"set" flag:"delete-lock" help:"Prevent deletion of the checkpoint."`

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

	ScaleToZero InstanceScaleToZero `field:",embed" mirror:"instance.scale_to_zero"`

	Restart struct {
		Policy string `mirror:"instance.restart_policy"`
	}

	InstanceRef string          `field:"instance,invisible,valueless" create:"set,required" flag-arg:"instance" completion-predictor:"resource-key-instance" help:"Instance to create a checkpoint from."`
	WaitTimeout types.DurationS `field:"wait-timeout,invisible,valueless" create:"set"`

	Instance platform.Instance `field:"-" json:"instance"`
	Metro    *config.Metro     `field:"-" json:"metro"`
	Profile  *config.Profile   `field:"-" json:"profile"`

	key multimetro.Key
}

func (InstanceCheckpoint) Type() resource.Type {
	return resource.Type{
		Name:  "instance-checkpoint",
		Names: "instance-checkpoints",
	}
}

func (i InstanceCheckpoint) Key() resource.Key {
	return i.key
}

func (i InstanceCheckpoint) Raw() any {
	return i.Instance
}

func (i InstanceCheckpoint) Fields(ctx context.Context) ([]resource.Field, error) {
	return resource.FieldsFromStruct(i)
}

func (InstanceCheckpoint) List(ctx context.Context) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, err
	}
	return group.CollectAllSlices(ctx, g, func(ctx context.Context, c multimetro.MetroClient) ([]resource.Resource, error) {
		log.G(ctx).Trace().Msg("listing instance checkpoints")
		resp, err := c.GetCheckpointInstances(ctx, nil, platform.GetCheckpointInstancesOpts{Details: new(true)})
		if err != nil {
			return nil, err
		}
		var results []resource.Resource
		var errs []error
		if resp == nil || resp.Data == nil {
			return nil, nil
		}
		for _, instance := range resp.Data.Instances {
			result, err := InstanceCheckpoint{}.load(nil, instance, &c.Metro, profile)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			results = append(results, result)
		}
		return results, errors.Join(errs...)
	})
}

func (InstanceCheckpoint) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, err
	}
	return group.CollectRefsSlices(ctx, g, multimetro.ParseKeys(keys).Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) ([]resource.Resource, group.Refs, error) {
		log.G(ctx).Trace().Msg("getting instance checkpoints")
		resp, err := c.GetCheckpointInstances(ctx, refs.NameOrUUIDs(), platform.GetCheckpointInstancesOpts{Details: new(true)})
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
			result, err := InstanceCheckpoint{}.load(matchedRef, instance, &c.Metro, profile)
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

func (InstanceCheckpoint) load(ref *group.Ref, instance platform.Instance, metro *config.Metro, profile *config.Profile) (InstanceCheckpoint, error) {
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

	result := InstanceCheckpoint{
		Instance: instance,
		Metro:    metro,
		Profile:  profile,
		key:      multimetro.Key(*ref),
	}
	err := mirror.Mirror(result, &result)
	if err != nil {
		return InstanceCheckpoint{}, fmt.Errorf("could not mirror instance checkpoint data: %w", err)
	}
	return result, nil
}

func (InstanceCheckpoint) Delete(ctx context.Context, keys []string) error {
	parsedKeys := multimetro.ParseKeys(keys)

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, parsedKeys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		log.G(ctx).Trace().Msg("deleting instance checkpoints")
		resp, err := c.DeleteCheckpointInstances(ctx, refs.NameOrUUIDs())
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, err
		}
		var deleted []group.Ref
		for _, cp := range resp.Data.Instances {
			status := cp.Status
			if status != "" && status != platform.ResponseStatusSuccess {
				continue
			}
			deleted = append(deleted, group.Ref{
				Metro: c.Metro.Name,
				Name:  cp.Name,
				UUID:  cp.Uuid,
			})
		}
		return deleted, nil
	})
}

func (InstanceCheckpoint) Edit(ctx context.Context, key string, fields []resource.Field) error {
	parsedKeys := multimetro.ParseKeys([]string{key})
	patches, err := patchRequests(fields, instanceCheckpointPatchSpec)
	if err != nil {
		return err
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, parsedKeys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		reqs := make([]platform.UpdateCheckpointInstancesRequestItem, 0, len(refs)*len(patches))
		for _, ref := range refs {
			for _, patch := range patches {
				req := platform.UpdateCheckpointInstancesRequestItem{
					Op:    platform.MutableCheckpointInstanceOperation(patch.Op),
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
		log.G(ctx).Trace().Msg("updating instance checkpoint")
		_, err := c.UpdateCheckpointInstances(ctx, reqs)
		if err != nil {
			if platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
				return nil, nil
			}
			return nil, err
		}
		return refs, nil
	})
}

func instanceCheckpointPatchSpec(path string, op patchOp, value any) (platform.MutableCheckpointInstanceProperty, any, error) {
	var zero platform.MutableCheckpointInstanceProperty
	switch path {
	case "tags":
		return platform.MutableCheckpointInstancePropertyTags, value.([]string), nil
	case "delete-lock":
		if value == nil {
			return zero, nil, nil
		}
		switch v := value.(type) {
		case bool:
			return platform.MutableCheckpointInstancePropertyDeleteLock, v, nil
		case *bool:
			return platform.MutableCheckpointInstancePropertyDeleteLock, *v, nil
		}
		return zero, nil, nil
	default:
		return zero, nil, nil
	}
}

func (InstanceCheckpoint) Create(ctx context.Context, fields []resource.Field) ([]resource.Resource, error) {
	var instance string
	var timeoutS *int64
	for key, field := range resource.IterFields(fields) {
		if field.Create == nil || field.Create.Set == nil {
			continue
		}
		switch key.String() {
		case "instance":
			instance = field.Create.Set.(string)
		case "wait-timeout":
			timeout := field.Create.Set.(types.DurationS)
			timeoutS = new(int64(timeout))
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
		log.G(ctx).Trace().Str("ref", refStr).Msg("creating instance checkpoint")
		req := platform.CreateCheckpointInstancesRequestItem{
			From:     ref.NameOrUUID(),
			TimeoutS: timeoutS,
		}
		resp, err := c.CreateCheckpointInstances(ctx, []platform.CreateCheckpointInstancesRequestItem{req})
		if err != nil {
			return nil, fmt.Errorf("failed to create checkpoint for %s: %w", refStr, err)
		}
		if resp == nil || resp.Data == nil || len(resp.Data.Instances) == 0 {
			return nil, fmt.Errorf("no checkpoint created for %s", refStr)
		}
		var created multimetro.Keys
		var errs []error
		for _, cp := range resp.Data.Instances {
			status := cp.Status
			if status != "" && status != platform.ResponseStatusSuccess {
				name := cmp.Or(cp.Name, cp.Uuid)
				message := ptr.ZeroIfNil(cp.Message)
				if message == "" {
					message = "unknown error"
				}
				errs = append(errs, fmt.Errorf("checkpoint create failed for %s: %s", name, message))
				continue
			}
			created = append(created, multimetro.Key{
				Metro: c.Metro.Name,
				UUID:  cp.Uuid,
				Name:  cp.Name,
			})
		}
		return created, errors.Join(errs...)
	})
	if err != nil {
		return nil, err
	}
	if len(created) == 0 {
		return nil, fmt.Errorf("no checkpoint created for %s", refStr)
	}

	return InstanceCheckpoint{}.Get(ctx, created.Strings())
}

func (InstanceCheckpoint) Examples() map[cmd.CmdType][]kingkong.Example {
	return map[cmd.CmdType][]kingkong.Example{
		cmd.CmdTypeGet: {
			{
				Description: "Inspect a checkpoint by name or UUID",
				Commands:    []string{"unikraft instance checkpoint get my-checkpoint"},
			},
		},
		cmd.CmdTypeList: {
			{
				Description: "List checkpoints across metros",
				Commands:    []string{"unikraft instance checkpoint list"},
			},
		},
		cmd.CmdTypeCreate: {
			{
				Description: "Create a checkpoint from a running instance",
				Commands:    []string{"unikraft instance checkpoint create my-instance"},
			},
		},
		cmd.CmdTypeEdit: {
			{
				Description: "Update checkpoint tags",
				Commands: []string{
					"unikraft instance checkpoint edit my-checkpoint --set tags=env-dev",
				},
			},
		},
		cmd.CmdTypeDelete: {
			{
				Description: "Delete a checkpoint",
				Commands:    []string{"unikraft instance checkpoint delete my-checkpoint"},
			},
		},
	}
}

// InstanceHistoryEntry represents a single checkpoint in the history of an
// instance or checkpoint. It models one row of history output as a resource so
// that it can be rendered through the standard printer framework.
type InstanceHistoryEntry struct {
	Metro   LinkName[Metro]    `field:"metro,short" json:"metro"`
	Target  string             `field:",short" json:"target"`
	Name    string             `field:",short" json:"name"`
	UUID    string             `field:",long" json:"uuid"`
	Created types.RelativeTime `field:"created,short" json:"created"`

	key multimetro.Key
}

func (InstanceHistoryEntry) Type() resource.Type {
	return resource.Type{
		Name:  "instance-history-entry",
		Names: "instance-history-entries",
	}
}

func (i InstanceHistoryEntry) Key() resource.Key {
	return i.key
}

func (i InstanceHistoryEntry) Raw() any {
	return i
}

func (i InstanceHistoryEntry) Fields(ctx context.Context) ([]resource.Field, error) {
	return resource.FieldsFromStruct(i)
}

// getInstanceHistory fetches checkpoint history entries for the given targets
// using the provided fetch function (either the instance or checkpoint history
// endpoint) and flattens them into InstanceHistoryEntry rows.
func getInstanceHistory(ctx context.Context, targets []string, fetch func(ctx context.Context, c multimetro.MetroClient, ids []platform.NameOrUUID) (*platform.Response[platform.GetCheckpointHistoryResponseData], error)) ([]resource.Resource, error) {
	keys := multimetro.ParseKeys(targets)

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	return group.CollectRefsSlices(ctx, g, keys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) ([]resource.Resource, group.Refs, error) {
		log.G(ctx).Trace().Msg("getting checkpoint history")
		resp, err := fetch(ctx, c, refs.NameOrUUIDs())
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, nil, err
		}
		var found []group.Ref
		var results []resource.Resource
		if resp == nil || resp.Data == nil {
			return nil, nil, nil
		}
		for _, inst := range resp.Data.Instances {
			if inst.Status == nil || *inst.Status != platform.ResponseStatusSuccess {
				continue
			}
			found = append(found, group.Ref{
				Metro: c.Metro.Name,
				Name:  inst.Name,
				UUID:  inst.Uuid,
			})
			target := multimetro.Key{Metro: c.Metro.Name, Name: inst.Name, UUID: inst.Uuid}
			for _, entry := range inst.History {
				results = append(results, InstanceHistoryEntry{
					Metro:   LinkName[Metro](c.Metro.Name),
					Target:  target.String(),
					Name:    entry.Name,
					UUID:    entry.Uuid,
					Created: types.RelativeTime(entry.CreatedAt),
					key:     multimetro.Key{Metro: c.Metro.Name, Name: entry.Name, UUID: entry.Uuid},
				})
			}
		}
		return results, found, nil
	})
}

// renderInstanceHistory prints history entries through the printer framework.
func renderInstanceHistory(ctx context.Context, out io.Writer, opts cmd.FormatOpts, entries []resource.Resource) error {
	return opts.Output.
		WithDefault(cmd.PrinterTypeTable).
		Print(ctx, out, opts.Field, InstanceHistoryEntry{}, entries...)
}

// InstanceCheckpointHistoryCmd shows the checkpoint history for a checkpoint.
type InstanceCheckpointHistoryCmd struct {
	Targets []string `arg:"" name:"target" completion-predictor:"resource-key-instance-checkpoint" help:"Target checkpoints to show history for."`

	cmd.FormatOpts
}

func (cmd InstanceCheckpointHistoryCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Show the history of a checkpoint",
			Commands:    []string{"unikraft instance checkpoint history my-checkpoint"},
		},
	}
}

func (c *InstanceCheckpointHistoryCmd) Run(ctx context.Context, stdio config.Stdio) error {
	entries, err := getInstanceHistory(ctx, c.Targets, func(ctx context.Context, mc multimetro.MetroClient, ids []platform.NameOrUUID) (*platform.Response[platform.GetCheckpointHistoryResponseData], error) {
		return mc.GetCheckpointHistory(ctx, ids, platform.GetCheckpointHistoryOpts{})
	})
	if err != nil && len(entries) == 0 {
		return err
	}
	if printErr := renderInstanceHistory(ctx, stdio.Stdout, c.FormatOpts, entries); printErr != nil {
		return errors.Join(err, printErr)
	}
	return err
}
