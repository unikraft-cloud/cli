// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/cloud/sdk/platform/lineage"
	"unikraft.com/x/colors"
	xio "unikraft.com/x/io"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
	xstdio "unikraft.com/x/stdio"

	"unikraft.com/cli/internal/multimetro"
	"unikraft.com/cli/internal/types"
	"unikraft.com/cli/pkg/model"
	"unikraft.com/cli/pkg/model/cmd"
)

// lineagePalette colors the lines of a lineage graph. The colors repeat.
var lineagePalette = []compat.AdaptiveColor{
	{Light: colors.Blue600, Dark: colors.Blue400},
	{Light: colors.Emerald600, Dark: colors.Emerald400},
	{Light: colors.Orange600, Dark: colors.Orange400},
	{Light: colors.Rose600, Dark: colors.Rose400},
}

// lineageHelp ends the help of the lineage commands.
const lineageHelp = " A table draws the tree in its graph column: ◉ is the target, ● an instance, ○ a checkpoint, ◎ a template and ◌ a deleted instance."

// InstanceHistoryOpts are the options of the lineage commands.
type InstanceHistoryOpts struct {
	Limit uint `help:"Show at most this many nodes of each lineage: the newest ones and the target. 0 shows all."`

	cmd.FormatOpts
}

func (InstanceHistoryOpts) HelpSections() []kingkong.HelpSection {
	return cmd.ModelCmd[InstanceHistoryEntry]{}.HelpSections()
}

// InstanceHistoryEntry is one instance, template or checkpoint in the lineage
// of a target.
type InstanceHistoryEntry struct {
	Graph    string               `field:",short" json:"-" help:"Lineage graph line of the node. Only tables show it."`
	Metro    LinkName[Metro]      `field:"metro,long" json:"metro" help:"Metro the node is in."`
	Name     string               `field:",short" json:"name" help:"Instance, template or checkpoint name. Empty for a deleted instance."`
	Kind     lineage.Kind         `field:",long" json:"kind" help:"Node kind: instance, template or checkpoint."`
	State    *types.InstanceState `field:",short" json:"state,omitempty" help:"Lifecycle state: an instance state, template, checkpoint, or deleted for a deleted instance."`
	Created  *types.RelativeTime  `field:"created,short" json:"created,omitempty" help:"Creation time."`
	UUID     string               `field:",long" json:"uuid" help:"Unique identifier assigned at creation."`
	Parent   string               `field:",long" json:"parent" help:"Name or UUID of the instance, template or checkpoint that the node comes from."`
	Relation lineage.Relation     `field:",long" json:"relation" help:"How the node relates to its parent."`
	Target   string               `field:",long" json:"target" help:"Instance, template or checkpoint the lineage was requested for."`

	key multimetro.Key
}

func (InstanceHistoryEntry) Type() model.Type {
	return model.Type{
		Name:  "instance-history-entry",
		Names: "instance-history-entries",
	}
}

func (i InstanceHistoryEntry) Key() model.Key {
	return i.key
}

func (i InstanceHistoryEntry) Raw() any {
	return i
}

func (i InstanceHistoryEntry) Fields(ctx context.Context) ([]model.Field, error) {
	return model.FieldsFromStruct(i)
}

// lineageTarget is an instance, template or checkpoint to show the lineage
// of. order is its position in the keys given by the user.
type lineageTarget struct {
	key   multimetro.Key
	inst  platform.Instance
	order int
}

// instanceLineage is the lineage of a target. nodes holds every tree of the
// metro, and scope the tree of the target.
type instanceLineage struct {
	target       lineageTarget
	nodes, scope lineage.Tree
}

