# Customer App — the branded SaaS

Where: `apps/customer-app/`

This is **the product the customer sees**. The user's job is to brand it
into their SaaS (DocuChat, Forge, Mercer, whatever). It's a Next.js App
Router app with better-auth wired, sign-up flow that auto-provisions
tenant + membership + API key, and shadcn/ui throughout.

## The directory structure

```
apps/customer-app/src/
├── app/
│   ├── (auth)/             # ← DON'T edit — sign-in, sign-up, sign-out
│   │   ├── sign-in/
│   │   ├── sign-up/
│   │   ├── sign-out/
│   │   └── layout.tsx
│   ├── api/                # ← MOSTLY don't edit — better-auth handler,
│   │   ├── auth/[...all]/  #   onboarding key, and the runtime proxy
│   │   ├── customer/onboarding-key/
│   │   └── v1/[...path]/   # ← proxy to the runtime (/api/v1/...)
│   ├── dashboard/          # ← the one shipped customer page; copy it
│   │   └── page.tsx
│   ├── <your-route>/       # ← EDIT FREELY — add your pages here
│   ├── brand.css           # ← generated from brand.yaml; don't hand-edit
│   ├── favicon.ico         # ← your favicon lives HERE (no public/ dir)
│   ├── globals.css
│   ├── layout.tsx          # ← root layout; brand-only edits
│   └── page.tsx            # ← landing page; edit freely
├── components/
│   ├── ui/                 # ← DON'T edit — shadcn primitives
│   ├── app-sidebar.tsx     # ← the sidebar; nav is an inline items array
│   ├── nav-*.tsx           # ← sidebar sections
│   └── ... your components
├── hooks/
├── lib/                    # api, auth, auth-client, brand (generated),
│   └── ...                 # db, provisioning, session, sso, utils
└── middleware.ts           # ← the auth gate
```

There is **no `(app)/` route group**, no `components/layout/`, and no
`public/` directory. Routes are plain folders under `src/app/`; creating
`src/app/(app)/dashboard/page.tsx` alongside the real `src/app/dashboard/`
is a hard Next.js duplicate-route build failure.

## Edit zones — the contract

| Zone | Edit policy | What lives there |
|---|---|---|
| `(auth)/` | **Don't edit** | Pre-wired better-auth flows (sign-up auto-provisions tenant + membership + API key) |
| `app/<route>/` | **Edit freely** | Your customer-facing pages |
| `api/` | **Mostly don't edit** | Proxy routes to the runtime; better-auth handlers |
| `components/ui/` | **Don't edit** | shadcn primitives — extend if needed, don't modify |
| `components/app-sidebar.tsx` | **Brand only** | Sidebar shell + the inline nav `items` array |
| `app/layout.tsx` | **Brand only** | Root layout; brand colors / fonts |
| `app/page.tsx` | **Edit freely** | Landing page |

When you add a page, drop it at `src/app/<your-route>/page.tsx`. Gating is
**deny-by-default**: `src/middleware.ts` matches every route and redirects
to `/sign-in` unless the path starts with one of the `PUBLIC_PREFIXES`
(`/sign-in`, `/sign-up`, `/api/`, `/_next`, `/favicon`) — so a new page is
signed-in-only automatically, without belonging to any route group. In
personal mode (`AF_STACK_MODE=personal`) the gate is skipped entirely.

## What's pre-wired (don't reinvent)

### Auth

- Email + password: works.
- OAuth providers: set `GOOGLE_CLIENT_ID` / `GITHUB_CLIENT_ID` / etc. in
  `.env`; better-auth auto-detects.
- Magic links: works if email adapter is configured.

On every sign-up, a database hook (`apps/dashboard/src/lib/auth.ts`'s
`databaseHooks.user.create.after`) mirrors the user into `suite_users`,
creates a `suite_memberships` row in the default tenant, and issues an
API key. Don't reimplement this.

### Tenant + RLS

The customer's tenant is bound to their session. Every request to
`/api/v1/...` carries the tenant header automatically; RLS enforces.
Don't pass tenant IDs around.

### Layout shell

There is no shared app shell layout — each page composes its own by
mounting `<SidebarProvider>` + `<AppSidebar>`. Copy
`src/app/dashboard/page.tsx` as the pattern. You're free to edit the brand
bits (logo, name, color) in `components/app-sidebar.tsx`; leave the auth +
session machinery alone.

### Brand theming

CSS variables in `apps/customer-app/src/app/brand.css` — generated from
`brand.yaml` by `scripts/generate-brand.mjs`, so edit `brand.yaml`, not the
CSS. Every shadcn primitive, every chart, every page inherits the palette.

Don't hardcode hex colors. Use Tailwind tokens (`bg-primary`,
`text-foreground`, `border-border`, `text-muted-foreground`).

