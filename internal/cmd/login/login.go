// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package login

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	jujuerrors "github.com/juju/errors"
	"github.com/pkg/browser"
	"unikraft.com/cloud/sdk/controlplane"
	"unikraft.com/x/log"
	"unikraft.com/x/ptr"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/httpclient"
	"unikraft.com/cli/internal/logfmt"
)

type LoginCmd struct {
	Check   bool          `name:"check" help:"Check if the user is already logged in."`
	Timeout time.Duration `short:"t" name:"timeout" default:"5m" help:"Timeout for the login request."`

	ControlPlane  string `name:"controlplane" default:"https://controlplane.unikraft.cloud" help:"Control plane URL to use for login."`
	AllowInsecure bool   `name:"allow-insecure" short:"k" help:"Allow insecure server connections when using SSL."`

	NoBrowser bool `name:"no-browser" help:"Do not open the browser automatically for login."`

	Token        *os.File `help:"Path to a file containing the authentication token (or '-' for stdin)."`
	Organization string   `help:"Organization to associate the login with."`
}

func (cmd *LoginCmd) Run(ctx context.Context, cfg *config.Config) error {
	if cmd.Token != nil {
		defer cmd.Token.Close()
	}

	// Create a temporary profile for authentication
	tempProfile := &config.Profile{
		Type:         config.InterpolateString(string(config.ProfileTypeCloud)),
		ControlPlane: config.InterpolateString(cmp.Or(cmd.ControlPlane, controlplane.DefaultEndpoint)),
	}

	if cmd.Check {
		profile, err := cfg.CurrentProfile()
		if err != nil {
			return jujuerrors.Errorf("no existing authentication token found")
		}
		if profile.Token.String() != "" {
			log.G(ctx).Info().
				Str("organization", profile.Organization.String()).
				Msg("existing authentication token found")
			return nil
		}
		return jujuerrors.Errorf("no existing authentication token found")
	}

	// Get the token either from file or via browser authentication
	var token, organization string
	if cmd.Token != nil {
		log.G(ctx).Info().
			Msg("reading authentication token from file")

		dt, err := os.ReadFile(cmd.Token.Name())
		if err != nil {
			return jujuerrors.Annotate(err, "reading token file")
		}
		token = strings.TrimSpace(string(dt))
		organization = cmd.Organization
	} else {
		resp, err := cmd.getAuth(ctx, tempProfile)
		if err != nil {
			return jujuerrors.Annotate(err, "getting authentication token")
		}
		if resp.Status == string(controlplane.ResponseStatusERROR) {
			return jujuerrors.Annotate(jujuerrors.New(resp.Message), "authentication failed")
		}
		if resp.Data == nil {
			return jujuerrors.New("no data received from control plane, please try again")
		}
		if resp.Data.Token == nil {
			return jujuerrors.New("no authentication token received from control plane, please try again")
		}
		token = *resp.Data.Token
		organization = cmp.Or(ptr.ZeroIfNil(resp.Data.OrganizationName), cmd.Organization)
	}

	if organization == "" {
		return jujuerrors.New("no organization provided or received from control plane")
	}

	// Log the organization we're logging into
	log.G(ctx).Info().
		Str("organization", organization).
		Msg("authenticated")

	// Determine the controlplane to use for this login
	loginControlPlane := cmp.Or(cmd.ControlPlane, controlplane.DefaultEndpoint)

	// Find or create profile based on organization and controlplane
	profile, err := cmd.findOrCreateProfile(cfg, organization, loginControlPlane)
	if err != nil {
		return jujuerrors.Annotate(err, "finding or creating profile")
	}
	profile.Token = config.InterpolateString(token)
	profile.Organization = config.InterpolateString(organization)
	profile.ControlPlane = config.InterpolateString(loginControlPlane)

	// Fetch and merge metros
	newMetros, err := cmd.getMetros(ctx, profile)
	if err != nil || len(newMetros) == 0 {
		log.G(ctx).
			Warn().
			Err(err).
			Msg("could not list metros for profile: please add metros manually")
	} else {
		existingMetros := make(map[string]struct{}, len(profile.Metros))
		for _, metro := range profile.Metros {
			existingMetros[metro.Name.String()] = struct{}{}
		}
		for _, metro := range newMetros {
			if _, ok := existingMetros[metro.Name.String()]; !ok {
				profile.Metros = append(profile.Metros, metro)
			}
		}
	}

	// Save the profile
	cfg.DefaultProfile = config.InterpolateString(profile.Name)
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]config.Profile)
	}
	cfg.Profiles[profile.Name] = *profile

	if err := cfg.Save(); err != nil {
		return jujuerrors.Annotate(err, "saving profile")
	}

	log.G(ctx).Info().
		Str("profile", profile.Name).
		Msg("login successful")
	return nil
}

