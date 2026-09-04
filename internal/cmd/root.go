// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/alecthomas/kong"
	ctrdlog "github.com/containerd/log"
	kongcompletion "github.com/jotaen/kong-completion"
	jujuerrors "github.com/juju/errors"
	"github.com/posener/complete"
	"github.com/sirupsen/logrus"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
	"unikraft.com/x/version"

	"unikraft.com/cli/internal/cmd/login"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/logfmt"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	xkong "unikraft.com/cli/internal/x/kong"
	xmaps "unikraft.com/cli/internal/x/maps"
)

type UnikraftCLI struct {
	globalFlags

	Run   RunCmd   `cmd:"" group:"cmd-commands" help:"Run an image as an instance." set:"name=instance" set:"names=instances"`
	Build BuildCmd `cmd:"" group:"cmd-commands" help:"Build a Unikraft project into a container image."`
	TUI   TUICmd   `cmd:"" group:"cmd-commands" help:"Browse resources in a TUI."`

	Metros       MetrosCmd       `cmd:"" group:"cmd-resources" help:"Manage Unikraft Cloud metros." aliases:"metro,metros" set:"name=metro" set:"names=metros"`
	Quotas       MetroQuotasCmd  `cmd:"" group:"cmd-resources" help:"Display quota usage for metros." aliases:"quota"`
	Instances    InstancesCmd    `cmd:"" group:"cmd-resources" help:"Manage Unikraft Cloud instances." aliases:"instance,instances,vm,vms" set:"name=instance" set:"names=instances"`
	Volumes      VolumesCmd      `cmd:"" group:"cmd-resources" help:"Manage Unikraft Cloud volumes." aliases:"volume,volumes,vol,vols" set:"name=volume" set:"names=volumes"`
	Services     ServicesCmd     `cmd:"" group:"cmd-resources" help:"Manage Unikraft Cloud services." aliases:"service,services,svc,svcs" set:"name=service" set:"names=services"`
	Certificates CertificatesCmd `cmd:"" group:"cmd-resources" help:"Manage Unikraft Cloud certificates." aliases:"certificate,certificates,crt,crts,cert,certs" set:"name=certificate" set:"names=certificates"`
	Images       ImagesCmd       `cmd:"" group:"cmd-resources" help:"Manage Unikraft Cloud images." aliases:"image,images,img,imgs" set:"name=image" set:"names=images"`
	Resources    AnyResourceCmd  `cmd:"" group:"cmd-resources" hidden:"" help:"Manage any Unikraft Cloud resource." aliases:"resource,resources" set:"name=resource" set:"names=resources"`

	API APICmd `cmd:"" group:"cmd-utilities" help:"Make an authenticated HTTP request to the Unikraft Cloud API."`

	Login   login.LoginCmd  `cmd:"" group:"cmd-config" help:"Login to Unikraft Cloud."`
	Logout  login.LogoutCmd `cmd:"" group:"cmd-config" help:"Logout from Unikraft Cloud."`
	Profile ProfileCmd      `cmd:"" group:"cmd-config" help:"Manage Unikraft Cloud profiles." aliases:"profile,profiles" set:"name=profile" set:"names=profiles"`
	Config  ConfigCmd       `cmd:"" group:"cmd-config" help:"Manage CLI configuration." aliases:"config,conf,cfg" set:"name=path" set:"names=paths"`

	Completion kongcompletion.Completion `cmd:"" group:"cmd-utilities" completion-shell-default:"false" help:"Outputs shell code for initialising tab completions."`
	Version    version.VersionCmd        `cmd:"" group:"cmd-utilities" help:"Show version information." aliases:"version,ver,v"`
	Upgrade    UpgradeCmd                `cmd:"" group:"cmd-utilities" help:"Upgrade the Unikraft CLI to the latest version."`

	SendAnalytics SendAnalyticsCmd `cmd:"" group:"cmd-utilities" help:"Send analytics payload (used internally for detached analytics)." name:"_send_analytics" hidden:""`
}

