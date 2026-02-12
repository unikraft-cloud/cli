// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package kraftfile

import (
	"cmp"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/mod/semver"
	"mvdan.cc/sh/v3/shell"
	"sigs.k8s.io/yaml"
)

const (
	SpecVersionMin = "v0.6.0" // XXX: ONLY FOR TESTING
	SpecVersionMax = "v0.7.0"
)

// ParseBytes parses a Kraftfile from bytes and validates the spec version.
func ParseBytes(data []byte) (*Kraftfile, error) {
	var header struct {
		Spec          string `json:"spec,omitempty"`
		Specification string `json:"specification,omitempty"`
	}
	if err := yaml.Unmarshal(data, &header); err != nil {
		return nil, err
	}
	spec, err := normalizeSpec(cmp.Or(header.Spec, header.Specification))
	if err != nil {
		return nil, err
	}

	var kf Kraftfile
	if err := yaml.Unmarshal(data, &kf); err != nil {
		return nil, err
	}
	kf.Spec = spec
	return &kf, nil
}

func normalizeSpec(spec string) (string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", fmt.Errorf("missing 'spec' version attribute")
	}
	if !strings.HasPrefix(spec, "v") {
		spec = "v" + spec
	}

	if semver.Compare(spec, SpecVersionMin) < 0 || semver.Compare(spec, SpecVersionMax) > 0 {
		return "", fmt.Errorf("unsupported spec version %q", spec)
	}
	return semver.MajorMinor(spec), nil
}

func (cmd *Command) UnmarshalJSON(data []byte) error {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	switch value := raw.(type) {
	case nil:
		return nil
	case string:
		fields, err := shell.Fields(value, func(field string) string { return "" })
		if err != nil {
			return fmt.Errorf("could not parse cmd string: %w", err)
		}
		*cmd = fields
		return nil
	case []any:
		list := make([]string, 0, len(value))
		for _, item := range value {
			str, ok := item.(string)
			if !ok {
				return fmt.Errorf("cmd array entries must be strings")
			}
			list = append(list, str)
		}
		*cmd = list
		return nil
	default:
		return fmt.Errorf("invalid cmd value type %T", raw)
	}
}

func (m *Map) UnmarshalJSON(data []byte) error {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	switch value := raw.(type) {
	case nil:
		return nil
	case []any:
		items := make([]MapPair, 0, len(value))
		for _, item := range value {
			str, ok := item.(string)
			if !ok {
				return fmt.Errorf("map list entries must be strings")
			}
			key, val := splitMapPair(str)
			items = append(items, MapPair{Key: key, Value: val})
		}
		*m = items
		return nil
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		items := make([]MapPair, 0, len(keys))
		for _, key := range keys {
			value := value[key]
			switch value.(type) {
			case string, float64, bool, nil:
			default:
				return fmt.Errorf("map values must be scalar")
			}
			items = append(items, MapPair{Key: key, Value: value})
		}
		*m = items
		return nil
	default:
		return fmt.Errorf("invalid map value type %T", raw)
	}
}

