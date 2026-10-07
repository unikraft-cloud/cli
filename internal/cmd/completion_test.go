// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPowershellCompletion(t *testing.T) {
	code := powershellCompletion("unikraft")

	assert.Contains(t, code, `-CommandName 'unikraft', 'unikraft.exe'`, "both names complete")
	assert.Contains(t, code, `& $commandAst.GetCommandName()`, "the binary runs as typed")
	assert.Contains(t, code, "$env:COMP_LINE", "the binary reads the line as for the other shells")
	assert.Contains(t, code, "UTF8.GetByteCount", "COMP_POINT counts bytes")
}

func TestShellCompletion(t *testing.T) {
	bin := "C:/Users/O'Neil Smith/unikraft.exe"

	assert.Equal(t,
		`complete -o default -o bashdefault -C ''\''C:/Users/O'\''\'\'''\''Neil Smith/unikraft.exe'\''' unikraft`,
		shellCompletion("bash", "unikraft", bin),
		"bash parses the path twice")
	assert.Contains(t,
		shellCompletion("zsh", "unikraft", bin),
		"bashcompinit\n"+shellCompletion("bash", "unikraft", bin),
		"zsh uses the bash code")
	assert.Contains(t,
		shellCompletion("fish", "unikraft", bin),
		`    'C:/Users/O\'Neil Smith/unikraft.exe'`+"\n",
		"fish parses the path once")
}
