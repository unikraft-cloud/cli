// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/alecthomas/kong"
	"github.com/distribution/reference"
	"github.com/google/uuid"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/cloud/sdk/platform/stop"
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
	cmd.GettableResourceCmd[Instance]
	cmd.WaitableResourceCmd[Instance]
	cmd.ListableResourceCmd[Instance]
	cmd.BulkDeletableResourceCmd[Instance]

	Create InstanceCreateCmd `cmd:"" help:"Create an instance."`
	Edit   InstanceEditCmd   `cmd:"" help:"Edit an instance."`

	Logs    InstancesLogsCmd    `cmd:"" help:"Fetch and display instance logs."`
	Start   InstancesStartCmd   `cmd:"" help:"Start one or more instances."`
	Stop    InstancesStopCmd    `cmd:"" help:"Stop one or more instances."`
	Restart InstancesRestartCmd `cmd:"" help:"Restart one or more instances."`
}

// InstanceCreateCmd extends the generic resource create command with shortcut
// flags for commonly used instance fields. Each field tagged with
// `shortcut:"<path>"` is translated into a --set <path>=<value> entry before
// the standard create pipeline runs.
type InstanceCreateCmd struct {
	cmd.ResourceCreateCmd[Instance]

	// Shortcut flags - ordered to match Instance struct layout.
	Metro string `group:"flag-create" shortcut:"metro" help:"Metro to deploy in." placeholder:"metro" example:"fra,sfo,nyc"`
	Name  string `group:"flag-create" shortcut:"name" short:"n" help:"Instance name." placeholder:"name"`

	Image string `group:"flag-create" shortcut:"image" help:"Image to deploy." placeholder:"<name>:<tag>" example:"nginx:latest,my-app:v1.2.3"`

	Args []string `group:"flag-create" shortcut:"runtime.args" help:"Arguments to pass to the instance." placeholder:"arg"`
	Env  []string `group:"flag-create" shortcut:"runtime.env" short:"e" help:"Environment variables." placeholder:"<key>=<value>" example:"DEBUG=true,PORT=8080"`

	Memory types.SizeMebibytes `group:"flag-create" shortcut:"resources.memory" short:"m" help:"Memory allocation." placeholder:"size" example:"128MiB,1GiB"`
	Vcpus  int                 `group:"flag-create" shortcut:"resources.vcpus" help:"Number of vCPUs." placeholder:"n" example:"1,2,4"`

	Volume []InstanceVolume `group:"flag-create" shortcut:"volumes" short:"v" help:"Attach volume." placeholder:"<name>:<path>[:<ro>]" example:"my-vol:/data,cache:/tmp:ro"`

	Service InstanceService `group:"flag-create" shortcut:"service" help:"Service group name or key." placeholder:"name"`
	Publish []Service       `group:"flag-create" shortcut:"service.services" short:"p" help:"Publish port." placeholder:"<src>:<dest>[/<handlers>]" example:"443:8080/http+tls,80:8080/http"`
	Domain  []Domain        `group:"flag-create" shortcut:"service.domains" help:"Service domain." placeholder:"fqdn" example:"example.com,api.example.com"`

	ScaleToZero InstanceScaleToZero `group:"flag-create" shortcut:"scale-to-zero" help:"Scale-to-zero options." placeholder:"<key>=<value>" example:"policy=on\\,cooldown-time=300"`

	Restart string `group:"flag-create" shortcut:"restart.policy" help:"Restart policy." placeholder:"policy" example:"always,on-failure,never"`

	Autostart *bool    `group:"flag-create" shortcut:"autostart" help:"Start instance automatically."`
	Replicas  int64    `group:"flag-create" shortcut:"replicas" help:"Number of replicas." placeholder:"n" example:"1,3"`
	Features  []string `group:"flag-create" shortcut:"features" help:"Instance features." placeholder:"feature"`
}

func (c *InstanceCreateCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	return c.ResourceCreateCmd.Run(ctx, stdio, sandbox)
}

