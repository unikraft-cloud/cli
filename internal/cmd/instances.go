// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/MakeNowJust/heredoc"
	"github.com/alecthomas/kong"
	"github.com/distribution/reference"
	"github.com/go-json-experiment/json/jsontext"
	"mvdan.cc/sh/v3/shell"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/cloud/sdk/platform/logs"
	"unikraft.com/cloud/sdk/platform/stop"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/mirror"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/muxreader"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/cli/internal/tunnel"
	"unikraft.com/cli/internal/types"
)

type InstancesCmd struct {
	cmd.ResourceCmd[Instance]
	cmd.GettableResourceCmd[Instance]
	cmd.WaitableResourceCmd[Instance]
	cmd.ListableResourceCmd[Instance]
	cmd.BulkDeletableResourceCmd[Instance]

	Create     InstanceCreateCmd      `cmd:"" aliases:"new" help:"Create an instance."`
	Run        InstanceRunCmd         `cmd:"" help:"Create an instance and optionally follow its logs."`
	Edit       InstanceEditCmd        `cmd:"" help:"Edit an instance."`
	Template   InstanceTemplatesCmd   `cmd:"" group:"cmd-templates" help:"Manage instance templates." aliases:"templates" set:"name=instance-template" set:"names=instance-templates"`
	Checkpoint InstanceCheckpointsCmd `cmd:"" group:"cmd-checkpoints" help:"Manage instance checkpoints." aliases:"checkpoints" set:"name=instance-checkpoint" set:"names=instance-checkpoints"`

	Logs    InstancesLogsCmd    `cmd:"" help:"Fetch and display instance logs."`
	Start   InstancesStartCmd   `cmd:"" help:"Start one or more instances."`
	Stop    InstancesStopCmd    `cmd:"" help:"Stop one or more instances."`
	Suspend InstancesSuspendCmd `cmd:"" help:"Suspend one or more instances."`
	Restart InstancesRestartCmd `cmd:"" help:"Restart one or more instances."`
	Tunnel  InstancesTunnelCmd  `cmd:"" aliases:"port-forward" help:"Forward a local port to an unexposed instance."`
	History InstanceHistoryCmd  `cmd:"" help:"Show checkpoint history for an instance."`

	Exec  ExecSandboxInstanceCmd  `cmd:"" help:"Exec a command on a sandbox instance."`
	Copy  CopySandboxInstanceCmd  `cmd:"" aliases:"cp" help:"Copy a file to or from a sandbox instance."`
	Write WriteSandboxInstanceCmd `cmd:"" help:"Write a file to a sandbox instance."`
	Read  ReadSandboxInstanceCmd  `cmd:"" help:"Read a file from a sandbox instance."`
	Mkdir MkdirSandboxInstanceCmd `cmd:"" help:"Create a directory on a sandbox instance."`
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

	Image      string                 `group:"flag-create" shortcut:"image" help:"Image to deploy." placeholder:"<name>:<tag>" example:"nginx:latest,my-app:v1.2.3"`
	PullPolicy *platform.PullPolicy   `group:"flag-create" shortcut:"pull-policy" help:"Image pull policy." placeholder:"policy" example:"always,never,if_not_present"`
	Type       *platform.InstanceType `group:"flag-create" shortcut:"type" help:"Type of virtual machine to run. \"full\" requires a plan with full VM support." placeholder:"type" example:"micro,full"`

	Args InstanceArgs `group:"flag-create" shortcut:"runtime.args" help:"Arguments to pass to the instance." placeholder:"arg"`
	Env  []string     `group:"flag-create" shortcut:"runtime.env" short:"e" sep:"none" help:"Environment variable." placeholder:"<key>=<value>" example:"DEBUG=true"`

	Memory        types.SizeMebibytes     `group:"flag-create" shortcut:"resources.memory" short:"m" help:"Memory allocation." placeholder:"size" example:"128MiB,1GiB"`
	Vcpus         int                     `group:"flag-create" shortcut:"resources.vcpus" help:"Number of vCPUs." placeholder:"n" example:"1,2,4"`
	Gpus          int                     `group:"flag-create" shortcut:"resources.gpus" help:"Number of GPUs to attach. Requires type \"full\" and a plan with GPU support. Currently limited to 1." placeholder:"n" example:"0,1"`
	SchedPriority *platform.SchedPriority `group:"flag-create" shortcut:"sched-priority" help:"Scheduling priority for the instance." placeholder:"priority" example:"normal,medium,high,admin"`

	Volume []InstanceVolume `group:"flag-create" shortcut:"volumes" short:"v" sep:"none" help:"Attach volume." placeholder:"<name>:<path>[:<options>]" example:"my-vol:/data,cache:/tmp:ro,data:/mnt:size=10GiB"`
	Rom    []InstanceRom    `group:"flag-create" shortcut:"roms" sep:"none" help:"Attach ROM." placeholder:"image=<ref>,at=<path>" example:"image=myuser/my-rom:latest\\,at=/rom0\\,name=my-rom,dir=./mydata\\,at=/rom"`

	Plugin []InstancePlugin `group:"flag-create" shortcut:"plugins" sep:"none" help:"Load plugin into the instance." placeholder:"name=<name>,rom=<ref>" example:"name=sandbox,rom=plugins/sandbox:latest"`

	Service InstanceService `group:"flag-create" shortcut:"service" help:"Service group name or key." placeholder:"name"`
	Publish []Service       `group:"flag-create" shortcut:"service.services" short:"p" sep:"none" help:"Publish port." placeholder:"<src>:<dest>[/<handlers>]" example:"443:8080/http+tls"`
	Domain  []Domain        `group:"flag-create" shortcut:"service.domains" sep:"none" help:"Service domain." placeholder:"fqdn" example:"example.com"`

	ScaleToZero InstanceScaleToZero `group:"flag-create" shortcut:"scale-to-zero" help:"Scale-to-zero options.\n  policy: on | idle | off\n  cooldown-time: cooldown in ms before scaling to zero\n  notify-time: notification time in ms before scaling to zero\n  stateful: true | false" placeholder:"<key>=<value>" example:"on,policy=on\\,cooldown-time=300,policy=on\\,stateful=true\\,cooldown-time=500\\,notify-time=100"`

	Tag []string `group:"flag-create" shortcut:"tags" sep:"none" help:"Instance tag." placeholder:"tag" example:"env-prod"`

	Restart string `group:"flag-create" shortcut:"restart.policy" help:"Restart policy." placeholder:"policy" example:"always,on-failure,never"`

	Autostart    *bool          `group:"flag-create" shortcut:"autostart" help:"Start instance automatically."`
	Replicas     int64          `group:"flag-create" shortcut:"replicas" help:"Number of replicas." placeholder:"n" example:"1,3"`
	Feature      []string       `group:"flag-create" shortcut:"features" sep:"none" help:"Instance feature." placeholder:"feature"`
	Template     string         `group:"flag-create" shortcut:"template" help:"Create from instance template." placeholder:"name"`
	Branch       multimetro.Key `group:"flag-create" shortcut:"branch" help:"Branch from an existing instance." placeholder:"instance"`
	Checkpoint   multimetro.Key `group:"flag-create" shortcut:"checkpoint" help:"Create from a checkpoint." placeholder:"checkpoint"`
	DeleteOnStop bool           `group:"flag-create" name:"rm" help:"Automatically delete the instance when it stops."`
}