func (cli UnikraftCLI) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Login to Unikraft Cloud",
			Commands: []string{
				"unikraft login",
			},
		},
		{
			Description: "List instances across metros",
			Commands: []string{
				"unikraft instances list",
			},
		},
		{
			Description: "Build and publish an image from a Kraftfile",
			Commands: []string{
				"unikraft image build . --output my-org/my-app:latest",
			},
		},
		{
			Description: "Deploy a new instance from an image",
			Commands: []string{
				"unikraft run --metro=sfo --image=nginx:latest --autostart -p 443:8080/http+tls --scale-to-zero policy=on",
			},
		},
		{
			Description: "Switch to a different profile",
			Commands: []string{
				"unikraft profile use my-other-profile",
			},
		},
	}
}

type globalFlags struct {
	ConfigPath string `group:"flag-global" name:"config" env:"UNIKRAFT_CONFIG" help:"Path to the configuration file." placeholder:"file"`

	LogLevel log.Level `group:"flag-global" name:"log-level" env:"UNIKRAFT_LOG_LEVEL" help:"Set the logging level." enum:"trace,debug,info,warn,error,fatal" placeholder:"level" default:"info"`
	LogType  log.Type  `group:"flag-global" name:"log-type" env:"UNIKRAFT_LOG_TYPE" help:"Set the log type." enum:"text,json" placeholder:"type" default:"text"`

	Profile string `group:"flag-global" name:"profile" env:"UNIKRAFT_PROFILE" help:"Set the current profile." placeholder:"name"`

	Telemetry bool          `group:"flag-global" name:"telemetry" env:"UNIKRAFT_TELEMETRY" help:"Toggle usage analytics." default:"true" negatable:""`
	Timeout   time.Duration `group:"flag-global" name:"timeout" env:"UNIKRAFT_TIMEOUT" help:"Set a deadline for the command (e.g. 30s, 5m, 1h)." placeholder:"duration" optional:""`
}