### Billing and API keys — NOT shipped as customer pages

There is no customer-facing billing page and no customer-facing API-key
page. The **operator dashboard** owns both today
(`apps/dashboard/src/app/(dashboard)/platform/billing` and
`.../people/keys`).

To add a customer-facing one, create
`apps/customer-app/src/app/billing/page.tsx` and call the runtime through
the app's own proxy route (`src/app/api/v1/[...path]/route.ts`) — the
`@af-stack/sdk` package is **not** a dependency of `apps/customer-app`, so
don't `import { suite } from "@af-stack/sdk"` here.

## How to use suite.* from customer pages

Pages are React Server Components by default. Server-side fetches inherit
the customer's session cookie.

```tsx
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card"

export default async function YourPage() {
  // Fetch your workload module routes through the proxy.
  const data = await fetch(
    `${process.env.NEXT_PUBLIC_RUNTIME_URL}/workload/<your-id>/items`,
    { cache: "no-store", credentials: "include" },
  ).then(r => r.json())

  return (
    <div className="container mx-auto p-6">
      <h1>Your Items</h1>
      {/* ... */}
    </div>
  )
}
```

For client-side mutations (form submits, button clicks), use a `"use
client"` component that calls a route handler, which proxies to the
runtime.

## Brand bits — the minimum brand pass

To take a fresh fork from BackAI to your product:

1. **`brand.yaml`** — the source of truth for app name, colors, and logo.
   The product name reaches components through the generated
   `src/lib/brand.ts` (`brand.displayName`); don't hardcode it.
2. **`apps/customer-app/src/app/page.tsx`** — landing page copy.
3. **`apps/customer-app/src/components/app-sidebar.tsx`** — the sidebar
   nav: the inline `items` array passed to `<NavMain>`.
4. **`apps/customer-app/src/app/favicon.ico`** — your favicon (there is no
   `public/` directory).
5. **`apps/customer-app/src/app/dashboard/page.tsx`** — the first thing
   customers see after sign-in.

`af-stack init --name "<Name>" --color "<#hex>"` does step 1 for you (and
`--logo <path>` sets the light+dark mark in `brand.yaml`); regenerate the
derived files with `pnpm run generate:brand`.

## Adding a page — concrete walkthrough

The user wants `/items` as a customer-facing list:

1. Create `apps/customer-app/src/app/items/page.tsx`. Copy from
   `snippets/customer-app-page.tsx`.
2. Edit the title, description, and `fetchItems()` to point at your
   workload module's `/items` route.
3. Add `Items` to the inline `items` array in
   `apps/customer-app/src/components/app-sidebar.tsx`.
4. Test: `docker compose up`, sign up, click "Items" in the sidebar.

## What you should NOT do in the customer-app

| Don't | Why | Do instead |
|---|---|---|
| Call an LLM provider directly | Bypasses gateway, no cost on the customer's tenant | Route through your workload module → agent or `suite.llm` |
| Read the DB directly | Customer app shouldn't know your schema | Route through your workload module |
| Add a backend route in `app/api/...` for business logic | API routes here are proxies, not domain logic | Add to your workload module |
| Modify `(auth)/` | Breaks the sign-up auto-provisioning | Override theme via brand only |
| Create `src/app/(app)/...` | The route group doesn't exist; duplicates a real route and fails the Next.js build | Plain folder: `src/app/<route>/page.tsx` |
| Use hex colors | Breaks dark mode + theming | Tailwind tokens |
| Pass tenant_id around manually | RLS handles it | Trust the session |
| Build a chat history UI without using AgentField Session memory | Drift from the platform | Call your agent which uses Session-scope memory |

## Customer-app vs dashboard

| Question | Customer App | Dashboard |
|---|---|---|
| Who logs in? | Your end users (customers) | Operators (you / your support team) |
| Multi-tenant? | One user = one tenant scope | Cross-tenant operator view |
| Editable? | Brand + add pages | Mostly platform; plugins extend |
| Brand | Product name | "BackAI" (or your brand if you're white-labeling for resellers) |
| Visible to | Public (after auth) | Operators only |

Don't put operator-only features in customer-app or vice versa.

## Sign-up auto-provisioning details

When a customer signs up:

1. better-auth creates a `user` row.
2. Database hook fires: mirrors to `suite_users`, creates `suite_memberships`
   row in default tenant, issues `suite_api_keys` row.
3. The customer can immediately access tenant-scoped routes via their
   session cookie.

This means every customer is a "first-class" tenant from the moment they
sign up. The customer-app can call `/workload/<id>/...` and the runtime
resolves their tenant from the session.

If you want a different provisioning flow (e.g. invite-only), edit
`apps/dashboard/src/lib/auth.ts` `databaseHooks.user.create.after` —
that's the right surface to customize.
