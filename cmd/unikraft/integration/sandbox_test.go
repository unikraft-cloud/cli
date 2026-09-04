// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unikraft.com/cloud/plugins/sandbox"

	integ "unikraft.com/cli/internal/integration"
)

const (
	sandboxPlugin = sandbox.PluginName

	sandboxKraftfile = `
spec: v0.7
name: sandbox-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source: ./Dockerfile
  type: dockerfile
cmd: ["tail", "-f", "/dev/null"]
`
)

// newSandboxInstance builds the fixture image, creates a running instance
// serving the sandbox plugin on it, and returns the instance's name.
func newSandboxInstance(t *testing.T, r *integ.TestEnv) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, fstest.Apply(
		fstest.CreateFile("Dockerfile", []byte("FROM busybox:latest\n"), 0o644),
		fstest.CreateFile("Kraftfile", []byte(sandboxKraftfile), 0o644),
	).Apply(dir))

	image := r.Config.Profile.Organization + "/sandbox-e2e:" + uniq()
	r.Run(t, []string{"unikraft", "build", ".", "--output", image}, integ.WithWorkDir(dir))

	name := "test-" + uniq()
	r.Run(t, []string{
		"unikraft", "instance", "create",
		"--output", "quiet",
		"--name", name,
		"--metro", r.Config.MetroName,
		"--image", image,
		"--plugin", "name=" + sandboxPlugin + ",rom=" + sandboxPluginRom,
		"--memory", "512",
		"--vcpus", "1",
		"--autostart",
	})
	r.Run(t, []string{"unikraft", "--timeout", "60s", "instance", "wait", "--until", "state==running", name})

	return name
}

// remoteContents reads remote off the instance and returns its contents,
// through the plugin rather than through a shell on the instance.
func remoteContents(t *testing.T, r *integ.TestEnv, instName, remote string) string {
	t.Helper()

	dir := t.TempDir()
	r.Run(t, []string{"unikraft", "instance", "read", instName, remote, "./fetched"}, integ.WithWorkDir(dir))
	data, err := os.ReadFile(filepath.Join(dir, "fetched"))
	require.NoError(t, err)

	return string(data)
}

