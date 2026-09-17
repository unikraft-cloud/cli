// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package sandbox

import "strings"

// environ is the environment as the plugin API takes it, from KEY=VALUE
// records as [os/exec.Cmd] takes them: a later record for a name wins, and one
// without a value sets the name empty. No records is no environment.
func environ(records []string) map[string]string {
	if len(records) == 0 {
		return nil
	}
	env := make(map[string]string, len(records))
	for _, record := range records {
		name, value, _ := strings.Cut(record, "=")
		env[name] = value
	}
	return env
}
