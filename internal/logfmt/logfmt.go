// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package logfmt

import (
	"fmt"
	"io"

	"charm.land/lipgloss/v2"
	"github.com/rs/zerolog"

	"unikraft.com/x/colors"
	"unikraft.com/x/log"
)

const LogLevelSymbol = "│"

// New returns a slog.Logger backed by a JSON or text handler.
func New(sink io.Writer, typ log.Type, level log.Level) *log.Logger {
	var logger log.Logger

	switch typ {
	case log.JSONType:
		logger = zerolog.New(sink)
	case log.TextType:
		fallthrough
	default:
		logger = zerolog.New(zerolog.ConsoleWriter{
			Out: newScreenWrappedWriter(sink),
			FormatLevel: func(i any) string {
				if ll, ok := i.(string); ok {
					level, _ := zerolog.ParseLevel(ll)
					return levelColors[level](LogLevelSymbol)
				}
				return fmt.Sprintf("%s", i)
			},
			FormatFieldName: func(i any) string {
				return colors.InfoFg(fmt.Sprintf("%s=", i))
			},
			FormatMessage: func(i any) string {
				return fmt.Sprintf("%s", i)
			},
			PartsExclude: []string{"time"},
		})
	}

	logger = logger.Level(level).With().Timestamp().Logger()
	return &logger
}

var (
	traceColor = colors.AdaptiveColor{Light: colors.Slate200, Dark: colors.Slate600}
	debugColor = colors.AdaptiveColor{Light: colors.Slate300, Dark: colors.Slate500}
	infoColor  = colors.AdaptiveColor{Light: colors.Slate400, Dark: colors.Slate400}

	levelColors = map[log.Level]func(str ...string) string{
		log.TraceLevel: lipgloss.NewStyle().Background(traceColor).Foreground(traceColor).Render,
		log.DebugLevel: lipgloss.NewStyle().Background(debugColor).Foreground(debugColor).Render,
		log.InfoLevel:  lipgloss.NewStyle().Background(infoColor).Foreground(infoColor).Render,
		log.WarnLevel:  lipgloss.NewStyle().Background(colors.Warning).Foreground(colors.Warning).Render,
		log.ErrorLevel: lipgloss.NewStyle().Background(colors.Error).Foreground(colors.Error).Render,
		log.FatalLevel: lipgloss.NewStyle().Background(colors.Error).Foreground(colors.Error).Render,
		log.PanicLevel: lipgloss.NewStyle().Background(colors.Error).Foreground(colors.Error).Render,
	}
)
