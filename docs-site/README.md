# AF Stack docs

The canonical documentation site for AF Stack, built with
[Astro Starlight](https://starlight.astro.build/). Source-of-truth lives
in `src/content/docs/`. Static output is built to `dist/` and deployed
independently from the runtime / dashboard.

## Local dev

Requires Node 20+ and npm.

```bash
cd docs-site
npm install
npm run dev        # http://localhost:4321
```

## Build

```bash
npm run build      # writes static site to dist/
npm run preview    # serve dist/ at http://localhost:4321
```

## Where things live

| Path | Purpose |
|---|---|
| `src/content/docs/` | All markdown / MDX pages. One file per route. |
| `src/content/docs/index.mdx` | Homepage. Hero, feature grid, code blocks. |
| `src/styles/custom.css` | Light theme overrides on top of the Starlight defaults. |
| `astro.config.mjs` | Site metadata, sidebar tree, GitHub edit links. |
| `public/` | Static assets served from `/`. |
| `scripts/` | Build-time helpers (OpenAPI fetch, code-sample inject). |

## Deployment

The site is committed to the repo. CI builds it on every push (see
`.github/workflows/ci.yml`) so docs regressions are caught early.

Production hosting target is `docs.af-stack.dev` — point any static host
(Netlify, Cloudflare Pages, Vercel, S3 + CloudFront) at `dist/` after a
build.

## Search

Starlight ships with [Pagefind](https://pagefind.app/) baked in.
Building the site generates the search index automatically; no separate
search service is required.
