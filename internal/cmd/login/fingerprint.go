// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package login

import (
	"unikraft.com/cloud/sdk/controlplane"
	"unikraft.com/x/fingerprint"
	"unikraft.com/x/version"
)

func getFingerprint() (controlplane.RequestSigninRequest, error) {
	fp, err := fingerprint.New()
	if err != nil {
		return controlplane.RequestSigninRequest{}, err
	}

	req := controlplane.RequestSigninRequest{
		Hostname:       fp.Hostname,
		Os:             &fp.Os,
		OsVersion:      fp.OsVersion,
		Container:      &fp.Container,
		Distro:         fp.Distro,
		DistroVersion:  fp.DistroVersion,
		DistroCodename: fp.DistroCodename,
		CliVersion:     &version.Version,
		Goarch:         &fp.Goarch,
		Goos:           &fp.Goos,
		GoVersion:      fp.GoVersion,
	}
	return req, nil
}
