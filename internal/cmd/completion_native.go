// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

//go:build !js

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/alecthomas/kong"
	kongcompletion "github.com/jotaen/kong-completion"
)

func (c *CompletionCmd) Run(ctx *kong.Context) error {
	shell := c.Shell
	if shell == "" && runtime.GOOS == "windows" {
		// Git Bash sets SHELL; the other Windows shells are PowerShell.
		shell = "powershell"
		if sh := os.Getenv("SHELL"); sh != "" {
			shell = strings.TrimSuffix(filepath.Base(sh), ".exe")
		}
		if !slices.Contains([]string{"bash", "zsh", "fish", "powershell"}, shell) {
			return fmt.Errorf("shell %q is not supported, give one of bash, zsh, fish or powershell", shell)
		}
	}

	name := ctx.Model.Name
	var out string
	switch {
	case shell == "powershell" && c.Code:
		out = powershellCompletion(name)
	case shell == "powershell":
		out = fmt.Sprintf(""+
			"Execute the following command to activate tab completion for %[1]s in powershell:\n\n"+
			"    %[1]s %[2]s -c powershell | Out-String | Invoke-Expression\n\n"+
			"Note that this only takes effect for your current shell session. For permanent activation (beyond the current shell session), you can e.g. paste this command into your powershell’s init file, which usually is: $PROFILE",
			name, ctx.Selected().Name)
	case runtime.GOOS == "windows" && c.Code:
		// kong-completion does not quote the path, which breaks Windows backslashes and spaces.
		bin, err := os.Executable()
		if err != nil {
			return fmt.Errorf("couldn't determine absolute path to binary: %w", err)
		}
		out = shellCompletion(shell, name, filepath.ToSlash(bin))
	default:
		return (&kongcompletion.Completion{Shell: shell, Code: c.Code}).Run(ctx)
	}
	if _, err := fmt.Fprintln(ctx.Stdout, out); err != nil {
		return err
	}

	// Exit here, as kong-completion does, so that nothing else prints.
	ctx.Exit(0)
	return nil
}

// shellCompletion gives the bash, zsh or fish code of kong-completion, with
// the path to the binary quoted.
func shellCompletion(shell, name, bin string) string {
	if shell == "fish" {
		bin = "'" + strings.NewReplacer(`\`, `\\`, "'", `\'`).Replace(bin) + "'"
		return fmt.Sprintf(fishCompletionCode, name, bin)
	}

	// bash and zsh parse the -C command again when they complete, so quote it twice.
	quote := func(s string) string {
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	code := fmt.Sprintf("complete -o default -o bashdefault -C %s %s", quote(quote(bin)), name)
	if shell == "zsh" {
		code = "#compdef " + name + "\nautoload -U +X bashcompinit && bashcompinit\n" + code
	}
	return code
}

const fishCompletionCode = `function __complete_%[1]s
    set -lx COMP_LINE (commandline -cp)
    test -z (commandline -ct)
    and set COMP_LINE "$COMP_LINE "
    %[2]s
end
complete -f -c %[1]s -a "(__complete_%[1]s)"`

// powershellCompletion gives the PowerShell code that asks the binary for
// completions through COMP_LINE and COMP_POINT, as the other shells do.
func powershellCompletion(name string) string {
	quote := func(s string) string {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	return fmt.Sprintf(powershellCompletionCode, quote(name), quote(name+".exe"))
}

// The script runs the command as typed, as PowerShell reads native output in
// the OEM code page, which breaks a non-ASCII path in the script.
const powershellCompletionCode = `Register-ArgumentCompleter -Native -CommandName %[1]s, %[2]s -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)
    $point = $cursorPosition - $commandAst.Extent.StartOffset
    $line = $commandAst.Extent.Text.PadRight($point)
    $env:COMP_LINE = $line
    # COMP_POINT counts bytes, as in bash.
    $env:COMP_POINT = [Text.Encoding]::UTF8.GetByteCount($line.Substring(0, $point))
    try {
        & $commandAst.GetCommandName() 2>$null | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
        }
    } finally {
        Remove-Item Env:COMP_LINE, Env:COMP_POINT
    }
}`
