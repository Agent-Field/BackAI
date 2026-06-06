#!/bin/bash
# Create multiple databases on Postgres startup.
# Used by docker-compose to provision afstack + agentfield databases.

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
