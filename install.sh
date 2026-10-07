#!/usr/bin/env sh
set -eu

CHANNEL="${UNIKRAFT_CLI_INSTALL_CHANNEL:-stable}"
VERSION="${UNIKRAFT_CLI_INSTALL_VERSION:-}"
BIN_DIR="${UNIKRAFT_CLI_INSTALL_BIN_DIR:-}"

# Override URLs for testing
BASE_URL="${UNIKRAFT_CLI_INSTALL_URL:-https://pkg.unikraft.com}"

# Spinner characters
SPINNER_CHARS="|/-\\"
SPINNER_PID=""
SPINNER_MSG=""

# Colors
CYAN=$(printf '\033[36m')
GREEN=$(printf '\033[32m')
RED=$(printf '\033[31m')
YELLOW=$(printf '\033[33m')
RESET=$(printf '\033[0m')

# Clean up spinner on exit
cleanup() {
  stop_spinner
}
trap cleanup EXIT

start_spinner() {
  SPINNER_MSG="$1"
  # Only show spinner if stdout is a terminal
  if [ ! -t 1 ]; then
    return
  fi
  (
    i=0
    len=${#SPINNER_CHARS}
    while :; do
      idx=$((i % len + 1))
      ch=$(printf "%s" "$SPINNER_CHARS" | cut -c "$idx")
      printf "\r%s%s%s %s " "$CYAN" "$ch" "$RESET" "$SPINNER_MSG"
      i=$((i + 1))
      sleep 0.1
    done
  ) &
  SPINNER_PID=$!
}

stop_spinner() {
  if [ -n "${SPINNER_PID:-}" ]; then
    kill "$SPINNER_PID" 2>/dev/null || true
    wait "$SPINNER_PID" 2>/dev/null || true
    SPINNER_PID=""
    # Clear the spinner line
    if [ -t 1 ]; then
      printf "\r\033[K"
    fi
  fi
}

step_done() {
  msg="$1"
  stop_spinner
  printf "%s✓%s %s\n" "$GREEN" "$RESET" "$msg"
}

step_fail() {
  msg="$1"
  stop_spinner
  printf "%s✗%s %s\n" "$RED" "$RESET" "$msg" >&2
}

usage() {
  echo "Install the Unikraft CLI"
  echo
  echo "Usage: $0 [--channel <stable|staging>] [--version vX.Y.Z] [--bin-dir <dir>]"
  echo
  echo "Options:"
  echo "  --channel    Release channel (default: stable). Ignored if --version is set."
  echo "  --version    Install a specific version tag (e.g., v1.2.3)."
  echo "  --bin-dir    Directory to install the unikraft CLI binary into (default: ~/.local/bin)."
  echo "  -h, --help   Show this help message."
  echo
  echo "Influential environment variables:"
  echo "  UNIKRAFT_CLI_INSTALL_URL       Override base download URL"
  echo "  UNIKRAFT_CLI_INSTALL_CHANNEL   Override default channel"
  echo "  UNIKRAFT_CLI_INSTALL_VERSION   Override version"
  echo "  UNIKRAFT_CLI_INSTALL_BIN_DIR   Override install directory"
}

err() { echo "Error: $*" >&2; exit 1; }

need_cmd() { command -v "$1" >/dev/null 2>&1 || err "Required command not found: $1"; }

# HTTP download abstraction - uses curl if available, falls back to wget
# Usage: http_download <url> <output_file>
# Returns: HTTP status code (or approximation for wget)
# Sets: HTTP_CODE variable, SSL_ERROR variable ("1" if SSL error, "" otherwise)
http_download() {
  url="$1"
  output="$2"
  SSL_ERROR=""

  if command -v curl >/dev/null 2>&1; then
    curl_exit=0
    HTTP_CODE=$(curl -sSL -w '%{http_code}' -o "$output" "$url" 2>/dev/null) || curl_exit=$?
    if [ "$curl_exit" -ne 0 ]; then
      # curl SSL-related exit codes:
      # 35: SSL connect error
      # 51: Peer's SSL certificate was not OK
      # 60: Peer certificate cannot be authenticated with known CA certificates
      # 77: Problem reading SSL CA cert
      case "$curl_exit" in
        35|51|60|77) SSL_ERROR="1" ;;
      esac
      HTTP_CODE="000"
    fi
  elif command -v wget >/dev/null 2>&1; then
    # wget doesn't easily return HTTP codes, so we parse server response
    stderr_file=$(mktemp)
    if wget -q -S -O "$output" "$url" 2>"$stderr_file"; then
      wget_exit=0
    else
      wget_exit=$?
    fi

    # wget exit code 5 indicates SSL verification failure
    if [ "$wget_exit" -eq 5 ]; then
      SSL_ERROR="1"
    fi

    # Try to extract HTTP code from server response
    HTTP_CODE=$(awk '{for (i=1; i<=NF; i++) if ($i ~ /^[0-9][0-9][0-9]$/) code=$i} END{print code}' "$stderr_file")
    # If we couldn't parse it, use exit-code-based approximation
    if [ -z "$HTTP_CODE" ]; then
      if [ "$wget_exit" -eq 0 ]; then
        HTTP_CODE="200"
      else
        HTTP_CODE="000"
      fi
    fi
    rm -f "$stderr_file"
  else
    err "Neither curl nor wget found. Please install one of them."
  fi
}

