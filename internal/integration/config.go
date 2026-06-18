// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mitchellh/copystructure"
	"unikraft.com/cli/internal/config"
	"unikraft.com/x/ptr"
)

func LoadConfig(t *testing.T, names ...string) (*Config, error) {
	if len(names) > 0 {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		return loadConfig(filepath.Join(configDir, "unikraft", names[0]))
	}

	return populate()
}

type Config struct {
	Config  *config.Config
	Profile *config.Profile

	Metro     *config.Metro
	MetroName string
}

var (
	cfg     *Config
	once    sync.Once
	onceErr error
)

func populate() (*Config, error) {
	once.Do(func() {
		path, err := config.ConfigFilePath()
		if err != nil {
			onceErr = err
			return
		}
		cfg, onceErr = loadConfig(path)
	})
	if onceErr != nil {
		return nil, onceErr
	}

	cloned, err := copystructure.Copy(cfg)
	if err != nil {
		return nil, err
	}
	return cloned.(*Config), nil
}

func loadConfig(path string) (*Config, error) {
	baseCfg, err := config.Load(path)
	if err != nil || baseCfg == nil {
		return nil, err
	}
	if profileName := os.Getenv("UNIKRAFT_PROFILE"); profileName != "" {
		baseCfg.OverrideCurrentProfile(profileName)
	}

	profile, err := baseCfg.CurrentProfile()
	if err != nil {
		return nil, err
	}

	profile.Name = "default"
	if len(profile.Metros) == 0 {
		return nil, nil
	}
	profile.ControlPlane = ""
	profile.Metros = profile.Metros[:1]
	profile.Metros[0].Name = "test"
	profile.Metros[0].Country = "xx"
	profile.Metros[0].Insecure = new(ptr.ZeroIfNil(profile.Metros[0].Insecure))

	config := &config.Config{
		DefaultProfile: profile.Name,
		Profiles:       map[string]config.Profile{profile.Name: *profile},
	}

	return &Config{
		Config:    config,
		Profile:   profile,
		Metro:     &profile.Metros[0],
		MetroName: profile.Metros[0].Name,
	}, nil
}
