// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd_test

import (
	"bytes"
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	integ "unikraft.com/cli/internal/integration"

	"unikraft.com/cloud/sdk/platform"

	"unikraft.com/cli/internal/cmd"
	"unikraft.com/cli/internal/resource"
	resourcecmd "unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/types"
)

// sectionHeader returns a header line centered within 80 characters.
func sectionHeader(name string) string {
	const width = 80
	middle := " " + name + " "
	pad := width - len(middle)
	left := pad / 2
	right := pad - left
	return strings.Repeat("=", left) + middle + strings.Repeat("=", right)
}

// dumpResource renders a resource using kv, kv-all, table, and debug printers
// and returns the concatenated output.
func dumpResource(t *testing.T, res resource.Resource) string {
	t.Helper()
	var output strings.Builder

	type section struct {
		name       string
		format     resourcecmd.PrinterType
		fieldSpecs []string
	}

	sections := []section{
		{"kv", resourcecmd.PrinterTypeKeyValue, nil},
		{"kv-all", resourcecmd.PrinterTypeKeyValue, []string{"all"}},
		{"table", resourcecmd.PrinterTypeTable, nil},
		{"debug", resourcecmd.PrintTypeDebug, nil},
	}

	for _, s := range sections {
		printer := resourcecmd.Printer{Type: s.format}

		var buf bytes.Buffer
		err := printer.Print(t.Context(), &buf, s.fieldSpecs, res, res)
		require.NoError(t, err)

		rendered := ansi.Strip(buf.String())
		rendered = strings.TrimRightFunc(rendered, unicode.IsSpace)

		output.WriteString(sectionHeader(s.name) + "\n")
		output.WriteString(rendered)
		output.WriteString("\n\n")
	}

	return strings.TrimRightFunc(output.String(), unicode.IsSpace) + "\n"
}

// TestOutput runs printer/output tests for all resource types.
// They construct static sample data and verify that rendering through
// kv/kv-all/table/debug printers is stable.
func TestOutput(t *testing.T) {
	t.Parallel()

	run := func(name string, fn func(*testing.T)) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fn(t)
		})
	}

	run("instances", instancesOutputTests)
	run("instance-templates", instanceTemplatesOutputTests)
	run("instance-checkpoints", instanceCheckpointsOutputTests)
	run("instance-history", instanceHistoryOutputTests)
	run("volumes", volumesOutputTests)
	run("volume-templates", volumeTemplatesOutputTests)
	run("services", servicesOutputTests)
	run("certificates", certificatesOutputTests)
	run("images", imagesOutputTests)
	run("instances-http-oci", instancesHTTPOCIOutputTests)
}

func instancesOutputTests(t *testing.T) {
	sample := cmd.Instance{
		Metro: "fra",
		Name:  "my-instance",
		UUID:  "7b79b250-0658-4d10-8dc0-d854399d7e74",
		Tags:  []string{"env-prod", "team-core"},
		State: types.InstanceState(platform.InstanceStateRunning),
		Service: &cmd.InstanceService{
			Link: cmd.Link[cmd.ServiceGroup]{
				Name: "my-service",
				UUID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
			},
			Services: []*cmd.Service{
				{Source: 443, Destination: 8080, Handlers: []platform.ConnectionHandler{"tls", "http"}},
			},
			Domains: []cmd.Domain{
				{FQDN: "example.unikraft.app"},
			},
		},
		Volumes: []*cmd.InstanceVolume{
			{Link: cmd.Link[cmd.Volume]{Name: "my-volume"}, At: "/data", Readonly: true},
		},
		Roms: []*cmd.InstanceRom{
			{Name: "my-rom", Image: "myuser/my-rom:latest", At: "/rom"},
		},
		ScaleToZero: cmd.InstanceScaleToZero{
			Policy:       "on",
			Stateful:     true,
			CooldownTime: 500,
			NotifyTime:   100,
		},
	}
	sample.Runtime.Args = cmd.InstanceArgs{"arg1", "arg2"}
	sample.Runtime.Env = map[string]string{"KEY1": "val1", "KEY2": "val2"}
	sample.Resources.Memory = 256
	sample.Resources.VCPUs = 2
	sample.Networks = append(sample.Networks, cmd.InstanceNetwork{})
	sample.Networks[0].UUID = "net-uuid-1234"
	sample.Networks[0].PrivateIP = "192.168.1.10"
	sample.Networks[0].MAC = "aa:bb:cc:dd:ee:ff"
	vmType := platform.InstanceTypeFull
	sample.Type_ = &vmType
	sample.Gpus = []cmd.InstanceGpu{
		{UUID: "gpu-uuid-1234", Model: "10de:1eb8"},
	}
	sample.Resources.GPUs = len(sample.Gpus)
	sample.Timing.Uptime = types.DurationMS(1500)
	sample.Timing.BootTime = types.DurationUS(250000)
	sample.Timing.NetTime = types.DurationUS(100000)
	sample.Restart.Policy = "always"
	sample.Restart.StartCount = 3
	sample.Restart.RestartCount = 1
	schedPriority := platform.SchedPriorityHigh
	sample.SchedPriority = &schedPriority
	sample.Stop.Reason = "crashed"
	sample.Stop.Origin = "kernel"
	exitCode := uint32(1)
	sample.Stop.ExitCode = &exitCode
	require.NoError(t, sample.Image.UnmarshalText([]byte("nginx:latest")))

	integ.Gild[resource.Resource](t, dumpResource, sample)
}

