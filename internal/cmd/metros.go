// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/alecthomas/kong"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/httpclient"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/types"
	"unikraft.com/cli/internal/xsync"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"
)

type MetrosCmd struct {
	cmd.ResourceCmd[Metro]
	cmd.GettableResourceCmd[Metro]
	cmd.ListableResourceCmd[Metro]
	cmd.DeletableResourceCmd[Metro]

	Create MetroCreateCmd `cmd:"" help:"Create a metro."`
	Edit   MetroEditCmd   `cmd:"" help:"Edit a metro."`
}

// MetroCreateCmd extends the generic create command with shortcut flags.
type MetroCreateCmd struct {
	cmd.ResourceCreateCmd[Metro]

	Name     string `group:"flag-create" shortcut:"name" short:"n" help:"Metro name." placeholder:"name" example:"fra,sfo,nyc"`
	Endpoint string `group:"flag-create" shortcut:"endpoint" help:"Metro endpoint URL." placeholder:"url" example:"https://api.fra.unikraft.cloud"`
	Country  string `group:"flag-create" shortcut:"country" help:"Country code." placeholder:"code" example:"de,us,gb"`
}

func (c *MetroCreateCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	return c.ResourceCreateCmd.Run(ctx, stdio, sandbox)
}

// MetroEditCmd extends the generic edit command with shortcut flags.
type MetroEditCmd struct {
	cmd.ResourceEditCmd[Metro]

	Endpoint string `group:"flag-edit" shortcut:"endpoint" help:"Metro endpoint URL." placeholder:"url" example:"https://api.fra.unikraft.cloud"`
	Country  string `group:"flag-edit" shortcut:"country" help:"Country code." placeholder:"code" example:"de,us,gb"`
}

func (c *MetroEditCmd) Run(ctx context.Context, stdio config.Stdio, sandbox *resource.Sandbox, kctx *kong.Context) error {
	if err := cmd.ApplyShortcutFlags(&c.SetArgs, kctx.Flags()); err != nil {
		return err
	}
	return c.ResourceEditCmd.Run(ctx, stdio, sandbox)
}

type Metro struct {
	Name     string `field:",short" json:"name" create:"set,required"`
	Country  string `field:",short" json:"country" create:"set" edit:"set"`
	Endpoint string `field:",short" json:"endpoint" create:"set,required" edit:"set"`
	Insecure *bool  `field:",long" json:"insecure"`
}

func (Metro) Type() resource.Type {
	return resource.Type{
		Name:  "metro",
		Names: "metros",
	}
}

func (i Metro) Key() resource.Key {
	return staticKey(i.Name)
}

func (i Metro) Raw() any {
	return i
}

