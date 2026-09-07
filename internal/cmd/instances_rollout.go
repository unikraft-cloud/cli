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
	"strings"
	"time"

	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/cli/internal/rollout"
	"unikraft.com/cli/internal/timeouts"
	"unikraft.com/cli/internal/types"
)

// InstanceRollout holds the options of a rollout.
type InstanceRollout struct {
	Type         InstanceRolloutMode `name:"type" json:"type,omitempty"`
	HealthyAfter types.DurationS     `name:"healthy-after" json:"healthy-after,omitempty"`
}

// UnmarshalText reads the rollout options. A mode alone sets the type.
func (r *InstanceRollout) UnmarshalText(data []byte) error {
	str := strings.TrimSpace(string(data))
	if str == "" {
		return nil
	}
	var mode InstanceRolloutMode
	if mode.UnmarshalText([]byte(str)) == nil {
		r.Type = mode
		return nil
	}

	type rolloutAlias InstanceRollout
	parsed, err := value.Parse[rolloutAlias]([]string{str})
	if err != nil {
		return err
	}
	if parsed.HealthyAfter < 0 {
		return fmt.Errorf("rollout healthy-after cannot be negative")
	}
	*r = InstanceRollout(parsed)
	return nil
}

// InstanceRolloutMode is how a rollout swaps the old instances for the new.
type InstanceRolloutMode string

const (
	// RolloutRolling means every new instance comes up before any old one goes.
	RolloutRolling InstanceRolloutMode = "rolling"
	// RolloutReplace means the old instances stop before the new ones start.
	RolloutReplace InstanceRolloutMode = "replace"
)

// UnmarshalText reads a rollout type and refuses an unknown one.
func (m *InstanceRolloutMode) UnmarshalText(data []byte) error {
	switch mode := InstanceRolloutMode(data); mode {
	case RolloutRolling, RolloutReplace:
		*m = mode
		return nil
	}
	return fmt.Errorf("unknown rollout type %q, want %q or %q", data, RolloutRolling, RolloutReplace)
}

// rolloutPollInterval is how long a rollout leaves between two reads.
const rolloutPollInterval = 2 * time.Second

