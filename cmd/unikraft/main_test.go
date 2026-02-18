// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2025, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/lunixbochs/vtclean"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gotest.tools/v3/golden"
	"mvdan.cc/sh/v3/shell"

	"unikraft.com/x/log"

	"unikraft.com/cli/internal/cmd"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/resource"
)

const unikraftCmd = "unikraft"

type testCase struct {
	name     string
	commands []command
	online   bool
	cleaners []cleaner
}

type command struct {
	args       []string
	allowErr   bool
	captureEnv string
}

var testCases []testCase

var (
	testToken  string
	testMetros []string
)

const (
	defaultMetro = "test"
)

func TestGolden(t *testing.T) {
	if len(testCases) == 0 {
		t.Skip("no test cases")
	}
	t.Parallel()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.online && testToken == "" {
				t.Skip("skipping online test that requires UKC_TOKEN")
			}
			if tc.online && len(testMetros) == 0 {
				t.Skip("skipping online test that requires UKC_METRO/UKC_METROS")
			}

			ctx := t.Context()
			ctx = log.WithLogger(ctx, log.New(t.Output(), log.TextType, log.TraceLevel))

			assert.NotEmpty(t, tc.commands, "no commands specified")

			configPath := filepath.Join(t.TempDir(), "config.yaml")
			cfg, profile := defaultCfg()
			if cfg != nil {
				cfg.Path = configPath
				require.NoError(t, cfg.Save())
			}

			sandboxPath := filepath.Join(t.TempDir(), "sandbox.json")
			t.Cleanup(func() {
				cfg, err := config.Load(configPath)
				ctx := ctx
				if cfg != nil {
					require.NoError(t, err)
					ctx = config.WithConfig(ctx, cfg)
				}

				sandbox, err := resource.LoadSandbox(sandboxPath, cmd.SandboxedResources...)
				require.NoError(t, err)
				require.NotNil(t, sandbox)
				require.NoError(t, sandbox.Teardown(context.WithoutCancel(ctx)))
			})

			expander := &expander{}
			output := strings.Builder{}
			for i, command := range tc.commands {
				require.NotEmpty(t, command.args, "no command specified")
				var args []string
				if command.args[0] == unikraftCmd {
					args = append(args, "go", "run", ".")
					args = append(args, command.args[1:]...)
				} else {
					args = command.args
				}
				args = expander.expandArgs(args)

				log.G(ctx).Debug().
					Strs("args", args).
					Msg("executing command")

				cmd := exec.CommandContext(ctx, args[0], args[1:]...)
				var stdout, stderr bytes.Buffer
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr
				cmd.Env = os.Environ()
				cmd.Env = slices.DeleteFunc(cmd.Env, func(s string) bool {
					return strings.HasPrefix(s, "UNIKRAFT_")
				})
				cmd.Env = append(cmd.Env, "NO_COLOR=1") // color makes golden files harder to read
				cmd.Env = append(cmd.Env, "UNIKRAFT_CONFIG="+configPath)
				cmd.Env = append(cmd.Env, resource.UnikraftSandboxEnv+"="+sandboxPath)

				err := cmd.Run()
				if command.captureEnv != "" {
					value := strings.TrimSpace(stdout.String())
					if value == "" {
						value = strings.TrimSpace(stderr.String())
					}
					if value != "" {
						if expander.env == nil {
							expander.env = make(map[string]string)
						}
						expander.env[command.captureEnv] = value
					}
				}
				var exitErr *exec.ExitError
				var exitCode int
				if errors.As(err, &exitErr) && command.allowErr {
					exitCode = exitErr.ExitCode()
					// ignore exit errors for help commands
					err = nil
				}
				require.NoError(t, err, "command %q failed\nstdout:\n%s\nstderr:\n%s",
					strings.Join(args, " "),
					stdout.String(),
					stderr.String(),
				)

				report := report{
					args:       command.args,
					stdout:     stdout.String(),
					stderr:     stderr.String(),
					exitCode:   exitCode,
					captureEnv: command.captureEnv,
				}

				report.cleaners = append(report.cleaners, tc.cleaners...)
				report.cleaners = append(report.cleaners, expander.cleaners()...)
				if profile != nil {
					for _, metro := range profile.Metros {
						report.cleaners = append(
							report.cleaners,
							cleaner{
								pattern: regexp.MustCompile(regexp.QuoteMeta(metro.Endpoint.String())),
								repl:    "https://api." + metro.Name.String() + ".unikraft.internal/",
							},
							cleaner{
								pattern: regexp.MustCompile(regexp.QuoteMeta(metro.Index().Host)),
								repl:    "index." + metro.Name.String() + ".unikraft.internal",
							},
						)
					}
				}
				if i != 0 {
					output.WriteString("\n")
				}
				output.WriteString(report.String())
			}

			golden.Assert(t, output.String(), t.Name(), "\n"+output.String())
		})
	}
}

