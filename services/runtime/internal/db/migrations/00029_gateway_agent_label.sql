-- +goose Up
-- LLM gateway calls are now logged into suite_gateway_requests so the
-- dashboard Runs view reflects real app activity, not just agent-execute
-- forwards. The endpoint column stays truthful ("/api/v1/llm/chat/completions"),
-- so the caller identity from X-AF-Reasoner lands in its own column.
alter table suite_gateway_requests
  add column if not exists agent_label text;

create index if not exists suite_gateway_requests_agent_label_idx
  on suite_gateway_requests (agent_label, created_at desc)
  where agent_label is not null;

-- +goose Down
drop index if exists suite_gateway_requests_agent_label_idx;
alter table suite_gateway_requests
  drop column if exists agent_label;
