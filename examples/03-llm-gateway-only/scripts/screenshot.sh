#!/usr/bin/env bash
# Capture cost.png for the README.
#
# Runs test-call.sh first so the dashboard has real spend data, then
# invokes the repo-root capture-screenshots.mjs targeted at this stack's
# dashboard port.
#
# The capture script writes all screenshots to <repo-root>/dashboard-
# screenshots/. We only care about cost.png for this example's README.
#
# Requires:
#   - docker compose up running here
#   - node + playwright installed at the repo root (cd ../../scripts && npm i)

set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "${HERE}/../../.." && pwd)"

PORT="${AF_STACK_PORT:-8080}"
DASH_PORT="${AF_STACK_DASHBOARD_PORT:-3000}"

echo "==> generating spend with test-call.sh"
AF_STACK_PORT="${PORT}" "${HERE}/test-call.sh" || {
    echo "test-call.sh failed; screenshot will show empty state" >&2
}

echo
echo "==> capturing dashboard screenshots via playwright"
DASHBOARD_URL="http://localhost:${DASH_PORT}" \
AF_STACK_PORT="${PORT}" \
node "${REPO_ROOT}/scripts/capture-screenshots.mjs" || {
    echo "capture-screenshots.mjs failed" >&2
    exit 1
}

# The capture script writes every page; this example only needs cost.png.
OUT="${REPO_ROOT}/dashboard-screenshots/cost.png"
if [ -f "${OUT}" ]; then
    echo
    echo "✓ cost.png ready at: ${OUT}"
    echo "  reference it from this README as ../../dashboard-screenshots/cost.png"
else
    echo "WARN: ${OUT} not found" >&2
    exit 1
fi
