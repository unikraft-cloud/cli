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

	jujuerrors "github.com/juju/errors"
	"github.com/pkg/browser"
	"unikraft.com/cloud/sdk/controlplane"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/logfmt"
	"unikraft.com/cli/internal/multimetro"
)

type LoginCmd struct {
	Check bool `name:"check" help:"Check if the user is already logged in."`

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
		Type:         config.ProfileTypeCloud,
		ControlPlane: cmp.Or(cmd.ControlPlane, controlplane.DefaultEndpoint),
		Insecure:     cmd.AllowInsecure,
	}

	if cmd.Check {
		profile, err := cfg.CurrentProfile()
		if err != nil {
			return jujuerrors.Errorf("no existing authentication token found")
		}
		if profile.Token != "" {
			log.G(ctx).Info().
				Str("organization", profile.Organization).
				Msg("existing authentication token found")
			return nil
		}
		return jujuerrors.Errorf("no existing authentication token found")
	}

	// Get the token either from file or via browser authentication
	var token, organization, userUUID, orgUUID string
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
		if resp.Status == controlplane.ResponseStatusError {
			return jujuerrors.Annotate(jujuerrors.New(resp.Message), "authentication failed")
		}
		if resp.Data == nil {
			return jujuerrors.New("no data received from control plane, please try again")
		}
		if resp.Data.Token == "" {
			return jujuerrors.New("no authentication token received from control plane, please try again")
		}
		token = resp.Data.Token
		organization = cmp.Or(resp.Data.OrganizationName, cmd.Organization)
		userUUID = resp.Data.UserUuid
		orgUUID = resp.Data.OrganizationUuid
	}
	tempProfile.Token = token

	if orgUUID == "" || organization == "" {
		auth, err := cmd.getOrg(ctx, tempProfile)
		switch {
		case err != nil && organization == "":
			log.G(ctx).Error().
				Msg("could not determine organization from control plane, specify organization with --organization flag")
			return err
		case err != nil:
			log.G(ctx).Warn().
				Err(err).
				Msg("could not fetch organization details from control plane")
		default:
			if organization == "" {
				log.G(ctx).Info().
					Str("organization", auth.OrganizationName).
					Msg("found organization from control plane")
			}
			organization = cmp.Or(organization, auth.OrganizationName)
			orgUUID = auth.OrganizationUuid
		}
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
	profile.Token = token
	profile.Organization = organization
	profile.UserUUID = userUUID
	profile.OrganizationUUID = orgUUID
	profile.ControlPlane = loginControlPlane
	profile.Insecure = cmd.AllowInsecure

	// Fetch and merge metros
	newMetros, err := multimetro.GetMetros(ctx, profile)
	if err != nil || len(newMetros) == 0 {
		log.G(ctx).
			Warn().
			Err(err).
			Msg("could not list metros for profile: please add metros manually")
	}
	existingMetros := make(map[string]struct{}, len(profile.Metros))
	for _, metro := range profile.Metros {
		existingMetros[metro.Name] = struct{}{}
	}
	for _, metro := range newMetros {
		if _, ok := existingMetros[metro.Name]; !ok {
			profile.Metros = append(profile.Metros, metro)
		}
	}

	// Save the profile
	cfg.DefaultProfile = profile.Name
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
	// If we've explicitly specified a profile name, use that
	if name, ok := cfg.OverriddenCurrentProfile(); ok {
		if profile, ok := cfg.Profiles[name]; ok {
			return &profile, nil
		}
		return &config.Profile{
			Type: config.ProfileTypeCloud,
			Name: name,
		}, nil
	}

	// Search existing profiles for one with matching organization and controlplane
	for name, profile := range cfg.Profiles {
		if profile.Organization == organization && profile.ControlPlane == loginControlPlane {
			p := profile // copy to avoid returning pointer to loop variable
			p.Name = name
			return &p, nil
		}
	}

	// Check if a profile with the organization name already exists
	if existing, ok := cfg.Profiles[organization]; ok {
		if existing.Organization != organization && existing.Organization != "" {
			// Profile exists for a different organization - error
			return nil, jujuerrors.Errorf(
				"profile %q already exists for organization %q; "+
					"cannot overwrite with organization %q",
				organization, existing.Organization, organization,
			)
		}
		if existing.Organization == "" {
			// Profile exists but has no organization set - merge into it
			p := existing // copy to avoid returning pointer to map value
			p.Name = organization
			return &p, nil
		}
		// Profile exists for same org but different controlplane - create with unique name
		profileName := cmd.generateUniqueProfileName(cfg, organization)
		return &config.Profile{
			Type: config.ProfileTypeCloud,
			Name: profileName,
		}, nil
	}

	// No existing profile found, create a new one with organization as the name
	return &config.Profile{
		Type: config.ProfileTypeCloud,
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

func (cmd *LoginCmd) getAuth(ctx context.Context, profile *config.Profile) (*controlplane.Response[controlplane.CheckAuthorizationResponseData], error) {
	client, err := multimetro.NewControlClientFromProfile(profile)
	if err != nil {
		return nil, err
	}

	log.G(ctx).Trace().
		Str("controlplane", profile.ControlPlane).
		Msg("fetching authentication")

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
		log.G(ctx).Info().Msgf("  %s", signinResp.Data.AuthorizationUrl)
		log.G(ctx).Info().Msg(" ")
	} else {
		log.G(ctx).
			Info().
			Str("url", signinResp.Data.AuthorizationUrl).
			Msg("login")
	}

	checkResp, err := client.CheckAuthorization(ctx, controlplane.CheckAuthorizationRequest{
		RequestId: signinResp.Data.RequestId,
	})
	if err != nil {
		return nil, jujuerrors.Annotate(err, "checking authorization")
	}

	if !cmd.NoBrowser {
		if err := browser.OpenURL(signinResp.Data.AuthorizationUrl); err != nil {
			log.G(ctx).
				Debug().
				Err(err).
				Msg("could not open browser, please visit the URL manually")
		}
	}

	// TODO: run a spinner here
	for {
		select {
		case event := <-checkResp:
			if event == nil {
				continue
			}
			return event, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (cmd *LoginCmd) getOrg(ctx context.Context, profile *config.Profile) (*controlplane.GetAuthorizationResponseData, error) {
	log.G(ctx).Trace().
		Str("controlplane", profile.ControlPlane).
		Msg("fetching organization")

	client, err := multimetro.NewControlClientFromProfile(profile)
	if err != nil {
		return nil, err
	}

	resp, err := client.GetAuthorization(ctx)
	if err != nil {
		return nil, jujuerrors.Annotate(err, "getting authorization")
	}
	if resp.Data == nil || resp.Data.OrganizationName == "" {
		return nil, jujuerrors.New("no organization name received from control plane")
	}
	return resp.Data, nil
}
