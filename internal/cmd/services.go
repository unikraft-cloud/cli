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
	"strconv"
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
	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/cli/internal/types"
)

type ServicesCmd struct {
	cmd.ResourceCmd[ServiceGroup]
	cmd.GettableResourceCmd[ServiceGroup]
	cmd.WaitableResourceCmd[ServiceGroup]
	cmd.ListableResourceCmd[ServiceGroup]
	cmd.BulkDeletableResourceCmd[ServiceGroup]

	Create ServiceCreateCmd `cmd:"" help:"Create a service group."`
	Edit   ServiceEditCmd   `cmd:"" help:"Edit a service group."`
}

// ServiceCreateCmd extends the generic resource create command with shortcut
// flags for commonly used service group fields. Each field tagged with
// `shortcut:"<path>"` is translated into a --set <path>=<value> entry before
// the standard create pipeline runs.
type ServiceCreateCmd struct {
	cmd.ResourceCreateCmd[ServiceGroup]

	Metro string `group:"flag-create" shortcut:"metro" help:"Metro to create in." placeholder:"metro" example:"fra,sfo,nyc"`
	Name  string `group:"flag-create" shortcut:"name" help:"Service group name." placeholder:"name"`

	SoftLimit uint64 `group:"flag-create" shortcut:"limits.soft" help:"Soft limit." placeholder:"n" example:"1,5"`
	HardLimit uint64 `group:"flag-create" shortcut:"limits.hard" help:"Hard limit." placeholder:"n" example:"10,100"`

	Domains  []Domain  `group:"flag-create" shortcut:"domains" help:"Service domains." placeholder:"fqdn" example:"example.com,api.example.com"`
	Services []Service `group:"flag-create" shortcut:"services" help:"Service ports." placeholder:"<src>:<dest>[/<handlers>]" example:"443:8080/http+tls,80:8080/http"`
}

func (c *ServiceCreateCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	return c.ResourceCreateCmd.Run(ctx, stdio, sandbox)
}

// ServiceEditCmd extends the generic resource edit command with shortcut
// flags for commonly used editable service group fields. Each field tagged with
// `shortcut:"<path>"` is translated into a --set <path>=<value> entry before
// the standard edit pipeline runs.
type ServiceEditCmd struct {
	cmd.ResourceEditCmd[ServiceGroup]

	SoftLimit uint64 `group:"flag-edit" shortcut:"limits.soft" help:"Soft limit." placeholder:"n" example:"1,5"`
	HardLimit uint64 `group:"flag-edit" shortcut:"limits.hard" help:"Hard limit." placeholder:"n" example:"10,100"`

	Domains  []Domain  `group:"flag-edit" shortcut:"domains" help:"Service domains." placeholder:"fqdn" example:"example.com,api.example.com"`
	Services []Service `group:"flag-edit" shortcut:"services" help:"Service ports." placeholder:"<src>:<dest>[/<handlers>]" example:"443:8080/http+tls,80:8080/http"`
}

func (c *ServiceEditCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	return c.ResourceEditCmd.Run(ctx, stdio, sandbox)
}

type ServiceGroup struct {
	MetroName LinkName[Metro] `mirror:"metro.name" field:"metro,short" create:"set,required"`
	Name      string          `mirror:"service_group.name" field:",short" create:"set"`
	UUID      string          `mirror:"service_group.uuid" field:",long"`

	Persistent bool `mirror:"service_group.persistent" field:",long"`
	Autoscale  bool `mirror:"service_group.autoscale" field:",short"`

	Limits struct {
		Soft uint64 `mirror:"service_group.soft_limit" field:",long" create:"set" edit:"set"`
		Hard uint64 `mirror:"service_group.hard_limit" field:",long" create:"set" edit:"set"`
	}

	Timestamps struct {
		Created types.RelativeTime `mirror:"service_group.created_at" field:",short"`
	}

	Domains []Domain `mirror:"service_group.domains" field:",embed" create:"set" edit:"set,add,del"`

	Instances []struct {
		Link[Instance]
	} `mirror:"service_group.instances"`

	Services []*Service `mirror:"service_group.services" field:",embed" create:"set,required" edit:"set,add,del"`

	ServiceGroup platform.ServiceGroup `field:"-" json:"service_group"`
	Metro        *config.Metro         `field:"-" json:"metro"`

	key multimetro.Key
}

type Service struct {
	Source      uint32                     `mirror:"port" json:"source" field:",short"`
	Destination uint32                     `mirror:"destination_port" json:"destination" field:",short"`
	Handlers    []platform.ServiceHandlers `mirror:"handlers" json:"handlers" field:",short"`
}

func (s *Service) MarshalText() ([]byte, error) {
	handlers := make([]string, len(s.Handlers))
	for i, handler := range s.Handlers {
		handlers[i] = string(handler)
	}
	return fmt.Appendf([]byte{}, "%d:%d/%s",
		s.Source,
		s.Destination,
		strings.Join(handlers, "+"),
	), nil
}

