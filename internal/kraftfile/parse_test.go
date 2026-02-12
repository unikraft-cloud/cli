// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package kraftfile

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSpecAndName(t *testing.T) {
	input := `spec: v0.7
name: helloworld
`
	requireValidSchema(t, input)
	doc, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	require.Equal(t, "v0.7", doc.Spec)
	require.Equal(t, "helloworld", doc.Name)
}

func TestParseSpecificationAlias(t *testing.T) {
	input := `specification: v0.7
`
	requireValidSchema(t, input)
	doc, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	require.Equal(t, "v0.7", doc.Spec)
}

func TestParseCmdArray(t *testing.T) {
	input := `spec: v0.7
cmd: ["-c", "/nginx/conf/nginx.conf"]
`
	requireValidSchema(t, input)
	doc, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	require.Equal(t, Command{"-c", "/nginx/conf/nginx.conf"}, doc.Cmd)
}

func TestParseCmdString(t *testing.T) {
	input := `spec: v0.7
cmd: "-c /nginx/conf/nginx.conf"
`
	requireValidSchema(t, input)
	doc, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	require.Equal(t, Command{"-c", "/nginx/conf/nginx.conf"}, doc.Cmd)
}

func TestParseEnvListAndMap(t *testing.T) {
	input := `spec: v0.7
env:
- HOME=/
`
	requireValidSchema(t, input)
	doc, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	require.Equal(t, Map{{Key: "HOME", Value: "/"}}, doc.Env)
	value, ok := doc.Env.Lookup("HOME")
	require.True(t, ok)
	require.Equal(t, "/", value)

	input = `spec: v0.7
env:
  ZETA: z
  ALPHA: a
`
	requireValidSchema(t, input)
	doc, err = ParseBytes([]byte(input))
	require.NoError(t, err)
	require.Equal(t, Map{{Key: "ALPHA", Value: "a"}, {Key: "ZETA", Value: "z"}}, doc.Env)
	require.Equal(t, "a", doc.Env.Get("ALPHA"))
	require.Equal(t, "z", doc.Env.Get("ZETA"))
}

func TestParseLabelsListAndMap(t *testing.T) {
	input := `spec: v0.7
labels:
  app: demo
  tier: web
`
	requireValidSchema(t, input)
	doc, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	require.Equal(t, map[string]string{"app": "demo", "tier": "web"}, doc.Labels)
	require.Equal(t, "web", doc.Labels["tier"])
}

func TestParseVolumesAndTargets(t *testing.T) {
	input := `spec: v0.7
volumes:
- ./src:/dest
targets:
  - qemu/x86_64
  - plat: qemu
    arch: x86_64
`
	requireValidSchema(t, input)
	doc, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	require.Len(t, doc.Volumes, 1)
	require.Equal(t, "./src", doc.Volumes[0].Source)
	require.Equal(t, "/dest", doc.Volumes[0].Destination)
	require.Len(t, doc.Targets, 2)
	require.Equal(t, "qemu", doc.Targets[0].Plat)
	require.Equal(t, "x86_64", doc.Targets[0].Arch)
	require.Equal(t, "qemu", doc.Targets[1].Plat)
	require.Equal(t, "x86_64", doc.Targets[1].Arch)
}

func TestParseVolumesLongHand(t *testing.T) {
	input := `spec: v0.7
volumes:
- source: ./src
  destination: /dest
  driver: 9pfs
  readonly: false
- ./data:/data:ro
`
	requireValidSchema(t, input)
	doc, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	require.Len(t, doc.Volumes, 2)
	require.Equal(t, "./src", doc.Volumes[0].Source)
	require.Equal(t, "/dest", doc.Volumes[0].Destination)
	require.Equal(t, "9pfs", doc.Volumes[0].Driver)
	require.False(t, doc.Volumes[0].ReadOnly)
	require.Equal(t, "./data", doc.Volumes[1].Source)
	require.Equal(t, "/data", doc.Volumes[1].Destination)
	require.Equal(t, "ro", doc.Volumes[1].Mode)
}

func TestParseRootfsStringAndListUnsupported(t *testing.T) {
	input := `spec: v0.7
rootfs: ./Dockerfile
`
	requireValidSchema(t, input)
	doc, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	require.NotNil(t, doc.Rootfs)
	require.Equal(t, "./Dockerfile", doc.Rootfs.Source)
	require.Equal(t, FsTypeCpio, doc.Rootfs.Type)

	input = `spec: v0.7
rootfs:
  source: ./initramfs.erofs
  type: erofs
`
	requireValidSchema(t, input)
	doc, err = ParseBytes([]byte(input))
	require.NoError(t, err)
	require.NotNil(t, doc.Rootfs)
	require.Equal(t, "./initramfs.erofs", doc.Rootfs.Source)
	require.Equal(t, FsTypeErofs, doc.Rootfs.Type)
}

