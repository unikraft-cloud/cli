// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"github.com/alecthomas/kong"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"sigs.k8s.io/yaml"

	"unikraft.com/cli/internal/resource"
	"unikraft.com/cli/internal/resource/cmd"
	"unikraft.com/cli/internal/tablewriter"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"
)

// MdxCmd generates markdown documentation for the CLI.
type MdxCmd struct {
	Outdir string `arg:"" required:"" help:"Output directory for generated documentation."`
}

func (c *MdxCmd) Run(ctx context.Context) error {
	compat.Profile = colorprofile.NoTTY
	lipgloss.Writer.Profile = colorprofile.NoTTY
	_ = os.Setenv("NO_COLOR", "1")

	if err := os.MkdirAll(c.Outdir, 0o775); err != nil {
		return fmt.Errorf("could not create parent directories: %w", err)
	}

	parser, err := CreateParser()
	if err != nil {
		return fmt.Errorf("could not create parser: %w", err)
	}

	for child := range IterChildren(parser.Model.Node) {
		if err := generateMarkdown(ctx, child, c.Outdir); err != nil {
			return err
		}
	}
	log.G(ctx).Info().Str("dir", c.Outdir).Msg("Generated documentation")
	return nil
}

func generateMarkdown(ctx context.Context, node *kong.Node, dir string) error {
	buf := new(bytes.Buffer)
	name := NodePath(node)
	help := strings.TrimSuffix(node.Help, "\n")
	if idx := strings.LastIndex(help, "\n"); idx != -1 {
		lastLine := help[idx+1:]
		if strings.TrimSpace(lastLine) == "" {
			help = strings.TrimSuffix(help, "\n"+lastLine)
		}
	}

	frontmatter := struct {
		Title       string `json:"title"`
		Description string `json:"description,omitempty"`
	}{
		Title:       name,
		Description: help,
	}
	frontmatterBytes, err := yaml.Marshal(frontmatter)
	if err != nil {
		return fmt.Errorf("could not marshal frontmatter for %s: %w", name, err)
	}

	buf.WriteString("---\n")
	buf.Write(frontmatterBytes)
	buf.WriteString("---\n\n")

	description := help
	if node.Parent != nil && node.Detail != "" {
		description = node.Detail
	}
	if description != "" {
		buf.WriteString(escapeMdx(description) + "\n\n")
	}

	if IsRunnable(node) {
		fmt.Fprintf(buf, "## Usage\n\n```\n%s\n```\n\n", ansi.Strip(kingkong.Summary(node)))
	}

	printDocsExamples(buf, node)

	printDocsOptions(buf, node)

	if target, ok := node.Target.Interface().(cmd.ResourceCmdInterface); ok {
		src := target.Underlying()
		fields, err := src.Fields(ctx)
		if err != nil {
			return fmt.Errorf("could not get fields for %s: %w", name, err)
		}
		fields, _ = resource.FilterFieldsByPath(fields, nil, true)
		printDocsFields(buf, fields)
	}

	hasSeeAlso := false
	basename := strings.ReplaceAll(name, " ", "/") + ".mdx"
	for related := range SeeAlso(node) {
		if !hasSeeAlso {
			buf.WriteString("## See Also\n\n")
			hasSeeAlso = true
		}

		relatedName := NodePath(related)
		link := strings.ReplaceAll(relatedName, " ", "/") + ".mdx"
		link, err := filepath.Rel(filepath.Dir(basename), link)
		if err != nil {
			return err
		}
		fmt.Fprintf(buf, "* [`%s`](%s): %s\n", relatedName, link, escapeMdx(related.Help))
	}
	if hasSeeAlso {
		buf.WriteString("\n")
	}

	filename := filepath.Join(dir, basename)
	log.G(ctx).Debug().Str("dir", filepath.Dir(filename)).Msg("mkdir")

	if err := os.MkdirAll(filepath.Dir(filename), 0o775); err != nil {
		return err
	}

	log.G(ctx).Debug().Str("file", filename).Msg("write")

	w, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer func() {
		if w != nil {
			w.Close()
		}
	}()

	_, err = buf.WriteTo(w)
	if err != nil {
		return err
	}

	err = w.Close()
	w = nil
	return err
}

func printDocsExamples(buf *bytes.Buffer, node *kong.Node) {
	examples := ExamplesForNode(node)
	if len(examples) == 0 {
		return
	}

	buf.WriteString("## Examples\n\n")
	for _, example := range examples {
		if example.Description != "" {
			buf.WriteString(escapeMdx(example.Description) + ":\n\n")
		}
		buf.WriteString("```bash\n")
		buf.WriteString(formatExampleCommands(example))
		buf.WriteString("\n```\n\n")
	}
}

func printDocsOptions(buf *bytes.Buffer, node *kong.Node) {
	localFlags := CollectLocalFlags(node)
	if len(localFlags) > 0 {
		buf.WriteString("## Options\n\n```\n")
		for _, f := range localFlags {
			buf.WriteString(FormatFlag(f) + "\n")
		}
		buf.WriteString("```\n\n")
	}

	inheritedFlags := CollectInheritedFlags(node)
	if len(inheritedFlags) > 0 {
		buf.WriteString("## Options inherited from parent commands\n\n```\n")
		for _, f := range inheritedFlags {
			buf.WriteString(FormatFlag(f) + "\n")
		}
		buf.WriteString("```\n\n")
	}
}

func printDocsFields(buf *bytes.Buffer, fields []resource.Field) {
	w := tablewriter.TableWriter(buf)
	defer w.Flush()

	fmt.Fprintln(w, "## Resource Fields")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| Field | Type | Operations |")
	fmt.Fprintln(w, "|-------|------|------------|")
	for path, field := range resource.IterFields(fields) {
		var typeStr string
		if field.Value != nil {
			typeStr = formatType(reflect.TypeOf(field.Value))
			typeStr = strings.ToLower(typeStr)
			typeStr = fmt.Sprintf("`%s`", typeStr)
		}

		operations := []string{}
		if field.Create != nil {
			operations = append(operations, "create")
		}
		if field.Edit != nil {
			operations = append(operations, "edit")
		}

		fmt.Fprintf(w, "| `%s` | %s | %s |\n", path, typeStr, strings.Join(operations, "/"))
	}
	fmt.Fprintln(w)
}

func formatType(tp reflect.Type) string {
	switch tp.Kind() {
	case reflect.Pointer:
		return formatType(tp.Elem())
	case reflect.Slice:
		return "[" + formatType(tp.Elem()) + "]"
	case reflect.Map:
		return "{" + formatType(tp.Key()) + ": " + formatType(tp.Elem()) + "}"
	case reflect.Struct:
		return tp.Name()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "integer"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "float"
	default:
		return tp.Kind().String()
	}
}
