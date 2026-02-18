// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package config

import (
	"cmp"
	"fmt"
	"net/url"
	"strings"

	"github.com/MakeNowJust/heredoc"
	jujuerrors "github.com/juju/errors"
)

// DefaultProfile is the name of the default profile used by the Unikraft CLI.
// It is used when no specific profile is set or when the user has not created
// any profiles yet.
const DefaultProfile = "default"

// ErrNotSetup is returned when there are no profiles available in the
// configuration.
var ErrNotSetup = jujuerrors.New(heredoc.Docf(`
		profile not setup;

		use %[1]sunikraft login%[1]s to get started,

		or visit https://unikraft.com/docs/cli for more information`, "`"))

type ErrProfileNotFound struct {
	Name string
}

func (e ErrProfileNotFound) Error() string {
	return fmt.Sprintf("profile %q not found", e.Name)
}

// ProfileType represents the type of profile used in the Unikraft CLI.
// It can be either "local" for local profiles or "cloud" for cloud-based
// profiles.
type ProfileType string

const (
	// ProfileTypeLocal indicates a local profile, which is typically used for
	// development and testing purposes on the user's machine.
	ProfileTypeLocal ProfileType = "local"

	// ProfileTypeCloud indicates a cloud profile, which is used for accessing
	// cloud-based resources and services provided by Unikraft.
	ProfileTypeCloud ProfileType = "cloud"
)

// Profile represents a user profile configuration for the Unikraft CLI.
type Profile struct {
	// Name of the profile
	Name string `json:"-"`
	// Type of the profile
	Type Interpolate[string] `json:"type"`
	// Token is the authentication token associated with the profile, used for
	// authenticating with Unikraft Cloud services.
	Token Interpolate[string] `json:"token"`
	// Organization is the organization associated with the profile.
	Organization Interpolate[string] `json:"organization,omitzero"`
	// ControlPlane is the endpoint for the control plane associated with the profile.
	ControlPlane Interpolate[string] `json:"controlplane,omitzero"`
	// Metros is a static list of metros.
	Metros []Metro `json:"metros,omitempty"`
}

func (p Profile) Validate() error {
	if p.Name == "" {
		return fmt.Errorf("profile name cannot be empty")
	}
	switch p.Type.String() {
	case string(ProfileTypeCloud):
		if p.Token.String() == "" {
			return fmt.Errorf("cloud profiles require a token")
		}
	case string(ProfileTypeLocal):
		return fmt.Errorf("local profiles are not currently supported")
	default:
		return fmt.Errorf("invalid profile type: %s", p.Type.String())
	}
	return nil
}

// Metro represents a metro configuration for a profile in the Unikraft CLI.
type Metro struct {
	// Name of the metro.
	Name Interpolate[string] `json:"name"`
	// Endpoint for the metro.
	Endpoint Interpolate[string] `json:"endpoint"`
	// Country code for the metro.
	Country Interpolate[string] `json:"country"`
	// Allows insecure connections to the metro, skipping TLS verification.
	Insecure bool `json:"insecure,omitempty"`
}

type Index struct {
	// Host is the hostname of the index to connect to.
	Host string
	// HTTP indicates whether the index should be accessed over HTTP instead of HTTPS.
	HTTP bool
	// Insecure skips TLS verification when connecting to the index.
	Insecure bool
}

func (m Metro) Index() Index {
	endpoint := m.Endpoint.String()
	u, err := url.Parse(endpoint)
	if err != nil {
		return Index{Host: endpoint}
	}
	hostname := u.Hostname()
	hostname, _ = strings.CutPrefix(hostname, "api.")
	return Index{
		Host:     "index." + hostname,
		HTTP:     u.Scheme == "http",
		Insecure: m.Insecure,
	}
}

// ProfileList returns a slice of profile names (aliases) from the
// configuration.
func (config *Config) ProfileList() []Profile {
	if config == nil {
		return nil
	}

	list := make([]Profile, 0, len(config.Profiles))
	for _, profile := range config.Profiles {
		list = append(list, profile)
	}
	return list
}

// CurrentProfile returns the currently selected profile from the configuration.
// If the profile does not exist, it returns an error.
func (config *Config) CurrentProfile() (*Profile, error) {
	if config == nil {
		return nil, ErrNotSetup
	}

	profile, ok := config.Profiles[config.CurrentProfileName()]
	if !ok {
		if len(config.Profiles) == 0 && (config.DefaultProfile.String() == "" || config.DefaultProfile.String() == DefaultProfile) {
			return nil, ErrNotSetup
		}
		return nil, ErrProfileNotFound{Name: config.CurrentProfileName()}
	}

	return &profile, nil
}

func (config *Config) CurrentProfileName() string {
	if config == nil {
		return DefaultProfile
	}
	return cmp.Or(config.selectedProfile, config.DefaultProfile.String(), DefaultProfile)
}

func (config *Config) OverrideCurrentProfile(name string) {
	config.selectedProfile = name
}