// MarshalJSON outputs the struct form (not the short text form).
// This takes precedence over MarshalText for JSON/YAML serialization.
func (s *Service) MarshalJSON() ([]byte, error) {
	type serviceJSON Service // alias to avoid recursion
	return json.Marshal((*serviceJSON)(s))
}

// UnmarshalJSON parses both the struct form and the short text form.
// This takes precedence over UnmarshalText for JSON/YAML deserialization.
func (s *Service) UnmarshalJSON(data []byte) error {
	if len(data) != 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		return s.UnmarshalText([]byte(text))
	}
	type serviceJSON Service // alias to avoid recursion
	return json.Unmarshal(data, (*serviceJSON)(s))
}

func (s *Service) UnmarshalText(text []byte) error {
	str := string(text)
	ports, handlers, _ := strings.Cut(str, "/")
	src, dest, ok := strings.Cut(ports, ":")
	if !ok {
		return fmt.Errorf("invalid service format, expected SOURCE:DESTINATION/HANDLERS")
	}

	srcPort, err := strconv.Atoi(src)
	if err != nil {
		return fmt.Errorf("invalid source port: %w", err)
	}
	s.Source = uint32(srcPort)

	destPort, err := strconv.Atoi(dest)
	if err != nil {
		return fmt.Errorf("invalid destination port: %w", err)
	}
	s.Destination = uint32(destPort)

	if handlers != "" {
		for handler := range strings.SplitSeq(handlers, "+") {
			s.Handlers = append(s.Handlers, platform.ServiceHandlers(handler))
		}
	}

	return nil
}

type Domain struct {
	// TODO: consolidate these and make easier to parse
	FQDN string `name:"fqdn" json:"fqdn" mirror:"fqdn" field:",short"`
	Name string `name:"name" json:"name,omitempty" field:"-"` // field:"-" excludes from field system, name:"name" allows --set parsing

	Certificate struct {
		Link[Certificate]
	} `name:"certificate" json:"certificate,omitzero" mirror:"certificate"`
}

func (d *Domain) UnmarshalText(text []byte) error {
	str := strings.TrimSpace(string(text))
	if str == "" {
		return nil
	}

	if strings.Contains(str, "=") {
		type domainAlias Domain
		parsed, err := value.Parse[domainAlias]([]string{str})
		if err != nil {
			return err
		}
		*d = Domain(parsed)
		return nil
	}

	if trimmed, ok := strings.CutSuffix(str, "."); ok {
		d.FQDN = trimmed
		return nil
	}

	if strings.Contains(str, ".") {
		d.FQDN = str
		return nil
	}

	d.Name = str
	return nil
}

func (d *Domain) UnmarshalJSON(data []byte) error {
	if len(data) != 0 && data[0] == '"' {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		return d.UnmarshalText([]byte(text))
	}
	type domainJSON Domain
	return json.Unmarshal(data, (*domainJSON)(d))
}

func (ServiceGroup) Type() resource.Type {
	return resource.Type{
		Name:  "service",
		Names: "services",
	}
}

func (s ServiceGroup) Key() resource.Key {
	return s.key
}

func (s ServiceGroup) Raw() any {
	return s.ServiceGroup
}

func (s ServiceGroup) Fields(ctx context.Context) ([]resource.Field, error) {
	s.MetroName = LinkName[Metro](defaultMetro(ctx, string(s.MetroName)))
	return resource.FieldsFromStruct(s)
}

func (ServiceGroup) List(ctx context.Context) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return group.CollectAllSlices(ctx, g, func(ctx context.Context, c multimetro.MetroClient) ([]resource.Resource, error) {
		log.G(ctx).Trace().Msg("listing service groups")
		resp, err := c.GetServiceGroups(ctx, nil, platform.GetServiceGroupsOpts{Details: new(true)})
		if err != nil {
			return nil, err
		}
		var results []resource.Resource
		var errs []error
		for _, serviceGroup := range resp.Data.ServiceGroups {
			result, err := ServiceGroup{}.load(nil, serviceGroup, &c.Metro)
			if err != nil {
				errs = append(errs, err)
			}
			results = append(results, result)
		}
		return results, errors.Join(errs...)
	})
}

