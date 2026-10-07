// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package kong_test

import (
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	wkong "unikraft.com/cli/internal/w/kong"
)

func TestOptional(t *testing.T) {
	type CLI struct {
		Watch *time.Duration `short:"w" long:"watch" type:"optional"`
		Sort  string         `long:"sort"`
		Arg   string         `arg:"" optional:""`
	}

	tests := []struct {
		name      string
		args      []string
		wantWatch *time.Duration
		wantSort  string
		wantArg   string
	}{
		{
			name:      "no flags",
			args:      nil,
			wantWatch: nil,
			wantSort:  "",
		},
		{
			name:      "watch only",
			args:      []string{"-w"},
			wantWatch: new(time.Duration),
			wantSort:  "",
		},
		{
			name:      "short watch with inline value",
			args:      []string{"-w5s"},
			wantWatch: new(5 * time.Second),
			wantSort:  "",
		},
		{
			name:      "watch with equals value",
			args:      []string{"--watch=5s"},
			wantWatch: new(5 * time.Second),
			wantSort:  "",
		},
		{
			name:      "sort before watch",
			args:      []string{"--sort", "name", "-w"},
			wantWatch: new(time.Duration),
			wantSort:  "name",
		},
		{
			name:      "watch before sort",
			args:      []string{"-w", "--sort", "name"},
			wantWatch: new(time.Duration),
			wantSort:  "name",
		},
		{
			name:      "watch with value before sort",
			args:      []string{"--watch=5s", "--sort", "name"},
			wantWatch: new(5 * time.Second),
			wantSort:  "name",
		},
		{
			name:      "space-separated long does not consume value",
			args:      []string{"--watch", "5s"},
			wantWatch: new(time.Duration),
			wantArg:   "5s",
		},
		{
			name:      "space-separated short does not consume value",
			args:      []string{"-w", "5s"},
			wantWatch: new(time.Duration),
			wantArg:   "5s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cli CLI
			parser, err := kong.New(&cli, kong.NamedMapper("optional", wkong.Optional()))
			require.NoError(t, err)

			_, err = parser.Parse(tt.args)
			require.NoError(t, err)

			if tt.wantWatch == nil {
				assert.Nil(t, cli.Watch)
			} else {
				require.NotNil(t, cli.Watch)
				assert.Equal(t, *tt.wantWatch, *cli.Watch)
			}
			assert.Equal(t, tt.wantSort, cli.Sort)
			assert.Equal(t, tt.wantArg, cli.Arg)
		})
	}
}
