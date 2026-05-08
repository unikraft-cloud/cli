// SPDX-License-Identifier: BSD-3-Clause
// Copyright (c) 2026, Unikraft GmbH and The Unikraft CLI Authors.
// Licensed under the BSD-3-Clause License (the "License").
// You may not use this file except in compliance with the License.

package cmd

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	jujuerrors "github.com/juju/errors"
	"golang.org/x/mod/semver"
	"unikraft.com/x/kingkong"
	"unikraft.com/x/log"

	"unikraft.com/cli/internal/binorigin"
	"unikraft.com/cli/internal/config"
	"unikraft.com/cli/internal/httpclient"
	"unikraft.com/cli/internal/tui/progdl"
	"unikraft.com/cli/internal/version"
)

// ErrChecksumNotAvailable indicates the checksum file could not be fetched.
var ErrChecksumNotAvailable = errors.New("checksum file not available")

type UpgradeCmd struct {
	Channel string `help:"Release channel to upgrade from." default:"stable" enum:"stable,staging"`
	Force   bool   `short:"f" help:"Force upgrade even if already at latest version."`
	Version string `short:"v" help:"Upgrade to a specific version."`
	BinDir  string `help:"Directory where to install the binary. If empty, uses the current binary location."`
	BaseUrl string `help:"Base URL for fetching releases." env:"UNIKRAFT_CLI_INSTALL_URL" default:"https://pkg.unikraft.com" hidden:"true"`
}

func (UpgradeCmd) Examples() []kingkong.Example {
	return []kingkong.Example{
		{
			Description: "Upgrade to the latest stable version",
			Commands:    []string{"unikraft upgrade"},
		},
		{
			Description: "Upgrade to a specific version",
			Commands:    []string{"unikraft upgrade --version v1.2.3"},
		},
		{
			Description: "Upgrade from the staging channel",
			Commands:    []string{"unikraft upgrade --channel staging"},
		},
		{
			Description: "Force re-install even if already at the latest version",
			Commands:    []string{"unikraft upgrade --force"},
		},
	}
}

func (cmd *UpgradeCmd) Run(ctx context.Context, stdio config.Stdio) error {
	// Detect if running with sudo and warn
	if os.Getenv("SUDO_UID") != "" || os.Getenv("SUDO_GID") != "" || os.Getenv("SUDO_USER") != "" {
		if !cmd.Force {
			return jujuerrors.New("running with sudo is not recommended; use --force to dangerously override")
		}
		log.G(ctx).
			Warn().
			Msg("running with sudo is not recommended")
	}

	log.G(ctx).
		Debug().
		Msg("determining how the CLI was previously installed")

	// Detect binary origin and reject package manager installations
	origin, err := binorigin.DetectBinaryOrigin(ctx)
	if err != nil {
		log.G(ctx).
			Warn().
			Err(err).
			Msg("could not detect binary origin, proceeding with upgrade")
	} else {
		log.G(ctx).
			Debug().
			Str("origin", origin.String()).
			Msg("detected binary origin")

		switch origin {
		case binorigin.OriginAPT, binorigin.OriginRPM, binorigin.OriginAPK, binorigin.OriginBrew:
			return jujuerrors.Errorf(
				"binary was installed via %s; please use your package manager to upgrade instead:\n\n  %s",
				origin.String(),
				origin.PackageManagerCommand(),
			)
		}
	}

	var targetVersion string
	if cmd.Version != "" {
		// Use the specified version
		targetVersion = cmd.Version
		log.G(ctx).
			Info().
			Str("version", targetVersion).
			Msg("upgrading to specified version")
	} else {
		// Fetch latest version from channel
		log.G(ctx).
			Info().
			Str("channel", cmd.Channel).
			Msg("checking for updates")

		latestVersion, err := fetchLatestVersion(ctx, cmd.BaseUrl, cmd.Channel)
		if err != nil {
			return jujuerrors.Annotate(err, "fetching latest version")
		}
		targetVersion = latestVersion

		currentVersion := version.Version
		cmp := semver.Compare(currentVersion, targetVersion)
		if cmp >= 0 && !cmd.Force {
			log.G(ctx).
				Info().
				Str("version", currentVersion).
				Msg("already at latest version")
			return nil
		}

		if cmp < 0 {
			log.G(ctx).
				Info().
				Str("current", currentVersion).
				Str("latest", targetVersion).
				Msg("new version available")
		}
	}

	// Determine platform and architecture
	platform := runtime.GOOS
	arch := runtime.GOARCH

	// Determine destination directory
	var destDir string
	if cmd.BinDir != "" {
		destDir = cmd.BinDir
	} else {
		// Use current binary location
		execPath, err := os.Executable()
		if err != nil {
			return jujuerrors.Annotate(err, "getting current executable path")
		}
		execPath, err = filepath.EvalSymlinks(execPath)
		if err != nil {
			return jujuerrors.Annotate(err, "resolving symlinks")
		}
		destDir = filepath.Dir(execPath)
	}

	// Download and install
	if err := downloadAndInstall(ctx, stdio, cmd.Force, cmd.BaseUrl, targetVersion, platform, arch, destDir); err != nil {
		return jujuerrors.Annotate(err, "downloading and installing")
	}

	log.G(ctx).Info().
		Str("version", targetVersion).
		Msg("upgrade complete")

	return nil
}

