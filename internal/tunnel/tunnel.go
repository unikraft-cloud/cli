// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"

	"golang.org/x/sync/errgroup"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/cloud/sdk/platform/volimport"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"
)

// Target describes a single forwarding destination parsed from the CLI
// argument format [LOCAL_PORT:][METRO/]INSTANCE:DEST_PORT[/TYPE].
type Target struct {
	// host is the instance identifier (possibly including a metro prefix like
	// "fra/my-instance").
	host string
	// source is the local port to listen on. 0 means the OS picks a free port.
	source uint16
	// dest is the port on the remote instance to forward to.
	dest uint16
	// network is the connection type, e.g. "tcp" (like in net.Dial).
	network string
	// exposedProxyPort is the port exposed by the tunnel service for this
	// target. It is computed from the proxy port configuration.
	exposedProxyPort uint16
}

// resolvedTarget is a Target once its instance has been looked up on the
// platform, recording its private IP and the metro it lives in.
type resolvedTarget struct {
	Target
	ip    string
	metro string
}

type Tunnel struct {
	targets []resolvedTarget

	auth string
	// proxies maps metro name to the running tunnel proxy in that metro.
	// Populated by Run, consumed by Close. Entries are pointers so that
	// Close's "closed" bookkeeping mutates the map's own entries.
	proxies map[string]*proxyInfo
}

// New creates a tunnel from the given targets. It resolves each target's
// instance to a private IP and metro via the platform API.
func New(ctx context.Context, targets []Target) (*Tunnel, error) {
	authStr, err := volimport.GenRandAuth()
	if err != nil {
		return nil, fmt.Errorf("could not generate auth string: %w", err)
	}

	resolved, err := resolveTargets(ctx, targets)
	if err != nil {
		return nil, err
	}

	return &Tunnel{targets: resolved, auth: authStr}, nil
}

// resolveTargets looks up each target's instance on the platform, returning a
// resolvedTarget carrying its private IP and metro alongside the original
// Target.
//
// TODO(craciunoiuc): extract a generic group.CollectRefsMap helper for this
// key -> instance lookup, so the same logic can be shared with "instance
// logs".
func resolveTargets(ctx context.Context, targets []Target) ([]resolvedTarget, error) {
	keys := make(multimetro.Keys, len(targets))
	for i, tgt := range targets {
		keys[i] = multimetro.ParseKey(tgt.host)
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	type instanceInfo struct {
		ip    string
		metro string
	}
	type resolvedInstance struct {
		ref  group.Ref
		info instanceInfo
	}

	resolved, err := group.CollectRefsSlices(ctx, g, keys.Refs(),
		func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) ([]resolvedInstance, group.Refs, error) {
			log.G(ctx).Trace().Msg("getting instances for tunnel")
			resp, err := c.GetInstances(ctx, refs.NameOrUUIDs(), platform.GetInstancesOpts{Details: new(true)})
			if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
				return nil, nil, err
			}
			if resp == nil || resp.Data == nil {
				return nil, nil, nil
			}
			var found group.Refs
			var results []resolvedInstance
			for i, inst := range resp.Data.Instances {
				if inst.Status == nil || *inst.Status != platform.ResponseStatusSuccess {
					continue
				}
				if len(inst.NetworkInterfaces) == 0 || inst.NetworkInterfaces[0].PrivateIp == "" {
					return nil, nil, fmt.Errorf("instance %q has no private IP", refs[i].Display)
				}
				found = append(found, refs[i])
				results = append(results, resolvedInstance{
					ref: refs[i],
					info: instanceInfo{
						ip:    inst.NetworkInterfaces[0].PrivateIp,
						metro: c.Metro.Name,
					},
				})
			}
			return results, found, nil
		})
	if err != nil {
		return nil, fmt.Errorf("could not resolve instances: %w", err)
	}

	// NOTE(craciunoiuc): if the same instance name appears in multiple metros
	// (no metro prefix given), the last-seen result wins. Users should include
	// a metro prefix to disambiguate (e.g. fra/my-instance:8080).
	infoByDisplay := make(map[string]instanceInfo, len(resolved))
	for _, r := range resolved {
		infoByDisplay[r.ref.Display] = r.info
	}

	resolvedTargets := make([]resolvedTarget, len(targets))
	for i, tgt := range targets {
		info, ok := infoByDisplay[tgt.host]
		if !ok {
			return nil, fmt.Errorf("could not determine metro for %q: include the metro prefix (e.g. fra/my-instance:8080)", tgt.host)
		}
		resolvedTargets[i] = resolvedTarget{Target: tgt, ip: info.ip, metro: info.metro}
	}
	return resolvedTargets, nil
}

