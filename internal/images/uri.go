// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package images

import (
	"fmt"

	"github.com/distribution/reference"
	"unikraft.com/x/image-spec/uri"
)

// stripImageURI removes an "oci://" prefix from key and rejects every other
// scheme. Keys without a scheme pass through unchanged.
func stripImageURI(key string) (string, error) {
	if parsed, err := uri.Parse(key); err == nil {
		if parsed.Scheme != uri.SchemeOCI {
			return "", fmt.Errorf("%w: invalid scheme %q", reference.ErrReferenceInvalidFormat, parsed.Scheme)
		}
		key = parsed.Path
	}
	return key, nil
}
