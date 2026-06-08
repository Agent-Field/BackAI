-- Template: Workload module migration with RLS.
--
-- Drop into examples/<your-app>/migrations/00001_<table>.sql.
-- Replace YOUR_DOMAIN_TABLE and column list with your schema.
--
-- The runtime owns:  suite_tenants, suite_users, suite_memberships,
--                    suite_api_keys, suite_cost_events, suite_jobs,
--                    suite_audit_log, suite_secrets, suite_memory, ...
-- Your module owns:  YOUR_DOMAIN_TABLE — keep names prefixed with your
--                    module id so they're easy to identify in the DB
--                    studio (e.g. notable_notes, forge_reviews,
--                    docuchat_documents).
--
-- RLS pattern:
--   - The runtime's tenancy middleware sets `app.tenant_id` GUC at the
--     start of every request via SET LOCAL.
--   - This file's RLS policy reads it back via current_setting().
--   - Operator scripts that need cross-tenant access can SET LOCAL
--     `app.bypass_rls = 'on'` for that transaction.
--   - Use current_setting('app.tenant_id', true) — the `true` makes it
--     missing_ok so a bare psql session doesn't error before the
--     runtime sets the GUC.

CREATE TABLE IF NOT EXISTS YOUR_DOMAIN_TABLE (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   text        NOT NULL,
    user_id     text        NOT NULL,
    -- Your columns here.
    title       text        NOT NULL,
    body        text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Index the dominant read path. Most apps list-by-tenant ordered by
-- recency; that's a composite (tenant_id, created_at DESC) index.
CREATE INDEX IF NOT EXISTS YOUR_DOMAIN_TABLE_tenant_recent_idx
    ON YOUR_DOMAIN_TABLE (tenant_id, created_at DESC);

-- Enable RLS — every read/write scoped to the tenant on the request.
ALTER TABLE YOUR_DOMAIN_TABLE ENABLE ROW LEVEL SECURITY;

-- Single policy covers SELECT / UPDATE / DELETE.
DROP POLICY IF EXISTS YOUR_DOMAIN_TABLE_tenant_isolation ON YOUR_DOMAIN_TABLE;
CREATE POLICY YOUR_DOMAIN_TABLE_tenant_isolation ON YOUR_DOMAIN_TABLE
    USING (
        current_setting('app.bypass_rls', true) = 'on'
        OR tenant_id = current_setting('app.tenant_id', true)
    )
    WITH CHECK (
        current_setting('app.bypass_rls', true) = 'on'
        OR tenant_id = current_setting('app.tenant_id', true)
    );

-- Optional: trigger to auto-update updated_at on UPDATEs.
CREATE OR REPLACE FUNCTION YOUR_DOMAIN_TABLE_touch_updated_at()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS YOUR_DOMAIN_TABLE_updated_at_trg ON YOUR_DOMAIN_TABLE;
CREATE TRIGGER YOUR_DOMAIN_TABLE_updated_at_trg
    BEFORE UPDATE ON YOUR_DOMAIN_TABLE
    FOR EACH ROW EXECUTE FUNCTION YOUR_DOMAIN_TABLE_touch_updated_at();
