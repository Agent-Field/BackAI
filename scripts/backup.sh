#!/usr/bin/env bash
# Back up the AF Stack runtime database.
#
# Default: drops ephemeral tables (audit_log, llm_cache, river_*,
# webhook_deliveries) to keep backups compact. Override with --include-all
# when you need a full forensic snapshot.
#
# Usage:
#   ./scripts/backup.sh --url postgres://... --out /backups/af.sql.gz
#   ./scripts/backup.sh --include-all --out /backups/af-full.sql.gz
#
# Env defaults: $AF_STACK_DATABASE_URL is used when --url is omitted.

set -euo pipefail

green() { printf "\033[32m%s\033[0m\n" "$1"; }
red()   { printf "\033[31m%s\033[0m\n" "$1"; }

URL=""
OUT=""
EXCLUDE_EPHEMERAL=1

while [[ $# -gt 0 ]]; do
    case "$1" in
        --url)            URL="$2"; shift 2;;
        --out)            OUT="$2"; shift 2;;
        --include-all)    EXCLUDE_EPHEMERAL=0; shift;;
        --exclude-ephemeral) EXCLUDE_EPHEMERAL=1; shift;;
        -h|--help)
            sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *)
            red "unknown flag: $1"; exit 1;;
    esac
done

URL="${URL:-${AF_STACK_DATABASE_URL:-}}"
[ -n "$URL" ] || { red "missing database url (set AF_STACK_DATABASE_URL or pass --url)"; exit 1; }
[ -n "$OUT" ] || { red "missing --out"; exit 1; }

command -v pg_dump >/dev/null || { red "pg_dump not installed"; exit 1; }
command -v gzip    >/dev/null || { red "gzip not installed";    exit 1; }

EXCLUDE_FLAGS=()
if [ "$EXCLUDE_EPHEMERAL" = "1" ]; then
    EXCLUDE_FLAGS+=(
        --exclude-table-data=suite_audit_log
        --exclude-table-data=suite_llm_cache
        --exclude-table-data=suite_webhook_deliveries
        --exclude-table-data='river_*'
    )
fi

mkdir -p "$(dirname "$OUT")"

green "==> Backing up to $OUT"
pg_dump --format=custom --no-owner --no-privileges \
    "${EXCLUDE_FLAGS[@]}" \
    "$URL" | gzip -9 > "$OUT"

SIZE=$(du -h "$OUT" | cut -f1)
green "==> Done. $OUT ($SIZE)"
green "    Test the restore quarterly. See docs/backup-restore.md."
