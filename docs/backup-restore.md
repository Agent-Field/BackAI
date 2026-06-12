# Backup and Restore

What to back up, how often, and how to restore. The runtime keeps state
in two places — PostgreSQL and the storage adapter (MinIO / S3). Get
both right and you can lose the whole cluster without losing data.

## What's persistent

### PostgreSQL

The runtime database holds every operational fact: tenants, API keys,
costs, jobs, crons, secrets, memory entries, notifications, webhooks,
sandbox runs, billing customers, usage meters. Lose this and you lose
the suite.

Three layers of priority:

| Tier | Tables | Loss = |
|---|---|---|
| 1 (must back up) | `suite_tenants`, `suite_users`, `suite_memberships`, `suite_api_keys`, `suite_secrets`, `suite_billing_customers`, `suite_skills`, `suite_mcp_servers`, `suite_webhook_endpoints`, `suite_notifications` (queued/scheduled only), `suite_crons` | Tenants can't log in, integrations broken, customers can't be charged |
| 2 (should back up) | `suite_cost_events` (last 90 days), `suite_usage_meters`, `suite_memory_entries`, `notable_notes` (any workload module tables) | Cost data + agent state lost; recoverable from logs in theory |
| 3 (ephemeral) | `suite_jobs`, `suite_runs`, `suite_audit_log`, `suite_webhook_deliveries`, `suite_sandbox_runs`, `suite_llm_cache`, `river_*` | Forensics + cache only; safe to truncate to shrink backups |

### Storage adapter

MinIO (or external S3) holds:

- Sandbox run stdout/stderr. Mostly ephemeral; the dashboard
  references it via signed URLs.
- Webhook delivery payloads (large bodies stored out-of-row).
- Notable note attachments (if you enable the example).
- Anything your custom workload modules write.

Treat the bucket as recoverable but not load-bearing — sandbox logs
can be discarded after 30 days; webhook payloads after the dedup
window expires.

### Secrets vault key material

`suite_secrets` values are encrypted with a 32-byte data key. With
`AF_STACK_KMS_PROVIDER=env`, that data key is `AF_STACK_KMS_KEY`. With
cloud BYOK (`aws`, `gcp`, or `azure`), the runtime unwraps
`AF_STACK_KMS_ENCRYPTED_DATA_KEY` through the configured cloud KMS key at
boot.

If you lose the env key, the encrypted data key, or access to the cloud
KMS key that unwraps it, every stored secret is permanently unreadable.
**Back this up separately from the database**. Never commit it.

## Frequency recommendations

| Workload | DB | Storage | KMS key |
|---|---|---|---|
| Dev / staging | Weekly | None | Once at provisioning |
| Production, < 100 tenants | Daily + WAL | Weekly | Quarterly attestation |
| Production, 100+ tenants | Continuous WAL + daily snapshot | Daily | Quarterly attestation |
| High-cost LLM workloads | Same as above, + hourly snapshot during heavy usage so an accidental DELETE on cost_events is recoverable | — | — |

## Database backup

`scripts/backup.sh` wraps `pg_dump` with the right flags:

```bash
./scripts/backup.sh \
  --url "$AF_STACK_DATABASE_URL" \
  --out /backups/af-stack-$(date +%Y%m%d-%H%M).sql.gz \
  --exclude-ephemeral
```

Internally, the script runs:

```bash
pg_dump --format=custom --no-owner --no-privileges \
  --exclude-table-data=suite_audit_log \
  --exclude-table-data=suite_llm_cache \
  --exclude-table-data='river_*' \
  --exclude-table-data=suite_webhook_deliveries \
  "$URL" | gzip -9 > "$OUT"
```

The `--exclude-table-data` flags drop the contents of ephemeral tables
but preserve their schema — so the restored DB has the same shape, just
no history.

For point-in-time recovery, use the WAL-archive feature of your managed
Postgres (Cloud SQL, RDS, Crunchy, etc.) or self-host with
[wal-g](https://github.com/wal-g/wal-g) / pgBackRest.

## Database restore

`scripts/restore.sh`:

```bash
./scripts/restore.sh \
  --url "$AF_STACK_DATABASE_URL" \
  --from /backups/af-stack-20260607-0300.sql.gz \
  --confirm-overwrite
```

Internally:

```bash
gunzip -c "$FROM" | pg_restore --clean --if-exists --no-owner \
  --no-privileges -d "$URL"
```

After restore, **always** run the runtime migrations again — they're
idempotent and will catch any schema drift between the backup vintage
and the current code:

```bash
docker compose run --rm runtime /usr/local/bin/af-stack migrate up
```

## Storage backup

For MinIO (in-cluster) — back up the bucket with `mc mirror`:

```bash
mc alias set af http://minio:9000 minio minio-secret
mc mirror --overwrite af/af-stack s3://your-prod-backup-bucket/backai-$(date +%Y%m%d)/
```

For external S3 — set up cross-bucket replication or use
[`aws s3 sync`](https://docs.aws.amazon.com/cli/latest/reference/s3/sync.html)
on a cron. The bucket is your backup; just enable versioning + object
lock and you have a tamper-evident archive.

## Storage restore

Mirror back the other way:

```bash
mc mirror s3://your-prod-backup-bucket/backai-20260607/ af/af-stack
```

Sandbox run rows in PostgreSQL reference storage by URL. After a
restore, expect some signed-URL endpoints to 404 until you re-link or
mark old runs as archived — there's a `scripts/storage-relink.sh`
helper for this.

## KMS key rotation

The active data key encrypts secret values inside `suite_secrets`. To
rotate:

1. Set `AF_STACK_KMS_KEY_NEW=<new 64-char hex>` for the env provider, or
   configure a newly wrapped cloud BYOK data key alongside the current
   one.
2. Run `af-stack secrets rotate-kms` — re-encrypts every row with the
   new key in a transaction (uses both old + new keys during the migration).
3. Restart the runtime with only the new env key or wrapped data key.
4. Archive the old key material labelled `<env>-pre-<timestamp>`.

## Backup verification

Untested backups don't exist. Once a quarter:

1. Pick a recent backup file.
2. Spin up a fresh Postgres locally.
3. Restore into it.
4. Run `scripts/test-quickstart.sh` against a runtime pointed at the
   restored DB. Smoke-test that login + a single LLM call work.

If the test fails, your backup procedure is broken — fix it, don't
just hope the next one works.

## Restore-time gotchas

- **Cookies expire on restore.** Anyone holding a session cookie from
  before the backup vintage still has a valid `better-auth.session`
  row, BUT if you restored an older snapshot their session may have
  been deleted. Force re-login by truncating `session` after restore
  if you want a clean slate.
- **River jobs.** The river queue tables (`river_*`) are excluded from
  the default backup. After restore, in-flight jobs from before the
  snapshot are lost. Most jobs are safe to lose (cron will re-fire); if
  you have critical async work, change the backup script to include
  them.
- **Stripe state drift.** Billing customer rows reference Stripe IDs.
  If you restore an old snapshot, the Stripe-side state has moved on.
  Reconcile with the Stripe webhook handler — it'll catch up on the
  next sync event.

## Disaster recovery test

Once a year, run a full disaster recovery drill: provision a fresh
cluster, restore from your most recent backup, validate every tenant
can log in and make a request. Time it. Document the steps.

The drill is the only way you find out whether your backups actually
work.
