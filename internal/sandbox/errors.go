// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package sandbox

import (
	"errors"
	"fmt"
	"net/http"

	plugin "unikraft.com/cloud/plugins/sandbox"
)

var ErrNotRunning = errors.New("the instance is not running")

// ExitError is what [Cmd.Wait] reports for a command that ran and exited
// non-zero, so that a caller which only asks whether the command worked is
// answered by the error alone. Code is what the instance reported, which is
// the negated signal for a command that was killed by one.
type ExitError struct {
	UUID string
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("command %s exited with status %d", e.UUID, e.Code)
}

func (e *ExitError) ExitCode() int { return e.Code }

func notServing(err error) (*plugin.APIError, bool) {
	apiErr, ok := plugin.GetAPIError(err)
	if !ok || apiErr.Status != "" || apiErr.StatusCode == 0 {
		return nil, false
	}
	return apiErr, true
}

func (t Target) notRunning(err error) error {
	apiErr, ok := notServing(err)
	if !ok || !apiErr.IsNotFound() {
		return nil
	}
	return fmt.Errorf("%w, or has no %q plugin", ErrNotRunning, t.Plugin)
}

func (t Target) apiError(what string, err error) error {
	if err == nil {
		return nil
	}
	if down := t.notRunning(err); down != nil {
		return down
	}
	if apiErr, ok := notServing(err); ok {
		return fmt.Errorf("%s: the %q plugin answered %d %s",
			what, t.Plugin, apiErr.StatusCode, http.StatusText(apiErr.StatusCode))
	}
	return fmt.Errorf("%s: %w", what, err)
}