func (i Metro) Fields() ([]resource.Field, error) {
	fields, err := resource.FieldsFromStruct(i)
	if err != nil {
		return nil, err
	}

	baseClient := httpclient.GetClient(ptr.ZeroIfNil(i.Insecure))

	quotas := &metroQuotas{
		httpClient: baseClient,
		endpoint:   i.Endpoint,
		name:       i.Name,
	}
	quotaFields, err := resource.FieldsFromStruct(quotas)
	if err != nil {
		return nil, err
	}
	fields = append(fields, resource.Field{
		Name:      "quotas",
		Verbosity: resource.FieldVerbosityLong,
		Subfields: quotaFields,
	})

	u, _ := url.Parse(i.Endpoint)
	host := ""
	scheme := ""
	port := ""
	if u != nil {
		host = u.Hostname()
		scheme = u.Scheme
		port = u.Port()
	}
	if port == "" {
		switch scheme {
		case "http":
			port = "80"
		case "":
			port = ""
		default:
			port = "443"
		}
	}

	const timeout = 5 * time.Second

	resolveIPs := xsync.OnceCtxValues(func(ctx context.Context) ([]string, error) {
		if host == "" {
			return nil, nil
		}
		if ip := net.ParseIP(host); ip != nil {
			return []string{ip.String()}, nil
		}

		log.G(ctx).Trace().Str("metro", i.Name).Msg("resolving metro IP")
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		seen := make(map[string]struct{}, len(addrs))
		ips := make([]string, 0, len(addrs))
		for _, addr := range addrs {
			if addr.IP == nil {
				continue
			}
			s := addr.IP.String()
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			ips = append(ips, s)
		}
		return ips, nil
	})

	ip := xsync.OnceCtxValues(func(ctx context.Context) (any, error) {
		ips, err := resolveIPs(ctx)
		if err != nil || len(ips) == 0 {
			return "", nil
		}
		return ips, nil
	})

	ping := xsync.OnceCtxValues(func(ctx context.Context) (any, error) {
		if port == "" {
			return "", nil
		}

		ips, err := resolveIPs(ctx)
		if err != nil || len(ips) == 0 {
			return "", nil
		}

		log.G(ctx).Trace().Str("metro", i.Name).Msg("pinging metro")
		addr := net.JoinHostPort(ips[0], port)
		dialer := &net.Dialer{Timeout: timeout}
		start := time.Now()
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		elapsed := time.Since(start)
		if err != nil {
			return "", nil
		}
		conn.Close()
		return types.PingLatency(elapsed), nil
	})

	online := xsync.OnceCtxValues(func(ctx context.Context) (any, error) {
		log.G(ctx).Trace().Str("metro", i.Name).Msg("checking metro online status")
		client := &http.Client{
			Timeout:   timeout,
			Transport: baseClient.Transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, i.Endpoint, nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return types.MetroStatusOffline, nil
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 500 {
			// Consider any 2xx-4xx response as "online"
			return types.MetroStatusOnline, nil
		}
		return types.MetroStatusOffline, nil
	})

	// Add a grouped "status" field with lazy-computed subfields
	fields = append(fields, resource.Field{
		Name:      "status",
		Verbosity: resource.FieldVerbosityLong,
		Subfields: []resource.Field{
			{
				Name:          "ip",
				Verbosity:     resource.FieldVerbosityLong,
				ValueCallback: ip,
			},
			{
				Name:          "ping",
				Verbosity:     resource.FieldVerbosityLong,
				ValueCallback: ping,
			},
			{
				Name:          "online",
				Verbosity:     resource.FieldVerbosityLong,
				ValueCallback: online,
			},
		},
	})

	return fields, nil
}

func (Metro) List(ctx context.Context) ([]resource.Resource, error) {
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, err
	}

	var results []resource.Resource
	for _, metro := range profile.Metros {
		result := Metro{
			Name:     metro.Name,
			Country:  metro.Country,
			Endpoint: metro.Endpoint,
			Insecure: metro.Insecure,
		}
		results = append(results, result)
	}
	return results, nil
}

func (Metro) Get(ctx context.Context, keys []string) ([]resource.Resource, error) {
	return getFromListable(ctx, Metro{}, keys)
}

func (Metro) Create(ctx context.Context, fields []resource.Field) ([]resource.Resource, error) {
	cfg := config.FromContextOrDefault(ctx)
	profile, err := cfg.CurrentProfile()
	if err != nil {
		return nil, err
	}

	var metro config.Metro
	for key, field := range resource.IterFields(fields) {
		if field.Create == nil || field.Create.Set == nil {
			continue
		}
		switch key.String() {
		case "name":
			metro.Name = field.Create.Set.(string)
		case "country":
			metro.Country = field.Create.Set.(string)
		case "endpoint":
			metro.Endpoint = field.Create.Set.(string)
		}
	}

	for _, existing := range profile.Metros {
		if existing.Name == metro.Name {
			return nil, fmt.Errorf("metro already exists: %s", metro.Name)
		}
	}

	updated := *profile
	updated.Metros = append(slices.Clone(updated.Metros), metro)
	cfg.AddProfile(updated)
	if err := cfg.Save(); err != nil {
		return nil, err
	}

	return []resource.Resource{Metro{
		Name:     metro.Name,
		Country:  metro.Country,
		Endpoint: metro.Endpoint,
	}}, nil
}

