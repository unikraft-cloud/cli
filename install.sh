#!/usr/bin/env sh

set -eu

CHANNEL="${UNIKRAFT_CLI_INSTALL_CHANNEL:-stable}"
VERSION="${UNIKRAFT_CLI_INSTALL_VERSION:-}"
BIN_DIR="${UNIKRAFT_CLI_INSTALL_BIN_DIR:-}"

# Override URLs for testing
BASE_URL="${UNIKRAFT_CLI_INSTALL_URL:-https://pkg.unikraft.com}"

# Spinner characters
SPINNER_CHARS='|/-\'
SPINNER_PID=""
SPINNER_IDX=0

# Colors
CYAN='\033[36m'
GREEN='\033[32m'
RED='\033[31m'
YELLOW='\033[33m'
RESET='\033[0m'

# Clean up spinner on exit
cleanup() {
  stop_spinner
}
trap cleanup EXIT

start_spinner() {
  _spinner_msg="$1"
  # Only show spinner if stdout is a terminal
  if [ ! -t 1 ]; then
    return
  fi
  (
    _i=0
    _len=${#SPINNER_CHARS}
    while true; do
      _char=$(printf '%s' "$SPINNER_CHARS" | cut -c$((_i % _len + 1)))
      printf "\r${CYAN}%s${RESET} %s " "$_char" "$_spinner_msg"
      _i=$((_i + 1))
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
  local msg="$1"
  stop_spinner
  printf "${GREEN}✓${RESET} %s\n" "$msg"
}

step_fail() {
  local msg="$1"
  stop_spinner
  printf "${RED}✗${RESET} %s\n" "$msg" >&2
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
# Sets: HTTP_CODE variable, HTTP_ERROR_DETAIL for additional error info
http_download() {
  _url="$1"
  _output="$2"
  HTTP_ERROR_DETAIL=""

  if command -v curl >/dev/null 2>&1; then
    _stderr_file=$(mktemp)
    _exit_code=0
    HTTP_CODE=$(curl -sSL -w '%{http_code}' -o "$_output" "$_url" 2>"$_stderr_file") || _exit_code=$?
    if [ "$_exit_code" != "0" ]; then
      HTTP_CODE="000"
      # curl exit codes: 60 = SSL cert problem, 77 = SSL CA cert issue, 35 = SSL connect error
      if [ "$_exit_code" = "60" ] || [ "$_exit_code" = "77" ] || [ "$_exit_code" = "35" ]; then
        HTTP_ERROR_DETAIL="ssl"
      elif grep -qi 'ssl\|certificate\|tls' "$_stderr_file" 2>/dev/null; then
        HTTP_ERROR_DETAIL="ssl"
      fi
    fi
    rm -f "$_stderr_file"
  elif command -v wget >/dev/null 2>&1; then
    # wget doesn't easily return HTTP codes, so we parse --server-response
    _stderr_file=$(mktemp)
    if wget -q --server-response -O "$_output" "$_url" 2>"$_stderr_file"; then
      HTTP_CODE="200"
    else
      # Check for SSL/certificate errors in stderr
      if grep -qi 'ssl\|certificate\|tls' "$_stderr_file" 2>/dev/null; then
        HTTP_ERROR_DETAIL="ssl"
      fi
      # Try to extract HTTP code from server response
      HTTP_CODE=$(grep -o 'HTTP/[0-9.]* [0-9]*' "$_stderr_file" | tail -1 | awk '{print $2}')
      # If we couldn't parse it, use exit-code-based approximation
      if [ -z "$HTTP_CODE" ]; then
        HTTP_CODE="000"
      fi
    fi
    rm -f "$_stderr_file"
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
  local os arch
  os=$(uname -s)
  arch=$(uname -m)

  case "$arch" in
    x86_64|amd64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) err "Unsupported architecture: $arch" ;;
  esac

  case "$os" in
    Linux) PLATFORM="linux" EXT="tar.gz" BIN_NAME="unikraft" ;;
    Darwin) PLATFORM="darwin" EXT="tar.gz" BIN_NAME="unikraft" ;;
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

  _v=""
  _ch_file_url="${BASE_URL}/endpoints/cli/content/${CHANNEL}.txt"

  # Try to fetch the channel file, capturing both output and HTTP code
  _tmpfile=$(mktemp)
  http_download "$_ch_file_url" "$_tmpfile"

  if [ "$HTTP_CODE" = "200" ]; then
    _v=$(tr -d '\r\n' < "$_tmpfile")
  fi
  rm -f "$_tmpfile"

  if [ -z "$_v" ]; then
    step_fail "Fetching latest version"
    if [ "$HTTP_CODE" = "000" ]; then
      if [ "$HTTP_ERROR_DETAIL" = "ssl" ]; then
        err "SSL/TLS error: could not verify server certificate. You may need to install CA certificates (e.g., ca-certificates package)."
      else
        err "Network error: could not connect to $BASE_URL"
      fi
    elif [ "$HTTP_CODE" = "404" ]; then
      err "Version file not found at ${_ch_file_url} (HTTP 404)"
    else
      err "Failed to fetch version info (HTTP $HTTP_CODE)"
    fi
  fi

  step_done "Fetching latest version ($_v)"
  VERSION="$_v"
  PREFIX="endpoints/cli/content/${VERSION}/"
}