// InstanceEditCmd extends the generic resource edit command with shortcut
// flags for commonly used editable instance fields. Each field tagged with
// `shortcut:"<path>"` is translated into a --set <path>=<value> entry before
// the standard edit pipeline runs.
type InstanceEditCmd struct {
	cmd.ResourceEditCmd[Instance]

	// Shortcut flags - only fields that support editing.
	Image string `group:"flag-edit" shortcut:"image" help:"Image to deploy." placeholder:"<name>:<tag>" example:"nginx:latest,my-app:v1.2.3"`

	Args []string `group:"flag-edit" shortcut:"runtime.args" help:"Arguments to pass to the instance." placeholder:"arg"`
	Env  []string `group:"flag-edit" shortcut:"runtime.env" short:"e" help:"Environment variables." placeholder:"<key>=<value>" example:"DEBUG=true,PORT=8080"`

	Memory types.SizeMebibytes `group:"flag-edit" shortcut:"resources.memory" short:"m" help:"Memory allocation." placeholder:"size" example:"128MiB,1GiB"`
	Vcpus  int                 `group:"flag-edit" shortcut:"resources.vcpus" help:"Number of vCPUs." placeholder:"n" example:"1,2,4"`

	ScaleToZero InstanceScaleToZero `group:"flag-edit" shortcut:"scale-to-zero" help:"Scale-to-zero options." placeholder:"<key>=<value>" example:"policy=on\\,cooldown-time=300"`
}

func (c *InstanceEditCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	return c.ResourceEditCmd.Run(ctx, stdio, sandbox)
}

type Instance struct {
	MetroName string `mirror:"metro.name" field:"metro,short" create:"set,required"`
	Name      string `mirror:"instance.name" field:",short" create:"set"`
	UUID      string `mirror:"instance.uuid" field:",long"`

	Tags []string `mirror:"instance.tags"`

	State types.InstanceState `mirror:"instance.state" field:",short" edit:"set"`

	Image types.ImageRef[reference.Named] `mirror:"instance.image" field:",short" create:"set,required" edit:"set"`

	Runtime struct {
		Args []string          `mirror:"instance.args" field:",short" create:"set" edit:"set"`
		Env  map[string]string `mirror:"instance.env" field:",long" create:"set" edit:"set,add,del=keys"`
	}

	Resources struct {
		Memory types.SizeMebibytes `mirror:"instance.memory_mb" field:",short" create:"set" edit:"set"`
		VCPUs  int                 `mirror:"instance.vcpus" field:"vcpus,short" create:"set" edit:"set"`
	}

	Service *InstanceService  `mirror:"instance.service_group" field:",embed" create:"set"`
	Volumes []*InstanceVolume `mirror:"instance.volumes" field:",embed" create:"set"`

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

	ScaleToZero InstanceScaleToZero `field:",embed" mirror:"instance.scale_to_zero" create:"set" edit:"set"`

	Timing struct {
		Uptime   types.DurationMS `mirror:"instance.uptime_ms"`
		BootTime types.DurationUS `mirror:"instance.boot_time_us" field:",long"`
		NetTime  types.DurationUS `mirror:"instance.net_time_us"`
	}

	Restart struct {
		Policy       string `mirror:"instance.restart_policy" create:"set"`
		StartCount   int    `mirror:"instance.start_count"`
		RestartCount int    `mirror:"instance.restart_count"`
	}

	Autostart   bool            `field:"autostart,invisible,valueless" create:"set"`
	Replicas    int64           `field:"replicas,invisible,valueless" create:"set"`
	WaitTimeout types.DurationS `field:"wait-timeout,invisible,valueless" create:"set"`
	Features    []string        `field:"features,invisible,valueless" create:"set"`
	Vsock       bool            `field:"vsock,invisible,valueless" create:"set" edit:"set"`

	Stop struct {
		Reason string     `field:",long"`
		Origin string     `field:"origin,hidden"`
		Errno  stop.Errno `field:"errno,hidden"`

		ExitCode *uint32 `mirror:"instance.exit_code" field:"exit-code,long"`
	} `field:",long"`

	Instance platform.Instance `field:"-" json:"instance"`
	Metro    *config.Metro     `field:"-" json:"metro"`
	Profile  *config.Profile   `field:"-" json:"profile"`

	key multimetro.Key
}

