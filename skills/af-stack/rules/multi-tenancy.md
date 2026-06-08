# Multi-Tenancy — the RLS pattern

AF Stack multi-tenancy is enforced at the **database boundary** via
Postgres Row-Level Security (RLS), keyed on a per-request GUC the
middleware sets. **A buggy handler cannot leak across tenants — the
database refuses to read someone else's rows.**

This is load-bearing for security. Every file you write must follow it.

## The mechanism

1. **Resolver middleware** parses the API key (Authorization header) or
   session cookie, looks up the tenant in `suite_api_keys` /
   `suite_memberships`, and binds the tenant ID into the request
   context.
2. **DB connection wrapper** issues `SET LOCAL app.tenant_id = '<uuid>'`
   at the start of every transaction (Go) or via `set_config()` in
   Python.
3. **RLS policies** on every tenant-scoped table read `current_setting('app.tenant_id', true)`
   and filter accordingly.
4. **Tenant context propagation**: when calling other services (workload
   modules, agents), the resolver forwards `x-af-stack-tenant-id` and
   `x-af-stack-user-id` headers so the downstream service can re-bind.

## Reading the tenant in your code

### Go (in-runtime, workload modules or platform code)

```go
import "github.com/Agent-Field/backai/services/runtime/internal/tenantctx"

func MyHandler(w http.ResponseWriter, r *http.Request) {
    tenantID := tenantctx.TenantID(r.Context())  // guaranteed set by middleware
    userID := tenantctx.UserID(r.Context())
    // ... use tenantID + userID
}
```

### Python (sidecar workload module)

```python
from fastapi import Header, HTTPException

def _require_tenant(tenant: str | None, user: str | None) -> tuple[str, str]:
    if not tenant: raise HTTPException(401, {"code": "NO_TENANT"})
    if not user:   raise HTTPException(401, {"code": "NO_USER"})
    return tenant.strip(), user.strip()

@app.post("/items")
async def create_item(
    req: CreateRequest,
    x_af_stack_tenant_id: str | None = Header(default=None),
    x_af_stack_user_id: str | None = Header(default=None),
):
    tenant_id, user_id = _require_tenant(x_af_stack_tenant_id, x_af_stack_user_id)
    # ...
```

### TypeScript (customer-app server components)

```ts
// The session middleware binds the tenant. Server-side fetches that
// include the session cookie inherit it. You usually don't need to
// touch the tenant_id directly in customer-app code.
```

## Binding the tenant on the DB connection

### Python pattern

```python
@asynccontextmanager
async def _tenant_conn(tenant_id: str):
    async with await psycopg.AsyncConnection.connect(DATABASE_URL) as conn:
        async with conn.transaction():
            await conn.execute(
                "SELECT set_config('app.tenant_id', %s, true)", (tenant_id,)
            )
            yield conn

# Usage
async with _tenant_conn(tenant_id) as conn:
    rows = await (await conn.execute("SELECT * FROM your_table")).fetchall()
    # All queries here are RLS-scoped to tenant_id automatically.
```

### Go pattern

