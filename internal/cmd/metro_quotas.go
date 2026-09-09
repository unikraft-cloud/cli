// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"github.com/docker/go-units"
	"golang.org/x/sync/errgroup"

	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/kvwriter"
	"unikraft.com/cli/internal/multimetro"
	xio "unikraft.com/cli/internal/x/io"
	"unikraft.com/cloud/sdk/platform"
	"unikraft.com/cloud/sdk/platform/group"
	"unikraft.com/x/colors"
	"unikraft.com/x/log"
)

// MetroQuotasCmd displays quota usage for metros in an interactive TUI.
// Tabs allow switching between individual metros.
// quotaErrorStyle renders per-metro fetch errors.
var quotaErrorStyle = lipgloss.NewStyle().Foreground(colors.Error)

type MetroQuotasCmd struct {
	Metro []string       `short:"m" help:"Show quotas for specific metros only." placeholder:"metro"`
	Watch *time.Duration `short:"w" help:"Watch for changes and refresh output. Defaults to 2s." type:"optional" placeholder:"duration"`
}

func (cmd *MetroQuotasCmd) Run(ctx context.Context, stdio config.Stdio) error {
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return fmt.Errorf("failed to load profile: %w", err)
	}

	if _, err := selectedMetros(profile, cmd.Metro); err != nil {
		return err
	}

	var watch *time.Duration
	if cmd.Watch != nil {
		w := cmp.Or(*cmd.Watch, 2*time.Second)
		watch = &w
	}

	if xio.IsTTY(stdio.Stdout) {
		return runQuotasTUI(ctx, stdio, cmd, watch)
	}
	if cmd.Watch != nil {
		return cmd.watchRenderLoop(ctx, stdio.Stdout, *watch)
	}
	return cmd.renderOnce(ctx, stdio.Stdout)
}

func (cmd *MetroQuotasCmd) watchRenderLoop(ctx context.Context, out io.Writer, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if err := cmd.renderOnce(ctx, out); err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			_, _ = fmt.Fprint(out, "\n")
		}
	}
}

func (cmd *MetroQuotasCmd) renderOnce(ctx context.Context, out io.Writer) error {
	result := fetchAllMetros(ctx, cmd.Metro)
	if result.err != nil {
		return result.err
	}
	if len(result.data) == 0 {
		return fmt.Errorf("no quota data available")
	}
	_, err := fmt.Fprint(out, renderQuotaSections(result.data, result.errors, result.tabs, result.userName))
	return err
}

