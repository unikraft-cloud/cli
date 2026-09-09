// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package images

import (
	"fmt"

	"github.com/distribution/reference"
	imagespec "unikraft.com/x/image-spec"
)

// stripImageURI removes an "oci://" prefix from key and rejects every other
// scheme.  Keys without a scheme pass through unchanged.
func stripImageURI(key string) (string, error) {
	if uri, err := imagespec.ParseURI(key); err == nil {
		if uri.Scheme != imagespec.URISchemeOCI {
			return "", fmt.Errorf("%w: invalid scheme %q", reference.ErrReferenceInvalidFormat, uri.Scheme)
		}
		key = uri.Path
	}
	return key, nil
}
