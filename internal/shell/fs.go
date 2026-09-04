// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package shell

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"mvdan.cc/sh/v3/interp"
)

const (
	statScript = `p=$1
if [ -n "$2" ] && [ -L "$p" ]; then k=L; n=0
elif [ -d "$p" ]; then k=d; n=0
elif [ -f "$p" ]; then k=f; n=$(wc -c < "$p")
elif [ -p "$p" ]; then k=p; n=0
elif [ -S "$p" ]; then k=S; n=0
elif [ -b "$p" ]; then k=b; n=0
elif [ -c "$p" ]; then k=c; n=0
elif [ -e "$p" ]; then k=o; n=0
else exit 1
fi
m=
[ -u "$p" ] && m=${m}u
[ -g "$p" ] && m=${m}g
[ -k "$p" ] && m=${m}k
echo "$k $n $m"`

	accessScript = `p=$1; shift
[ -e "$p" ] || { echo missing; exit 0; }
for t in "$@"; do
[ "$t" "$p" ] || { echo denied; exit 0; }
done
echo ok`

	readDirScript = `d=$1
[ -e "$d" ] || { echo missing; exit 0; }
[ -d "$d" ] || { echo notdir; exit 0; }
cd -- "$d" 2>/dev/null || { echo denied; exit 0; }
l=$(ls -1A 2>/dev/null) || { echo denied; exit 0; }
echo ok
[ -n "$l" ] || exit 0
printf '%s\n' "$l" | while IFS= read -r e; do
if [ -d "$e" ]; then printf 'd %s\n' "$e"; else printf 'f %s\n' "$e"; fi
done`

	probeDir = "/"

	readScript = `cat -- "$1"`

	writeScript  = `{ echo ok; cat >&3; } 3> "$1" || { echo no; exit 1; }`
	appendScript = `{ echo ok; cat >&3; } 3>> "$1" || { echo no; exit 1; }`

	writeAck  = "ok\n"
	readAhead = 32 << 10

	unknownID = math.MaxUint32
)

func (s *session) statHandler(ctx context.Context, name string, followSymlinks bool) (fs.FileInfo, error) {
	return s.stat(ctx, interp.HandlerCtx(ctx).Dir, name, followSymlinks)
}

func (s *session) stat(ctx context.Context, dir, name string, followSymlinks bool) (fs.FileInfo, error) {
	p := resolve(dir, name)

	lstat := ""
	if !followSymlinks {
		lstat = "1"
	}

	out, err := s.script(ctx, statScript, p, lstat)
	if err != nil {
		return nil, &fs.PathError{Op: "stat", Path: p, Err: err}
	}

	info := remoteFileInfo{name: path.Base(p)}
	fields := strings.Fields(out)
	if len(fields) > 0 {
		info.kind = fields[0]
	}
	if len(fields) > 1 {
		info.size, _ = strconv.ParseInt(fields[1], 10, 64)
	}
	if len(fields) > 2 {
		info.special = fields[2]
	}
	return info, nil
}

func (s *session) accessHandler(ctx context.Context, name string, mode interp.AccessMode) error {
	return s.access(ctx, interp.HandlerCtx(ctx).Dir, name, mode)
}

func (s *session) access(ctx context.Context, dir, name string, mode interp.AccessMode) error {
	p := resolve(dir, name)

	var tests []string
	if mode&interp.AccessRead != 0 {
		tests = append(tests, "-r")
	}
	if mode&interp.AccessWrite != 0 {
		tests = append(tests, "-w")
	}
	if mode&interp.AccessExec != 0 {
		tests = append(tests, "-x")
	}

	out, err := s.script(ctx, accessScript, append([]string{p}, tests...)...)
	if err != nil {
		return &fs.PathError{Op: "access", Path: p, Err: err}
	}

	switch strings.TrimSpace(out) {
	case "ok":
		return nil
	case "missing":
		return &fs.PathError{Op: "access", Path: p, Err: fs.ErrNotExist}
	default:
		return &fs.PathError{Op: "access", Path: p, Err: fs.ErrPermission}
	}
}

