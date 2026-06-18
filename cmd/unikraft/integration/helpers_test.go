// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/mitchellh/copystructure"
	"github.com/stretchr/testify/require"

	"unikraft.com/x/log"

	"unikraft.com/cli/internal/cmd"
	"unikraft.com/cli/internal/config"
	integ "unikraft.com/cli/internal/integration"
	"unikraft.com/cli/internal/resource"
)

var (
	uniqSeed    string
	uniqCounter atomic.Uint64
)

func init() {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	uniqSeed = hex.EncodeToString(b[:])
}

func uniq() string {
	n := uniqCounter.Add(1)
	return fmt.Sprintf("%s%06x", uniqSeed, n)
}

func runner(t *testing.T, online bool, configNames ...string) *integ.TestEnv {
	t.Helper()
	integ.SkipUnlessIntegration(t)
	t.Parallel()

	unikraftPath := integ.BuildUnikraft(t)

	baseCfg, err := integ.LoadConfig(t, configNames...)
	require.NoError(t, err)

	if online && baseCfg == nil {
		if len(configNames) == 0 {
			t.Skip("online test requires config, but no config found")
		}
		t.Skipf("online test requires %q, but no config found", configNames[0])
	}

	configName := "config.yaml"
	if len(configNames) > 0 {
		configName = configNames[0]
	}
	configPath := filepath.Join(t.TempDir(), configName)
	var testCfg *integ.Config
	if baseCfg != nil {
		cloned, err := copystructure.Copy(baseCfg)
		require.NoError(t, err)
		testCfg = cloned.(*integ.Config)
		testCfg.Config.Path = configPath
		require.NoError(t, testCfg.Config.Save())
	}

	ctx := t.Context()
	ctx = log.WithLogger(ctx, log.New(t.Output(), log.TextType, log.TraceLevel))

	sandboxPath := filepath.Join(t.TempDir(), "sandbox.json")
	t.Cleanup(func() {
		ctx := ctx
		if testCfg != nil {
			ctx = config.WithConfig(ctx, testCfg.Config)
		}

		if _, statErr := os.Stat(sandboxPath); os.IsNotExist(statErr) {
			return
		}

		sandbox, err := resource.LoadSandbox(sandboxPath, cmd.SandboxedResources...)
		require.NoError(t, err)
		require.NotNil(t, sandbox)

		require.NoError(t, sandbox.Teardown(context.WithoutCancel(ctx)))
	})

	return integ.NewTestEnv(t, unikraftPath).
		WithConfig(testCfg, configPath).
		WithSandboxPath(sandboxPath)
}
