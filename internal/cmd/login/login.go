// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package login

import (
	"context"
	"os"
	"time"

	jujuerrors "github.com/juju/errors"
	"github.com/pkg/browser"
	"unikraft.com/cloud/sdk/controlplane"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/httpclient"
)

type LoginCmd struct {
	AllowInsecure bool          `long:"allow-insecure" short:"k" help:"Allow insecure server connections when using SSL."`
	Controlplane  string        `long:"controlplane" default:"https://controlplane.unikraft.cloud" help:"Control plane URL to use for login."`
	Force         bool          `long:"force" short:"f" help:"Force re-authentication even if an existing token is present."`
	NoBrowser     bool          `long:"no-browser" help:"Do not open the browser automatically for login."`
	Timeout       time.Duration `short:"t" long:"timeout" default:"5m" help:"Timeout for the login request."`
}

func (cmd *LoginCmd) Run(cfg *config.Config) error {
	ctx := cfg.Context

	profile, err := config.G(ctx).CurrentProfile()
	if err != nil && jujuerrors.Is(err, config.ErrNoCurrentProfile) {
		// Set up a new profile if no current profile exists.
		profile = &config.Profile{
			Type:         config.ProfileTypeCloud,
			Name:         config.DefaultProfileName,
			Controlplane: cmd.Controlplane,
		}
	} else if err != nil && jujuerrors.Is(err, config.ErrProfileNotFound) {
		// Set up a new profile for the new profile.
		profile = &config.Profile{
			Type:         config.ProfileTypeCloud,
			Name:         cfg.Profile,
			Controlplane: cmd.Controlplane,
		}
	} else if err != nil {
		return jujuerrors.Annotate(err, "getting current profile")
	}

	if !cmd.Force && profile.Token != "" {
		log.G(ctx).Info().
			Msg("existing authentication token found, re-authenticating")
	}

	if token := os.Getenv("UKC_TOKEN"); !cmd.Force && token != "" {
		// TODO: validate token
		log.G(ctx).Info().
			Msg("using authentication token from UKC_TOKEN environment variable")
		profile.Token = token
	} else {
		resp, err := cmd.getAuth(ctx, profile)
		if err != nil {
			return jujuerrors.Annotate(err, "getting authentication token")
		}
		profile.Token = *resp.Token
		profile.Organization = *resp.OrganizationName
	}

	log.G(ctx).
		Warn().
		Msg("no metros configured; please add them manually")

	cfg.Profile = profile.Name
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]config.Profile)
	}
	cfg.Profiles[profile.Name] = *profile

	if err := cfg.Save(); err != nil {
		return jujuerrors.Annotate(err, "saving profile")
	}

	log.G(ctx).Info().
		Msg("login successful")
	return nil
}

func (cmd *LoginCmd) getAuth(ctx context.Context, profile *config.Profile) (*controlplane.CheckAuthorizationResponseData, error) {
	server := profile.Controlplane
	if len(cmd.Controlplane) > 0 {
		// Override the control plane if one is provided via the command line.
		server = cmd.Controlplane
	} else if len(server) == 0 {
		// If no control plane is set, use the default control plane.
		server = controlplane.DefaultEndpoint
	}

	copts := []controlplane.ClientOption{
		controlplane.WithDefaultEndpoint(server),
	}

	if cmd.AllowInsecure {
		copts = append(copts, controlplane.WithHTTPClient(httpclient.InsecureHTTPClient))
	} else {
		copts = append(copts, controlplane.WithHTTPClient(httpclient.DefaultHTTPClient))
	}

	client := controlplane.NewClient(copts...)

	signinResp, err := client.RequestSignin(ctx, getFingerprint(ctx))
	if err != nil {
		return nil, jujuerrors.Annotate(err, "signing in")
	} else if signinResp.Data == nil {
		return nil, jujuerrors.New("no data received from control plane, please try again")
	}

	if config.G(ctx).LogType == log.TextType {
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
				if event.Status == string(controlplane.ResponseStatusSUCCESS) {
					return
				} else {
					log.G(ctx).
						Error().
						Err(jujuerrors.Errorf("login failed: %s", event.Message))
				}
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

	return event.Data, nil
}
