// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"bytes"
	"context"
	"encoding"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/Masterminds/sprig/v3"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"sigs.k8s.io/yaml"
	"unikraft.com/x/joinerrgroup"

	"unikraft.com/cli/internal/kvwriter"
	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/value"
	"unikraft.com/cli/internal/tabwriter"
	wslices "unikraft.com/cli/internal/w/slices"
)

type PrinterType string

const (
	PrinterTypeTable    PrinterType = "table"
	PrinterTypeKeyValue PrinterType = "kv"
	PrinterTypeJSON     PrinterType = "json"
	PrinterTypeYAML     PrinterType = "yaml"
	PrinterTypeRaw      PrinterType = "raw"
	PrinterTypeQuiet    PrinterType = "quiet"
	PrinterTypeTemplate PrinterType = "template"

	PrintTypeDebug PrinterType = "debug"
)

const (
	PrinterTableValueAvailable = "available"
	PrinterTableValueMax       = "max"
)

func (p Printer) Validate() error {
	switch p.Type {
	case PrinterTypeTable:
		switch p.Value {
		case "", PrinterTableValueAvailable, PrinterTableValueMax:
		default:
			return fmt.Errorf("table printer accepts only '' or '%s' or '%s' as value", PrinterTableValueAvailable, PrinterTableValueMax)
		}
	case PrinterTypeTemplate:
		if p.Value == "" {
			return fmt.Errorf("template printer requires a template string")
		}
	case PrinterTypeKeyValue, PrinterTypeJSON, PrinterTypeYAML, PrinterTypeRaw, PrinterTypeQuiet, PrintTypeDebug:
		if p.Value != "" {
			return fmt.Errorf("printer type %s does not accept a value", p.Type)
		}
	default:
		return fmt.Errorf("unknown printer type: %s", p.Type)
	}
	return nil
}

type Printer struct {
	Type  PrinterType
	Value string
}

var _ encoding.TextUnmarshaler = (*Printer)(nil)

func (p *Printer) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		return nil
	}

	k, v, _ := strings.Cut(string(text), "=")
	p.Type = PrinterType(k)
	p.Value = v
	return p.Validate()
}

func ParsePrinter(s string) (Printer, error) {
	pr := Printer{}
	err := pr.UnmarshalText([]byte(s))
	if err != nil {
		return Printer{}, err
	}
	return pr, nil
}

func (p Printer) WithDefault(tp PrinterType) Printer {
	if p.Type == "" {
		p.Type = tp
	}
	return p
}

func (p Printer) Print(ctx context.Context, out io.Writer, fieldSpecs []string, base resource.Resource, resources ...resource.Resource) error {
	switch p.Type {
	case "":
		return fmt.Errorf("printer type not specified")
	case PrinterTypeTable:
		opts := []tabwriter.TabWriterOpt{}
		if p.Value == "" || p.Value == PrinterTableValueAvailable {
			opts = append(opts, tabwriter.WithMaxScreenWidth())
		}
		tw := tabwriter.TabWriter(out, opts...)
		err := printTable(ctx, tw, fieldSpecs, base, resources...)
		if err != nil {
			return err
		}
		return tw.Flush()
	case PrinterTypeKeyValue:
		bw := kvwriter.KeyValueWriter(out)
		err := printKV(ctx, bw, fieldSpecs, resources...)
		if err != nil {
			return err
		}
		return bw.Flush()
	case PrinterTypeJSON:
		return printJSON(ctx, out, fieldSpecs, resources...)
	case PrinterTypeYAML:
		return printYAML(ctx, out, fieldSpecs, resources...)
	case PrinterTypeQuiet:
		return printQuiet(ctx, out, fieldSpecs, resources...)
	case PrinterTypeRaw:
		return printRaw(out, resources...)
	case PrinterTypeTemplate:
		return printTemplate(ctx, out, p.Value, resources...)
	case PrintTypeDebug:
		return printDebug(ctx, out, resources...)
	default:
		return fmt.Errorf("unknown printer type: %s", p.Type)
	}
}