// Every other sample carries a registry reference, so nothing exercises how an
// OCI layout served over HTTP renders.
func instancesHTTPOCIOutputTests(t *testing.T) {
	sample := cmd.Instance{
		Metro: "fra",
		Name:  "my-instance",
		UUID:  "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		State: types.InstanceState(platform.InstanceStateRunning),
	}
	sample.Resources.Memory = 256
	sample.Resources.VCPUs = 1
	require.NoError(t, sample.Image.UnmarshalText([]byte(
		"https+oci://cdn.example.com/me/app/@sha256:"+
			"43d3d758e6fba7d4734ac142cfdbf8aa786fcbbfd828017eecaadc5140a4b190")))

	integ.Gild[resource.Resource](t, dumpResource, sample)
}

func instanceTemplatesOutputTests(t *testing.T) {
	sample := cmd.InstanceTemplate{
		Metro: "fra",
		Name:  "my-template",
		UUID:  "d4e5f6a7-b8c9-0123-def0-123456789abc",
		Tags:  []string{"env-staging"},
		State: types.InstanceState(platform.InstanceStateStopped),
	}
	sample.Resources.Memory = 128
	sample.Resources.VCPUs = 1
	require.NoError(t, sample.Image.UnmarshalText([]byte("nginx:latest")))

	integ.Gild[resource.Resource](t, dumpResource, sample)
}

func instanceCheckpointsOutputTests(t *testing.T) {
	sample := cmd.InstanceCheckpoint{
		MetroName:  "fra",
		Name:       "my-checkpoint",
		UUID:       "f6a7b8c9-d0e1-2345-f012-3456789abcde",
		Tags:       []string{"env-prod", "team-core"},
		DeleteLock: true,
		State:      types.InstanceState(platform.InstanceStateCheckpoint),
		Volumes: []*cmd.InstanceVolume{
			{Link: cmd.Link[cmd.Volume]{Name: "my-volume"}, At: "/data", Readonly: true},
		},
	}
	sample.Runtime.Args = cmd.InstanceArgs{"arg1", "arg2"}
	sample.Runtime.Env = map[string]string{"KEY1": "val1", "KEY2": "val2"}
	sample.Resources.Memory = 256
	sample.Resources.VCPUs = 2
	sample.Restart.Policy = "always"
	require.NoError(t, sample.Image.UnmarshalText([]byte("nginx:latest")))

	integ.Gild[resource.Resource](t, dumpResource, sample)
}

func instanceHistoryOutputTests(t *testing.T) {
	sample := cmd.InstanceHistoryEntry{
		Metro:  "fra",
		Target: "fra/my-instance",
		Name:   "my-checkpoint",
		UUID:   "f6a7b8c9-d0e1-2345-f012-3456789abcde",
		// Created is left as the zero value so it renders deterministically
		// ("never") instead of a wall-clock-relative string.
	}

	integ.Gild[resource.Resource](t, dumpResource, sample)
}

