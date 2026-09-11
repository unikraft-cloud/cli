// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package builder

import (
	"os"
	"strings"

	"github.com/pkg/errors"
)

// ParseContextNames parses --build-context values in the form name=value.
func ParseContextNames(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}

	result := make(map[string]string, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}

		kv := strings.SplitN(value, "=", 2)
		if len(kv) != 2 || kv[0] == "" {
			return nil, errors.Errorf(
				"invalid context value: %s, expected key=value",
				value,
			)
		}
		result[kv[0]] = kv[1]
	}

	return result, nil
}

func isLocalBuildContext(value string) bool {
	switch {
	case strings.HasPrefix(value, "docker-image://"),
		strings.HasPrefix(value, "target:"),
		strings.Contains(value, "://"):
		return false
	}
	if _, err := os.Stat(value); err == nil {
		return true
	}
	return strings.HasPrefix(value, "./") ||
		strings.HasPrefix(value, "../") ||
		strings.HasPrefix(value, "/")
}
