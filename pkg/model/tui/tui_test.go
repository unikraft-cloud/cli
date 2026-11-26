// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package tui_test

import (
	"context"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/stretchr/testify/require"
	"gotest.tools/v3/golden"

	"unikraft.com/cli/internal/cmd"
	modelt "unikraft.com/cli/internal/model/testing"
	"unikraft.com/cli/internal/tui/testkit"
	"unikraft.com/cli/pkg/model"
	tui "unikraft.com/cli/pkg/model/tui"
)

func TestMain(m *testing.M) {
	lipgloss.Writer.Profile = colorprofile.Ascii
	os.Exit(m.Run())
}

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

	runner := testkit.New(m, 80, 18)
	defer runner.Stop()

	listView, err := runner.WaitUntilSnapshot(2*time.Second, 0, func(s string) bool {
		return strings.Contains(s, "id-test1") && strings.Contains(s, "id-test2") && !strings.Contains(s, "Loading...")
	})
	require.NoErrorf(t, err, "wait failed: %v\n\nlast snapshot:\n%s", err, runner.Snapshot())

	runner.PressKeys(tea.Key{Code: tea.KeyEnter})
	detailView, err := runner.WaitUntilSnapshot(2*time.Second, 0, func(s string) bool {
		return strings.Contains(s, "tests > test test1") && strings.Contains(s, "settings") && strings.Contains(s, "authors")
	})
	require.NoErrorf(t, err, "wait failed: %v\n\nlast snapshot:\n%s", err, runner.Snapshot())

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

	runner := testkit.New(m, 140, 18)
	defer runner.Stop()

	listView, err := runner.WaitUntilSnapshot(2*time.Second, 0, func(s string) bool {
		return strings.Contains(s, "id-test1") && strings.Contains(s, "id-test2") && !strings.Contains(s, "Loading...")
	})
	require.NoErrorf(t, err, "wait failed: %v\n\nlast snapshot:\n%s", err, runner.Snapshot())

	runner.PressKeys(tea.Key{Code: tea.KeyEnter})
	detailView, err := runner.WaitUntilSnapshot(2*time.Second, 0, func(s string) bool {
		return strings.Contains(s, "tests > test test1") && strings.Contains(s, "settings") && strings.Contains(s, "authors")
	})
	require.NoErrorf(t, err, "wait failed: %v\n\nlast snapshot:\n%s", err, runner.Snapshot())

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

	runner := testkit.New(m, 80, 22)
	defer runner.Stop()

	view, err := runner.WaitUntilSnapshot(3*time.Second, 0, func(s string) bool {
		return strings.Contains(s, "c'_'o  .--'")
	})
	require.NoErrorf(t, err, "wait failed: %v\n\nlast snapshot:\n%s", err, runner.Snapshot())

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
	runner := testkit.New(m, 80, 30)
	defer runner.Stop()

	listView, err := runner.WaitUntilSnapshot(2*time.Second, 0, func(s string) bool {
		return strings.Contains(s, "id-test1") && strings.Contains(s, "id-test2") && !strings.Contains(s, "Loading...")
	})
	require.NoErrorf(t, err, "wait failed: %v\n\nlast snapshot:\n%s", err, runner.Snapshot())

	runner.PressKeys(tea.Key{Code: tea.KeyEnter})
	detailView, err := runner.WaitUntilSnapshot(2*time.Second, 0, func(s string) bool {
		return strings.Contains(s, "tests > test test1") &&
			strings.Contains(s, "sample logs") &&
			strings.Contains(s, "Field") &&
			strings.Contains(s, "id-test1") &&
			!strings.Contains(s, "Loading...")
	})
	require.NoErrorf(t, err, "wait failed: %v\n\nlast snapshot:\n%s", err, runner.Snapshot())

	runner.PressKeys(tea.Key{Code: tea.KeyTab})
	logsView, err := runner.WaitUntilSnapshot(2*time.Second, 0, func(s string) bool {
		return strings.Contains(s, "sample logs") &&
			strings.Contains(s, "Field") &&
			strings.Contains(s, "id-test1") &&
			!strings.Contains(s, "Loading...")
	})
	require.NoErrorf(t, err, "wait failed: %v\n\nlast snapshot:\n%s", err, runner.Snapshot())

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
	runner := testkit.New(m, 80, 40)
	defer runner.Stop()

	_, err = runner.WaitUntilSnapshot(2*time.Second, 0, func(s string) bool {
		return strings.Contains(s, "id-test1") && !strings.Contains(s, "Loading...")
	})
	require.NoErrorf(t, err, "wait failed: %v\n\nlast snapshot:\n%s", err, runner.Snapshot())

	runner.PressKeys(tea.Key{Code: tea.KeyEnter})
	_, err = runner.WaitUntilSnapshot(2*time.Second, 0, func(s string) bool {
		return strings.Contains(s, "sample logs") && strings.Contains(s, "Focus this panel to load.")
	})
	require.NoErrorf(t, err, "wait failed: %v\n\nlast snapshot:\n%s", err, runner.Snapshot())
	require.Zero(t, lazyLoads.Load(), "the panel loads only on focus")

	runner.PressKeys(tea.Key{Code: tea.KeyTab}, tea.Key{Code: tea.KeyTab})
	_, err = runner.WaitUntilSnapshot(2*time.Second, 0, func(s string) bool {
		return strings.Contains(s, "sample logs") && strings.Contains(s, "sample history")
	})
	require.NoErrorf(t, err, "wait failed: %v\n\nlast snapshot:\n%s", err, runner.Snapshot())
	require.EqualValues(t, 1, lazyLoads.Load())
}
