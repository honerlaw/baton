#!/usr/bin/env bash
# Install the latest (or a specific) baton release.
#
# Usage:
#   curl -sSfL https://raw.githubusercontent.com/honerlaw/baton/main/install.sh | bash
#
# Environment variables:
#   VERSION      Tag to install (default: latest). Example: VERSION=v1.0.0
#   INSTALL_DIR  Target directory for the binary. Defaults to /usr/local/bin
#                when writable, otherwise $HOME/.local/bin.
#   REPO         GitHub repo in "owner/name" form. Default: honerlaw/baton.
#
# Requires: curl (or wget), tar, uname, sha256sum (or shasum).

set -euo pipefail

REPO="${REPO:-honerlaw/baton}"
VERSION="${VERSION:-latest}"

log()  { printf '\033[0;34m==>\033[0m %s\n' "$*" >&2; }
warn() { printf '\033[0;33m==>\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[0;31mxx>\033[0m %s\n' "$*" >&2; exit 1; }

have() { command -v "$1" >/dev/null 2>&1; }

detect_os() {
  local os
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    linux)   echo linux ;;
    darwin)  echo darwin ;;
    *)       die "unsupported OS: $os (this installer only handles linux and macOS; Windows users: download the .zip from the releases page)" ;;
  esac
}

detect_arch() {
  local arch
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64)  echo amd64 ;;
    arm64|aarch64) echo arm64 ;;
    *)             die "unsupported architecture: $arch" ;;
  esac
}

fetch() {
  local url="$1" out="$2"
  if have curl; then
    curl -fsSL -o "$out" "$url"
  elif have wget; then
    wget -qO "$out" "$url"
  else
    die "curl or wget is required"
  fi
}

fetch_stdout() {
  local url="$1"
  if have curl; then
    curl -fsSL "$url"
  elif have wget; then
    wget -qO- "$url"
  else
    die "curl or wget is required"
  fi
}

resolve_version() {
  if [ "$VERSION" = "latest" ]; then
    local tag
    tag="$(fetch_stdout "https://api.github.com/repos/${REPO}/releases/latest" \
      | grep -o '"tag_name": *"[^"]*"' | head -n1 | cut -d'"' -f4)"
    [ -n "$tag" ] || die "could not determine latest release for ${REPO}"
    echo "$tag"
  else
    echo "$VERSION"
  fi
}

sha256() {
  if have sha256sum; then
    sha256sum "$1" | awk '{print $1}'
  elif have shasum; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    die "sha256sum or shasum is required"
  fi
}

pick_install_dir() {
  if [ -n "${INSTALL_DIR:-}" ]; then
    echo "$INSTALL_DIR"
    return
  fi
  if [ -w /usr/local/bin ] || { [ "$(id -u)" = "0" ] && [ -d /usr/local/bin ]; }; then
    echo /usr/local/bin
  else
    echo "$HOME/.local/bin"
  fi
}

main() {
  local os arch version base tarball tarball_path checksums_path
  local install_dir tmpdir

  os="$(detect_os)"
  arch="$(detect_arch)"
  version="$(resolve_version)"
  install_dir="$(pick_install_dir)"

  tarball="baton_${version}_${os}_${arch}.tar.gz"
  base="https://github.com/${REPO}/releases/download/${version}"

  log "installing baton ${version} (${os}/${arch}) → ${install_dir}"

  tmpdir="$(mktemp -d)"
  trap 'rm -rf "$tmpdir"' EXIT

  tarball_path="${tmpdir}/${tarball}"
  checksums_path="${tmpdir}/checksums.txt"

  log "downloading ${tarball}"
  fetch "${base}/${tarball}" "$tarball_path"

  log "verifying checksum"
  fetch "${base}/checksums.txt" "$checksums_path"
  local got want
  got="$(sha256 "$tarball_path")"
  want="$(awk -v name="$tarball" '$2 == name { print $1 }' "$checksums_path")"
  [ -n "$want" ] || die "checksum for $tarball not found in checksums.txt"
  [ "$got" = "$want" ] || die "checksum mismatch: expected $want, got $got"

  log "extracting"
  tar -xzf "$tarball_path" -C "$tmpdir"

  local extracted="${tmpdir}/baton_${version}_${os}_${arch}/baton"
  [ -f "$extracted" ] || die "binary not found after extract: $extracted"
  chmod +x "$extracted"

  mkdir -p "$install_dir"
  mv "$extracted" "${install_dir}/baton"

  log "installed: ${install_dir}/baton"
  if ! echo ":$PATH:" | grep -q ":${install_dir}:"; then
    warn "${install_dir} is not on your PATH; add it with:"
    warn "  echo 'export PATH=\"${install_dir}:\$PATH\"' >> ~/.bashrc   # or ~/.zshrc"
  fi

  "${install_dir}/baton" version || true
}

main "$@"
