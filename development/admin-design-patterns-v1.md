# BackAI Admin Design Patterns v1

This is the implementation contract for the new BackAI admin at `/`. The product source of truth remains `development/ui-plan-v1.md`; the grooming zip is aesthetic reference only.

## Design Read

Dense developer-operator console for people running their own BackAI fork. The language is monochrome, quiet, utilitarian, and fast. It should feel closer to a debugger and database console than a marketing dashboard.

## Shell

- Left sidebar, 206px expanded and 56px rail. Groups match `ui-plan-v1.md`: Overview, Operate, Build, Customers, Setup, pinned Brand.
- Top bar is 48px: wordmark, tenant scope, breadcrumbs, command palette, alerts, theme, profile.
- Tenant scope is a lens. The sidebar never changes with tenant scope.
- Brand is pinned. Old admin remains reachable only as archival `/old`.
- No Develop group in the new admin. API Explorer lives under Build.

## Grid And Page Grammar

- Page container: max 1480px, 24px desktop padding, 16px mobile padding, 12-16px region gaps.
- Use a 12-column mental grid. Layouts can be asymmetric, but they must collapse explicitly below `md`.
- Every page has four ordered regions: title row, single-row controls, primary canvas, secondary or detail region.
- Title row contains page title, live or state badge, adapter pill when relevant, and one primary action.
- Control bar contains scope, time, filters, group-by, search, or density controls. Keep it one row on desktop.
- Primary canvas changes per page archetype. Do not reuse a generic table-plus-sidebar for everything.
- Secondary region is for detail, drilldown, history, related objects, or capability notes.

## Page Archetypes

| Archetype | Use For | Layout Rule |
|---|---|---|
| Command center | Home | KPI strip, activity stream, service posture, quick actions |
| Split-pane debugger | Runs, Errors, Traces, Queue | Dense list/table left, selected detail/timeline right |
| Analytics workspace | Cost, Cache, Billing | Controls, chart/series, ranked side rail, budget/status context |
| Delivery inbox | Webhooks, Notification deliveries | Delivery list, payload/provider response drawer, retry affordances |
| Approval queue | Approvals | Decision list, blocked-object context, approve/deny/cancel action strip |
| Timeline | Activity, Audit | Chronological feed with actor/resource filters and JSON detail |
| Topology grid | Health, Setup adapters | Service/capability matrix with native-admin link-outs |
| Log console | Logs, Sandbox runs | Virtual-looking mono stream, tail/pause controls, structured expansion |
| Registry/detail | Agents, Reasoners, Tools, Skills, Harnesses, Crons, Modules, Feature flags | Catalog/list with selected entity detail and action drawer |
| Workbench | Sandboxes, SQL, Search, API Explorer | Input/editor panel, run controls, results panel |
| Data explorer | Tables, Memory, Storage | Left object browser, right inspector with tabs/results |
| Customer drilldown | Tenants, API keys, Members, Sessions, Budgets, OAuth | Operational table plus security/usage/detail side rail |
| Config inventory | Setup pages | Read-only or drawer-mutated config rows with capability caveats |
| Read-only file display | Brand | Token/asset summary and file-edit guidance |

## Anti-Duplication Rule

A page fails review if it only swaps table columns into the same generic layout. Each page must name its archetype and use the layout shape that fits its purpose.

## State Grammar

- Loading: skeletons match final dimensions. No generic spinners where the final layout is known.
- Empty: no data exists. Provide the smallest concrete action to create or observe data.
- Filtered empty: data may exist, but filters hide it. Offer clear-filter action.
- Missing: current backend or adapter does not expose the capability. Record the exact missing endpoint or capability.
- Degraded: adapter is unhealthy or stale. Show last-known data and the last checked time.
- Error: API failed or schema mismatched. Show endpoint context and a retry affordance.
- Disabled: explain why the control is unavailable. Do not hide important unavailable actions.

## Motion And Interaction Tokens

- Hover/focus/active states are shared by shadcn variants and app primitives. No one-off hover classes for a page.
- Fast transitions: 120ms. Standard: 150ms. Sheets: 200ms. Use transform and opacity only.
- Active press: subtle `scale(0.98)` or foreground/border change. No bouncing.
- Live update: number tick or small timestamp refresh. No flashing.
- Skeleton shimmer is allowed only under reduced-motion-safe CSS.
- Respect `prefers-reduced-motion`.

## Tables And Lists

- Dense row height: 32-36px. Comfortable row height: 40px.
- Tables that can grow must include bulk selection, sticky header or persistent controls, empty filtered state, keyboard focus, pagination or virtualization strategy, and detail drawer.
- Bulk actions are shown only when rows are selected.
- Comparable numeric data uses mono, tabular alignment, and right alignment.
- Long text truncates with hover/detail access, not overflow.

## Logs And Streams

- Logs use mono text, severity filtering, service/tenant/time filters, tail mode, pause/resume, copy/export, and structured field expansion.
- Keep the DOM bounded. Use pagination or virtualization for production-sized streams.
- Live pages show connection status and last updated time. Refresh is present only when manual reconciliation matters.

## Component Rules

- Use stock shadcn primitives first: Sheet, Table, Tabs, Command, Sidebar, Skeleton, Empty, Chart, Field, Tooltip, Sonner, Badge, Button, Select, Input, Dialog, Alert, Progress.
- Add only official shadcn primitives when missing.
- Custom components are composition primitives only: page header, filter bar, KPI strip, adapter pill, live badge, status state, data table shell, bulk action bar, detail drawer, log stream, timeline, schema form, code block, metric card.
- Cards are for real entities, repeated items, or framed tools. Page sections are not card soup.
- Forms use label above input, helper text, error text, and "Show as code" in drawers.
- Destructive actions use Alert Dialog.

## Icons

- Use one icon family in the admin app. This codebase already uses `lucide-react`, so continue with lucide for consistency.
- Icon size: 16px normal, 14px compact, 20px hero/action only.
- Stroke width stays default unless the family is globally changed.
- Icon-only controls require tooltips and screen-reader text.
- Icons are semantic. Do not add decorative dots unless they represent real status.

## API Truth

- Every route is marked as `backed`, `derived`, `missing`, or `degraded`.
- Missing or partial pages must record the exact missing endpoint or adapter capability.
- Derived data is labeled in the implementation contract and never presented as backend truth.
- No fake-precise numbers. Seeded fallback is permitted only when the runtime is unreachable and the page clearly says seeded.

## Screenshot Gate

Final review requires screenshots for every left-nav page. Review each screenshot for: route correctness, page-specific layout, visible state hierarchy, text fit, hover/focus consistency where exercised, empty/missing/degraded truth, and no duplicated mock canvas.
