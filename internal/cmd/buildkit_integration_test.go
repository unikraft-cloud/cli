// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package cmd

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"unikraft.com/cloud/sdk/platform"
	xbuildkit "unikraft.com/x/buildkit"
	"unikraft.com/x/ptr"

	"unikraft.com/cli/internal/httpclient"
	"unikraft.com/cli/internal/integration"
)

// TestBuildkitPlatformAcquire creates a builder in the integration profile's
// metro and deletes it afterwards.
func TestBuildkitPlatformAcquire(t *testing.T) {
	integration.SkipUnlessIntegration(t)
	cfg, err := integration.LoadRemote(t)
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("integration config not found")
	}
	require.NoError(t, err)
	if cfg == nil || cfg.Config == nil {
		t.Skip("integration config unavailable")
	}

	pc := platform.NewClient(
		platform.WithHTTPClient(httpclient.GetClient(ptr.ZeroIfNil(cfg.Metro.Insecure))),
		platform.WithToken(cfg.Profile.Token),
		platform.WithDefaultMetro(cfg.Metro.URL()),
	)
	p := xbuildkit.NewPlatform(pc,
		xbuildkit.WithVCPUs(1),
		xbuildkit.WithMemory(1024),
		xbuildkit.WithCacheSize(2048),
		xbuildkit.WithInsecure(ptr.ZeroIfNil(cfg.Metro.Insecure)),
	)
	key := xbuildkit.Key{Name: "test-" + strings.ToLower(rand.Text()[:8])}
	name := "buildkit-" + key.Name
	t.Cleanup(func() { deleteBuilder(t, pc, name) })

	// Two first builds race to create the builder.
	var wg sync.WaitGroup
	daemons := make([]*xbuildkit.Daemon, 2)
	errs := make([]error, 2)
	for i := range daemons {
		wg.Go(func() { daemons[i], errs[i] = p.Acquire(t.Context(), key) })
	}
	wg.Wait()
	for i, d := range daemons {
		require.NoError(t, errs[i])
		assert.Equal(t, daemons[0].Addr, d.Addr)
		assert.Equal(t, daemons[0].TLS, d.TLS)
	}
	solve(t, daemons[0])
	for _, d := range daemons {
		require.NoError(t, d.Release())
	}

	// A stopped builder does not wake on connect.
	_, err = pc.StopInstances(t.Context(), []platform.StopInstancesRequestItem{{Name: &name}})
	require.NoError(t, err)
	_, err = pc.WaitInstances(t.Context(), []platform.WaitInstancesRequestItem{{Name: &name, State: new(platform.InstanceStateStopped)}}, platform.WaitInstancesOpts{})
	require.NoError(t, err)
	d, err := p.Acquire(t.Context(), key)
	require.NoError(t, err)
	solve(t, d)
	require.NoError(t, d.Release())

	// Nor does one that is still shutting down when Acquire finds it.
	_, err = pc.StopInstances(t.Context(), []platform.StopInstancesRequestItem{{Name: &name, DrainTimeoutMs: new(uint64(10_000))}})
	require.NoError(t, err)
	d, err = p.Acquire(t.Context(), key)
	require.NoError(t, err)
	solve(t, d)
	require.NoError(t, d.Release())
}

func solve(t *testing.T, d *xbuildkit.Daemon) {
	t.Helper()
	def, err := llb.Image("docker.io/library/alpine:3.22", llb.LinuxAmd64).Run(llb.Shlex("true")).Root().Marshal(t.Context(), llb.LinuxAmd64)
	require.NoError(t, err)
	_, err = d.Client.Solve(t.Context(), def, client.SolveOpt{}, nil)
	require.NoError(t, err)
}

func deleteBuilder(t *testing.T, pc platform.Client, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	_, err := pc.DeleteInstances(ctx, []platform.DeleteInstanceRequestItem{{Name: &name, DontRetain: new(true)}})
	if err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
		t.Errorf("deleting builder %s: %v", name, err)
	}
	// The volume stays attached until the instance is gone.
	vol := name + "-cache"
	for {
		_, err := pc.DeleteVolumes(ctx, []platform.NameOrUUID{{Name: &vol}})
		if err == nil || platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
			return
		}
		select {
		case <-ctx.Done():
			t.Errorf("deleting volume %s: %v", vol, err)
			return
		case <-time.After(3 * time.Second):
		}
	}
}
