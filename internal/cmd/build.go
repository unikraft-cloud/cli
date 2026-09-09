// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"strings"

	"unikraft.com/x/kingkong"
)

type ImageBuildCmd struct {
	Input  string   `arg:"" default:"." help:"Path to the input directory."`
	Output string   `short:"o" help:"Output destination"`
	Arch   []string `help:"Only build the Kraftfile targets of these architectures. Defaults to every declared target; required when none are declared." example:"x86_64,arm64"`

	// similar to docker compose build
	BuildArg []string `sep:"none" help:"Set build-time variables."`
	NoCache  bool     `help:"Do not use cache when building the image."`
	Secret   []string `sep:"none" help:"Secret to expose to the build (format: \"id=mysecret[,src=/local/secret]\")."`
	SSH      []string `sep:"none" help:"SSH agent socket or keys to expose to the build (format: \"default|<id>[=<socket>|<key>[,<key>]]\")."`

	Insecure []string `help:"Allow insecure (HTTP/unverified TLS) connections to registries. Specify hostnames to restrict, or omit to apply to all." type:"optional"`
}

func (ImageBuildCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Build the project in the current directory",
			Commands: []string{
				"unikraft image build .",
			},
		},
		{
			Description: "Build and publish an image from a Kraftfile",
			Commands: []string{
				"unikraft image build . --output my-org/my-app:latest",
			},
		},
		{
			Description: "Build with custom build arguments",
			Commands: []string{
				"unikraft image build ./app --build-arg VERSION=1.2.3 --build-arg COMMIT=abc123",
			},
		},
		{
			Description: "Build with secret and SSH access",
			Commands: []string{
				"unikraft image build . --secret id=npm,src=$HOME/.npmrc --ssh default=$SSH_AUTH_SOCK",
			},
		},
		{
			Description: "Build for arm64 and publish the image",
			Commands: []string{
				"unikraft image build . --arch arm64 --output my-org/my-app:latest",
			},
		},
		{
			Description: "Build and save to a local OCI archive",
			Commands: []string{
				"unikraft image build . --output ./dist/my-app.oci.tar",
			},
		},
	}
}

type BuildCmd struct {
	ImageBuildCmd `embed:""`
}

func (BuildCmd) Examples() []kingkong.Example {
	imageExamples := ImageBuildCmd{}.Examples()
	for i := range imageExamples {
		for j := range imageExamples[i].Commands {
			imageExamples[i].Commands[j] = strings.Replace(
				imageExamples[i].Commands[j],
				"unikraft image build",
				"unikraft build",
				1,
			)
		}
	}

	return imageExamples
}
