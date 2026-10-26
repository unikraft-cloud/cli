// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package mcp

import (
	"slices"
	"strings"

	modelmcp "unikraft.com/cli/internal/model/mcp"
)

func Instructions(readOnly bool, resources []string, docsURL string) string {
	text := `Tools are named <resource>_<operation>: list, create, edit and delete for each Unikraft Cloud resource type. A list tool returns everything unless keys names specific resources, so it doubles as get. image_get inspects any image reference, including public ones. instance_control starts, stops, restarts or suspends instances and instance_logs reads their console output. instance_create starts the new instance unless autostart is set to false; do that only when the user explicitly asks for a stopped instance.

Resource keys accept a name or a UUID, optionally prefixed with metro/ to target one metro, as in fra/my-app. Reads fan out to every metro of the current profile; narrow a list with filter entries such as metro==fra or state==running.

Create takes a set object and edit takes set, add and del objects, all keyed by dotted field paths, exactly like the CLI's --set, --add and --del flags. Values may be strings, parsed as on the command line, or JSON values of the field's type. Pass dry_run to preview the resulting patch without applying it.

A sandbox is an instance with the sandbox plugin loaded, so to create one call instance_create with the plugin in the same set as the image, for example {"image": "official/alpine:latest", "plugins": ["name=sandbox,image=plugins/sandbox:latest"]}. The image must run on the base-compat runtime; official/alpine:latest and official/debian-slim:latest ship the standard Linux utilities so most commands work. To keep files across restarts, put a volume such as ":/data:size=1GiB" in volumes, which creates it and attaches it at /data in the same call, and add config={"persist_path":"/data"} to the plugin entry. Plugins load at boot, so a plugin added to a running instance with instance_edit only takes effect after a restart with instance_control. Inside a running sandbox, sandbox_exec runs a command, sandbox_read returns a text file and sandbox_write creates or appends to one; interactive shells and bulk file copies stay with the unikraft instance shell and copy CLI commands.

Results come back as JSON text and as structured content under "resources". Pass fields to limit which field paths are returned.`
	if docsURL != "" {
		text += "\n\nThe documentation is available as resources: read unikraft://docs for the table of contents and unikraft://docs/<path> for a page, the same content as " + strings.TrimSuffix(docsURL, "/") + "/<path>. The CLI reference lives under unikraft://docs/cli/unikraft."
	}
	if len(resources) > 0 {
		names := make([]string, 0, len(resources))
		for _, name := range resources {
			names = append(names, modelmcp.NormalizeName(name))
		}
		slices.Sort(names)
		text += "\n\nOnly the tools of these resource types are served: " + strings.Join(slices.Compact(names), ", ") + "."
	}
	if readOnly {
		text += "\n\nThis server runs in read-only mode: only the list, image_get, instance_logs and sandbox_read tools are available."
	}
	return text
}
