#!/usr/bin/env bash
# Quick load test against a running AF Stack deploy. Uses k6 (preferred)
# or vegeta (fallback). Tests 100 / 500 / 1000 req/s for 60s each
# against /api/v1/cost (which is in-memory + cached, so it's a fair
# representation of the runtime's hot path).
#
# Usage:
#   ./scripts/load-test.sh
#   ./scripts/load-test.sh --target http://localhost:8080 --rates 50,200,500

set -euo pipefail

TARGET="${AF_STACK_LOAD_TEST_TARGET:-http://localhost:8080}"
RATES="100,500,1000"
DURATION="60s"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --target)   TARGET="$2"; shift 2;;
        --rates)    RATES="$2"; shift 2;;
        --duration) DURATION="$2"; shift 2;;
        -h|--help)
            head -n 8 "$0" | tail -n 7 | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *) echo "unknown flag: $1" >&2; exit 1;;
    esac
done

green()  { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
red()    { printf "\033[31m%s\033[0m\n" "$1"; }

# Sanity check: target responding?
if ! curl -s -o /dev/null -w "%{http_code}" "$TARGET/health" | grep -q "^200$"; then
    red "target $TARGET unreachable or unhealthy — abort."
    exit 1
fi

# Detect which loader is installed.
if command -v k6 >/dev/null 2>&1; then
    LOADER="k6"
elif command -v vegeta >/dev/null 2>&1; then
    LOADER="vegeta"
else
    red "Need k6 or vegeta installed. Try: brew install k6"
    exit 1
fi

green "==> Loader: $LOADER. Target: $TARGET. Duration per stage: $DURATION"

IFS=',' read -ra RATE_ARRAY <<< "$RATES"
for rate in "${RATE_ARRAY[@]}"; do
    green ""
    green "==> $rate req/s for $DURATION"

    if [ "$LOADER" = "k6" ]; then
        cat > /tmp/af-stack-load.js <<EOF
import http from 'k6/http'
import { check } from 'k6'
export const options = {
    scenarios: {
        constant: {
            executor: 'constant-arrival-rate',
            rate: $rate, timeUnit: '1s',
            duration: '$DURATION',
            preAllocatedVUs: 50, maxVUs: 500,
        }
    },
    thresholds: {
        'http_req_failed':   ['rate<0.02'],
        'http_req_duration': ['p(95)<500'],
    }
}
export default function () {
    const res = http.get('$TARGET/api/v1/cost')
    check(res, { 'status 200': r => r.status === 200 })
}
EOF
        k6 run --summary-trend-stats="avg,p(95),p(99)" /tmp/af-stack-load.js || true
    else
        echo "GET $TARGET/api/v1/cost" \
            | vegeta attack -rate "$rate" -duration "$DURATION" \
            | vegeta report -type=text
    fi
done

green ""
green "==> Done. See docs/multi-replica.md for what these numbers mean."