func splitMapPair(value string) (string, any) {
	parts := strings.SplitN(value, "=", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return value, nil
}

func (ref *Unikraft) UnmarshalJSON(data []byte) error {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	switch value := raw.(type) {
	case nil:
		return nil
	case string, float64, bool:
		ref.Version = fmt.Sprint(value)
		return nil
	case map[string]any:
		type alias Unikraft
		return json.Unmarshal(data, (*alias)(ref))
	default:
		return fmt.Errorf("invalid component value type %T", raw)
	}
}

func (ref *Library) UnmarshalJSON(data []byte) error {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	switch value := raw.(type) {
	case nil:
		return nil
	case string, float64, bool:
		ref.Version = fmt.Sprint(value)
		return nil
	case map[string]any:
		type alias Library
		return json.Unmarshal(data, (*alias)(ref))
	default:
		return fmt.Errorf("invalid library value type %T", raw)
	}
}

func (ref *Template) UnmarshalJSON(data []byte) error {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	switch value := raw.(type) {
	case nil:
		return nil
	case string, float64, bool:
		ref.Source = fmt.Sprint(value)
		return nil
	case map[string]any:
		type alias Template
		return json.Unmarshal(data, (*alias)(ref))
	default:
		return fmt.Errorf("invalid template value type %T", raw)
	}
}

func (rootfs *Rootfs) UnmarshalJSON(data []byte) error {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	switch raw.(type) {
	case nil:
		return nil
	case string:
		if err := json.Unmarshal(data, &rootfs.Source); err != nil {
			return err
		}
	case map[string]any:
		type alias Rootfs
		if err := json.Unmarshal(data, (*alias)(rootfs)); err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid rootfs value type %T", raw)
	}

	rootfs.Type = cmp.Or(rootfs.Type, FsTypeCpio)
	return nil
}

func (fsType *FsType) UnmarshalJSON(data []byte) error {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	switch value := raw.(type) {
	case nil:
		return nil
	case string:
		switch value {
		case "cpio", "erofs":
			*fsType = FsType(value)
		default:
			return fmt.Errorf("invalid fs typ %q", value)
		}
		return nil
	default:
		return fmt.Errorf("invalid fsType value type %T", raw)
	}
}

func (volumes *Volumes) UnmarshalJSON(data []byte) error {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	switch raw.(type) {
	case nil:
		return nil
	case []any:
		var list []Volume
		if err := json.Unmarshal(data, &list); err != nil {
			return err
		}
		*volumes = list
		return nil
	default:
		return fmt.Errorf("invalid volumes value type %T", raw)
	}
}

func (volume *Volume) UnmarshalJSON(data []byte) error {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	switch value := raw.(type) {
	case nil:
		return nil
	case string:
		if err := volume.UnmarshalText([]byte(value)); err != nil {
			return err
		}
		return nil
	case map[string]any:
		type alias Volume
		return json.Unmarshal(data, (*alias)(volume))
	default:
		return fmt.Errorf("invalid volume value type %T", raw)
	}
}

func (target *Target) UnmarshalJSON(data []byte) error {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	switch value := raw.(type) {
	case nil:
		return nil
	case string:
		return parseTargetString(value, target)
	case map[string]any:
		type alias Target
		if err := json.Unmarshal(data, (*alias)(target)); err != nil {
			return err
		}
		var rawMap map[string]any
		if err := json.Unmarshal(data, &rawMap); err != nil {
			return err
		}
		if target.Arch == "" {
			if value, ok := rawMap["architecture"]; ok {
				arch, ok := value.(string)
				if !ok {
					return fmt.Errorf("architecture must be a string")
				}
				target.Arch = arch
			}
		}
		if target.Plat == "" {
			if value, ok := rawMap["platform"]; ok {
				plat, ok := value.(string)
				if !ok {
					return fmt.Errorf("platform must be a string")
				}
				target.Plat = plat
			}
		}
		return nil
	default:
		return fmt.Errorf("invalid target value type %T", raw)
	}
}

func parseTargetString(value string, target *Target) error {
	plat, arch, ok := strings.Cut(value, "/")
	if !ok {
		return fmt.Errorf("invalid target string %q: expected format 'platform/architecture'", value)
	}
	target.Plat = plat
	target.Arch = arch
	return nil
}

func (volume *Volume) UnmarshalText(text []byte) error {
	entry := string(text)
	parts := strings.SplitN(entry, ":", 3)
	if len(parts) == 1 {
		// When no colon is specified, assume the root file system
		volume.Source = entry
		volume.Destination = "/"
		return nil
	}
	volume.Source = parts[0]
	volume.Destination = parts[1]
	if len(parts) == 3 {
		volume.Mode = parts[2]
	}
	return nil
}
