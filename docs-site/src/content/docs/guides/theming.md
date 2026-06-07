---
title: Theming
description: Reskin every dashboard page with one CSS file. No fork, no provider swap.
sidebar:
  order: 3
---

The AF Stack dashboard is themed entirely through CSS custom properties.
Override the variables in a single file and you can reskin every page —
sidebar, buttons, cards, badges, charts — without forking the codebase.

## Where the variables live

All design tokens are defined in
`apps/dashboard/src/app/globals.css` under two scopes:

- `:root` — the light-mode defaults.
- `.dark` — the dark-mode overrides (the dashboard ships with dark
  selected by default; toggle with the moon icon in the top right).

The variables use [`oklch`](https://oklch.com/) so palette shifts stay
perceptually uniform across the spectrum.

## The tokens

The dashboard's shadcn surface reads these variables. Override any
subset; everything you don't touch falls back to the defaults.

### Surface

| Variable | Meaning |
|---|---|
| `--background` | Page background. |
| `--foreground` | Default body text. |
| `--card` / `--card-foreground` | Card surface + text on cards. |
| `--popover` / `--popover-foreground` | Dropdowns, dialogs. |
| `--muted` / `--muted-foreground` | Subtle background + helper text. |
| `--border` | Default border colour. |
| `--input` | Input field border. |
| `--ring` | Focus ring. |
| `--radius` | Border radius (default `0.625rem`). |

### Brand

| Variable | Meaning |
|---|---|
| `--primary` / `--primary-foreground` | Primary buttons, active sidebar item. |
| `--accent` / `--accent-foreground` | Hover state on muted controls. |
| `--secondary` / `--secondary-foreground` | Secondary buttons, badge backgrounds. |
| `--destructive` | Delete buttons, error badges. |

### Sidebar (an isolated palette so the chrome reads as a separate surface)

| Variable | Meaning |
|---|---|
| `--sidebar` | Sidebar background. |
| `--sidebar-foreground` | Sidebar text. |
| `--sidebar-primary` | Active nav item background. |
| `--sidebar-accent` | Hover state on nav items. |
| `--sidebar-border` | Vertical rule between sidebar and main. |

### Charts (five-step categorical palette)

| Variable | Used by |
|---|---|
| `--chart-1` to `--chart-5` | Cost area chart, sparklines, top-N breakdowns. |

## Reskinning the dashboard

The supported pattern is to drop a file at
`apps/dashboard/src/app/brand.css` that overrides the variables you
want changed, and import it from `apps/dashboard/src/app/layout.tsx`
**after** `globals.css`.

```css
/* apps/dashboard/src/app/brand.css */

:root {
  --primary: oklch(0.55 0.21 145); /* a confident emerald */
  --primary-foreground: oklch(0.985 0 0);
  --accent: oklch(0.96 0.04 145);
  --sidebar-primary: oklch(0.45 0.21 145);
  --chart-1: oklch(0.65 0.21 145);
  --chart-2: oklch(0.55 0.21 165);
  --chart-3: oklch(0.45 0.21 185);
  --radius: 0.5rem;
}

.dark {
  --primary: oklch(0.78 0.18 145);
  --primary-foreground: oklch(0.18 0 0);
  --accent: oklch(0.32 0.08 145);
  --sidebar-primary: oklch(0.66 0.18 145);
}
```

```tsx
// apps/dashboard/src/app/layout.tsx
import "./globals.css"
import "./brand.css" // <-- add this line
```

That's it. No props to wire, no theme provider to swap. Plugins and
the rest of the dashboard inherit the new tokens automatically because
every shadcn component reads from the same CSS variable surface.

## What NOT to override

- Don't change the variable names — the shadcn primitives reference
  them directly. Only the values.
- Don't use Tailwind utility colours (`bg-blue-500`, `text-emerald-600`)
  in custom components. Use the semantic tokens (`bg-primary`,
  `text-muted-foreground`) so reskinning sweeps your code too.
- Don't add hardcoded `dark:` colour overrides — the dark scope at
  `.dark` already swaps the values for you.

## Logo and favicon

The wordmark in the sidebar lives at
`apps/dashboard/src/app/(admin)/_layout/sidebar.tsx`. Replace the
markup or set the `--af-brand-logo` data URL in your `brand.css` if you
want to centralise the swap.

Favicons live under `apps/dashboard/public/`. Replace `favicon.ico`,
`icon.png`, and `apple-icon.png` in place — Next.js picks them up
automatically.

## Verifying

```bash
docker compose build dashboard
docker compose up -d dashboard
open http://localhost:33000
```

Reload, toggle dark mode, click through the sidebar, open a Dialog, and
make sure your palette reads cleanly in both modes. The Cost page is the
most chart-heavy view — that's where chart palette mistakes show first.
