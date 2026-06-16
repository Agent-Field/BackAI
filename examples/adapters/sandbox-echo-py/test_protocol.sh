#!/usr/bin/env bash
set -o pipefail

# BackAI Sandbox v1 Echo Adapter — Protocol Smoke Tests
# 
# Tests each endpoint with curl and prints PASS/FAIL.
# Assumes the adapter is running on port 8090.

PORT=8090
BASE_URL="http://localhost:${PORT}"
ADAPTER_TOKEN="${BACKAI_ADAPTER_TOKEN:-}"
PASS_COUNT=0
FAIL_COUNT=0

# Helper to extract http code from curl output
function extract_code() {
    echo "$1" | tail -1
}

function extract_body() {
    echo "$1" | sed '$d'
}

# Helper to make a request with optional auth
function req() {
    local method=$1
    local path=$2
    local body=$3
    
    local auth_header=""
    if [ -n "$ADAPTER_TOKEN" ]; then
        auth_header="-H Authorization:\ Bearer\ ${ADAPTER_TOKEN}"
    fi
    
    if [ -z "$body" ]; then
        # GET/DELETE
        curl -s -w "\n%{http_code}" \
            -X "$method" \
            $auth_header \
            -H "X-BackAI-Request-Id: test-$(date +%s%N)" \
            "$BASE_URL$path"
    else
        # POST with body
        curl -s -w "\n%{http_code}" \
            -X "$method" \
            $auth_header \
            -H "Content-Type: application/json" \
            -H "X-BackAI-Request-Id: test-$(date +%s%N)" \
            -H "X-BackAI-Idempotency-Key: idem-$(date +%s%N)" \
            -d "$body" \
            "$BASE_URL$path"
    fi
}

function test_pass() {
    echo "✓ PASS: $1"
    ((PASS_COUNT++))
}

function test_fail() {
    echo "✗ FAIL: $1"
    ((FAIL_COUNT++))
}

echo "=== BackAI Sandbox v1 Echo Adapter — Protocol Tests ==="
echo "Target: $BASE_URL"
echo ""

# Test 1: GET /healthz
echo "Test 1: GET /healthz"
response=$(req GET "/healthz" "")
http_code=$(extract_code "$response")
body=$(extract_body "$response")
if [ "$http_code" = "200" ]; then
    if echo "$body" | grep -q '"status":"healthy"'; then
        test_pass "Health check returns 200 with status=healthy"
    else
        test_fail "Health check missing status=healthy: $body"
    fi
else
    test_fail "Health check returned HTTP $http_code (expected 200)"
fi
echo ""

# Test 2: GET /v1/capabilities
echo "Test 2: GET /v1/capabilities"
response=$(req GET "/v1/capabilities" "")
http_code=$(extract_code "$response")
body=$(extract_body "$response")
if [ "$http_code" = "200" ]; then
    if echo "$body" | grep -q '"name":"echo"' && echo "$body" | grep -q '"supports_streaming":true'; then
        test_pass "Capabilities returns 200 with name=echo and streaming support"
    else
        test_fail "Capabilities missing expected fields: $body"
    fi
else
    test_fail "Capabilities returned HTTP $http_code (expected 200)"
fi
echo ""

# Test 3: GET /v1/info
echo "Test 3: GET /v1/info"
response=$(req GET "/v1/info" "")
http_code=$(extract_code "$response")
body=$(extract_body "$response")
if [ "$http_code" = "200" ]; then
    if echo "$body" | grep -q '"docs"'; then
        test_pass "Info returns 200 with docs field"
    else
        test_fail "Info missing docs field: $body"
    fi
else
    test_fail "Info returned HTTP $http_code (expected 200)"
fi
echo ""

# Test 4: POST /v1/runs
echo "Test 4: POST /v1/runs"
run_spec='{"id":"test-run-1","tenant_id":"test","image":"python:3.12-slim","command":["python","-c","print(42)"],"timeout_s":30,"cpu":2,"memory_gb":4,"network":"open"}'
response=$(req POST "/v1/runs" "$run_spec")
http_code=$(extract_code "$response")
body=$(extract_body "$response")
if [ "$http_code" = "200" ]; then
    if echo "$body" | grep -q '"status":"done"' && echo "$body" | grep -q '"exit_code":0'; then
        test_pass "POST /v1/runs returns 200 with status=done and exit_code=0"
    else
        test_fail "POST /v1/runs missing expected fields: $body"
    fi
else
    test_fail "POST /v1/runs returned HTTP $http_code (expected 200)"
fi
echo ""

