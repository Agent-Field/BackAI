-- +goose Up
-- Item #22 — LiteLLM virtual keys.
--
-- AF Stack now mints a LiteLLM virtual key alongside every
-- suite_api_keys row when LiteLLM is reachable. Per-tenant budgets and
-- per-key rate limits are enforced upstream by LiteLLM (master-key
-- protected admin API at /key/*, spend tooling at /spend/*).
--
-- Columns added here are nullable. Existing rows without a LiteLLM
-- mapping keep working — the LLM gateway falls back to LITELLM_MASTER_KEY
-- when the per-tenant secret isn't present. New issuances populate them.
--
-- The plaintext LiteLLM secret is NOT stored on this row. It's encrypted
-- by the secrets vault under key "litellm/key/{api_key_id}" so an
-- accidental leak of the row reveals only the alias + a hash (defense in
-- depth — the row already lives behind RLS, but compounding helps).
--
-- Source of truth for spend + budget remaining moves to LiteLLM's
-- /spend/keys/{key} and /spend/users/{user} endpoints. suite_cost_events
-- becomes a write-through audit table — useful for forensic queries and
-- per-modality breakdowns, but not the canonical balance.

alter table suite_api_keys
    add column if not exists litellm_key_alias text,
    add column if not exists litellm_key_hash  text,
    add column if not exists budget_max_usd    numeric(10, 4),
    add column if not exists rate_limit_rpm    integer,
    add column if not exists rate_limit_tpm    integer;

-- Partial index — only rows that actually have a LiteLLM mapping are
-- worth indexing. The runtime hits this index when reconciling
-- AF Stack <-> LiteLLM during status reads / spend lookups.
create index if not exists suite_api_keys_litellm_alias_idx
    on suite_api_keys (litellm_key_alias)
    where litellm_key_alias is not null;

-- +goose Down
drop index if exists suite_api_keys_litellm_alias_idx;

alter table suite_api_keys
    drop column if exists rate_limit_tpm,
    drop column if exists rate_limit_rpm,
    drop column if exists budget_max_usd,
    drop column if exists litellm_key_hash,
    drop column if exists litellm_key_alias;