// findOrCreateProfile looks for an existing profile with the given
// organization and controlplane.  If found, it returns that profile for
// merging.  Otherwise, it creates a new profile using the organization name
// (with a suffix if needed to disambiguate controlplanes).  Returns an error
// if a profile with the same name already exists but for a different
// organization.
func (cmd *LoginCmd) findOrCreateProfile(cfg *config.Config, organization, loginControlPlane string) (*config.Profile, error) {
	// Search existing profiles for one with matching organization and controlplane
	for name, profile := range cfg.Profiles {
		if profile.Organization.String() == organization && profile.ControlPlane.String() == loginControlPlane {
			p := profile // copy to avoid returning pointer to loop variable
			p.Name = name
			return &p, nil
		}
	}

	// Check if a profile with the organization name already exists
	if existing, ok := cfg.Profiles[organization]; ok {
		if existing.Organization.String() != organization && existing.Organization.String() != "" {
			// Profile exists for a different organization - error
			return nil, jujuerrors.Errorf(
				"profile %q already exists for organization %q; "+
					"cannot overwrite with organization %q",
				organization, existing.Organization.String(), organization,
			)
		}
		if existing.Organization.String() == "" {
			// Profile exists but has no organization set - merge into it
			p := existing // copy to avoid returning pointer to map value
			p.Name = organization
			return &p, nil
		}
		// Profile exists for same org but different controlplane - create with unique name
		profileName := cmd.generateUniqueProfileName(cfg, organization)
		return &config.Profile{
			Type: config.InterpolateString(string(config.ProfileTypeCloud)),
			Name: profileName,
		}, nil
	}

	// No existing profile found, create a new one with organization as the name
	return &config.Profile{
		Type: config.InterpolateString(string(config.ProfileTypeCloud)),
		Name: organization,
	}, nil
}

// generateUniqueProfileName creates a unique profile name by appending a
// numeric suffix to the organization name.
func (cmd *LoginCmd) generateUniqueProfileName(cfg *config.Config, organization string) string {
	suffix := 2
	for {
		name := fmt.Sprintf("%s-%d", organization, suffix)
		if _, exists := cfg.Profiles[name]; !exists {
			return name
		}
		suffix++
	}
}

func (cmd *LoginCmd) getMetros(ctx context.Context, profile *config.Profile) ([]config.Metro, error) {
	copts := []controlplane.ClientOption{
		controlplane.WithDefaultEndpoint(profile.ControlPlane.String()),
		controlplane.WithToken(profile.Token.String()),
	}
	if cmd.AllowInsecure {
		copts = append(copts, controlplane.WithHTTPClient(httpclient.InsecureHTTPClient))
	} else {
		copts = append(copts, controlplane.WithHTTPClient(httpclient.DefaultHTTPClient))
	}
	client := controlplane.NewClient(copts...)

	metroResp, err := client.ListMetros(ctx)
	if err != nil {
		return nil, err
	}
	if metroResp == nil || metroResp.Data == nil {
		return nil, nil
	}

	var metros []config.Metro
	for _, metro := range metroResp.Data.Metros {
		metros = append(metros, config.Metro{
			Name:     config.InterpolateString(ptr.ZeroIfNil(metro.Name)),
			Endpoint: config.InterpolateString(ptr.ZeroIfNil(metro.Endpoint)),
			Country:  config.InterpolateString(ptr.ZeroIfNil(metro.Country)),
		})
	}
	return metros, nil
}

func (cmd *LoginCmd) getAuth(ctx context.Context, profile *config.Profile) (*controlplane.Response[controlplane.CheckAuthorizationResponseData], error) {
	copts := []controlplane.ClientOption{
		controlplane.WithDefaultEndpoint(profile.ControlPlane.String()),
	}
	if cmd.AllowInsecure {
		copts = append(copts, controlplane.WithHTTPClient(httpclient.InsecureHTTPClient))
	} else {
		copts = append(copts, controlplane.WithHTTPClient(httpclient.DefaultHTTPClient))
	}
	client := controlplane.NewClient(copts...)

	req, err := getFingerprint()
	if err != nil {
		return nil, jujuerrors.Annotate(err, "getting fingerprint")
	}
	signinResp, err := client.RequestSignin(ctx, req)
	if err != nil {
		return nil, jujuerrors.Annotate(err, "signing in")
	} else if signinResp.Data == nil {
		return nil, jujuerrors.New("no data received from control plane, please try again")
	}

	if logfmt.LogType(ctx) == log.TextType {
		log.G(ctx).Info().Msg(" ")
		log.G(ctx).Info().Msg("to authenticate, please visit:")
		log.G(ctx).Info().Msg(" ")
		log.G(ctx).Info().Msgf("  %s", *signinResp.Data.AuthorizationUrl)
		log.G(ctx).Info().Msg(" ")
	} else {
		log.G(ctx).
			Info().
			Str("url", *signinResp.Data.AuthorizationUrl).
			Msg("login")
	}

	checkResp, err := client.CheckAuthorization(ctx, controlplane.CheckAuthorizationRequest{
		RequestId: signinResp.Data.RequestId,
	})
	if err != nil {
		return nil, jujuerrors.Annotate(err, "checking authorization")
	}

	timeout := time.NewTimer(cmd.Timeout)
	ctx, cancel := context.WithCancel(ctx)

	var event *controlplane.Response[controlplane.CheckAuthorizationResponseData]
	go func() {
		defer cancel()
		for {
			select {
			case <-timeout.C:
				log.G(ctx).
					Error().
					Err(jujuerrors.New("login timed out, please try again"))
				return
			case event = <-checkResp:
				if event == nil {
					continue
				}
				return
			case <-ctx.Done():
				log.G(ctx).
					Error().
					Err(jujuerrors.Errorf("operation cancelled"))
				return
			}
		}
	}()

	if !cmd.NoBrowser {
		if err := browser.OpenURL(*signinResp.Data.AuthorizationUrl); err != nil {
			log.G(ctx).
				Debug().
				Err(err).
				Msg("could not open browser, please visit the URL manually")
		}
	}

	// TODO: run a spinner here
	log.G(ctx).
		Info().
		Str("timeout", cmd.Timeout.String()).
		Msg("waiting for confirmation")
	<-ctx.Done()

	if event == nil {
		return nil, jujuerrors.New("no event received, please try again")
	}

	return event, nil
}
