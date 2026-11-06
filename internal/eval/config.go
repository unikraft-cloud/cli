// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package eval

import (
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// Config holds the settings of the conversations of an eval run.
type Config struct {
	Client anthropic.Client
	// Model is the model under test.
	Model string
	// Effort is the model's effort level; empty keeps the API default.
	Effort string
	// Concurrency bounds the model requests in flight across conversations.
	Concurrency int
	// Timeout bounds one Send.
	Timeout time.Duration
	// MaxTurns bounds the model turns of one Send.
	MaxTurns int
	// Recorder receives a row and the transcript of every conversation when
	// its test ends; nil records nothing.
	Recorder *Recorder
}