func NewRootCmd(ctx context.Context, args []string, stdio config.Stdio) (context.Context, *kong.Context, *UnikraftCLI, func() error, error) {
	cli := UnikraftCLI{}

	parser, err := NewParser(&cli)
	if err != nil {
		// super unlikely, the config for the parser is all hardcoded, but we
		// should still *somehow* try and log it
		ctx = ctxWithLogger(ctx, stdio.Stderr, log.TextType, log.InfoLevel)
		return ctx, nil, nil, nil, jujuerrors.Annotate(err, "internal error")
	}
	parser.Stdout = stdio.Stdout
	parser.Stderr = stdio.Stderr

	kongcompletion.Register(
		parser,
		kongcompletion.WithPredictor("resource-key-path", complete.PredictFiles("*")),
		kongcompletion.WithPredictor("resource-key-profile", cmd.PredictResourceKey[Profile](ctx)),
		kongcompletion.WithPredictor("resource-key-metro", cmd.PredictResourceKey[Metro](ctx)),
		kongcompletion.WithPredictor("resource-key-instance", cmd.PredictResourceKey[Instance](ctx)),
		kongcompletion.WithPredictor("resource-key-instance-template", cmd.PredictResourceKey[InstanceTemplate](ctx)),
		kongcompletion.WithPredictor("resource-key-instance-checkpoint", cmd.PredictResourceKey[InstanceCheckpoint](ctx)),
		kongcompletion.WithPredictor("resource-key-volume", cmd.PredictResourceKey[Volume](ctx)),
		kongcompletion.WithPredictor("resource-key-volume-template", cmd.PredictResourceKey[VolumeTemplate](ctx)),
		kongcompletion.WithPredictor("resource-key-service", cmd.PredictResourceKey[ServiceGroup](ctx)),
		kongcompletion.WithPredictor("resource-key-certificate", cmd.PredictResourceKey[Certificate](ctx)),
		kongcompletion.WithPredictor("resource-key-image", cmd.PredictResourceKey[ImageEntry](ctx)),
		kongcompletion.WithPredictor("resource-key-resource", cmd.PredictResourceKey[AnyResource](ctx)),
	)

	kctx, err := parser.Parse(args)

	var parseErr *kong.ParseError
	printHelp := false
	if errors.As(err, &parseErr) {
		// we couldn't parse everything, but still try and manually load these
		// flags, since they're used for help + logging
		for _, flag := range parseErr.Context.Flags() {
			switch flag.Name {
			case "help":
				printHelp = flag.Set
			case "log-type":
				cli.LogType = parseErr.Context.FlagValue(flag).(log.Type)
			case "log-level":
				cli.LogLevel = parseErr.Context.FlagValue(flag).(log.Level)
			}
		}
		cli.LogType = cmp.Or(cli.LogType, log.TextType)
		cli.LogLevel = cmp.Or(cli.LogLevel, log.InfoLevel)

		if cli.LogType != log.TextType && cli.LogType != log.JSONType {
			cli.LogType = log.TextType
		}

		// HACK: kong provides UsageOnError, but this shows help for *all* parse
		// errors - we only want to show it only for parent commands.
		// See https://github.com/alecthomas/kong/issues/33
		if strings.HasPrefix(parseErr.Error(), "expected one of") {
			printHelp = true
		}
	}
	if err != nil {
		ctx = ctxWithLogger(ctx, stdio.Stderr, cli.LogType, cli.LogLevel)
		if printHelp {
			_ = parseErr.Context.PrintUsage(false)
			fmt.Fprintln(os.Stdout)
		}
		return ctx, nil, &cli, nil, jujuerrors.Annotate(err, "parsing arguments")
	}

	kctx.Bind(stdio)

	ctx = ctxWithLogger(ctx, stdio.Stderr, cli.LogType, cli.LogLevel)

	configPath := cli.ConfigPath
	if configPath == "" {
		configPath, err = config.ConfigFilePath()
		if err != nil {
			return ctx, nil, nil, nil, jujuerrors.Annotate(err, "getting config file path")
		}
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return ctx, nil, nil, nil, jujuerrors.Annotate(err, "loading config")
	}
	if cfg == nil {
		// This is the first time the configuration is being loaded (potentially a
		// fresh install), so we should set the path to the default location.  This
		// ensures that when the user eventually saves the configuration, it will be
		// saved to the expected location, even if they didn't explicitly specify
		// it.
		cfg = &config.Config{
			Path: configPath,
		}
	}
	if cli.globalFlags.Profile != "" {
		cfg.OverrideCurrentProfile(cli.globalFlags.Profile)
	}
	ctx = config.WithConfig(ctx, cfg)
	kctx.Bind(cfg)

	cancelTimeout := func() {}
	if cli.Timeout > 0 {
		ctx, cancelTimeout = newTimedOutContext(ctx, cli.Timeout)
	}

	kctx.BindTo(ctx, (*context.Context)(nil))

	partition, err := resource.LoadPartitionFromEnv(PartitionedResources...)
	if err != nil {
		cancelTimeout()
		return ctx, nil, nil, nil, jujuerrors.Annotate(err, "loading partition from environment")
	}
	if partition != nil {
		partitioned := xmaps.OrderedKeys(partition.Keys)
		slices.Sort(partitioned)
		log.G(ctx).Debug().
			Str("path", partition.Path).
			Strs("resources", partitioned).
			Msg("loaded partition from environment")
	}
	kctx.Bind(partition)
	kctx.Bind(kctx)

	cleanup := func() error {
		cancelTimeout()
		if err := partition.Save(); err != nil {
			return jujuerrors.Annotate(err, "saving partition")
		}
		return nil
	}

	log.G(ctx).
		Debug().
		Str("version", version.Version).
		Str("arch", runtime.GOARCH).
		Str("plat", runtime.GOOS).
		Str("commit", version.Commit).
		Str("built", version.BuildTime).
		Msg("unikraft CLI")

	return ctx, kctx, &cli, cleanup, nil
}

func ctxWithLogger(ctx context.Context, out io.Writer, type_ log.Type, level log.Level) context.Context {
	switch level.String() {
	case "trace":
		level = log.TraceLevel
	case "debug":
		level = log.DebugLevel
	case "info":
		level = log.InfoLevel
	case "warn":
		level = log.WarnLevel
	case "error":
		level = log.ErrorLevel
	case "fatal":
		level = log.FatalLevel
	case "panic":
		level = log.PanicLevel
	default:
		level = log.InfoLevel
	}
	ctx = logfmt.WithLogType(ctx, type_)
	ctx = log.WithLogger(ctx, logfmt.New(out, type_, level))

	ctx = ctrdlog.WithLogger(ctx, logrus.NewEntry(log.ToLogrus(
		log.G(ctx),
		log.WithLogrusLevelCap(logrus.DebugLevel),
	)))
	return ctx
}

