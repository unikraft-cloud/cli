// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cli/internal/config"
)

func TestBuilderBuildURL(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"builders": [{"name": "default", "default": true}]}`)
	}))
	t.Cleanup(srv.Close)

	list := func(buildURL, remote string) error {
		profile := "{type: cloud, token: a}"
		if buildURL != "" {
			profile = "{type: cloud, token: a, build_url: " + buildURL + "}"
		}
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte("profile: a\nprofiles:\n  a: "+profile+"\n"), 0o600))
		cfg, err := loadConfig(path, "")
		require.NoError(t, err)
		ctx := context.WithValue(config.WithConfig(t.Context(), cfg), builderRemoteKey{}, remote)
		_, err = Builder{}.List(ctx)
		return err
	}

	assert.NoError(t, list(srv.URL, ""))
	assert.NoError(t, list("http://127.0.0.1:1", srv.URL))
	assert.ErrorContains(t, list("", ""), "set build_url in profile a, or pass --remote")
}
