-- Shipwright — autonomous AI agent factory tables.
--
-- Two tables: shipwright_tasks holds the task envelope; shipwright_steps
-- holds per-step progress so the customer-app can render a live timeline.
-- RLS scoped on tenant_id via the runtime's app.tenant_id GUC.

CREATE TABLE IF NOT EXISTS shipwright_tasks (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       text NOT NULL,
    user_id         text NOT NULL,
    issue_url       text NOT NULL,
    title           text NOT NULL,
    description     text NOT NULL DEFAULT '',
    status          text NOT NULL DEFAULT 'queued',
    -- queued | running | completed | failed | cancelled
    run_id          text,
    execution_id    text,
    result_summary  text,
    diff_preview    text,
    error           text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    started_at      timestamptz,
    finished_at     timestamptz
);

CREATE INDEX IF NOT EXISTS shipwright_tasks_tenant_recent_idx
    ON shipwright_tasks (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS shipwright_tasks_status_idx
    ON shipwright_tasks (tenant_id, status, created_at DESC);

ALTER TABLE shipwright_tasks ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS shipwright_tasks_tenant_isolation ON shipwright_tasks;
CREATE POLICY shipwright_tasks_tenant_isolation ON shipwright_tasks
    USING (
        current_setting('app.bypass_rls', true) = 'on'
        OR tenant_id = current_setting('app.tenant_id', true)
    )
    WITH CHECK (
        current_setting('app.bypass_rls', true) = 'on'
        OR tenant_id = current_setting('app.tenant_id', true)
    );


CREATE TABLE IF NOT EXISTS shipwright_steps (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id         uuid NOT NULL REFERENCES shipwright_tasks(id) ON DELETE CASCADE,
    tenant_id       text NOT NULL,
    idx             integer NOT NULL,
    title           text NOT NULL,
    status          text NOT NULL DEFAULT 'pending',
    -- pending | running | completed | failed
    detail          text,
    started_at      timestamptz,
    finished_at     timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (task_id, idx)
);

CREATE INDEX IF NOT EXISTS shipwright_steps_task_idx
    ON shipwright_steps (task_id, idx);

ALTER TABLE shipwright_steps ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS shipwright_steps_tenant_isolation ON shipwright_steps;
CREATE POLICY shipwright_steps_tenant_isolation ON shipwright_steps
    USING (
        current_setting('app.bypass_rls', true) = 'on'
        OR tenant_id = current_setting('app.tenant_id', true)
    )
    WITH CHECK (
        current_setting('app.bypass_rls', true) = 'on'
        OR tenant_id = current_setting('app.tenant_id', true)
    );

CREATE OR REPLACE FUNCTION shipwright_tasks_touch_updated_at()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS shipwright_tasks_updated_at_trg ON shipwright_tasks;
CREATE TRIGGER shipwright_tasks_updated_at_trg
    BEFORE UPDATE ON shipwright_tasks
    FOR EACH ROW EXECUTE FUNCTION shipwright_tasks_touch_updated_at();