func (Metro) Edit(ctx context.Context, target resource.Resource, fields []resource.Field) (resource.Resource, error) {
	cfg := config.FromContextOrDefault(ctx)
	profile, err := cfg.CurrentProfile()
	if err != nil {
		return nil, err
	}

	metro := target.(Metro)
	updated := *profile
	for i := range updated.Metros {
		if updated.Metros[i].Name != metro.Name {
			continue
		}
		for key, field := range resource.IterFields(fields) {
			if field.Edit == nil || field.Edit.Set == nil {
				continue
			}
			switch key.String() {
			case "country":
				updated.Metros[i].Country = field.Edit.Set.(string)
			case "endpoint":
				updated.Metros[i].Endpoint = field.Edit.Set.(string)
			}
		}

		cfg.AddProfile(updated)
		if err := cfg.Save(); err != nil {
			return nil, err
		}

		return Metro{
			Name:     updated.Metros[i].Name,
			Country:  updated.Metros[i].Country,
			Endpoint: updated.Metros[i].Endpoint,
		}, nil
	}

	return nil, fmt.Errorf("metro not found: %s", metro.Name)
}

func (Metro) Delete(ctx context.Context, targets []resource.Resource) error {
	cfg := config.FromContextOrDefault(ctx)
	profile, err := cfg.CurrentProfile()
	if err != nil {
		return err
	}

	remove := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		metro := target.(Metro)
		remove[metro.Name] = struct{}{}
	}

	updated := *profile
	updated.Metros = slices.DeleteFunc(slices.Clone(updated.Metros), func(metro config.Metro) bool {
		_, ok := remove[metro.Name]
		return ok
	})

	cfg.AddProfile(updated)
	return cfg.Save()
}

func (Metro) Examples() map[cmd.CmdType][]kingkong.Example {
	return map[cmd.CmdType][]kingkong.Example{
		cmd.CmdTypeGet: {
			{
				Description: "Inspect a metro by name",
				Commands:    []string{"unikraft metro get fra"},
			},
		},
		cmd.CmdTypeList: {
			{
				Description: "List all metros",
				Commands:    []string{"unikraft metro list"},
			},
			{
				Description: "List metros with connection status",
				Commands:    []string{"unikraft metro list -f +status"},
			},
			{
				Description: "List metros with quota usage",
				Commands:    []string{"unikraft metro list -f +quotas"},
			},
		},
		cmd.CmdTypeCreate: {
			{
				Description: "Create a new metro",
				Commands: []string{
					`unikraft metro create \
  --name fra \
  --endpoint https://api.fra.unikraft.cloud \
  --country de`,
				},
			},
		},
		cmd.CmdTypeEdit: {
			{
				Description: "Update a metro endpoint",
				Commands:    []string{"unikraft metro edit fra --endpoint https://api.fra.unikraft.cloud"},
			},
		},
		cmd.CmdTypeDelete: {
			{
				Description: "Delete a metro",
				Commands:    []string{"unikraft metro delete fra"},
			},
		},
	}
}

type metroQuotas struct {
	Instances struct {
		Active types.Usage[int64] `field:",long,embed"`
		Total  types.Usage[int64] `field:",long,embed"`
	} `field:",long"`
	Vcpus struct {
		Active types.Usage[int64] `field:",long,embed"`
	} `field:",long"`
	Memory struct {
		Active types.Usage[types.SizeMebibytes] `field:",long,embed"`
	} `field:",long"`
	Services struct {
		Groups  types.Usage[int64] `field:",long,embed"`
		Exposed types.Usage[int64] `field:",long,embed"`
	} `field:",long"`
	Volumes struct {
		Count types.Usage[int64]               `field:",long,embed"`
		Total types.Usage[types.SizeMebibytes] `field:",long,embed"`
	} `field:",long"`
	Limits struct {
		Vcpus     types.Range[int64]               `field:",long,embed"`
		Memory    types.Range[types.SizeMebibytes] `field:",long,embed"`
		Volume    types.Range[types.SizeMebibytes] `field:",long,embed"`
		Autoscale types.Range[int64]               `field:",long,embed"`
	} `field:",long"`

	httpClient *http.Client
	endpoint   string
	name       string
}

