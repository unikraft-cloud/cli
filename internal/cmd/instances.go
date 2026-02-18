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

	"github.com/google/uuid"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/logs"
	"unikraft.com/cli/internal/mirror"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/muxreader"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/cli/internal/types"
)

type InstancesCmd struct {
	cmd.ResourceCmd[Instance]
	cmd.GettableResourceCmd[Instance]      `set:"name=instance" set:"names=instances"`
	cmd.WaitableResourceCmd[Instance]      `set:"name=instance" set:"names=instances"`
	cmd.ListableResourceCmd[Instance]      `set:"name=instance" set:"names=instances"`
	cmd.BulkDeletableResourceCmd[Instance] `set:"name=instance" set:"names=instances"`
	cmd.EditableResourceCmd[Instance]      `set:"name=instance" set:"names=instances"`
	cmd.CreatableResourceCmd[Instance]     `set:"name=instance" set:"names=instances"`

	Logs    InstancesLogsCmd    `cmd:"" help:"Fetch and display instance logs."`
	Start   InstancesStartCmd   `cmd:"" help:"Start one or more instances."`
	Stop    InstancesStopCmd    `cmd:"" help:"Stop one or more instances."`
	Restart InstancesRestartCmd `cmd:"" help:"Restart one or more instances."`
}

type Instance struct {
	MetroName string `mirror:"metro.name" field:"metro,short" create:"set,required"`
	Name      string `mirror:"instance.name" field:",short" create:"set"`
	UUID      string `mirror:"instance.uuid" field:",long"`

	Tags []string `mirror:"instance.tags"`

	State types.InstanceState `mirror:"instance.state" field:",short" edit:"set"`
	Image string              `mirror:"instance.image" field:",short" create:"set,required" edit:"set"`

	Runtime struct {
		Args []string          `mirror:"instance.args" field:",short" create:"set" edit:"set"`
		Env  map[string]string `mirror:"instance.env" field:",long" create:"set" edit:"set,add,del=keys"`
	}

	Resources struct {
		Memory types.SizeMebibytes `mirror:"instance.memory_mb" field:",short" create:"set" edit:"set"`
		VCPUs  int                 `mirror:"instance.vcpus" field:"vcpus,short" create:"set" edit:"set"`
	}

	Volumes []*InstanceVolume `mirror:"instance.volumes" field:",embed" create:"set"`

	Service struct {
		UUID    string   `mirror:"uuid" field:",long" create:"set"`
		Name    string   `mirror:"name" field:",long" create:"set"`
		Domains []Domain `mirror:"domains" field:",short,embed" create:"set"`

		// create-only fields
		Services  []*Service `field:",invisible,embed" create:"set"`
		SoftLimit uint32     `field:",invisible" create:"set"`
		HardLimit uint32     `field:",invisible" create:"set"`
	} `mirror:"instance.service_group"`

	Networks []struct {
		UUID      string `mirror:"uuid" field:",long"`
		PrivateIP string `mirror:"private_ip" field:",long"`
		MAC       string `mirror:"mac" field:",long"`
	} `mirror:"instance.network_interfaces"`

	Timestamps struct {
		Created types.RelativeTime `mirror:"instance.created_at" field:",short"`
		Started types.RelativeTime `mirror:"instance.started_at"`
		Stopped types.RelativeTime `mirror:"instance.stopped_at"`
	}

	ScaleToZero InstanceScaleToZero `field:",embed" mirror:"instance.scale_to_zero"`

	Timing struct {
		Uptime   types.DurationMS `mirror:"instance.uptime_ms"`
		BootTime types.DurationUS `mirror:"instance.boot_time_us"`
		NetTime  types.DurationUS `mirror:"instance.net_time_us"`
	}

	Restart struct {
		Policy       string `mirror:"instance.restart_policy" create:"set"`
		StartCount   int    `mirror:"instance.start_count"`
		RestartCount int    `mirror:"instance.restart_count"`
	}

	Stop struct {
		Reason   int `mirror:"instance.stop_reason"`
		ExitCode int `mirror:"instance.exit_code"`
		Code     int `mirror:"instance.stop_code"`
	}

	// create-only fields
	Autostart   bool            `field:",invisible" create:"set"`
	Replicas    int64           `field:",invisible" create:"set"`
	WaitTimeout types.DurationS `field:",invisible" create:"set"`
	Features    []string        `field:",invisible" create:"set"`

	Instance platform.Instance `field:"-" json:"instance"`
	Metro    *config.Metro     `field:"-" json:"metro"`
	Profile  *config.Profile   `field:"-" json:"profile"`
}