need_http_cmd() {
  if ! command -v curl >/dev/null 2>&1 && ! command -v wget >/dev/null 2>&1; then
    err "Neither curl nor wget found. Please install one of them."
  fi
}

parse_args() {
  while [ $# -gt 0 ]; do
    case "$1" in
      --channel)
        [ -z "${2:-}" ] && err "Option --channel requires a value"
        CHANNEL="$2"; shift 2 ;;
      --version)
        [ -z "${2:-}" ] && err "Option --version requires a value"
        VERSION="$2"; shift 2 ;;
      --bin-dir)
        [ -z "${2:-}" ] && err "Option --bin-dir requires a value"
        BIN_DIR="$2"; shift 2 ;;
      -h|--help)
        usage; exit 0 ;;
      *)
        err "Unknown argument: $1" ;;
    esac
  done
}

detect_platform() {
  os=$(uname -s)
  arch=$(uname -m)

  # On NetBSD, the machine name is the board (for example evbarm). Use the CPU type.
  if [ "$os" = "NetBSD" ]; then
    arch=$(uname -p)
  fi

  case "$arch" in
    x86_64|amd64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) err "Unsupported architecture: $arch" ;;
  esac

  case "$os" in
    Linux) PLATFORM="linux" EXT="tar.gz" BIN_NAME="unikraft" ;;
    Darwin) PLATFORM="darwin" EXT="tar.gz" BIN_NAME="unikraft" ;;
    FreeBSD) PLATFORM="freebsd" EXT="tar.gz" BIN_NAME="unikraft" ;;
    OpenBSD) PLATFORM="openbsd" EXT="tar.gz" BIN_NAME="unikraft" ;;
    NetBSD) PLATFORM="netbsd" EXT="tar.gz" BIN_NAME="unikraft" ;;
    *) err "Unsupported OS: $os (use the PowerShell installer on Windows)" ;;
  esac

  ARCH="$arch"
}

resolve_prefix() {
  # Determine S3 prefix where artifacts are stored
  if [ -n "$VERSION" ]; then
    PREFIX="endpoints/cli/content/${VERSION}/"
    return
  fi

  need_http_cmd

  start_spinner "Fetching latest version"

  v=""
  ch_file_url="${BASE_URL}/endpoints/cli/content/${CHANNEL}.txt"

  # Try to fetch the channel file, capturing both output and HTTP code
  tmpfile=$(mktemp)
  http_download "$ch_file_url" "$tmpfile"

  if [ "$HTTP_CODE" = "200" ]; then
    v=$(tr -d '\r\n' < "$tmpfile")
  fi
  rm -f "$tmpfile"

  if [ -z "$v" ]; then
    step_fail "Fetching latest version"
    if [ "$HTTP_CODE" = "000" ]; then
      if [ -n "$SSL_ERROR" ]; then
        err "SSL certificate verification failed connecting to $BASE_URL. Ensure CA certificates are installed (e.g., ca-certificates package)."
      else
        err "Network error: could not connect to $BASE_URL"
      fi
    elif [ "$HTTP_CODE" = "404" ]; then
      err "Version file not found at ${ch_file_url} (HTTP 404)"
    else
      err "Failed to fetch version info (HTTP $HTTP_CODE)"
    fi
  fi

  step_done "Fetching latest version ($v)"
  VERSION="$v"
  PREFIX="endpoints/cli/content/${VERSION}/"
}

dir_in_path() {
  check_dir="$1"
  # Normalize the directory path
  if [ -d "$check_dir" ]; then
    check_dir=$(cd "$check_dir" 2>/dev/null && pwd) || return 1
  fi
  case ":$PATH:" in
    *":$check_dir:") return 0 ;;
    *) return 1 ;;
  esac
}

