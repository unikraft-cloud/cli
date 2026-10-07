// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package platforms

import (
	"fmt"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
)

func TestOnlyArch(t *testing.T) {
	m := OnlyArch("arm64", "x86_64")
	require.True(t, m.Match(ocispec.Platform{OS: "fc", Architecture: "x86_64"}))
	require.True(t, m.Match(ocispec.Platform{OS: "qemu", Architecture: "amd64"}))
	require.True(t, m.Match(ocispec.Platform{OS: "kraftcloud", Architecture: "aarch64"}))
	require.True(t, m.Match(ocispec.Platform{OS: "linux", Architecture: "arm64", Variant: "v8"}))
	require.False(t, m.Match(ocispec.Platform{OS: "fc", Architecture: "riscv64"}))
	require.False(t, m.Match(ocispec.Platform{OS: "fc"}))
}

func TestOnlyArchLess(t *testing.T) {
	m := OnlyArch("arm64", "x86_64")
	arm := ocispec.Platform{OS: "fc", Architecture: "arm64"}
	intel := ocispec.Platform{OS: "fc", Architecture: "x86_64"}
	other := ocispec.Platform{OS: "fc", Architecture: "riscv64"}
	require.True(t, m.Less(arm, intel))
	require.False(t, m.Less(intel, arm))
	require.True(t, m.Less(intel, other))
	require.False(t, m.Less(other, other))
}

func TestOnlyArchString(t *testing.T) {
	require.Equal(t, "arm64, x86_64", fmt.Sprint(OnlyArch("arm64", "x86_64")))
}
