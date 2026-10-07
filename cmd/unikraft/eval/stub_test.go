// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package eval

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"

	"unikraft.com/cli/internal/eval"
	"unikraft.com/cli/internal/mcpserver/mcptest"
)

// stub answers the read-only tools of the session with plausible canned
// results and stops the run at the first other call, so that a model may
// look before it acts without any backend.
func stub(session *mcptest.Session) eval.Exec {
	return func(_ context.Context, name string, args json.RawMessage) (string, bool, error) {
		if tool := session.Tool(name); tool == nil || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			return "", false, eval.ErrStop
		}
		return canned(name, args), false, nil
	}
}

// canned gives the resource a call names, one default resource for an
// unfiltered list, and nothing for a filtered one, so that the model never
// learns of resources beyond the one in the prompt.
func canned(name string, args json.RawMessage) string {
	key := cmp.Or(gjson.GetBytes(args, "keys.0").String(), gjson.GetBytes(args, "key").String())
	switch {
	case name == "metro_list":
		return `[{"name":"fra","location":"Frankfurt"},{"name":"sfo","location":"San Francisco"}]`
	case name == "instance_logs":
		return "[    0.000000] Unikraft Cloud\n" + key + ": listening on 0.0.0.0:80\n"
	case name == "sandbox_read":
		return key + "\n"
	case key == "" && gjson.GetBytes(args, "filter").Exists():
		return "[]"
	}
	resource, _, _ := strings.Cut(name, "_")
	switch resource {
	case "instance":
		return fmt.Sprintf(`[{"metro":"fra","name":%q,"state":"running","image":"nginx:latest","memory":"128MiB","vcpus":1}]`, cmp.Or(key, "web-1"))
	case "image":
		return fmt.Sprintf(`[{"name":%q,"size":"12MiB","entrypoint":["nginx","-g","daemon off;"]}]`, cmp.Or(key, "nginx:latest"))
	case "volume":
		return fmt.Sprintf(`[{"metro":"fra","name":%q,"state":"available","size":"1GiB"}]`, cmp.Or(key, "data"))
	default:
		return fmt.Sprintf(`[{"metro":"fra","name":%q}]`, cmp.Or(key, resource))
	}
}
