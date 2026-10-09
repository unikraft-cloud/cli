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
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"unikraft.com/x/log"
)

// stepTimeout bounds each step of a rollout, so a stuck instance cannot hang
// it.
const stepTimeout = 900 * time.Second

// Set is a group of instances that a rollout acts on as one.
type Set interface {
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

// Strategy swaps the old set of instances for the new set that create makes.
type Strategy interface {
	// Rollout reports done when the new set takes the place of the old one.
	// When a step before that fails, it undoes the steps before it.
	Rollout(ctx context.Context, old Set, create func(context.Context) (Set, error)) (done bool, err error)
}

// Rolling starts the new set while the old set still runs.
type Rolling struct {
	// HealthyAfter is how long the new set must run before the old set goes.
	HealthyAfter time.Duration
}

// Rollout creates the new set, waits until it runs and deletes the old set.
func (r Rolling) Rollout(ctx context.Context, old Set, create func(context.Context) (Set, error)) (bool, error) {
	s := &swap{old: old}
	if err := s.create(ctx, create); err != nil {
		return false, s.undo(ctx, err)
	}
	if err := s.settle(ctx, r.HealthyAfter); err != nil {
		return false, s.undo(ctx, err)
	}
	return true, s.finish(ctx)
}

// Replace stops the old set before the new set starts. Create must make the
// new set without starting it.
type Replace struct {
	// HealthyAfter is how long the new set must run before the old set goes.
	HealthyAfter time.Duration
}

// Rollout creates the new set, stops the old set, starts the new set and
// deletes the old set when the new one runs.
func (r Replace) Rollout(ctx context.Context, old Set, create func(context.Context) (Set, error)) (bool, error) {
	s := &swap{old: old}
	if err := s.create(ctx, create); err != nil {
		return false, s.undo(ctx, err)
	}
	log.G(ctx).Info().
		Strs("instances", old.Names()).
		Msg("stopping the replaced instances")
	s.stoppedOld = true
	if err := within(ctx, old.Stop); err != nil {
		return false, s.undo(ctx, fmt.Errorf("stopping the replaced instances: %w", err))
	}
	s.startedNew = true
	if err := within(ctx, s.new.Start); err != nil {
		return false, s.undo(ctx, fmt.Errorf("starting the new instances: %w", err))
	}
	if err := s.settle(ctx, r.HealthyAfter); err != nil {
		return false, s.undo(ctx, err)
	}
	return true, s.finish(ctx)
}

// swap records the steps of a rollout that an undo must reverse.
type swap struct {
	old, new   Set
	stoppedOld bool
	startedNew bool
}

// create makes the new set. An interrupt cannot stop the create, so the undo
// knows every instance that it made.
func (s *swap) create(ctx context.Context, create func(context.Context) (Set, error)) error {
	defer holdInterrupts(ctx, "the rollout stops when the new instances are created")()
	err := within(context.WithoutCancel(ctx), func(ctx context.Context) error {
		var err error
		s.new, err = create(ctx)
		return err
	})
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return fmt.Errorf("rollout interrupted while the new instances were created: %s", ctx.Err())
	}
	return nil
}

// settle waits until the new set runs and, after healthyAfter, checks it
// again.
func (s *swap) settle(ctx context.Context, healthyAfter time.Duration) error {
	if err := within(ctx, s.new.Up); err != nil {
		return fmt.Errorf("the new instances did not come up: %w", err)
	}
	if healthyAfter <= 0 {
		return nil
	}
	log.G(ctx).Info().
		Str("healthy-after", healthyAfter.String()).
		Msg("keeping the new instances up before the replaced ones are removed")
	select {
	case <-ctx.Done():
		return fmt.Errorf("rollout interrupted while the new instances were up: %s", ctx.Err())
	case <-time.After(healthyAfter):
	}
	if err := within(ctx, s.new.Up); err != nil {
		return fmt.Errorf("the new instances did not stay up: %w", err)
	}
	return nil
}

// finish deletes the old set. The rollout is done also when the delete fails.
func (s *swap) finish(ctx context.Context) error {
	if err := within(context.WithoutCancel(ctx), s.old.Delete); err != nil {
		return fmt.Errorf("the new instances are up, but deleting the replaced ones failed: %w", err)
	}
	log.G(ctx).Info().
		Strs("instances", s.old.Names()).
		Msg("replaced instances deleted")
	return nil
}

// undo reverses the steps of a rollout that cause stopped. An interrupt
// cannot stop the undo half way.
func (s *swap) undo(ctx context.Context, cause error) error {
	ctx = context.WithoutCancel(ctx)
	defer holdInterrupts(ctx, "the rollout stops when the unwind is done")()

	errs := []error{errors.New(cause.Error())}
	if s.stoppedOld {
		if s.startedNew {
			if err := within(ctx, s.new.Stop); err != nil {
				errs = append(errs, fmt.Errorf("stopping the new instances: %w", err))
			}
		}
		log.G(ctx).Warn().
			Strs("instances", s.old.Names()).
			Msg("unwinding the rollout, starting the replaced instances again")
		var err error
		if err = within(ctx, s.old.Start); err != nil {
			err = fmt.Errorf("starting the replaced instances again: %w", err)
		} else if err = within(ctx, s.old.Up); err != nil {
			err = fmt.Errorf("the replaced instances did not come back: %w", err)
		}
		if err != nil {
			errs = append(errs, err, fmt.Errorf("the new instances %s are kept", strings.Join(s.new.Names(), " ")))
			return errors.Join(errs...)
		}
	}
	if s.new == nil || len(s.new.Names()) == 0 {
		return errors.Join(errs...)
	}
	log.G(ctx).Warn().
		Strs("instances", s.new.Names()).
		Msg("unwinding the rollout, deleting the new instances")
	if err := within(ctx, s.new.Delete); err != nil {
		errs = append(errs, fmt.Errorf("deleting the new instances: %w", err))
	}
	return errors.Join(errs...)
}

// holdInterrupts logs msg for each interrupt until the returned function is
// called, so a second interrupt does not end the process.
func holdInterrupts(ctx context.Context, msg string) (release func()) {
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)
	go func() {
		for range interrupts {
			log.G(ctx).Warn().Msg(msg)
		}
	}()
	return func() {
		signal.Stop(interrupts)
		close(interrupts)
	}
}

// within runs one step of a rollout with a time limit.
func within(ctx context.Context, step func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, stepTimeout)
	defer cancel()
	return step(ctx)
}