# Test 5: POST /v1/runs/stream
echo "Test 5: POST /v1/runs/stream"
run_spec='{"id":"test-run-2","tenant_id":"test","image":"python:3.12-slim","command":["echo","hello"],"timeout_s":30,"cpu":2,"memory_gb":4,"network":"open"}'
response=$(req POST "/v1/runs/stream" "$run_spec")
http_code=$(extract_code "$response")
body=$(extract_body "$response")
if [ "$http_code" = "200" ]; then
    if echo "$body" | grep -q '"event"' && echo "$body" | grep -q 'terminated'; then
        test_pass "POST /v1/runs/stream returns 200 with terminated event"
    else
        test_fail "POST /v1/runs/stream missing terminated event: $body"
    fi
else
    test_fail "POST /v1/runs/stream returned HTTP $http_code (expected 200)"
fi
echo ""

# Test 6: GET /v1/runs/{id}
echo "Test 6: GET /v1/runs/{id}"
response=$(req GET "/v1/runs/test-run-1" "")
http_code=$(extract_code "$response")
body=$(extract_body "$response")
if [ "$http_code" = "200" ]; then
    if echo "$body" | grep -q '"status":"done"'; then
        test_pass "GET /v1/runs/{id} returns 200 with status=done"
    else
        test_fail "GET /v1/runs/{id} missing status: $body"
    fi
else
    test_fail "GET /v1/runs/{id} returned HTTP $http_code (expected 200)"
fi
echo ""

# Test 7: DELETE /v1/runs/{id}
echo "Test 7: DELETE /v1/runs/{id}"
response=$(req DELETE "/v1/runs/test-run-1" "")
http_code=$(extract_code "$response")
if [ "$http_code" = "204" ]; then
    test_pass "DELETE /v1/runs/{id} returns 204"
else
    test_fail "DELETE /v1/runs/{id} returned HTTP $http_code (expected 204)"
fi
echo ""

# Test 8: GET /v1/pool
echo "Test 8: GET /v1/pool"
response=$(req GET "/v1/pool" "")
http_code=$(extract_code "$response")
body=$(extract_body "$response")
if [ "$http_code" = "200" ]; then
    if echo "$body" | grep -q '"adapter":"echo"'; then
        test_pass "GET /v1/pool returns 200 with adapter=echo"
    else
        test_fail "GET /v1/pool missing adapter field: $body"
    fi
else
    test_fail "GET /v1/pool returned HTTP $http_code (expected 200)"
fi
echo ""

# Test 9: Idempotency (POST /v1/runs with same key should return same result)
echo "Test 9: Idempotency caching"
run_spec='{"id":"test-run-3","tenant_id":"test","image":"python:3.12-slim","command":["python","-c","print(42)"],"timeout_s":30,"cpu":2,"memory_gb":4,"network":"open"}'
idem_key="idem-test-$(date +%s)"
response1=$(curl -s -w "\n%{http_code}" \
    -X POST \
    -H "Content-Type: application/json" \
    -H "X-BackAI-Request-Id: test-$(date +%s%N)" \
    -H "X-BackAI-Idempotency-Key: $idem_key" \
    -d "$run_spec" \
    "$BASE_URL/v1/runs")
body1=$(extract_body "$response1")
http_code1=$(extract_code "$response1")

# Repeat with same key
response2=$(curl -s -w "\n%{http_code}" \
    -X POST \
    -H "Content-Type: application/json" \
    -H "X-BackAI-Request-Id: test-$(date +%s%N)" \
    -H "X-BackAI-Idempotency-Key: $idem_key" \
    -d "$run_spec" \
    "$BASE_URL/v1/runs")
body2=$(extract_body "$response2")
http_code2=$(extract_code "$response2")

if [ "$http_code1" = "200" ] && [ "$http_code2" = "200" ]; then
    if [ "$body1" = "$body2" ]; then
        test_pass "Idempotency: repeated POST with same key returns identical response"
    else
        test_fail "Idempotency: responses differ on repeat"
    fi
else
    test_fail "Idempotency: unexpected HTTP codes ($http_code1, $http_code2)"
fi
echo ""

# Test 10: Bearer token auth (if token is set)
if [ -n "$ADAPTER_TOKEN" ]; then
    echo "Test 10: Bearer token auth"
    # Missing token
    response=$(curl -s -w "\n%{http_code}" \
        -X GET \
        "$BASE_URL/healthz")
    http_code=$(extract_code "$response")
    if [ "$http_code" = "401" ]; then
        test_pass "Missing token returns 401"
    else
        test_fail "Missing token returned HTTP $http_code (expected 401)"
    fi
    echo ""
fi

# Summary
echo "=== Test Summary ==="
echo "PASS: $PASS_COUNT"
echo "FAIL: $FAIL_COUNT"
echo ""

if [ $FAIL_COUNT -eq 0 ]; then
    echo "All tests passed!"
    exit 0
else
    echo "Some tests failed."
    exit 1
fi
