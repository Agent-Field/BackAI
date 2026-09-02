#!/usr/bin/env bash
# Assert GHCR images are anonymously pullable (no docker login).
#
# `af-stack init` apps pull these with no registry credentials. GHCR creates
# packages private by default, so a release whose images are private ships a
# CLI whose scaffolds cannot boot.
#
# Usage:
#   AF_STACK_VERSION=0.13.0 scripts/assert-ghcr-public.sh
#   AF_STACK_VERSION=0.13.0 scripts/assert-ghcr-public.sh runtime dashboard
#
# Do not use `curl -f` against the anonymous token endpoint — private
# packages return HTTP 401 with an empty body, and -f + JSON.parse crashes
# before the script can list which packages are private.
set -euo pipefail

VERSION="${AF_STACK_VERSION:?set AF_STACK_VERSION to the image tag to check}"
NAMESPACE="${GHCR_NAMESPACE:-agent-field}"
ORG="${GHCR_ORG:-Agent-Field}"

if [ "$#" -eq 0 ]; then
  set -- runtime dashboard customer-app supportdesk-agent
fi

token_file="$(mktemp)"
trap 'rm -f "$token_file"' EXIT

private=""
for svc in "$@"; do
  repo="${NAMESPACE}/af-stack-${svc}"
  image="ghcr.io/${repo}:${VERSION}"

  token_code="$(curl -sS -o "$token_file" -w '%{http_code}' \
    "https://ghcr.io/token?scope=repository:${repo}:pull" || echo "000")"
  if [ "$token_code" != "200" ]; then
    echo "${image} anonymous token: HTTP ${token_code} (private or missing)"
    private="${private} af-stack-${svc}"
    continue
  fi

  token="$(python3 -c '
import json, sys
try:
    data = json.load(open(sys.argv[1]))
except Exception:
    raise SystemExit(0)
print(data.get("token") or "")
' "$token_file")"
  if [ -z "$token" ]; then
    echo "${image} anonymous token: empty body"
    private="${private} af-stack-${svc}"
    continue
  fi

  # build-push-action with provenance:false still publishes an OCI image
  # manifest. Without vnd.oci.image.manifest.v1+json, GHCR returns 404
  # MANIFEST_UNKNOWN even for a public, pullable tag.
  code="$(curl -sS -o /dev/null -w '%{http_code}' \
    -H "Authorization: Bearer ${token}" \
    -H 'Accept: application/vnd.oci.image.manifest.v1+json, application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json' \
    "https://ghcr.io/v2/${repo}/manifests/${VERSION}" || echo "000")"
  echo "${image} anonymous pull: HTTP ${code}"
  if [ "$code" != "200" ]; then
    private="${private} af-stack-${svc}"
  fi
done

if [ -n "$private" ]; then
  echo
  echo "::error::these GHCR packages are not publicly pullable:${private} — make each one Public under the org's package settings and re-run Release; until then apps from \`af-stack init\` cannot boot their bundled backend"
  echo
  echo "GHCR creates packages private by default. One-time fix (org owner):"
  echo "  1. https://github.com/orgs/${ORG}/packages"
  echo "  2. Each package above → Package settings → Change visibility → Public"
  echo "  3. Or run: scripts/publish-ghcr-packages.sh"
  echo "  4. Re-run the Release workflow (Actions → Release → Run workflow)."
  echo
  echo "Settings URLs:"
  for pkg in $private; do
    echo "  https://github.com/orgs/${ORG}/packages/container/package/${pkg}/settings"
  done
  exit 1
fi