type InstanceVolume struct {
	UUID     string `mirror:"uuid" json:"uuid,omitempty" field:",long"`
	Name     string `mirror:"name" json:"name,omitempty" field:",long"`
	At       string `mirror:"at" json:"at" field:",long"`
	Readonly bool   `mirror:"readonly" json:"readonly,omitempty" field:",long"`

	// create-only field
	Size types.SizeMebibytes `field:",invisible,embed" create:"set"`
}

func (v *InstanceVolume) MarshalText() ([]byte, error) {
	parts := []string{
		cmp.Or(v.Name, v.UUID),
		v.At,
	}
	if v.Readonly {
		parts = append(parts, "ro")
	}
	if v.Size > 0 {
		s, err := value.Format(v.Size)
		if err != nil {
			return nil, err
		}
		parts = append(parts, fmt.Sprintf("size=%s", s))
	}
	return []byte(strings.Join(parts, ":")), nil
}

func (v *InstanceVolume) UnmarshalText(data []byte) error {
	str := string(data)
	parts := strings.Split(str, ":")
	if len(parts) < 2 {
		return fmt.Errorf("invalid volume format, expected NAME:AT, got %q", str)
	}

	name, at, parts := parts[0], parts[1], parts[2:]
	readonly := false
	size := types.SizeMebibytes(0)

	var err error
	for _, part := range parts {
		k, v, ok := strings.Cut(part, "=")
		switch k {
		case "ro":
			if ok {
				readonly, err = strconv.ParseBool(v)
				if err != nil {
					return err
				}
			} else {
				readonly = true
			}
		case "size":
			size, err = value.Parse[types.SizeMebibytes]([]string{v})
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("invalid volume option: %q", k)
		}
	}

	if name == "" {
		v.Size = size
	} else if uuid.Validate(name) == nil {
		v.UUID = name
	} else {
		v.Name = name
	}
	v.At = at
	v.Readonly = readonly

	return nil
}

type InstanceScaleToZero struct {
	Enabled      bool   `mirror:"enabled" field:",long"`
	Policy       string `mirror:"policy" field:",long" create:"set" edit:"set"`
	Stateful     bool   `mirror:"stateful" field:",long" create:"set" edit:"set"`
	CooldownTime int64  `mirror:"cooldown_time_ms" field:",long" create:"set" edit:"set"`
}

func (Instance) Type() resource.Type {
	return resource.Type{
		Name:  "instance",
		Names: "instances",
	}
}

func (i Instance) key() multimetro.Key {
	return multimetro.Key{
		Metro: i.MetroName,
		Name:  i.Name,
		UUID:  i.UUID,
	}
}

func (i Instance) Key() resource.Key {
	return i.key()
}

func (i Instance) Raw() any {
	return i.Instance
}

func (i Instance) Fields() ([]resource.Field, error) {
	result, err := resource.FieldsFromStruct(i)
	if err != nil {
		return nil, err
	}

	for key, field := range resource.IterFields(result) {
		switch key.String() {
		case "name":
			field.Hyperlink = i.hyperlink()
		case "service":
			nameField, _ := field.Get("name")
			uuidField, _ := field.Get("uuid")
			name, _ := nameField.Value.(string)
			uuid, _ := uuidField.Value.(string)
			if name != "" || uuid != "" {
				field.Links = append(field.Links, resource.Link{
					Type: "service",
					Key: multimetro.Key{
						Metro: i.Metro.Name.String(),
						Name:  name,
						UUID:  uuid,
					}.String(),
				})
			}
		}
	}

	return result, nil
}

