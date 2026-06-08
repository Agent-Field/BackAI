#!/usr/bin/env bash
# End-to-end Helm deploy validation against a local kind cluster.
#
# This is the CI-grade Kubernetes smoke test for deploy/helm/af-stack:
# it creates a disposable kind cluster, builds current runtime/dashboard
# images into that cluster when requested, installs the chart with the
# dev preset, then verifies the chart-owned health endpoints.

set -euo pipefail
cd "$(dirname "$0")/.."

green()  { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
red()    { printf "\033[31m%s\033[0m\n" "$1"; }
step()   { printf "\n\033[1;36m==> %s\033[0m\n" "$1"; }

skip() { yellow "SKIP: $1"; exit 0; }
fail() { red "FAIL: $1"; exit 1; }

command -v kind >/dev/null 2>&1 || skip "kind not installed"
command -v helm >/dev/null 2>&1 || skip "helm not installed"
command -v kubectl >/dev/null 2>&1 || skip "kubectl not installed"
command -v docker >/dev/null 2>&1 || skip "docker not installed"
docker info >/dev/null 2>&1 || skip "docker daemon not reachable"

CLUSTER="${AF_STACK_KIND_CLUSTER:-af-stack-helm-test}"
NS="${AF_STACK_HELM_NAMESPACE:-af-stack}"
RELEASE="${AF_STACK_HELM_RELEASE:-af-stack}"
BUILD_IMAGES="${AF_STACK_KIND_BUILD_IMAGES:-true}"
RUNTIME_IMAGE="${AF_STACK_KIND_RUNTIME_IMAGE:-af-stack-runtime:ci}"
DASHBOARD_IMAGE="${AF_STACK_KIND_DASHBOARD_IMAGE:-af-stack-dashboard:ci}"
TIMEOUT="${AF_STACK_HELM_TIMEOUT:-8m}"

cleanup() {
    yellow "==> Tearing down kind cluster $CLUSTER"
    kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true
}
trap cleanup EXIT

step "Create kind cluster $CLUSTER"
kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true
kind create cluster --name "$CLUSTER" --wait 120s

if [ "$BUILD_IMAGES" = "true" ]; then
    step "Build runtime image $RUNTIME_IMAGE"
    docker build -t "$RUNTIME_IMAGE" -f services/runtime/Dockerfile .
    kind load docker-image "$RUNTIME_IMAGE" --name "$CLUSTER"

    step "Build dashboard image $DASHBOARD_IMAGE"
    docker build -t "$DASHBOARD_IMAGE" -f apps/dashboard/Dockerfile .
    kind load docker-image "$DASHBOARD_IMAGE" --name "$CLUSTER"
fi

runtime_repo="${RUNTIME_IMAGE%:*}"
runtime_tag="${RUNTIME_IMAGE##*:}"
dashboard_repo="${DASHBOARD_IMAGE%:*}"
dashboard_tag="${DASHBOARD_IMAGE##*:}"

step "Install Helm dependencies"
helm dependency build deploy/helm/af-stack

step "Install chart"
helm upgrade --install "$RELEASE" ./deploy/helm/af-stack \
    --namespace "$NS" --create-namespace \
    --values ./deploy/helm/af-stack/values-dev.yaml \
    --set ingress.enabled=false \
    --set image.runtime.repository="$runtime_repo" \
    --set image.runtime.tag="$runtime_tag" \
    --set image.runtime.pullPolicy=Never \
    --set image.dashboard.repository="$dashboard_repo" \
    --set image.dashboard.tag="$dashboard_tag" \
    --set image.dashboard.pullPolicy=Never \
    --wait --timeout "$TIMEOUT"

step "Verify rollout"
kubectl -n "$NS" rollout status "deploy/${RELEASE}-runtime" --timeout=180s
kubectl -n "$NS" rollout status "deploy/${RELEASE}-dashboard" --timeout=180s

step "Port-forward runtime service"
kubectl -n "$NS" port-forward "svc/${RELEASE}-runtime" 18080:8080 >/tmp/af-stack-kind-runtime.log 2>&1 &
PF_PID=$!
trap 'kill "$PF_PID" >/dev/null 2>&1 || true; cleanup' EXIT
sleep 5

step "Probe runtime /health and /ready"
curl -fsS http://127.0.0.1:18080/health >/dev/null || fail "/health did not return 2xx"
curl -fsS http://127.0.0.1:18080/ready >/dev/null || fail "/ready did not return 2xx"

green ""
green "==> Helm kind smoke passed"
