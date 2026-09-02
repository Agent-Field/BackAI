#!/usr/bin/env bash
# BackAI af-stack CLI installer.
#
# Downloads the latest (or a pinned) release binary from GitHub, verifies it
# against the published checksums, and installs it onto your PATH.
#
#   curl -fsSL https://raw.githubusercontent.com/Agent-Field/backai/main/scripts/install.sh | bash
#
# Env overrides:
#   AF_STACK_VERSION        pin a release, e.g. v0.12.4 or 0.12.4 (default: latest release)
#   AF_STACK_INSTALL_DIR    install target (default: /usr/local/bin if writable, else ~/.local/bin)
#   AF_STACK_REPO           owner/name (default: Agent-Field/backai)
#   AF_STACK_DOWNLOAD_BASE  fetch the archive and checksums.txt from this URL instead of
#                           GitHub Releases (air-gapped mirrors, tests)
#   AF_STACK_SKIP_CHECKSUM  set to 1 to install even when checksums.txt cannot be fetched
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

# --- pick the install dir --------------------------------------------------
# Done before any download so a bad target fails in under a second, not
# after fetching and verifying the archive.
on_path() { case ":$PATH:" in *":$1:"*) return 0 ;; esac; return 1; }

# usable_dir: the directory is writable, or does not exist yet and its
# nearest existing ancestor is writable (so mkdir -p will succeed).
usable_dir() {
  local d="$1"
  while [ ! -d "$d" ]; do
    case "$d" in
      */*) d="${d%/*}"; [ -n "$d" ] || d="/" ;;
      *)   d="." ;;
    esac
  done
  [ -w "$d" ]
}

dir="${AF_STACK_INSTALL_DIR:-}"
# Trim trailing slashes (tab completion adds them) so the PATH comparison
# below matches and the printed path is clean.
while [ "$dir" != "/" ] && [ "${dir%/}" != "$dir" ]; do dir="${dir%/}"; done
if [ -z "$dir" ]; then
  # Prefer a usable candidate that is already on PATH, so `af-stack` works
  # in this shell right away; otherwise fall back to the first usable one.
  for candidate in /usr/local/bin "$HOME/.local/bin"; do
    if on_path "$candidate" && usable_dir "$candidate"; then dir="$candidate"; break; fi
  done
  if [ -z "$dir" ]; then
    if usable_dir /usr/local/bin; then dir=/usr/local/bin; else dir="$HOME/.local/bin"; fi
  fi
fi
if [ ! -d "$dir" ]; then
  mkdir -p "$dir" 2>/dev/null \
    || { command -v sudo >/dev/null 2>&1 && sudo mkdir -p "$dir"; } \
    || die "could not create $dir (set AF_STACK_INSTALL_DIR to a writable dir)"
fi

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

# --- download --------------------------------------------------------------
# Archives and checksums use the version WITHOUT the leading 'v'.
archive_for()   { printf '%s' "${BINARY}_${1#v}_${os}_${arch}.tar.gz"; }
download_base() {
  if [ -n "${AF_STACK_DOWNLOAD_BASE:-}" ]; then
    printf '%s' "${AF_STACK_DOWNLOAD_BASE%/}"
  else
    printf '%s' "https://github.com/$REPO/releases/download/$1"
  fi
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# fetch_archive TAG: sets archive/base for TAG and downloads the archive.
fetch_archive() {
  archive="$(archive_for "$1")"
  base="$(download_base "$1")"
  # Errors are kept for the final message so a failed first spelling of the
  # tag does not print a stray 404 when the retry succeeds.
  curl -fsSL "$base/$archive" -o "$tmp/$archive" 2>"$tmp/curl.err"
}

info "Downloading $(archive_for "$version") ($version)..."
if ! fetch_archive "$version"; then
  # Tags are normally v-prefixed, but AF_STACK_VERSION is often given bare
  # (release.yml and docker-compose.prod.yml use it that way). Try the other
  # spelling once before giving up.
  case "$version" in
    v*) alt="${version#v}" ;;
    *)  alt="v$version" ;;
  esac
  if fetch_archive "$alt"; then
    version="$alt"
  else
    die "download failed: $(download_base "$version")/$(archive_for "$version") (also tried tag $alt; $(tr -d '\n' < "$tmp/curl.err")). Check that the release exists and has a ${os}/${arch} asset; a private $REPO 404s until it is made public."
  fi
fi

# --- verify ----------------------------------------------------------------
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

if [ "${AF_STACK_SKIP_CHECKSUM:-0}" = "1" ]; then
  warn "AF_STACK_SKIP_CHECKSUM=1 — installing $archive without checksum verification"
else
  # Every release publishes checksums.txt, so failing to fetch it is a
  # transport problem, not a missing file. Fail closed rather than quietly
  # installing an unverified binary; branch on the HTTP status because
  # `curl -f` exits 22 for every 4xx/5xx alike.
  code="$(curl -sSL -o "$tmp/checksums.txt" -w '%{http_code}' "$base/checksums.txt" 2>"$tmp/curl.err")" || code=000
  if [ "$code" != "200" ]; then
    detail=""
    if [ -s "$tmp/curl.err" ]; then detail=" ($(tr -d '\n' < "$tmp/curl.err"))"; fi
    die "could not fetch $base/checksums.txt (HTTP $code$detail) — refusing to install an unverified binary. Retry, or set AF_STACK_SKIP_CHECKSUM=1 to override."
  fi
  info "Verifying checksum..."
  (cd "$tmp" && verify_checksum) || die "checksum verification failed for $archive"
fi

tar -xzf "$tmp/$archive" -C "$tmp"
[ -f "$tmp/$BINARY" ] || die "archive did not contain the '$BINARY' binary"

# --- install ---------------------------------------------------------------
# `install` (not mv) so a sudo install lands root-owned instead of leaving a
# user-writable executable in a system PATH directory.
if install -m 0755 "$tmp/$BINARY" "$dir/$BINARY" 2>/dev/null; then :
elif command -v sudo >/dev/null 2>&1 && sudo install -m 0755 "$tmp/$BINARY" "$dir/$BINARY"; then :
else die "could not install to $dir (set AF_STACK_INSTALL_DIR to a writable dir)"; fi

# Prove the installed binary runs before claiming success; a noexec mount or
# a truncated download would otherwise surface as the user's next command.
if ! probe="$("$dir/$BINARY" version 2>&1)"; then
  die "installed $dir/$BINARY but it will not run: ${probe:-no output} (a 'Permission denied' here usually means $dir is on a noexec mount)"
fi
info "Installed $probe to $dir/$BINARY"

# --- is it reachable as `af-stack`? ----------------------------------------
resolved="$(command -v "$BINARY" 2>/dev/null || true)"
if on_path "$dir" && [ "$resolved" = "$dir/$BINARY" ]; then
  : # `af-stack dev` works in this shell right now
elif on_path "$dir"; then
  warn "$dir is on your PATH but '$BINARY' currently resolves to ${resolved:-nothing} — remove or update that older copy"
else
  case "${SHELL##*/}" in
    zsh)  rc_hint="echo 'export PATH=\"$dir:\$PATH\"' >> ~/.zshrc" ;;
    bash) rc_hint="echo 'export PATH=\"$dir:\$PATH\"' >> ~/.bashrc" ;;
    fish) rc_hint="fish_add_path $dir" ;;
    *)    rc_hint="" ;;
  esac
  warn "$dir is not on your PATH, so '$BINARY dev' will not be found yet. In this shell run:"
  # shellcheck disable=SC2016  # the literal $PATH is what the user should type
  printf '    export PATH="%s:%s"\n' "$dir" '$PATH' >&2
  if [ -n "$rc_hint" ]; then
    printf '  and to make it permanent:\n    %s\n' "$rc_hint" >&2
  fi
fi