func renderQuotaSections(data map[string]*platform.Quotas, errors map[string]error, tabs []string, userName string) string {
	showMetroHeader := len(tabs) > 1
	var b strings.Builder
	for i, tab := range tabs {
		q := data[tab]
		if showMetroHeader {
			fmt.Fprintf(&b, "metro: %s\n\n", tab)
		}
		if q != nil {
			b.WriteString(renderQuotaView(q, userName, defaultQuotaBarWidth))
		} else if err := errors[tab]; err != nil {
			b.WriteString(quotaErrorStyle.Render(err.Error()))
			b.WriteString("\n")
		} else {
			b.WriteString("no quota data available\n")
		}
		if i < len(tabs)-1 {
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

// multiMetroResult holds results from fetching multiple metros.
type multiMetroResult struct {
	data     map[string]*platform.Quotas
	errors   map[string]error
	tabs     []string
	userName string
	err      error
}

const (
	defaultQuotaBarWidth = 36
	minQuotaBarWidth     = 10
)

func renderQuotaView(q *platform.Quotas, userName string, barWidth int) string {
	var b strings.Builder
	kv := kvwriter.KeyValueWriter(&b)

	// Header: user UUID and name.
	if q.Uuid != "" {
		fmt.Fprintf(kv, "user uuid: %s\n", q.Uuid)
	}
	if userName != "" {
		fmt.Fprintf(kv, "user name: %s\n", userName)
	}
	if q.Uuid != "" || userName != "" {
		fmt.Fprintln(kv)
	}

	enabledStyle := lipgloss.NewStyle().Foreground(colors.Primary)
	disabledStyle := lipgloss.NewStyle().Foreground(colors.Slate600)

	// Instances and vCPUs
	writeMetricRow(kv, "active instances", q.Used.LiveInstances, q.Hard.LiveInstances, false, barWidth)
	writeMetricRow(kv, "total instances", q.Used.Instances, q.Hard.Instances, false, barWidth)
	writeMetricRow(kv, "active vcpus", q.Used.LiveVcpus, q.Hard.LiveVcpus, false, barWidth)
	fmt.Fprintf(kv, "vcpu limit: %d-%d\n", q.Limits.MinVcpus, q.Limits.MaxVcpus)
	fmt.Fprintln(kv)

	// Memory
	writeMetricRow(kv, "active used memory", q.Used.LiveMemoryMb, q.Hard.LiveMemoryMb, true, barWidth)
	fmt.Fprintf(kv, "memory size limits: %s-%s\n", quotaFormatSizeMB(q.Limits.MinMemoryMb), quotaFormatSizeMB(q.Limits.MaxMemoryMb))
	fmt.Fprintln(kv)

	// Services
	writeMetricRow(kv, "exposed services", q.Used.Services, q.Hard.Services, false, barWidth)
	writeMetricRow(kv, "services", q.Used.ServiceGroups, q.Hard.ServiceGroups, false, barWidth)
	fmt.Fprintln(kv)

	// Volumes
	if q.Limits.MaxVolumeMb > 0 {
		writeMetricRow(kv, "active volumes", q.Used.Volumes, q.Hard.Volumes, false, barWidth)
		writeMetricRow(kv, "used volume space", q.Used.TotalVolumeMb, q.Hard.TotalVolumeMb, true, barWidth)
		fmt.Fprintf(kv, "volume size limits: %s-%s\n", quotaFormatSizeMB(q.Limits.MinVolumeMb), quotaFormatSizeMB(q.Limits.MaxVolumeMb))
		fmt.Fprintln(kv)
	}

	// Autoscaling
	if q.Limits.MaxAutoscaleSize > 1 {
		fmt.Fprintf(kv, "autoscale: %s\n", enabledStyle.Render("enabled"))
		fmt.Fprintf(kv, "autoscale limit: %d-%d\n", q.Limits.MinAutoscaleSize, q.Limits.MaxAutoscaleSize)
	} else {
		fmt.Fprintf(kv, "autoscale: %s\n", disabledStyle.Render("disabled"))
	}
	fmt.Fprintf(kv, "scale-to-zero: %s\n", enabledStyle.Render("enabled"))
	_ = kv.Flush()
	return b.String()
}

func writeMetricRow(out io.Writer, label string, used, limit int64, isMB bool, barWidth int) {
	emptyStyle := lipgloss.NewStyle().Background(compat.AdaptiveColor{Light: colors.Slate300, Dark: colors.Slate700})
	fullStyle := lipgloss.NewStyle().Foreground(colors.Primary)

	var renderBar string
	if limit <= 0 {
		renderBar = emptyStyle.Render(strings.Repeat(" ", barWidth))
	} else {
		filled := int(math.Floor(float64(used) / float64(limit) * float64(barWidth)))
		filled = max(0, min(filled, barWidth))
		bar := fullStyle.Render(strings.Repeat("█", filled))
		empty := emptyStyle.Render(strings.Repeat(" ", barWidth-filled))
		renderBar = bar + empty
	}

	if isMB {
		fmt.Fprintf(out, "%s: %s  %s/%s\n", label, renderBar, quotaFormatSizeMB(used), quotaFormatSizeMB(limit))
		return
	}
	fmt.Fprintf(out, "%s: %s  %d/%d\n", label, renderBar, used, limit)
}

func quotaFormatSizeMB(mb int64) string {
	const binary = 1024
	unitsBinary := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	return units.CustomSize("%.1f%s", float64(mb)*units.MiB, binary, unitsBinary)
}

func selectedMetros(profile *config.Profile, selectedNames []string) ([]config.Metro, error) {
	if len(profile.Metros) == 0 {
		return nil, fmt.Errorf("no metros configured in profile %q", profile.Name)
	}

	if len(selectedNames) == 0 {
		return profile.Metros, nil
	}

	byName := make(map[string]config.Metro, len(profile.Metros))
	for _, m := range profile.Metros {
		byName[m.Name] = m
	}

	metros := make([]config.Metro, 0, len(selectedNames))
	seen := make(map[string]struct{}, len(selectedNames))
	for _, name := range selectedNames {
		metro, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("metro %q not found in profile", name)
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		metros = append(metros, metro)
	}

	return metros, nil
}

// fetchAllMetros fetches quotas for all configured metros concurrently.
func fetchAllMetros(ctx context.Context, selectedNames []string) multiMetroResult {
	profile, err := config.G(ctx).CurrentProfile()
	if err != nil {
		return multiMetroResult{err: err}
	}

	userName := profile.Organization
	if userName == "" {
		userName = profile.Name
	}

	metros, err := selectedMetros(profile, selectedNames)
	if err != nil {
		return multiMetroResult{err: err}
	}

	g, err := multimetro.NewClient(ctx)
	if err != nil {
		return multiMetroResult{err: err}
	}

	results := make([]struct {
		metro  string
		quotas *platform.Quotas
		err    error
	}, len(metros))

	var eg errgroup.Group
	for i, m := range metros {
		eg.Go(func() error {
			err := group.DoMetro(ctx, g, m.Name, func(ctx context.Context, mc multimetro.MetroClient) error {
				log.G(ctx).Trace().Str("metro", m.Name).Msg("fetching metro quotas")
				resp, err := mc.GetUser(ctx)
				if err != nil {
					return err
				}
				if resp.Data == nil || len(resp.Data.Quotas) == 0 {
					return fmt.Errorf("no quota data")
				}
				results[i].quotas = &resp.Data.Quotas[0]
				return nil
			})
			if err != nil {
				results[i].err = fmt.Errorf("%s: %w", m.Name, err)
			}
			results[i].metro = m.Name
			return nil
		})
	}
	_ = eg.Wait()

	data := make(map[string]*platform.Quotas)
	errorsByMetro := make(map[string]error)
	tabs := make([]string, 0, len(metros))
	for _, r := range results {
		tabs = append(tabs, r.metro)
		if r.err != nil {
			errorsByMetro[r.metro] = r.err
			continue
		}
		data[r.metro] = r.quotas
	}

	if len(data) == 0 && len(errorsByMetro) > 0 {
		return multiMetroResult{data: data, errors: errorsByMetro, tabs: tabs, userName: userName}
	}

	return multiMetroResult{
		data:     data,
		errors:   errorsByMetro,
		tabs:     tabs,
		userName: userName,
	}
}
