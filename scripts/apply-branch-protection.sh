#!/usr/bin/env bash
# Apply the public-release branch ruleset on `main`.
#
# Requires a token with admin:repo (or org owner) on Agent-Field/BackAI.
# The cloud / Actions GITHUB_TOKEN cannot do this — it is a one-time
# maintainer action.
#
# Usage:
#   GH_HOST=github.com scripts/apply-branch-protection.sh
#   REPO=Agent-Field/BackAI scripts/apply-branch-protection.sh
set -euo pipefail

REPO="${REPO:-Agent-Field/BackAI}"
RULESET_NAME="${RULESET_NAME:-main public-release protection}"

if ! command -v gh >/dev/null 2>&1; then
  echo "gh CLI is required" >&2
  exit 1
fi

echo "Applying ruleset '$RULESET_NAME' on $REPO (main)"

# Reuse an existing ruleset of the same name so the script is idempotent.
existing_id="$(
  gh api "repos/${REPO}/rulesets" --jq \
    "[.[] | select(.name == \"${RULESET_NAME}\") | .id][0] // empty" \
    2>/dev/null || true
)"

payload="$(cat <<'JSON'
{
  "name": "main public-release protection",
  "target": "branch",
  "enforcement": "active",
  "bypass_actors": [],
  "conditions": {
    "ref_name": {
      "include": ["refs/heads/main"],
      "exclude": []
    }
  },
  "rules": [
    { "type": "deletion" },
    { "type": "non_fast_forward" },
    {
      "type": "pull_request",
      "parameters": {
        "required_approving_review_count": 1,
        "dismiss_stale_reviews_on_push": true,
        "require_code_owner_review": false,
        "require_last_push_approval": false,
        "required_review_thread_resolution": true,
        "allowed_merge_methods": ["squash", "merge"]
      }
    },
    {
      "type": "required_status_checks",
      "parameters": {
        "strict_required_status_checks_policy": true,
        "do_not_enforce_on_create": false,
        "required_status_checks": [
          { "context": "CI Success" },
          { "context": "Security Success" }
        ]
      }
    }
  ]
}
JSON
)"

if [ -n "${existing_id:-}" ]; then
  echo "Updating existing ruleset id=$existing_id"
  echo "$payload" | gh api --method PUT "repos/${REPO}/rulesets/${existing_id}" --input -
else
  echo "Creating ruleset"
  echo "$payload" | gh api --method POST "repos/${REPO}/rulesets" --input -
fi

echo
echo "Ruleset applied. Confirm at:"
echo "  https://github.com/${REPO}/settings/rules"
echo
echo "Expected enforcement on main:"
echo "  - no direct pushes / no force-push / no delete"
echo "  - PR required, 1 approving review, dismiss stale reviews"
echo "  - required checks: CI Success, Security Success (must be current)"