type InstanceService struct {
	Metro     string     `field:"-"`
	UUID      string     `mirror:"uuid" field:",long"`
	Name      string     `mirror:"name" field:",long"`
	Services  []*Service `mirror:"services" field:",invisible,valueless" create:"set"`
	Domains   []Domain   `mirror:"domains" field:",short,embed" create:"set"`
	SoftLimit uint32     `field:"soft-limit,invisible,valueless" create:"set"`
	HardLimit uint32     `field:"hard-limit,invisible,valueless" create:"set"`
}

func (i *InstanceService) UnmarshalText(data []byte) error {
	str := strings.TrimSpace(string(data))
	if str == "" {
		return nil
	}
	if strings.Contains(str, "=") {
		type instanceServiceAlias InstanceService
		parsed, err := value.Parse[instanceServiceAlias]([]string{str})
		if err != nil {
			return err
		}
		*i = InstanceService(parsed)
		return nil
	}
	key := multimetro.ParseKey(str)
	i.Metro = key.Metro
	i.UUID = key.UUID
	i.Name = key.Name
	return nil
}

type InstanceVolume struct {
	UUID     string `name:"uuid" mirror:"uuid" json:"uuid,omitempty" field:",long"`
	Name     string `name:"name" mirror:"name" json:"name,omitempty" field:",long"`
	At       string `name:"at" mirror:"at" json:"at" field:",long"`
	Readonly bool   `name:"readonly" mirror:"readonly" json:"readonly,omitempty" field:",long"`

	Size types.SizeMebibytes `name:"size" field:"size,invisible,valueless" create:"set"`
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

// MarshalJSON outputs the struct form (not the short text form).
// This takes precedence over MarshalText for JSON/YAML serialization.
func (v *InstanceVolume) MarshalJSON() ([]byte, error) {
	type volumeJSON InstanceVolume // alias to avoid recursion
	return json.Marshal((*volumeJSON)(v))
}

// UnmarshalJSON parses both the struct form and the short text form.
// This takes precedence over UnmarshalText for JSON/YAML deserialization.
func (v *InstanceVolume) UnmarshalJSON(data []byte) error {
	if len(data) != 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		return v.UnmarshalText([]byte(text))
	}
	type volumeJSON InstanceVolume // alias to avoid recursion
	return json.Unmarshal(data, (*volumeJSON)(v))
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

	if uuid.Validate(name) == nil {
		v.UUID = name
	} else if name != "" {
		v.Name = name
	}
	v.At = at
	v.Readonly = readonly
	v.Size = size

	return nil
}

type InstanceScaleToZero struct {
	Enabled      bool             `name:"-" json:"-" mirror:"enabled" field:",long"`
	Policy       string           `name:"policy" json:"policy,omitempty" mirror:"policy" field:",long"`
	Stateful     bool             `name:"stateful" json:"stateful,omitempty" mirror:"stateful" field:",long"`
	CooldownTime types.DurationMS `name:"cooldown-time" json:"cooldown-time,omitempty" mirror:"cooldown_time_ms" field:",long"`
	NotifyTime   types.DurationMS `name:"notify-time" json:"notify-time,omitempty" mirror:"notify_time_ms" field:",long"`
}

func (s *InstanceScaleToZero) UnmarshalText(data []byte) error {
	str := strings.TrimSpace(string(data))
	if str == "" {
		return nil
	}

	lower := strings.ToLower(str)
	if lower == "on" || lower == "off" {
		s.Policy = lower
		return nil
	}

	type scaleToZeroAlias InstanceScaleToZero
	parsed, err := value.Parse[scaleToZeroAlias]([]string{str})
	if err != nil {
		return err
	}
	*s = InstanceScaleToZero(parsed)
	return nil
}

