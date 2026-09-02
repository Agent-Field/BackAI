#!/bin/bash
# Postgres bootstrap for the compose stack.
#
#  1. Create the databases named in POSTGRES_MULTIPLE_DATABASES.
#  2. Optionally provision a restricted "serving" role for the runtime so that
#     per-tenant row-level security is actually enforced. The default
#     POSTGRES_USER is a superuser and OWNS every table, so it bypasses RLS
#     unconditionally — tenant isolation is only real when the runtime connects
#     as a NOSUPERUSER NOBYPASSRLS role. Migrations still run as POSTGRES_USER
#     (via AF_STACK_MIGRATE_DATABASE_URL); the runtime serves as this role.
#
# Runs only on first init (empty data dir). For an already-initialized volume,
# run the same SQL by hand or recreate the volume.

set -e

if [ -n "$POSTGRES_MULTIPLE_DATABASES" ]; then
  for db in $(echo "$POSTGRES_MULTIPLE_DATABASES" | tr ',' ' '); do
    # Skip if the database already exists (idempotent across container restarts).
    exists=$(psql -tAc "SELECT 1 FROM pg_database WHERE datname='$db'" --username "$POSTGRES_USER" 2>/dev/null || echo "")
    if [ "$exists" = "1" ]; then
      echo "Database already exists: $db (skipping)"
      continue
    fi
    echo "Creating database: $db"
    psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" <<-EOSQL
      CREATE DATABASE $db;
      GRANT ALL PRIVILEGES ON DATABASE $db TO $POSTGRES_USER;
EOSQL
  done
fi

# Restricted serving role for the runtime (real tenant isolation).
APP_ROLE="${AF_STACK_APP_DB_ROLE:-afstack_app}"
APP_DB="${AF_STACK_APP_DB_NAME:-afstack}"
if [ -n "$AF_STACK_APP_DB_PASSWORD" ]; then
  echo "Provisioning restricted serving role: $APP_ROLE on $APP_DB"
  # Role is cluster-wide; create it once (idempotent). No DDL/superuser/bypassrls.
  psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "postgres" <<-EOSQL
    DO \$\$
    BEGIN
      IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '${APP_ROLE}') THEN
        CREATE ROLE ${APP_ROLE} LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS
          PASSWORD '${AF_STACK_APP_DB_PASSWORD}';
      ELSE
        ALTER ROLE ${APP_ROLE} LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS
          PASSWORD '${AF_STACK_APP_DB_PASSWORD}';
      END IF;
    END
    \$\$;
EOSQL
  # Grants + default privileges are per-database; run inside the app DB. Default
  # privileges "FOR ROLE $POSTGRES_USER" make every future table that the owner
  # creates (i.e. each migration) auto-grant DML to the serving role.
  psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$APP_DB" <<-EOSQL
    GRANT CONNECT ON DATABASE ${APP_DB} TO ${APP_ROLE};
    GRANT USAGE ON SCHEMA public TO ${APP_ROLE};
    GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO ${APP_ROLE};
    GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO ${APP_ROLE};
    ALTER DEFAULT PRIVILEGES FOR ROLE ${POSTGRES_USER} IN SCHEMA public
      GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO ${APP_ROLE};
    ALTER DEFAULT PRIVILEGES FOR ROLE ${POSTGRES_USER} IN SCHEMA public
      GRANT USAGE, SELECT ON SEQUENCES TO ${APP_ROLE};
EOSQL
fi
