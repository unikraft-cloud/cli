// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
	"unikraft.com/x/integration/tuikit"
	"unikraft.com/x/integration/tuikit/script"
	"unikraft.com/x/kingkong"

	"unikraft.com/cli/internal/cmd"
	"unikraft.com/cli/internal/config"
)

type CLI struct {
	Config  string `name:"config" env:"UNIKRAFT_CONFIG" help:"Path to the configuration file." placeholder:"file"`
	Profile string `name:"profile" env:"UNIKRAFT_PROFILE" help:"Set the current profile." placeholder:"name"`

	Resource string `arg:"" optional:"" help:"Resource type to browse."`
	Name     string `arg:"" optional:"" help:"Resource key to open."`

	Width       int           `name:"width" help:"Window width." default:"80"`
	Height      int           `name:"height" help:"Window height." default:"24"`
	WaitTimeout time.Duration `name:"wait-timeout" help:"Default timeout for wait commands." default:"5s"`

	Script *os.File `name:"script" help:"Read commands from file ('-' for stdin)." default:"-" placeholder:"file"`
	Cmd    []string `name:"cmd" help:"Command line to run (repeatable)."`

	Output string `name:"output" help:"Output format." enum:"text,json" default:"text"`
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var cli CLI
	kctx := kong.Parse(&cli,
		kong.Name("testtui"),
		kong.Help(kingkong.HelpPrinter("")),
		kong.Description("Preview `unikraft tui` rendering via scripted Bubble Tea inputs."),
		kong.UsageOnError(),
	)

	if err := run(ctx, &cli); err != nil {
		kctx.FatalIfErrorf(err)
	}
}

func run(ctx context.Context, cli *CLI) error {
	configPath := cli.Config
	if configPath == "" {
		p, err := config.ConfigFilePath()
		if err != nil {
			return err
		}
		configPath = p
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = &config.Config{Path: configPath}
	}
	if cli.Profile != "" {
		cfg.OverrideCurrentProfile(cli.Profile)
	}
	ctx = config.WithConfig(ctx, cfg)

	model, err := cmd.NewTUIModel(ctx, cli.Resource, cli.Name)
	if err != nil {
		return err
	}

	runner := tuikit.New(model, tuikit.WithSize(cli.Width, cli.Height))
	defer runner.Stop()

	exec := &script.Executor{
		Runner:      runner,
		Out:         os.Stdout,
		JSON:        cli.Output == "json",
		WaitTimeout: cli.WaitTimeout,
	}

	for _, line := range cli.Cmd {
		if err := exec.RunLine(ctx, 0, line); err != nil {
			return err
		}
	}

	r := cli.Script
	if r == nil {
		r = os.Stdin
	}
	if cli.Script != nil && cli.Script != os.Stdin {
		defer cli.Script.Close()
	}
	return exec.Run(ctx, r)
}
