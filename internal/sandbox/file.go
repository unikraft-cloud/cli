// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package sandbox

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/docker/go-units"

	plugin "unikraft.com/cloud/plugins/sandbox"
	"unikraft.com/x/log"
)

const maxFileSize = 32 * units.MiB

func StatLocalFile(local, verb string) error {
	info, err := os.Stat(local)
	if err != nil {
		if target, linkErr := os.Readlink(local); linkErr == nil && errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("local file %q is a symlink to %q, which does not exist", local, target)
		}
		return fmt.Errorf("reading local file %q: %w", local, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%q is a directory: only single files can be %s", local, verb)
	}
	return checkFileSize(local, info.Size())
}

func checkFileSize(name string, size int64) error {
	if size > maxFileSize {
		return fmt.Errorf("%q is %d bytes, over the %s (%d bytes) limit for a single transfer: split it, or stream it with \"unikraft instance exec\"",
			name, size, units.BytesSize(maxFileSize), int64(maxFileSize))
	}
	return nil
}

type UploadOpts struct {
	Local   string
	Remote  string
	Append  bool
	Parents bool
}

func (t Target) Upload(ctx context.Context, opts UploadOpts) (string, error) {
	data, err := os.ReadFile(opts.Local)
	if err != nil {
		return "", fmt.Errorf("reading local file %q: %w", opts.Local, err)
	}

	filename := filepath.Base(opts.Local)

	remote := opts.Remote
	if strings.HasSuffix(remote, "/") {
		remote = path.Join(remote, filename)
	}

	if opts.Parents {
		if err := t.Mkdir(ctx, path.Dir(remote), true); err != nil {
			return "", fmt.Errorf("creating parent directories: %w", err)
		}
	}

	if err := t.WriteFile(ctx, remote, data, opts.Append); err != nil {
		if !strings.Contains(err.Error(), "Is a directory") {
			return "", err
		}
		remote = path.Join(remote, filename)
		if err := t.WriteFile(ctx, remote, data, opts.Append); err != nil {
			return "", err
		}
	}

	return remote, nil
}

type DownloadOpts struct {
	Remote  string
	Local   string
	Force   bool
	Parents bool
}

func (t Target) Download(ctx context.Context, opts DownloadOpts) (string, int, error) {
	data, err := t.ReadFile(ctx, opts.Remote)
	if err != nil {
		return "", 0, err
	}
	local := opts.Local
	if local == "" {
		local = path.Base(opts.Remote)
	} else if info, err := os.Stat(local); err == nil && info.IsDir() {
		local = filepath.Join(local, path.Base(opts.Remote))
	} else if strings.HasSuffix(local, string(os.PathSeparator)) {
		local = filepath.Join(local, path.Base(opts.Remote))
	}

	if opts.Parents {
		if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
			return "", 0, fmt.Errorf("creating local parent directories for %q: %w", local, err)
		}
	}

	const flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	perm := os.FileMode(0o644)
	keepPerm := false
	f, err := os.OpenFile(local, flags, perm)
	if errors.Is(err, os.ErrExist) {
		if !opts.Force {
			return "", 0, fmt.Errorf("local file %q already exists (use --force to overwrite)", local)
		}
		if info, statErr := os.Lstat(local); statErr == nil && info.Mode().IsRegular() {
			perm, keepPerm = info.Mode().Perm(), true
		}
		if err := os.Remove(local); err != nil {
			return "", 0, fmt.Errorf("replacing local file %q: %w", local, err)
		}
		f, err = os.OpenFile(local, flags, perm)
	}
	if err != nil {
		return "", 0, fmt.Errorf("writing local file %q: %w", local, err)
	}
	defer f.Close()
	if keepPerm {
		if err := f.Chmod(perm); err != nil {
			return "", 0, fmt.Errorf("restoring permissions on %q: %w", local, err)
		}
	}
	if _, err := f.Write(data); err != nil {
		return "", 0, fmt.Errorf("writing local file %q: %w", local, err)
	}
	if err := f.Close(); err != nil {
		return "", 0, fmt.Errorf("writing local file %q: %w", local, err)
	}

	return local, len(data), nil
}

func (t Target) WriteFile(ctx context.Context, remotePath string, data []byte, appendFile bool) error {
	log.G(ctx).Trace().Msg("writing file")

	req := plugin.WriteFileRequest{
		Path:     remotePath,
		Append:   appendFile,
		Encoding: plugin.FileEncodingBase64,
		Data:     base64.StdEncoding.EncodeToString(data),
	}
	if _, err := t.Client.WriteFile(ctx, t.Instance, &req, t.Opts...); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}

func (t Target) ReadFile(ctx context.Context, remotePath string) ([]byte, error) {
	log.G(ctx).Trace().Msg("reading file")

	req := plugin.ReadFileRequest{
		Path: remotePath,
	}
	resp, err := t.Client.ReadFile(ctx, t.Instance, &req, t.Opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("failed to read file: the %q plugin returned no contents", t.Plugin)
	}

	contents, err := base64.StdEncoding.DecodeString(resp.Data.Contents)
	if err != nil {
		return nil, fmt.Errorf("decoding file contents: %w", err)
	}
	return contents, nil
}

func (t Target) Mkdir(ctx context.Context, dir string, parents bool) error {
	log.G(ctx).Trace().Msg("creating directory")

	req := plugin.MkdirRequest{
		Path:    dir,
		Parents: parents,
	}
	if _, err := t.Client.CreateDirectory(ctx, t.Instance, &req, t.Opts...); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	return nil
}
