// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/alecthomas/kong"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
)

// CLI is the root command structure.
type CLI struct {
	LogLevel log.Level `group:"flag-global" name:"log-level" env:"UNIKRAFT_LOG_LEVEL" enum:"trace,debug,info,warn,error,fatal" placeholder:"level" default:"info"`
	LogType  log.Type  `group:"flag-global" name:"log-type" env:"UNIKRAFT_LOG_TYPE" enum:"text,json" placeholder:"type" default:"text"`

	Mdx MdxCmd `cmd:"" help:"Generate markdown documentation for the CLI."`
	Man ManCmd `cmd:"" help:"Generate man pages for the CLI."`
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	var cli CLI
	kctx := kong.Parse(&cli,
		kong.Name("gendocs"),
		kong.Help(kingkong.HelpPrinter("")),
		kong.Description("Generate documentation and man pages for the Unikraft CLI."),
		kong.UsageOnError(),
	)

	logger, err := log.New(ctx, log.Config{Sink: os.Stderr, Type: cli.LogType, Level: cli.LogLevel})

	kctx.FatalIfErrorf(err)

	ctx = log.WithLogger(ctx, logger)
	kctx.BindTo(ctx, (*context.Context)(nil))

	if err := kctx.Run(); err != nil {
		kctx.FatalIfErrorf(err)
	}
}