// RunResources creates the instances and, when asked for a rollout, replaces
// the instances already in the service group with them.
func (c *InstanceCreateCmd) RunResources(ctx context.Context, stdio config.Stdio, partition *resource.Partition) ([]resource.Resource, error) {
	if c.Rollout == nil {
		return c.ResourceCreateCmd.RunResources(ctx, stdio, partition)
	}
	opts := *c.Rollout
	opts.Type = cmp.Or(opts.Type, RolloutRolling)

	fields, err := c.CreateFields(ctx)
	if err != nil {
		return nil, err
	}
	var metro string
	var svc *InstanceService
	replicas, autostart := false, false
	for key, field := range resource.IterFields(fields) {
		if field.Create == nil || field.Create.Set == nil {
			continue
		}
		switch key.String() {
		case "metro":
			metro = string(field.Create.Set.(LinkName[Metro]))
		case "replicas":
			replicas = true
		case "autostart":
			autostart = field.Create.Set.(bool)
		case "service":
			svc = field.Create.Set.(*InstanceService)
		}
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

	if c.Save != "" {
		return c.ResourceCreateCmd.RunResources(ctx, stdio, partition)
	}
	return c.runServiceRollout(ctx, stdio, partition, opts, fields, metro, svc)
}

// runServiceRollout replaces every instance in the target service group with
// a new one, and deletes the old set once the new one runs.
func (c *InstanceCreateCmd) runServiceRollout(ctx context.Context, stdio config.Stdio, partition *resource.Partition, opts InstanceRollout, fields []resource.Field, metro string, svc *InstanceService) ([]resource.Resource, error) {
	svcGroup, oldKeys, err := rolloutTarget(ctx, metro, svc)
	if err != nil {
		if !c.DryRun {
			return nil, err
		}
		log.G(ctx).Warn().Err(err).
			Msg("cannot read the service group, replaced instances not shown")
	} else if svcGroup.Autoscale {
		return nil, fmt.Errorf("autoscaling enabled, can't rollout service group %q", cmp.Or(svcGroup.Name, svcGroup.UUID))
	}
	if err == nil && len(oldKeys) == 0 {
		log.G(ctx).Info().
			Str("service", svcGroup.Name).
			Msg("service group is empty, creating without a rollout")
		return c.ResourceCreateCmd.RunResources(ctx, stdio, partition)
	}
	count := int64(len(oldKeys))

	var running multimetro.Keys
	if opts.Type == RolloutReplace && !c.DryRun {
		results, err := (Instance{}).Get(ctx, oldKeys.Strings())
		if err != nil {
			return nil, fmt.Errorf("reading the instances of service group %q: %w", svcGroup.Name, err)
		}
		for _, r := range results {
			if inst := r.(Instance); inst.State.IsRunning() {
				running = append(running, inst.key)
			}
		}
	}

	healthyAfter := time.Duration(opts.HealthyAfter) * time.Second
	var strategy rollout.Strategy = rollout.Rolling{HealthyAfter: healthyAfter}
	if opts.Type == RolloutReplace {
		strategy = rollout.Replace{HealthyAfter: healthyAfter}
		for key, field := range resource.IterFields(fields) {
			if key.String() == "autostart" && field.Create != nil {
				field.Create.Set = false
			}
		}
	}
	if count > 1 {
		fields = append(fields, resource.Field{Name: "replicas", Create: &resource.Patch{Set: count - 1}})
	}
	if err := c.SetCreateFields(ctx, fields); err != nil {
		return nil, err
	}

	if c.DryRun {
		if _, err := c.ResourceCreateCmd.RunResources(ctx, stdio, partition); err != nil {
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
		Int64("instances", count).
		Msg("rolling out")

	quiet := stdio
	quiet.Stdout = io.Discard
	created := &instanceSet{g: g, partition: partition}
	done, err := strategy.Rollout(ctx,
		&instanceSet{g: g, partition: partition, keys: oldKeys, running: running},
		func(ctx context.Context) (rollout.Set, error) {
			res, err := c.ResourceCreateCmd.RunResources(ctx, quiet, partition)
			for _, r := range res {
				created.keys = append(created.keys, r.(Instance).key)
			}
			created.running = created.keys
			if err == nil && int64(len(res)) < count {
				err = fmt.Errorf("rollout created %d of %d instances", len(res), count)
			}
			return created, err
		})
	if !done {
		return nil, err
	}
	printed := make([]resource.Resource, len(created.last))
	for i, inst := range created.last {
		printed[i] = inst
	}
	if printErr := c.Output.WithDefault(cmd.PrinterTypeKeyValue).Print(ctx, stdio.Stdout, c.Field, Instance{}, printed...); printErr != nil {
		return printed, errors.Join(err, printErr)
	}
	return printed, err
}

// rolloutTarget reads back the service group whose instances a rollout
// replaces, with a key for each of them.
func rolloutTarget(ctx context.Context, metro string, svc *InstanceService) (ServiceGroup, multimetro.Keys, error) {
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

	old := make(multimetro.Keys, 0, len(svcGroup.Instances))
	for _, inst := range svcGroup.Instances {
		key := multimetro.Key{
			Metro: cmp.Or(inst.Metro, string(svcGroup.Metro)),
			Name:  inst.Name,
			UUID:  inst.UUID,
		}
		if key.Name == "" && key.UUID == "" {
			continue
		}
		old = append(old, key)
	}
	return svcGroup, old, nil
}

// instanceSet is a set of instances that a rollout acts on. Start and Up act
// only on the running instances.
type instanceSet struct {
	g         *group.Group[multimetro.MetroClient]
	partition *resource.Partition
	keys      multimetro.Keys
	running   multimetro.Keys
	seen      map[string]bool
	restarts  map[string]int
	last      []Instance
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
	if _, err := stopInstances(ctx, s.g, s.keys, StopOpts{DrainTimeout: -1}); err != nil {
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
			if _, err := timeouts.Tolerate(ctx, resp, err, "instances did not reach the state in time"); err != nil {
				return nil, err
			}
			if resp == nil || resp.Data == nil {
				return nil, nil
			}
			var waited group.Refs
			for _, instance := range resp.Data.Instances {
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

	pending := s.keys
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
	for {
		results, err := (Instance{}).Get(ctx, s.running.Strings())
		notFound, ok := errors.AsType[group.ErrRefNotFound](err)
		if err != nil && !ok {
			return err
		}
		s.last = make([]Instance, 0, len(results))
		var pending, failed []error
		for _, r := range results {
			inst := r.(Instance)
			s.seen[inst.UUID] = true
			if inst.Name != "" {
				s.seen[inst.Name] = true
			}
			s.last = append(s.last, inst)
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
	for _, inst := range s.last {
		if count, ok := s.restarts[inst.UUID]; ok && inst.Restart.RestartCount > count {
			restarted = append(restarted, fmt.Errorf("instance %s restarted", cmp.Or(inst.Name, inst.UUID)))
		}
	}
	s.restarts = make(map[string]int, len(s.last))
	for _, inst := range s.last {
		s.restarts[inst.UUID] = inst.Restart.RestartCount
	}
	return errors.Join(restarted...)
}

func (s *instanceSet) Delete(ctx context.Context) error {
	if len(s.keys) == 0 {
		return nil
	}
	err := s.partition.WrapDeletable(Instance{}).Delete(ctx, s.keys.Strings())
	if err == nil {
		return nil
	}
	var left []string
	results, getErr := (Instance{}).Get(ctx, s.keys.Strings())
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
