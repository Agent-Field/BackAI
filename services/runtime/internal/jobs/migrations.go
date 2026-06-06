// migrations.go runs River's embedded schema migrations.
//
// River ships its own migration sequence (river_migration table + the
// river_job table itself). These are versioned separately from the suite's
// goose-managed `00001_init.sql` / `00002_better_auth.sql` because River
// owns the schema and may add tables/columns in new releases.
//
// We invoke them via rivermigrate.Migrate(ctx, DirectionUp, nil) — idempotent,
// safe to call on every boot.
package jobs

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// MigrateUp brings River's schema up to HEAD using the given pgx pool. Safe
// to call repeatedly; River's migrator no-ops once it's at the latest
// version.
//
// schema is optional: when empty, River uses the connection's search_path.
// Tests that pin a custom schema should pass it here so the migrator
// writes the tables under the correct prefix.
func MigrateUp(ctx context.Context, pool *pgxpool.Pool, schema ...string) error {
	if pool == nil {
		return fmt.Errorf("jobs.MigrateUp: nil pool")
	}
	driver := riverpgxv5.New(pool)
	cfg := &rivermigrate.Config{}
	if len(schema) > 0 && schema[0] != "" {
		cfg.Schema = schema[0]
	}
	migrator, err := rivermigrate.New(driver, cfg)
	if err != nil {
		return fmt.Errorf("jobs.MigrateUp: new migrator: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("jobs.MigrateUp: migrate: %w", err)
	}
	return nil
}