// Run creates one tunnel service proxy instance per unique metro and stores
// their metadata. For each metro, a control relay is
// established to the proxy in that metro, then data relays are started for
// every target in that metro.
func (t *Tunnel) Run(ctx context.Context, g *group.Group[multimetro.MetroClient], proxyControlPort uint, tunnelImage string) error {
	// Group targets by metro.
	metroTargets := make(map[string][]resolvedTarget)
	for _, tgt := range t.targets {
		metroTargets[tgt.metro] = append(metroTargets[tgt.metro], tgt)
	}
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return fmt.Errorf("getting current profile: %w", err)
	}

	metroInsecureMap := make(map[string]bool)
	for _, m := range profile.Metros {
		metroInsecureMap[m.Name] = ptr.ZeroIfNil(m.Insecure)
	}

	t.proxies = make(map[string]*proxyInfo, len(metroTargets))
	for metro, targets := range metroTargets {
		info, err := createProxy(ctx, g, metro, targets, t.auth, proxyControlPort, tunnelImage)
		if info.uuid != "" {
			// Track the proxy even when it fails after creation, so Close can
			// still find and delete the already-provisioned, already-billed
			// instance.
			t.proxies[metro] = &info
		}
		if err != nil {
			return fmt.Errorf("creating proxy in metro %q: %w", metro, err)
		}
	}

	eg, ctx := errgroup.WithContext(ctx)

	// Start a control relay per metro proxy. Errors (including panics) are
	// propagated through the errgroup so a broken control relay cancels the
	// whole tunnel instead of only being logged.
	for metro, proxy := range t.proxies {
		cr := tunnelRelay{
			remoteAddr: net.JoinHostPort(proxy.fqdn, strconv.FormatUint(uint64(proxyControlPort), 10)),
			auth:       t.auth,
			insecure:   metroInsecureMap[metro],
		}
		ready := make(chan struct{})
		eg.Go(func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					log.G(ctx).Error().Interface("panic", r).Str("metro", metro).Msg("recovered from panic in control relay")
					err = fmt.Errorf("panic in control relay for metro %q: %v", metro, r)
				}
			}()
			defer close(ready)
			if cErr := cr.controlUp(ctx, ready); cErr != nil {
				return fmt.Errorf("control relay for metro %q: %w", metro, cErr)
			}
			return nil
		})
		// Wait for this metro's control relay before starting its data relays.
		<-ready
	}

	for _, tgt := range t.targets {
		proxy := t.proxies[tgt.metro]
		r := tunnelRelay{
			// TODO(antoineco): allow dual-stack by creating two separate listeners.
			// Alternatively, we could default to "::" to create a tcp46 socket, but
			// listening on all addresses is an insecure default.
			localAddr:  net.JoinHostPort("127.0.0.1", strconv.FormatUint(uint64(tgt.source), 10)),
			remoteAddr: net.JoinHostPort(proxy.fqdn, strconv.FormatUint(uint64(tgt.exposedProxyPort), 10)),
			// NOTE(craciunoiuc): Only TCP is supported at the moment. This refers to the
			// local listener; the remote side always uses TLS-over-TCP.
			connectionType: tgt.network,
			auth:           t.auth,
			name:           proxy.uuid,
			nameAddr:       fmt.Sprintf("%s:%d", tgt.host, tgt.dest),
			insecure:       metroInsecureMap[tgt.metro],
		}
		metro := tgt.metro
		eg.Go(func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					log.G(ctx).Error().Interface("panic", r).Str("metro", metro).Msg("recovered from panic in data relay")
					err = fmt.Errorf("panic in data relay for metro %q: %v", metro, r)
				}
			}()
			return r.up(ctx)
		})
	}

	return eg.Wait()
}

// Close removes all tunnel proxy instances across all metros.
func (t *Tunnel) Close(ctx context.Context, g *group.Group[multimetro.MetroClient]) error {
	var errs []error
	for metro, proxy := range t.proxies {
		if proxy.closed {
			continue
		}

		err := group.DoMetro(ctx, g, metro, func(ctx context.Context, c multimetro.MetroClient) error {
			log.G(ctx).Trace().Msg("deleting tunnel proxy instance")
			_, err := c.DeleteInstances(ctx, []platform.DeleteInstanceRequestItem{{Uuid: &proxy.uuid}})
			if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
				return fmt.Errorf("deleting proxy instance %q: %w", proxy.uuid, err)
			}
			proxy.closed = true
			return nil
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
