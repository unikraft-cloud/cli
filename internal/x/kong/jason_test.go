// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package kong_test

import (
	"encoding/json"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	xkong "unikraft.com/cli/internal/x/kong"
)

type jasonValue struct {
	A    int    `json:"a"`
	B    bool   `json:"b"`
	Text string `json:"-"`
}

func (v *jasonValue) UnmarshalText(data []byte) error {
	*v = jasonValue{Text: string(data)}
	return nil
}

func (v *jasonValue) UnmarshalJSON(data []byte) error {
	type alias jasonValue
	return json.Unmarshal(data, (*alias)(v))
}

func TestJason(t *testing.T) {
	type CLI struct {
		Value jasonValue `long:"value" type:"jason"`
		Other string     `long:"other"`
		Args  []string   `arg:"" optional:""`
	}

	tests := []struct {
		name      string
		args      []string
		wantValue jasonValue
		wantOther string
		wantArgs  []string
	}{
		{
			name:      "bare word uses text",
			args:      []string{"--value", "on", "x"},
			wantValue: jasonValue{Text: "on"},
			wantArgs:  []string{"x"},
		},
		{
			name:      "single item uses text",
			args:      []string{"--value", "a=1,b=true", "x"},
			wantValue: jasonValue{Text: "a=1,b=true"},
			wantArgs:  []string{"x"},
		},
		{
			name:      "single item without a comma uses jason",
			args:      []string{"--value", "b=true"},
			wantValue: jasonValue{B: true},
		},
		{
			name:      "bare word does not consume items",
			args:      []string{"--value", "on", "a=1"},
			wantValue: jasonValue{Text: "on"},
			wantArgs:  []string{"a=1"},
		},
		{
			name:      "multiple items use jason",
			args:      []string{"--value", "a=1", "b=true", "x"},
			wantValue: jasonValue{A: 1, B: true},
			wantArgs:  []string{"x"},
		},
		{
			name:      "equals form",
			args:      []string{"--value=a=1", "b=true"},
			wantValue: jasonValue{A: 1, B: true},
		},
		{
			name:      "json literal",
			args:      []string{"--value", `{"a":1}`, "x"},
			wantValue: jasonValue{A: 1},
			wantArgs:  []string{"x"},
		},
		{
			name:      "json literal merged with items",
			args:      []string{"--value", `{"a":1}`, "b=true"},
			wantValue: jasonValue{A: 1, B: true},
		},
		{
			name:      "single raw item",
			args:      []string{"--value", "a:=1"},
			wantValue: jasonValue{A: 1},
		},
		{
			name:      "stops at flags",
			args:      []string{"x", "--value", "a=1", "b=true", "--other", "y"},
			wantValue: jasonValue{A: 1, B: true},
			wantOther: "y",
			wantArgs:  []string{"x"},
		},
		{
			name:      "stops at double dash",
			args:      []string{"--value", "a=1", "b=true", "--", "a=2"},
			wantValue: jasonValue{A: 1, B: true},
			wantArgs:  []string{"a=2"},
		},
		{
			name:      "stops at non-item",
			args:      []string{"--value", "a=1", "b=true", "x", "a=2"},
			wantValue: jasonValue{A: 1, B: true},
			wantArgs:  []string{"x", "a=2"},
		},
		{
			name:      "last flag wins",
			args:      []string{"--value", "a=1", "b=true", "--value", "a=2", "a=3"},
			wantValue: jasonValue{A: 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cli CLI
			parser, err := kong.New(&cli, kong.NamedMapper("jason", xkong.Jason()))
			require.NoError(t, err)

			_, err = parser.Parse(tt.args)
			require.NoError(t, err)
			assert.Equal(t, tt.wantValue, cli.Value)
			assert.Equal(t, tt.wantOther, cli.Other)
			assert.Equal(t, tt.wantArgs, cli.Args)
		})
	}
}

func TestJasonErrors(t *testing.T) {
	type CLI struct {
		Value jasonValue `long:"value" type:"jason"`
	}

	for _, args := range [][]string{
		{"--value"},
		{"--value", "a=1", "b=nope"},
	} {
		var cli CLI
		parser, err := kong.New(&cli, kong.NamedMapper("jason", xkong.Jason()))
		require.NoError(t, err)
		_, err = parser.Parse(args)
		assert.Error(t, err, "%q", args)
	}
}

func TestJasonCollections(t *testing.T) {
	type CLI struct {
		Values []jasonValue      `long:"values" type:"jason" sep:"none"`
		Map    map[string]string `long:"map" type:"jason" sep:"none" mapsep:"none"`
	}

	tests := []struct {
		name       string
		args       []string
		wantValues []jasonValue
		wantMap    map[string]string
	}{
		{
			name: "each slice flag appends one element",
			args: []string{"--values", "a=1", "b=true", "--values", "on", "--values", "a=2,b=false", "--values", "a=3"},
			wantValues: []jasonValue{
				{A: 1, B: true},
				{Text: "on"},
				{Text: "a=2,b=false"},
				{A: 3},
			},
		},
		{
			name:    "each map flag adds its entries",
			args:    []string{"--map", "A=1", "B=x,y", "--map", "C=a,b", "--map", "A=2"},
			wantMap: map[string]string{"A": "2", "B": "x,y", "C": "a,b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cli CLI
			parser, err := kong.New(&cli, kong.NamedMapper("jason", xkong.Jason()))
			require.NoError(t, err)

			_, err = parser.Parse(tt.args)
			require.NoError(t, err)
			assert.Equal(t, tt.wantValues, cli.Values)
			assert.Equal(t, tt.wantMap, cli.Map)
		})
	}
}
