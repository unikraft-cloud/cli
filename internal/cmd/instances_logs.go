// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"errors"
	"io"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/cloud/sdk/platform/logs"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/muxreader"
	"unikraft.com/cli/pkg/model"
)

// statePollInterval is the time between two state checks of followed instances.
const statePollInterval = 2 * time.Second

// errLogsEnd tells a log reader that no more logs come.
var errLogsEnd = errors.New("no more logs")

// newInstanceLogMux merges the logs of instances. Run watch after the
// instances start, so that the logs of each instance end after it stops.
func newInstanceLogMux(ctx context.Context, keys multimetro.Keys, tail *int, follow, prefix bool) (*muxreader.Mux, func(), context.CancelFunc, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	mux := muxreader.New()
	if !prefix {
		mux.DisablePrefix()
	}

	ctx, cancel := context.WithCancel(ctx)

	var (
		mu        sync.Mutex
		followers []*logFollower
	)
	err = group.DoRefs(ctx, g, keys.Refs(), func(_ context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		for _, ref := range refs {
			var f *logFollower
			var client platform.Client = c
			if follow {
				f = &logFollower{Client: c, ctx: ctx, key: multimetro.Key(ref)}
				f.key.Metro = c.Metro.Name
				client = f
			}
			r, err := logs.InstanceLogs(ctx, client).Reader(ref.NameOrUUID(), tail, follow)
			if err != nil {
				return nil, err
			}
			if f != nil {
				f.r = r
				r = f
				mu.Lock()
				followers = append(followers, f)
				mu.Unlock()
			}
			mux.With(multimetro.Key(ref).String(), r)
		}
		return refs, nil
	})
	if err != nil {
		cancel()
		mux.Close()
		return nil, nil, nil, err
	}
	mux.Seal()

	return mux, func() { watchInstances(ctx, followers) }, cancel, nil
}

// instanceKeys returns the keys of instances, with their metro set.
func instanceKeys(instances []model.Model) multimetro.Keys {
	keys := make(multimetro.Keys, 0, len(instances))
	for _, m := range instances {
		keys = append(keys, m.Key().(multimetro.Key))
	}
	return keys
}

// watchInstances gives each follower its instance after the instance is
// deleted, or after it stops and the platform has no restart of it scheduled.
func watchInstances(ctx context.Context, pending []*logFollower) {
	for len(pending) > 0 {
		keys := make([]string, len(pending))
		for i, f := range pending {
			keys[i] = f.key.Canonical()
		}
		found, err := Instance{}.Get(ctx, keys)
		if err != nil {
			log.G(ctx).Debug().Err(err).Msg("could not get the followed instances")
		}

		var next []*logFollower
		for _, f := range pending {
			i := slices.IndexFunc(found, func(m model.Model) bool {
				inst, ok := m.(Instance)
				return ok && inst.key.Metro == f.key.Metro && (inst.Name == f.key.Name || inst.UUID == f.key.UUID)
			})
			if i >= 0 {
				inst := found[i].(Instance)
				state := platform.InstanceState(inst.State)
				scheduled := inst.Instance.Restart != nil && inst.Instance.Restart.NextAt != nil
				if state == platform.InstanceStateDeleted || state == platform.InstanceStateStopped && !scheduled {
					f.stopped.Store(&inst)
					continue
				}
			}
			next = append(next, f)
		}
		pending = next

		select {
		case <-ctx.Done():
			return
		case <-time.After(statePollInterval):
		}
	}
}

// logFollower follows the logs of one instance. After watch gives it the
// stopped instance, the first log request without new logs ends the logs.
type logFollower struct {
	platform.Client
	ctx     context.Context
	key     multimetro.Key
	r       io.Reader
	stopped atomic.Pointer[Instance]
	partial bool
}

// GetInstanceLogs reads the stop state before the request, so that an empty
// response after the stop is the end of the logs.
func (f *logFollower) GetInstanceLogs(ctx context.Context, request []platform.GetInstancesLogsRequestItem, opts platform.GetInstanceLogsOpts) (*platform.Response[platform.GetInstancesLogsResponseData], error) {
	stopped := f.stopped.Load() != nil
	resp, err := f.Client.GetInstanceLogs(ctx, request, opts)
	if !stopped || err != nil {
		return resp, err
	}
	if resp == nil || resp.Data == nil || len(resp.Data.Instances) == 0 || resp.Data.Instances[0].Output == "" {
		return nil, errLogsEnd
	}
	return resp, nil
}

// Read ends the logs after the instance stops or its logs are deleted. It
// ends an incomplete last line first, so that the stop notice comes last.
func (f *logFollower) Read(p []byte) (int, error) {
	n, err := f.r.Read(p)
	if n > 0 {
		f.partial = p[n-1] != '\n'
	}
	if err == nil || (!errors.Is(err, errLogsEnd) && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound)) {
		return n, err
	}
	if f.partial {
		f.partial = false
		return n + copy(p[n:], "\n"), nil
	}

	if inst := f.stopped.Load(); inst != nil {
		event := log.G(f.ctx).Info().Str("instance", f.key.String())
		if inst.Stop.Reason != "" {
			event = event.Str("reason", inst.Stop.Reason)
		}
		if inst.Stop.ExitCode != nil {
			event = event.Uint32("exit-code", *inst.Stop.ExitCode)
		}
		event.Msg("instance stopped")
	}
	return n, io.EOF
}
