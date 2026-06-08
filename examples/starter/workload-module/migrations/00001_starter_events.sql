-- SPDX-License-Identifier: Apache-2.0

CREATE TABLE IF NOT EXISTS starter_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  user_id text,
  kind text NOT NULL,
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS starter_events_tenant_created_idx
  ON starter_events (tenant_id, created_at DESC);