func (c *InstanceCreateCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	if c.DeleteOnStop {
		c.Set = append(c.Set, map[string]string{"features": string(platform.InstanceFeatureDeleteOnStop)})
	}
	if c.Service.Name != "" || c.Service.UUID != "" {
		if len(c.Publish) > 0 {
			return fmt.Errorf("--publish cannot be used with --service")
		}
		if len(c.Domain) > 0 {
			return fmt.Errorf("--domain cannot be used with --service")
		}
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

	Args InstanceArgs `group:"flag-edit" shortcut:"runtime.args" help:"Arguments to pass to the instance." placeholder:"arg"`
	Env  []string     `group:"flag-edit" shortcut:"runtime.env" short:"e" sep:"none" help:"Environment variable." placeholder:"<key>=<value>" example:"DEBUG=true"`

	Memory        types.SizeMebibytes     `group:"flag-edit" shortcut:"resources.memory" short:"m" help:"Memory allocation." placeholder:"size" example:"128MiB,1GiB"`
	Vcpus         int                     `group:"flag-edit" shortcut:"resources.vcpus" help:"Number of vCPUs." placeholder:"n" example:"1,2,4"`
	SchedPriority *platform.SchedPriority `group:"flag-edit" shortcut:"sched-priority" help:"Scheduling priority for the instance." placeholder:"priority" example:"normal,medium,high,admin"`

	Rom []InstanceRom `group:"flag-edit" shortcut:"roms" sep:"none" help:"Attach ROM." placeholder:"image=<ref>,at=<path>" example:"image=myuser/my-rom:latest\\,at=/rom0\\,name=my-rom,dir=./mydata\\,at=/rom"`

	Tag []string `group:"flag-edit" shortcut:"tags" sep:"none" help:"Instance tag." placeholder:"tag" example:"env-prod"`

	ScaleToZero InstanceScaleToZero `group:"flag-edit" shortcut:"scale-to-zero" help:"Scale-to-zero options.\n  policy: on | idle | off\n  cooldown-time: cooldown in ms before scaling to zero\n  notify-time: notification time in ms before scaling to zero\n  stateful: true | false" placeholder:"<key>=<value>" example:"on,policy=on\\,cooldown-time=300,policy=on\\,stateful=true\\,cooldown-time=500\\,notify-time=100"`

	DeleteLock *bool `group:"flag-edit" shortcut:"delete-lock" help:"Prevent instance deletion until the lock is removed."`
}

func (c *InstanceEditCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	return c.ResourceEditCmd.Run(ctx, stdio, sandbox)
}

type Instance struct {
	Metro LinkName[Metro] `field:"metro,short" create:"set,required"`
	Name  string          `mirror:"instance.name" field:",short" create:"set"`
	UUID  string          `mirror:"instance.uuid" field:",long"`

	Tags []string `mirror:"instance.tags" field:",long" create:"set" edit:"set,add,del"`

	State types.InstanceState `mirror:"instance.state" field:",short" edit:"set"`

	Image      types.ImageRef[reference.Named] `mirror:"instance.image" field:",short" create:"set" edit:"set"`
	PullPolicy *platform.PullPolicy            `field:"pull-policy,invisible,valueless" create:"set"`
	Type_      *platform.InstanceType          `mirror:"instance.type" field:"type,long" create:"set"`

	Runtime struct {
		Args InstanceArgs      `mirror:"instance.args" field:",short" create:"set" edit:"set"`
		Env  map[string]string `mirror:"instance.env" field:",long" create:"set" edit:"set,add,del=keys"`
	}

	Resources struct {
		Memory types.SizeMebibytes `mirror:"instance.memory_mb" field:",short" create:"set" edit:"set"`
		VCPUs  int                 `mirror:"instance.vcpus" field:"vcpus,short" create:"set" edit:"set"`
		GPUs   int                 `field:"gpus,long" create:"set"`
	}

	Service *InstanceService  `mirror:"instance.service_group" field:",embed" create:"set"`
	Volumes []*InstanceVolume `mirror:"instance.volumes" field:",embed" create:"set" edit:"add,del=strings"`
	Roms    []*InstanceRom    `mirror:"instance.roms" field:",embed" create:"set" edit:"set,add,del=strings"`
	Plugins []*InstancePlugin `mirror:"instance.plugins" field:",embed" create:"set"`

	Networks []InstanceNetwork `mirror:"instance.network_interfaces" field:",embed"`
	Gpus     []InstanceGpu     `mirror:"instance.gpus" field:"gpus,embed"`

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

	SchedPriority *platform.SchedPriority `mirror:"instance.sched_priority" field:"sched-priority,long" create:"set" edit:"set"`
	Autostart     bool                    `field:"autostart,invisible,valueless" create:"set"`
	Replicas      int64                   `field:"replicas,invisible,valueless" create:"set"`
	WaitTimeout   types.DurationS         `field:"wait-timeout,invisible,valueless" create:"set"`
	Features      []string                `field:"features,invisible,valueless" create:"set"`
	Vsock         bool                    `field:"vsock,invisible,valueless" create:"set" edit:"set"`
	Template      string                  `field:"template,invisible,valueless" create:"set"`
	Branch        multimetro.Key          `field:"branch,invisible,valueless" create:"set"`
	Checkpoint    multimetro.Key          `field:"checkpoint,invisible,valueless" create:"set"`

	Stop struct {
		Reason string     `field:",long"`
		Origin string     `field:"origin,hidden"`
		Errno  stop.Errno `field:"errno,hidden"`

		ExitCode *uint32 `mirror:"instance.exit_code" field:"exit-code,long"`
	} `field:",long"`

	Instance platform.Instance `field:"-" json:"instance"`
	Profile  *config.Profile   `field:"-" json:"profile"`

	key multimetro.Key

	DeleteLock bool `mirror:"instance.delete_lock" field:"delete-lock,long" edit:"set"`
}

type InstanceNetwork struct {
	UUID      string `mirror:"uuid" field:",long"`
	PrivateIP string `mirror:"private_ip" field:",long"`
	MAC       string `mirror:"mac" field:",long"`
}

type InstanceGpu struct {
	UUID  string `mirror:"uuid" field:",long"`
	Model string `mirror:"model" field:",long"`
}

type InstanceService struct {
	Link[ServiceGroup]
	Services  []*Service `mirror:"services" json:"services,omitempty" field:",invisible,valueless" create:"set"`
	Domains   []Domain   `mirror:"domains" json:"domains,omitempty" field:",short,embed" create:"set"`
	SoftLimit uint32     `json:"soft-limit,omitempty" field:"soft-limit,invisible,valueless" create:"set"`
	HardLimit uint32     `json:"hard-limit,omitempty" field:"hard-limit,invisible,valueless" create:"set"`
}

// UnmarshalText only links to a service group by name/key - use
// --set service.domains=... etc. for nested fields.
func (i *InstanceService) UnmarshalText(data []byte) error {
	link, err := ParseLink[ServiceGroup](data)
	if err != nil {
		return err
	}
	i.Link = link
	return nil
}

// MarshalText renders the service group link, matching UnmarshalText.
// Domains/Services/limits have no compact text form - use MarshalJSON or the
// dotted-path fields for those.
func (i InstanceService) MarshalText() ([]byte, error) {
	return FormatLink(i.Link)
}

// MarshalJSON outputs the struct form (not the short text form).
// This takes precedence over MarshalText for JSON/YAML serialization.
func (i InstanceService) MarshalJSON() ([]byte, error) {
	type instanceServiceJSON InstanceService // alias to avoid recursion
	return json.Marshal(instanceServiceJSON(i))
}

// UnmarshalJSON parses both the struct form and the short text form.
// This takes precedence over UnmarshalText for JSON/YAML deserialization.
func (i *InstanceService) UnmarshalJSON(data []byte) error {
	if len(data) != 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		return i.UnmarshalText([]byte(text))
	}
	type instanceServiceJSON InstanceService // alias to avoid recursion
	return json.Unmarshal(data, (*instanceServiceJSON)(i))
}

