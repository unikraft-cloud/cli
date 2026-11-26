// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package tui_test

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
	"gotest.tools/v3/golden"

	"unikraft.com/cli/internal/cmd"
	modelt "unikraft.com/cli/internal/model/testing"
	"unikraft.com/cli/internal/tui/uitui"
	"unikraft.com/cli/pkg/model"
	tui "unikraft.com/cli/pkg/model/tui"
	"unikraft.com/x/colors"
	"unikraft.com/x/integration/tuikit"
)

type subpanelTestModel struct {
	modelt.TestModel
}

func (subpanelTestModel) Subpanels(ctx context.Context, key string) []tea.Model {
	readerFunc := func(ctx context.Context) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("sample logs\n")), nil
	}
	return []tea.Model{tui.NewReaderPanelFunc(ctx, "Logs", readerFunc, true)}
}

type lazySubpanelTestModel struct {
	modelt.TestModel
}

var lazyLoads atomic.Int32

func (lazySubpanelTestModel) Subpanels(ctx context.Context, key string) []tea.Model {
	logs := func(ctx context.Context) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("sample logs\n")), nil
	}
	history := func(ctx context.Context) (io.ReadCloser, error) {
		lazyLoads.Add(1)
		return io.NopCloser(strings.NewReader("sample history\n")), nil
	}
	return []tea.Model{
		tui.NewReaderPanelFunc(ctx, "Logs", logs, true),
		tui.NewReaderPanelFunc(ctx, "History", history, false).LoadOnFocus(),
	}
}

func setupTestEnv() *modelt.TestEnv {
	env := modelt.NewTestEnv()
	for _, r := range modelt.BaseTestStore() {
		env.Add(r)
	}
	return env
}

func TestTUIBasic(t *testing.T) {
	env := setupTestEnv()
	ctx := modelt.WithTestEnv(context.Background(), env)

	registry := model.NewRegistry()
	desc := registry.Register(modelt.TestModel{}, modelt.TestModel{})
	m, err := tui.New(ctx, registry, tui.Home{}, desc.Name, "")
	require.NoError(t, err)

	r := tuikit.Start(t, m, tuikit.WithSize(80, 18))

	r.WaitMatchRegexp(t, `(?s)id-test1.*id-test2`)
	listView := r.WaitNotMatch(t, "Loading...")

	r.Press(t, "enter")
	r.Line(0).WaitMatch(t, "tests > test test1")
	detail := r.Heuristic(tuikit.Box{Match: "Field"})
	detail.WaitMatch(t, "settings")
	detail.WaitMatch(t, "authors")
	detailView := r.Snapshot()

	output := strings.Join([]string{
		"=== list ===",
		listView,
		"",
		"=== detail ===",
		detailView,
	}, "\n")

	golden.Assert(t, output, t.Name(), "\n"+output)
}

func TestTUIWidePanels(t *testing.T) {
	env := setupTestEnv()
	ctx := modelt.WithTestEnv(context.Background(), env)

	registry := model.NewRegistry()
	desc := registry.Register(modelt.TestModel{}, modelt.TestModel{})
	m, err := tui.New(ctx, registry, tui.Home{}, desc.Name, "")
	require.NoError(t, err)

	r := tuikit.Start(t, m, tuikit.WithSize(140, 18))

	r.WaitMatchRegexp(t, `(?s)id-test1.*id-test2`)
	listView := r.WaitNotMatch(t, "Loading...")

	r.Press(t, "enter")
	r.Line(0).WaitMatch(t, "tests > test test1")
	detail := r.Heuristic(tuikit.Box{Match: "Field"})
	detail.WaitMatch(t, "settings")
	detail.WaitMatch(t, "authors")
	detailView := r.Snapshot()

	output := strings.Join([]string{
		"=== list ===",
		listView,
		"",
		"=== list+detail ===",
		detailView,
	}, "\n")

	golden.Assert(t, output, t.Name(), "\n"+output)
}

