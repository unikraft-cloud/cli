// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"fmt"
	"strings"
)

// ParseContextNames parses --build-context values in the form name=value.
func ParseContextNames(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}

	result := make(map[string]string, len(values))
	for _, value := range values {
		name, contextValue, ok := strings.Cut(value, "=")
		if !ok ||
			strings.TrimSpace(name) == "" ||
			name != strings.TrimSpace(name) ||
			strings.TrimSpace(contextValue) == "" {
			return nil, fmt.Errorf("invalid build context %q: expected name=value with non-empty name and value", value)
		}

		if _, exists := result[name]; exists {
			return nil, fmt.Errorf("duplicate build context name %q", name)
		}

		result[name] = contextValue
	}

	return result, nil
}
