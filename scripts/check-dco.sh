#!/usr/bin/env bash
# Verify every non-merge commit in a range carries a Signed-off-by trailer.
# Usage: scripts/check-dco.sh [base-ref]
# Default base is origin/main. Dependabot commits are skipped (the bot
# cannot add a DCO trailer).
set -euo pipefail

base="${1:-origin/main}"

if ! git rev-parse --verify "$base" >/dev/null 2>&1; then
  echo "Base ref $base not found; fetching..."
  git fetch --no-tags origin "${base#origin/}"
fi

range="${base}..HEAD"
echo "Checking DCO on $range"

failed=0
checked=0
while IFS= read -r sha; do
  [ -z "$sha" ] && continue
  author_email="$(git log -1 --format='%ae' "$sha")"
  author_name="$(git log -1 --format='%an' "$sha")"
  subject="$(git log -1 --format='%s' "$sha")"

  case "$author_email" in
    *dependabot*|*\[bot\]@users.noreply.github.com)
      echo "SKIP  $sha  $subject  (bot: $author_name)"
      continue
      ;;
  esac
  case "$author_name" in
    dependabot[bot]|github-actions[bot])
      echo "SKIP  $sha  $subject  (bot: $author_name)"
      continue
      ;;
  esac

  checked=$((checked + 1))
  body="$(git log -1 --format='%B' "$sha")"
  if printf '%s\n' "$body" | grep -qiE '^Signed-off-by: .+ <.+>$'; then
    echo "OK    $sha  $subject"
  else
    echo "FAIL  $sha  $subject  (missing Signed-off-by)"
    failed=$((failed + 1))
  fi
done < <(git rev-list --no-merges "$range")

if [ "$checked" -eq 0 ]; then
  echo "No human commits in range; DCO check passed."
  exit 0
fi

if [ "$failed" -ne 0 ]; then
  echo
  echo "$failed commit(s) missing a Signed-off-by trailer."
  echo "Re-commit with:  git commit -s --amend   (or git rebase -i and re-sign)"
  echo "See CONTRIBUTING.md."
  exit 1
fi

echo "DCO OK ($checked commit(s))."
