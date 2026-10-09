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
	Type         InstanceRolloutMode     `name:"type" json:"type,omitempty"`
	By           InstanceRolloutSelector `name:"by" json:"by,omitempty"`
	HealthyAfter types.DurationS         `name:"healthy-after" json:"healthy-after,omitempty"`
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

// InstanceRolloutSelector is the field of the new instance that selects the old set.
type InstanceRolloutSelector string

const (
	// RolloutByService selects the instances in the service group of the new one.
	RolloutByService InstanceRolloutSelector = "service"
	// RolloutByTags selects the instances that have every tag of the new one.
	RolloutByTags InstanceRolloutSelector = "tags"
)

// UnmarshalText reads a rollout selector and refuses an unknown one.
func (b *InstanceRolloutSelector) UnmarshalText(data []byte) error {
	switch by := InstanceRolloutSelector(data); by {
	case RolloutByService, RolloutByTags:
		*b = by
		return nil
	}
	return fmt.Errorf("unknown rollout by %q, want %q or %q", data, RolloutByService, RolloutByTags)
}

// rolloutPollInterval is how long a rollout leaves between two reads.
const rolloutPollInterval = 2 * time.Second

// RunResources creates the instances and, when asked for a rollout, replaces
// the instances the rollout selects with them.
func (c *InstanceCreateCmd) RunResources(ctx context.Context, stdio config.Stdio, partition *resource.Partition) ([]resource.Resource, error) {
	if c.Rollout == nil {
		return c.ResourceCreateCmd.RunResources(ctx, stdio, partition)
	}
	opts := *c.Rollout
	opts.Type = cmp.Or(opts.Type, RolloutRolling)
	opts.By = cmp.Or(opts.By, RolloutByService)

	fields, err := c.CreateFields(ctx)
	if err != nil {
		return nil, err
	}
	var metro string
	var svc *InstanceService
	var tags []string
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
		case "tags":
			tags = field.Create.Set.([]string)
		}
	}
	if replicas {
		return nil, fmt.Errorf("--replicas cannot be used with --rollout")
	}
	if !autostart {
		return nil, fmt.Errorf("--rollout requires --autostart")
	}
	switch opts.By {
	case RolloutByService:
		if svc == nil || (svc.Name == "" && svc.UUID == "") {
			return nil, fmt.Errorf("--rollout requires an existing service group")
		}
	case RolloutByTags:
		if len(tags) == 0 {
			return nil, fmt.Errorf("--rollout=by=tags requires --tag")
		}
	}

	if c.Save != "" {
		return c.ResourceCreateCmd.RunResources(ctx, stdio, partition)
	}
	return c.runRollout(ctx, stdio, partition, opts, fields, metro, svc, tags)
}