// fetchLatestVersion retrieves the latest version string from the channel file.
func fetchLatestVersion(ctx context.Context, baseURL, channel string) (string, error) {
	url := fmt.Sprintf("%s/endpoints/cli/content/%s.txt", baseURL, channel)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", jujuerrors.Annotate(err, "creating request")
	}

	resp, err := httpclient.DefaultHTTPClient.Do(req)
	if err != nil {
		return "", jujuerrors.Annotate(err, "fetching version file")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", jujuerrors.Errorf("failed to fetch version (HTTP %d)", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", jujuerrors.Annotate(err, "reading response body")
	}

	return strings.TrimSpace(string(body)), nil
}

// downloadAndInstall downloads the CLI binary and installs it.
func downloadAndInstall(ctx context.Context, stdio config.Stdio, force bool, baseURL, ver, plat, arch, destDir string) error {
	asset := fmt.Sprintf("unikraft-%s-%s.tar.gz", plat, arch)
	assetURL := fmt.Sprintf("%s/endpoints/cli/content/%s/%s", baseURL, ver, asset)
	checksumURL := assetURL + ".sha256"

	log.G(ctx).
		Debug().
		Str("url", assetURL).
		Msg("downloading binary")

	// Create temp directory for download
	tmpDir, err := os.MkdirTemp("", "unikraft-upgrade-*")
	if err != nil {
		return jujuerrors.Annotate(err, "creating temp directory")
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, asset)

	log.G(ctx).
		Info().
		Str("version", ver).
		Str("platform", plat).
		Str("arch", arch).
		Msg("downloading")

	// Download the archive with progress bar
	if err := downloadFile(ctx, stdio, assetURL, archivePath); err != nil {
		return jujuerrors.Annotate(err, "downloading archive")
	}

	// Check if context was cancelled during download
	if err := ctx.Err(); err != nil {
		return jujuerrors.Annotate(err, "download interrupted")
	}

	// Verify checksum
	log.G(ctx).
		Debug().
		Str("checksum_url", checksumURL).
		Msg("verifying checksum")

	if err := verifyChecksum(ctx, archivePath, checksumURL); err != nil {
		if errors.Is(err, ErrChecksumNotAvailable) {
			// Checksum file not available is a warning, not fatal (matches install.sh behavior).
			log.G(ctx).
				Warn().
				Msg("checksum file not available, skipping verification")
		} else if !force {
			return jujuerrors.Annotate(err, "checksum verification failed")
		} else {
			// Checksum verification failure (e.g. mismatch) with --force is a warning.
			log.G(ctx).
				Warn().
				Err(err).
				Msg("checksum verification failed, continuing anyway")
		}
	} else {
		log.G(ctx).
			Debug().
			Msg("checksum verified successfully")
	}

	// Extract and get binary path
	log.G(ctx).
		Debug().
		Str("archive", archivePath).
		Str("dest", tmpDir).
		Msg("extracting binary")

	binaryPath, err := extractBinary(archivePath, tmpDir)
	if err != nil {
		return jujuerrors.Annotate(err, "extracting binary")
	}

	log.G(ctx).
		Debug().
		Str("path", binaryPath).
		Msg("extracted binary")

	// Determine destination path
	binaryName := "unikraft"
	if runtime.GOOS == "windows" {
		binaryName = "unikraft.exe"
	}
	destPath := filepath.Join(destDir, binaryName)

	log.G(ctx).
		Debug().
		Str("bin", destPath).
		Msg("installing")

	// Install the new binary
	if err := installBinary(binaryPath, destPath); err != nil {
		return jujuerrors.Annotate(err, "installing binary")
	}

	return nil
}

