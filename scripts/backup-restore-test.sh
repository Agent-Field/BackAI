#!/usr/bin/env bash
# Verify a BackAI database backup actually restores.
#
# Dumps the source database, restores it into a throwaway scratch database, and
# compares row counts on core tables. Exits non-zero on any mismatch or restore
# error — a green run is real evidence the backup is recoverable, not just that
# pg_dump produced a file.
#
# IMPORTANT: the source URL must authenticate as a role that can read every row
# (superuser or BYPASSRLS). Tenant tables ship with FORCE ROW LEVEL SECURITY, so
# a dump taken as the restricted serving role would silently contain ZERO tenant
# rows. Prefer AF_STACK_MIGRATE_DATABASE_URL (the privileged migrate role).
#
# Usage:
#   ./scripts/backup-restore-test.sh
#   ./scripts/backup-restore-test.sh --source postgres://... --scratch postgres://...
#
# Env defaults:
#   --source   $AF_STACK_MIGRATE_DATABASE_URL, else $AF_STACK_DATABASE_URL
#   --scratch  $BACKUP_TEST_SCRATCH_URL. When unset, a sibling database named
#              <dbname>_backup_test_<epoch> is created next to the source and
#              dropped on exit (requires CREATEDB on the source role).
set -euo pipefail

green() { printf "\033[32m%s\033[0m\n" "$1"; }
yellow() { printf "\033[33m%s\033[0m\n" "$1"; }
red() { printf "\033[31m%s\033[0m\n" "$1"; }

SOURCE=""
SCRATCH=""
while [[ $# -gt 0 ]]; do
	case "$1" in
	--source) SOURCE="$2"; shift 2 ;;
	--scratch) SCRATCH="$2"; shift 2 ;;
	-h | --help)
		sed -n '2,22p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	*) red "unknown flag: $1"; exit 2 ;;
	esac
done

SOURCE="${SOURCE:-${AF_STACK_MIGRATE_DATABASE_URL:-${AF_STACK_DATABASE_URL:-}}}"
SCRATCH="${SCRATCH:-${BACKUP_TEST_SCRATCH_URL:-}}"
[ -n "$SOURCE" ] || { red "missing source url (set AF_STACK_MIGRATE_DATABASE_URL / AF_STACK_DATABASE_URL or pass --source)"; exit 2; }

for bin in pg_dump pg_restore psql; do
	command -v "$bin" >/dev/null 2>&1 || { red "required tool not found: $bin (install the postgresql client)"; exit 2; }
done

DUMP_FILE="$(mktemp -t backai-backup-XXXXXX.dump)"
CREATED_SCRATCH=0

cleanup() {
	rm -f "$DUMP_FILE"
	if [ "$CREATED_SCRATCH" = "1" ] && [ -n "$SCRATCH_DB" ]; then
		# Drop the sibling scratch database we created.
		psql "$SOURCE" -v ON_ERROR_STOP=1 -tAc "drop database if exists \"$SCRATCH_DB\" with (force)" >/dev/null 2>&1 || \
			psql "$SOURCE" -tAc "drop database if exists \"$SCRATCH_DB\"" >/dev/null 2>&1 || true
	fi
}
trap cleanup EXIT

# Derive a sibling scratch database URL when none was supplied. The dbname is
# the last path segment of the source URL; swap it, preserving the query string.
SCRATCH_DB=""
if [ -z "$SCRATCH" ]; then
	base="${SOURCE%%\?*}"
	query=""
	[[ "$SOURCE" == *\?* ]] && query="?${SOURCE#*\?}"
	prefix="${base%/*}"
	SCRATCH_DB="$(basename "$base")_backup_test_$(date +%s)"
	SCRATCH="${prefix}/${SCRATCH_DB}${query}"
	yellow "creating scratch database ${SCRATCH_DB}"
	psql "$SOURCE" -v ON_ERROR_STOP=1 -c "create database \"$SCRATCH_DB\"" >/dev/null
	CREATED_SCRATCH=1
fi

green "==> dumping source (custom format)"
pg_dump --format=custom --no-owner --no-privileges --file="$DUMP_FILE" "$SOURCE"

green "==> restoring into scratch"
# --clean --if-exists makes a re-used scratch DB idempotent; errors during
# restore are fatal (ON_ERROR_STOP-equivalent via --exit-on-error).
pg_restore --no-owner --no-privileges --clean --if-exists --exit-on-error --dbname="$SCRATCH" "$DUMP_FILE"

green "==> comparing row counts on core tables"
CORE_TABLES=(
	suite_tenants
	suite_api_keys
	suite_cost_events
	suite_secrets
	suite_budgets
	suite_memory
	suite_webhooks
	suite_crons
)

count() { psql "$1" -tAc "select count(*) from public.\"$2\""; }
exists() { [ "$(psql "$1" -tAc "select to_regclass('public.$2') is not null")" = "t" ]; }

mismatch=0
compared=0
for t in "${CORE_TABLES[@]}"; do
	exists "$SOURCE" "$t" || { yellow "skip $t (not present in source)"; continue; }
	src="$(count "$SOURCE" "$t")"
	dst="$(count "$SCRATCH" "$t")"
	compared=$((compared + 1))
	if [ "$src" != "$dst" ]; then
		red "MISMATCH $t: source=$src restored=$dst"
		mismatch=1
	else
		green "ok $t: $src rows"
	fi
done

if [ "$compared" = "0" ]; then
	red "no core tables were present to compare — is the source URL a role that can read rows (FORCE RLS hides tenant tables from restricted roles)?"
	exit 1
fi

if [ "$mismatch" != "0" ]; then
	red "==> backup/restore verification FAILED"
	exit 1
fi

green "==> backup/restore verification PASSED ($compared core tables)"
