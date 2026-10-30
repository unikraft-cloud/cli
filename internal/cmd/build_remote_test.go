// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/moby/buildkit/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	buildapi "unikraft.com/cloud/sdk/build"
	"unikraft.com/x/build/buildflags"
)

func TestRemoteRequest(t *testing.T) {
	t.Parallel()

	project := func(t *testing.T, files ...string) string {
		dir := t.TempDir()
		for _, f := range files {
			require.NoError(t, os.WriteFile(filepath.Join(dir, f), nil, 0o644))
		}
		return dir
	}

	for name, tc := range map[string]struct {
		files []string
		input string
		cmd   ImageBuildCmd
		want  string
	}{
		"a project reads its Kraftfile from the session, with the flags as fields": {
			files: []string{"kraft.yaml", "Dockerfile"},
			cmd: ImageBuildCmd{
				Output:   "my-org/my-app:latest",
				Arch:     []string{"arm64"},
				BuildArg: []string{"VERSION=1.2.3"},
				NoCache:  true,
				Secret:   []string{"id=npm,src=/home/me/.npmrc"},
				SSH:      []string{"default"},
			},
			want: `{
				"kraftfile": {"session": "context", "filename": "kraft.yaml"},
				"contexts": {"context": {"session": "context"}},
				"platforms": ["arm64"],
				"build_args": {"VERSION": "1.2.3"},
				"no_cache": true,
				"secrets": {"npm": {"session": "npm"}},
				"ssh": {"default": {"session": "default"}},
				"registries": {"*": {"session": true}},
				"output": [{"image": "index.unikraft.io/my-org/my-app:latest"}]
			}`,
		},
		"a project without a Kraftfile builds its Dockerfile": {
			files: []string{"Dockerfile"},
			cmd:   ImageBuildCmd{Output: "ghcr.io/acme/app:1"},
			want: `{
				"contexts": {"context": {"session": "context"}},
				"no_cache": false,
				"registries": {"*": {"session": true}},
				"output": [{"image": "ghcr.io/acme/app:1"}]
			}`,
		},
		"a Dockerfile is built from its directory": {
			files: []string{"app.Dockerfile"},
			input: "app.Dockerfile",
			cmd:   ImageBuildCmd{Output: "ghcr.io/acme/app:1"},
			want: `{
				"contexts": {
					"context": {"session": "context"},
					"dockerfile": {"session": "context", "filename": "app.Dockerfile"}
				},
				"no_cache": false,
				"registries": {"*": {"session": true}},
				"output": [{"image": "ghcr.io/acme/app:1"}]
			}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := project(t, tc.files...)
			tc.cmd.Input = filepath.Join(dir, tc.input)
			secrets, err := buildflags.ParseSecretSpecs(tc.cmd.Secret)
			require.NoError(t, err)
			ssh, err := buildflags.ParseSSHSpecs(tc.cmd.SSH)
			require.NoError(t, err)

			req, served, err := tc.cmd.remoteRequest(secrets, ssh)
			require.NoError(t, err)
			assert.Equal(t, dir, served)

			b, err := json.Marshal(req)
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(b))
		})
	}

	for name, cmd := range map[string]ImageBuildCmd{
		"an output on disk":    {Output: "oci-archive://app.tar"},
		"insecure registries":  {Output: "my-org/my-app", Insecure: []string{}},
		"a plain http service": {Remote: "http://localhost:8080", Output: "my-org/my-app"},
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			t.Parallel()

			cmd.Input = project(t, "Dockerfile")
			_, _, err := cmd.remoteRequest(nil, nil)
			require.Error(t, err)
		})
	}
}

func TestFollowRemoteCancelsARefusedSession(t *testing.T) {
	t.Parallel()

	cancelled := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/builds":
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id": "b1", "status": "waiting", "session": "/v1/builds/b1/session"}`)
		case "/v1/builds/b1/session":
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error": {"code": "invalid", "message": "the session does not serve Secrets/GetSecret"}}`)
		case "/v1/builds/b1/cancel":
			cancelled <- struct{}{}
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"id": "b1", "status": "waiting"}`)
		}
	}))
	t.Cleanup(srv.Close)

	client, err := buildapi.New(srv.URL)
	require.NoError(t, err)
	s, err := session.NewSession(t.Context(), "")
	require.NoError(t, err)

	err = followRemote(t.Context(), client, buildapi.CreateBuildRequest{}, s, nil)
	var apiErr *buildapi.APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, buildapi.ErrorCodeInvalid, apiErr.Code)
	assert.Len(t, cancelled, 1)
}
