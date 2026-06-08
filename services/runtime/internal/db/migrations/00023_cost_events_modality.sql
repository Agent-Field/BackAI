-- +goose Up
-- #14 — Multimodal API: extend suite_cost_events with a modality
-- column so the dashboard can break costs out by text vs audio_speech
-- vs audio_transcription vs audio_translation vs image vs embedding.
--
-- All pre-existing rows are text-mode LLM chat/completions (the only
-- modality before multimodal landed), so the DEFAULT 'text' on the
-- column is the right backfill.
--
-- The btree index supports the dashboard's "cost by modality" filter
-- (WHERE tenant_id = $1 AND modality = $2 ORDER BY occurred_at DESC).

alter table suite_cost_events
    add column if not exists modality text not null default 'text';

create index if not exists suite_cost_events_modality_idx
    on suite_cost_events (tenant_id, modality, occurred_at desc);

-- +goose Down
drop index if exists suite_cost_events_modality_idx;
alter table suite_cost_events
    drop column if exists modality;
