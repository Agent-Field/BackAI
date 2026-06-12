# Customer App Editing Contract

The customer app is the product surface in a BackAI fork. Treat it as
mostly yours, with a few platform-owned edges.

## Edit Freely

These are the normal product areas:

- `src/app/(app)/*` pages and nested routes
- `src/components/*` product components
- `src/lib/api.ts` client helpers for customer-visible runtime calls
- sidebar links in `src/components/layout/customer-sidebar.tsx`

Start from `examples/starter/customer-app/first-action/page.tsx` when
adding the first logged-in workflow.

## Customize Through `brand.yaml`

Use root `brand.yaml` plus `pnpm run generate:brand` for:

- product name
- sidebar/auth-shell logo
- primary/accent colors
- dashboard/customer/runtime domains

Do not edit generated `src/app/brand.css` or `src/lib/brand.ts` by hand.

## Do Not Touch Casually

These are platform wiring, not product pages:

- `src/app/api/auth/[...all]/route.ts`
- `src/app/api/v1/[...path]/route.ts`
- `src/app/api/customer/*`
- `src/lib/auth.ts`
- `src/lib/session.ts`
- `src/lib/provisioning.ts`

Change these only when you are intentionally changing auth,
tenant-provisioning, or runtime-proxy behavior. Ordinary product work
should call the runtime through the existing proxy.