func volumesOutputTests(t *testing.T) {
	sample := cmd.Volume{
		Metro: "fra",
		Name:  "my-volume",
		UUID:  "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		Tags:  []string{"env-prod"},
		State: types.VolumeState(platform.VolumeStateAvailable),
		Size:  50,
		Free:  new(types.SizeMebibytes(10)),
		Usage: types.MeterUsage[types.SizeMebibytes]{
			Used:  40,
			Total: 50,
		},
		Filesystem:  "ext4",
		QuotaPolicy: "hard",
		Persistent:  true,
		AccessMode:  new(types.AccessMode(platform.VolumeAccessModeRwo)),
	}

	integ.Gild[resource.Resource](t, dumpResource, sample)
}

func volumeTemplatesOutputTests(t *testing.T) {
	sample := cmd.VolumeTemplate{
		Metro:      "fra",
		Name:       "my-vol-template",
		UUID:       "e5f6a7b8-c9d0-1234-ef01-23456789abcd",
		Tags:       []string{"env-staging"},
		State:      types.VolumeState(platform.VolumeStateAvailable),
		Size:       20,
		Filesystem: "ext4",
		Persistent: true,
	}

	integ.Gild[resource.Resource](t, dumpResource, sample)
}

func servicesOutputTests(t *testing.T) {
	sample := cmd.ServiceGroup{
		Metro:      "fra",
		Name:       "my-service",
		UUID:       "b2c3d4e5-f6a7-8901-bcde-f12345678901",
		Persistent: true,
		Autoscale:  true,
		Services: []*cmd.Service{
			{
				Source:      443,
				Destination: 8080,
				Handlers:    []platform.ConnectionHandler{"tls", "http"},
			},
			{
				Source:      80,
				Destination: 443,
				Handlers:    []platform.ConnectionHandler{"http", "redirect"},
			},
		},
		Domains: []cmd.Domain{
			{FQDN: "example.unikraft.app"},
		},
	}
	sample.Limits.Soft = 5
	sample.Limits.Hard = 50

	integ.Gild[resource.Resource](t, dumpResource, sample)
}

func certificatesOutputTests(t *testing.T) {
	sample := cmd.Certificate{
		Metro:        "fra",
		Name:         "my-cert",
		UUID:         "c3d4e5f6-a7b8-9012-cdef-123456789012",
		CommonName:   "example.unikraft.app",
		Subject:      "CN=example.unikraft.app",
		Issuer:       "CN=Test CA",
		SerialNumber: "1234567890",
		State:        types.CertificateState(platform.CertificateStateValid),
	}

	integ.Gild[resource.Resource](t, dumpResource, sample)
}

func imagesOutputTests(t *testing.T) {
	sample := &cmd.Image{
		Digest: digest.Digest("sha256:a3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4"),
		Config: cmd.ImageConfig{
			Cmd: []string{"/usr/sbin/nginx", "-g", "daemon off;"},
			Env: map[string]string{"PATH": "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"},
		},
		Metadata: cmd.ImageMetadata{
			Author: "unikraft.io",
		},
		Kernel: &cmd.ImageFile{
			Digest:    digest.Digest("sha256:b3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4"),
			MediaType: "application/vnd.unikraft.kernel.v1",
			Size:      types.SizeBytes(4194304),
		},
		Initrd: &cmd.ImageFile{
			Digest:    digest.Digest("sha256:c3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4"),
			MediaType: "application/vnd.unikraft.initrd.v1",
			Size:      types.SizeBytes(1048576),
		},
		KernelDebug: &cmd.ImageFile{
			Digest:    digest.Digest("sha256:d3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4"),
			MediaType: "application/vnd.unikraft.kernel.v1",
			Size:      types.SizeBytes(8388608),
		},
		Roms: []cmd.ImageFile{
			{
				Digest:    digest.Digest("sha256:e3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4"),
				MediaType: "application/vnd.unikraft.rom.v1",
				Size:      types.SizeBytes(524288),
			},
		},
	}
	require.NoError(t, sample.Ref.UnmarshalText([]byte("nginx:latest")))
	require.NoError(t, sample.Config.Platform.UnmarshalText([]byte("linux/amd64")))

	integ.Gild[resource.Resource](t, dumpResource, sample)
}
