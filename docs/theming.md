# Theming and Branding

AF Stack branding starts in root [`brand.yaml`](../brand.yaml). For a
new fork, prefer the CLI:

```bash
af-stack init --name "DocuChat" --color "#0A66C2" --logo ./logo.png
```

That writes `brand.yaml`, copies the logo into the generated app public
paths, and runs `pnpm run generate:brand`. If you edit `brand.yaml`
manually later, run `pnpm run generate:brand` again.

## Where the variables live

Generated brand tokens live in:

- `apps/dashboard/src/app/brand.css`
- `apps/customer-app/src/app/brand.css`

The app-level design tokens are defined in each app's `globals.css`
under two scopes:

- `:root` — the light-mode defaults.
- `.dark` — the dark-mode overrides (the dashboard ships with dark
  selected by default; toggle with the moon icon in the top right).

The variables use [`oklch`](https://oklch.com/) so palette shifts stay
perceptually uniform across the spectrum.

## The tokens

The dashboard's shadcn surface reads these variables. Override any
subset; everything you don't touch falls back to the defaults.

### Surface

| Variable                             | Meaning                             |
| ------------------------------------ | ----------------------------------- |
| `--background`                       | Page background.                    |
| `--foreground`                       | Default body text.                  |
| `--card` / `--card-foreground`       | Card surface + text on cards.       |
| `--popover` / `--popover-foreground` | Dropdowns, dialogs.                 |
| `--muted` / `--muted-foreground`     | Subtle background + helper text.    |
| `--border`                           | Default border colour.              |
| `--input`                            | Input field border.                 |
| `--ring`                             | Focus ring.                         |
| `--radius`                           | Border radius (default `0.625rem`). |

### Brand

| Variable                                 | Meaning                               |
| ---------------------------------------- | ------------------------------------- |
| `--primary` / `--primary-foreground`     | Primary buttons, active sidebar item. |
| `--accent` / `--accent-foreground`       | Hover state on muted controls.        |
| `--secondary` / `--secondary-foreground` | Secondary buttons, badge backgrounds. |
| `--destructive`                          | Delete buttons, error badges.         |

### Sidebar (an isolated palette so the chrome reads as a separate surface)

| Variable               | Meaning                                 |
| ---------------------- | --------------------------------------- |
| `--sidebar`            | Sidebar background.                     |
| `--sidebar-foreground` | Sidebar text.                           |
| `--sidebar-primary`    | Active nav item background.             |
| `--sidebar-accent`     | Hover state on nav items.               |
| `--sidebar-border`     | Vertical rule between sidebar and main. |

### Charts (five-step categorical palette)

| Variable                   | Used by                                        |
| -------------------------- | ---------------------------------------------- |
| `--chart-1` to `--chart-5` | Cost area chart, sparklines, top-N breakdowns. |

## Reskinning the apps

The supported pattern is to edit `brand.yaml`, not the generated files.
The generator writes `apps/dashboard/src/app/brand.css` and
`apps/customer-app/src/app/brand.css` from the same config.

```yaml
# brand.yaml

name: docuchat
codename: docuchat
display_name: DocuChat
short_description: AI-powered document Q&A.

palette:
  primary: "#0A66C2"
  accent: "#16A34A"
  dark_mode: true
```

```bash
pnpm run generate:brand
```

That's it. No props to wire, no theme provider to swap. Plugins, the
dashboard, and the customer app inherit the new tokens automatically
because every shadcn component reads from the same CSS variable surface.

## What NOT to override

- Don't change the variable names — the shadcn primitives reference
  them directly. Only the values.
- Don't use Tailwind utility colours (`bg-blue-500`, `text-emerald-600`)
  in custom components. Use the semantic tokens (`bg-primary`,
  `text-muted-foreground`) so reskinning sweeps your code too.
- Don't add hardcoded `dark:` colour overrides — the dark scope at
  `.dark` already swaps the values for you.

## Logo and favicon

Set logo paths in `brand.yaml`, or pass `--logo` to `af-stack init`.
`scripts/generate-brand.mjs` copies configured logos into:

- `apps/dashboard/public/brand/`
- `apps/customer-app/public/brand/`

The generated `brand.ts` modules expose the final public paths to each
layout shell. Favicons still live under each app's `public/` directory;
replace them in place when you need browser-tab assets.

## Verifying

```bash
docker compose build dashboard
docker compose up -d dashboard
open http://localhost:33000
```

Reload, toggle dark mode, click through the sidebar, open a Dialog, and
make sure your palette reads cleanly in both modes. The Cost page is the
most chart-heavy view — that's where chart palette mistakes show first.
