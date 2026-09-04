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

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"

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

	Create cmd.ResourceCreateCmd[ServiceGroup] `cmd:"" help:"Create a service group."`
	Edit   cmd.ResourceEditCmd[ServiceGroup]   `cmd:"" help:"Edit a service group."`
}

type ServiceGroup struct {
	Metro LinkName[Metro] `field:"metro,short" create:"set,required" flag:"metro" help:"Metro to create in." placeholder:"metro" example:"fra,sfo"`
	Name  string          `mirror:"service_group.name" field:",short" create:"set" flag:"name" help:"Service group name." placeholder:"name"`
	UUID  string          `mirror:"service_group.uuid" field:",long"`

	Persistent bool `mirror:"service_group.persistent" field:",long"`
	Autoscale  bool `mirror:"service_group.autoscale" field:",short"`

	Limits struct {
		Soft uint64 `mirror:"service_group.soft_limit" field:",long" create:"set" edit:"set" flag:"soft-limit" help:"Soft limit." placeholder:"n" example:"1,5"`
		Hard uint64 `mirror:"service_group.hard_limit" field:",long" create:"set" edit:"set" flag:"hard-limit" help:"Hard limit." placeholder:"n" example:"10,100"`
	}

	Timestamps struct {
		Created types.RelativeTime `mirror:"service_group.created_at" field:",short"`
	}

	Domains []Domain `mirror:"service_group.domains" field:",embed" create:"set" edit:"set,add,del" flag:"domain" sep:"none" help:"Service domain." placeholder:"fqdn" example:"example.com"`

	Instances []struct {
		Link[Instance]
	} `mirror:"service_group.instances"`

	Services []*Service `mirror:"service_group.services" field:",embed" create:"set,required" edit:"set,add,del" flag:"service" sep:"none" help:"Service port." placeholder:"<src>:<dest>[/<handlers>]" example:"443:8080/http+tls"`

	ServiceGroup platform.ServiceGroup `field:"-" json:"service_group"`

	key multimetro.Key
}

type Service struct {
	Source      uint32                       `mirror:"port" json:"source" field:",short"`
	Destination uint32                       `mirror:"destination_port" json:"destination" field:",short"`
	Handlers    []platform.ConnectionHandler `mirror:"handlers" json:"handlers" field:",short"`
}

func (s *Service) MarshalText() ([]byte, error) {
	handlers := make([]string, len(s.Handlers))
	for i, handler := range s.Handlers {
		handlers[i] = string(handler)
	}
	return fmt.Appendf(
		[]byte{}, "%d:%d/%s",
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
			s.Handlers = append(s.Handlers, platform.ConnectionHandler(handler))
		}
	}

	return nil
}