func (ServiceGroup) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return group.CollectRefsSlices(ctx, g, multimetro.ParseKeys(keys).Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) ([]resource.Resource, group.Refs, error) {
		log.G(ctx).Trace().Msg("getting service groups")
		resp, err := c.GetServiceGroups(ctx, refs.NameOrUUIDs(), platform.GetServiceGroupsOpts{Details: new(true)})
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, nil, err
		}
		var found []group.Ref
		var results []resource.Resource
		var errs []error
		for i, serviceGroup := range resp.Data.ServiceGroups {
			if serviceGroup.Status == nil || *serviceGroup.Status != platform.ResponseStatusSUCCESS {
				continue
			}
			result, err := ServiceGroup{}.load(&refs[i], serviceGroup, &c.Metro)
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

func (ServiceGroup) load(ref *group.Ref, serviceGroup platform.ServiceGroup, metro *config.Metro) (ServiceGroup, error) {
	if ref == nil {
		ref = &group.Ref{
			Metro: metro.Name,
			Name:  ptr.ZeroIfNil(serviceGroup.Name),
			UUID:  ptr.ZeroIfNil(serviceGroup.Uuid),
		}
	} else {
		ref.Metro = cmp.Or(ref.Metro, metro.Name)
		ref.Name = cmp.Or(ref.Name, ptr.ZeroIfNil(serviceGroup.Name))
		ref.UUID = cmp.Or(ref.UUID, ptr.ZeroIfNil(serviceGroup.Uuid))
	}

	result := ServiceGroup{
		ServiceGroup: serviceGroup,
		Metro:        metro,
		key:          multimetro.Key(*ref),
	}
	err := mirror.Mirror(result, &result)
	if err != nil {
		return ServiceGroup{}, fmt.Errorf("could not mirror service group data: %w", err)
	}
	return result, nil
}

func (ServiceGroup) Delete(ctx context.Context, targets []resource.Resource) error {
	keys := make(multimetro.Keys, len(targets))
	for i, target := range targets {
		sg := target.(ServiceGroup)
		keys[i] = sg.key
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, keys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		log.G(ctx).Trace().Msg("deleting service groups")
		_, err := c.DeleteServiceGroups(ctx, refs.NameOrUUIDs())
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, err
		}
		return refs, nil
	})
}

