# Dashboard Editing Contract

The dashboard is the operator console for the backend. In most forks,
you add operator-specific views through dashboard plugins instead of
editing the shell directly.

## Edit Freely

These are intended extension points:

- `plugins/<id>/plugin.ts`
- `plugins/<id>/page.tsx`
- copyable plugin examples under `examples/starter/dashboard-plugin/`
- docs for operator workflows

Run `pnpm generate:plugins` after adding or renaming a plugin.

## Customize Through `brand.yaml`

Use root `brand.yaml` plus `pnpm run generate:brand` for:

- operator-console display name
- logo
- primary/accent colors
- dashboard/customer/runtime domains

Do not edit generated `src/app/brand.css` or `src/lib/brand.ts` by hand.

## Do Not Touch Casually

These are platform shell or admin-control paths:

- `src/app/(admin)/layout.tsx`
- `src/app/api/auth/[...all]/route.ts`
- `src/app/api/v1/[...path]/route.ts`
- `src/lib/session.ts`
- `src/lib/nav.ts`
- generated plugin routes under `src/app/(admin)/plugins/*`

Change them only when the dashboard IA, auth gate, or runtime proxy
contract itself is changing. For fork-specific views, prefer a plugin.