func (i Instance) hyperlink() string {
	if i.Profile == nil || i.Profile.ControlPlane.String() == "" {
		return ""
	}
	if i.Name == "" || i.Profile.Organization.String() == "" {
		return ""
	}
	return fmt.Sprintf(
		"https://console.unikraft.cloud/org/%s/instances/%s/%s",
		i.Profile.Organization.String(),
		i.MetroName,
		i.Name,
	)
}

func (Instance) List(ctx context.Context) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, err
	}
	return group.CollectAllSlices(ctx, g, func(ctx context.Context, c multimetro.MetroClient) ([]resource.Resource, error) {
		log.G(ctx).Trace().Msg("listing instances")
		resp, err := c.GetInstances(ctx, nil, true)
		if err != nil {
			return nil, err
		}
		var results []resource.Resource
		for _, instance := range resp.Data.Instances {
			result, err := Instance{}.load(instance, &c.Metro, profile)
			if err != nil {
				return nil, err
			}
			results = append(results, result)
		}
		return results, nil
	})
}

func (Instance) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, err
	}
	return group.CollectRefsSlices(ctx, g, multimetro.ParseKeys(keys).Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) ([]resource.Resource, group.Refs, error) {
		log.G(ctx).Trace().Msg("getting instances")
		resp, err := c.GetInstances(ctx, refs.NameOrUUIDs(), true)
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, nil, err
		}
		var found []group.Ref
		var results []resource.Resource
		for _, instance := range resp.Data.Instances {
			if instance.Status == nil || *instance.Status != platform.ResponseStatusSUCCESS {
				continue
			}
			result, err := Instance{}.load(instance, &c.Metro, profile)
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

func (Instance) load(instance platform.Instance, metro *config.Metro, profile *config.Profile) (Instance, error) {
	result := Instance{
		Instance: instance,
		Metro:    metro,
		Profile:  profile,
	}
	err := mirror.Mirror(result, &result)
	if err != nil {
		return Instance{}, fmt.Errorf("could not mirror instance data: %w", err)
	}

	if name, _, ok := strings.Cut(result.Image, "@"); ok {
		result.Image = name
	}
	return result, nil
}

func (Instance) Delete(ctx context.Context, targets []resource.Resource) error {
	keys := make(multimetro.Keys, 0, len(targets))
	for _, target := range targets {
		instance := target.(Instance)
		keys = append(keys, instance.key())
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, keys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		log.G(ctx).Trace().Msg("deleting instances")
		instances, err := c.DeleteInstances(ctx, refs.NameOrUUIDs())
		if err != nil {
			return nil, err
		}
		var deleted []group.Ref
		for _, instance := range instances.Data.Instances {
			if instance.Status == nil || *instance.Status != platform.ResponseStatusSUCCESS {
				continue
			}
			deleted = append(deleted, group.Ref{
				Metro: c.Metro.Name.String(),
				Name:  *instance.Name,
				UUID:  *instance.Uuid,
			})
		}
		return deleted, nil
	})
}

