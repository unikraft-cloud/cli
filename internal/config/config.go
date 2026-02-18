// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package config

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	jujuerrors "github.com/juju/errors"
	"sigs.k8s.io/yaml"

	"unikraft.com/cli/internal/yamlmerge"
)

// Config represents the global configuration for the Unikraft CLI.
type Config struct {
	Path string `json:"-"`

	DefaultProfile Interpolate[string] `json:"profile"`
	Profiles       map[string]Profile  `json:"profiles"`

	selectedProfile string
}

// defaultConfigFilename is the default name of the configuration file used by
// the Unikraft CLI.
var defaultConfigFilename = "config.yaml"

// ConfigFilePath returns the path to the Unikraft CLI configuration file.
func ConfigFilePath() (string, error) {
	if path, ok := os.LookupEnv("UNIKRAFT_CONFIG"); ok {
		return path, nil
	}

	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return "", jujuerrors.Annotate(err, "getting user config dir")
	}
	return filepath.Join(userConfigDir, "unikraft", defaultConfigFilename), nil
}

// Save the current configuration to the configuration file.
func (c *Config) Save() error {
	if c.Path == "" {
		return jujuerrors.New("config file path is not set")
	}

	updated, err := mergeConfig(c)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(c.Path), 0o755); err != nil {
		return jujuerrors.Annotate(err, "creating config directory")
	}
	f, err := os.Create(c.Path)
	if err != nil {
		return jujuerrors.Annotate(err, "opening config file")
	}

	if _, err := f.Write(updated); err != nil {
		f.Close()
		return jujuerrors.Annotate(err, "writing config file")
	}
	if err := f.Close(); err != nil {
		return jujuerrors.Annotate(err, "closing config file")
	}
	return nil
}

// Load reads the configuration from the specified file path.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, jujuerrors.Annotate(err, "opening config file")
	}
	defer f.Close()

	dt, err := io.ReadAll(f)
	if err != nil {
		return nil, jujuerrors.Annotate(err, "reading config file")
	}

	c := Config{}
	if err := yaml.Unmarshal(dt, &c); err != nil {
		return nil, jujuerrors.Annotate(err, "decoding config file")
	}

	var validationErrs error
	for name, profile := range c.Profiles {
		profile.Name = name
		if err := profile.Validate(); err != nil {
			validationErrs = errors.Join(validationErrs, jujuerrors.Annotatef(err, "validating profile %q", name))
		}
		c.Profiles[name] = profile
	}
	if validationErrs != nil {
		return nil, validationErrs
	}

	c.Path = path
	return &c, nil
}

func mergeConfig(c *Config) ([]byte, error) {
	desired, err := yaml.Marshal(c)
	if err != nil {
		return nil, jujuerrors.Annotate(err, "marshalling config to yaml")
	}

	existing, err := os.ReadFile(c.Path)
	if errors.Is(err, os.ErrNotExist) {
		return desired, nil
	}
	if err != nil {
		return nil, jujuerrors.Annotate(err, "reading config file")
	}

	return yamlmerge.MergeYAML(existing, desired)
}