func TestSandbox(t *testing.T) {
	// One instance covers all of these: they only run commands on it.
	t.Run("exec", func(t *testing.T) {
		r := runner(t, true, []string{staging})
		instName := newSandboxInstance(t, r)

		// Both output streams come back, stdout and stderr alike.
		out := r.Run(t, []string{"unikraft", "instance", "exec", instName, "--", "sh", "-c", "echo to-stdout; echo to-stderr >&2"})
		assert.Contains(t, out, "to-stdout")
		assert.Contains(t, out, "to-stderr")

		// The command is a command line by the time it reaches the plugin, so
		// arguments the shell would otherwise split or expand are quoted first.
		out = r.Run(t, []string{"unikraft", "instance", "exec", instName, "--", "echo", "two words", "*"})
		assert.Contains(t, out, "two words *")

		// Naming the plugin explicitly addresses the same one the default does.
		out = r.Run(t, []string{"unikraft", "instance", "exec", instName, "--plugin", sandboxPlugin, "--", "echo", "named-plugin"})
		assert.Contains(t, out, "named-plugin")

		// --dir runs the command from that directory, and --env is the whole
		// environment it sees.
		out = r.Run(t, []string{"unikraft", "instance", "exec", instName, "--dir", "/etc", "--", "pwd"})
		assert.Contains(t, out, "/etc")

		out = r.Run(t, []string{"unikraft", "instance", "exec", instName, "-e", "GREETING=hi", "-e", "WHO=exec", "--", "sh", "-c", "echo $GREETING-$WHO"})
		assert.Contains(t, out, "hi-exec")

		// Local standard input is fed to the remote command, and closing it is
		// what lets a command reading to EOF finish.
		out = r.Run(t, []string{"unikraft", "instance", "exec", instName, "--", "cat"}, integ.WithStdin("fed-by-stdin\n"))
		assert.Contains(t, out, "fed-by-stdin")

		// A file written by the shell is the same file the plugin reads, so both
		// see one filesystem.
		remote := "/sb-exec-" + uniq() + ".txt"
		r.Run(t, []string{"unikraft", "instance", "exec", instName, "--", "sh", "-c", "echo written-by-the-shell > " + remote})
		assert.Equal(t, "written-by-the-shell\n", remoteContents(t, r, instName, remote))

		r.Run(t, []string{"unikraft", "instance", "delete", instName})
	})

	t.Run("write-read", func(t *testing.T) {
		t.Run("roundtrip", func(t *testing.T) {
			r := runner(t, true, []string{staging})
			instName := newSandboxInstance(t, r)
			remote := "/sb-write-" + uniq() + ".txt"
			body := "written by the integration suite\n"
			dir := t.TempDir()
			require.NoError(t, fstest.Apply(
				fstest.CreateFile("payload.txt", []byte(body), 0o644),
			).Apply(dir))

			out := r.Run(t, []string{"unikraft", "instance", "write", instName, "./payload.txt", remote}, integ.WithWorkDir(dir))
			assert.Regexp(t, `file written`, out)

			out = r.Run(t, []string{"unikraft", "instance", "read", instName, remote, "./fetched.txt"}, integ.WithWorkDir(dir))
			assert.Regexp(t, `file read`, out)

			fetched, err := os.ReadFile(filepath.Join(dir, "fetched.txt"))
			require.NoError(t, err)
			assert.Equal(t, body, string(fetched))

			r.Run(t, []string{"unikraft", "instance", "delete", instName})
		})

		t.Run("append", func(t *testing.T) {
			r := runner(t, true, []string{staging})
			instName := newSandboxInstance(t, r)
			remote := "/sb-append-" + uniq() + ".txt"
			dir := t.TempDir()
			require.NoError(t, fstest.Apply(
				fstest.CreateFile("first.txt", []byte("first line\n"), 0o644),
				fstest.CreateFile("second.txt", []byte("second line\n"), 0o644),
			).Apply(dir))

			r.Run(t, []string{"unikraft", "instance", "write", instName, "./first.txt", remote}, integ.WithWorkDir(dir))
			r.Run(t, []string{"unikraft", "instance", "write", instName, "./second.txt", remote, "--append"}, integ.WithWorkDir(dir))
			assert.Equal(t, "first line\nsecond line\n", remoteContents(t, r, instName, remote))

			// Without --append the next write replaces the file.
			r.Run(t, []string{"unikraft", "instance", "write", instName, "./second.txt", remote}, integ.WithWorkDir(dir))
			assert.Equal(t, "second line\n", remoteContents(t, r, instName, remote))

			r.Run(t, []string{"unikraft", "instance", "delete", instName})
		})

		t.Run("parents", func(t *testing.T) {
			r := runner(t, true, []string{staging})
			instName := newSandboxInstance(t, r)
			remote := "/sb-write-" + uniq() + "/nested/payload.txt"
			dir := t.TempDir()
			require.NoError(t, fstest.Apply(
				fstest.CreateFile("payload.txt", []byte("nested\n"), 0o644),
			).Apply(dir))

			r.Run(t, []string{"unikraft", "instance", "write", instName, "./payload.txt", remote, "--parents"}, integ.WithWorkDir(dir))
			assert.Equal(t, "nested\n", remoteContents(t, r, instName, remote))

			r.Run(t, []string{"unikraft", "instance", "delete", instName})
		})

		// A read defaults to the remote file's base name, and --force overwrites
		// an existing local file.
		t.Run("local-destination", func(t *testing.T) {
			r := runner(t, true, []string{staging})
			instName := newSandboxInstance(t, r)
			remote := "/sb-read-" + uniq() + ".txt"
			dir := t.TempDir()
			require.NoError(t, fstest.Apply(
				fstest.CreateFile("payload.txt", []byte("remote contents\n"), 0o644),
			).Apply(dir))

			r.Run(t, []string{"unikraft", "instance", "write", instName, "./payload.txt", remote}, integ.WithWorkDir(dir))

			// No local path given: the remote base name, in the working directory.
			r.Run(t, []string{"unikraft", "instance", "read", instName, remote}, integ.WithWorkDir(dir))
			fetched, err := os.ReadFile(filepath.Join(dir, filepath.Base(remote)))
			require.NoError(t, err)
			assert.Equal(t, "remote contents\n", string(fetched))

			r.Run(t, []string{"unikraft", "instance", "read", instName, remote, "--force"}, integ.WithWorkDir(dir))

			// A local path naming a directory is written into.
			require.NoError(t, os.Mkdir(filepath.Join(dir, "into"), 0o755))
			r.Run(t, []string{"unikraft", "instance", "read", instName, remote, "./into"}, integ.WithWorkDir(dir))
			fetched, err = os.ReadFile(filepath.Join(dir, "into", filepath.Base(remote)))
			require.NoError(t, err)
			assert.Equal(t, "remote contents\n", string(fetched))

			r.Run(t, []string{"unikraft", "instance", "delete", instName})
		})
	})

	t.Run("copy", func(t *testing.T) {
		t.Run("upload-download", func(t *testing.T) {
			r := runner(t, true, []string{staging})
			instName := newSandboxInstance(t, r)
			remote := "/sb-copy-" + uniq() + ".txt"
			body := "copied by the integration suite\n"
			dir := t.TempDir()
			require.NoError(t, fstest.Apply(
				fstest.CreateFile("payload.txt", []byte(body), 0o644),
			).Apply(dir))

			out := r.Run(t, []string{"unikraft", "instance", "copy", "./payload.txt", instName + ":" + remote}, integ.WithWorkDir(dir))
			assert.Regexp(t, `file written`, out)

			out = r.Run(t, []string{"unikraft", "instance", "copy", instName + ":" + remote, "./fetched.txt"}, integ.WithWorkDir(dir))
			assert.Regexp(t, `file read`, out)

			fetched, err := os.ReadFile(filepath.Join(dir, "fetched.txt"))
			require.NoError(t, err)
			assert.Equal(t, body, string(fetched))

			r.Run(t, []string{"unikraft", "instance", "delete", instName})
		})

		// cp is the alias the command is reached by in scp's spelling.
		t.Run("cp-alias", func(t *testing.T) {
			r := runner(t, true, []string{staging})
			instName := newSandboxInstance(t, r)
			remote := "/sb-cp-" + uniq() + ".txt"
			dir := t.TempDir()
			require.NoError(t, fstest.Apply(
				fstest.CreateFile("payload.txt", []byte("via cp\n"), 0o644),
			).Apply(dir))

			r.Run(t, []string{"unikraft", "instance", "cp", "./payload.txt", instName + ":" + remote}, integ.WithWorkDir(dir))
			assert.Equal(t, "via cp\n", remoteContents(t, r, instName, remote))

			r.Run(t, []string{"unikraft", "instance", "delete", instName})
		})

		t.Run("into-local-directory", func(t *testing.T) {
			r := runner(t, true, []string{staging})
			instName := newSandboxInstance(t, r)
			remote := "/sb-copy-" + uniq() + ".txt"
			dir := t.TempDir()
			require.NoError(t, fstest.Apply(
				fstest.CreateFile("payload.txt", []byte("into a directory\n"), 0o644),
			).Apply(dir))
			require.NoError(t, os.Mkdir(filepath.Join(dir, "logs"), 0o755))

			r.Run(t, []string{"unikraft", "instance", "copy", "./payload.txt", instName + ":" + remote}, integ.WithWorkDir(dir))
			r.Run(t, []string{"unikraft", "instance", "copy", instName + ":" + remote, "./logs/"}, integ.WithWorkDir(dir))

			fetched, err := os.ReadFile(filepath.Join(dir, "logs", filepath.Base(remote)))
			require.NoError(t, err)
			assert.Equal(t, "into a directory\n", string(fetched))

			r.Run(t, []string{"unikraft", "instance", "delete", instName})
		})

		t.Run("remote-parents", func(t *testing.T) {
			r := runner(t, true, []string{staging})
			instName := newSandboxInstance(t, r)
			remote := "/sb-copy-" + uniq() + "/nested/payload.txt"
			dir := t.TempDir()
			require.NoError(t, fstest.Apply(
				fstest.CreateFile("payload.txt", []byte("nested copy\n"), 0o644),
			).Apply(dir))

			r.Run(t, []string{"unikraft", "instance", "copy", "./payload.txt", instName + ":" + remote, "--parents"}, integ.WithWorkDir(dir))
			assert.Equal(t, "nested copy\n", remoteContents(t, r, instName, remote))

			r.Run(t, []string{"unikraft", "instance", "delete", instName})
		})

		// A destination naming an instance but no path keeps the local base name,
		// the way "scp file host:" does.
		t.Run("destination-without-path", func(t *testing.T) {
			r := runner(t, true, []string{staging})
			instName := newSandboxInstance(t, r)
			local := "sb-copy-" + uniq() + ".txt"
			dir := t.TempDir()
			require.NoError(t, fstest.Apply(
				fstest.CreateFile(local, []byte("no remote path\n"), 0o644),
			).Apply(dir))

			out := r.Run(t, []string{"unikraft", "instance", "copy", "./" + local, instName + ":"}, integ.WithWorkDir(dir))
			assert.Regexp(t, `file written`, out)
			assert.Contains(t, out, local)

			r.Run(t, []string{"unikraft", "instance", "delete", instName})
		})

		// A target keeps its "<metro>/" qualifier and its "name:" prefix: the
		// colon those end with is not the separator.
		t.Run("qualified-target", func(t *testing.T) {
			r := runner(t, true, []string{staging})
			instName := newSandboxInstance(t, r)
			dir := t.TempDir()
			require.NoError(t, fstest.Apply(
				fstest.CreateFile("payload.txt", []byte("qualified\n"), 0o644),
			).Apply(dir))

			metroRemote := "/sb-metro-" + uniq() + ".txt"
			r.Run(t, []string{
				"unikraft", "instance", "copy",
				"./payload.txt", r.Config.MetroName + "/" + instName + ":" + metroRemote,
			}, integ.WithWorkDir(dir))
			assert.Equal(t, "qualified\n", remoteContents(t, r, instName, metroRemote))

			nameRemote := "/sb-name-" + uniq() + ".txt"
			r.Run(t, []string{
				"unikraft", "instance", "copy",
				"./payload.txt", "name:" + instName + ":" + nameRemote,
			}, integ.WithWorkDir(dir))
			assert.Equal(t, "qualified\n", remoteContents(t, r, instName, nameRemote))

			r.Run(t, []string{"unikraft", "instance", "delete", instName})
		})
	})
}
