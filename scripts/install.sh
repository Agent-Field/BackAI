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
# Two rules here, both learned the hard way:
#
#  1. Never pipe curl straight into an early-exiting reader (grep -m1, head).
#     Under `set -o pipefail` the reader closes the pipe as soon as it has its
#     match, curl fails its remaining write (exit 23, "Failed writing body"),
#     and the whole resolution "fails" even though the tag parsed fine. Buffer
#     the response into a variable first, then parse.
#
#  2. Prefer the github.com redirect over the REST API. The unauthenticated
#     API allows 60 requests/hour per IP, which shared CI runners and office
#     NATs exhaust routinely; /releases/latest -> 302 -> /releases/tag/<tag>
#     has no such limit. The API is only a fallback.
latest_tag_from_redirect() {
  local final tag
  final="$(curl -fsSIL -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" 2>/dev/null)" || return 1
  tag="${final##*/releases/tag/}"
  # No redirect to a tag (no releases yet, or an unexpected page) — signal
  # the caller to fall back rather than returning junk.
  [ -n "$tag" ] && [ "$tag" != "$final" ] || return 1
  printf '%s\n' "$tag"
}

latest_tag_from_api() {
  local body tag
  body="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest")" || return 1
  # sed reads the whole buffer (no early exit), so no broken-pipe hazard.
  tag="$(printf '%s\n' "$body" | sed -nE 's/.*"tag_name": *"([^"]+)".*/\1/p')"
  tag="${tag%%$'\n'*}"
  [ -n "$tag" ] || return 1
  printf '%s\n' "$tag"
}

version="${AF_STACK_VERSION:-}"
if [ -z "$version" ]; then
  info "Resolving latest release of $REPO..."
  version="$(latest_tag_from_redirect)" || version="$(latest_tag_from_api)" \
    || die "could not determine the latest release of $REPO (is it public and released? pin one with AF_STACK_VERSION=vX.Y.Z)"
fi
case "$version" in
  v[0-9]*|[0-9]*) ;;
  *) die "unexpected release tag '$version' (expected something like v0.12.4)" ;;
esac
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

verify_checksum() {
  # Runs in $tmp. Returns non-zero on any mismatch or missing entry; the
  # caller turns that into a hard error (a `die` inside a subshell would
  # only exit the subshell).
  local expected actual
  expected="$(grep -E "[[:space:]]\*?${archive}\$" checksums.txt | awk '{print $1}')"
  expected="${expected%%$'\n'*}"
  [ -n "$expected" ] || { warn "no entry for $archive in checksums.txt"; return 1; }
  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$archive" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
  else
    warn "no sha256sum/shasum available — skipping checksum verification"
    return 0
  fi
  [ "$actual" = "$expected" ]
}

if curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" 2>/dev/null; then
  info "Verifying checksum..."
  (cd "$tmp" && verify_checksum) || die "checksum verification failed for $archive"
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