type report struct {
	args       []string
	stdout     string
	stderr     string
	exitCode   int
	captureEnv string
	cleaners   []cleaner
}

func (report *report) String() string {
	out := strings.Builder{}

	cmd := strings.Join(formatArgs(report.args), " ")
	if report.captureEnv != "" {
		cmd = report.captureEnv + "=$(" + cmd + ")"
	}
	out.WriteString("$ " + cmd + "\n\n")
	if report.captureEnv == "" {
		stdout := report.cleanOutput(report.stdout)
		if len(stdout) > 0 {
			out.WriteString("stdout:\n" + indent(stdout, "\t") + "\n\n")
		}
		stderr := report.cleanOutput(report.stderr)
		if len(stderr) > 0 {
			out.WriteString("stderr:\n" + indent(stderr, "\t") + "\n\n")
		}
		if report.exitCode != 0 {
			out.WriteString("exit code: " + strconv.Itoa(report.exitCode) + "\n\n")
		}
	}

	return strings.TrimSpace(out.String()) + "\n"
}

func (report *report) cleanOutput(s string) string {
	// remove ANSI escape sequences
	s = vtclean.Clean(s, false)
	s = strings.TrimRightFunc(s, unicode.IsSpace)
	if s == "" {
		return ""
	}

	// apply any necessary cleanup to the output here
	for _, c := range report.cleaners {
		s = c.pattern.ReplaceAllString(s, c.repl)
	}
	for _, c := range cleaners {
		s = c.pattern.ReplaceAllString(s, c.repl)
	}

	return s
}

func formatArgs(args []string) []string {
	formatted := make([]string, 0, len(args))
	for _, arg := range args {
		formatted = append(formatted, quoteArg(arg))
	}
	return formatted
}

func quoteArg(arg string) string {
	if arg == "" {
		return "''"
	}
	if strings.ContainsAny(arg, " \t\n{}()") {
		return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
	}
	return arg
}

func indent(s string, indent string) string {
	result := strings.Builder{}
	for line := range strings.Lines(s) {
		if len(strings.TrimSpace(line)) > 0 {
			result.WriteString(indent)
		}
		result.WriteString(line)
	}
	return result.String()
}

type cleaner struct {
	pattern *regexp.Regexp
	repl    string
}

// cleaners are patterns applied to command output to normalize variable data
// so we get consistent golden files.
var cleaners = []cleaner{
	{
		// datetimes like "2000-01-02T12:34:56+01:00" change between runs
		pattern: regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|(\+\d{2}:\d{2}))?\b`),
		repl:    "YYYY-MM-DDTHH:MM:SSZ",
	},
	{
		// times like "12:34:56" or "12:34:56PM" change between runs
		pattern: regexp.MustCompile(`\b\d\d?:\d\d:\d\d([AP]M)?\b`),
		repl:    "HH:MM:SS",
	},
	{
		// times like "12:34" or "12:34PM" change between runs
		pattern: regexp.MustCompile(`\b\d\d?:\d\d?([AP]M)?\b`),
		repl:    "HH:MM",
	},
	{
		// dates like "2000-01-02" change between runs
		pattern: regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}\b`),
		repl:    "YYYY-MM-DD",
	},
	{
		// relative times like "just now", "2 hours ago", or "in 5 minutes" change between runs
		pattern: regexp.MustCompile(`\bjust now\b|\b(?:in\s+)?\d+\s+(?:minute|minutes|hour|hours|day|days|week|weeks|month|months|year|years)(?:\s+ago)?\b`),
		repl:    "RELATIVE_TIME",
	},
	{
		// runtime versions like "go1.25.4" change between go releases
		pattern: wordCleaner(runtime.Version()),
		repl:    "goX.Y.Z",
	},
	{
		// platforms like "linux/amd64" change between systems
		pattern: wordCleanerf("%s/%s", runtime.GOOS, runtime.GOARCH),
		repl:    "GOOS/GOARCH",
	},
	{
		// uuids like "12345678-1234-1234-1234-123456789abc" change between runs
		pattern: regexp.MustCompile(`\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`),
		repl:    "12345678-1234-1234-1234-123456789abc",
	},
	{
		// image digests like "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890" may change between runs
		pattern: regexp.MustCompile(`\bsha256:[0-9a-f]{64}\b`),
		repl:    "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
	},
}

