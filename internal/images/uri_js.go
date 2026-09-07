// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package images

import (
	"fmt"
	"strings"

	"github.com/distribution/reference"
)

// stripImageURI rejects image URIs.  The web console has no local image store,
// so only plain registry references are accepted.
func stripImageURI(key string) (string, error) {
	if scheme, rest, ok := strings.Cut(key, "://"); ok {
		if scheme != "oci" {
			return "", fmt.Errorf("%w: invalid scheme %q", reference.ErrReferenceInvalidFormat, scheme)
		}
		key = rest
	}
	return key, nil
}
