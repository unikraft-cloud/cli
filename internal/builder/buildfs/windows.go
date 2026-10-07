// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package buildfs

import (
	"io/fs"
	"strings"
)

// WindowsDirFS gives a Windows directory the modes BuildKit gives it.  NTFS
// keeps no executable bit, so each mode gets 0111 and loses group and other write.
func WindowsDirFS(fsys fs.FS) fs.FS {
	return windowsFS{fsys}
}

type windowsFS struct {
	fs.FS
}

func (w windowsFS) Stat(name string) (fs.FileInfo, error) {
	info, err := fs.Stat(w.FS, name)
	if err != nil {
		return nil, err
	}
	return windowsInfo{info}, nil
}

func (w windowsFS) Lstat(name string) (fs.FileInfo, error) {
	info, err := fs.Lstat(w.FS, name)
	if err != nil {
		return nil, err
	}
	return windowsInfo{info}, nil
}

func (w windowsFS) ReadDir(name string) ([]fs.DirEntry, error) {
	entries, err := fs.ReadDir(w.FS, name)
	for i, entry := range entries {
		entries[i] = windowsEntry{entry}
	}
	return entries, err
}

// ReadLink gives the target with forward slashes, as the guest reads it.
func (w windowsFS) ReadLink(name string) (string, error) {
	target, err := fs.ReadLink(w.FS, name)
	return strings.ReplaceAll(target, `\`, "/"), err
}

type windowsEntry struct {
	fs.DirEntry
}

func (e windowsEntry) Info() (fs.FileInfo, error) {
	info, err := e.DirEntry.Info()
	if err != nil {
		return nil, err
	}
	return windowsInfo{info}, nil
}

type windowsInfo struct {
	fs.FileInfo
}

func (i windowsInfo) Mode() fs.FileMode {
	mode := i.FileInfo.Mode()
	return mode&^fs.ModePerm | (mode.Perm()|0o111)&0o755
}