type InstanceVolume struct {
	Link[Volume]
	At       string `name:"at" mirror:"at" json:"at" field:",long"`
	Readonly bool   `name:"readonly" mirror:"readonly" json:"readonly,omitempty" field:",long"`

	Size types.SizeMebibytes `name:"size" json:"size,omitempty" field:"size,invisible,valueless" create:"set"`
}

func (v *InstanceVolume) MarshalText() ([]byte, error) {
	volKey, err := FormatLink(v.Link)
	if err != nil {
		return nil, err
	}
	parts := []string{string(volKey)}
	if v.At != "" || v.Readonly || v.Size > 0 {
		parts = append(parts, v.At)
	}
	if v.Readonly {
		parts = append(parts, "ro")
	}
	if v.Size > 0 {
		s, err := value.Render(v.Size, value.RenderOpts{})
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

	link, err := ParseLink[Volume]([]byte(name))
	if err != nil {
		return err
	}
	v.Link = link
	v.At = at
	v.Readonly = readonly
	v.Size = size

	return nil
}

// InstanceRom represents a ROM blob attached to an instance.
// Parsed via value.Parse as comma-separated key=value pairs:
//
//	image=<ref>,at=<path>[,name=<name>]
//	dir=<localpath>,at=<path>[,name=<name>]
type InstanceRom struct {
	Name  string `name:"name" mirror:"name" json:"name,omitempty" field:",long"`
	Image string `name:"image" mirror:"image" json:"image,omitempty" field:",long"`
	Dir   string `name:"dir" json:"-" field:"dir,invisible,long"`
	At    string `name:"at" mirror:"at" json:"at,omitempty" field:",long"`
}

func (r *InstanceRom) UnmarshalText(data []byte) error {
	type alias InstanceRom
	parsed, err := value.Parse[alias]([]string{string(data)})
	if err != nil {
		return err
	}
	*r = InstanceRom(parsed)
	if r.Name == "" {
		name := strings.TrimLeft(r.At, "/")
		name = strings.ReplaceAll(name, "/", "-")
		var b strings.Builder
		for _, r := range name {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
				b.WriteRune(r)
			}
		}
		r.Name = b.String()
	}
	return nil
}

func (r *InstanceRom) UnmarshalJSON(data []byte) error {
	if len(data) != 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		return r.UnmarshalText([]byte(text))
	}
	type romJSON InstanceRom // alias to avoid recursion
	return json.Unmarshal(data, (*romJSON)(r))
}

// InstancePlugin represents a plugin loaded into an instance.
// Parsed via value.Parse as comma-separated key=value pairs:
//
//	name=<name>,image=<ref>[,<key>=<value>...]
type InstancePlugin struct {
	Name   string `name:"name" mirror:"name" json:"name" field:",long"`
	Rom    string `name:"rom" mirror:"rom" json:"rom" field:",long"`
	Config string `name:"config" mirror:"config" json:"config,omitempty" field:",long"`
}

func (p *InstancePlugin) UnmarshalText(data []byte) error {
	type alias InstancePlugin
	parsed, err := value.Parse[alias]([]string{string(data)})
	if err != nil {
		return err
	}
	*p = InstancePlugin(parsed)
	return nil
}

