// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/cloud/sdk/platform/timeouts"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/model"
	"unikraft.com/cli/internal/model/cmd"
	"unikraft.com/cli/internal/model/value"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/rollout"
)

// InstanceRollout holds the options of a rollout.
type InstanceRollout struct {
	Type  InstanceRolloutMode `name:"type" json:"type,omitempty"`
	Batch int                 `name:"batch" json:"batch,omitempty"`
}

// UnmarshalText reads the rollout options as <key>=<value> pairs or as JSON.
func (r *InstanceRollout) UnmarshalText(data []byte) error {
	type rolloutAlias InstanceRollout
	parsed, err := value.Parse[rolloutAlias]([]string{string(data)})
	if err != nil {
		return err
	}
	if parsed.Batch < 0 {
		return fmt.Errorf("rollout batch cannot be negative")
	}
	*r = InstanceRollout(parsed)
	return nil
}

// InstanceRolloutMode is how a rollout swaps the old instances for the new.
type InstanceRolloutMode string

const (
	// RolloutRolling means the new instances replace the old ones a batch at a time.
	RolloutRolling InstanceRolloutMode = "rolling"
)

// UnmarshalText reads a rollout type and refuses an unknown one.
func (m *InstanceRolloutMode) UnmarshalText(data []byte) error {
	if mode := InstanceRolloutMode(data); mode == RolloutRolling {
		*m = mode
		return nil
	}
	return fmt.Errorf("unknown rollout type %q, want %q", data, RolloutRolling)
}

// rolloutPollInterval is how long a rollout leaves between two reads.
const rolloutPollInterval = 2 * time.Second