// readInstanceLineages reads the lineage of each target once per UUID, in their
// order. It lists all instances, templates and checkpoints of their metros.
func readInstanceLineages(ctx context.Context, targets []lineageTarget, limit uint) ([]instanceLineage, error) {
	targets = slices.SortedStableFunc(slices.Values(targets), func(a, b lineageTarget) int {
		return cmp.Compare(a.order, b.order)
	})
	seen := map[string]bool{}
	byMetro := map[string][]lineageTarget{}
	for _, t := range targets {
		if seen[t.inst.Uuid] {
			continue
		}
		seen[t.inst.Uuid] = true
		byMetro[t.key.Metro] = append(byMetro[t.key.Metro], t)
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	lins, err := group.CollectMetrosSlices(ctx, g, slices.Collect(maps.Keys(byMetro)), func(ctx context.Context, c multimetro.MetroClient) ([]instanceLineage, error) {
		log.G(ctx).Trace().Msg("listing instances, templates and checkpoints for their lineage")
		resp, err := c.GetInstances(ctx, nil, platform.GetInstancesOpts{Details: new(true)})
		if err != nil {
			return nil, fmt.Errorf("could not list the instances of %s: %w", c.Metro.Name, err)
		}
		tresp, err := c.GetTemplateInstances(ctx, nil, platform.GetTemplateInstancesOpts{Details: new(true)})
		if err != nil {
			log.G(ctx).Warn().Err(err).Msgf("could not list the templates of %s, so instances created from them show as roots", c.Metro.Name)
		}
		cresp, err := c.GetCheckpointInstances(ctx, nil, platform.GetCheckpointInstancesOpts{Details: new(true)})
		if err != nil {
			log.G(ctx).Warn().Err(err).Msgf("could not list the checkpoints of %s, so checkpoints of deleted instances may not show", c.Metro.Name)
		}
		var listed, insts []platform.Instance
		if resp != nil && resp.Data != nil {
			listed = resp.Data.Instances
		}
		if tresp != nil && tresp.Data != nil {
			listed = append(slices.Clip(listed), tresp.Data.Instances...)
		}
		if cresp != nil && cresp.Data != nil {
			listed = append(slices.Clip(listed), cresp.Data.Instances...)
		}
		for _, t := range byMetro[c.Metro.Name] {
			if t.inst.State == platform.InstanceStateCheckpoint || t.inst.State == platform.InstanceStateTemplate {
				listed = append(slices.Clip(listed), t.inst)
			}
		}
		for _, inst := range listed {
			if inst.Status != nil && *inst.Status != platform.ResponseStatusSuccess {
				continue
			}
			insts = append(insts, inst)
		}
		read, err := lineage.FetchHistories(ctx, c, insts)
		if err := context.Cause(ctx); err != nil {
			return nil, err
		}
		if err != nil {
			return nil, fmt.Errorf("could not read the checkpoint histories of %s: %w", c.Metro.Name, err)
		}
		for _, err := range read.Errs {
			log.G(ctx).Debug().Err(err).Msg("could not read some checkpoint histories")
		}

		nodes := lineage.Build(insts, read.Instances)
		var lins []instanceLineage
		for _, t := range byMetro[c.Metro.Name] {
			scope, truncated, found := nodes.ScopeOf(t.inst, int(limit))
			switch {
			case !found:
				log.G(ctx).Warn().Msgf("the lineage of %s is not known, so it shows alone", t.key)
			case nodes.MissesHistory(t.inst.Uuid, read.Missing):
				log.G(ctx).Warn().Msgf("some checkpoint histories could not be read, the lineage of %s may be incomplete", t.key)
			}
			if truncated {
				log.G(ctx).Info().Msgf("the lineage of %s has more than %d nodes, --limit 0 shows all", t.key, limit)
			}
			lins = append(lins, instanceLineage{target: t, nodes: nodes, scope: scope})
		}
		return lins, nil
	})
	slices.SortStableFunc(lins, func(a, b instanceLineage) int {
		return cmp.Or(cmp.Compare(a.target.order, b.target.order), cmp.Compare(a.target.key.Metro, b.target.key.Metro))
	})
	return lins, err
}

// lineageGraph draws the rows of l, with current marked as ◉.
func lineageGraph(l lineage.Layout, current string) []string {
	grid := l.Grid()
	if row, ok := l.RowOf[current]; ok {
		grid[row][l.Lane[current]*lineage.LaneWidth].Rune = '◉'
	}

	rows := make([]string, len(grid))
	var b, run strings.Builder
	for i, cells := range grid {
		b.Reset()
		for j := 0; j < len(cells); {
			color := cells[j].Color
			run.Reset()
			for ; j < len(cells) && cells[j].Color == color; j++ {
				run.WriteRune(cells[j].Rune)
			}
			if color < 0 {
				b.WriteString(run.String())
				continue
			}
			style := lipgloss.NewStyle().Foreground(lineagePalette[color%len(lineagePalette)])
			b.WriteString(style.Render(run.String()))
		}
		rows[i] = strings.TrimRight(b.String(), " ")
	}
	return rows
}

// historyEntries returns a row for each node of lin, in the order of l, the
// layout of lin.scope.
func historyEntries(lin instanceLineage, l lineage.Layout) []InstanceHistoryEntry {
	metro := lin.target.key.Metro
	target := lin.target.key.String()

	entries := make([]InstanceHistoryEntry, len(l.Rows))
	for i, n := range l.Rows {
		relation, parent := lin.nodes.RelationOf(n)
		e := InstanceHistoryEntry{
			Metro:    LinkName[Metro](metro),
			Name:     n.Name,
			Kind:     n.Kind,
			Relation: relation,
			Target:   target,
		}
		if !parent.Deleted {
			e.Parent = cmp.Or(parent.Name, parent.UUID)
		}
		if n.Deleted {
			e.State = new(types.InstanceState(platform.InstanceStateDeleted))
		} else {
			e.UUID = n.UUID
			e.Created = new(types.RelativeTime(n.CreatedAt))
			e.key = multimetro.Key{Metro: metro, Name: n.Name, UUID: n.UUID}
			state := n.State
			if state == "" && n.Kind == lineage.KindCheckpoint {
				state = platform.InstanceStateCheckpoint
			}
			if state != "" {
				e.State = new(types.InstanceState(state))
			}
		}
		entries[i] = e
	}
	return entries
}

// printLineageTables prints a table of each lineage with p, with its graph at
// most half the terminal wide. With more than one lineage, it writes each key.
func printLineageTables(ctx context.Context, w io.Writer, p cmd.Printer, fields []string, lins []instanceLineage) error {
	width := 80
	if xio.IsTTY(w) && xio.TermWidth(w) > 0 {
		width = xio.TermWidth(w)
	}
	lanes := max((width/2-1)/lineage.LaneWidth, 1)

	for i, lin := range lins {
		if len(lins) > 1 {
			if i > 0 {
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w, lin.target.key)
		}
		l := lin.scope.Layout(lineage.WithMaxLanes(lanes))
		graph := lineageGraph(l, lin.target.inst.Uuid)
		var entries []model.Model
		for j, e := range historyEntries(lin, l) {
			e.Graph = graph[j]
			entries = append(entries, e)
		}
		if err := p.Print(ctx, w, fields, InstanceHistoryEntry{}, entries...); err != nil {
			return err
		}
	}
	return nil
}

// runInstanceHistory prints the lineages of the instances, templates or
// checkpoints in found, which a Get of keys returned with getErr.
func runInstanceHistory(ctx context.Context, stdio xstdio.Stdio, opts InstanceHistoryOpts, keys []string, found []model.Model, getErr error) error {
	if len(found) == 0 {
		return getErr
	}
	parsed := multimetro.ParseKeys(keys)
	targets := make([]lineageTarget, 0, len(found))
	for _, m := range found {
		var t lineageTarget
		switch m := m.(type) {
		case Instance:
			t = lineageTarget{key: m.key, inst: m.Instance}
		case InstanceTemplate:
			t = lineageTarget{key: m.key, inst: m.Instance}
		case InstanceCheckpoint:
			t = lineageTarget{key: m.key, inst: m.Instance}
		}
		t.key = multimetro.Key{Metro: t.key.Metro, Name: t.inst.Name, UUID: t.inst.Uuid}
		t.order = slices.IndexFunc(parsed, func(k multimetro.Key) bool {
			return (k.Metro == "" || k.Metro == t.key.Metro) &&
				(k.UUID == t.inst.Uuid || (k.UUID == "" && k.Name == t.inst.Name))
		})
		targets = append(targets, t)
	}

	lins, err := readInstanceLineages(ctx, targets, opts.Limit)
	err = errors.Join(getErr, err)
	if len(lins) == 0 {
		return err
	}

	p := opts.Output.WithDefault(cmd.PrinterTypeTable)
	if p.Type == cmd.PrinterTypeTable {
		return errors.Join(err, printLineageTables(ctx, stdio.Stdout, p, opts.Field, lins))
	}
	fields := opts.Field
	if len(fields) == 0 && (p.Type == cmd.PrinterTypeJSON || p.Type == cmd.PrinterTypeYAML) {
		fields = []string{cmd.FieldSpecAll, "-graph"}
	}
	var entries []model.Model
	seen := map[string]bool{}
	for _, lin := range lins {
		for _, e := range historyEntries(lin, lin.scope.Layout()) {
			if p.Type == cmd.PrinterTypeQuiet && len(fields) == 0 && (e.UUID == "" || seen[e.UUID]) {
				continue
			}
			seen[e.UUID] = true
			entries = append(entries, e)
		}
	}
	printErr := p.Print(ctx, stdio.Stdout, fields, InstanceHistoryEntry{}, entries...)
	return errors.Join(err, printErr)
}