choose_bindir() {
  default_bin_dir="${UNIKRAFT_CLI_INSTALL_DEFAULT_BIN_DIR:-$HOME/.local/bin}"

  if [ -n "$BIN_DIR" ]; then
    # User explicitly specified a directory - use it as-is
    DEST_DIR="$BIN_DIR"
  else
    # Check common home bin directories in preference order
    if [ -n "${UNIKRAFT_CLI_INSTALL_PREFERRED_DIRS:-}" ]; then
      preferred_dirs="$UNIKRAFT_CLI_INSTALL_PREFERRED_DIRS"
    else
      preferred_dirs="$HOME/.local/bin:$HOME/bin:$HOME/.bin"
    fi

    DEST_DIR=""
    old_ifs=$IFS
    IFS=:
    for dir in $preferred_dirs; do
      if dir_in_path "$dir"; then
        DEST_DIR="$dir"
        break
      fi
    done
    IFS=$old_ifs

    # If none of the preferred dirs are in PATH, warn and use default
    if [ -z "$DEST_DIR" ]; then
      printf "%sWarning:%s None of the standard bin directories (~/.local/bin, ~/bin, ~/.bin) are in your PATH.\n" "$YELLOW" "$RESET"
      printf "         Using %s%s%s - you may need to add it to your PATH.\n" "$CYAN" "$default_bin_dir" "$RESET"
      DEST_DIR="$default_bin_dir"
    fi
  fi

  mkdir_err=$(mkdir -p "$DEST_DIR" 2>&1) || err "Could not create bin directory $DEST_DIR: $mkdir_err"
  if [ ! -d "$DEST_DIR" ]; then
    err "Could not create bin directory: $DEST_DIR"
  fi
}

verify_checksum() {
  archive_path="$1"
  sha_url="$2"

  start_spinner "Verifying checksum"

  sha_tmp=$(mktemp)
  http_download "$sha_url" "$sha_tmp"

  if [ "$HTTP_CODE" != "200" ]; then
    rm -f "$sha_tmp" || true
    # Checksum file missing is a warning, not a fatal error
    stop_spinner
    printf "%s⚠%s Checksum file not available, skipping verification\n" "$YELLOW" "$RESET"
    return
  fi

  expected=$(awk '{print $1; exit}' "$sha_tmp")

  if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$archive_path") || err "Failed to calculate sha256"
    actual=${actual%% *}
  elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "$archive_path") || err "Failed to calculate sha256"
    actual=${actual%% *}
  else
    need_cmd openssl
    actual=$(openssl dgst -sha256 "$archive_path") || err "Failed to calculate sha256"
    actual=${actual##* }
  fi

  rm -f "$sha_tmp" || true

  if [ "$actual" != "$expected" ]; then
    step_fail "Verifying checksum"
    err "Checksum mismatch: expected $expected, got $actual. The download may be corrupted."
  fi

  step_done "Verifying checksum"
}

extract_and_install() {
  archive_path="$1"

  start_spinner "Installing to $DEST_DIR"

  extract_err=$(tar -xzf "$archive_path" "$BIN_NAME" 2>&1) || {
    step_fail "Installing to $DEST_DIR"
    err "Failed to extract archive: $extract_err"
  }

  install_err=$(install -m 0755 "$BIN_NAME" "$DEST_DIR/$BIN_NAME" 2>&1) || {
    step_fail "Installing to $DEST_DIR"
    err "Failed to install binary to $DEST_DIR: $install_err"
  }

  step_done "Installed to $DEST_DIR"
}

post_install_note() {
  case ":$PATH:" in
    *":$DEST_DIR:") return 0 ;;
  esac
  if [ -n "$DEST_DIR" ]; then
    echo "  Add to PATH: export PATH=\"$DEST_DIR:\$PATH\""
  fi
}

configure_auth() {
  printf "\nRun %sunikraft login%s to get started.\n" "$CYAN" "$RESET"
}

main() {
  parse_args "$@"
  detect_platform
  resolve_prefix
  choose_bindir

  asset="unikraft-${PLATFORM}-${ARCH}.${EXT}"
  url="${BASE_URL}/${PREFIX}${asset}"
  sha_url="${url}.sha256"

  need_http_cmd
  need_cmd tar

  tmpdir=$(mktemp -d)
  archive_path="$tmpdir/$asset"

  start_spinner "Downloading Unikraft CLI"

  http_download "$url" "$archive_path"

  if [ "$HTTP_CODE" != "200" ]; then
    step_fail "Downloading Unikraft CLI"
    if [ "$HTTP_CODE" = "000" ]; then
      if [ -n "$SSL_ERROR" ]; then
        err "SSL certificate verification failed. Ensure CA certificates are installed (e.g., ca-certificates package)."
      else
        err "Network error: could not connect to download server"
      fi
    elif [ "$HTTP_CODE" = "404" ]; then
      err "Binary not found (HTTP 404). Version $VERSION may not exist for ${PLATFORM}/${ARCH}."
    else
      err "Download failed (HTTP $HTTP_CODE)"
    fi
  fi

  step_done "Downloading Unikraft CLI"

  verify_checksum "$archive_path" "$sha_url"

  (cd "$tmpdir" && extract_and_install "$archive_path")

  rm -rf "$tmpdir" || true
  post_install_note
  configure_auth
}

main "$@"