type Domain struct {
	// TODO: consolidate these and make easier to parse
	FQDN string `name:"fqdn" json:"fqdn" mirror:"fqdn" field:",short"`
	Name string `name:"name" json:"name,omitempty" field:"-"` // field:"-" excludes from field system, name:"name" allows --set parsing

	Certificate TextLink[Certificate] `name:"certificate" json:"certificate,omitzero" mirror:"certificate" field:",embed"`
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
	s.Metro = LinkName[Metro](defaultMetro(ctx, string(s.Metro)))
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
		if resp == nil || resp.Data == nil {
			return nil, nil
		}
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
		if resp == nil || resp.Data == nil {
			return nil, nil, nil
		}
		for _, serviceGroup := range resp.Data.ServiceGroups {
			if serviceGroup.Status == nil || *serviceGroup.Status != platform.ResponseStatusSuccess {
				continue
			}

			matchedRef := matchRef(refs, serviceGroup.Name, serviceGroup.Uuid)
			result, err := ServiceGroup{}.load(matchedRef, serviceGroup, &c.Metro)
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

func (ServiceGroup) load(ref *group.Ref, serviceGroup platform.ServiceGroup, metro *config.Metro) (ServiceGroup, error) {
	if ref == nil {
		ref = &group.Ref{
			Metro: metro.Name,
			Name:  serviceGroup.Name,
			UUID:  serviceGroup.Uuid,
		}
	} else {
		ref.Metro = cmp.Or(ref.Metro, metro.Name)
		ref.Name = cmp.Or(ref.Name, serviceGroup.Name)
		ref.UUID = cmp.Or(ref.UUID, serviceGroup.Uuid)
	}

	result := ServiceGroup{
		ServiceGroup: serviceGroup,
		Metro:        LinkName[Metro](metro.Name),
		key:          multimetro.Key(*ref),
	}
	err := mirror.Mirror(result, &result)
	if err != nil {
		return ServiceGroup{}, fmt.Errorf("could not mirror service group data: %w", err)
	}
	return result, nil
}

func (ServiceGroup) Delete(ctx context.Context, keys []string) error {
	parsedKeys := multimetro.ParseKeys(keys)

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, parsedKeys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
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
					d := platform.CreateServiceGroupRequestDomain{
						Name: name,
					}
					if ref := domain.Certificate.Ref(); ref.Name != "" || ref.UUID != "" {
						d.Certificate = new(ref.NameOrUUID())
					}
					req.Domains = append(req.Domains, d)
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
		if resp == nil || resp.Data == nil || len(resp.Data.ServiceGroups) == 0 {
			return nil, fmt.Errorf("no service groups created")
		}
		created := make(multimetro.Keys, 0, len(resp.Data.ServiceGroups))
		for _, group := range resp.Data.ServiceGroups {
			key := multimetro.Key{
				Metro: c.Metro.Name,
				UUID:  group.Uuid,
				Name:  group.Name,
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

func (ServiceGroup) Edit(ctx context.Context, key string, fields []resource.Field) error {
	parsedKeys := multimetro.ParseKeys([]string{key})
	patches, err := patchRequests(fields, serviceGroupPatchSpec)
	if err != nil {
		return err
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, parsedKeys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		reqs := make([]platform.UpdateServiceGroupsRequestItem, 0, len(refs)*len(patches))
		for _, ref := range refs {
			for _, patch := range patches {
				req := platform.UpdateServiceGroupsRequestItem{
					Op:    platform.MutableServiceGroupOperation(patch.Op),
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
		log.G(ctx).Trace().Msg("updating service group")
		_, err := c.UpdateServiceGroups(ctx, reqs)
		if err != nil {
			if platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
				return nil, nil
			}
			return nil, err
		}
		return refs, nil
	})
}

func (ServiceGroup) Examples() map[cmd.CmdType][]kingkong.Example {
	return map[cmd.CmdType][]kingkong.Example{
		cmd.CmdTypeGet: {
			{
				Description: "Inspect a service group by name or UUID",
				Commands:    []string{"unikraft service get demo-service"},
			},
		},
		cmd.CmdTypeList: {
			{
				Description: "List all service groups",
				Commands:    []string{"unikraft service list"},
			},
		},
		cmd.CmdTypeCreate: {
			{
				Description: "Create a new service group",
				Commands: []string{
					// `unikraft service create \
					//   --set name=demo-service \
					//   --set metro=fra \
					//   --set domains=demo \
					//   --set services=443:8080/tls+http`,
					`unikraft service create \
	--name demo-service \
	--metro fra \
	--domain demo \
	--service 443:8080/tls+http`,
				},
			},
		},
		cmd.CmdTypeEdit: {
			{
				Description: "Add a new service port",
				Commands: []string{
					// "unikraft service edit demo-service --add services=8443:8080/tls",
					"unikraft service edit demo-service --service 8443:8080/tls",
				},
			},
		},
		cmd.CmdTypeDelete: {
			{
				Description: "Delete a service group by name or UUID",
				Commands:    []string{"unikraft service delete demo-service"},
			},
		},
	}
}

func serviceGroupPatchSpec(path string, _ patchOp, value any) (platform.MutableServiceGroupProperty, any, error) {
	var zero platform.MutableServiceGroupProperty
	switch path {
	case "limits.soft":
		return platform.MutableServiceGroupPropertySoftLimit, value.(uint64), nil
	case "limits.hard":
		return platform.MutableServiceGroupPropertyHardLimit, value.(uint64), nil
	case "domains":
		nvalue := []platform.CreateServiceGroupRequestDomain{}
		for _, domain := range value.([]Domain) {
			name := domain.Name
			if name == "" {
				name = domain.FQDN + "."
			}
			d := platform.CreateServiceGroupRequestDomain{
				Name: name,
			}
			if ref := domain.Certificate.Ref(); ref.Name != "" || ref.UUID != "" {
				d.Certificate = new(ref.NameOrUUID())
			}
			nvalue = append(nvalue, d)
		}
		return platform.MutableServiceGroupPropertyDomains, nvalue, nil
	case "services":
		nvalue := []platform.Service{}
		for _, svc := range value.([]*Service) {
			nvalue = append(nvalue, platform.Service{
				Port:            svc.Source,
				DestinationPort: &svc.Destination,
				Handlers:        svc.Handlers,
			})
		}
		return platform.MutableServiceGroupPropertyServices, nvalue, nil
	default:
		return zero, nil, nil
	}
}
