// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package kraftfile

import (
	"bytes"
	_ "embed"
	"fmt"
	"sync"

	validator "github.com/santhosh-tekuri/jsonschema/v5"
	"sigs.k8s.io/yaml"
)

//go:embed schema.json
var schemaJSON []byte

var (
	compiledSchema *validator.Schema
	compiledErr    error
	compiledOnce   sync.Once
)

// Validate checks that the provided Kraftfile bytes conform to the schema.
func Validate(data []byte) error {
	schema, err := loadSchema()
	if err != nil {
		return err
	}

	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}

	if err := schema.Validate(doc); err != nil {
		return err
	}

	return nil
}

func loadSchema() (*validator.Schema, error) {
	compiledOnce.Do(func() {
		compiledSchema, compiledErr = buildSchema()
	})

	return compiledSchema, compiledErr
}

func buildSchema() (*validator.Schema, error) {
	compiler := validator.NewCompiler()
	const schemaResource = "kraftfile.schema.json"
	if err := compiler.AddResource(schemaResource, bytes.NewReader(schemaJSON)); err != nil {
		return nil, fmt.Errorf("register schema: %w", err)
	}
	compiled, err := compiler.Compile(schemaResource)
	if err != nil {
		return nil, fmt.Errorf("compile schema: %w", err)
	}
	return compiled, nil
}
