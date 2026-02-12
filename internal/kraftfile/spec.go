// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package kraftfile

import "fmt"

// Kraftfile represents a parsed Kraftfile
type Kraftfile struct {
	Spec string `json:"-"`

	Name    string   `json:"name,omitempty"`
	Targets []Target `json:"targets,omitempty"`

	Cmd    Command           `json:"cmd,omitempty"`
	Env    Map               `json:"env,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`

	Rootfs   *Rootfs   `json:"rootfs,omitempty"`
	Unikraft *Unikraft `json:"unikraft,omitempty"`
	Roms     []string  `json:"roms,omitempty"`

	Volumes   Volumes            `json:"volumes,omitempty"`
	Runtime   string             `json:"runtime,omitempty"`
	Template  *Template          `json:"template,omitempty"`
	Libraries map[string]Library `json:"libraries,omitempty"`

	// Deprecated: OutDir is no longer supported.
	OutDir string `json:"outdir,omitempty"`
}

// Command supports a string or array form.
type Command []string

// Map stores ordered key/value pairs from list or map inputs.
type Map []MapPair

// MapPair represents a single key/value pair.
type MapPair struct {
	Key   string
	Value any
}

// Get returns the value for a key or nil if missing.
func (m Map) Get(key string) any {
	value, _ := m.Lookup(key)
	return value
}

// Lookup returns the value and whether the key exists.
func (m Map) Lookup(key string) (any, bool) {
	for _, item := range m {
		if item.Key == key {
			return item.Value, true
		}
	}
	return nil, false
}

func (m Map) AsMap() map[string]any {
	result := make(map[string]any, len(m))
	for _, item := range m {
		result[item.Key] = item.Value
	}
	return result
}

func (m Map) AsStringMap() map[string]string {
	result := make(map[string]string, len(m))
	for _, item := range m {
		var valueStr string
		switch v := item.Value.(type) {
		case string:
			valueStr = v
		case nil:
			valueStr = ""
		default:
			valueStr = fmt.Sprint(v)
		}
		result[item.Key] = valueStr
	}
	return result
}

// Unikraft supports scalar or structured component references.
type Unikraft struct {
	Source  string `json:"source,omitempty"`
	Version any    `json:"version,omitempty"`
	KConfig Map    `json:"kconfig,omitempty"`
}

// Template extends component references with a template name.
type Template struct {
	Source  string `json:"source,omitempty"`
	Version any    `json:"version,omitempty"`
}

// Library supports scalar or structured component references.
type Library struct {
	Source  string `json:"source,omitempty"`
	Version any    `json:"version,omitempty"`
	KConfig Map    `json:"kconfig,omitempty"`
}

// Rootfs supports a string or structured rootfs object.
type Rootfs struct {
	Type   FsType `json:"type,omitempty"`
	Source string `json:"source,omitempty"`
}

type FsType string

const (
	FsTypeCpio  = FsType("cpio")
	FsTypeErofs = FsType("erofs")
)

func (fsType FsType) String() string {
	return string(fsType)
}

// Volumes supports a string or list of volume entries.
type Volumes []Volume

type Volume struct {
	Driver      string `json:"driver,omitempty"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination,omitempty"`
	Mode        any    `json:"mode,omitempty"`
	ReadOnly    bool   `json:"readonly,omitempty"`
}

// Target supports shorthand or structured target entries.
type Target struct {
	Arch    string `json:"arch,omitempty"`
	Plat    string `json:"plat,omitempty"`
	KConfig Map    `json:"kconfig,omitempty"`
}
