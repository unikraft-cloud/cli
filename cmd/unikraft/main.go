// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"strings"
	"syscall"

	"github.com/alecthomas/kong"
	jujuerrors "github.com/juju/errors"

	"github.com/charmbracelet/colorprofile"

	"unikraft.com/cli/internal/cmd"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/logfmt"
	"unikraft.com/cli/internal/telemetry"
	"unikraft.com/x/colors"
	"unikraft.com/x/log"
)

func main() {
	// Recover from panics and report crashes before re-panicking
	defer telemetry.RecoverAndReport()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var (
		err error

		args  = os.Args[1:]
		stdio = config.Stdio{
			Stdin:  os.Stdin,
			Stdout: colorprofile.NewWriter(os.Stdout, os.Environ()),
			Stderr: colorprofile.NewWriter(os.Stderr, os.Environ()),
		}
	)

	ctx, err = run(ctx, args, stdio)
	if err == nil {
		// catch context cancellation errors, and make sure we show them, even if
		// the command succeeded
		err = ctx.Err()
	}

	// Track command completion for telemetry
	cmdPath, ok := telemetry.CommandFromContext(ctx)
	if ok && (err == nil || errors.Is(err, context.Canceled)) {
		telemetry.TrackCommandSuccess(cmdPath)
	}

	if err != nil && !errors.Is(err, context.Canceled) {
		if ok {
			telemetry.TrackCommandError(cmdPath, err)
		}

		if logfmt.LogType(ctx) == log.TextType {
			log.G(ctx).Error().Msg(" ")
			log.G(ctx).Error().Msg(colors.ErrorFg("error:"))
		}

		if log.G(ctx).GetLevel() <= log.DebugLevel {
			if juerr, ok := err.(*jujuerrors.Err); ok {
				for _, cause := range juerr.StackTrace() {
					logErr(ctx, cause)
				}
			} else {
				logErr(ctx, err.Error())
			}
		} else {
			logErr(ctx, err.Error())
		}

		if logfmt.LogType(ctx) == log.TextType {
			log.G(ctx).Error().Msg(" ")
		}
	}
	if err != nil {
		os.Exit(1)
	}
}

func logErr(ctx context.Context, msg string) {
	if logfmt.LogType(ctx) == log.TextType {
		for line := range strings.SplitSeq(msg, "\n") {
			log.G(ctx).Error().Msgf("  %s", line)
		}
	} else {
		msg = strings.ReplaceAll(msg, "\n\n", " ")
		msg = strings.ReplaceAll(msg, "\n", " ")
		log.G(ctx).Error().Msg(msg)
	}
}

// buildCommandPath constructs a space-separated command path from kong context.
// e.g., "instances list" or "run"
func buildCommandPath(kctx *kong.Context) string {
	var parts []string
	for _, path := range kctx.Path {
		if path.Command != nil {
			parts = append(parts, path.Command.Name)
		}
	}
	if len(parts) == 0 {
		return "unikraft"
	}
	return strings.Join(parts, " ")
}

func getMethod(value reflect.Value, name string) reflect.Value {
	method := value.MethodByName(name)
	if !method.IsValid() {
		if value.CanAddr() {
			method = value.Addr().MethodByName(name)
		}
	}
	return method
}

func run(ctx context.Context, args []string, stdio config.Stdio) (context.Context, error) {
	ctx, cli, opts, cleanup, err := cmd.NewRootCmd(ctx, args, stdio)
	if err != nil {
		return ctx, err
	}

	// Build command path early for telemetry decisions (e.g., "instances list")
	cmdPath := buildCommandPath(cli)

	// Prevent recursive analytics: when the "send analytics" subcommand runs, it
	// should not trigger another analytics event, which would spawn another
	// "send analytics" subprocess, creating infinite recursion.
	isSendAnalytics := cmdPath == "_send_analytics"

	// Initialize analytics if telemetry is enabled.
	//
	// These usage analytics are keyed to the logged-in account when there is
	// one, and anonymous otherwise.  This information is used to help us
	// understand how the CLI is being used and to improve it over time.  We may
	// collect information such as which commands are used, how often they are
	// used, and any errors that occur.  This data is aggregated and analyzed to
	// identify trends and areas for improvement.
	//
	// Unikraft is committed to user privacy and data protection; visit[0] for
	// more information.
	//
	// [0]: https://unikraft.com/company/legal/privacy

	// If the DO_NOT_TRACK environment variable is set, regardless of value, we
	// should not initialize telemetry at all.  This is a pretty common convention
	// for respecting user privacy preferences, and allows users to easily opt out
	// of telemetry without needing to understand specific environment variables
	// for our CLI.
	_, doNotTrack := os.LookupEnv("DO_NOT_TRACK")
	if !doNotTrack && opts.Telemetry && !isSendAnalytics {
		profile, _ := config.G(ctx).CurrentProfile()
		if err := telemetry.Init(profile); err != nil {
			log.G(ctx).
				Debug().
				Err(err).
				Msg("failed to initialize analytics, telemetry is disabled")
		} else {
			log.G(ctx).
				Debug().
				Msg("collecting usage analytics, set `UNIKRAFT_TELEMETRY=false` to disable")
		}
	}

	node := cli.Selected()
	if node == nil {
		if len(cli.Path) == 0 {
			return ctx, fmt.Errorf("no command selected")
		}

		selected := cli.Path[0].Node()
		if selected.Type == kong.ApplicationNode {
			method := getMethod(selected.Target, "Run")
			if method.IsValid() {
				node = selected
			}
		}

		if node == nil {
			return ctx, fmt.Errorf("no command selected")
		}
	}

	if !doNotTrack && opts.Telemetry && !isSendAnalytics {
		telemetry.TrackCommandStart(cmdPath)
		ctx = telemetry.WithCommand(ctx, cmdPath)
	}

	err = cli.RunNode(node, &opts.ConfigPath)
	if cleanup != nil {
		err = errors.Join(err, cleanup())
	}
	return ctx, err
}
