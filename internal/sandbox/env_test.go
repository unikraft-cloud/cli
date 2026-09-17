// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package sandbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnviron(t *testing.T) {
	assert.Nil(t, environ(nil), "no records is no environment to send, not an empty one")
	assert.Equal(t, map[string]string{"A": "2", "B": "x=y", "C": ""},
		environ([]string{"A=1", "B=x=y", "C", "A=2"}),
		"a record splits on its first =, one without a value is set empty, and a later record wins")
}
