-- Notable example schema — one table, one index, RLS keyed on tenant_id.
--
-- Why this exists: the runtime owns suite_tenants / suite_billing_customers /
-- suite_usage_meters; this migration owns the *application's* domain table
-- so the example shows the canonical pattern for adding tenant-scoped data
-- on top of AF Stack.
--
-- RLS policy notes:
--   - The runtime's tenancy module (services/runtime/internal/tenancy) sets
--     the `app.tenant_id` GUC at the start of every request via SET LOCAL.
--     We read it back here as a string and compare against tenant_id.
--   - Operators that need to read across tenants (e.g. the seed script)
--     can `SET LOCAL app.bypass_rls = 'on'` for that transaction.
--   - The policy uses `current_setting(..., true)` (missing_ok=true) so
--     bare psql sessions don't error before the runtime has set the GUC.

CREATE TABLE IF NOT EXISTS notable_notes (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   text        NOT NULL,
    user_id     text        NOT NULL,
    title       text        NOT NULL,
    body        text        NOT NULL DEFAULT '',
    -- Tags arrive from the suggest_tags agent or the user; either way we
    -- store them as a text[] so listing/filtering stays index-friendly.
    tags        text[]      NOT NULL DEFAULT ARRAY[]::text[],
    -- LLM-produced TLDR. Nullable because notes start their life without
    -- one — the user explicitly clicks "Summarize" to fill it.
    tldr        text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Tenant listing is the dominant read path ("show me my notes, newest
-- first"). Composite index keeps it index-only.
CREATE INDEX IF NOT EXISTS notable_notes_tenant_updated_idx
    ON notable_notes (tenant_id, updated_at DESC);

-- RLS — every read/write scoped to the tenant on the request.
ALTER TABLE notable_notes ENABLE ROW LEVEL SECURITY;

-- Single policy covers SELECT/UPDATE/DELETE; INSERTs get a separate WITH
-- CHECK below so apps can't accidentally write a row for the wrong tenant
-- by setting tenant_id manually.
DROP POLICY IF EXISTS notable_notes_tenant_isolation ON notable_notes;
CREATE POLICY notable_notes_tenant_isolation ON notable_notes
    USING (
        current_setting('app.bypass_rls', true) = 'on'
        OR tenant_id = current_setting('app.tenant_id', true)
    )
    WITH CHECK (
        current_setting('app.bypass_rls', true) = 'on'
        OR tenant_id = current_setting('app.tenant_id', true)
    );

-- updated_at auto-bump. We don't trust callers to set it.
CREATE OR REPLACE FUNCTION notable_notes_touch_updated_at()
RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS notable_notes_touch ON notable_notes;
CREATE TRIGGER notable_notes_touch
    BEFORE UPDATE ON notable_notes
    FOR EACH ROW
    EXECUTE FUNCTION notable_notes_touch_updated_at();