// inlineFilesFromDir walks a local directory and returns its contents as
// base64-encoded InlineFile entries suitable for the platform API.
func inlineFilesFromDir(dir string) ([]platform.InlineFile, error) {
	var files []platform.InlineFile
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("non-regular file %q not allow in ROM directory", path)
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		enc := platform.InlineDataEncodingBase64
		files = append(files, platform.InlineFile{
			Path:     "/" + filepath.ToSlash(rel),
			Encoding: &enc,
			Data:     base64.StdEncoding.EncodeToString(data),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("directory %q is empty", dir)
	}
	return files, nil
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
	if lower == "on" || lower == "off" || lower == "idle" {
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

func (s *InstanceScaleToZero) UnmarshalJSON(data []byte) error {
	if len(data) != 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		return s.UnmarshalText([]byte(text))
	}
	type scaleToZeroJSON InstanceScaleToZero
	return json.Unmarshal(data, (*scaleToZeroJSON)(s))
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

// InstanceArgs is a []string type that uses shell-style word splitting when
// unmarshaling from a string.
type InstanceArgs []string

func (a *InstanceArgs) UnmarshalText(data []byte) error {
	s := string(data)
	if s == "" {
		*a = nil
		return nil
	}
	if data[0] == '[' {
		var s []string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*a = s
		return nil
	}
	fields, err := shell.Fields(s, func(string) string { return "" })
	if err != nil {
		return fmt.Errorf("parsing args: %w", err)
	}
	*a = fields
	return nil
}

func (a *InstanceArgs) UnmarshalJSON(data []byte) error {
	// Try as JSON array first.
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*a = arr
		return nil
	}
	// Fall back to JSON string → shell split.
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	return a.UnmarshalText([]byte(s))
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

func (i Instance) Fields(ctx context.Context) ([]resource.Field, error) {
	i.Metro = LinkName[Metro](defaultMetro(ctx, string(i.Metro)))
	result, err := resource.FieldsFromStruct(i)
	if err != nil {
		return nil, err
	}

	for key, field := range resource.IterFields(result) {
		if key.String() == "name" {
			field.Hyperlink = i.hyperlink()
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
		i.Metro,
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
		resp, err := c.GetInstances(ctx, nil, platform.GetInstancesOpts{Details: new(true)})
		if err != nil {
			return nil, err
		}
		var results []resource.Resource
		var errs []error
		if resp == nil || resp.Data == nil {
			return nil, nil
		}
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
		resp, err := c.GetInstances(ctx, refs.NameOrUUIDs(), platform.GetInstancesOpts{Details: new(true)})
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
			result, err := Instance{}.load(matchedRef, instance, &c.Metro, profile)
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

func (Instance) load(ref *group.Ref, instance platform.Instance, metro *config.Metro, profile *config.Profile) (Instance, error) {
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

	result := Instance{
		Instance: instance,
		Metro:    LinkName[Metro](metro.Name),
		Profile:  profile,
		key:      multimetro.Key(*ref),
	}
	err := mirror.Mirror(result, &result)
	if err != nil {
		return Instance{}, fmt.Errorf("could not mirror instance data: %w", err)
	}
	result.Resources.GPUs = len(result.Gpus)
	if s := instance.Stop(); s != nil {
		result.Stop.Reason = s.String()
		result.Stop.Origin = s.Origin()
		if stopCode := s.KernelStopCode(); stopCode != nil {
			result.Stop.Errno = stop.Errno(stopCode.Errno())
		}
	}
	return result, nil
}

func (Instance) Delete(ctx context.Context, keys []string) error {
	parsedKeys := multimetro.ParseKeys(keys)

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, parsedKeys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		log.G(ctx).Trace().Msg("deleting instances")
		reqs := make([]platform.DeleteInstanceRequestItem, len(refs))
		for i, ref := range refs.NameOrUUIDs() {
			reqs[i] = platform.DeleteInstanceRequestItem{
				Uuid:     ref.Uuid,
				Name:     ref.Name,
				TimeoutS: new(int64(-1)),
			}
		}
		instances, err := c.DeleteInstances(ctx, reqs)
		if err != nil && strings.Contains(err.Error(), "timeout_s") && strings.Contains(err.Error(), "is not a valid member") {
			// HACK: retry without timeout_s for metros that don't support it.
			for i := range reqs {
				reqs[i].TimeoutS = nil
			}
			instances, err = c.DeleteInstances(ctx, reqs)
		}
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, err
		}
		var deleted []group.Ref
		for _, instance := range instances.Data.Instances {
			if instance.Status != platform.ResponseStatusSuccess {
				continue
			}
			deleted = append(deleted, group.Ref{
				Metro: c.Metro.Name,
				Name:  instance.Name,
				UUID:  instance.Uuid,
			})
		}
		return deleted, nil
	})
}

func (Instance) Edit(ctx context.Context, key string, fields []resource.Field) error {
	parsedKeys := multimetro.ParseKeys([]string{key})

	// Check if there's a state transition requested - if so, we need current state.
	var targetState types.InstanceState
	var hasStateChange bool
	if stateFields := resource.GetFieldByPath(fields, resource.FieldPath{"state"}); len(stateFields) > 0 {
		field := stateFields[0]
		if field.Edit != nil && field.Edit.Set != nil {
			targetState = field.Edit.Set.(types.InstanceState)
			field.Edit = nil
			hasStateChange = true
		}
	}
	var currentState types.InstanceState
	if hasStateChange {
		resources, err := Instance{}.Get(ctx, []string{key})
		if err != nil {
			return err
		}
		currentState = resources[0].(Instance).State
	}

	// Extract volume add/del ops before the standard patch pipeline.
	var volumeAddOps []*InstanceVolume
	var volumeDelOps []string
	for path, field := range resource.IterFields(fields) {
		if path.String() != "volumes" || field.Edit == nil {
			continue
		}
		if add, ok := field.Edit.Add.([]*InstanceVolume); ok {
			volumeAddOps = add
			field.Edit.Add = nil
		}
		if del, ok := field.Edit.Del.([]string); ok {
			volumeDelOps = del
			field.Edit.Del = nil
		}
		break
	}

	patches, err := patchRequests(fields, instancePatchSpec)
	if err != nil {
		return err
	}
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}

	if hasStateChange && currentState.IsRunning() && !targetState.IsRunning() {
		_, err := stopInstances(ctx, g, parsedKeys, StopOpts{DrainTimeout: -1})
		if err != nil {
			return err
		}
	}

	if len(patches) > 0 {
		err = group.DoRefs(ctx, g, parsedKeys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
			reqs := make([]platform.UpdateInstancesRequestItem, 0, len(refs)*len(patches))
			for _, ref := range refs {
				for _, patch := range patches {
					req := platform.UpdateInstancesRequestItem{
						Op:    platform.MutableInstanceOperation(patch.Op),
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
			log.G(ctx).Trace().Msg("updating instance")
			_, err := c.UpdateInstances(ctx, reqs)
			if err != nil {
				if platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
					return nil, nil
				}
				return nil, err
			}
			return refs, nil
		})
		if err != nil {
			return err
		}
	}

	// Handle volume attach/detach ops using the dedicated volume APIs.
	if len(volumeAddOps) > 0 || len(volumeDelOps) > 0 {
		err = group.DoRefs(ctx, g, parsedKeys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
			var attachReqs []platform.AttachVolumesRequestItem
			var detachReqs []platform.DetachVolumesRequestItem
			for _, ref := range refs {
				instRef := platform.NameOrUUID{
					Uuid: ptr.NilIfZero(ref.UUID),
					Name: ptr.NilIfZero(ref.Name),
				}
				for _, vol := range volumeAddOps {
					if vol.Metro != "" && vol.Metro != c.Metro.Name {
						return nil, fmt.Errorf("volume %q is in metro %q but instance is in metro %q: cross-metro volume attachment is not supported",
							cmp.Or(vol.Name, vol.UUID), vol.Metro, c.Metro.Name)
					}
					if vol.At == "" {
						return nil, fmt.Errorf("volume %q: mount path is required for attach (format: NAME:PATH[:<options>])",
							cmp.Or(vol.Name, vol.UUID))
					}
					req := platform.AttachVolumesRequestItem{
						AttachTo: instRef,
						At:       vol.At,
					}
					if vol.UUID != "" {
						req.Uuid = ptr.NilIfZero(vol.UUID)
					} else {
						req.Name = ptr.NilIfZero(vol.Name)
					}
					if vol.Readonly {
						req.Readonly = new(true)
					}
					attachReqs = append(attachReqs, req)
				}
				for _, volKey := range volumeDelOps {
					ref := multimetro.ParseKey(volKey)
					if ref.Metro != "" && ref.Metro != c.Metro.Name {
						return nil, fmt.Errorf("volume %q is in metro %q but instance is in metro %q: cross-metro volume attachment is not supported",
							cmp.Or(ref.Name, ref.UUID), ref.Metro, c.Metro.Name)
					}
					req := platform.DetachVolumesRequestItem{
						From: &instRef,
					}
					if ref.UUID != "" {
						req.Uuid = ptr.NilIfZero(ref.UUID)
					} else {
						req.Name = ptr.NilIfZero(ref.Name)
					}
					detachReqs = append(detachReqs, req)
				}
			}
			if len(attachReqs) > 0 {
				log.G(ctx).Trace().Msg("attaching volumes to instance")
				if _, err := c.AttachVolumes(ctx, attachReqs); err != nil {
					return nil, err
				}
			}
			if len(detachReqs) > 0 {
				log.G(ctx).Trace().Msg("detaching volumes from instance")
				if _, err := c.DetachVolumes(ctx, detachReqs); err != nil {
					return nil, err
				}
			}
			return refs, nil
		})
		if err != nil {
			return err
		}
	}

	if hasStateChange && !currentState.IsRunning() && targetState.IsRunning() {
		_, err := startInstances(ctx, g, parsedKeys)
		if err != nil {
			return err
		}
	}

	return nil
}

func instancePatchSpec(path string, op patchOp, value any) (platform.MutableInstanceProperty, any, error) {
	var zero platform.MutableInstanceProperty
	switch path {
	case "tags":
		return platform.MutableInstancePropertyTags, value.([]string), nil
	case "image":
		return platform.MutableInstancePropertyImage, value.(types.ImageRef[reference.Named]).Reference.String(), nil
	case "runtime.args":
		return platform.MutableInstancePropertyArgs, []string(value.(InstanceArgs)), nil
	case "runtime.env":
		if op == patchOpDel {
			return platform.MutableInstancePropertyEnv, value.([]string), nil
		}
		return platform.MutableInstancePropertyEnv, value.(map[string]string), nil
	case "resources.memory":
		return platform.MutableInstancePropertyMemoryMb, int64(value.(types.SizeMebibytes)), nil
	case "resources.vcpus":
		return platform.MutableInstancePropertyVcpus, value.(int), nil
	case "sched-priority":
		return platform.MutableInstancePropertySchedPriority, string(*value.(*platform.SchedPriority)), nil
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
		return platform.MutableInstancePropertyScaleToZero, req, nil
	case "vsock":
		return "vsock", value.(bool), nil
	case "roms":
		if op == patchOpDel {
			return platform.MutableInstancePropertyRoms, value.([]string), nil
		}
		roms := value.([]*InstanceRom)
		var reqRoms []map[string]any
		for _, rom := range roms {
			if rom.Image == "" && rom.Dir == "" {
				continue
			}

			reqRom := map[string]any{}
			if rom.Dir != "" {
				files, err := inlineFilesFromDir(rom.Dir)
				if err != nil {
					return zero, nil, fmt.Errorf("reading rom dir %q: %w", rom.Dir, err)
				}
				reqRom["files"] = files
			} else {
				reqRom["image"] = rom.Image
			}
			if rom.Name != "" {
				reqRom["name"] = rom.Name
			}
			if rom.At != "" {
				reqRom["at"] = rom.At
			}
			reqRoms = append(reqRoms, reqRom)
		}
		return platform.MutableInstancePropertyRoms, reqRoms, nil
	case "delete-lock":
		return platform.MutableInstancePropertyDeleteLock, value.(bool), nil
	default:
		return zero, nil, nil
	}
}

func (Instance) Create(ctx context.Context, fields []resource.Field) ([]resource.Resource, error) {
	var req platform.CreateInstanceRequest
	var metro string
	var imageURL string
	var pullPolicy *platform.PullPolicy
	for key, field := range resource.IterFields(fields) {
		if field.Create == nil || field.Create.Set == nil {
			continue
		}
		switch key.String() {
		case "name":
			name := field.Create.Set.(string)
			req.Name = &name
		case "tags":
			req.Tags = field.Create.Set.([]string)
		case "metro":
			metro = string(field.Create.Set.(LinkName[Metro]))
		case "image":
			imageURL = field.Create.Set.(types.ImageRef[reference.Named]).Reference.String()
		case "pull-policy":
			pullPolicy = field.Create.Set.(*platform.PullPolicy)
		case "type":
			req.Type = field.Create.Set.(*platform.InstanceType)
		case "runtime.args":
			req.Args = []string(field.Create.Set.(InstanceArgs))
		case "runtime.env":
			req.Env = field.Create.Set.(map[string]string)
		case "resources.memory":
			mem := int64(field.Create.Set.(types.SizeMebibytes))
			req.MemoryMb = &mem
		case "resources.vcpus":
			vcpus := int32(field.Create.Set.(int))
			req.Vcpus = &vcpus
		case "resources.gpus":
			gpus := int32(field.Create.Set.(int))
			req.Gpus = &gpus
		case "sched-priority":
			v := field.Create.Set.(*platform.SchedPriority)
			req.SchedPriority = v
		case "restart.policy":
			policy := platform.InstanceRestartPolicy(field.Create.Set.(string))
			req.RestartPolicy = &policy
		case "scale-to-zero":
			scale := field.Create.Set.(InstanceScaleToZero)
			req.ScaleToZero = &platform.CreateInstanceScaleToZero{}
			if scale.Policy != "" {
				req.ScaleToZero.Policy = new(platform.InstanceScaleToZeroPolicy(scale.Policy))
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
				if vol.Metro != "" && vol.Metro != metro {
					return nil, fmt.Errorf("cannot create instance: metro mismatch between volume (%q) and instance (%q)", vol.Metro, metro)
				}
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
					reqVol.SizeMb = new(uint64(vol.Size))
				}
				if vol.Readonly {
					reqVol.Readonly = &vol.Readonly
				}
				req.Volumes = append(req.Volumes, reqVol)
			}
		case "roms":
			for _, rom := range field.Create.Set.([]*InstanceRom) {
				sources := 0
				if rom.Image != "" {
					sources++
				}
				if rom.Dir != "" {
					sources++
				}
				if sources > 1 {
					return nil, fmt.Errorf("cannot specify more than one of image= or dir= for a ROM")
				}
				if sources == 0 {
					return nil, fmt.Errorf("must specify one of image= or dir= for a ROM")
				}

				var reqRom platform.CreateInstanceRequestRom
				if rom.Dir != "" {
					files, err := inlineFilesFromDir(rom.Dir)
					if err != nil {
						return nil, fmt.Errorf("reading rom dir %q: %w", rom.Dir, err)
					}
					reqRom.Files = files
				} else {
					reqRom.Image = platform.ImageReference(rom.Image)
				}
				if rom.Name != "" {
					reqRom.Name = rom.Name
				}
				if rom.At != "" {
					atJSON, _ := json.Marshal(rom.At)
					if reqRom.AdditionalProperties == nil {
						reqRom.AdditionalProperties = make(map[string]jsontext.Value)
					}
					reqRom.AdditionalProperties["at"] = jsontext.Value(atJSON)
				}
				req.Roms = append(req.Roms, reqRom)
			}
		case "plugins":
			for _, plugin := range field.Create.Set.([]*InstancePlugin) {
				reqPlugin := platform.CreateInstanceRequestPlugin{
					Name: plugin.Name,
					Rom:  platform.ImageReference(plugin.Rom),
				}
				if plugin.Config != "" {
					var config any
					if err := json.Unmarshal([]byte(plugin.Config), &config); err != nil {
						return nil, fmt.Errorf("parsing plugin config: %w", err)
					}
					reqPlugin.Config = &config
				}
				req.Plugins = append(req.Plugins, reqPlugin)
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
					d := platform.CreateInstanceRequestDomain{
						Name: name,
					}
					if ref := domain.Certificate.Ref(); ref.Name != "" || ref.UUID != "" {
						d.Certificate = new(ref.NameOrUUID())
					}
					req.ServiceGroup.Domains = append(req.ServiceGroup.Domains, d)
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
				req.Features = append(req.Features, platform.InstanceFeature(f))
			}
		case "vsock":
			vsock := field.Create.Set.(bool)
			dt, err := json.Marshal(vsock)
			if err != nil {
				return nil, err
			}
			if req.AdditionalProperties == nil {
				req.AdditionalProperties = make(map[string]jsontext.Value)
			}
			req.AdditionalProperties["vsock"] = jsontext.Value(dt)
		case "template":
			template := field.Create.Set.(string)
			key := multimetro.ParseKey(template)
			if key.Metro != "" && metro != "" && key.Metro != metro {
				return nil, fmt.Errorf("metro mismatch between template (%q) and volume (%q)", key.Metro, metro)
			}
			req.Template = &platform.CreateInstanceRequestTemplate{}
			if key.UUID != "" {
				req.Template.Uuid = &key.UUID
			} else if key.Name != "" {
				req.Template.Name = &key.Name
			} else {
				req.Template.Name = &template
			}
		case "branch":
			branch := field.Create.Set.(multimetro.Key)
			if branch.Metro != "" && branch.Metro != metro {
				return nil, fmt.Errorf("cannot create instance: metro mismatch between branch (%q) and instance (%q)", branch.Metro, metro)
			}
			req.BranchFrom = new(branch.Ref().NameOrUUID())
		case "checkpoint":
			cp := field.Create.Set.(multimetro.Key)
			if cp.Metro != "" && cp.Metro != metro {
				return nil, fmt.Errorf("cannot create instance: metro mismatch between checkpoint (%q) and instance (%q)", cp.Metro, metro)
			}
			req.Checkpoint = new(cp.Ref().NameOrUUID())
		}
	}

	if imageURL != "" {
		if pullPolicy != nil {
			req.Image = platform.ImageSpec{Url: imageURL, PullPolicy: pullPolicy}
		} else {
			req.Image = platform.ImageReference(imageURL)
		}
	}

	// Validate that exactly one of image, template, branch, or checkpoint is provided
	sources := 0
	if req.Image != nil {
		sources++
	}
	if req.Template != nil {
		sources++
	}
	if req.BranchFrom != nil {
		sources++
	}
	if req.Checkpoint != nil {
		sources++
	}
	if sources == 0 {
		return nil, fmt.Errorf("either --image, --template, --branch, or --checkpoint must be specified")
	}
	if sources > 1 {
		return nil, fmt.Errorf("only one of --image, --template, --branch, or --checkpoint may be specified")
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
		if resp == nil || resp.Data == nil || len(resp.Data.Instances) == 0 {
			return nil, fmt.Errorf("no instances created")
		}
		created := make(multimetro.Keys, 0, len(resp.Data.Instances))
		for _, instance := range resp.Data.Instances {
			key := multimetro.Key{
				Metro: c.Metro.Name,
				UUID:  instance.Uuid,
				Name:  instance.Name,
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
			{
				Description: "Create an instance from a template",
				Commands: []string{
					`unikraft instance create \
	  --name new-instance \
	  --metro fra \
	  --template my-template`,
				},
			},
			{
				Description: "Create an instance with a plugin",
				Commands: []string{
					`unikraft instance create \
	  --name demo-instance \
	  --metro fra \
	  --image nginx:latest \
	  --plugin name=sandbox,rom=plugins/sandbox:latest`,
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
			{
				Description: "Attach a ROM image to an instance",
				Commands: []string{
					`unikraft instance edit demo-instance \
	  --set roms=image=myuser/my-rom:latest,at=/rom0`,
				},
			},
			{
				Description: "Add a ROM to an instance without replacing existing ones",
				Commands: []string{
					`unikraft instance edit demo-instance \
	  --add roms=dir=./mydata,at=/rom`,
				},
			},
			{
				Description: "Remove a ROM from an instance by name",
				Commands: []string{
					"unikraft instance edit demo-instance --del roms=my-rom",
				},
			},
			{
				Description: "Attach a volume to an instance",
				Commands: []string{
					"unikraft instance edit demo-instance --add volumes=my-vol:/data",
				},
			},
			{
				Description: "Detach a volume from an instance",
				Commands: []string{
					"unikraft instance edit demo-instance --del volumes=my-vol",
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
	Tail   *int `help:"Number of lines to show from the end of the logs."`
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
	keys := multimetro.ParseKeys(cmd.Targets)

	_, err := Instance{}.Get(ctx, keys.Strings())
	if err != nil {
		return err
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

	Follow bool `help:"Follow log output after starting."`

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
		{
			Description: "Start an instance and follow its logs",
			Commands: []string{
				"unikraft instance start demo-instance --follow",
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

	var mux *muxreader.Mux
	if c.Follow {
		var cancel context.CancelFunc
		mux, cancel, err = newInstanceLogMux(ctx, keys, new(0), true)
		if err != nil {
			return err
		}
		defer mux.Close()
		defer cancel()
	}

	started, startErr := startInstances(ctx, g, keys)
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
	if err := errors.Join(opErr, diffErr); err != nil {
		return err
	}
	if !c.Follow || len(started) == 0 {
		return nil
	}
	fmt.Fprintln(stdio.Stdout)

	_, err = io.Copy(stdio.Stdout, mux)
	if mux != nil && errors.Is(err, context.Canceled) {
		return nil
	}
	return err
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
	stopped, stopErr := stopInstances(ctx, g, keys, c.StopOpts)
	opErr = errors.Join(opErr, stopErr)
	if len(stopped) == 0 {
		return opErr
	}

	updated, getErr := Instance{}.Get(ctx, stopped.Strings())
	if getErr != nil {
		if _, ok := getErr.(group.ErrRefNotFound); !ok {
			opErr = errors.Join(opErr, getErr)
			if len(updated) == 0 {
				return opErr
			}
		}
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

	Follow bool `help:"Follow log output after restarting."`

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
		{
			Description: "Restart an instance and follow its logs",
			Commands: []string{
				"unikraft instance restart demo-instance --follow",
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

	stopped, stopErr := stopInstances(ctx, g, keys, c.StopOpts)
	opErr = errors.Join(opErr, stopErr)
	if len(stopped) == 0 {
		return opErr
	}

	var (
		mux    *muxreader.Mux
		cancel context.CancelFunc
	)
	if c.Follow {
		mux, cancel, err = newInstanceLogMux(ctx, stopped, new(0), true)
		if err != nil {
			return err
		}
		defer mux.Close()
		defer cancel()
	}

	started, startErr := startInstances(ctx, g, stopped)
	if len(started) == 0 {
		if _, ok := startErr.(group.ErrRefNotFound); ok {
			return errors.Join(opErr, fmt.Errorf("cannot restart: instance(s) were deleted before they could be started (check if %q feature is enabled)", platform.InstanceFeatureDeleteOnStop))
		}
		opErr = errors.Join(opErr, startErr)
		return opErr
	}
	opErr = errors.Join(opErr, startErr)

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
	if err := errors.Join(opErr, diffErr); err != nil {
		return err
	}
	if !c.Follow || len(started) == 0 {
		return nil
	}
	fmt.Fprintln(stdio.Stdout)

	_, err = io.Copy(stdio.Stdout, mux)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// InstanceHistoryCmd shows the checkpoint history for an instance.
type InstanceHistoryCmd struct {
	Targets []string `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instances to show history for."`

	cmd.FormatOpts
}

func (cmd InstanceHistoryCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Show the checkpoint history of an instance",
			Commands:    []string{"unikraft instance history my-instance"},
		},
	}
}

func (c *InstanceHistoryCmd) Run(ctx context.Context, stdio config.Stdio) error {
	entries, err := getInstanceHistory(ctx, c.Targets, func(ctx context.Context, mc multimetro.MetroClient, ids []platform.NameOrUUID) (*platform.Response[platform.GetCheckpointHistoryResponseData], error) {
		return mc.GetInstanceHistory(ctx, ids, platform.GetInstanceHistoryOpts{})
	})
	if err != nil && len(entries) == 0 {
		return err
	}
	if printErr := renderInstanceHistory(ctx, stdio.Stdout, c.FormatOpts, entries); printErr != nil {
		return errors.Join(err, printErr)
	}
	return err
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
		reqs := make([]platform.StartInstancesRequestItem, 0, len(refs))
		for _, ref := range refs.NameOrUUIDs() {
			reqs = append(reqs, platform.StartInstancesRequestItem{
				Name: ref.Name,
				Uuid: ref.Uuid,
			})
		}
		resp, err := c.StartInstances(ctx, reqs)
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, nil, err
		}
		var started multimetro.Keys
		if resp == nil || resp.Data == nil {
			return nil, nil, nil
		}
		for _, instance := range resp.Data.Instances {
			if instance.Status != platform.ResponseStatusSuccess {
				continue
			}
			started = append(started, multimetro.Key{
				Metro: c.Metro.Name,
				Name:  instance.Name,
				UUID:  instance.Uuid,
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
		if resp == nil || resp.Data == nil {
			return nil, nil, nil
		}
		for _, instance := range resp.Data.Instances {
			if instance.Status == nil || *instance.Status != platform.ResponseStatusSuccess {
				continue
			}
			stopped = append(stopped, multimetro.Key{
				Metro: c.Metro.Name,
				Name:  instance.Name,
				UUID:  instance.Uuid,
			})
		}
		return stopped, stopped.Refs(), nil
	})
	return multimetro.Keys(stopped), err
}

type InstancesSuspendCmd struct {
	Targets      []string         `arg:"" name:"target" completion-predictor:"resource-key-instance" help:"Target instances to suspend."`
	DrainTimeout types.DurationMS `help:"Timeout in milliseconds for draining connections before suspending." default:"-1"`

	cmd.FormatOpts
}

func (cmd InstancesSuspendCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Suspend an instance",
			Commands: []string{
				"unikraft instance suspend demo-instance",
			},
		},
		{
			Description: "Suspend with a drain timeout",
			Commands: []string{
				"unikraft instance suspend demo-instance --drain-timeout 30000",
			},
		},
	}
}

func (c *InstancesSuspendCmd) Run(ctx context.Context, stdio config.Stdio) error {
	keys := multimetro.ParseKeys(c.Targets)
	before, opErr := Instance{}.Get(ctx, keys.Strings())
	if opErr != nil && len(before) == 0 {
		return opErr
	}
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	suspended, suspendErr := suspendInstances(ctx, g, keys, c.DrainTimeout)
	opErr = errors.Join(opErr, suspendErr)
	if len(suspended) == 0 {
		return opErr
	}

	updated, getErr := Instance{}.Get(ctx, suspended.Strings())
	opErr = errors.Join(opErr, getErr)
	if getErr != nil && len(updated) == 0 {
		return opErr
	}

	keySet := make(map[string]struct{}, len(suspended))
	for _, k := range suspended {
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

func suspendInstances(ctx context.Context, g *group.Group[multimetro.MetroClient], keys multimetro.Keys, drainTimeout types.DurationMS) (multimetro.Keys, error) {
	suspended, err := group.CollectRefsSlices(ctx, g, keys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) ([]multimetro.Key, group.Refs, error) {
		log.G(ctx).Trace().Msg("suspending instances")
		reqs := make([]platform.SuspendInstancesRequestItem, 0, len(refs))
		for _, ref := range refs {
			req := platform.SuspendInstancesRequestItem{
				Uuid: ref.NameOrUUID().Uuid,
				Name: ref.NameOrUUID().Name,
			}
			if drainTimeout >= 0 {
				timeout := uint64(drainTimeout)
				req.DrainTimeoutMs = &timeout
			}
			reqs = append(reqs, req)
		}
		resp, err := c.SuspendInstances(ctx, reqs)
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, nil, err
		}
		var suspended multimetro.Keys
		if resp == nil || resp.Data == nil {
			return nil, nil, nil
		}
		for _, instance := range resp.Data.Instances {
			if instance.Status == nil || *instance.Status != platform.ResponseStatusSuccess {
				continue
			}
			suspended = append(suspended, multimetro.Key{
				Metro: c.Metro.Name,
				Name:  instance.Name,
				UUID:  instance.Uuid,
			})
		}
		return suspended, suspended.Refs(), nil
	})
	return multimetro.Keys(suspended), err
}

type InstancesTunnelCmd struct {
	Targets []string `arg:"" name:"target" min:"1" help:"Forwarding target(s) in the form [LOCAL_PORT:]<INSTANCE>:DEST_PORT[/TYPE]." placeholder:"<target>"`

	TunnelProxyPorts   []string `short:"p" name:"tunnel-proxy-port"   help:"Remote port(s) exposed by the tunnel service. When a single value is given it is used as the starting port for multiple targets." default:"4444" hidden:""`
	ProxyControlPort   uint     `short:"P" name:"tunnel-control-port" help:"Command-and-control port of the tunnel service." default:"4443" hidden:""`
	TunnelServiceImage string   `          name:"tunnel-image"        help:"Image to use for the tunnel service." default:"official/utils/tunnel:1.0" hidden:""`
}

func (InstancesTunnelCmd) Help() string {
	return heredoc.Docf(`
		Forward a local port to an unexposed instance through an intermediate
		TLS tunnel service.

		When you need to access an instance on Unikraft Cloud which is not
		publicly exposed to the internet, you can use the
		%[1]sunikraft instance tunnel%[1]s subcommand to forward from a local
		port to a port which the instance listens on.
	`, "`")
}

func (cmd InstancesTunnelCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Forward local port 8080 to instance \"nginx\" port 8080",
			Commands:    []string{"unikraft instance tunnel nginx:8080"},
		},
		{
			Description: "Forward local port 8333 to instance \"nginx\" port 8080",
			Commands:    []string{"unikraft instance tunnel 8333:nginx:8080"},
		},
		{
			Description: "Forward multiple ports from multiple instances",
			Commands:    []string{"unikraft instance tunnel 8080:my-instance1:8080/tcp 8443:my-instance2:8080/tcp"},
		},
		{
			Description: "Forward local port 8080 to instance \"my-instance1\" port 8080 on fra metro using TCP",
			Commands:    []string{"unikraft instance tunnel 8080:fra/my-instance1:8080/tcp"},
		},
		{
			Description: "Use a custom relay port to avoid collisions",
			Commands:    []string{"unikraft instance tunnel -p 5500 my-instance:8080"},
		},
	}
}

func (cmd *InstancesTunnelCmd) Run(ctx context.Context, stdio config.Stdio) error {
	targets, err := tunnel.ParseTargets(cmd.Targets, cmd.TunnelProxyPorts)
	if err != nil {
		return fmt.Errorf("could not parse targets: %w", err)
	}

	tun, err := tunnel.New(ctx, targets)
	if err != nil {
		return fmt.Errorf("could not create tunnel: %w", err)
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}

	defer func() {
		// Detach from ctx so cleanup survives the same Ctrl-C that triggered
		// it, but bound it so a stuck API call can't leave a still-billed
		// proxy instance orphaned.
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := tun.Close(closeCtx, g); err != nil {
			log.G(ctx).Error().Err(err).Msg("could not terminate tunnel proxy")
		}
	}()

	return tun.Run(ctx, g, cmd.ProxyControlPort, cmd.TunnelServiceImage)
}