func (s *session) readDirHandler(ctx context.Context, name string) ([]fs.DirEntry, error) {
	return s.readDir(ctx, interp.HandlerCtx(ctx).Dir, name)
}

func (s *session) readDir(ctx context.Context, dir, name string) ([]fs.DirEntry, error) {
	p := resolve(dir, name)

	out, err := s.script(ctx, readDirScript, p)
	if err != nil {
		return nil, &fs.PathError{Op: "readdir", Path: p, Err: err}
	}

	status, listing, _ := strings.Cut(out, "\n")
	switch strings.TrimSpace(status) {
	case "ok":
	case "notdir":
		return nil, &fs.PathError{Op: "readdir", Path: p, Err: syscall.ENOTDIR}
	case "denied":
		return nil, &fs.PathError{Op: "readdir", Path: p, Err: fs.ErrPermission}
	default:
		return nil, &fs.PathError{Op: "readdir", Path: p, Err: fs.ErrNotExist}
	}

	var entries []fs.DirEntry
	for line := range strings.SplitSeq(listing, "\n") {
		kind, entry, ok := strings.Cut(strings.TrimRight(line, "\r"), " ")
		if !ok || entry == "" {
			continue
		}
		entries = append(entries, remoteDirEntry{remoteFileInfo{name: entry, kind: kind}})
	}
	return entries, nil
}

func (s *session) open(ctx context.Context, name string, flag int, _ os.FileMode) (io.ReadWriteCloser, error) {
	if name == os.DevNull {
		return devNull{}, nil
	}
	p := resolve(interp.HandlerCtx(ctx).Dir, name)

	if flag&(os.O_WRONLY|os.O_RDWR|os.O_APPEND|os.O_CREATE|os.O_TRUNC) == 0 {
		return s.openRead(ctx, p)
	}
	return s.openWrite(ctx, p, flag&os.O_APPEND != 0)
}

func (s *session) openRead(ctx context.Context, p string) (io.ReadWriteCloser, error) {
	pr, pw := io.Pipe()
	go func() {
		_ = pw.CloseWithError(s.redirect(ctx, "open", p, readScript, Streams{Out: pw}))
	}()

	head := make([]byte, readAhead)
	n, err := pr.Read(head)
	switch {
	case err == nil, errors.Is(err, io.EOF):
		return &remoteReader{r: pr, head: head[:n]}, nil
	default:
		_ = pr.CloseWithError(err)
		return nil, err
	}
}

func (s *session) openWrite(ctx context.Context, p string, appending bool) (io.ReadWriteCloser, error) {
	snippet := writeScript
	if appending {
		snippet = appendScript
	}

	pr, pw := io.Pipe()
	ackR, ackW := io.Pipe()
	w := &remoteWriter{w: pw, s: s, done: make(chan error, 1)}
	go func() {
		err := s.redirect(ctx, "write", p, snippet, Streams{In: pr, Out: ackW})
		_ = pr.CloseWithError(err)
		_ = ackW.CloseWithError(err)
		w.done <- err
	}()

	ack := make([]byte, len(writeAck))
	if _, err := io.ReadFull(ackR, ack); err != nil || string(ack) != writeAck {
		_ = pw.Close()
		switch failed := <-w.done; {
		case ctx.Err() != nil:
			return devNull{}, nil
		case failed != nil:
			return nil, failed
		default:
			return nil, &fs.PathError{Op: "write", Path: p, Err: fs.ErrInvalid}
		}
	}

	go func() { _, _ = io.Copy(io.Discard, ackR) }()
	return w, nil
}

func (s *session) redirect(ctx context.Context, op, p, snippet string, streams Streams) error {
	var errOut bytes.Buffer
	streams.Err = &errOut

	code, err := s.cfg.Transport.Exec(ctx, streams, probeDir, nil,
		[]string{"sh", "-c", snippet, "sh", p})
	switch {
	case err != nil:
		return &fs.PathError{Op: op, Path: p, Err: err}
	case ctx.Err() != nil, code < 0:
		return nil
	case code != 0:
		return &fs.PathError{Op: op, Path: p, Err: redirectError(&errOut)}
	default:
		return nil
	}
}

