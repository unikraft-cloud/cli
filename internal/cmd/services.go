// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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

type ServicesCmd struct {
	cmd.ResourceCmd[ServiceGroup]
	cmd.GettableResourceCmd[ServiceGroup]      `set:"name=service" set:"names=services"`
	cmd.WaitableResourceCmd[ServiceGroup]      `set:"name=service" set:"names=services"`
	cmd.ListableResourceCmd[ServiceGroup]      `set:"name=service" set:"names=services"`
	cmd.BulkDeletableResourceCmd[ServiceGroup] `set:"name=service" set:"names=services"`
	cmd.EditableResourceCmd[ServiceGroup]      `set:"name=service" set:"names=services"`
	cmd.CreatableResourceCmd[ServiceGroup]     `set:"name=service" set:"names=services"`
}

type ServiceGroup struct {
	MetroName string `mirror:"metro.name" field:"metro,short" create:"set,required"`
	Name      string `mirror:"service_group.name" field:",short" create:"set"`
	UUID      string `mirror:"service_group.uuid" field:",long"`

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
		Name string `mirror:"name" field:",long"`
		UUID string `mirror:"uuid" field:",long"`
	} `mirror:"service_group.instances"`

	Services []*Service `mirror:"service_group.services" field:",embed" create:"set,required" edit:"set,add,del"`

	ServiceGroup platform.ServiceGroup `field:"-" json:"service_group"`
	Metro        *config.Metro         `field:"-" json:"metro"`
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
	FQDN string `mirror:"fqdn" field:",short"`
	Name string `field:",invisible"` // edit-only field

	Certificate struct {
		Name string `mirror:"name" field:",long"`
		UUID string `mirror:"uuid" field:",long"`
	} `mirror:"certificate"`
}

func (ServiceGroup) Type() resource.Type {
	return resource.Type{
		Name:  "service",
		Names: "services",
	}
}

func (s ServiceGroup) key() multimetro.Key {
	return multimetro.Key{
		Metro: s.MetroName,
		Name:  s.Name,
		UUID:  s.UUID,
	}
}

func (s ServiceGroup) Key() resource.Key {
	return s.key()
}

func (s ServiceGroup) Raw() any {
	return s.ServiceGroup
}

func (s ServiceGroup) Fields() ([]resource.Field, error) {
	result, err := resource.FieldsFromStruct(s)
	if err != nil {
		return nil, err
	}

	for key, field := range resource.IterFields(result) {
		if key.MatchesString("domains.*.certificate") {
			nameField, _ := field.Get("name")
			uuidField, _ := field.Get("uuid")
			name, _ := nameField.Value.(string)
			uuid, _ := uuidField.Value.(string)
			if name != "" && uuid != "" {
				field.Links = append(field.Links, resource.Link{
					Type: "certificate",
					Key: multimetro.Key{
						Metro: s.Metro.Name.String(),
						Name:  name,
						UUID:  uuid,
					}.String(),
				})
			}
		}
	}

	return result, nil
}

func (ServiceGroup) List(ctx context.Context) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return group.CollectAllSlices(ctx, g, func(ctx context.Context, c multimetro.MetroClient) ([]resource.Resource, error) {
		log.G(ctx).Trace().Msg("listing service groups")
		resp, err := c.GetServiceGroups(ctx, nil, true)
		if err != nil {
			return nil, err
		}
		var results []resource.Resource
		for _, serviceGroup := range resp.Data.ServiceGroups {
			result, err := ServiceGroup{}.load(serviceGroup, &c.Metro)
			if err != nil {
				return nil, err
			}
			results = append(results, result)
		}
		return results, nil
	})
}

func (ServiceGroup) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return group.CollectRefsSlices(ctx, g, multimetro.ParseKeys(keys).Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) ([]resource.Resource, group.Refs, error) {
		log.G(ctx).Trace().Msg("getting service groups")
		resp, err := c.GetServiceGroups(ctx, refs.NameOrUUIDs(), true)
		if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return nil, nil, err
		}
		var found []group.Ref
		var results []resource.Resource
		for _, serviceGroup := range resp.Data.ServiceGroups {
			if serviceGroup.Status == nil || *serviceGroup.Status != platform.ResponseStatusSUCCESS {
				continue
			}
			result, err := ServiceGroup{}.load(serviceGroup, &c.Metro)
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

func (ServiceGroup) load(serviceGroup platform.ServiceGroup, metro *config.Metro) (ServiceGroup, error) {
	result := ServiceGroup{
		ServiceGroup: serviceGroup,
		Metro:        metro,
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
		keys[i] = sg.key()
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return err
	}
	return group.DoRefs(ctx, g, keys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		log.G(ctx).Trace().Msg("deleting service groups")
		_, err := c.DeleteServiceGroups(ctx, refs.NameOrUUIDs())
		return refs, err
	})
}

func (ServiceGroup) Create(ctx context.Context, fields []resource.Field) ([]resource.Resource, error) {
	var req platform.CreateServiceGroupRequest
	var metro string
	for key, field := range resource.IterFields(fields) {
		if field.Create != nil && field.Create.Set != nil {
			switch key.String() {
			case "metro":
				metro = field.Create.Set.(string)
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
				Metro: c.Metro.Name.String(),
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
			Value: platform.Ptr(patch.Value),
		})
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	err = group.DoMetro(ctx, g, sg.key().Metro, func(ctx context.Context, metroClient multimetro.MetroClient) error {
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
					`unikraft service create \
  --set name=demo-service \
  --set metro=fra \
  --set domains=name=demo \
  --set services=443:8080/tls+http`,
				},
			},
		},
		cmd.CmdTypeEdit: {
			{
				Description: "Add a new service port",
				Commands:    []string{"unikraft service edit demo-service --add services=8443:8080/tls"},
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