func (s InstanceScaleToZero) MarshalText() ([]byte, error) {
	var parts []string
	if s.Policy != "" {
		parts = append(parts, fmt.Sprintf("policy=%s", s.Policy))
	}
	if s.Stateful {
		parts = append(parts, "stateful=true")
	}
	if s.CooldownTime > 0 {
		parts = append(parts, fmt.Sprintf("cooldown-time=%s", s.CooldownTime))
	}
	if s.NotifyTime > 0 {
		parts = append(parts, fmt.Sprintf("notify-time=%s", s.NotifyTime))
	}
	return []byte(strings.Join(parts, ",")), nil
}

func (s InstanceScaleToZero) MarshalJSON() ([]byte, error) {
	type scaleToZeroJSON InstanceScaleToZero
	return json.Marshal(scaleToZeroJSON(s))
}

func (Instance) Type() resource.Type {
	return resource.Type{
		Name:  "instance",
		Names: "instances",
	}
}

func (i Instance) Key() resource.Key {
	return i.key
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
		switch {
		case key.String() == "name":
			field.Hyperlink = i.hyperlink()
		case key.String() == "metro":
			if i.MetroName != "" {
				field.Links = append(field.Links, resource.Link{
					Type: "metro",
					Key:  i.MetroName,
				})
			}
		case key.String() == "service":
			nameField, _ := field.Get("name")
			uuidField, _ := field.Get("uuid")
			name, _ := nameField.Value.(string)
			uuid, _ := uuidField.Value.(string)
			if name != "" || uuid != "" {
				field.Links = append(field.Links, resource.Link{
					Type: "service",
					Key: multimetro.Key{
						Metro: i.Metro.Name,
						Name:  name,
						UUID:  uuid,
					}.String(),
				})
			}
		case key.MatchesString("volumes.*"):
			// Add link to volume resource
			nameField, _ := field.Get("name")
			uuidField, _ := field.Get("uuid")
			name, _ := nameField.Value.(string)
			uuid, _ := uuidField.Value.(string)
			if name != "" || uuid != "" {
				field.Links = append(field.Links, resource.Link{
					Type: "volume",
					Key: multimetro.Key{
						Metro: i.Metro.Name,
						Name:  name,
						UUID:  uuid,
					}.String(),
				})
			}
		case key.String() == "image":
			if i.Image.Reference != nil {
				field.Links = append(field.Links, resource.Link{
					Type: "image",
					Key:  i.Image.Reference.String(),
				})
			}
		}
	}

	return result, nil
}