// runRollout replaces every instance in the service group with a new one, and
// deletes the old set once the new one runs.
func (c *InstanceCreateCmd) runRollout(ctx context.Context, stdio config.Stdio, partition *model.Partition) ([]model.Model, error) {
	opts := *c.Rollout
	opts.Type = cmp.Or(opts.Type, RolloutRolling)
	opts.Batch = cmp.Or(opts.Batch, 1)

	fields, err := c.CreateFields(ctx)
	if err != nil {
		return nil, err
	}
	var metro string
	var svc *InstanceService
	named, replicas, autostart := false, false, false
	for key, field := range model.IterFields(fields) {
		if field.Create == nil || field.Create.Set == nil {
			continue
		}
		switch key.String() {
		case "metro":
			metro = string(field.Create.Set.(LinkName[Metro]))
		case "name":
			named = true
		case "replicas":
			replicas = true
		case "autostart":
			autostart = field.Create.Set.(bool)
		case "service":
			svc = field.Create.Set.(*InstanceService)
		}
	}
	if named {
		return nil, fmt.Errorf("--name cannot be used with --rollout")
	}
	if replicas {
		return nil, fmt.Errorf("--replicas cannot be used with --rollout")
	}
	if !autostart {
		return nil, fmt.Errorf("--rollout requires --autostart")
	}
	if svc == nil || (svc.Name == "" && svc.UUID == "") {
		return nil, fmt.Errorf("--rollout requires an existing service group")
	}

	svcGroup, old, err := rolloutTargetService(ctx, metro, svc)
	if err != nil {
		if !c.DryRun {
			return nil, err
		}
		log.G(ctx).Warn().Err(err).
			Msg("cannot read the service group, replaced instances not shown")
	} else if svcGroup.Autoscale {
		return nil, fmt.Errorf("autoscaling enabled, can't rollout service group %q", cmp.Or(svcGroup.Name, svcGroup.UUID))
	}
	old = slices.DeleteFunc(old, func(inst Instance) bool { return partition.Missing(inst) })
	if err == nil && len(old) == 0 {
		log.G(ctx).Info().
			Str("service", svcGroup.Name).
			Msg("no instances to replace, creating without a rollout")
		return c.Create(ctx, stdio, partition, fields)
	}
	oldKeys := make(multimetro.Keys, len(old))
	for i, inst := range old {
		oldKeys[i] = inst.key
	}

	var running multimetro.Keys
	for _, inst := range old {
		if inst.State.IsRunning() {
			running = append(running, inst.key)
		}
	}

	var strategy rollout.Strategy = rollout.Rolling{Batch: opts.Batch}
	first := min(opts.Batch, len(oldKeys))
	fieldsFor := func(n int) []model.Field {
		batch := slices.Clone(fields)
		for i, field := range batch {
			if field.Name == "autostart" && field.Create != nil {
				create := *field.Create
				create.Set = false
				batch[i].Create = &create
			}
		}
		if n > 1 {
			batch = append(batch, model.Field{Name: "replicas", Create: &model.Patch{Set: int64(n - 1)}})
		}
		return batch
	}

	if c.DryRun {
		if _, err := c.Create(ctx, stdio, partition, fieldsFor(first)); err != nil {
			return nil, err
		}
		if len(oldKeys) > 0 {
			fmt.Fprintf(stdio.Stdout, "replaces := %s\n", strings.Join(oldKeys.Strings(), " "))
		}
		return nil, nil
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	log.G(ctx).Info().
		Str("service", svcGroup.Name).
		Str("mode", string(opts.Type)).
		Int("instances", len(oldKeys)).
		Msg("rolling out")

	quiet := stdio
	quiet.Stdout = io.Discard
	var created []*instanceSet
	done, err := strategy.Apply(ctx,
		&instanceSet{g: g, partition: partition, keys: oldKeys, running: running},
		func(ctx context.Context, replaces rollout.Set) (rollout.Set, error) {
			old := replaces.(*instanceSet)
			n := len(old.keys)
			res, err := c.Create(ctx, quiet, partition, fieldsFor(n))
			set := &instanceSet{g: g, partition: partition}
			for _, r := range res {
				set.keys = append(set.keys, r.(Instance).key)
			}
			set.running = set.keys[:min(len(old.running), len(set.keys))]
			created = append(created, set)
			if err == nil && len(res) < n {
				err = fmt.Errorf("rollout created %d of %d instances", len(res), n)
			}
			return set, err
		})
	if !done {
		return nil, err
	}
	var keys multimetro.Keys
	for _, set := range created {
		keys = append(keys, set.keys...)
	}
	results, readErr := (Instance{}).Get(ctx, keys.ByUUID())
	err = errors.Join(err, readErr)
	printed := make([]model.Model, 0, len(results))
	for _, r := range results {
		inst := r.(Instance)
		inst.key.Display = ""
		printed = append(printed, inst)
	}
	if printErr := c.Output.WithDefault(cmd.PrinterTypeKeyValue).Print(ctx, stdio.Stdout, c.Field, Instance{}, printed...); printErr != nil {
		return printed, errors.Join(err, printErr)
	}
	return printed, err
}

// rolloutTargetService reads back the service group whose instances a rollout
// replaces, and those instances.
func rolloutTargetService(ctx context.Context, metro string, svc *InstanceService) (ServiceGroup, []Instance, error) {
	var none ServiceGroup
	key := multimetro.Key{Metro: cmp.Or(svc.Metro, metro), UUID: svc.UUID}
	if svc.UUID == "" {
		key.Name = svc.Name
	}
	results, err := (ServiceGroup{}).Get(ctx, []string{key.Canonical()})
	if err != nil {
		return none, nil, fmt.Errorf("looking up service group %q: %w", key.Canonical(), err)
	}
	if len(results) == 0 {
		return none, nil, fmt.Errorf("service group %q not found", key.Canonical())
	}
	svcGroup := results[0].(ServiceGroup)

	keys := make(multimetro.Keys, 0, len(svcGroup.Instances))
	for _, inst := range svcGroup.Instances {
		key := multimetro.Key{
			Metro: cmp.Or(inst.Metro, string(svcGroup.Metro)),
			Name:  inst.Name,
			UUID:  inst.UUID,
		}
		if key.Name == "" && key.UUID == "" {
			continue
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return svcGroup, nil, nil
	}
	results, err = (Instance{}).Get(ctx, keys.Strings())
	if err != nil {
		return none, nil, fmt.Errorf("reading the instances of service group %q: %w", svcGroup.Name, err)
	}
	old := make([]Instance, len(results))
	for i, r := range results {
		old[i] = r.(Instance)
	}
	return svcGroup, old, nil
}

// instanceSet is a set of instances that a rollout acts on. Start and Up act
// only on the running instances, and Stop counts a gone instance as stopped.
type instanceSet struct {
	g         *group.Group[multimetro.MetroClient]
	partition *model.Partition
	keys      multimetro.Keys
	running   multimetro.Keys
	seen      map[string]bool
	restarts  map[string]int
}

func (s *instanceSet) Len() int {
	return len(s.keys)
}

func (s *instanceSet) Take(n int) (rollout.Set, rollout.Set) {
	n = min(n, len(s.keys))
	batch := &instanceSet{g: s.g, partition: s.partition, keys: s.keys[:n:n]}
	rest := &instanceSet{g: s.g, partition: s.partition, keys: s.keys[n:]}
	for _, key := range s.running {
		if slices.Contains(batch.keys, key) {
			batch.running = append(batch.running, key)
		} else {
			rest.running = append(rest.running, key)
		}
	}
	return batch, rest
}

func (s *instanceSet) Names() []string {
	return s.keys.Strings()
}

func (s *instanceSet) Start(ctx context.Context) error {
	if len(s.running) == 0 {
		return nil
	}
	_, err := startInstances(ctx, s.g, s.running)
	return err
}

func (s *instanceSet) Stop(ctx context.Context) error {
	pending := s.keys
	_, err := stopInstances(ctx, s.g, s.keys, StopOpts{DrainTimeout: -1})
	if notFound, ok := errors.AsType[group.ErrRefNotFound](err); ok {
		log.G(ctx).Warn().
			Strs("instances", notFound.Refs.Strings()).
			Msg("the instances are gone, so they count as stopped")
		pending = withoutRefs(pending, notFound.Refs)
	} else if err != nil {
		return err
	}
	state := platform.InstanceStateStopped
	ask := func(ctx context.Context, keys multimetro.Keys) error {
		return group.DoRefs(ctx, s.g, keys.Refs(), func(ctx context.Context, c multimetro.MetroClient, refs group.Refs) (group.Refs, error) {
			log.G(ctx).Trace().Str("state", string(state)).Msg("waiting for instances")
			reqs := make([]platform.WaitInstancesRequestItem, 0, len(refs))
			for _, ref := range refs.NameOrUUIDs() {
				reqs = append(reqs, platform.WaitInstancesRequestItem{
					Name:     ref.Name,
					Uuid:     ref.Uuid,
					State:    &state,
					TimeoutS: new(int64(-1)),
				})
			}
			resp, err := timeouts.TryWithFallback(ctx, reqs, func(ctx context.Context, reqs []platform.WaitInstancesRequestItem) (*platform.Response[platform.WaitInstancesResponseData], error) {
				return c.WaitInstances(ctx, reqs, platform.WaitInstancesOpts{})
			})
			if _, err := timeouts.Tolerate(ctx, resp, err, "instances did not reach the state in time"); err != nil && !platform.ErrorContainsOnly(err, platform.APIHTTPErrorNotFound) {
				return nil, err
			}
			if resp == nil || resp.Data == nil {
				return nil, nil
			}
			var waited group.Refs
			for _, instance := range resp.Data.Instances {
				if instance.Error != nil && platform.APIHTTPError(*instance.Error) == platform.APIHTTPErrorNotFound {
					i := slices.IndexFunc(refs, func(ref group.Ref) bool {
						return cmp.Or(ref.UUID, ref.Name) == cmp.Or(instance.Uuid, instance.Name)
					})
					if i >= 0 {
						log.G(ctx).Warn().
							Str("instance", refs[i].String()).
							Msg("the instance is gone, so it counts as stopped")
						waited = append(waited, refs[i])
					}
					continue
				}
				if instance.State != state {
					log.G(ctx).Trace().
						Str("instance", cmp.Or(instance.Name, instance.Uuid)).
						Str("state", string(instance.State)).
						Msgf("instance is not %s yet", state)
					continue
				}
				waited = append(waited, group.Ref{
					Metro: c.Metro.Name,
					Name:  instance.Name,
					UUID:  instance.Uuid,
				})
			}
			return waited, nil
		})
	}

	for {
		err := ask(ctx, pending)
		notFound, ok := errors.AsType[group.ErrRefNotFound](err)
		if err == nil || !ok {
			return err
		}
		pending = make(multimetro.Keys, 0, len(notFound.Refs))
		for _, ref := range notFound.Refs {
			pending = append(pending, multimetro.Key(ref))
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(rolloutPollInterval):
		}
	}
}

func (s *instanceSet) Up(ctx context.Context) error {
	if len(s.running) == 0 {
		return nil
	}
	if s.seen == nil {
		s.seen = make(map[string]bool, len(s.running))
	}
	var last []Instance
	for {
		results, err := (Instance{}).Get(ctx, s.running.ByUUID())
		notFound, ok := errors.AsType[group.ErrRefNotFound](err)
		if err != nil && !ok {
			return err
		}
		last = make([]Instance, 0, len(results))
		var pending, failed []error
		for _, r := range results {
			inst := r.(Instance)
			s.seen[inst.UUID] = true
			if inst.Name != "" {
				s.seen[inst.Name] = true
			}
			last = append(last, inst)
			name := cmp.Or(inst.Name, inst.UUID)
			switch platform.InstanceState(inst.State) {
			case platform.InstanceStateRunning, platform.InstanceStateStandby:
			case platform.InstanceStateStarting:
				pending = append(pending, fmt.Errorf("instance %s is %s", name, inst.State))
			default:
				failed = append(failed, fmt.Errorf("instance %s is %s, not running", name, inst.State))
			}
		}
		for _, ref := range notFound.Refs {
			name := cmp.Or(ref.Name, ref.UUID)
			if (ref.UUID != "" && s.seen[ref.UUID]) || (ref.Name != "" && s.seen[ref.Name]) {
				failed = append(failed, fmt.Errorf("instance %s was deleted", name))
				continue
			}
			pending = append(pending, fmt.Errorf("instance %s is not listed yet", name))
		}
		if len(failed) > 0 {
			return errors.Join(failed...)
		}
		if len(pending) == 0 {
			break
		}
		select {
		case <-ctx.Done():
			return errors.Join(pending...)
		case <-time.After(rolloutPollInterval):
		}
	}

	var restarted []error
	for _, inst := range last {
		if count, ok := s.restarts[inst.UUID]; ok && inst.Restart.RestartCount > count {
			restarted = append(restarted, fmt.Errorf("instance %s restarted", cmp.Or(inst.Name, inst.UUID)))
		}
	}
	s.restarts = make(map[string]int, len(last))
	for _, inst := range last {
		s.restarts[inst.UUID] = inst.Restart.RestartCount
	}
	return errors.Join(restarted...)
}

func (s *instanceSet) Delete(ctx context.Context) error {
	if len(s.keys) == 0 {
		return nil
	}
	err := s.partition.WrapDeletable(Instance{}).Delete(ctx, s.keys.ByUUID())
	if err == nil {
		return nil
	}
	var left []string
	results, getErr := (Instance{}).Get(ctx, s.keys.ByUUID())
	if _, ok := errors.AsType[group.ErrRefNotFound](getErr); getErr == nil || ok {
		for _, r := range results {
			inst := r.(Instance)
			left = append(left, cmp.Or(inst.Name, inst.UUID))
		}
	} else {
		left = s.keys.Strings()
	}
	if len(left) == 0 {
		log.G(ctx).Warn().Err(err).
			Msg("the instances are gone, but their delete reported an error")
		return nil
	}
	return fmt.Errorf("%s still exist: %w", strings.Join(left, " "), err)
}

// withoutRefs returns the keys that none of refs names.
func withoutRefs(keys multimetro.Keys, refs group.Refs) multimetro.Keys {
	return slices.DeleteFunc(slices.Clone(keys), func(key multimetro.Key) bool {
		return slices.ContainsFunc(refs, func(ref group.Ref) bool {
			return cmp.Or(ref.UUID, ref.Name) == cmp.Or(key.UUID, key.Name)
		})
	})
}
