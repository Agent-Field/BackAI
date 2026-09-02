# Branch protection (public `main`)

`main` is the release branch. CI and security must be green before a
merge, and the branch must not accept force-pushes or unreviewed
direct commits.

The Actions token used by cloud agents **cannot** create rulesets
(admin API, HTTP 403). A maintainer with repo admin applies the
ruleset once:

```bash
# from a clone, authenticated as an org owner / repo admin
scripts/apply-branch-protection.sh
```

Or in the GitHub UI: **Settings → Rules → Rulesets → New branch
ruleset**, targeting `main`, with the same rules as below.

## Required rules

| Rule | Value |
| --- | --- |
| Target | `refs/heads/main` |
| Deletion | blocked |
| Force push / non-fast-forward | blocked |
| Pull request | required |
| Approving reviews | 1 |
| Dismiss stale reviews on new push | yes |
| Conversation resolution | required |
| Allowed merge methods | squash, merge |
| Require status checks to be up to date | yes |
| Required checks | `CI Success`, `Security Success` |

`CI Success` (`.github/workflows/ci.yml`) aggregates lint, test,
compose/deploy validation, the install-script gate, docs, and the DCO
job. Path-filtered jobs that skip still count as success.

`Security Success` (`.github/workflows/security.yml`) aggregates
pnpm/npm audit, pip-audit, gosec, and trivy. CodeQL uploads results
on the public repo but is **not** a required check, so a first-time
CodeQL setup issue cannot block every PR.

## DCO

Every human PR commit must carry `Signed-off-by` (`git commit -s`).
The `DCO` job in CI enforces this on pull requests. Dependabot is
skipped. See [CONTRIBUTING.md](../CONTRIBUTING.md).

## After applying

```bash
gh api repos/Agent-Field/BackAI/rulesets
gh api repos/Agent-Field/BackAI/branches/main --jq '{name,protected,protection}'
```

`protected` should be `true` (or a ruleset should list `main`).
