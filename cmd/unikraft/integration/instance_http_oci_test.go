// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integ "unikraft.com/cli/internal/integration"
)

// httpOCIServer serves a volume mounted at /data as http+oci layouts: the node
// only fetches a layout if it is served with the layout media type.
var httpOCIServer = &integ.SharedImage{
	Name: "http-oci-server-e2e",
	Files: map[string]string{
		"main.go": `package main

import "net/http"

func main() {
	files := http.FileServer(http.Dir("/data"))
	http.ListenAndServe(":8080", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.oci.layout.v1+tar")
		files.ServeHTTP(w, r)
	}))
}
`,
		"Dockerfile": `FROM golang:1.26-alpine AS build
COPY main.go /src/main.go
RUN cd /src && CGO_ENABLED=0 go build -o /server main.go

FROM scratch
COPY --from=build /server /server
`,
		"Kraftfile": `spec: v0.7
name: http-oci-server-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source: ./Dockerfile
cmd: ["/server"]
`,
	},
}

func TestInstancesHTTPOCI(t *testing.T) {
	r := runner(t, true, []string{staging, stable})
	payload := integ.Busybox.Build(t, r)
	server := httpOCIServer.Build(t, r)
	volName := "test-" + uniq()
	serverName := "test-" + uniq()
	instName := "test-" + uniq()

	layout := filepath.Join(t.TempDir(), "layout")
	r.Run(t, []string{"unikraft", "image", "copy", payload, "oci-layout://" + layout})

	repository := r.Config.Profile.Organization + "/http-oci-e2e"
	content := t.TempDir()
	dgst := tarLayout(t, layout, filepath.Join(content, repository))

	r.Run(t, []string{
		"unikraft", "volume", "create", "--output", "quiet",
		"--set", "name=" + volName, "--set", "size=64", "--set", "metro=" + r.Config.MetroName,
	})
	r.Run(t, []string{"unikraft", "--timeout", "30s", "volume", "wait", "--until", "state==available", volName})
	r.Run(t, []string{"unikraft", "volume", "import", volName, "--source", content})

	r.Run(t, []string{
		"unikraft", "run", "--name", serverName, "--metro", r.Config.MetroName, "--output", "quiet",
		"--image", server, "-p", "80:8080/http", "-v", volName + ":/data",
	})
	r.Run(t, []string{"unikraft", "--timeout", "30s", "instance", "wait", "--until", "state==running", serverName})
	fqdn := strings.TrimSpace(r.Run(t, []string{
		"unikraft", "instance", "inspect", serverName,
		"--output", "template={{ range .service.domains }}{{ .fqdn }}{{ end }}",
	}))
	require.NotEmpty(t, fqdn)

	image := "http+oci://" + fqdn + "/" + repository + "/@" + dgst
	r.Run(t, []string{
		"unikraft", "run", "--name", instName, "--metro", r.Config.MetroName, "--output", "quiet",
		"--image", image, "--args", "echo UNIKRAFT_HTTP_OCI_OK",
	})
	r.Run(t, []string{"unikraft", "--timeout", "60s", "instance", "wait", "--until", "state==stopped", instName})

	out := r.Run(t, []string{"unikraft", "instance", "inspect", instName})
	assert.Contains(t, out, image)
	out = r.Run(t, []string{"unikraft", "instance", "logs", instName})
	assert.Contains(t, out, "UNIKRAFT_HTTP_OCI_OK")

	r.Run(t, []string{"unikraft", "instance", "delete", instName})
	r.Run(t, []string{"unikraft", "instance", "delete", serverName})
}

// tarLayout packs the layout at src into dir, named by its digest as http+oci
// addresses it.
//
// HACK: the node's unpacker only takes an index.json that lists the manifest
// itself, and a tarball with every directory before its contents, root
// included - neither of which the CLI's layouts give it (TOOL-369).
func tarLayout(t *testing.T, src, dir string) string {
	t.Helper()
	flattenIndex(t, src)
	require.NoError(t, os.MkdirAll(dir, 0o755))

	f, err := os.CreateTemp(dir, "layout-*")
	require.NoError(t, err)
	defer f.Close()

	h := sha256.New()
	tw := tar.NewWriter(io.MultiWriter(f, h))
	require.NoError(t, filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		hdr.Name = "./" + filepath.ToSlash(rel)
		if rel == "." {
			hdr.Name = "./"
		} else if d.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		r, err := os.Open(path)
		if err != nil {
			return err
		}
		defer r.Close()
		_, err = io.Copy(tw, r)
		return err
	}))
	require.NoError(t, tw.Close())

	dgst := "sha256:" + hex.EncodeToString(h.Sum(nil))
	require.NoError(t, os.Rename(f.Name(), filepath.Join(dir, "@"+dgst)))
	return dgst
}

// flattenIndex lifts the manifests of a nested index up into index.json.
func flattenIndex(t *testing.T, layout string) {
	t.Helper()

	var index ocispec.Index
	data, err := os.ReadFile(filepath.Join(layout, ocispec.ImageIndexFile))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &index))
	require.Len(t, index.Manifests, 1)
	if index.Manifests[0].MediaType != ocispec.MediaTypeImageIndex {
		return
	}

	var nested ocispec.Index
	desc := index.Manifests[0]
	data, err = os.ReadFile(filepath.Join(layout, ocispec.ImageBlobsDir, desc.Digest.Algorithm().String(), desc.Digest.Encoded()))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &nested))

	index.Manifests = nested.Manifests
	for i := range index.Manifests {
		if index.Manifests[i].Annotations == nil {
			index.Manifests[i].Annotations = map[string]string{}
		}
		maps.Copy(index.Manifests[i].Annotations, desc.Annotations)
	}
	data, err = json.Marshal(index)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(layout, ocispec.ImageIndexFile), data, 0o644))
}