dir_in_path() {
  _check_dir="$1"
  # Normalize the directory path
  if [ -d "$_check_dir" ]; then
    _check_dir=$(cd "$_check_dir" 2>/dev/null && pwd) || return 1
  fi
  echo ":$PATH:" | grep -q ":$_check_dir:"
}

choose_bindir() {
  _default_bin_dir="${UNIKRAFT_CLI_INSTALL_DEFAULT_BIN_DIR:-$HOME/.local/bin}"

  if [ -n "$BIN_DIR" ]; then
    # User explicitly specified a directory - use it as-is
    DEST_DIR="$BIN_DIR"
  else
    # Check common home bin directories in preference order
    if [ -n "${UNIKRAFT_CLI_INSTALL_PREFERRED_DIRS:-}" ]; then
      _preferred_dirs="$UNIKRAFT_CLI_INSTALL_PREFERRED_DIRS"
    else
      _preferred_dirs="$HOME/.local/bin:$HOME/bin:$HOME/.bin"
    fi

    DEST_DIR=""
    _old_ifs="$IFS"
    IFS=':'
    for _dir in $_preferred_dirs; do
      IFS="$_old_ifs"
      if dir_in_path "$_dir"; then
        DEST_DIR="$_dir"
        break
      fi
    done
    IFS="$_old_ifs"

    # If none of the preferred dirs are in PATH, warn and use default
    if [ -z "$DEST_DIR" ]; then
      printf "${YELLOW}Warning:${RESET} None of the standard bin directories (~/.local/bin, ~/bin, ~/.bin) are in your PATH.\n"
      printf "         Using ${CYAN}%s${RESET} - you may need to add it to your PATH.\n" "$_default_bin_dir"
      DEST_DIR="$_default_bin_dir"
    fi
  fi

  _mkdir_err=""
  if ! _mkdir_err=$(mkdir -p "$DEST_DIR" 2>&1); then
    err "Could not create bin directory $DEST_DIR: $_mkdir_err"
  fi
  if [ ! -d "$DEST_DIR" ]; then
    err "Could not create bin directory: $DEST_DIR"
  fi
}

verify_checksum() {
  _archive_path="$1"
  _sha_url="$2"

  start_spinner "Verifying checksum"

  _sha_tmp=$(mktemp)
  http_download "$_sha_url" "$_sha_tmp"

  if [ "$HTTP_CODE" != "200" ]; then
    rm -f "$_sha_tmp" || true
    # Checksum file missing is a warning, not a fatal error
    stop_spinner
    printf "${YELLOW}⚠${RESET} Checksum file not available, skipping verification\n"
    return
  fi

  _raw=$(cat "$_sha_tmp")
  # If file includes filename, take first field; else treat as raw hash
  _expected=$(echo "$_raw" | awk '{print $1}')

  if command -v sha256sum >/dev/null 2>&1; then
    _actual=$(sha256sum "$_archive_path" | awk '{print $1}')
  else
    need_cmd shasum
    _actual=$(shasum -a 256 "$_archive_path" | awk '{print $1}')
  fi

  rm -f "$_sha_tmp" || true

  if [ "$_actual" != "$_expected" ]; then
    step_fail "Verifying checksum"
    err "Checksum mismatch: expected $_expected, got $_actual. The download may be corrupted."
  fi

  step_done "Verifying checksum"
}

extract_and_install() {
  _archive_path="$1"

  start_spinner "Installing to $DEST_DIR"

  _extract_err=""
  if ! _extract_err=$(tar -xzf "$_archive_path" "$BIN_NAME" 2>&1); then
    step_fail "Installing to $DEST_DIR"
    err "Failed to extract archive: $_extract_err"
  fi

  _install_err=""
  if ! _install_err=$(install -m 0755 "$BIN_NAME" "$DEST_DIR/$BIN_NAME" 2>&1); then
    step_fail "Installing to $DEST_DIR"
    err "Failed to install binary to $DEST_DIR: $_install_err"
  fi

  step_done "Installed to $DEST_DIR"
}

post_install_note() {
  if ! echo ":$PATH:" | grep -q ":$DEST_DIR:"; then
    echo "  Add to PATH: export PATH=\"$DEST_DIR:\$PATH\""
  fi
}

configure_auth() {
  printf "\nRun ${CYAN}unikraft login${RESET} to get started.\n"
}

main() {
  parse_args "$@"
  detect_platform
  resolve_prefix
  choose_bindir

  _asset="unikraft-${PLATFORM}-${ARCH}.${EXT}"
  _url="${BASE_URL}/${PREFIX}${_asset}"
  _sha_url="${_url}.sha256"

  need_http_cmd
  need_cmd tar

  _tmpdir=$(mktemp -d)
  _archive_path="$_tmpdir/$_asset"

  start_spinner "Downloading Unikraft CLI"

  http_download "$_url" "$_archive_path"

  if [ "$HTTP_CODE" != "200" ]; then
    step_fail "Downloading Unikraft CLI"
    if [ "$HTTP_CODE" = "000" ]; then
      if [ "$HTTP_ERROR_DETAIL" = "ssl" ]; then
        err "SSL/TLS error: could not verify server certificate. You may need to install CA certificates (e.g., ca-certificates package)."
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

  verify_checksum "$_archive_path" "$_sha_url"

  (cd "$_tmpdir" && extract_and_install "$_archive_path")

  rm -rf "$_tmpdir" || true
  post_install_note
  configure_auth
}

main "$@"
