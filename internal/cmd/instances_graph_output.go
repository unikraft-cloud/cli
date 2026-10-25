// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"context"
	"math"
	"strconv"
	"time"

	"unikraft.com/cli/internal/model"
	"unikraft.com/cli/internal/model/value"
	"unikraft.com/cli/internal/multimetro"
)

// GraphSample is one sample of one metric of an instance. It is a row of
// the output of "instances graph --output".
type GraphSample struct {
	Time     GraphTime  `field:"time,short" json:"time"`
	Instance string     `field:",short" json:"instance"`
	Metric   string     `field:",short" json:"metric"`
	Value    GraphValue `field:",short" json:"value"`
	Unit     string     `field:",short" json:"unit"`

	key multimetro.Key
}

func (GraphSample) Type() model.Type {
	return model.Type{
		Name:  "instance-graph-sample",
		Names: "instance-graph-samples",
	}
}

func (s GraphSample) Key() model.Key {
	return s.key
}

func (s GraphSample) Raw() any {
	return s
}

func (s GraphSample) Fields(ctx context.Context) ([]model.Field, error) {
	return model.FieldsFromStruct(s)
}

// GraphTime is the time at which a sample was received.
type GraphTime time.Time

func (t GraphTime) MarshalText() ([]byte, error) {
	return []byte(time.Time(t).Format(time.RFC3339Nano)), nil
}

func (t GraphTime) Render(value.RenderOpts) (string, error) {
	return time.Time(t).Format(time.RFC3339), nil
}

// GraphValue is the value of a sample. A sample without a value is null in
// JSON and "-" in a table, and a zero is a real zero.
type GraphValue struct {
	v *float64
}

// NewGraphValue returns the value of a sample that has a number.
func NewGraphValue(v float64) GraphValue {
	return GraphValue{v: &v}
}

func (v GraphValue) MarshalJSON() ([]byte, error) {
	if v.v == nil {
		return []byte("null"), nil
	}
	return strconv.AppendFloat(nil, *v.v, 'g', -1, 64), nil
}

func (v GraphValue) Render(value.RenderOpts) (string, error) {
	if v.v == nil {
		return "-", nil
	}
	return strconv.FormatFloat(math.Round(*v.v*1000)/1000, 'f', -1, 64), nil
}
