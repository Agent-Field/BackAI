#!/usr/bin/env bash
# fetch-openapi.sh — refresh docs-site/public/openapi.json from the running
# runtime, then enrich with per-language code samples.
#
# Why this exists
# ---------------
# The runtime serves a complete OpenAPI 3.1 spec at /openapi.json. Rather
# than depending on a live runtime to render docs (which would break offline
# builds and CI), we snapshot the spec into the static `public/` folder.
# Starlight serves anything under public/ verbatim, so the spec ships with
# the site and the Scalar reference loads instantly with no proxy.
#
# Re-run this script whenever the runtime's routes change. The git diff on
# public/openapi.json is the review surface for "did this PR change the
# public API contract".
#
# Behaviour
# ---------
# * Probes http://localhost:38080/openapi.json (override via AF_STACK_PORT).
# * On success: writes the spec, then runs scripts/inject-code-samples.mjs
#   to attach x-codeSamples to ~20 key endpoints.
# * On failure (runtime not running, network error, non-2xx response):
#   prints a yellow warning and exits 0. This keeps CI green when nobody
#   has the dev stack up — the previous snapshot in public/openapi.json
#   stays in place.
#
# Usage:  ./docs-site/scripts/fetch-openapi.sh

set -euo pipefail

PORT="${AF_STACK_PORT:-38080}"
URL="http://localhost:${PORT}/openapi.json"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DOCS_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUT="$DOCS_ROOT/public/openapi.json"
INJECTOR="$SCRIPT_DIR/inject-code-samples.mjs"

yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
green()  { printf "\033[32m%s\033[0m\n" "$1"; }

mkdir -p "$(dirname "$OUT")"

# Curl with a 5s connect timeout and 30s overall cap. -f makes curl exit
# non-zero on >=400 so we treat "spec endpoint missing" as a failure to
# fetch (not a successful empty file). -S surfaces real network errors
# even with -s.
TMP="$(mktemp -t af-openapi.XXXXXX)"
trap 'rm -f "$TMP" 2>/dev/null || true' EXIT

if curl -fsS \
    --connect-timeout 5 \
    --max-time 30 \
    -H "Accept: application/json" \
    "$URL" -o "$TMP"; then
    # Light sanity check: must be JSON and must declare openapi.
    if ! grep -q '"openapi"' "$TMP"; then
        yellow "WARN: fetched /openapi.json but it does not look like an OpenAPI spec."
        yellow "      Leaving the existing $OUT in place (if any)."
        exit 0
    fi
    mv "$TMP" "$OUT"
    trap - EXIT
    green "fetched OpenAPI spec from $URL"
    green "wrote $OUT"
else
    yellow "WARN: could not reach $URL — runtime not running?"
    yellow "      Skipping spec refresh. The previous snapshot (if any) is unchanged."
    yellow "      Run \`docker compose up\` and re-run this script when the runtime is healthy."
    exit 0
fi

# Enrich the spec with x-codeSamples. The injector is a no-op if it can't
# find the spec; we only get here on a successful refresh so it should
# always have something to chew on.
if [ -x "$(command -v node 2>/dev/null)" ] && [ -f "$INJECTOR" ]; then
    node "$INJECTOR" "$OUT"
    green "injected x-codeSamples"
else
    yellow "NOTE: node or inject-code-samples.mjs missing — skipping code-sample injection."
fi