func (i Instance) hyperlink() string {
	if i.Profile == nil || i.Profile.ControlPlane == "" {
		return ""
	}
	if i.Name == "" || i.Profile.Organization == "" {
		return ""
	}
	return fmt.Sprintf(
		"https://console.unikraft.cloud/org/%s/instances/%s/%s",
		i.Profile.Organization,
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
		resp, err := c.GetInstances(ctx, nil, new(true))
		if err != nil {
			return nil, err
		}
		var results []resource.Resource
		var errs []error
		for _, instance := range resp.Data.Instances {
			result, err := Instance{}.load(nil, instance, &c.Metro, profile)
			if err != nil {
				errs = append(errs, err)
			}
			results = append(results, result)
		}
		return results, errors.Join(errs...)
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
		resp, err := c.GetInstances(ctx, refs.NameOrUUIDs(), new(true))
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
			result, err := Instance{}.load(&refs[i], instance, &c.Metro, profile)
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

func (Instance) load(ref *group.Ref, instance platform.Instance, metro *config.Metro, profile *config.Profile) (Instance, error) {
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

	result := Instance{
		Instance: instance,
		Metro:    metro,
		Profile:  profile,
		key:      multimetro.Key(*ref),
	}
	err := mirror.Mirror(result, &result)
	if err != nil {
		return Instance{}, fmt.Errorf("could not mirror instance data: %w", err)
	}
	if s := instance.Stop(); s != nil {
		result.Stop.Reason = s.String()
		result.Stop.Origin = s.Origin()
		if stopCode := s.KernelStopCode(); stopCode != nil {
			result.Stop.Errno = stop.Errno(stopCode.Errno())
		}
	}
	return result, nil
}

func (Instance) Delete(ctx context.Context, targets []resource.Resource) error {
	keys := make(multimetro.Keys, 0, len(targets))
	for _, target := range targets {
		instance := target.(Instance)
		keys = append(keys, instance.key)
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, keys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		log.G(ctx).Trace().Msg("deleting instances")
		instances, err := c.DeleteInstances(ctx, refs.NameOrUUIDs())
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, err
		}
		var deleted []group.Ref
		for _, instance := range instances.Data.Instances {
			if instance.Status == nil || *instance.Status != platform.ResponseStatusSUCCESS {
				continue
			}
			deleted = append(deleted, group.Ref{
				Metro: c.Metro.Name,
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
		if field.Edit != nil && field.Edit.Set != nil {
			targetState = field.Edit.Set.(types.InstanceState)
			field.Edit = nil
		}
	}

	patches := patchRequests(fields, instancePatchSpec)
	reqs := make([]platform.UpdateInstancesRequestItem, 0, len(patches))
	for _, patch := range patches {
		reqs = append(reqs, platform.UpdateInstancesRequestItem{
			Uuid:  &instance.UUID,
			Op:    platform.UpdateInstancesRequestItemOp(patch.Op),
			Prop:  patch.Prop,
			Value: new(patch.Value),
		})
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	if instance.State.IsRunning() && !targetState.IsRunning() {
		_, err := stopInstances(ctx, g, multimetro.Keys{instance.key}, StopOpts{DrainTimeout: -1})
		if err != nil {
			return nil, err
		}
	}
	if len(reqs) > 0 {
		err = group.DoMetro(ctx, g, instance.key.Metro, func(ctx context.Context, c multimetro.MetroClient) error {
			log.G(ctx).Trace().Msg("updating instance")
			_, err := c.UpdateInstances(ctx, reqs)
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	if !instance.State.IsRunning() && targetState.IsRunning() {
		_, err := startInstances(ctx, g, multimetro.Keys{instance.key})
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
		return platform.UpdateInstancesRequestItemPropImage, value.(types.ImageRef[reference.Named]).Reference.String()
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
	case "scale-to-zero":
		value := value.(InstanceScaleToZero)
		req := map[string]any{}
		if value.Policy != "" {
			req["policy"] = value.Policy
		}
		if value.Stateful {
			req["stateful"] = value.Stateful
		}
		if value.CooldownTime > 0 {
			req["cooldown_time_ms"] = int32(value.CooldownTime)
		}
		if value.NotifyTime > 0 {
			req["notify_time_ms"] = int32(value.NotifyTime)
		}
		return platform.UpdateInstancesRequestItemPropScale_to_zero, req
	case "vsock":
		return "vsock", value.(bool)
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
			req.Image = new(field.Create.Set.(types.ImageRef[reference.Named]).Reference.String())
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
		case "scale-to-zero":
			scale := field.Create.Set.(InstanceScaleToZero)
			req.ScaleToZero = &platform.CreateInstanceRequestScaleToZero{}
			if scale.Policy != "" {
				req.ScaleToZero.Policy = new(platform.CreateInstanceRequestScaleToZeroPolicy(scale.Policy))
			}
			if scale.Stateful {
				req.ScaleToZero.Stateful = &scale.Stateful
			}
			if scale.CooldownTime > 0 {
				cooldown := int32(scale.CooldownTime)
				req.ScaleToZero.CooldownTimeMs = &cooldown
			}
			if scale.NotifyTime > 0 {
				notify := int32(scale.NotifyTime)
				req.ScaleToZero.NotifyTimeMs = &notify
			}
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
					reqVol.SizeMb = new(int64(vol.Size))
				}
				if vol.Readonly {
					reqVol.Readonly = &vol.Readonly
				}
				req.Volumes = append(req.Volumes, reqVol)
			}
		case "service":
			svc := field.Create.Set.(*InstanceService)
			if req.ServiceGroup == nil {
				req.ServiceGroup = &platform.CreateInstanceRequestServiceGroup{}
			}
			if svc.Metro != "" && svc.Metro != metro {
				return nil, fmt.Errorf("cannot create instance: metro mismatch between service (%q) and instance (%q)", svc.Metro, metro)
			}
			req.ServiceGroup.Name = ptr.NilIfZero(svc.Name)
			req.ServiceGroup.Uuid = ptr.NilIfZero(svc.UUID)
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
			req.TimeoutS = new(int64(timeout))
		case "features":
			features := field.Create.Set.([]string)
			for _, f := range features {
				req.Features = append(req.Features, platform.CreateInstanceRequestFeatures(f))
			}
		case "vsock":
			vsock := field.Create.Set.(bool)
			dt, err := json.Marshal(vsock)
			if err != nil {
				return nil, err
			}
			if req.AdditionalProperties == nil {
				req.AdditionalProperties = make(map[string]json.RawMessage)
			}
			req.AdditionalProperties["vsock"] = dt
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
				Metro: c.Metro.Name,
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
					// `unikraft instance create \
					//   --set name=demo-instance \
					//   --set metro=fra \
					//   --set image=nginx:latest \
					//   --set autostart=true \
					//   --set resources.memory=128 \
					//   --set resources.vcpus=1`,
					`unikraft instance create \
	  --name demo-instance \
	  --metro fra \
	  --image nginx:latest \
	  --autostart \
	  --memory 128 \
	  --vcpus 1`,
				},
			},
		},
		cmd.CmdTypeEdit: {
			{
				Description: "Resize instance memory",
				Commands: []string{
					// "unikraft instance edit demo-instance --set resources.memory=256",
					"unikraft instance edit demo-instance --memory 256",
				},
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
	Targets []string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instances to fetch logs for."`

	Prefix bool `help:"Prefix log lines with instance name." negatable:"" default:"true"`
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
	instances, err := Instance{}.Get(ctx, cmd.Targets)
	if err != nil {
		return err
	}
	keys := make(multimetro.Keys, 0, len(instances))
	for _, instance := range instances {
		key := instance.(Instance).key
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
	if !cmd.Prefix {
		mux.DisablePrefix()
	}
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
	Targets []string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instances to start."`

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
	keys := multimetro.ParseKeys(c.Targets)
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
		targetKeys = append(targetKeys, res.(Instance).key)
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
		keySet[k.Canonical()] = struct{}{}
	}
	before = slices.DeleteFunc(slices.Clone(before), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().Canonical()]
		return !ok
	})
	updated = slices.DeleteFunc(slices.Clone(updated), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().Canonical()]
		return !ok
	})

	diffErr := cmd.Diff(ctx, stdio.Stdout, c.FormatOpts, Instance{}, before, updated)
	return errors.Join(opErr, diffErr)
}

type InstancesStopCmd struct {
	Targets []string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instances to stop."`
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
	keys := multimetro.ParseKeys(c.Targets)
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
		targetKeys = append(targetKeys, res.(Instance).key)
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
		keySet[k.Canonical()] = struct{}{}
	}
	before = slices.DeleteFunc(slices.Clone(before), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().Canonical()]
		return !ok
	})
	updated = slices.DeleteFunc(slices.Clone(updated), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().Canonical()]
		return !ok
	})

	diffErr := cmd.Diff(ctx, stdio.Stdout, c.FormatOpts, Instance{}, before, updated)
	return errors.Join(opErr, diffErr)
}

type InstancesRestartCmd struct {
	Targets []string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instances to restart."`
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
	keys := multimetro.ParseKeys(c.Targets)
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
		targetKeys = append(targetKeys, res.(Instance).key)
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
		keySet[k.Canonical()] = struct{}{}
	}
	before = slices.DeleteFunc(slices.Clone(before), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().Canonical()]
		return !ok
	})
	updated = slices.DeleteFunc(slices.Clone(updated), func(r resource.Resource) bool {
		_, ok := keySet[r.Key().Canonical()]
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
				Metro: c.Metro.Name,
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
				Metro: c.Metro.Name,
				Name:  *instance.Name,
				UUID:  *instance.Uuid,
			})
		}
		return stopped, stopped.Refs(), nil
	})
	return multimetro.Keys(stopped), err
}
