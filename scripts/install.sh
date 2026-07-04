#!/usr/bin/env bash
# BackAI af-stack CLI installer.
#
# Downloads the latest (or a pinned) release binary from GitHub, verifies it
# against the published checksums, and installs it onto your PATH.
#
#   curl -fsSL https://raw.githubusercontent.com/Agent-Field/backai/main/scripts/install.sh | bash
#
# Env overrides:
#   AF_STACK_VERSION   pin a version, e.g. v0.6.0 (default: latest release)
#   AF_STACK_INSTALL_DIR   install target (default: /usr/local/bin, else ~/.local/bin)
#   AF_STACK_REPO      owner/name (default: Agent-Field/backai)
set -euo pipefail

REPO="${AF_STACK_REPO:-Agent-Field/backai}"
BINARY="af-stack"

info()  { printf '\033[0;34m==>\033[0m %s\n' "$1" >&2; }
warn()  { printf '\033[0;33mwarning:\033[0m %s\n' "$1" >&2; }
die()   { printf '\033[0;31merror:\033[0m %s\n' "$1" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "required tool '$1' not found on PATH"; }
need curl
need tar

# --- detect platform -------------------------------------------------------
os="$(uname -s)"
case "$os" in
  Linux)  os="linux" ;;
  Darwin) os="darwin" ;;
  *) die "unsupported OS '$os' (this script handles Linux and macOS; on Windows use the .zip from the Releases page)" ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64)  arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) die "unsupported architecture '$arch'" ;;
esac

# --- resolve version -------------------------------------------------------
version="${AF_STACK_VERSION:-}"
if [ -z "$version" ]; then
  info "Resolving latest release of $REPO..."
  version="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')" \
    || die "could not query the GitHub API for the latest release"
  [ -n "$version" ] || die "could not determine the latest release tag (is $REPO public and released?)"
fi
# checksums/archives use the version WITHOUT the leading 'v'.
ver_noprefix="${version#v}"

archive="${BINARY}_${ver_noprefix}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$version"

# --- download + verify -----------------------------------------------------
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
info "Downloading $archive ($version)..."
curl -fsSL "$base/$archive" -o "$tmp/$archive" \
  || die "download failed. If $REPO is private, this 404s until it's made public. Otherwise check that $archive exists on the release."

if curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" 2>/dev/null; then
  info "Verifying checksum..."
  ( cd "$tmp"
    if command -v sha256sum >/dev/null 2>&1; then
      grep " ${archive}\$" checksums.txt | sha256sum -c - >/dev/null \
        || die "checksum verification failed for $archive"
    elif command -v shasum >/dev/null 2>&1; then
      grep " ${archive}\$" checksums.txt | shasum -a 256 -c - >/dev/null \
        || die "checksum verification failed for $archive"
    else
      warn "no sha256sum/shasum available — skipping checksum verification"
    fi )
else
  warn "checksums.txt not found on the release — skipping verification"
fi

tar -xzf "$tmp/$archive" -C "$tmp"
[ -f "$tmp/$BINARY" ] || die "archive did not contain the '$BINARY' binary"
chmod +x "$tmp/$BINARY"

# --- install ---------------------------------------------------------------
dir="${AF_STACK_INSTALL_DIR:-}"
if [ -z "$dir" ]; then
  if [ -w /usr/local/bin ] 2>/dev/null; then dir="/usr/local/bin"; else dir="$HOME/.local/bin"; fi
fi
mkdir -p "$dir"

if mv "$tmp/$BINARY" "$dir/$BINARY" 2>/dev/null; then :;
elif command -v sudo >/dev/null 2>&1 && sudo mv "$tmp/$BINARY" "$dir/$BINARY"; then :;
else die "could not install to $dir (set AF_STACK_INSTALL_DIR to a writable dir)"; fi

info "Installed $BINARY $version to $dir/$BINARY"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) warn "$dir is not on your PATH — add it, e.g.: export PATH=\"$dir:\$PATH\"" ;;
esac
"$dir/$BINARY" version 2>/dev/null || true