The runtime's pgx pool integration automatically issues `SET LOCAL
app.tenant_id` at the start of every checkout when the tenant context is
present. You just write normal queries — the pool wrapper handles it.

## The RLS policy pattern

Every tenant-scoped table you create:

```sql
CREATE TABLE your_table (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   text        NOT NULL,    -- ← required
    user_id     text        NOT NULL,    -- ← required if user-scoped
    -- ... your columns
    created_at  timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE your_table ENABLE ROW LEVEL SECURITY;

CREATE POLICY your_table_tenant_isolation ON your_table
    USING (
        current_setting('app.bypass_rls', true) = 'on'
        OR tenant_id = current_setting('app.tenant_id', true)
    )
    WITH CHECK (
        current_setting('app.bypass_rls', true) = 'on'
        OR tenant_id = current_setting('app.tenant_id', true)
    );
```

Why `true` in `current_setting('...', true)`: that's the `missing_ok`
flag — when the GUC isn't set (e.g. a bare psql session), the query
returns NULL instead of erroring. Without it, every query fails before
the runtime sets the GUC.

The `WITH CHECK` clause prevents inserts that would write a row for a
different tenant — defense in depth.

## Cross-tenant operator queries (the escape)

Operator-only flows (dashboards, audits, exports) sometimes need to read
across tenants. Use the bypass:

```python
async with _admin_conn() as conn:  # sets app.bypass_rls = 'on'
    rows = await (await conn.execute(
        "SELECT tenant_id, COUNT(*) FROM your_table GROUP BY tenant_id"
    )).fetchall()
```

**Rules for bypass**:

- Only in operator-authenticated routes (dashboard plugins, admin APIs).
- ALWAYS audit-log the access (the runtime's admin middleware fires
  audit automatically).
- Never in customer-facing routes. Never in agent reasoners.
- Keep bypass code paths small and reviewable.

## Anti-patterns — the security-critical ones

| Anti-pattern | Why it's a security bug | Correct |
|---|---|---|
| Reading `tenant_id` from request body | Attacker sends another tenant's ID | Use the header (set by runtime resolver) |
| `WHERE tenant_id = $tenant_id` in your query | RLS already filters — explicit check is redundant AND can hide RLS not being on | Just write `WHERE` clauses for your business logic; RLS handles tenant |
| Skipping `SET LOCAL app.tenant_id` for a "fast" query | RLS returns 0 rows or errors | Always bind |
| Using `app.bypass_rls` in customer-facing routes | Reads across tenants | Use only in operator routes, with audit |
| Pooled connections that don't reset GUCs between transactions | Tenant A's GUC leaks to tenant B's request | `SET LOCAL` (not `SET`) — tied to the transaction |
| Storing tenant_id in JWT and trusting it | Forgeable | Resolver looks up from the API key / session |
| RLS off because "it's annoying for dev" | One forgotten line = data leak | Use `app.bypass_rls` for dev scripts; never disable RLS |

## Common patterns

### Pattern: per-user query within the current tenant

```sql
SELECT * FROM your_table
WHERE user_id = current_setting('app.user_id', true)
ORDER BY created_at DESC;
```

(RLS already scopes to tenant; you just need the user filter.)

### Pattern: paginated list

```sql
SELECT * FROM your_table
ORDER BY created_at DESC
LIMIT $limit OFFSET $offset;
```

(RLS scopes to tenant; you handle pagination.)

### Pattern: aggregations within tenant

```sql
SELECT COUNT(*), SUM(some_metric)
FROM your_table
WHERE created_at > now() - interval '30 days';
```

### Pattern: cross-tenant aggregation (operator)

```sql
-- Inside _admin_conn:
SELECT tenant_id, COUNT(*) FROM your_table
GROUP BY tenant_id
ORDER BY COUNT(*) DESC;
```

## Testing multi-tenancy

The runtime ships `scripts/test-multi-tenancy.sh` which proves
isolation. Adapt for your tables:

1. Create two tenants A and B.
2. Insert rows for each.
3. Bind as A → read returns only A's rows.
4. Bind as B → read returns only B's rows.
5. No binding → reads return 0 rows (RLS denies).

If your tests don't follow this shape, your RLS isn't being exercised.

## When NOT to use a tenant column

Some tables are inherently global (operator-managed):

| Table | Scope | RLS? |
|---|---|---|
| `suite_tenants` | Global (operator manages) | Admin-only |
| `suite_users` | Global (user ↔ multiple tenants) | Admin-only |
| `suite_memberships` | The join table | Admin-only |
| `suite_audit_log` | Global; rows have tenant_id but query is admin | Admin-only |
| Your domain tables | Tenant-scoped | YES (the pattern above) |

If you're writing a tenant-agnostic table (config, system metadata),
don't add the `tenant_id` column. Make it admin-managed.
