// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	"github.com/invopop/jsonschema"
	"unikraft.com/cli/internal/kraftfile"
	"unikraft.com/x/kingkong"
)

type CLI struct {
	Output string `name:"output" help:"Write schema to file instead of stdout." placeholder:"path"`
}

func (cli *CLI) Run() error {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: true,
		ExpandedStruct:            true,
	}

	schema := reflector.Reflect(&kraftfile.Kraftfile{})
	data, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal schema: %w", err)
	}

	data = append(data, '\n')
	if cli.Output == "" {
		_, err := os.Stdout.Write(data)
		return err
	}

	if err := os.WriteFile(cli.Output, data, 0o644); err != nil {
		return fmt.Errorf("failed to write schema: %w", err)
	}

	return nil
}

func main() {
	var cli CLI
	kctx := kong.Parse(&cli,
		kong.Name("kraftfile-schema"),
		kong.Help(kingkong.HelpPrinter("")),
		kong.Description("Generate a JSON Schema for the Kraftfile spec."),
		kong.UsageOnError(),
	)

	if err := kctx.Run(); err != nil {
		kctx.FatalIfErrorf(err)
	}
}
