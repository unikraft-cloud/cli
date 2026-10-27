// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"os"
	"slices"
	"sync"
	"testing"

	"github.com/mitchellh/copystructure"
	"unikraft.com/cli/internal/config"
	"unikraft.com/x/ptr"
)

func LoadRemote(t *testing.T) (*Remote, error) {
	return populate()
}

type Remote struct {
	Config  *config.Config
	Profile *config.Profile

	Metro     *config.Metro
	MetroName string
}

var (
	cfg     *Remote
	once    sync.Once
	onceErr error
)

func populate() (*Remote, error) {
	once.Do(func() {
		path, err := config.ConfigFilePath()
		if err != nil {
			onceErr = err
			return
		}
		baseCfg, err := config.Load(path)
		if err != nil || baseCfg == nil {
			onceErr = err
			return
		}
		if profileName := os.Getenv("UNIKRAFT_PROFILE"); profileName != "" {
			baseCfg.OverrideCurrentProfile(profileName)
		}

		profile, err := baseCfg.CurrentProfile()
		if err != nil {
			onceErr = err
			return
		}

		profile.Name = "default"
		if len(profile.Metros) == 0 {
			return
		}
		profile.Type = config.ProfileTypeCloud
		profile.ControlPlane = ""
		profile.MetroDefault = ""
		profile.Metros = profile.Metros[:1]
		profile.Metros[0].Name = "test"
		profile.Metros[0].Location = "xxx"
		profile.Metros[0].Insecure = new(ptr.ZeroIfNil(profile.Metros[0].Insecure))

		config := &config.Config{
			DefaultProfile: profile.Name,
			Profiles:       map[string]config.Profile{profile.Name: *profile},
		}

		cfg = &Remote{
			Config:    config,
			Profile:   profile,
			Metro:     &profile.Metros[0],
			MetroName: profile.Metros[0].Name,
		}
	})
	if onceErr != nil {
		return nil, onceErr
	}

	cloned, err := copystructure.Copy(cfg)
	if err != nil {
		return nil, err
	}
	return cloned.(*Remote), nil
}

// GetTestServer returns nil only when UNIKRAFT_X_TEST_SERVER is set and not
// present in servers. When the env var is unset/empty, it returns a pointer
// to "" (meaning "no specific server selected").
func GetTestServer(servers []string) *string {
	server := os.Getenv("UNIKRAFT_X_TEST_SERVER")
	if server == "" {
		return new(string)
	}

	if slices.Contains(servers, server) {
		return &server
	}

	return nil
}
