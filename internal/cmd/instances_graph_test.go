// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"testing"
	"time"

	"github.com/docker/go-units"
	"github.com/stretchr/testify/require"

	"unikraft.com/cloud/sdk/platform"
)

func TestResolveInstanceGraphField(t *testing.T) {
	t.Run("defaults to metrics.rss", func(t *testing.T) {
		path, field, err := resolveInstanceGraphField(nil)
		require.NoError(t, err)
		require.Equal(t, "metrics.rss", path)
		require.Equal(t, instanceGraphSourceMetrics, field.Source)
	})

	t.Run("rejects more than one field", func(t *testing.T) {
		_, _, err := resolveInstanceGraphField([]string{"metrics.rss", "metrics.cpu"})
		require.ErrorContains(t, err, "multiple fields")
	})

	t.Run("rejects unsupported field", func(t *testing.T) {
		_, _, err := resolveInstanceGraphField([]string{"does.not.exist"})
		require.ErrorContains(t, err, "unsupported graph field")
		require.ErrorContains(t, err, "metrics.rss")
	})

	instance := Instance{}
	instance.Resources.Memory = 256
	instance.Resources.VCPUs = 4
	metrics := platform.GetInstancesMetricsResponseInstanceMetrics{
		RssBytes:  1024,
		CpuTimeMs: 500,
	}

	cases := []struct {
		path   string
		source instanceGraphSource
		want   float64
	}{
		{"metrics.rss", instanceGraphSourceMetrics, 1024},
		{"metrics.cpu", instanceGraphSourceMetrics, 500},
		{"resources.memory", instanceGraphSourceInstance, 256 * float64(units.MiB)},
		{"resources.vcpus", instanceGraphSourceInstance, 4},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			path, field, err := resolveInstanceGraphField([]string{tc.path})
			require.NoError(t, err)
			require.Equal(t, tc.path, path)
			require.Equal(t, tc.source, field.Source)

			switch tc.source {
			case instanceGraphSourceInstance:
				require.InDelta(t, tc.want, field.FromInstance(instance), 0)
			case instanceGraphSourceMetrics:
				require.InDelta(t, tc.want, field.FromMetrics(metrics), 0)
			}
		})
	}
}

func TestInstanceGraphAxisFormatter(t *testing.T) {
	t.Run("bytes", func(t *testing.T) {
		fmter := instanceGraphAxisFormatter(instanceGraphAxisFormatBytes)
		require.Equal(t, units.BytesSize(1536), fmter(0, 1536))
	})

	t.Run("duration", func(t *testing.T) {
		fmter := instanceGraphAxisFormatter(instanceGraphAxisFormatDuration)
		require.Equal(t, (1500 * time.Millisecond).String(), fmter(0, 1500))
	})

	t.Run("number", func(t *testing.T) {
		fmter := instanceGraphAxisFormatter(instanceGraphAxisFormatNumber)
		require.Equal(t, "4", fmter(0, 4))
	})
}
