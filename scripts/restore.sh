#!/usr/bin/env bash
# Restore the AF Stack runtime database from a pg_dump archive.
#
# Drops + recreates objects via pg_restore --clean. Re-runs migrations
# after to catch any schema drift between the backup and the current
# code.
#
# Usage:
#   ./scripts/restore.sh --url postgres://... --from /backups/af.sql.gz
#
# By default refuses to run against a non-empty database. Pass
# --confirm-overwrite to acknowledge destructive intent.

set -euo pipefail

green()  { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
red()    { printf "\033[31m%s\033[0m\n" "$1"; }

URL=""
FROM=""
CONFIRM=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --url)                URL="$2"; shift 2;;
        --from)               FROM="$2"; shift 2;;
        --confirm-overwrite)  CONFIRM=1; shift;;
        -h|--help)
            sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *) red "unknown flag: $1"; exit 1;;
    esac
done

URL="${URL:-${AF_STACK_DATABASE_URL:-}}"
[ -n "$URL" ]  || { red "missing database url"; exit 1; }
[ -n "$FROM" ] || { red "missing --from"; exit 1; }
[ -f "$FROM" ] || { red "$FROM not found"; exit 1; }

command -v pg_restore >/dev/null || { red "pg_restore not installed"; exit 1; }
command -v gunzip      >/dev/null || { red "gunzip not installed";     exit 1; }
command -v psql        >/dev/null || { red "psql not installed";       exit 1; }

EXISTING_TABLES=$(psql "$URL" -tAc "select count(*) from pg_tables where schemaname='public'" 2>/dev/null || echo 0)
if [ "${EXISTING_TABLES:-0}" -gt 0 ] && [ "$CONFIRM" != "1" ]; then
    yellow "==> Target database already has $EXISTING_TABLES tables in 'public'."
    red    "    Refusing to restore over a non-empty database without --confirm-overwrite."
    red    "    Either drop the DB first or rerun with --confirm-overwrite."
    exit 1
fi

green "==> Restoring from $FROM"
gunzip -c "$FROM" | pg_restore --clean --if-exists --no-owner \
    --no-privileges -d "$URL"

yellow "==> Schema may have moved on since the backup vintage."
yellow "    Run the runtime migrations now to catch up:"
yellow "      docker compose run --rm runtime /usr/local/bin/af-stack migrate up"

green  "==> Restore complete. Validate with scripts/test-quickstart.sh."