func (q *metroQuotas) Lazy(ctx context.Context) (any, error) {
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, err
	}

	client := platform.NewClient(
		platform.WithHTTPClient(q.httpClient),
		platform.WithToken(profile.Token),
		platform.WithDefaultMetro(q.endpoint),
	)

	log.G(ctx).Trace().Str("metro", q.name).Msg("fetching metro quotas")
	resp, err := client.GetUser(ctx)
	if err != nil {
		return nil, err
	}
	if resp.Data == nil || len(resp.Data.Quotas) == 0 {
		return new(metroQuotas), nil
	}

	quotas := &resp.Data.Quotas[0]

	result := new(metroQuotas)
	result.Instances.Active = types.Usage[int64]{
		Used:  ptr.ZeroIfNil(quotas.Used.LiveInstances),
		Limit: ptr.ZeroIfNil(quotas.Hard.LiveInstances),
	}
	result.Instances.Total = types.Usage[int64]{
		Used:  ptr.ZeroIfNil(quotas.Used.Instances),
		Limit: ptr.ZeroIfNil(quotas.Hard.Instances),
	}
	result.Vcpus.Active = types.Usage[int64]{
		Used:  ptr.ZeroIfNil(quotas.Used.LiveVcpus),
		Limit: ptr.ZeroIfNil(quotas.Hard.LiveVcpus),
	}
	result.Memory.Active = types.Usage[types.SizeMebibytes]{
		Used:  types.SizeMebibytes(ptr.ZeroIfNil(quotas.Used.LiveMemoryMb)),
		Limit: types.SizeMebibytes(ptr.ZeroIfNil(quotas.Hard.LiveMemoryMb)),
	}
	result.Services.Groups = types.Usage[int64]{
		Used:  ptr.ZeroIfNil(quotas.Used.ServiceGroups),
		Limit: ptr.ZeroIfNil(quotas.Hard.ServiceGroups),
	}
	result.Services.Exposed = types.Usage[int64]{
		Used:  ptr.ZeroIfNil(quotas.Used.Services),
		Limit: ptr.ZeroIfNil(quotas.Hard.Services),
	}
	result.Volumes.Count = types.Usage[int64]{
		Used:  ptr.ZeroIfNil(quotas.Used.Volumes),
		Limit: ptr.ZeroIfNil(quotas.Hard.Volumes),
	}
	result.Volumes.Total = types.Usage[types.SizeMebibytes]{
		Used:  types.SizeMebibytes(ptr.ZeroIfNil(quotas.Used.TotalVolumeMb)),
		Limit: types.SizeMebibytes(ptr.ZeroIfNil(quotas.Hard.TotalVolumeMb)),
	}

	result.Limits.Vcpus = types.Range[int64]{
		Min: ptr.ZeroIfNil(quotas.Limits.MinVcpus),
		Max: ptr.ZeroIfNil(quotas.Limits.MaxVcpus),
	}
	result.Limits.Memory = types.Range[types.SizeMebibytes]{
		Min: types.SizeMebibytes(ptr.ZeroIfNil(quotas.Limits.MinMemoryMb)),
		Max: types.SizeMebibytes(ptr.ZeroIfNil(quotas.Limits.MaxMemoryMb)),
	}
	result.Limits.Volume = types.Range[types.SizeMebibytes]{
		Min: types.SizeMebibytes(ptr.ZeroIfNil(quotas.Limits.MinVolumeMb)),
		Max: types.SizeMebibytes(ptr.ZeroIfNil(quotas.Limits.MaxVolumeMb)),
	}
	result.Limits.Autoscale = types.Range[int64]{
		Min: ptr.ZeroIfNil(quotas.Limits.MinAutoscaleSize),
		Max: ptr.ZeroIfNil(quotas.Limits.MaxAutoscaleSize),
	}

	return result, nil
}