func (Instance) Edit(ctx context.Context, target resource.Resource, fields []resource.Field) (resource.Resource, error) {
	instance := target.(Instance)

	targetState := instance.State
	if fields := resource.GetFieldByPath(fields, resource.FieldPath{"state"}); len(fields) > 0 {
		field := fields[0]
		targetState = field.Edit.Set.(types.InstanceState)
		field.Edit = nil
	}

	patches := patchRequests(fields, instancePatchSpec)
	reqs := make([]platform.UpdateInstancesRequestItem, 0, len(patches))
	for _, patch := range patches {
		reqs = append(reqs, platform.UpdateInstancesRequestItem{
			Uuid:  &instance.UUID,
			Op:    platform.UpdateInstancesRequestItemOp(patch.Op),
			Prop:  patch.Prop,
			Value: platform.Ptr(patch.Value),
		})
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	if instance.State.IsRunning() && !targetState.IsRunning() {
		_, err := stopInstances(ctx, g, multimetro.Keys{instance.key()}, StopOpts{DrainTimeout: -1})
		if err != nil {
			return nil, err
		}
	}
	if len(reqs) > 0 {
		err = group.DoMetro(ctx, g, instance.key().Metro, func(ctx context.Context, c multimetro.MetroClient) error {
			log.G(ctx).Trace().Msg("updating instance")
			_, err := c.UpdateInstances(ctx, reqs)
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	if !instance.State.IsRunning() && targetState.IsRunning() {
		_, err := startInstances(ctx, g, multimetro.Keys{instance.key()})
		if err != nil {
			return nil, err
		}
	}
	results, err := Instance{}.Get(ctx, []string{instance.Key().String()})
	if err != nil {
		return nil, err
	}
	return results[0], nil
}

func instancePatchSpec(path string, op patchOp, value any) (platform.UpdateInstancesRequestItemProp, any) {
	var zero platform.UpdateInstancesRequestItemProp
	switch path {
	case "image":
		return platform.UpdateInstancesRequestItemPropImage, value.(string)
	case "runtime.args":
		return platform.UpdateInstancesRequestItemPropArgs, value.([]string)
	case "runtime.env":
		if op == patchOpDel {
			return platform.UpdateInstancesRequestItemPropEnv, value.([]string)
		}
		return platform.UpdateInstancesRequestItemPropEnv, value.(map[string]string)
	case "resources.memory":
		return platform.UpdateInstancesRequestItemPropMemory_mb, int64(value.(types.SizeMebibytes))
	case "resources.vcpus":
		return platform.UpdateInstancesRequestItemPropVcpus, value.(int)
	case "scale-to-zero.policy":
		return platform.UpdateInstancesRequestItemPropScale_to_zero, map[string]any{"policy": value.(string)}
	case "scale-to-zero.stateful":
		return platform.UpdateInstancesRequestItemPropScale_to_zero, map[string]any{"stateful": value.(bool)}
	case "scale-to-zero.cooldown-time":
		return platform.UpdateInstancesRequestItemPropScale_to_zero, map[string]any{"cooldown_time_ms": int32(value.(int64))}
	default:
		return zero, nil
	}
}

func (Instance) Create(ctx context.Context, fields []resource.Field) ([]resource.Resource, error) {
	var req platform.CreateInstanceRequest
	var metro string
	for key, field := range resource.IterFields(fields) {
		if field.Create == nil || field.Create.Set == nil {
			continue
		}
		switch key.String() {
		case "name":
			name := field.Create.Set.(string)
			req.Name = &name
		case "metro":
			metro = field.Create.Set.(string)
		case "image":
			req.Image = field.Create.Set.(string)
		case "runtime.args":
			req.Args = field.Create.Set.([]string)
		case "runtime.env":
			req.Env = field.Create.Set.(map[string]string)
		case "resources.memory":
			mem := int64(field.Create.Set.(types.SizeMebibytes))
			req.MemoryMb = &mem
		case "resources.vcpus":
			vcpus := int32(field.Create.Set.(int))
			req.Vcpus = &vcpus
		case "restart.policy":
			policy := platform.CreateInstanceRequestRestartPolicy(field.Create.Set.(string))
			req.RestartPolicy = &policy
		case "scale-to-zero.policy":
			if req.ScaleToZero == nil {
				req.ScaleToZero = &platform.CreateInstanceRequestScaleToZero{}
			}
			policy := platform.CreateInstanceRequestScaleToZeroPolicy(field.Create.Set.(string))
			req.ScaleToZero.Policy = &policy
		case "scale-to-zero.stateful":
			if req.ScaleToZero == nil {
				req.ScaleToZero = &platform.CreateInstanceRequestScaleToZero{}
			}
			stateful := field.Create.Set.(bool)
			req.ScaleToZero.Stateful = &stateful
		case "scale-to-zero.cooldown-time":
			if req.ScaleToZero == nil {
				req.ScaleToZero = &platform.CreateInstanceRequestScaleToZero{}
			}
			cooldown := int32(field.Create.Set.(int64))
			req.ScaleToZero.CooldownTimeMs = &cooldown
		case "volumes":
			for _, vol := range field.Create.Set.([]*InstanceVolume) {
				reqVol := platform.CreateInstanceRequestVolume{
					At: vol.At,
				}
				if vol.UUID != "" {
					reqVol.Uuid = &vol.UUID
				}
				if vol.Name != "" {
					reqVol.Name = &vol.Name
				}
				if vol.Size > 0 {
					reqVol.SizeMb = ptr.ToPtr(int64(vol.Size))
				}
				if vol.Readonly {
					reqVol.Readonly = &vol.Readonly
				}
				req.Volumes = append(req.Volumes, reqVol)
			}
		case "service.uuid":
			uuid := field.Create.Set.(string)
			if uuid != "" {
				if req.ServiceGroup == nil {
					req.ServiceGroup = &platform.CreateInstanceRequestServiceGroup{}
				}
				req.ServiceGroup.Uuid = &uuid
			}
		case "service.name":
			name := field.Create.Set.(string)
			if name != "" {
				if req.ServiceGroup == nil {
					req.ServiceGroup = &platform.CreateInstanceRequestServiceGroup{}
				}
				req.ServiceGroup.Name = &name
			}
		case "service.services":
			services := field.Create.Set.([]*Service)
			if len(services) > 0 {
				if req.ServiceGroup == nil {
					req.ServiceGroup = &platform.CreateInstanceRequestServiceGroup{}
				}
				for _, svc := range services {
					req.ServiceGroup.Services = append(req.ServiceGroup.Services, platform.Service{
						Port:            svc.Source,
						DestinationPort: &svc.Destination,
						Handlers:        svc.Handlers,
					})
				}
			}
		case "service.domains":
			domains := field.Create.Set.([]Domain)
			if len(domains) > 0 {
				if req.ServiceGroup == nil {
					req.ServiceGroup = &platform.CreateInstanceRequestServiceGroup{}
				}
				for _, domain := range domains {
					name := domain.Name
					if name == "" {
						name = domain.FQDN + "."
					}
					req.ServiceGroup.Domains = append(req.ServiceGroup.Domains, platform.CreateInstanceRequestDomain{
						Name: name,
					})
				}
			}
		case "service.soft-limit":
			limit := field.Create.Set.(uint32)
			if limit > 0 {
				if req.ServiceGroup == nil {
					req.ServiceGroup = &platform.CreateInstanceRequestServiceGroup{}
				}
				req.ServiceGroup.SoftLimit = &limit
			}
		case "service.hard-limit":
			limit := field.Create.Set.(uint32)
			if limit > 0 {
				if req.ServiceGroup == nil {
					req.ServiceGroup = &platform.CreateInstanceRequestServiceGroup{}
				}
				req.ServiceGroup.HardLimit = &limit
			}
		case "autostart":
			autostart := field.Create.Set.(bool)
			req.Autostart = &autostart
		case "replicas":
			replicas := field.Create.Set.(int64)
			req.Replicas = &replicas
		case "wait-timeout":
			timeout := field.Create.Set.(types.DurationS)
			req.TimeoutS = ptr.ToPtr(int64(timeout))
		case "features":
			features := field.Create.Set.([]string)
			for _, f := range features {
				req.Features = append(req.Features, platform.CreateInstanceRequestFeatures(f))
			}
		}
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	keys, err := group.CollectMetro(ctx, g, metro, func(ctx context.Context, c multimetro.MetroClient) (multimetro.Keys, error) {
		log.G(ctx).Trace().Msg("creating instance")
		resp, err := c.CreateInstance(ctx, req)
		if err != nil {
			return nil, err
		}
		if len(resp.Data.Instances) == 0 {
			return nil, fmt.Errorf("no instances created")
		}
		created := make(multimetro.Keys, 0, len(resp.Data.Instances))
		for _, instance := range resp.Data.Instances {
			key := multimetro.Key{
				Metro: c.Metro.Name.String(),
				UUID:  ptr.ZeroIfNil(instance.Uuid),
				Name:  ptr.ZeroIfNil(instance.Name),
			}
			created = append(created, key)
		}
		return created, nil
	})
	if err != nil {
		return nil, err
	}
	results, err := Instance{}.Get(ctx, keys.Strings())
	if err != nil {
		return nil, err
	}
	return results, nil
}

func (Instance) Examples() map[cmd.CmdType][]kingkong.Example {
	return map[cmd.CmdType][]kingkong.Example{
		cmd.CmdTypeGet: {
			{
				Description: "Inspect an instance by name or UUID",
				Commands:    []string{"unikraft instance get demo-instance"},
			},
		},
		cmd.CmdTypeList: {
			{
				Description: "List instances across metros",
				Commands:    []string{"unikraft instance list"},
			},
		},
		cmd.CmdTypeCreate: {
			{
				Description: "Create a new instance",
				Commands: []string{
					`unikraft instance create \
  --set name=demo-instance \
  --set metro=fra \
  --set image=nginx:latest \
  --set autostart=true \
  --set resources.memory=128 \
  --set resources.vcpus=1`,
				},
			},
		},
		cmd.CmdTypeEdit: {
			{
				Description: "Resize instance memory",
				Commands:    []string{"unikraft instance edit demo-instance --set resources.memory=256"},
			},
		},
		cmd.CmdTypeDelete: {
			{
				Description: "Delete an instance by name or UUID",
				Commands:    []string{"unikraft instance delete demo-instance"},
			},
		},
	}
}

type InstancesLogsCmd struct {
	Name []string `arg:"" completion-predictor:"resource-key-instance" help:"Names of the instances to fetch logs for."`

	Tail   int  `help:"Number of lines to show from the end of the logs."`
	Follow bool `short:"f" help:"Follow log output."`
}

func (cmd InstancesLogsCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Fetch logs from an instance",
			Commands: []string{
				"unikraft instance logs my-instance",
			},
		},
		{
			Description: "Fetch the last 100 lines of logs from an instance",
			Commands: []string{
				"unikraft instance logs my-instance --tail 100",
			},
		},
		{
			Description: "Follow logs from an instance in real-time",
			Commands: []string{
				"unikraft instance logs my-instance --follow",
			},
		},
	}
}

func (cmd *InstancesLogsCmd) Run(ctx context.Context, stdio config.Stdio) error {
	// HACK: we resolve the keys early, so that we can assume that all the
	// instances actually exist (this is a potential race condition, but it's
	// acceptable for now)
	instances, err := Instance{}.Get(ctx, cmd.Name)
	if err != nil {
		return err
	}
	keys := make(multimetro.Keys, 0, len(instances))
	for _, instance := range instances {
		key := instance.(Instance).key()
		if key.Metro == "" {
			return fmt.Errorf("key %q not fully resolved", key)
		}
		keys = append(keys, key)
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}

	mux := muxreader.New()
	defer mux.Close()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	err = group.DoRefs(ctx, g, keys.Refs(), func(_ context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		for _, ref := range refs {
			key := multimetro.Key(ref)
			r, err := logs.InstanceLogs(ctx, c).Reader(ref.NameOrUUID(), cmd.Tail, cmd.Follow)
			if err != nil {
				return nil, err
			}
			mux.With(key.String(), r)
		}
		return refs, nil
	})
	if err != nil {
		return err
	}
	mux.Seal()

	_, err = io.Copy(stdio.Stdout, mux)
	if cmd.Follow && errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

type InstancesStartCmd struct {
	Name []string `arg:"" completion-predictor:"resource-key-instance" help:"Names of the instances to start."`

	cmd.FormatOpts
}

func (cmd InstancesStartCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Start an instance",
			Commands: []string{
				"unikraft instance start demo-instance",
			},
		},
	}
}

func (c *InstancesStartCmd) Run(ctx context.Context, stdio config.Stdio) error {
	keys := multimetro.ParseKeys(c.Name)
	before, opErr := Instance{}.Get(ctx, keys.Strings())
	if opErr != nil && len(before) == 0 {
		return opErr
	}
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	var targetKeys multimetro.Keys
	for _, res := range before {
		targetKeys = append(targetKeys, res.(Instance).key())
	}
	if len(targetKeys) == 0 {
		targetKeys = keys
	}

	started, startErr := startInstances(ctx, g, targetKeys)
	opErr = errors.Join(opErr, startErr)
	if len(started) == 0 {
		return opErr
	}

	updated, getErr := Instance{}.Get(ctx, started.Strings())
	opErr = errors.Join(opErr, getErr)
	if getErr != nil && len(updated) == 0 {
		return opErr
	}

	keySet := make(map[string]struct{}, len(started))
	for _, k := range started {
		keySet[k.String()] = struct{}{}
	}
	before = slices.DeleteFunc(slices.Clone(before), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().String()]
		return !ok
	})
	updated = slices.DeleteFunc(slices.Clone(updated), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().String()]
		return !ok
	})

	diffErr := cmd.Diff(ctx, stdio.Stdout, c.FormatOpts, Instance{}, before, updated)
	return errors.Join(opErr, diffErr)
}

