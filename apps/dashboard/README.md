This is a [Next.js](https://nextjs.org) project bootstrapped with [`create-next-app`](https://nextjs.org/docs/app/api-reference/cli/create-next-app).

## Getting Started

First, run the development server:

```bash
npm run dev
# or
yarn dev
# or
pnpm dev
# or
bun dev
```

Open [http://localhost:3000](http://localhost:3000) with your browser to see the result.

You can start editing the page by modifying `app/page.tsx`. The page auto-updates as you edit the file.

This project uses [`next/font`](https://nextjs.org/docs/app/building-your-application/optimizing/fonts) to automatically optimize and load [Geist](https://vercel.com/font), a new font family for Vercel.

## Learn More

To learn more about Next.js, take a look at the following resources:

- [Next.js Documentation](https://nextjs.org/docs) - learn about Next.js features and API.
- [Learn Next.js](https://nextjs.org/learn) - an interactive Next.js tutorial.

You can check out [the Next.js GitHub repository](https://github.com/vercel/next.js) - your feedback and contributions are welcome!

## Deploy on Vercel

The easiest way to deploy your Next.js app is to use the [Vercel Platform](https://vercel.com/new?utm_medium=default-template&filter=next.js&utm_source=create-next-app&utm_campaign=create-next-app-readme) from the creators of Next.js.

Check out our [Next.js deployment documentation](https://nextjs.org/docs/app/building-your-application/deploying) for more details.

## Add a plugin

A plugin is a contributor-owned folder under `apps/dashboard/plugins/<id>/`.
The shell auto-discovers it — no edits to `nav.ts`, the sidebar, or the
command palette.

A plugin is two files:

```
apps/dashboard/plugins/<id>/
├── plugin.ts   # manifest — default-exports definePlugin({...})
└── page.tsx    # page — default-exports a React component (server or client)
```

Example (`apps/dashboard/plugins/hello/plugin.ts`):

```ts
import { Sparkles } from "lucide-react"
import { definePlugin } from "@/lib/plugins"

export default definePlugin({
  id: "hello",
  label: "Hello",
  icon: Sparkles,
  description: "Example plugin",
  group: "system", // build | operate | customers | system
})
```

The next `pnpm dev` or `pnpm build` runs
`scripts/generate-plugins-manifest.mjs`, which:

1. Regenerates `src/lib/plugins.generated.ts` (the manifest `loadPlugins()` reads).
2. Writes a route proxy at `src/app/(admin)/plugins/<id>/page.tsx` so the page
   inherits the dashboard chrome (sidebar, topbar, auth gate).

Both generated paths are gitignored. The plugin appears in the sidebar group
chosen by `group`, in ⌘K, and on the Settings → Plugins tab.

To regenerate manually:

```bash
pnpm generate:plugins
```
