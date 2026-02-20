#!/usr/bin/env bash
set -euo pipefail

REPO="${MINICLAWD_REPO:-yifanes/miniclawd}"
BIN_NAME="miniclawd"
API_URL="https://api.github.com/repos/${REPO}/releases/latest"
INSTALL_METHOD="${MINICLAWD_INSTALL_METHOD:-auto}" # auto|release|homebrew|go

log() {
  printf '%s\n' "$*"
}

err() {
  printf 'Error: %s\n' "$*" >&2
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1
}

detect_os() {
  case "$(uname -s)" in
    Darwin) echo "darwin" ;;
    Linux) echo "linux" ;;
    *)
      err "Unsupported OS: $(uname -s)"
      exit 1
      ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *)
      err "Unsupported architecture: $(uname -m)"
      exit 1
      ;;
  esac
}

detect_install_dir() {
  if [ -n "${MINICLAWD_INSTALL_DIR:-}" ]; then
    echo "$MINICLAWD_INSTALL_DIR"
    return
  fi
  if [ -w "/usr/local/bin" ]; then
    echo "/usr/local/bin"
    return
  fi
  if [ -d "$HOME/.local/bin" ] || mkdir -p "$HOME/.local/bin" 2>/dev/null; then
    echo "$HOME/.local/bin"
    return
  fi
  echo "/usr/local/bin"
}

download_release_json() {
  if need_cmd curl; then
    curl -fsSL "$API_URL"
  elif need_cmd wget; then
    wget -qO- "$API_URL"
  else
    err "Neither curl nor wget is available"
    exit 1
  fi
}

extract_asset_url() {
  # GoReleaser archive naming: miniclawd_VERSION_OS_ARCH.tar.gz
  local release_json="$1"
  local os="$2"
  local arch="$3"

  printf '%s\n' "$release_json" \
    | grep -Eo 'https://[^"]+' \
    | grep '/releases/download/' \
    | grep -E "/${BIN_NAME}_[0-9]+\.[0-9]+\.[0-9]+_${os}_${arch}\.(tar\.gz|zip)$" \
    | head -n1
}

download_file() {
  local url="$1"
  local output="$2"
  if need_cmd curl; then
    curl -fL "$url" -o "$output"
  else
    wget -O "$output" "$url"
  fi
}

install_from_archive() {
  local archive="$1"
  local install_dir="$2"
  local tmpdir="$3"
  local extracted=0

  case "$archive" in
    *.tar.gz|*.tgz)
      tar -xzf "$archive" -C "$tmpdir"
      extracted=1
      ;;
    *.zip)
      if ! need_cmd unzip; then
        err "unzip is required to extract zip archives"
        return 1
      fi
      unzip -q "$archive" -d "$tmpdir"
      extracted=1
      ;;
  esac

  if [ "$extracted" -eq 0 ]; then
    if tar -tzf "$archive" >/dev/null 2>&1; then
      tar -xzf "$archive" -C "$tmpdir"
      extracted=1
    elif need_cmd unzip && unzip -tq "$archive" >/dev/null 2>&1; then
      unzip -q "$archive" -d "$tmpdir"
      extracted=1
    fi
  fi

  if [ "$extracted" -eq 0 ]; then
    err "Unknown archive format: $archive"
    return 1
  fi

  local bin_path
  bin_path="$(find "$tmpdir" -type f -name "$BIN_NAME" | head -n1)"
  if [ -z "$bin_path" ]; then
    err "Could not find '$BIN_NAME' in archive"
    return 1
  fi

  chmod +x "$bin_path"
  if [ -w "$install_dir" ]; then
    cp "$bin_path" "$install_dir/$BIN_NAME"
  else
    if need_cmd sudo; then
      log "Need elevated permissions to install to $install_dir"
      sudo cp "$bin_path" "$install_dir/$BIN_NAME"
    else
      err "No write permission for $install_dir and sudo not available"
      return 1
    fi
  fi
}

fallback_install() {
  local os="$1"
  if [ "$os" = "darwin" ] && need_cmd brew; then
    log "Falling back to Homebrew..."
    brew tap yifanes/tap
    brew install miniclawd
    return 0
  fi

  if need_cmd go; then
    log "Falling back to go install (may take a while)..."
    go install "github.com/${REPO}@latest"
    return 0
  fi

  err "No compatible release asset found and no fallback installer available."
  err "Install Go or Homebrew, or build from source: https://github.com/${REPO}"
  return 1
}

main() {
  local os arch install_dir release_json asset_url tmpdir archive asset_filename

  os="$(detect_os)"
  arch="$(detect_arch)"
  install_dir="$(detect_install_dir)"

  log "Installing ${BIN_NAME} for ${os}/${arch}..."

  case "$INSTALL_METHOD" in
    auto|release)
      release_json="$(download_release_json)"
      asset_url="$(extract_asset_url "$release_json" "$os" "$arch" || true)"
      if [ -z "$asset_url" ]; then
        log "No prebuilt binary found for ${os}/${arch} in latest release."
        if [ "$INSTALL_METHOD" = "release" ]; then
          err "MINICLAWD_INSTALL_METHOD=release set, so no fallback installer will be used."
          exit 1
        fi
        fallback_install "$os"
      else
        tmpdir="$(mktemp -d)"
        trap 'if [ -n "${tmpdir:-}" ]; then rm -rf "$tmpdir"; fi' EXIT
        asset_filename="${asset_url##*/}"
        asset_filename="${asset_filename%%\?*}"
        if [ -z "$asset_filename" ] || [ "$asset_filename" = "$asset_url" ]; then
          asset_filename="${BIN_NAME}.archive"
        fi
        archive="$tmpdir/$asset_filename"
        log "Downloading: $asset_url"
        download_file "$asset_url" "$archive"
        install_from_archive "$archive" "$install_dir" "$tmpdir"
      fi
      ;;
    homebrew)
      if ! need_cmd brew; then
        err "Homebrew not found. Install it from https://brew.sh"
        exit 1
      fi
      brew tap yifanes/tap
      brew install miniclawd
      ;;
    go)
      if ! need_cmd go; then
        err "Go not found. Install it from https://go.dev/dl"
        exit 1
      fi
      go install "github.com/${REPO}@latest"
      ;;
    *)
      err "Unknown MINICLAWD_INSTALL_METHOD: $INSTALL_METHOD (expected auto|release|homebrew|go)"
      exit 1
      ;;
  esac

  log ""
  log "Installed ${BIN_NAME} successfully!"
  if [ "$install_dir" = "$HOME/.local/bin" ]; then
    log "Make sure '\$HOME/.local/bin' is in PATH."
    log "Example: export PATH=\"\$HOME/.local/bin:\$PATH\""
  fi
  log "Run: ${BIN_NAME} help"
}

main "$@"
