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

	integ "unikraft.com/cli/internal/integration"
)

const (
	// sandboxPlugin is the plugin name the sandbox commands address by default,
	// and sandboxPluginRom is the ROM serving it.
	sandboxPlugin    = "sandbox"
	sandboxPluginRom = "plugins/sandbox:staging"

	// Plugins are started by the Linux runtime's init, which mounts the plugin
	// ROM at /uk/plugins/<name> and execs its init. The unikernel images the
	// rest of the suite runs on (nginx:latest, base:latest) have no plugin
	// framework at all, so the plugin subtests need a base-compat image.
	sandboxRuntime = "base-compat"

	// sandboxIdleCmd keeps a rootfs-less base-compat instance up. An instance
	// lives as long as its application does, and base-compat's embedded rootfs
	// holds exactly two binaries: init itself and the ukp-fs FUSE server, which
	// blocks serving its mount. A plugin exiting does not stop the instance, so
	// the plugin alone cannot hold one open.
	sandboxIdleCmd = "/opt/uk/ukp-fs /keepalive"
)

// newSandboxInstance creates a running instance serving the sandbox plugin and
// returns its name.
//
// The instance carries no rootfs, which is enough for every operation the plugin
// implements itself — files and directories — but leaves nothing for it to exec:
// the plugin runs commands through "/bin/sh -c", so covering "instance exec"
// needs an image with a shell built on top of this runtime. Remote paths must
// also stay clear of the /keepalive mount sandboxIdleCmd serves, which answers
// anything but its own control files with ENOSYS.
func newSandboxInstance(t *testing.T, r *integ.TestEnv) string {
	t.Helper()

	name := "test-" + uniq()
	r.Run(t, []string{
		"unikraft", "instance", "create",
		"--output", "quiet",
		"--name", name,
		"--metro", r.Config.MetroName,
		"--image", sandboxRuntime,
		"--args", sandboxIdleCmd,
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
