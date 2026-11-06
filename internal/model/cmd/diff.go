// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"bytes"
	"context"
	"io"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/sergi/go-diff/diffmatchpatch"
	"unikraft.com/cli/internal/kvwriter"
	"unikraft.com/cli/internal/model"
	"unikraft.com/cli/internal/prettydiff"
	"unikraft.com/cli/internal/tabwriter"
)

// Diff produces a pretty diff between two sets of resources, writing the
// output to the provided writer.
func Diff(ctx context.Context, out io.Writer, format FormatOpts, empty model.Model, before []model.Model, after []model.Model) error {
	before, after = diffOrder(before, after)

	switch format.Output.Type {
	case "", PrinterTypeKeyValue:
		start := &bytes.Buffer{}
		if err := printKV(ctx, start, []string(format.Field), before...); err != nil {
			return err
		}
		end := &bytes.Buffer{}
		if err := printKV(ctx, end, []string(format.Field), after...); err != nil {
			return err
		}

		dmp := diffmatchpatch.New()
		diffs := dmp.DiffMain(
			// clean all ANSI escape sequences from the output before diffing to avoid
			// trying to diff them
			// NOTE: it would be nice to preserve those in the output, but the diffing
			// would have to be done differently
			ansi.Strip(start.String()),
			ansi.Strip(end.String()),
			false,
		)

		tw := kvwriter.KeyValueWriter(out, kvwriter.WithIndent("  "))
		if _, err := io.Copy(tw, strings.NewReader(prettydiff.Render(diffs))); err != nil {
			return err
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		return nil

	case PrinterTypeTable:
		start := &bytes.Buffer{}
		if err := printTable(ctx, start, []string(format.Field), empty, before...); err != nil {
			return err
		}
		end := &bytes.Buffer{}
		if err := printTable(ctx, end, []string(format.Field), empty, after...); err != nil {
			return err
		}

		dmp := diffmatchpatch.New()
		diffs := dmp.DiffMain(
			// same cleaning as above
			ansi.Strip(start.String()),
			ansi.Strip(end.String()),
			false,
		)

		tw := tabwriter.TabWriter(out, tabwriter.WithMaxScreenWidth())
		if _, err := io.Copy(tw, strings.NewReader(prettydiff.Render(diffs))); err != nil {
			return err
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		return nil

	default:
		return format.Output.Print(ctx, out, []string(format.Field), nil, after...)
	}
}

// diffOrder reorders the after resources to match the order of the before
// resources as much as possible. This should hopefully improve the diff
// output.
func diffOrder(before []model.Model, after []model.Model) ([]model.Model, []model.Model) {
	afterMap := make(map[string]model.Model, len(after))
	for _, r := range after {
		key := r.Key().String()
		afterMap[key] = r
	}
	afterFound := make(map[string]struct{}, len(after))

	var orderedAfter []model.Model
	for _, r := range before {
		key := r.Key().String()
		if afterR, ok := afterMap[key]; ok {
			orderedAfter = append(orderedAfter, afterR)
			afterFound[key] = struct{}{}
		}
	}
	for _, r := range after {
		key := r.Key().String()
		if _, ok := afterFound[key]; !ok {
			orderedAfter = append(orderedAfter, r)
		}
	}
	return before, orderedAfter
}