type InstancesStopCmd struct {
	Name []string `arg:"" completion-predictor:"resource-key-instance" help:"Names of the instances to stop."`
	StopOpts

	cmd.FormatOpts
}

func (cmd InstancesStopCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Stop an instance",
			Commands: []string{
				"unikraft instance stop demo-instance",
			},
		},
		{
			Description: "Stop with a drain timeout",
			Commands: []string{
				"unikraft instance stop demo-instance --drain-timeout 30000",
			},
		},
		{
			Description: "Force stop an instance",
			Commands: []string{
				"unikraft instance stop demo-instance --force",
			},
		},
	}
}

func (c *InstancesStopCmd) Run(ctx context.Context, stdio config.Stdio) error {
	keys := multimetro.ParseKeys(c.Name)
	before, opErr := Instance{}.Get(ctx, keys.Strings())
	if opErr != nil && len(before) == 0 {
		return opErr
	}
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	var targetKeys multimetro.Keys
	for _, res := range before {
		targetKeys = append(targetKeys, res.(Instance).key())
	}
	if len(targetKeys) == 0 {
		targetKeys = keys
	}
	stopped, stopErr := stopInstances(ctx, g, targetKeys, c.StopOpts)
	opErr = errors.Join(opErr, stopErr)
	if len(stopped) == 0 {
		return opErr
	}

	updated, getErr := Instance{}.Get(ctx, stopped.Strings())
	opErr = errors.Join(opErr, getErr)
	if getErr != nil && len(updated) == 0 {
		return opErr
	}

	keySet := make(map[string]struct{}, len(stopped))
	for _, k := range stopped {
		keySet[k.String()] = struct{}{}
	}
	before = slices.DeleteFunc(slices.Clone(before), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().String()]
		return !ok
	})
	updated = slices.DeleteFunc(slices.Clone(updated), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().String()]
		return !ok
	})

	diffErr := cmd.Diff(ctx, stdio.Stdout, c.FormatOpts, Instance{}, before, updated)
	return errors.Join(opErr, diffErr)
}

