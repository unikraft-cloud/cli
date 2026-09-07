// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build js

package login

import (
	"runtime"

	"unikraft.com/cloud/sdk/controlplane"
	"unikraft.com/x/ptr"
	"unikraft.com/x/version"
)

// getFingerprint reports a fixed identity.  The browser exposes no machine
// characteristics, so every web console session looks the same.
func getFingerprint() (controlplane.RequestSigninRequest, error) {
	return controlplane.RequestSigninRequest{
		Hostname:   "web-console",
		Os:         ptr.Ptr("browser"),
		Container:  ptr.Ptr(false),
		CliVersion: &version.Version,
		Goarch:     ptr.Ptr(runtime.GOARCH),
		Goos:       ptr.Ptr(runtime.GOOS),
		GoVersion:  ptr.Ptr(runtime.Version()),
	}, nil
}
