// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/alecthomas/kong"

	"unikraft.com/cli/internal/jason"
	"unikraft.com/cli/internal/resource/patch"
	xkong "unikraft.com/cli/internal/x/kong"
)

// SetValue is one --set: either a <name>=<value> pair, or a name followed by
// jason items.
type SetValue struct {
	Key   string
	Value string
	Items []jason.Item
}

// SetValues decodes repeated --set flags, taking the jason items that follow a
// bare name, as in `--set scale-to-zero policy=on cooldown-time=300`.
type SetValues []SetValue

func (s *SetValues) Decode(ctx *kong.DecodeContext) error {
	token := ctx.Scan.Pop()
	if token.IsEOL() {
		return fmt.Errorf(`missing value, expecting "<name>=<value>" or "<name> <item>..."`)
	}
	arg := fmt.Sprint(token.Value)
	if key, value, ok := strings.Cut(arg, "="); ok {
		*s = append(*s, SetValue{Key: key, Value: value})
		return nil
	}
	items := xkong.ScanItems(ctx)
	if len(items) == 0 {
		return fmt.Errorf(`expected "<name>=<value>" or "%s <item>..." but got %q`, arg, arg)
	}
	*s = append(*s, SetValue{Key: arg, Items: items})
	return nil
}

type SetArgs struct {
	Set     SetValues           `collapse:"patch-set" placeholder:"<name>=<value>" help:"Key-value pairs to set on the ${name}. Use <name> <key>=<value> ... to set a structured field by key."`
	SetFile []map[string]string `collapse:"patch-set" placeholder:"<name>=<filename>" help:"Files containing key-value pairs to set on the ${name}." sep:"none" mapsep:"none"`
}

func (args SetArgs) Apply(spec *patch.PatchSpec) error {
	if spec.Set == nil {
		spec.Set = make(map[string][]string)
	}
	for _, set := range args.Set {
		if set.Items == nil {
			spec.Set[set.Key] = append(spec.Set[set.Key], set.Value)
			continue
		}
		if spec.SetItems == nil {
			spec.SetItems = make(map[string][]jason.Item)
		}
		spec.SetItems[set.Key] = append(spec.SetItems[set.Key], set.Items...)
	}
	return appendFileArgs(spec.Set, args.SetFile, "set")
}

type AddArgs struct {
	Add     []map[string]string `collapse:"patch-add" placeholder:"<name>=<value>" help:"Key-value pairs to add to the ${name}." sep:"none" mapsep:"none"`
	AddFile []map[string]string `collapse:"patch-add" placeholder:"<name>=<filename>" help:"Files containing key-value pairs to add to the ${name}." sep:"none" mapsep:"none"`
}

func (args AddArgs) Apply(spec *patch.PatchSpec) error {
	if spec.Add == nil {
		spec.Add = make(map[string][]string)
	}
	appendArgs(spec.Add, args.Add)
	return appendFileArgs(spec.Add, args.AddFile, "add")
}

type DelArgs struct {
	Del     []map[string]string `collapse:"patch-del" placeholder:"<name>=<value>" help:"Keys to delete from the ${name}." sep:"none" mapsep:"none"`
	DelFile []map[string]string `collapse:"patch-del" placeholder:"<name>=<filename>" help:"Files containing keys to delete from the ${name}." sep:"none" mapsep:"none"`
}

func (args DelArgs) Apply(spec *patch.PatchSpec) error {
	if spec.Del == nil {
		spec.Del = make(map[string][]string)
	}
	appendArgs(spec.Del, args.Del)
	return appendFileArgs(spec.Del, args.DelFile, "del")
}

func appendArgs(target map[string][]string, args []map[string]string) {
	for _, m := range args {
		for k, v := range m {
			target[k] = append(target[k], v)
		}
	}
}

func appendFileArgs(target map[string][]string, args []map[string]string, op string) error {
	for _, m := range args {
		for k, path := range m {
			f, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("failed to open %s file for key %s: %w", op, k, err)
			}
			data, readErr := io.ReadAll(f)
			closeErr := f.Close()
			if readErr != nil {
				return fmt.Errorf("failed to read %s file for key %s: %w", op, k, readErr)
			}
			if closeErr != nil {
				return fmt.Errorf("failed to read %s file for key %s: %w", op, k, closeErr)
			}
			target[k] = append(target[k], strings.TrimSpace(string(data)))
		}
	}
	return nil
}