type InstancesRestartCmd struct {
	Name []string `arg:"" completion-predictor:"resource-key-instance" help:"Names of the instances to restart."`
	StopOpts

	cmd.FormatOpts
}

func (cmd InstancesRestartCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Restart an instance",
			Commands: []string{
				"unikraft instance restart demo-instance",
			},
		},
		{
			Description: "Force restart an instance",
			Commands: []string{
				"unikraft instance restart demo-instance --force",
			},
		},
	}
}

func (c *InstancesRestartCmd) Run(ctx context.Context, stdio config.Stdio) error {
	keys := multimetro.ParseKeys(c.Name)
	before, opErr := Instance{}.Get(ctx, keys.Strings())
	if opErr != nil && len(before) == 0 {
		return opErr
	}
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	var targetKeys multimetro.Keys
	for _, res := range before {
		targetKeys = append(targetKeys, res.(Instance).key())
	}

	stopped, stopErr := stopInstances(ctx, g, targetKeys, c.StopOpts)
	opErr = errors.Join(opErr, stopErr)
	if len(stopped) == 0 {
		return opErr
	}

	started, startErr := startInstances(ctx, g, stopped)
	opErr = errors.Join(opErr, startErr)
	if len(started) == 0 {
		return opErr
	}

	updated, getErr := Instance{}.Get(ctx, started.Strings())
	opErr = errors.Join(opErr, getErr)
	if getErr != nil && len(updated) == 0 {
		return opErr
	}

	keySet := make(map[string]struct{}, len(started))
	for _, k := range started {
		keySet[k.String()] = struct{}{}
	}
	before = slices.DeleteFunc(slices.Clone(before), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().String()]
		return !ok
	})
	updated = slices.DeleteFunc(slices.Clone(updated), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().String()]
		return !ok
	})

	diffErr := cmd.Diff(ctx, stdio.Stdout, c.FormatOpts, Instance{}, before, updated)
	return errors.Join(opErr, diffErr)
}

