// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"time"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/httpclient"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/types"
	wsync "unikraft.com/cli/internal/w/sync"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"
)

type MetrosCmd struct {
	cmd.ResourceCmd[Metro]
	cmd.GettableResourceCmd[Metro]
	cmd.ListableResourceCmd[Metro]
}

type Metro struct {
	Name     string `field:",short" json:"name"`
	Location string `field:",short" json:"location"`
	Endpoint string `field:",short" json:"endpoint"`
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

func (i Metro) Fields(ctx context.Context) ([]resource.Field, error) {
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

	resolveIPs := wsync.OnceCtxValues(func(ctx context.Context) ([]string, error) {
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

	ip := wsync.OnceCtxValues(func(ctx context.Context) (any, error) {
		ips, err := resolveIPs(ctx)
		if err != nil || len(ips) == 0 {
			return "", nil
		}
		return ips, nil
	})

	ping := wsync.OnceCtxValues(func(ctx context.Context) (any, error) {
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

	online := wsync.OnceCtxValues(func(ctx context.Context) (any, error) {
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
			Location: metro.Location,
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
	if resp == nil || resp.Data == nil || len(resp.Data.Quotas) == 0 {
		return new(metroQuotas), nil
	}

	quotas := &resp.Data.Quotas[0]
	result := metroQuotasFromPlatform(quotas)
	return &result, nil
}

func metroQuotasFromPlatform(quotas *platform.Quotas) metroQuotas {
	if quotas == nil {
		return metroQuotas{}
	}

	result := metroQuotas{}
	result.Instances.Active = types.Usage[int64]{
		Used:  quotas.Used.LiveInstances,
		Limit: quotas.Hard.LiveInstances,
	}
	result.Instances.Total = types.Usage[int64]{
		Used:  quotas.Used.Instances,
		Limit: quotas.Hard.Instances,
	}
	result.Vcpus.Active = types.Usage[int64]{
		Used:  quotas.Used.LiveVcpus,
		Limit: quotas.Hard.LiveVcpus,
	}
	result.Memory.Active = types.Usage[types.SizeMebibytes]{
		Used:  types.SizeMebibytes(quotas.Used.LiveMemoryMb),
		Limit: types.SizeMebibytes(quotas.Hard.LiveMemoryMb),
	}
	result.Services.Groups = types.Usage[int64]{
		Used:  quotas.Used.ServiceGroups,
		Limit: quotas.Hard.ServiceGroups,
	}
	result.Services.Exposed = types.Usage[int64]{
		Used:  quotas.Used.Services,
		Limit: quotas.Hard.Services,
	}
	result.Volumes.Count = types.Usage[int64]{
		Used:  quotas.Used.Volumes,
		Limit: quotas.Hard.Volumes,
	}
	result.Volumes.Total = types.Usage[types.SizeMebibytes]{
		Used:  types.SizeMebibytes(quotas.Used.TotalVolumeMb),
		Limit: types.SizeMebibytes(quotas.Hard.TotalVolumeMb),
	}

	result.Limits.Vcpus = types.Range[int64]{
		Min: quotas.Limits.MinVcpus,
		Max: quotas.Limits.MaxVcpus,
	}
	result.Limits.Memory = types.Range[types.SizeMebibytes]{
		Min: types.SizeMebibytes(quotas.Limits.MinMemoryMb),
		Max: types.SizeMebibytes(quotas.Limits.MaxMemoryMb),
	}
	result.Limits.Volume = types.Range[types.SizeMebibytes]{
		Min: types.SizeMebibytes(quotas.Limits.MinVolumeMb),
		Max: types.SizeMebibytes(quotas.Limits.MaxVolumeMb),
	}
	result.Limits.Autoscale = types.Range[int64]{
		Min: quotas.Limits.MinAutoscaleSize,
		Max: quotas.Limits.MaxAutoscaleSize,
	}

	return result
}
