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
	"unikraft.com/cli/internal/model"
	"unikraft.com/cli/internal/model/cmd"
	"unikraft.com/cli/internal/types"
	wsync "unikraft.com/cli/internal/w/sync"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"
)

type MetrosCmd struct {
	cmd.ModelCmd[Metro]
	cmd.GettableModelCmd[Metro]
	cmd.ListableModelCmd[Metro]
}

type Metro struct {
	Name     string `field:",short" json:"name" help:"Metro name, such as fra."`
	Location string `field:",short" json:"location" help:"IATA code of the metro location."`
	Endpoint string `field:",short" json:"endpoint" help:"API endpoint URL of the metro."`
	Insecure *bool  `field:",long" json:"insecure" help:"Skip TLS verification when connecting to the metro's API and image index."`

	url string
}

func (Metro) Type() model.Type {
	return model.Type{
		Name:  "metro",
		Names: "metros",
	}
}

func (i Metro) Key() model.Key {
	return staticKey(i.Name)
}

func (i Metro) Raw() any {
	return i
}

func (i Metro) Fields(ctx context.Context) ([]model.Field, error) {
	fields, err := model.FieldsFromStruct(i)
	if err != nil {
		return nil, err
	}

	baseClient := httpclient.GetClient(ptr.ZeroIfNil(i.Insecure))

	quotas := &metroQuotas{
		httpClient: baseClient,
		endpoint:   i.url,
		name:       i.Name,
	}
	quotaFields, err := model.FieldsFromStruct(quotas)
	if err != nil {
		return nil, err
	}
	fields = append(fields, model.Field{
		Name:      "quotas",
		Help:      "Usage and limits of your quota in the metro.",
		Verbosity: model.FieldVerbosityLong,
		Subfields: quotaFields,
	})

	u, _ := url.Parse(i.url)
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
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, i.url, nil)
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
	fields = append(fields, model.Field{
		Name:      "status",
		Help:      "Reachability of the metro endpoint.",
		Verbosity: model.FieldVerbosityLong,
		Subfields: []model.Field{
			{
				Name:          "ip",
				Help:          "IP addresses the endpoint resolves to.",
				Verbosity:     model.FieldVerbosityLong,
				ValueCallback: ip,
			},
			{
				Name:          "ping",
				Help:          "TCP connection latency to the endpoint.",
				Verbosity:     model.FieldVerbosityLong,
				ValueCallback: ping,
			},
			{
				Name:          "online",
				Help:          "Whether the endpoint answers an HTTP request without a server error: online or offline.",
				Verbosity:     model.FieldVerbosityLong,
				ValueCallback: online,
			},
		},
	})

	return fields, nil
}

func (Metro) List(ctx context.Context) ([]model.Model, error) {
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return nil, err
	}

	var results []model.Model
	for _, metro := range profile.Metros {
		result := Metro{
			Name:     metro.Name,
			Location: metro.Location,
			Endpoint: metro.Endpoint,
			Insecure: metro.Insecure,
			url:      metro.URL(),
		}
		results = append(results, result)
	}
	return results, nil
}

func (Metro) Get(ctx context.Context, keys []string) ([]model.Model, error) {
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
		Active types.Usage[int64] `field:",long,embed" help:"Instances not in the stopped state. The platform reports the vCPU limit as its limit."`
		Total  types.Usage[int64] `field:",long,embed" help:"All instances."`
	} `field:",long" help:"Instance counts."`
	Vcpus struct {
		Active types.Usage[int64] `field:",long,embed" help:"vCPUs of instances not in the stopped state."`
	} `field:",long" help:"vCPU usage."`
	Memory struct {
		Active types.Usage[types.SizeMebibytes] `field:",long,embed" help:"Memory assigned to instances not in the stopped state."`
	} `field:",long" help:"Memory usage."`
	Services struct {
		Groups  types.Usage[int64] `field:",long,embed" help:"Service groups."`
		Exposed types.Usage[int64] `field:",long,embed" help:"Published ports across all service groups."`
	} `field:",long" help:"Service usage."`
	Volumes struct {
		Count types.Usage[int64]               `field:",long,embed" help:"Number of volumes."`
		Total types.Usage[types.SizeMebibytes] `field:",long,embed" help:"Volume space counted against your quota: the full size of static volumes plus the used space of dynamic ones."`
	} `field:",long" help:"Volume usage."`
	Limits struct {
		Vcpus     types.Range[int64]               `field:",long,embed" help:"vCPUs per instance."`
		Memory    types.Range[types.SizeMebibytes] `field:",long,embed" help:"Memory per instance."`
		Volume    types.Range[types.SizeMebibytes] `field:",long,embed" help:"Size of a volume."`
		Autoscale types.Range[int64]               `field:",long,embed" help:"Size of an autoscale group."`
	} `field:",long" help:"Allowed range for a single resource."`

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
