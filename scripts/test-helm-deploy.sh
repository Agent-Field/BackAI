#!/usr/bin/env bash
# End-to-end Helm deploy validation against a local k3d cluster.
#
# Spins up k3d, helm-installs the chart with values-dev.yaml, waits for
# pods ready, runs the quickstart against the k3d-exposed port, then
# tears down. Skips cleanly when k3d isn't installed.

set -euo pipefail
cd "$(dirname "$0")/.."

green()  { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
red()    { printf "\033[31m%s\033[0m\n" "$1"; }
step()   { printf "\n\033[1;36m==> %s\033[0m\n" "$1"; }

skip() { yellow "SKIP: $1"; exit 0; }
fail() { red "FAIL: $1"; exit 1; }

command -v k3d  >/dev/null 2>&1 || skip "k3d not installed — brew install k3d"
command -v helm >/dev/null 2>&1 || skip "helm not installed — brew install helm"
command -v kubectl >/dev/null 2>&1 || skip "kubectl not installed"

CLUSTER="${AF_STACK_K3D_CLUSTER:-af-stack-helm-test}"
NS="af-stack"

cleanup() {
    yellow "==> Tearing down cluster $CLUSTER"
    k3d cluster delete "$CLUSTER" >/dev/null 2>&1 || true
}
trap cleanup EXIT

step "Create k3d cluster $CLUSTER"
k3d cluster delete "$CLUSTER" >/dev/null 2>&1 || true
k3d cluster create "$CLUSTER" \
    --port "38080:80@loadbalancer" \
    --agents 1 \
    --wait

step "Update Helm dependencies"
( cd deploy/helm/af-stack && helm dep update . )

step "Install chart with values-dev.yaml"
helm install af-stack ./deploy/helm/af-stack \
    --namespace "$NS" --create-namespace \
    --values ./deploy/helm/af-stack/values-dev.yaml \
    --set ingress.hosts[0].host=localhost \
    --set ingress.hosts[0].paths[0].path=/ \
    --set ingress.hosts[0].paths[0].pathType=Prefix \
    --wait --timeout 5m

step "Probe /health"
sleep 5
if curl -sf -o /dev/null http://localhost:38080/health; then
    green "  /health 200"
else
    fail "/health did not return 200"
fi

step "Probe /ready"
if curl -sf -o /dev/null http://localhost:38080/ready; then
    green "  /ready 200"
else
    fail "/ready did not return 200"
fi

step "Smoke-test quickstart"
if scripts/test-quickstart.sh; then
    green "  quickstart passed"
else
    fail "quickstart failed"
fi

green ""
green "==> All Helm deploy checks passed"
