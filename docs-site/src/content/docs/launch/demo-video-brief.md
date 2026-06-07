---
title: Demo video recording brief
description: One-page brief for the launch demo screencast.
sidebar:
  order: 3
---

*A brief, not a script. Adapt as you record.*

## Goal

A 5 — 8 minute screencast that takes a viewer from "what is AF Stack"
to "I want to clone it and try it." Shipped on the launch day.

## Audience

Indie hackers and small AI-product teams who are about to roll their
own. They've used Supabase or Firebase; they understand the difference
between rolling your own auth and using a BaaS. They have not heard of
AF Stack.

## Beats (with rough timing)

**0:00 — 0:30 / The premise.**
Open with the dashboard cost tab, real numbers, a sparkline. Voiceover:
"This is what your AI app's backend looks like if you don't roll it
yourself."

**0:30 — 1:30 / The boot.**
Terminal, side by side with the browser. `git clone …`, `cp .env.example .env`,
edit one line (the OpenRouter key), `docker compose up`. Open
`localhost:33000`. Sign in. Show the empty dashboard.

**1:30 — 3:00 / The hello world.**
Open a Python file. Three lines: import OpenAI, point at the gateway,
chat completion. Run it. Switch back to the dashboard. Click Cost.
The call is there. Click into the event. Show tokens + USD + tenant.

**3:00 — 4:30 / Multi-tenancy.**
Switch on multi-tenancy. Create a tenant. Issue an API key. Make a
call as that tenant. Show the per-tenant drilldown — usage charts,
member list, recent runs. Emphasise that the database is enforcing
isolation, not the handler.

**4:30 — 6:00 / A real example.**
Switch to Notable (Example 01). Quick tour: 3 AF agents, a custom
dashboard plugin, billing meter ticking on note creation. Don't show
every detail — show that the platform composes.

**6:00 — 7:00 / Production shape.**
Cycle through: Operate → Sandbox Activity (real runs), Operate →
Metrics (real numbers), Operate → Webhook Activity (real deliveries).
Brief mention of the Helm chart + Fly button.

**7:00 — 8:00 / The ask.**
GitHub link. Apache 2.0. Six examples in the repo. "Clone it, try it,
tell us what's missing." End on the cost tab with the sparkline still
visible.

## Production notes

- **Resolution:** record at 1440p (or 4K if you have it), export to
  1080p. Sharper than the dashboard's native ratio.
- **Dark mode** for everything. Brand-consistent.
- **No mouse highlights / Magnify** unless you're showing something
  small (status badges, KPI numbers).
- **Voiceover, not on-screen text.** Don't pause on screens silently.
- **No music**, or extremely subtle music. The dashboard does the talking.
- **Code blocks:** use a 16+ point font. The viewer should be able to
  read the OpenRouter key change without squinting.

## Equipment

- Screen recording: ScreenStudio (macOS) or OBS.
- Mic: USB condenser (Blue Yeti / Shure MV7) into Logic / Audacity.
- Editing: ScreenStudio handles cuts + zooms; final pass in Resolve
  or Final Cut.

## After recording

- YouTube (unlisted), set thumbnail (the cost dashboard with sparkline).
- Embed in the homepage hero on docs.af-stack.dev (replace the static
  screenshot).
- Embed in the GitHub README under the hero block.
- Include the link in the HN post and the Twitter thread.

## What this video is NOT

- Not a deep architecture walkthrough. Save that for a follow-up.
- Not a tutorial. The quickstart doc handles that.
- Not selling a SaaS. AF Stack is open source; the video should make
  someone want to clone it, not buy it.