func printKV(ctx context.Context, out io.Writer, specs []string, resources ...resource.Resource) error {
	if len(resources) == 0 {
		return nil
	}

	// Resolve all resources in parallel
	resolved := make([][]resource.Field, len(resources))
	eg := joinerrgroup.Group{}
	for i, res := range resources {
		eg.Go(func() error {
			fields, err := res.Fields(ctx)
			if err != nil {
				return err
			}
			fields, err = SelectFields(fields, false, resource.FieldVerbosityLong, specs)
			if err != nil {
				return err
			}
			if err := resource.ResolveAllFields(ctx, fields); err != nil {
				return err
			}
			resolved[i] = fields
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}

	for i, fields := range resolved {
		if i > 0 {
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
		fields = resource.DedupeFields(fields)
		if err := printKVFields(out, nil, fields, 0, 0); err != nil {
			return err
		}
	}
	return nil
}

func printKVFields(out io.Writer, parent *resource.Field, fields []resource.Field, current int, indent int) error {
	for i, field := range fields {
		var line bytes.Buffer
		nextCurrent := 0
		nextIndent := indent + 1
		if parent != nil && parent.Elem != nil {
			line.WriteString(strings.Repeat("  ", max(0, indent-1)))
			line.WriteString("- ")
			nextCurrent = indent
			nextIndent = indent
			if field.Value != nil {
				out, err := field.Render(value.RenderOpts{})
				if err != nil {
					return err
				}
				line.WriteString(out)
				line.WriteString("\n")
			}
		} else {
			if i == 0 {
				line.WriteString(strings.Repeat("  ", max(0, indent-current)))
			} else {
				line.WriteString(strings.Repeat("  ", indent))
			}
			line.WriteString(field.Name + ":")
			if err := printKVValue(&line, field.Value, nextIndent); err != nil {
				return err
			}
		}
		if _, err := io.Copy(out, &line); err != nil {
			return err
		}

		if field.Value == nil {
			if err := printKVFields(out, &field, field.Subfields, nextCurrent, nextIndent); err != nil {
				return err
			}
		}
	}
	return nil
}

func printKVValue(out io.Writer, v any, indent int) error {
	if v == nil {
		_, err := fmt.Fprintln(out)
		return err
	}

	// FIXME: should probably be more configurable per-field

	switch v := v.(type) {
	case map[string]string:
		_, err := fmt.Fprintln(out)
		if err != nil {
			return err
		}
		var lines []string
		for key, val := range v {
			line := fmt.Sprintf("%s%s: %s\n", strings.Repeat("  ", indent), key, val)
			lines = append(lines, line)
		}
		slices.Sort(lines)
		_, err = io.WriteString(out, strings.Join(lines, ""))
		return err
	default:
		line, err := value.Render(v, value.RenderOpts{})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, " %s\n", line)
		return err
	}
}

func printTable(ctx context.Context, out io.Writer, fieldSpecs []string, base resource.Resource, resources ...resource.Resource) error {
	headers, err := base.Fields(ctx)
	if err != nil {
		return err
	}

	headers, err = SelectFields(headers, true, resource.FieldVerbosityShort, fieldSpecs)
	if err != nil {
		return err
	}
	for i := range headers {
		headers[i] = resource.PruneFields(headers[i])
	}

	headerPaths, headerFields := wslices.Collect2(resource.IterFields(headers))

	profile := lipgloss.Writer.Profile
	color := profile != colorprofile.NoTTY && profile != colorprofile.Ascii
	headerStyle := lipgloss.NewStyle()
	if color {
		headerStyle = headerStyle.Bold(true)
	}

	firstCol := true
	for i, header := range headerFields {
		if header.HasChildren() && header.Value == nil {
			continue
		}
		path := headerPaths[i]

		if !firstCol {
			_, err := fmt.Fprint(out, "\t")
			if err != nil {
				return err
			}
		}
		firstCol = false
		name := strings.ToUpper(headerName(path))
		_, err := fmt.Fprintf(out, "%s", headerStyle.SetString(name).String())
		if err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(out)
	if err != nil {
		return err
	}

	// Resolve all resources in parallel
	resolved, err := resolveResources(ctx, resources, headerPaths)
	if err != nil {
		return err
	}

	for _, res := range resolved {
		fields, err := res.Fields(ctx)
		if err != nil {
			return err
		}

		firstCol := true
		for i, header := range headerFields {
			if header.HasChildren() && header.Value == nil {
				continue
			}
			path := headerPaths[i]

			if !firstCol {
				_, err = fmt.Fprint(out, "\t")
				if err != nil {
					return err
				}
			}
			firstCol = false

			fields := resource.GetFieldByPath(fields, path)
			for i := range fields {
				fields[i] = resource.PruneFields(fields[i])
			}
			fieldIdx := -1
			for _, field := range resource.IterFields(fields) {
				if field.Value == nil {
					continue
				}

				// Render before deciding whether to skip: a value can be the
				// reflect zero value yet still have a meaningful rendering
				// (e.g. a zero RelativeTime renders as "never"), so emptiness
				// must be judged by the rendered text, not field.IsEmpty().
				rendered, err := field.Render(value.RenderOpts{Short: true})
				if err != nil {
					return err
				}
				if rendered == "" {
					continue
				}
				fieldIdx++

				if fieldIdx > 0 {
					_, err := fmt.Fprint(out, ", ")
					if err != nil {
						return err
					}
				}

				if field.Hyperlink != "" {
					// TODO: use lipgloss styles when it supports hyperlinks
					// https://github.com/charmbracelet/lipgloss/issues/220
					if color {
						rendered = ansi.SetHyperlink(field.Hyperlink) + rendered + ansi.ResetHyperlink()
					}
				}
				_, err = fmt.Fprint(out, rendered)
				if err != nil {
					return err
				}
			}
		}
		_, err = fmt.Fprintln(out)
		if err != nil {
			return err
		}
	}

	return nil
}

func headerName(path resource.FieldPath) string {
	for _, part := range slices.Backward(path) {
		if part == "*" {
			continue
		}
		return part
	}
	return ""
}

func printQuiet(ctx context.Context, out io.Writer, specs []string, resources ...resource.Resource) error {
	if specs == nil {
		for _, res := range resources {
			if _, err := fmt.Fprintln(out, res.Key()); err != nil {
				return err
			}
		}
		return nil
	}

	if len(resources) == 0 {
		return nil
	}

	// Resolve all resources in parallel
	resolved := make([][]resource.Field, len(resources))
	eg := joinerrgroup.Group{}
	for i, res := range resources {
		eg.Go(func() error {
			fields, err := res.Fields(ctx)
			if err != nil {
				return err
			}
			fields, err = SelectFields(fields, false, resource.FieldVerbosityNone, specs)
			if err != nil {
				return err
			}
			if err := resource.ResolveAllFields(ctx, fields); err != nil {
				return err
			}
			resolved[i] = fields
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}

	for _, fields := range resolved {
		i := 0
		for _, field := range resource.IterFields(fields) {
			if field.HasChildren() && field.Value == nil {
				continue
			}
			s, err := field.Render(value.RenderOpts{Quiet: true})
			if err != nil {
				return err
			}
			if i > 0 {
				fmt.Fprint(out, " ")
			}
			fmt.Fprint(out, s)
			i++
		}
		fmt.Fprintln(out)
	}
	return nil
}

func printJSON(ctx context.Context, out io.Writer, specs []string, resources ...resource.Resource) error {
	input, err := fieldsToInput(ctx, specs, resources)
	if err != nil {
		return err
	}
	dt, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(dt))
	return err
}

func printYAML(ctx context.Context, out io.Writer, specs []string, resources ...resource.Resource) error {
	input, err := fieldsToInput(ctx, specs, resources)
	if err != nil {
		return err
	}
	dt, err := yaml.Marshal(input)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(out, string(dt))
	return err
}

func fieldsToInput(ctx context.Context, specs []string, resources []resource.Resource) ([]any, error) {
	if len(specs) == 0 {
		// json/yaml historically included every field by default, so
		// default to the equivalent of "-fall" rather than the
		// verbosity-based defaults used by kv/table/quiet.
		specs = []string{FieldSpecAll}
	}

	input := make([]any, len(resources))
	eg := joinerrgroup.Group{}
	for i, res := range resources {
		eg.Go(func() error {
			fields, err := res.Fields(ctx)
			if err != nil {
				return err
			}
			fields, err = SelectFields(fields, false, resource.FieldVerbosityLong, specs)
			if err != nil {
				return err
			}
			if err := resource.ResolveAllFields(ctx, fields); err != nil {
				return err
			}
			input[i] = resource.FieldsToMap(fields)
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	return input, nil
}

func printRaw(out io.Writer, resources ...resource.Resource) error {
	input := make([]any, 0, len(resources))
	for _, res := range resources {
		input = append(input, res.Raw())
	}
	dt, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, string(dt))
	return err
}

func printTemplate(ctx context.Context, out io.Writer, tmplStr string, resources ...resource.Resource) error {
	tmpl, err := template.New("out").
		Funcs(sprig.TxtFuncMap()).
		Parse(tmplStr)
	if err != nil {
		return err
	}

	resolved, err := resolveAllResources(ctx, resources)
	if err != nil {
		return err
	}

	for _, res := range resolved {
		fields, err := res.Fields(ctx)
		if err != nil {
			return err
		}
		input := resource.FieldsToMap(fields)
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, input); err != nil {
			return err
		}
		if _, err := io.WriteString(out, buf.String()); err != nil {
			return err
		}
		if !strings.HasSuffix(buf.String(), "\n") {
			if _, err := io.WriteString(out, "\n"); err != nil {
				return err
			}
		}
	}
	return nil
}

func printDebug(ctx context.Context, out io.Writer, resources ...resource.Resource) error {
	resolved, err := resolveAllResources(ctx, resources)
	if err != nil {
		return err
	}

	for _, res := range resolved {
		fields, err := res.Fields(ctx)
		if err != nil {
			return err
		}
		dt, err := json.MarshalIndent(fields, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(dt))
		if err != nil {
			return err
		}
	}
	return nil
}

func PrintPatches(out io.Writer, fields []resource.Field, create bool) error {
	tw := kvwriter.KeyValueWriter(
		out,
		kvwriter.WithSeparator(":=", "+=", "-="),
		kvwriter.WithAlignedSeparator(),
	)
	for path, field := range resource.IterFields(fields) {
		var patch *resource.Patch
		if create {
			patch = field.Create
		} else {
			patch = field.Edit
		}
		if patch == nil {
			continue
		}
		if patch.Set != nil {
			out, err := value.Render(patch.Set, value.RenderOpts{})
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(tw, "%s := %s\n", path.String(), out); err != nil {
				return err
			}
		}
		if patch.Add != nil {
			out, err := value.Render(patch.Add, value.RenderOpts{})
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(tw, "%s += %s\n", path.String(), out); err != nil {
				return err
			}
		}
		if patch.Del != nil {
			out, err := value.Render(patch.Del, value.RenderOpts{})
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(tw, "%s -= %s\n", path.String(), out); err != nil {
				return err
			}
		}
	}
	return tw.Flush()
}
