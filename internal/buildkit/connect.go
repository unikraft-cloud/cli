// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The KraftKit Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package buildkit

import (
	"context"
	"fmt"
	"net"
	"os"
	"runtime"

	"github.com/moby/buildkit/client"
	bkappdefaults "github.com/moby/buildkit/util/appdefaults"
	dockerclient "github.com/moby/moby/client"

	_ "github.com/moby/buildkit/client/connhelper/dockercontainer"
	_ "github.com/moby/buildkit/client/connhelper/kubepod"
	_ "github.com/moby/buildkit/client/connhelper/nerdctlcontainer"
	_ "github.com/moby/buildkit/client/connhelper/npipe"
	_ "github.com/moby/buildkit/client/connhelper/podmancontainer"
	_ "github.com/moby/buildkit/client/connhelper/ssh"

	"unikraft.com/x/log"
)

func ConnectToBuildkit(ctx context.Context) (c *client.Client, cleanup func(), rerr error) {
	if c = BuildkitFromContext(ctx); c != nil {
		return c, func() {}, nil
	}

	var buildkitInfo *client.Info
	var buildkitAddr string

	// Check if there is a buildkit host configured
	if addr, ok := os.LookupEnv("BUILDKIT_HOST"); ok {
		var err error
		c, err = client.New(ctx, addr)
		if err != nil {
			return nil, nil, fmt.Errorf("creating buildkit client: %w", err)
		}
		buildkitAddr = addr
		buildkitInfo, err = c.Info(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("connecting to buildkit client: %w", err)
		}
		log.G(ctx).Debug().Str("addr", addr).Msg("using configured buildkit")
	}

	// Check if the default buildkit socket is available.  On Windows, the
	// default is a native buildkitd, which cannot build Linux images.
	if c == nil && runtime.GOOS != "windows" {
		var err error
		c, err = client.New(ctx, bkappdefaults.Address)
		if err != nil {
			return nil, nil, fmt.Errorf("creating default buildkit client: %w", err)
		}
		buildkitAddr = bkappdefaults.Address
		buildkitInfo, err = c.Info(ctx)
		if err != nil {
			c.Close()
			c = nil
		}
		if c != nil {
			log.G(ctx).Debug().Msg("using default buildkit socket")
		}
	}

	// Check if the docker buildkit host can be used
	if c == nil {
		var err error
		c, buildkitInfo, err = dockerBuildkit(ctx)
		if err != nil {
			return nil, nil, err
		}
		buildkitAddr = "<docker>"
		if c != nil {
			log.G(ctx).Debug().Msg("using docker buildkit")
		}
	}

	// If no other buildkits found, error out!
	if c == nil {
		return nil, nil, fmt.Errorf("buildkit not found; please start a docker or buildkit daemon, or set BUILDKIT_HOST")
	}

	log.G(ctx).
		Info().
		Str("addr", buildkitAddr).
		Str("version", buildkitInfo.BuildkitVersion.Version).
		Msg("found buildkit")
	return c, cleanup, nil
}

// see logic from https://github.com/docker/buildx/blob/master/driver/docker/driver.go
func dockerBuildkit(ctx context.Context) (*client.Client, *client.Info, error) {
	d, err := dockerclient.New(dockerclient.FromEnv)
	if err != nil {
		return nil, nil, nil
	}

	_, err = d.ServerVersion(ctx, dockerclient.ServerVersionOptions{})
	if err != nil {
		return nil, nil, nil
	}

	opts := []client.ClientOpt{
		client.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return d.DialHijack(ctx, "/grpc", "h2c", nil)
		}), client.WithSessionDialer(func(ctx context.Context, proto string, meta map[string][]string) (net.Conn, error) {
			return d.DialHijack(ctx, "/session", proto, meta)
		}),
	}

	c, err := client.New(ctx, "", opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("creating docker buildkit client: %w", err)
	}

	// NOTE: logic for detecting if the containerd snapshotter is enabled
	// var hasCtrdSnapshotter bool
	// workers, err := c.ListWorkers(ctx)
	// if err != nil {
	// 	return nil, nil, c.Close()
	// }
	// for _, w := range workers {
	// 	if _, ok := w.Labels["org.mobyproject.buildkit.worker.snapshotter"]; ok {
	// 		hasCtrdSnapshotter = true
	// 	}
	// }
	// if !hasCtrdSnapshotter {
	// 	return nil, nil, c.Close()
	// }

	info, err := c.Info(ctx)
	if err != nil {
		return nil, nil, c.Close()
	}
	return c, info, nil
}
