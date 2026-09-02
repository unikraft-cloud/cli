// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package config

import (
	"cmp"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/MakeNowJust/heredoc"
	jujuerrors "github.com/juju/errors"
	"unikraft.com/x/ptr"
)

// DefaultProfile is the name of the default profile used by the Unikraft CLI.
// It is used when no specific profile is set or when the user has not created
// any profiles yet.
const DefaultProfile = "default"

// EnvProfile is the name of the virtual profile created from the environment
// variables UKC_TOKEN and UKC_METRO. It is used when the UNIKRAFT_PROFILE_ENV
// environment variable is set to true.
const EnvProfile = "env"

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
type ProfileType string

const (
	// ProfileTypeLocal indicates a local profile, which is typically used for
	// development and testing purposes on the user's machine.
	ProfileTypeLocal ProfileType = "local"

	// ProfileTypeCloud indicates a cloud profile, which is used for accessing
	// cloud-based resources and services provided by Unikraft.
	ProfileTypeCloud ProfileType = "cloud"

	// ProfileTypeLegacy indicates a legacy profile, which is used for backward
	// compatibility with the kraft CLI.
	ProfileTypeLegacy ProfileType = "legacy"
)

// Profile represents a user profile configuration for the Unikraft CLI.
type Profile struct {
	// Name of the profile
	Name string `json:"-" field:",short"`
	// Type of the profile
	Type ProfileType `json:"type" field:",long"`
	// Token is the authentication token associated with the profile, used for
	// authenticating with Unikraft Cloud services.
	Token string `json:"token,omitempty" field:",long"`
	// Organization is the organization associated with the profile.
	Organization string `json:"organization,omitempty" field:",short"`
	// UserUUID is the UUID of the user who approved the login, if known.
	UserUUID string `json:"user_uuid,omitempty" field:",long"`
	// OrganizationUUID is the UUID of the organization of the token.
	OrganizationUUID string `json:"organization_uuid,omitempty" field:",long"`
	// ControlPlane is the endpoint for the control plane associated with the profile.
	ControlPlane string `json:"controlplane,omitempty" field:",long"`
	// Insecure indicates whether to allow insecure connections to the control plane, skipping TLS verification.
	Insecure bool `json:"controlplane_insecure,omitempty" field:",long"`
	// Metros is a static list of metros.
	Metros []Metro `json:"metros,omitempty" field:",long,embed"`
	// MetroDefault is the default metro to use when creating resources.
	MetroDefault string `json:"metro_default,omitempty"`
}

func (p Profile) GetDefaultMetro() string {
	if p.MetroDefault != "" {
		return p.MetroDefault
	}
	if len(p.Metros) == 1 {
		return p.Metros[0].Name
	}
	return ""
}

func (p *Profile) populate() {
	if p.Type == ProfileTypeLegacy {
		*p = profileFromEnv(p.Name)
	}
}

// profileFromEnv constructs a Profile from the UKC_TOKEN and UKC_METRO
// environment variables. It is used both to hydrate legacy profiles from the
// environment and to create a virtual profile when UNIKRAFT_PROFILE_ENV is set.
func profileFromEnv(name string) Profile {
	p := Profile{
		Name:  name,
		Type:  ProfileTypeLegacy,
		Token: os.Getenv("UKC_TOKEN"),
	}

	if raw := os.Getenv("UKC_METRO"); raw != "" {
		p.Metros = []Metro{metroFromEnv(raw)}
	}
	return p
}

// metroFromEnv parses the UKC_METRO environment variable into a Metro.
func metroFromEnv(raw string) Metro {
	var name, endpoint string
	if u, err := url.Parse(raw); err == nil && u.IsAbs() && u.Host != "" {
		u.Path = strings.TrimSuffix(strings.TrimSuffix(u.Path, "/"), "/v1")
		name, _, _ = strings.Cut(strings.TrimPrefix(u.Host, "api."), ".")
		endpoint = u.String()
	} else {
		name = raw
		endpoint = fmt.Sprintf("https://api.%s.unikraft.cloud", raw)
	}

	var insecure *bool
	if v := os.Getenv("UKC_ALLOW_INSECURE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			insecure = &b
		}
	}
	return Metro{
		Name:     name,
		Endpoint: endpoint,
		Insecure: insecure,
	}
}

func (p *Profile) depopulate() {
	if p.Type == ProfileTypeLegacy {
		p.Token = ""
		p.Metros = nil
	}
}

func (p Profile) Validate() error {
	if p.Name == "" {
		return fmt.Errorf("profile name cannot be empty")
	}
	switch p.Type {
	case ProfileTypeCloud:
		if p.Token == "" {
			return fmt.Errorf("cloud profiles require a token")
		}
	case ProfileTypeLocal:
		return fmt.Errorf("local profiles are not currently supported")
	case ProfileTypeLegacy:
	default:
		return fmt.Errorf("invalid profile type: %s", p.Type)
	}
	return nil
}

// Metro represents a metro configuration for a profile in the Unikraft CLI.
type Metro struct {
	// Name of the metro.
	Name string `json:"name" field:",short"`
	// Endpoint for the metro.
	Endpoint string `json:"endpoint" field:",long"`
	// Location is the IATA code for where the metro is located.
	Location string `json:"location" field:",short"`
	// Allows insecure connections to the metro, skipping TLS verification.
	Insecure *bool `json:"insecure,omitempty" field:",long"`
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
	u, err := url.Parse(m.Endpoint)
	if err != nil {
		return Index{Host: m.Endpoint}
	}
	hostname := u.Hostname()
	hostname, _ = strings.CutPrefix(hostname, "api.")
	return Index{
		Host:     "index." + hostname,
		HTTP:     u.Scheme == "http",
		Insecure: ptr.ZeroIfNil(m.Insecure),
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
	if b, _ := strconv.ParseBool(os.Getenv("UNIKRAFT_PROFILE_ENV")); b {
		p := profileFromEnv(EnvProfile)
		return &p, nil
	}

	if config == nil {
		return nil, ErrNotSetup
	}

	profile, ok := config.Profiles[config.CurrentProfileName()]
	if !ok {
		if len(config.Profiles) == 0 && (config.DefaultProfile == "" || config.DefaultProfile == DefaultProfile) {
			return nil, ErrNotSetup
		}
		return nil, ErrProfileNotFound{Name: config.CurrentProfileName()}
	}

	return &profile, nil
}

func (config *Config) CurrentProfileName() string {
	if b, _ := strconv.ParseBool(os.Getenv("UNIKRAFT_PROFILE_ENV")); b {
		return EnvProfile
	}
	if config == nil {
		return DefaultProfile
	}
	return cmp.Or(config.selectedProfile, config.DefaultProfile, DefaultProfile)
}

func (config *Config) OverrideCurrentProfile(name string) {
	config.selectedProfile = name
}

func (config *Config) OverriddenCurrentProfile() (string, bool) {
	if config == nil {
		return "", false
	}
	if config.selectedProfile == "" {
		return "", false
	}
	return config.selectedProfile, true
}