func TestTUIHomePanel(t *testing.T) {
	ctx := context.Background()

	m, err := cmd.NewTUIModel(ctx, "", "")
	require.NoError(t, err)

	r := tuikit.Start(t, m, tuikit.WithSize(80, 22))

	view := r.WaitMatch(t, "c'_'o  .--'")

	selected := tuikit.Style{Bold: new(true), Foreground: colors.Primary}
	r.Match("certificates").WaitStyle(t, selected)
	r.Press(t, "down")
	r.Match("images").WaitStyle(t, selected)
	r.Match("certificates").WaitStyle(t, tuikit.Style{Bold: new(false)})

	output := strings.Join([]string{
		"=== home ===",
		view,
	}, "\n")
	output += "\n"

	golden.Assert(t, output, t.Name(), "\n"+output)
}

func TestTUIDetailWithSubpanel(t *testing.T) {
	env := setupTestEnv()
	ctx := modelt.WithTestEnv(context.Background(), env)

	registry := model.NewRegistry()
	desc := registry.Register(subpanelTestModel{}, subpanelTestModel{})
	m, err := tui.New(ctx, registry, tui.Home{}, desc.Name, "")
	require.NoError(t, err)
	r := tuikit.Start(t, m, tuikit.WithSize(80, 30))

	r.WaitMatchRegexp(t, `(?s)id-test1.*id-test2`)
	listView := r.WaitNotMatch(t, "Loading...")
	r.Match("id-test1").WaitStyle(t, tuikit.Style{Bold: new(true)})
	r.Match("id-test2").WaitStyle(t, tuikit.Style{Bold: new(false)})

	r.Press(t, "enter")
	r.Line(0).WaitMatch(t, "tests > test test1")
	detail := r.Heuristic(tuikit.Box{Match: "Field"})
	logs := r.Heuristic(tuikit.Box{Match: "sample logs"})
	focused := tuikit.Style{Foreground: uitui.FocusedBorderColor}
	detail.WaitMatch(t, "id-test1")
	detail.Line(0).WaitStyle(t, focused)
	logs.WaitNotMatch(t, "Loading...")
	detailView := r.WaitNotMatch(t, "Loading...")

	r.Press(t, "tab")
	logs.Line(0).WaitStyle(t, focused)
	r.Line(-1).WaitNotMatch(t, "r refresh")
	logsView := r.Snapshot()

	output := strings.Join([]string{
		"=== list ===",
		listView,
		"",
		"=== detail (main focused) ===",
		detailView,
		"",
		"=== detail (logs focused) ===",
		logsView,
	}, "\n")
	output += "\n"

	golden.Assert(t, output, t.Name(), "\n"+output)
}

func TestTUIDetailWithLazySubpanel(t *testing.T) {
	lazyLoads.Store(0)
	env := setupTestEnv()
	ctx := modelt.WithTestEnv(context.Background(), env)

	registry := model.NewRegistry()
	desc := registry.Register(lazySubpanelTestModel{}, lazySubpanelTestModel{})
	m, err := tui.New(ctx, registry, tui.Home{}, desc.Name, "")
	require.NoError(t, err)
	r := tuikit.Start(t, m, tuikit.WithSize(80, 40))

	r.WaitMatch(t, "id-test1")
	r.WaitNotMatch(t, "Loading...")

	r.Press(t, "enter")
	r.Heuristic(tuikit.Box{Match: "sample logs"}).WaitExists(t)
	r.Heuristic(tuikit.Box{Match: "Focus this panel to load."}).WaitExists(t)
	require.Zero(t, lazyLoads.Load(), "the panel loads only on focus")

	r.Press(t, "tab", "tab")
	history := r.Heuristic(tuikit.Box{Match: "sample history"})
	history.WaitExists(t)
	history.Line(0).WaitStyle(t, tuikit.Style{Foreground: uitui.FocusedBorderColor})
	require.EqualValues(t, 1, lazyLoads.Load())
}