type StopOpts struct {
	Force        bool             `help:"Force stop the instance immediately."`
	DrainTimeout types.DurationMS `help:"Timeout in milliseconds for draining connections before stopping." default:"-1"`
}

func (args *StopOpts) toReq(nameOrUUID platform.NameOrUUID) platform.StopInstancesRequestItem {
	req := platform.StopInstancesRequestItem{
		Uuid: nameOrUUID.Uuid,
		Name: nameOrUUID.Name,
	}
	if args.Force {
		req.Force = &args.Force
	}
	if args.DrainTimeout >= 0 {
		timeout := uint64(args.DrainTimeout)
		req.DrainTimeoutMs = &timeout
	}
	return req
}

func startInstances(ctx context.Context, g *group.Group[multimetro.MetroClient], keys multimetro.Keys) (multimetro.Keys, error) {
	started, err := group.CollectRefsSlices(ctx, g, keys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) ([]multimetro.Key, group.Refs, error) {
		log.G(ctx).Trace().Msg("starting instances")
		resp, err := c.StartInstances(ctx, refs.NameOrUUIDs())
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, nil, err
		}
		var started multimetro.Keys
		for _, instance := range resp.Data.Instances {
			if instance.Status == nil || *instance.Status != platform.ResponseStatusSUCCESS {
				continue
			}
			started = append(started, multimetro.Key{
				Metro: c.Metro.Name.String(),
				Name:  *instance.Name,
				UUID:  *instance.Uuid,
			})
		}
		return started, started.Refs(), nil
	})
	return multimetro.Keys(started), err
}

func stopInstances(ctx context.Context, g *group.Group[multimetro.MetroClient], keys multimetro.Keys, opts StopOpts) (multimetro.Keys, error) {
	stopped, err := group.CollectRefsSlices(ctx, g, keys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) ([]multimetro.Key, group.Refs, error) {
		log.G(ctx).Trace().Msg("stopping instances")
		reqs := make([]platform.StopInstancesRequestItem, 0, len(refs))
		for _, key := range refs {
			reqs = append(reqs, opts.toReq(key.NameOrUUID()))
		}
		resp, err := c.StopInstances(ctx, reqs)
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, nil, err
		}
		var stopped multimetro.Keys
		for _, instance := range resp.Data.Instances {
			if instance.Status == nil || *instance.Status != platform.ResponseStatusSUCCESS {
				continue
			}
			stopped = append(stopped, multimetro.Key{
				Metro: c.Metro.Name.String(),
				Name:  *instance.Name,
				UUID:  *instance.Uuid,
			})
		}
		return stopped, stopped.Refs(), nil
	})
	return multimetro.Keys(stopped), err
}