func TestParseComponentsAndLibraries(t *testing.T) {
	input := `spec: v0.7
unikraft: stable
template:
  source: https://github.com/unikraft/app-elfloader.git
  version: staging
libraries:
  lwip:
    source: https://github.com/unikraft/lib-lwip.git
    version: staging
    kconfig:
      CONFIG_LWIP_TCP: 'y'
`
	requireValidSchema(t, input)
	doc, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	require.NotNil(t, doc.Unikraft)
	require.Empty(t, doc.Unikraft.Source)
	require.Equal(t, "stable", doc.Unikraft.Version)
	require.NotNil(t, doc.Template)
	require.Equal(t, "https://github.com/unikraft/app-elfloader.git", doc.Template.Source)
	require.Equal(t, "staging", doc.Template.Version)
	require.Contains(t, doc.Libraries, "lwip")
	require.Equal(t, "https://github.com/unikraft/lib-lwip.git", doc.Libraries["lwip"].Source)
	require.Equal(t, "staging", doc.Libraries["lwip"].Version)
	value, ok := doc.Libraries["lwip"].KConfig.Lookup("CONFIG_LWIP_TCP")
	require.True(t, ok)
	require.Equal(t, "y", value)
}

func TestParseTargetsMapFields(t *testing.T) {
	input := `spec: v0.7
targets:
- platform: qemu
  architecture: x86_64
`
	requireValidSchema(t, input)
	doc, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	require.Len(t, doc.Targets, 1)
	require.Equal(t, "qemu", doc.Targets[0].Plat)
	require.Equal(t, "x86_64", doc.Targets[0].Arch)
}

func TestParseTargetsWithKConfigMap(t *testing.T) {
	input := `spec: v0.7

name: helloworld

unikraft: stable

targets:
- name: helloworld-qemu-x86_64-9pfs
  plat: qemu
  arch: x86_64
  kconfig:
    CONFIG_LIBVFSCORE_AUTOMOUNT_ROOTFS: "y"
    CONFIG_LIBVFSCORE_ROOTFS_9PFS: "y"
    CONFIG_LIBVFSCORE_ROOTFS: "9pfs"
    CONFIG_LIBVFSCORE_ROOTDEV: "fs0"

- name: helloworld-qemu-x86_64-initrd
  plat: qemu
  arch: x86_64
  kconfig:
    CONFIG_LIBVFSCORE_AUTOMOUNT_ROOTFS: "y"
    CONFIG_LIBVFSCORE_ROOTFS_INITRD: "y"
    CONFIG_LIBVFSCORE_ROOTFS: "initrd"
`
	requireValidSchema(t, input)
	doc, err := ParseBytes([]byte(input))
	require.NoError(t, err)
	require.Len(t, doc.Targets, 2)
	require.Equal(t, "qemu", doc.Targets[0].Plat)
	require.Equal(t, "x86_64", doc.Targets[0].Arch)
	require.Equal(t, "y", doc.Targets[0].KConfig.Get("CONFIG_LIBVFSCORE_AUTOMOUNT_ROOTFS"))
	require.Equal(t, "9pfs", doc.Targets[0].KConfig.Get("CONFIG_LIBVFSCORE_ROOTFS"))
	value, ok := doc.Targets[0].KConfig.Lookup("CONFIG_LIBVFSCORE_ROOTDEV")
	require.True(t, ok)
	require.Equal(t, "fs0", value)
	require.Equal(t, "qemu", doc.Targets[1].Plat)
	require.Equal(t, "x86_64", doc.Targets[1].Arch)
	require.Equal(t, "y", doc.Targets[1].KConfig.Get("CONFIG_LIBVFSCORE_AUTOMOUNT_ROOTFS"))
	require.Equal(t, "initrd", doc.Targets[1].KConfig.Get("CONFIG_LIBVFSCORE_ROOTFS"))
}

func TestParseSpecErrors(t *testing.T) {
	input := `name: missing-spec
`
	requireSchemaError(t, input, "spec")
	_, err := ParseBytes([]byte(input))
	require.Error(t, err)
	require.ErrorContains(t, err, "missing 'spec' version attribute")

	input = `spec: v0.8
`
	requireSchemaError(t, input, "spec")
	_, err = ParseBytes([]byte(input))
	require.Error(t, err)
	require.ErrorContains(t, err, "unsupported spec version")
}

func TestParseInvalidCmdList(t *testing.T) {
	input := `spec: v0.7
cmd: [1, 2]
`
	requireSchemaError(t, input, "cmd")
	_, err := ParseBytes([]byte(input))
	require.Error(t, err)
	require.ErrorContains(t, err, "cmd array entries must be strings")
}

func requireValidSchema(t *testing.T, input string) {
	t.Helper()
	requireSchemaOK(t, input)
}

func requireSchemaOK(t *testing.T, input string) {
	t.Helper()
	err := Validate([]byte(input))
	require.NoError(t, err)
}

func requireSchemaError(t *testing.T, input string, contains string) {
	t.Helper()
	err := Validate([]byte(input))
	require.Error(t, err)
	require.ErrorContains(t, err, contains)
}
