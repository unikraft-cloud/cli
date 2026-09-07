// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

// Package rollout swaps an old set of instances for a new one.
package rollout

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"unikraft.com/x/log"
)

// stepTimeout bounds each step of a rollout, so a stuck instance cannot hang
// it.
const stepTimeout = 900 * time.Second

// Set is a group of instances that a rollout acts on as one.
type Set interface {
	// Len returns the number of instances.
	Len() int
	// Take splits the first n instances off the set.
	Take(n int) (batch, rest Set)
	// Names returns the names of the instances.
	Names() []string
	// Start starts the instances that ran before the rollout.
	Start(ctx context.Context) error
	// Stop stops the instances and waits until they stop.
	Stop(ctx context.Context) error
	// Up waits until the instances run. It fails when an instance restarted
	// or was deleted after the last Up.
	Up(ctx context.Context) error
	// Delete deletes the instances.
	Delete(ctx context.Context) error
}

// CreateFunc makes a new set of n instances that do not run yet.
type CreateFunc func(ctx context.Context, n int) (Set, error)

// Strategy swaps the old set of instances for new sets that create makes.
type Strategy interface {
	// Apply reports done when the new sets take the place of the old one.
	// When a step before that fails, it undoes the steps before it.
	Apply(ctx context.Context, old Set, create CreateFunc) (done bool, err error)
}

// apply replaces old in batches. With stopFirst, each old batch stops before
// its new batch starts, and else after the new batch runs.
func apply(ctx context.Context, old Set, create CreateFunc, batch int, stopFirst bool, healthyAfter time.Duration) (bool, error) {
	p := &progress{old: old, stopFirst: stopFirst}
	for rest := old; rest.Len() > 0; {
		var next Set
		next, rest = rest.Take(batch)

		var created Set
		err := within(context.WithoutCancel(ctx), func(ctx context.Context) error {
			var err error
			created, err = create(ctx, next.Len())
			return err
		})
		if created != nil {
			p.news = append(p.news, created)
		}
		if err != nil {
			return false, p.undo(ctx, err)
		}
		if ctx.Err() != nil {
			return false, p.undo(ctx, fmt.Errorf("rollout interrupted while the new instances were created: %s", ctx.Err()))
		}

		if stopFirst {
			if err := p.stop(ctx, next); err != nil {
				return false, p.undo(ctx, err)
			}
		}
		p.started = len(p.news)
		if err := within(ctx, created.Start); err != nil {
			return false, p.undo(ctx, fmt.Errorf("starting the new instances: %w", err))
		}
		if err := within(ctx, created.Up); err != nil {
			return false, p.undo(ctx, fmt.Errorf("the new instances did not come up: %w", err))
		}
		if healthyAfter > 0 {
			log.G(ctx).Info().
				Str("healthy-after", healthyAfter.String()).
				Msg("keeping the new instances up before the rollout goes on")
			select {
			case <-ctx.Done():
				return false, p.undo(ctx, fmt.Errorf("rollout interrupted while the new instances were up: %s", ctx.Err()))
			case <-time.After(healthyAfter):
			}
			if err := within(ctx, created.Up); err != nil {
				return false, p.undo(ctx, fmt.Errorf("the new instances did not stay up: %w", err))
			}
		}
		if !stopFirst {
			if err := p.stop(ctx, next); err != nil {
				return false, p.undo(ctx, err)
			}
		}
	}

	if err := within(context.WithoutCancel(ctx), old.Delete); err != nil {
		return true, fmt.Errorf("the new instances are up, but deleting the replaced ones failed: %w", err)
	}
	log.G(ctx).Info().
		Strs("instances", old.Names()).
		Msg("replaced instances deleted")
	return true, nil
}

// progress records the steps of a rollout that an undo must reverse.
type progress struct {
	old       Set
	stopFirst bool
	stopped   int
	news      []Set
	started   int
}

// stop stops a batch of the old set when the earlier new sets still run. The
// undo starts the batch again.
func (p *progress) stop(ctx context.Context, batch Set) error {
	for _, set := range p.news[:len(p.news)-1] {
		if err := within(ctx, set.Up); err != nil {
			return fmt.Errorf("the new instances did not stay up: %w", err)
		}
	}
	log.G(ctx).Info().
		Strs("instances", batch.Names()).
		Msg("stopping the replaced instances")
	p.stopped += batch.Len()
	if err := within(ctx, batch.Stop); err != nil {
		return fmt.Errorf("stopping the replaced instances: %w", err)
	}
	return nil
}

// undo reverses the steps of a rollout that cause stopped. An interrupt
// cannot stop the undo half way.
func (p *progress) undo(ctx context.Context, cause error) error {
	ctx = context.WithoutCancel(ctx)

	var created []string
	for _, set := range p.news {
		created = append(created, set.Names()...)
	}
	// Keep only the text of the cause, as the CLI shows no error that wraps a cancel.
	errs := []error{errors.New(cause.Error())}
	if p.stopped > 0 {
		if p.stopFirst {
			for _, set := range p.news[:p.started] {
				if err := within(ctx, set.Stop); err != nil {
					errs = append(errs, fmt.Errorf("stopping the new instances: %w", err))
				}
			}
		}
		stopped, _ := p.old.Take(p.stopped)
		log.G(ctx).Warn().
			Strs("instances", stopped.Names()).
			Msg("unwinding the rollout, starting the replaced instances again")
		var err error
		if err = within(ctx, stopped.Start); err != nil {
			err = fmt.Errorf("starting the replaced instances again: %w", err)
		} else if err = within(ctx, stopped.Up); err != nil {
			err = fmt.Errorf("the replaced instances did not come back: %w", err)
		}
		if err != nil {
			errs = append(errs, err)
			log.G(ctx).Warn().
				Strs("instances", created).
				Msg("unwinding the rollout, keeping the new instances")
			kept := p.news[p.started:]
			if p.stopFirst {
				kept = p.news
			}
			for _, set := range kept {
				if err := within(ctx, set.Start); err != nil {
					errs = append(errs, fmt.Errorf("starting the kept new instances: %w", err))
				}
			}
			errs = append(errs, fmt.Errorf("the new instances %s are kept", strings.Join(created, " ")))
			return errors.Join(errs...)
		}
	}
	if len(created) == 0 {
		return errors.Join(errs...)
	}
	log.G(ctx).Warn().
		Strs("instances", created).
		Msg("unwinding the rollout, deleting the new instances")
	for _, set := range p.news {
		if err := within(ctx, set.Delete); err != nil {
			errs = append(errs, fmt.Errorf("deleting the new instances: %w", err))
		}
	}
	return errors.Join(errs...)
}

// within runs one step of a rollout with a time limit.
func within(ctx context.Context, step func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, stepTimeout)
	defer cancel()
	return step(ctx)
}