// runRollout replaces every instance the rollout selects with a new one, and
// deletes the old set once the new one runs.
func (c *InstanceCreateCmd) runRollout(ctx context.Context, stdio config.Stdio, partition *resource.Partition, opts InstanceRollout, fields []resource.Field, metro string, svc *InstanceService, tags []string) ([]resource.Resource, error) {
	target := strings.Join(tags, ",")
	if opts.By == RolloutByService {
		target = cmp.Or(svc.Name, svc.UUID)
	}
	groups, old, err := rolloutTarget(ctx, opts.By, metro, svc, tags)
	if err != nil {
		if !c.DryRun {
			return nil, err
		}
		log.G(ctx).Warn().Err(err).
			Msg("cannot read the instances to replace, replaced instances not shown")
	}
	for _, svcGroup := range groups {
		if svcGroup.Autoscale {
			return nil, fmt.Errorf("autoscaling enabled, can't rollout service group %q", cmp.Or(svcGroup.Name, svcGroup.UUID))
		}
	}
	if err == nil && len(old) == 0 {
		log.G(ctx).Info().
			Str(string(opts.By), target).
			Msg("no instances to replace, creating without a rollout")
		return c.ResourceCreateCmd.RunResources(ctx, stdio, partition)
	}
	oldKeys := make(multimetro.Keys, len(old))
	for i, inst := range old {
		oldKeys[i] = inst.key
	}
	count := int64(len(oldKeys))

	var running multimetro.Keys
	for _, inst := range old {
		if inst.State.IsRunning() {
			running = append(running, inst.key)
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
		Str(string(opts.By), target).
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

// rolloutTarget reads back the instances a rollout replaces, and the service
// groups that the old and the new instances are in.
func rolloutTarget(ctx context.Context, by InstanceRolloutSelector, metro string, svc *InstanceService, tags []string) ([]ServiceGroup, []Instance, error) {
	if by == RolloutByTags {
		if metro == "" {
			return nil, nil, fmt.Errorf("--rollout=by=tags requires --metro")
		}
		g, err := multimetro.NewClient(ctx)
		if err != nil {
			return nil, nil, err
		}
		profile, err := config.G(ctx).CurrentProfile()
		if err != nil {
			return nil, nil, err
		}
		old, err := group.CollectMetro(ctx, g, metro, func(ctx context.Context, c multimetro.MetroClient) ([]Instance, error) {
			resp, err := c.GetInstances(ctx, nil, platform.GetInstancesOpts{Details: new(true), Tags: tags})
			if err != nil {
				return nil, err
			}
			if resp == nil || resp.Data == nil {
				return nil, nil
			}
			var found []Instance
			for _, instance := range resp.Data.Instances {
				inst, err := Instance{}.load(nil, instance, &c.Metro, profile)
				if err != nil {
					return nil, err
				}
				missing := slices.ContainsFunc(tags, func(tag string) bool {
					return !slices.Contains(inst.Tags, tag)
				})
				if !missing {
					found = append(found, inst)
				}
			}
			return found, nil
		})
		if err != nil {
			return nil, nil, fmt.Errorf("listing the instances with tags %s: %w", strings.Join(tags, ","), err)
		}

		links := []*InstanceService{svc}
		for _, inst := range old {
			links = append(links, inst.Service)
		}
		var keys []string
		for _, link := range links {
			if link == nil || (link.Name == "" && link.UUID == "") {
				continue
			}
			keys = append(keys, serviceGroupKey(link, metro))
		}
		slices.Sort(keys)
		keys = slices.Compact(keys)
		if len(keys) == 0 {
			return nil, old, nil
		}
		results, err := (ServiceGroup{}).Get(ctx, keys)
		if err != nil {
			return nil, nil, fmt.Errorf("reading the service groups of the instances with tags %s: %w", strings.Join(tags, ","), err)
		}
		groups := make([]ServiceGroup, len(results))
		for i, r := range results {
			groups[i] = r.(ServiceGroup)
		}
		return groups, old, nil
	}

	key := serviceGroupKey(svc, metro)
	results, err := (ServiceGroup{}).Get(ctx, []string{key})
	if err != nil {
		return nil, nil, fmt.Errorf("looking up service group %q: %w", key, err)
	}
	if len(results) == 0 {
		return nil, nil, fmt.Errorf("service group %q not found", key)
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
		return []ServiceGroup{svcGroup}, nil, nil
	}
	results, err = (Instance{}).Get(ctx, keys.Strings())
	if err != nil {
		return nil, nil, fmt.Errorf("reading the instances of service group %q: %w", svcGroup.Name, err)
	}
	old := make([]Instance, len(results))
	for i, r := range results {
		old[i] = r.(Instance)
	}
	return []ServiceGroup{svcGroup}, old, nil
}

// serviceGroupKey keys the service group a link names, by its UUID when the
// link has one.
func serviceGroupKey(link *InstanceService, metro string) string {
	key := multimetro.Key{Metro: cmp.Or(link.Metro, metro), UUID: link.UUID}
	if link.UUID == "" {
		key.Name = link.Name
	}
	return key.Canonical()
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