func NewParser(cli *UnikraftCLI) (*kong.Kong, error) {
	helpOptions := kong.HelpOptions{
		Compact:             true,
		FlagsLast:           true,
		NoExpandSubcommands: true,
	}
	globalFlagGroup := kong.Group{
		Key:   "flag-global",
		Title: kingkong.Underline("Global flags") + ":",
	}

	return kong.New(cli,
		kong.Name("unikraft"),
		kong.UsageOnError(),
		kong.Description("The Unikraft Command-Line Interface."),
		kingkong.DescriptionDetail("The Unikraft Command-Line Interface.\n"+
			"    _         \n"+
			"  c'3'o  .-.  Docs:   https://unikraft.com/docs/cli\n"+
			"  (| |)_/     Issues: https://github.com/unikraft-cloud/cli/issues\n"+
			"              "),
		kong.ConfigureHelp(helpOptions),
		kong.Help(kingkong.HelpPrinter(version.Version)),
		kong.WithBeforeReset(func(value *kong.Path) error {
			if value == nil || value.App == nil || value.App.Flags == nil {
				return nil
			}

			for _, f := range value.App.Flags {
				if f.Name != "help" {
					continue
				}

				f.Group = &kong.Group{
					Key:   "flag-global",
					Title: kingkong.Underline("Global flags") + ":",
				}
			}

			return nil
		}),
		kong.ExplicitGroups([]kong.Group{
			{
				Key:   "flag-create",
				Title: kingkong.Underline("Create flags") + ":",
			},
			{
				Key:   "flag-edit",
				Title: kingkong.Underline("Edit flags") + ":",
			},
			globalFlagGroup,
			{
				Key:   "flag-local",
				Title: kingkong.Underline("Subcommand flags") + ":",
			},
			{
				Key:   "cmd-commands",
				Title: kingkong.Underline("Commands") + ":",
			},
			{
				Key:   "cmd-resources",
				Title: kingkong.Underline("Resources") + ":",
			},
			{
				Key:   "cmd-templates",
				Title: kingkong.Underline("Templates") + ":",
			},
			{
				Key:   "cmd-checkpoints",
				Title: kingkong.Underline("Checkpoints") + ":",
			},
			{
				Key:   "cmd-config",
				Title: kingkong.Underline("Config") + ":",
			},
			{
				Key:   "cmd-utilities",
				Title: kingkong.Underline("Utilities") + ":",
			},
		}),
		kong.NamedMapper("optional", xkong.Optional()),
		sandboxKongVars,
	)
}

var PartitionedResources = []resource.Resource{
	Image{},
	Instance{},
	InstanceTemplate{},
	InstanceCheckpoint{},
	Volume{},
	VolumeTemplate{},
	ServiceGroup{},
	Certificate{},
}

type timedOutContext struct {
	context.Context // parent
	done            chan struct{}
	mu              sync.Mutex
	ctxErr          error
	deadline        time.Time
}

func (c *timedOutContext) Done() <-chan struct{} { return c.done }

func (c *timedOutContext) Err() error {
	select {
	case <-c.done:
	default:
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ctxErr
}

func (c *timedOutContext) Deadline() (time.Time, bool) {
	return c.deadline, true
}

func newTimedOutContext(parent context.Context, timeout time.Duration) (context.Context, func()) {
	c := &timedOutContext{
		Context:  parent,
		done:     make(chan struct{}),
		deadline: time.Now().Add(timeout),
	}

	stopCh := make(chan struct{}, 1)

	var (
		once     sync.Once
		stopOnce sync.Once
	)

	closeWithErr := func(err error) {
		once.Do(func() {
			c.mu.Lock()
			c.ctxErr = err
			c.mu.Unlock()
			close(c.done)
		})
	}

	timer := time.NewTimer(timeout)
	go func() {
		defer timer.Stop()
		select {
		case <-timer.C:
			closeWithErr(fmt.Errorf("timed out"))
		case <-parent.Done():
			closeWithErr(parent.Err())
		case <-stopCh:
			// cleanup canceled
		}
	}()

	stopFn := func() {
		once.Do(func() {})
		stopOnce.Do(func() { stopCh <- struct{}{} }) // signal goroutine to exit
	}

	return c, stopFn
}

type staticKey string

func (s staticKey) String() string {
	return string(s)
}

func (s staticKey) Canonical() string {
	return string(s)
}
