// Migrations: embedded SQL files run with goose on boot.
//
// Source files live in services/runtime/internal/db/migrations/. Each file
// follows goose convention: an `-- +goose Up` and `-- +goose Down` block.

package db

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate applies all pending up-migrations against the database.
//
// Idempotent: if the schema is already at HEAD, this is a no-op.
func (d *DB) Migrate(ctx context.Context) error {
	if d == nil || d.Pool == nil {
		return fmt.Errorf("db.Migrate: not initialized")
	}
	goose.SetBaseFS(migrationFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("db.Migrate: dialect: %w", err)
	}

	sqlDB := stdlib.OpenDBFromPool(d.Pool)
	defer sqlDB.Close()

	if err := goose.UpContext(ctx, sqlDB, "migrations"); err != nil {
		return fmt.Errorf("db.Migrate: up: %w", err)
	}
	return nil
}
