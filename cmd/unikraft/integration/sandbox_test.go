// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package integration

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	integ "unikraft.com/cli/internal/integration"
)

const (
	// sandboxPlugin is the plugin name the sandbox commands address by default,
	// and sandboxPluginRom is the ROM serving it.
	sandboxPlugin = "sandbox"

	// TODO: sandbox.PluginROM, which the plugin's own specification declares,
	// once the ROM published there carries the cwd and environment support the
	// exec subtests need. The one it serves today accepts both and ignores them.
	sandboxPluginRom = "dragosgheorghioiu/sandbox-rom"

	// Only the Linux runtime starts plugins, so the images the rest of the
	// suite runs on (nginx:latest, base:latest) can't serve one. The plugin
	// runs commands through "/bin/sh -c", so the fixture needs a rootfs with
	// a shell on top of that runtime, and a command that holds the instance
	// open: it lives as long as its application does, and a plugin exiting
	// does not keep it up.
	sandboxKraftfile = `
spec: v0.7
name: sandbox-e2e
runtime: base-compat:latest
rootfs:
  format: erofs
  source: ./Dockerfile
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

	// The image is registered in the test's resource sandbox, which deletes it
	// when the test ends.
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
	// Copy specifications are resolved before anything is sent to an instance,
	// so these need no API.
	t.Run("copy-spec", func(t *testing.T) {
		t.Run("neither-names-an-instance", func(t *testing.T) {
			r := runner(t, false, []string{staging, stable, prod})

			out := r.Run(t, []string{"unikraft", "instance", "copy", "./a.txt", "./b.txt"}, integ.ExpectFail())
			assert.Regexp(t, `neither "\./a\.txt" nor "\./b\.txt" names an instance`, out)
			assert.Regexp(t, `written as <instance>:<path>`, out)
		})

		t.Run("both-name-instances", func(t *testing.T) {
			r := runner(t, false, []string{staging, stable, prod})

			out := r.Run(t, []string{"unikraft", "instance", "copy", "src-inst:/etc/motd", "dst-inst:/tmp/motd"}, integ.ExpectFail())
			assert.Regexp(t, `cannot copy from one instance to another`, out)
			assert.Regexp(t, `copy "src-inst:/etc/motd" to a local path first`, out)
		})

		t.Run("missing-local-source", func(t *testing.T) {
			r := runner(t, false, []string{staging, stable, prod})

			out := r.Run(t, []string{"unikraft", "instance", "copy", "./nope.txt", "dst-inst:/tmp/x"}, integ.ExpectFail())
			assert.Regexp(t, `reading local file "\./nope\.txt"`, out)
			assert.Regexp(t, `no such file or directory`, out)
		})

		t.Run("directory-source", func(t *testing.T) {
			r := runner(t, false, []string{staging, stable, prod})
			dir := t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(dir, "payload"), 0o755))

			out := r.Run(t, []string{"unikraft", "instance", "copy", "./payload", "dst-inst:/tmp/x"}, integ.ExpectFail(), integ.WithWorkDir(dir))
			assert.Regexp(t, `"\./payload" is a directory: only single files can be copied`, out)
		})

		// A local path that opens like a filesystem path is a path, even with a
		// colon in it, so a copy naming two of them names no instance at all.
		t.Run("colon-in-local-path", func(t *testing.T) {
			r := runner(t, false, []string{staging, stable, prod})

			out := r.Run(t, []string{"unikraft", "instance", "copy", "./back:up.tar", "./copy.tar"}, integ.ExpectFail())
			assert.Regexp(t, `neither "\./back:up\.tar" nor "\./copy\.tar" names an instance`, out)
		})

		// A "~/" path carrying a colon is a path too, even though its first
		// element looks like a target.
		t.Run("colon-in-home-path", func(t *testing.T) {
			r := runner(t, false, []string{staging, stable, prod})

			out := r.Run(t, []string{"unikraft", "instance", "copy", "~/back:up.tar", "./copy.tar"}, integ.ExpectFail())
			assert.Regexp(t, `neither "~/back:up\.tar" nor "\./copy\.tar" names an instance`, out)
		})

		// The specifications below name an instance, so the local source is
		// stat'd and reported missing before any request is made. That the
		// error names the local file at all is what shows the destination was
		// read as a target rather than as a second local path.
		for _, tt := range []struct {
			name string
			dst  string
		}{
			{"plain-target", "my-inst:/tmp/x"},
			{"metro-qualified-target", "fra0/my-inst:/tmp/x"},
			{"name-prefixed-target", "name:my-inst:/tmp/x"},
			{"uuid-prefixed-target", "uuid:abc123:/tmp/x"},
			{"metro-and-name-prefixed-target", "fra0/name:my-inst:/tmp/x"},
			// The separator is the first colon that isn't the prefix's own, so
			// later colons stay in the remote path.
			{"colon-in-remote-path", "my-inst:/tmp/a:b"},
			// A one-character instance name is addressable: nothing here reads
			// a lone letter as a drive.
			{"single-character-target", "a:/tmp/x"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				r := runner(t, false, []string{staging, stable, prod})

				out := r.Run(t, []string{"unikraft", "instance", "copy", "./nope.txt", tt.dst}, integ.ExpectFail())
				assert.Regexp(t, `reading local file "\./nope\.txt"`, out)
				assert.Regexp(t, `no such file or directory`, out)
			})
		}

		// A "name:" or "uuid:" prefix with no second colon carries no path, so
		// there is no separator and the whole specification is a local one.
		for _, tt := range []struct {
			name string
			src  string
		}{
			{"name-prefix-without-path", "name:my-inst"},
			{"uuid-prefix-without-path", "uuid:abc123"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				r := runner(t, false, []string{staging, stable, prod})

				out := r.Run(t, []string{"unikraft", "instance", "copy", tt.src, "./b.txt"}, integ.ExpectFail())
				assert.Regexp(t, `neither "`+regexp.QuoteMeta(tt.src)+`" nor "\./b\.txt" names an instance`, out)
			})
		}

		t.Run("write-missing-local", func(t *testing.T) {
			r := runner(t, false, []string{staging, stable, prod})

			out := r.Run(t, []string{"unikraft", "instance", "write", "dst-inst", "./nope.txt", "/tmp/x"}, integ.ExpectFail())
			assert.Regexp(t, `<local>:.*no such file or directory`, out)
		})
	})

	// The state and plugin checks happen before the plugin is contacted, so
	// these need an instance but nothing serving on it.
	t.Run("preconditions", func(t *testing.T) {
		t.Run("not-running", func(t *testing.T) {
			r := runner(t, true, []string{staging, stable})
			instName := "test-" + uniq()

			r.Run(t, []string{
				"unikraft", "instance", "create",
				"--output", "quiet",
				"--name", instName,
				"--metro", r.Config.MetroName,
				"--image", "nginx:latest",
				"--memory", "128",
				"--vcpus", "1",
			})

			out := r.Run(t, []string{"unikraft", "instance", "exec", instName, "--", "echo", "hello"}, integ.ExpectFail())
			assert.Regexp(t, `instance "`+instName+`" is not running`, out)
			assert.Regexp(t, `unikraft instance start `+instName, out)

			r.Run(t, []string{"unikraft", "instance", "delete", instName})
		})

		t.Run("no-plugins", func(t *testing.T) {
			r := runner(t, true, []string{staging, stable})
			instName := "test-" + uniq()

			r.Run(t, []string{
				"unikraft", "instance", "create",
				"--output", "quiet",
				"--name", instName,
				"--metro", r.Config.MetroName,
				"--image", "nginx:latest",
				"--memory", "128",
				"--vcpus", "1",
				"--autostart",
			})
			r.Run(t, []string{"unikraft", "--timeout", "30s", "instance", "wait", "--until", "state==running", instName})

			out := r.Run(t, []string{"unikraft", "instance", "exec", instName, "--", "echo", "hello"}, integ.ExpectFail())
			assert.Regexp(t, `instance "`+instName+`" has no plugins loaded`, out)
			assert.Regexp(t, `nothing answers to "sandbox"`, out)

			r.Run(t, []string{"unikraft", "instance", "delete", instName})
		})

		t.Run("unknown-instance", func(t *testing.T) {
			r := runner(t, true, []string{staging, stable})

			out := r.Run(t, []string{"unikraft", "instance", "exec", "test-missing-" + uniq(), "--", "echo", "hello"}, integ.ExpectFail())
			assert.Regexp(t, `(?i)not found`, out)
		})

		// The plugin an instance does carry is named in the error, and this check
		// also happens before the plugin is contacted, so it needs no shell.
		t.Run("unknown-plugin", func(t *testing.T) {
			r := runner(t, true, []string{staging})
			instName := newSandboxInstance(t, r)

			out := r.Run(t, []string{
				"unikraft", "instance", "exec", instName,
				"--plugin", "nope",
				"--", "echo", "hello",
			}, integ.ExpectFail())
			assert.Regexp(t, `instance "`+instName+`" has no plugin named "nope"`, out)
			assert.Regexp(t, `it has: `+sandboxPlugin, out)

			r.Run(t, []string{"unikraft", "instance", "delete", instName})
		})
	})

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

		out = r.Run(t, []string{"unikraft", "instance", "exec", instName, "--env", "GREETING=hi,WHO=exec", "--", "sh", "-c", "echo $GREETING-$WHO"})
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

		// --cmd-timeout bounds the wait for the command to finish, and one that
		// outlives it is reported as timed out.
		out = r.Run(t, []string{"unikraft", "instance", "exec", instName, "--cmd-timeout", "2000", "--", "sleep", "30"}, integ.ExpectFail())
		assert.Regexp(t, `timed out waiting for command to finish`, out)

		r.Run(t, []string{"unikraft", "instance", "delete", instName})
	})

	t.Run("mkdir", func(t *testing.T) {
		t.Run("create", func(t *testing.T) {
			r := runner(t, true, []string{staging})
			instName := newSandboxInstance(t, r)
			dir := "/sb-mkdir-" + uniq()
			local := t.TempDir()
			require.NoError(t, fstest.Apply(
				fstest.CreateFile("payload.txt", []byte("in a created directory\n"), 0o644),
			).Apply(local))

			out := r.Run(t, []string{"unikraft", "instance", "mkdir", instName, dir})
			assert.Contains(t, out, `created directory "`+dir+`"`)

			// A write into it only lands if the directory is really there.
			r.Run(t, []string{"unikraft", "instance", "write", instName, "./payload.txt", dir + "/payload.txt"}, integ.WithWorkDir(local))
			assert.Equal(t, "in a created directory\n", remoteContents(t, r, instName, dir+"/payload.txt"))

			r.Run(t, []string{"unikraft", "instance", "delete", instName})
		})

		t.Run("parents", func(t *testing.T) {
			r := runner(t, true, []string{staging})
			instName := newSandboxInstance(t, r)
			dir := "/sb-mkdir-" + uniq() + "/nested/deep"
			local := t.TempDir()
			require.NoError(t, fstest.Apply(
				fstest.CreateFile("payload.txt", []byte("nested\n"), 0o644),
			).Apply(local))

			// Without --parents the missing intermediate directories are an error.
			out := r.Run(t, []string{"unikraft", "instance", "mkdir", instName, dir}, integ.ExpectFail())
			assert.Regexp(t, `failed to create directory`, out)

			r.Run(t, []string{"unikraft", "instance", "mkdir", instName, dir, "--parents"})

			r.Run(t, []string{"unikraft", "instance", "write", instName, "./payload.txt", dir + "/payload.txt"}, integ.WithWorkDir(local))
			assert.Equal(t, "nested\n", remoteContents(t, r, instName, dir+"/payload.txt"))

			r.Run(t, []string{"unikraft", "instance", "delete", instName})
		})
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

			out := r.Run(t, []string{"unikraft", "instance", "write", instName, "./payload.txt", remote}, integ.ExpectFail(), integ.WithWorkDir(dir))
			assert.Regexp(t, `failed to write file`, out)

			r.Run(t, []string{"unikraft", "instance", "write", instName, "./payload.txt", remote, "--parents"}, integ.WithWorkDir(dir))
			assert.Equal(t, "nested\n", remoteContents(t, r, instName, remote))

			r.Run(t, []string{"unikraft", "instance", "delete", instName})
		})

		// A read defaults to the remote file's base name, and only overwrites an
		// existing local file when told to.
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

			// The same read again refuses to clobber it.
			out := r.Run(t, []string{"unikraft", "instance", "read", instName, remote}, integ.ExpectFail(), integ.WithWorkDir(dir))
			assert.Regexp(t, `already exists \(use --force to overwrite\)`, out)

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