// downloadFile downloads a file from a URL to the specified path with a progress bar.
func downloadFile(ctx context.Context, stdio config.Stdio, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return jujuerrors.Annotate(err, "creating request")
	}

	resp, err := httpclient.DefaultHTTPClient.Do(req)
	if err != nil {
		return jujuerrors.Annotate(err, "downloading file")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return jujuerrors.Errorf("download failed (HTTP %d)", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return jujuerrors.Annotate(err, "creating output file")
	}
	defer out.Close()

	// Use progress bar for download
	if err := progdl.DownloadWithProgress(ctx, stdio.Stdout, resp.Body, out, resp.ContentLength); err != nil {
		return err
	}

	return nil
}

// verifyChecksum downloads and verifies the SHA256 checksum of a file.
func verifyChecksum(ctx context.Context, filePath, checksumURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return jujuerrors.Annotate(err, "creating checksum request")
	}

	resp, err := httpclient.DefaultHTTPClient.Do(req)
	if err != nil {
		return jujuerrors.Annotate(err, "fetching checksum")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w (HTTP %d)", ErrChecksumNotAvailable, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return jujuerrors.Annotate(err, "reading checksum response")
	}

	// Parse checksum - first field if space-separated
	expectedStr := strings.TrimSpace(string(body))
	if idx := strings.Index(expectedStr, " "); idx != -1 {
		expectedStr = expectedStr[:idx]
	}

	// Calculate actual checksum
	f, err := os.Open(filePath)
	if err != nil {
		return jujuerrors.Annotate(err, "opening file for checksum")
	}
	defer f.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return jujuerrors.Annotate(err, "calculating checksum")
	}
	actual := hex.EncodeToString(hash.Sum(nil))

	if actual != expectedStr {
		return jujuerrors.Errorf("checksum mismatch: expected %s, got %s", expectedStr, actual)
	}

	return nil
}

// extractBinary extracts the unikraft binary from a tar.gz archive.
func extractBinary(archivePath, destDir string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", jujuerrors.Annotate(err, "opening archive")
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return "", jujuerrors.Annotate(err, "creating gzip reader")
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	binaryName := "unikraft"
	if runtime.GOOS == "windows" {
		binaryName = "unikraft.exe"
	}

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", jujuerrors.Annotate(err, "reading tar entry")
		}

		// Look for the unikraft binary
		if header.Typeflag == tar.TypeReg && filepath.Base(header.Name) == binaryName {
			destPath := filepath.Join(destDir, binaryName)
			outFile, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY, 0o755)
			if err != nil {
				return "", jujuerrors.Annotate(err, "creating output file")
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				outFile.Close()
				return "", jujuerrors.Annotate(err, "extracting binary")
			}
			outFile.Close()
			return destPath, nil
		}
	}

	return "", jujuerrors.New("binary not found in archive")
}

// installBinary copies the new binary to the destination, handling the case
// where the destination is the currently running executable.
func installBinary(srcPath, destPath string) error {
	// Read the new binary
	newBinary, err := os.ReadFile(srcPath)
	if err != nil {
		return jujuerrors.Annotate(err, "reading new binary")
	}

	// Get the original file info to preserve permissions, or use default mode
	// if the destination doesn't exist (e.g., fresh install to new directory)
	var mode os.FileMode = 0o755
	if info, err := os.Stat(destPath); err == nil {
		mode = info.Mode()
	} else if !os.IsNotExist(err) {
		return jujuerrors.Annotate(err, "getting destination info")
	}

	// On Unix-like systems, we can't write to a running executable directly.
	// Instead, we rename the old binary and write the new one.
	destDir := filepath.Dir(destPath)
	destBase := filepath.Base(destPath)

	// Create a backup path
	backupPath := filepath.Join(destDir, "."+destBase+".old")

	// Remove any existing backup
	_ = os.Remove(backupPath)

	// Rename current executable to backup (skip if destination doesn't exist)
	destExists := true
	if err := os.Rename(destPath, backupPath); err != nil {
		if !os.IsNotExist(err) {
			return jujuerrors.Annotate(err, "backing up current binary")
		}
		destExists = false
	}

	// Write new binary
	if err := os.WriteFile(destPath, newBinary, mode); err != nil {
		// Try to restore backup if we made one
		if destExists {
			_ = os.Rename(backupPath, destPath)
		}
		return jujuerrors.Annotate(err, "writing new binary")
	}

	// Remove backup if we made one
	if destExists {
		_ = os.Remove(backupPath)
	}

	return nil
}