func wordCleaner(word string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(word) + `\b`)
}

func wordCleanerf(word string, args ...any) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(fmt.Sprintf(word, args...)) + `\b`)
}

type expander struct {
	uniq  map[string]string
	certs map[string]*generatedCert
	env   map[string]string
}

type generatedCert struct {
	cn    string
	chain string
	key   string
}

func (e *expander) expandArgs(args []string) []string {
	if e.uniq == nil {
		e.uniq = make(map[string]string)
	}
	if e.certs == nil {
		e.certs = make(map[string]*generatedCert)
	}
	if e.env == nil {
		e.env = make(map[string]string)
	}
	expanded := make([]string, 0, len(args))
	for _, arg := range args {
		arg, err := shell.Expand(arg, func(varname string) string {
			if val, ok := e.env[varname]; ok {
				return val
			}
			prefix, rest, ok := strings.Cut(varname, "_")
			if !ok {
				return ""
			}
			switch prefix {
			case "UNIQ":
				if val, ok := e.uniq[rest]; ok {
					return val
				}
				result := fmt.Sprintf("%x", rand.Text())[:12]
				e.uniq[rest] = result
				return result
			case "CERT":
				name, field, ok := strings.Cut(rest, "_")
				if !ok {
					return ""
				}
				cert, ok := e.certs[name]
				if !ok {
					cert = generateCert()
					e.certs[name] = cert
				}
				switch field {
				case "CN":
					return cert.cn
				case "CHAIN":
					return cert.chain
				case "KEY":
					return cert.key
				}
			}
			return ""
		})
		if err != nil {
			panic(err)
		}
		expanded = append(expanded, arg)
	}
	return expanded
}

func (e *expander) cleaners() []cleaner {
	cleaners := make([]cleaner, 0, len(e.uniq)+len(e.certs))
	for varname, val := range e.uniq {
		cleaners = append(cleaners, cleaner{
			pattern: wordCleaner(val),
			repl:    fmt.Sprintf("<%s>", varname),
		})
	}
	for name, cert := range e.certs {
		// Clean the CN (without trailing dot)
		cn := strings.TrimSuffix(cert.cn, ".")
		cleaners = append(cleaners, cleaner{
			pattern: regexp.MustCompile(regexp.QuoteMeta(cn)),
			repl:    fmt.Sprintf("<CERT_%s_CN>", name),
		})
	}
	return cleaners
}

func defaultCfg() (*config.Config, *config.Profile) {
	if testToken == "" {
		return nil, nil
	}
	profile := &config.Profile{
		Type:  config.InterpolateString(string(config.ProfileTypeCloud)),
		Name:  "default",
		Token: config.InterpolateString(testToken),
	}
	for _, metro := range testMetros {
		profile.Metros = append(profile.Metros, config.Metro{
			Name:     config.InterpolateString(defaultMetro),
			Endpoint: config.InterpolateString(metro),
			Country:  config.InterpolateString("xx"),
		})
		break
	}
	cfg := &config.Config{
		DefaultProfile: config.InterpolateString(profile.Name),
		Profiles: map[string]config.Profile{
			profile.Name: *profile,
		},
	}
	return cfg, profile
}

func generateCert() *generatedCert {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}

	cn := fmt.Sprintf("test-%x.unikraft.io", rand.Text()[:12])
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: cn,
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		panic(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		panic(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyDER,
	})

	return &generatedCert{
		cn:    cn + ".",
		chain: string(certPEM),
		key:   string(keyPEM),
	}
}
