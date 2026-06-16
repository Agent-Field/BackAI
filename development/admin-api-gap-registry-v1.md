# BackAI Admin API Gap Registry v1

This file records the UI contract status for every page in `development/ui-plan-v1.md`. Update it when routes or adapter capabilities change.

Legend: `backed` = endpoint exists and primary UI can be served. `derived` = primary data exists but UI computes grouping/forecast/status locally. `missing` = endpoint or adapter capability is absent. `degraded` = endpoint exists but active adapter/runtime may not expose the full data.

| Page | Status | Current Source | Gap To Record |
|---|---|---|---|
| Home | backed | `/api/v1/home/overview`, `/api/v1/metrics/summary`, `/api/v1/activity`, `/health`, `/ready`, realtime | None |
| Runs | backed | `/api/v1/runs`, run events, AgentField link, run actions | None |
| Cost | derived | `/api/v1/cost`, `/api/v1/cost/events`, budgets, cache stats | Forecast, cache savings, reasoner grouping are client-derived unless backend adds aggregates |
| Errors | derived | `/api/v1/logs?level=error,fatal` | Dedicated grouped errors endpoint missing |
| Traces | degraded | external OTel store link, no listed product endpoint | `GET /api/v1/traces` or adapter trace-browser capability missing |
| Queue | backed | `/api/v1/queues/summary`, `/api/v1/jobs`, `/api/v1/jobs/definitions`, retry/enqueue | None |
| Cache | backed | `/api/v1/llm/cache/stats` | Flush actions only render if backend adds flush endpoints |
| Sandbox runs | backed | `/api/v1/sandbox/runs`, detail, logs, delete | None |
| Webhook deliveries | backed | `/api/v1/webhooks/deliveries`, detail, retry | None |
| Notification deliveries | backed | `/api/v1/notifications`, detail, stats, send | Mute future-notification action needs policy endpoint if shipped |
| Approvals | backed | `/api/v1/approvals`, detail, decide | None |
| Activity | backed | `/api/v1/activity` | CSV export can be client-side until backend export exists |
| Health | derived | `/health`, `/ready`, `/api/v1/metrics/summary`, service health links | Deep DB/cert/worker stats endpoint missing |
| Logs | backed | `/api/v1/logs` | WebSocket/SSE tail capability is optional; use refresh if absent |
| Agents | backed | `/api/v1/agents`, invoke routes, realtime runs | None |
| Reasoners | derived | derived from `/api/v1/agents` | Cross-agent latency/cost analytics deferred |
| Tools | derived | `/api/v1/tools/native`, `/api/v1/tools/adapters`, `/api/v1/mcp/tools` | Usage analytics deferred |
| Skills | backed | `/api/v1/skills`, MCP server/tool routes | None |
| Harnesses | backed | `/api/v1/harnesses`, provider probe | None |
| Crons | backed | `/api/v1/crons` | Manual trigger endpoint not in snapshot |
| Sandboxes playground | backed | `/api/v1/sandbox/run`, pool, logs, cancel | None |
| Modules | backed | `/api/v1/modules` | Enable/disable writes config outside runtime API in v1 |
| Data Tables | backed | DB table/detail/rows routes | None |
| Data SQL | backed | `/api/v1/db/sql` | Saved snippets/history are local UI until backend storage exists |
| Data Memory | backed | memory list/get/search/put/delete | None |
| Data Storage | backed | storage list/get/signed-url/upload/delete | None |
| Data Search | backed | search query and document upsert/delete | Index stats endpoint missing |
| Feature flags | backed | `/api/v1/config/flags` | Tenant override history endpoint missing |
| API Explorer | backed | `/openapi.json` | Type generation downloads are UI-side unless generator endpoint lands |
| Shipwright | backed-conditional | `/api/v1/shipwright/tasks` | Omit from nav unless v1 product scope enables it |
| Tenants | backed | admin tenant list/detail/drilldown/create/update/delete | None |
| API keys | backed | admin keys list/issue/spend/revoke | Rotate is represented as revoke + issue unless backend rotate lands |
| Members | backed | admin users, memberships, export, erase | Invite/disable account endpoints may need auth adapter support |
| Sessions | degraded | better-auth DB/session data and auth logs | Adapter session-enumeration capability missing |
| Budgets | backed | admin budgets list/get/set | Delete budget can be represented by set-to-empty only if backend supports it |
| Audit log | backed | `/api/v1/admin/audit` | Export can be client-side until backend export exists |
| OAuth connections | backed | `/api/v1/oauth/connections`, providers, authorize, delete, token | Refresh history endpoint missing |
| Billing summary | backed | billing customers/meters/portal | Churn signals are derived until backend flags them |
| Setup Adapters | degraded | `/api/v1/plugins`, service health, env, tool adapters | Universal `GET /api/v1/admin/adapters` capability declaration missing |
| Auth providers | degraded | better-auth config, auth protocol docs | Runtime adapter capability endpoint pending |
| LLM providers | degraded | `/api/v1/llm/models`, LiteLLM link-out | Adapter-aware gateway capabilities pending |
| Sandbox adapter | backed | `/api/v1/sandbox/pool` | Runtime adapter switch is env-only |
| Webhook subscribers | backed | webhook endpoint routes, send test | Full event catalog may live in Svix link-out |
| Notification channels | degraded | env display plus send test | Channel CRUD endpoint missing |
| Secrets | backed | secrets list/get/put/reveal/rotate/delete | None |
| Observability | derived | metrics endpoint and env config | Runtime config writes are env-only |
| Billing adapter | derived | billing customer/meter routes and env config | Runtime adapter switch is env-only |
| Deploy targets | missing | env/config conventions and provider link-outs | No runtime provisioning/status endpoint in v1 |
| Brand | missing | `brand.yaml` file read | `GET /api/v1/admin/brand` not present |
