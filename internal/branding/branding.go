// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package branding

import "github.com/MakeNowJust/heredoc"

const (
	Name       = "unikraft"
	Product    = "Unikraft Cloud"
	WebsiteURL = "https://unikraft.com"
	DocsURL    = "https://unikraft.com/docs/"
	RepoURL    = "https://github.com/unikraft-cloud/cli"
	IssuesURL  = RepoURL + "/issues"
)

var Art = heredoc.Docf(`
	  __ _____  (_) /__ _______ _/ _/ /_
	 / // / _ \/ /  '_// __/ _ %[1]s/ _/ __/
	\_,_/_//_/_/_/\_\/_/  \_,_/_/ \__/`, "`")
