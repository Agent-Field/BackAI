---
title: Launch-day runbook
description: T-minus checklist for the public release.
sidebar:
  order: 4
---

The day-of and week-before checklist for going public with AF Stack.
Print this. Tick the boxes as you go. Don't skip the verifications —
once the post goes up, the room moves fast and you can't fix things in
real time.

## T-1 week — external tester quickstart

Five named external testers, each running the quickstart cold (no help
from you) and reporting back. Goal: every step works, the docs are
unambiguous, the dashboard renders correctly.

- [ ] Tester 1 (name + handle): ___ — completed: ___
- [ ] Tester 2: ___ — completed: ___
- [ ] Tester 3: ___ — completed: ___
- [ ] Tester 4: ___ — completed: ___
- [ ] Tester 5: ___ — completed: ___

For each tester, log every issue they hit. Anything that blocked them
for more than 60 seconds is a docs bug. Fix before launch.

## T-1 week — infrastructure

- [ ] DNS for `docs.af-stack.dev` resolves and serves the docs site.
- [ ] TLS cert valid for `docs.af-stack.dev` (Let's Encrypt or your
      CA).
- [ ] DNS for the marketing site (if separate) resolves.
- [ ] GitHub repo description + topics set: `ai`, `backend`,
      `multi-tenant`, `llm`, `sandbox`, `self-hostable`.
- [ ] Repo README hero block polished: tagline, two CTAs, demo
      screenshot.
- [ ] LICENSE file is Apache 2.0 (verify).
- [ ] CONTRIBUTING.md polished.
- [ ] SECURITY.md is at the repo root and links to a reporting email.
- [ ] `openapi.json` snapshot in `docs-site/public/` is fresh (run
      `docs-site/scripts/fetch-openapi.sh` against a current
      `docker compose up` instance).

## T-1 week — demo video

- [ ] Recorded per [demo video brief](/launch/demo-video-brief/).
- [ ] Uploaded to YouTube as Unlisted (flip to Public on launch day).
- [ ] Thumbnail set (the cost dashboard with sparkline).
- [ ] Embedded in the homepage hero on `docs.af-stack.dev`.
- [ ] Embedded in the GitHub README under the hero block.
- [ ] Captions enabled (auto-generated then hand-corrected).

## T-3 days — soft launch

Tell ~5 friends-of-the-project, watch them try it, fix anything that
breaks. This is your last chance.

- [ ] Discord server (if you're spinning one up) is live and you have
      moderator coverage for the first 48h.
- [ ] Twitter account warmed up — last 2 weeks have ≥3 substantive
      tweets so the announcement doesn't look like a cold start.
- [ ] Hacker News account is in good standing.
- [ ] LinkedIn personal account ready.

## T-1 day — final sweep

- [ ] Run all `scripts/test-*.sh` smoke tests against a fresh
      `docker compose up`. Each one should PASS or skip cleanly.
- [ ] `cd docs-site && npm run build` clean. Spot-check 5 random
      pages for broken links.
- [ ] `helm lint deploy/helm/af-stack/` clean.
- [ ] `gosec ./services/...` + `npm audit --omit=dev` + `pip-audit`
      clean.
- [ ] `git log --oneline -20` reads as a coherent narrative. Squash
      any "fix typo" commits that escaped.
- [ ] Repo is flipped to Public — but make this the LAST step.

## T-0 — launch day

### Posting order (Eastern time)

| Time | Action |
|---|---|
| 06:30 | Final smoke test of the live deploys (docs site, demo video). |
| 07:30 | Repo goes public on GitHub. |
| 08:00 | Hacker News "Show HN" post. Use the [draft](/launch/social/). |
| 08:05 | Tweet the announcement. Pin it. |
| 08:10 | Twitter thread. |
| 08:30 | LinkedIn post. |
| 09:00 | Discord announce — link to HN + Twitter, ask for upvotes |
| 09:00 | Reach out personally to ~10 people who might amplify (other founders, AI engineers, your investors). |
| Throughout the day | Reply to every HN comment within 15 minutes. Same on Twitter. |

### Numbers to watch

- HN: position on the front page. Goal: top 10 by 11am ET. Top 5 by 2pm.
- GitHub: stars. Goal: 200 by EOD 1.
- Twitter: impressions on the announcement tweet.
- docs.af-stack.dev: uniques + bounce rate.
- Discord: joins, active users.

### Don'ts

- Don't manipulate HN ranking (no upvote rings, no asking for
  upvotes outside Discord). The community catches this fast.
- Don't argue with skeptical commenters. Engage with their concerns
  honestly; ignore the trolls.
- Don't promise features in real time. "We'll consider that" is the
  right answer; "yes we'll add it by next month" is a trap.
- Don't push code on launch day. Anything broken can wait 24 hours.

## 48 hours after — monitor

- [ ] On-call schedule:
  - Day of: ___ (you, probably).
  - Day 1 (24h after): ___.
  - Day 2 (48h after): ___.
- [ ] Watch the GitHub issues — triage within 4h.
- [ ] Watch the security inbox — within 1h.
- [ ] Watch Discord — questions answered within 30 minutes during
      business hours.

## Rollback procedure

If something serious goes wrong (leaked secret accidentally committed
despite the audit, critical vuln found in the first 24h):

1. **Don't panic-delete the repo.** Deleting + recreating breaks every
   star, fork, and clone. The internet has copies anyway.
2. For a leaked secret: rotate the credential at the source (Stripe,
   AWS, etc.), then `git filter-repo` the commit, force-push, and
   document the incident in a public security advisory. Recognise that
   the secret is **compromised** from the moment it hit GitHub —
   rotation is mandatory.
3. For a critical vuln: write the patch, ship it, then a public
   advisory via GitHub's security advisory feature with a clear
   "upgrade to v1.0.1" recommendation. CVE if warranted.
4. The HN post stands. Add a top-level reply with the fix.

## After the launch dust settles

- [ ] Pin the launch HN thread to the GitHub repo via the topics field.
- [ ] Write a "first week in numbers" blog post (stars, downloads,
      Discord, top contributors).
- [ ] Triage the issue backlog into a v1.1 roadmap.
- [ ] Send thank-yous to the top amplifiers.
