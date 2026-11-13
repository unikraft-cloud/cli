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

// instanceLogs merges the logs of instances. Only Copy starts to watch them,
// thus start and restart can make instanceLogs before the instances boot.
type instanceLogs struct {
	mux      *muxreader.Mux
	ctx      context.Context
	cancel   context.CancelFunc
	follow   bool
	followed []*followedInstance
}

// followedInstance is an instance whose logs end after it stops.
type followedInstance struct {
	key     multimetro.Key
	r       *logs.FollowReader
	stopped atomic.Pointer[Instance]
}

func newInstanceLogs(ctx context.Context, keys multimetro.Keys, tail *int, follow, prefix bool) (*instanceLogs, error) {
	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	mux := muxreader.New()
	if !prefix {
		mux.DisablePrefix()
	}

	ctx, cancel := context.WithCancel(ctx)
	l := &instanceLogs{mux: mux, ctx: ctx, cancel: cancel, follow: follow}

	var mu sync.Mutex
	err = group.DoRefs(ctx, g, keys.Refs(), func(_ context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
		for _, ref := range refs {
			r, err := logs.InstanceLogs(ctx, c).Reader(ref.NameOrUUID(), tail, follow)
			if err != nil {
				return nil, err
			}
			var end func(error) error
			if follow {
				f := &followedInstance{key: multimetro.Key(ref), r: r}
				f.key.Metro = c.Metro.Name
				mu.Lock()
				l.followed = append(l.followed, f)
				mu.Unlock()
				end = func(err error) error {
					if err != nil {
						if ctx.Err() != nil {
							return err
						}
						log.G(ctx).Warn().Err(err).Str("instance", f.key.String()).Msg("stopped following the logs")
						return nil
					}
					inst := f.stopped.Load()
					if inst == nil {
						return nil
					}
					event := log.G(ctx).Info().Str("instance", f.key.String())
					if inst.Stop.Reason != "" {
						event = event.Str("reason", inst.Stop.Reason)
					}
					if inst.Stop.ExitCode != nil {
						event = event.Uint32("exit-code", *inst.Stop.ExitCode)
					}
					event.Msg("instance stopped")
					return nil
				}
			}
			mux.With(multimetro.Key(ref).String(), r, end)
		}
		return refs, nil
	})
	if err != nil {
		l.Close()
		return nil, err
	}
	mux.Seal()

	return l, nil
}

// Copy writes the logs to w until they end. When it follows the logs, the
// logs of each instance end after the instance stops.
func (l *instanceLogs) Copy(w io.Writer) error {
	if l.follow {
		go l.watch()
	}
	_, err := io.Copy(w, l.mux)
	if l.follow && errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// Close stops the logs of all instances.
func (l *instanceLogs) Close() {
	l.cancel()
	l.mux.Close()
}

// watch ends the logs of each instance after it is deleted, or after it stops
// and the platform has no restart of it scheduled.
func (l *instanceLogs) watch() {
	pending := l.followed
	for len(pending) > 0 {
		keys := make([]string, len(pending))
		for i, f := range pending {
			keys[i] = f.key.Canonical()
		}
		found, err := Instance{}.Get(l.ctx, keys)
		if err != nil {
			log.G(l.ctx).Debug().Err(err).Msg("could not get the followed instances")
		}

		var next []*followedInstance
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
					f.r.StopFollowing()
					continue
				}
			}
			next = append(next, f)
		}
		pending = next

		select {
		case <-l.ctx.Done():
			return
		case <-time.After(statePollInterval):
		}
	}
}

// instanceKeys returns the keys of instances, with their metro set.
func instanceKeys(instances []model.Model) multimetro.Keys {
	keys := make(multimetro.Keys, 0, len(instances))
	for _, m := range instances {
		keys = append(keys, m.Key().(multimetro.Key))
	}
	return keys
}
