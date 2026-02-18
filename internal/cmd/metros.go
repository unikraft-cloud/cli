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
	"unikraft.com/cli/internal/xsync"
	"unikraft.com/x/kingkong"
)

type MetrosCmd struct {
	cmd.ResourceCmd[Metro]
	cmd.GettableResourceCmd[Metro] `set:"name=metro" set:"names=metros"`
	cmd.ListableResourceCmd[Metro] `set:"name=metro" set:"names=metros"`
}

type Metro struct {
	Name     string `field:",short" json:"name"`
	Country  string `field:",short" json:"country"`
	Endpoint string `field:",short" json:"endpoint"`
	Insecure bool   `field:",long" json:"insecure"`
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

	baseClient := httpclient.GetClient(i.Insecure)

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
			Name:     metro.Name.String(),
			Country:  metro.Country.String(),
			Endpoint: metro.Endpoint.String(),
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
		},
	}
}
