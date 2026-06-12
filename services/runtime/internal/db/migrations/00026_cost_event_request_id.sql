-- +goose Up
-- Add the gateway request id to cost events so a customer-facing app
-- can deep-link an LLM action to its exact operator dashboard row.

alter table suite_cost_events
    add column if not exists request_id text;

create index if not exists suite_cost_events_request_id_idx
    on suite_cost_events (request_id);

create index if not exists suite_cost_events_tenant_request_id_idx
    on suite_cost_events (tenant_id, request_id);

-- +goose Down
drop index if exists suite_cost_events_tenant_request_id_idx;
drop index if exists suite_cost_events_request_id_idx;
alter table suite_cost_events
    drop column if exists request_id;