func (ServiceGroup) Create(ctx context.Context, fields []resource.Field) ([]resource.Resource, error) {
	var req platform.CreateServiceGroupRequest
	var metro string
	for key, field := range resource.IterFields(fields) {
		if field.Create != nil && field.Create.Set != nil {
			switch key.String() {
			case "metro":
				metro = string(field.Create.Set.(LinkName[Metro]))
			case "name":
				name := field.Create.Set.(string)
				req.Name = &name
			case "limits.soft":
				limit := field.Create.Set.(uint64)
				req.SoftLimit = &limit
			case "limits.hard":
				limit := field.Create.Set.(uint64)
				req.HardLimit = &limit
			case "domains":
				for _, domain := range field.Create.Set.([]Domain) {
					name := domain.Name
					if name == "" {
						name = domain.FQDN + "."
					}
					req.Domains = append(req.Domains, platform.CreateServiceGroupRequestDomain{
						Name: name,
					})
				}
			case "services":
				for _, svc := range field.Create.Set.([]*Service) {
					req.Services = append(req.Services, platform.Service{
						Port:            svc.Source,
						DestinationPort: &svc.Destination,
						Handlers:        svc.Handlers,
					})
				}
			}
		}
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	keys, err := group.CollectMetro(ctx, g, metro, func(ctx context.Context, c multimetro.MetroClient) (multimetro.Keys, error) {
		log.G(ctx).Trace().Msg("creating service group")
		resp, err := c.CreateServiceGroup(ctx, req)
		if err != nil {
			return nil, err
		}
		if len(resp.Data.ServiceGroups) == 0 {
			return nil, fmt.Errorf("no service groups created")
		}
		created := make(multimetro.Keys, 0, len(resp.Data.ServiceGroups))
		for _, group := range resp.Data.ServiceGroups {
			key := multimetro.Key{
				Metro: c.Metro.Name,
				UUID:  ptr.ZeroIfNil(group.Uuid),
				Name:  ptr.ZeroIfNil(group.Name),
			}
			created = append(created, key)
		}
		return created, nil
	})
	if err != nil {
		return nil, err
	}
	results, err := ServiceGroup{}.Get(ctx, keys.Strings())
	if err != nil {
		return nil, err
	}
	return results, nil
}

func (ServiceGroup) Edit(ctx context.Context, target resource.Resource, fields []resource.Field) (resource.Resource, error) {
	sg := target.(ServiceGroup)
	patches := patchRequests(fields, serviceGroupPatchSpec)
	reqs := make([]platform.UpdateServiceGroupsRequestItem, 0, len(patches))
	for _, patch := range patches {
		reqs = append(reqs, platform.UpdateServiceGroupsRequestItem{
			Uuid:  &sg.UUID,
			Op:    platform.UpdateServiceGroupsRequestItemOp(patch.Op),
			Prop:  patch.Prop,
			Value: new(patch.Value),
		})
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	err = group.DoMetro(ctx, g, sg.key.Metro, func(ctx context.Context, metroClient multimetro.MetroClient) error {
		log.G(ctx).Trace().Msg("updating service group")
		_, err := metroClient.UpdateServiceGroups(ctx, reqs)
		return err
	})
	if err != nil {
		return nil, err
	}
	results, err := ServiceGroup{}.Get(ctx, []string{sg.Key().String()})
	if err != nil {
		return nil, err
	}
	return results[0], nil
}

func (ServiceGroup) Examples() map[cmd.CmdType][]kingkong.Example {
	return map[cmd.CmdType][]kingkong.Example{
		cmd.CmdTypeGet: {
			{
				Description: "Inspect a service group by name",
				Commands:    []string{"unikraft service get demo-service"},
			},
			{
				Description: "Inspect a service group by UUID",
				Commands:    []string{"unikraft service get d4f0c132-a70e-4777-9631-7dfe08a1c15d"},
			},
			{
				Description: "Inspect a service group in a specific metro",
				Commands:    []string{"unikraft service get fra/demo-service"},
			},
			{
				Description: "Show service details in JSON format",
				Commands:    []string{"unikraft service get demo-service -o json"},
			},
		},
		cmd.CmdTypeList: {
			{
				Description: "List all service groups across metros",
				Commands:    []string{"unikraft service list"},
			},
			{
				Description: "List services and watch for changes",
				Commands:    []string{"unikraft service list -w"},
			},
		},
		cmd.CmdTypeCreate: {
			{
				Description: "Create a service group with HTTPS",
				Commands: []string{
					`unikraft service create \
	--name demo-service \
	--metro fra \
	--domains demo \
	--services 443:8080/http+tls`,
				},
			},
			{
				Description: "Create a service group with HTTPS and HTTP-to-HTTPS redirect",
				Commands: []string{
					`unikraft service create \
	--name web-service \
	--metro fra \
	--domains my-app.example.com \
	--services 443:8080/http+tls \
	--services 80:443/http+redirect`,
				},
			},
			{
				Description: "Create a service group with rate limits",
				Commands: []string{
					`unikraft service create \
	--name api-service \
	--metro fra \
	--services 443:8080/http+tls \
	--soft-limit 100 \
	--hard-limit 200`,
				},
			},
			{
				Description: "Preview service creation without applying",
				Commands: []string{
					`unikraft service create \
	--name test-service \
	--metro fra \
	--services 443:8080/http+tls \
	--dry-run`,
				},
			},
		},
		cmd.CmdTypeEdit: {
			{
				Description: "Add a new service port",
				Commands: []string{
					"unikraft service edit demo-service --add services=8443:9090/http+tls",
				},
			},
			{
				Description: "Add a domain to a service group",
				Commands: []string{
					"unikraft service edit demo-service --add domains=api.example.com",
				},
			},
			{
				Description: "Update rate limits",
				Commands: []string{
					"unikraft service edit demo-service --soft-limit 50 --hard-limit 100",
				},
			},
			{
				Description: "Remove a domain from a service group",
				Commands: []string{
					"unikraft service edit demo-service --del domains=old.example.com",
				},
			},
		},
		cmd.CmdTypeDelete: {
			{
				Description: "Delete a service group by name",
				Commands:    []string{"unikraft service delete demo-service"},
			},
			{
				Description: "Delete a service group by UUID",
				Commands:    []string{"unikraft service delete d4f0c132-a70e-4777-9631-7dfe08a1c15d"},
			},
			{
				Description: "Delete all service groups (with confirmation)",
				Commands:    []string{"unikraft service delete --all"},
			},
		},
	}
}

func serviceGroupPatchSpec(path string, _ patchOp, value any) (platform.UpdateServiceGroupsRequestItemProp, any) {
	var zero platform.UpdateServiceGroupsRequestItemProp
	switch path {
	case "limits.soft":
		return platform.UpdateServiceGroupsRequestItemPropSoft_limit, value.(uint64)
	case "limits.hard":
		return platform.UpdateServiceGroupsRequestItemPropHard_limit, value.(uint64)
	case "domains":
		nvalue := []platform.CreateServiceGroupRequestDomain{}
		for _, domain := range value.([]Domain) {
			name := domain.Name
			if name == "" {
				name = domain.FQDN + "."
			}
			nvalue = append(nvalue, platform.CreateServiceGroupRequestDomain{
				Name: name,
			})
		}
		return platform.UpdateServiceGroupsRequestItemPropDomains, nvalue
	case "services":
		nvalue := []platform.Service{}
		for _, svc := range value.([]*Service) {
			nvalue = append(nvalue, platform.Service{
				Port:            svc.Source,
				DestinationPort: &svc.Destination,
				Handlers:        svc.Handlers,
			})
		}
		return platform.UpdateServiceGroupsRequestItemPropServices, nvalue
	default:
		return zero, nil
	}
}
