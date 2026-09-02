#!/usr/bin/env bash
# Make the release GHCR packages publicly pullable.
#
# GHCR creates packages private on first push. The REST API can change
# visibility for some tokens; org-owned container packages often 404 even
# for admins — in that case this script prints the UI path and exits 1.
#
# Requires a token with write:packages (and package admin) on the org.
# GITHUB_TOKEN from Actions may work after the workflow that published the
# package; cloud-agent tokens typically cannot. A one-time org-owner click
# in the package settings is the reliable fallback.
#
# Usage:
#   scripts/publish-ghcr-packages.sh
#   scripts/publish-ghcr-packages.sh af-stack-runtime af-stack-dashboard
set -euo pipefail

if ! command -v gh >/dev/null 2>&1; then
  echo "gh CLI is required" >&2
  exit 1
fi

ORG="${GHCR_ORG:-Agent-Field}"

if [ "$#" -eq 0 ]; then
  set -- af-stack-runtime af-stack-dashboard af-stack-customer-app af-stack-supportdesk-agent
fi

failed=""
for pkg in "$@"; do
  settings="https://github.com/orgs/${ORG}/packages/container/package/${pkg}/settings"
  vis=""
  # Retry GET — a just-pushed package can 404 for a few seconds.
  # gh prints the error JSON on stdout even when it exits non-zero; only
  # keep a real visibility enum so we don't treat "Package not found" as one.
  for attempt in 1 2 3; do
    got="$(gh api "orgs/${ORG}/packages/container/${pkg}" --jq .visibility 2>/dev/null || true)"
    case "$got" in
      public|private|internal)
        vis="$got"
        break
        ;;
    esac
    if [ "$attempt" -lt 3 ]; then
      sleep $((attempt * 2))
    fi
  done

  if [ "$vis" = "public" ]; then
    echo "OK  ${pkg} already public"
    continue
  fi

  if [ -n "$vis" ]; then
    echo "…  ${pkg} is ${vis}; trying PATCH visibility=public"
  else
    echo "…  ${pkg} not readable via API; trying PATCH visibility=public"
  fi

  if gh api --method PATCH "orgs/${ORG}/packages/container/${pkg}" \
    -f visibility=public >/dev/null 2>/tmp/ghcr-vis-err; then
    echo "OK  ${pkg} set public"
    continue
  fi

  err="$(tr '\n' ' ' </tmp/ghcr-vis-err | head -c 300)"
  echo "FAIL ${pkg} — API cannot change visibility (${err})"
  echo "     Open ${settings} → Change visibility → Public"
  failed="${failed} ${pkg}"
done

if [ -n "$failed" ]; then
  echo
  echo "Still private:${failed}"
  echo "The REST API cannot change visibility for some org-owned GHCR"
  echo "packages. An org owner must flip them once in the UI; later"
  echo "releases reuse the same names and stay public."
  exit 1
fi