func redirectError(errOut *bytes.Buffer) error {
	said := strings.TrimSpace(errOut.String())
	if _, detail, ok := strings.Cut(said, ": "); ok {
		said = detail
	}
	if said == "" {
		return fs.ErrInvalid
	}
	return errors.New(said)
}

func (s *session) script(ctx context.Context, snippet string, args ...string) (string, error) {
	var out, errOut bytes.Buffer

	ctx = Detached(ctx)

	code, err := s.cfg.Transport.Exec(ctx, Streams{Out: &out, Err: &errOut}, probeDir, nil,
		append([]string{"sh", "-c", snippet, "sh"}, args...))
	switch {
	case err != nil:
		return "", err
	case code < 0:
		return "", fmt.Errorf("the probe was signalled (%d)", -code)
	case code != 0:
		return "", fs.ErrNotExist
	default:
		return out.String(), nil
	}
}

// remoteReader streams a file off the instance, head first: what the open had to read.
type remoteReader struct {
	r    *io.PipeReader
	head []byte
}

func (f *remoteReader) Read(p []byte) (int, error) {
	if len(f.head) > 0 {
		n := copy(p, f.head)
		f.head = f.head[n:]
		return n, nil
	}
	return f.r.Read(p)
}

func (f *remoteReader) Write([]byte) (int, error) { return 0, fs.ErrInvalid }

func (f *remoteReader) Close() error { return f.r.Close() }

// remoteWriter streams a redirection onto the instance.
type remoteWriter struct {
	w    *io.PipeWriter
	s    *session
	done chan error
}

func (f *remoteWriter) Read([]byte) (int, error) { return 0, fs.ErrInvalid }

func (f *remoteWriter) Write(p []byte) (int, error) { return f.w.Write(p) }

func (f *remoteWriter) Close() error {
	_ = f.w.Close()

	err := <-f.done
	if err != nil {
		fmt.Fprintln(f.s.console.Err, errorStyle.Render(err.Error()))
	}
	return err
}

type devNull struct{}

func (devNull) Read([]byte) (int, error)    { return 0, io.EOF }
func (devNull) Write(p []byte) (int, error) { return len(p), nil }
func (devNull) Close() error                { return nil }

// remoteFileInfo answers what statScript reported, kind being its one-letter file type.
type remoteFileInfo struct {
	name    string
	size    int64
	kind    string
	special string
}

func (f remoteFileInfo) Name() string { return f.name }
func (f remoteFileInfo) Size() int64  { return f.size }
func (f remoteFileInfo) IsDir() bool  { return f.kind == "d" }

func (f remoteFileInfo) Sys() any { return &syscall.Stat_t{Uid: unknownID, Gid: unknownID} }

func (f remoteFileInfo) ModTime() time.Time { return time.Time{} }

func (f remoteFileInfo) Mode() fs.FileMode {
	var mode fs.FileMode
	switch f.kind {
	case "d":
		mode = fs.ModeDir | 0o755
	case "L":
		mode = fs.ModeSymlink | 0o777
	case "p":
		mode = fs.ModeNamedPipe | 0o644
	case "S":
		mode = fs.ModeSocket | 0o644
	case "b":
		mode = fs.ModeDevice | 0o644
	case "c":
		mode = fs.ModeDevice | fs.ModeCharDevice | 0o644
	case "f":
		mode = 0o644
	default:
		mode = fs.ModeIrregular | 0o644
	}

	for _, bit := range f.special {
		switch bit {
		case 'u':
			mode |= fs.ModeSetuid
		case 'g':
			mode |= fs.ModeSetgid
		case 'k':
			mode |= fs.ModeSticky
		}
	}
	return mode
}

type remoteDirEntry struct{ info remoteFileInfo }

func (e remoteDirEntry) Name() string               { return e.info.Name() }
func (e remoteDirEntry) IsDir() bool                { return e.info.IsDir() }
func (e remoteDirEntry) Type() fs.FileMode          { return e.info.Mode().Type() }
func (e remoteDirEntry) Info() (fs.FileInfo, error) { return e.info, nil }
