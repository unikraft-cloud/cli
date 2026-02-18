// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package login

import (
	"context"

	jujuerrors "github.com/juju/errors"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/config"
)

type LogoutCmd struct{}

func (cmd *LogoutCmd) Run(ctx context.Context, cfg *config.Config) error {
	profile, err := cfg.CurrentProfile()
	if err != nil {
		return err
	}

	delete(cfg.Profiles, profile.Name)
	if cfg.DefaultProfile.String() == profile.Name {
		cfg.DefaultProfile = config.InterpolateString("")
	}

	if err := cfg.Save(); err != nil {
		return jujuerrors.Annotate(err, "saving profile")
	}

	log.G(ctx).
		Info().
		Msg("logout successful")

	return nil
}
