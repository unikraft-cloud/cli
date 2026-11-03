// SPDX-License-Identifier: MIT
// Copyright (c) 2017, Eyal Posener
//
// This code is originally copied from the complete project.
// (the original functions are not exported).
// https://github.com/posener/complete/blob/29f43e246ec41ee311a0a9bc24b15cb4ece4e513/complete.go#L84-L97
// https://github.com/posener/complete/blob/f7264fe38585e1c6efe68ea27bc27747fc21cf84/args.go#L44-L60

package cmd

import (
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/posener/complete"
)

const (
	envLine  = "COMP_LINE"
	envPoint = "COMP_POINT"
)

func getEnv() (line string, point int, ok bool) {
	line = os.Getenv(envLine)
	if line == "" {
		return
	}
	point, err := strconv.Atoi(os.Getenv(envPoint))
	if err != nil {
		// If failed parsing point for some reason, set it to point
		// on the end of the line.
		point = len(line)
	}
	return line, point, true
}

func newArgs(line string) complete.Args {
	var (
		all       []string
		completed []string
	)
	parts := splitFields(line)
	if len(parts) > 0 {
		all = parts[1:]
		completed = removeLast(parts[1:])
	}
	return complete.Args{
		All:           all,
		Completed:     completed,
		Last:          last(parts),
		LastCompleted: last(completed),
	}
}

// splitFields returns a list of fields from the given command line.
// If the last character is space, it appends an empty field in the end
// indicating that the field before it was completed.
// If the last field is of the form "a=b", it splits it to two fields: "a", "b",
// So it can be completed.
func splitFields(line string) []string {
	parts := strings.Fields(line)

	// Add empty field if the last field was completed.
	if len(line) > 0 && unicode.IsSpace(rune(line[len(line)-1])) {
		parts = append(parts, "")
	}

	// Treat the last field if it is of the form "a=b"
	parts = splitLastEqual(parts)
	return parts
}

func splitLastEqual(line []string) []string {
	if len(line) == 0 {
		return line
	}
	parts := strings.Split(line[len(line)-1], "=")
	return append(line[:len(line)-1], parts...)
}

func removeLast(a []string) []string {
	if len(a) > 0 {
		return a[:len(a)-1]
	}
	return a
}

func last(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[len(args)-1]
}
