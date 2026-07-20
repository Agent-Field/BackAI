// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// MigrationDB is the DB surface ApplyMigrations needs: transactions plus
// the read/write for the tracking table. *pgxpool.Pool satisfies it.
type MigrationDB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Begin(ctx context.Context) (pgx.Tx, error)
}

// migrationTrackingTable is the platform table (migration 00036) that
// records which module migrations have been applied. It is platform-owned
// (not tenant-scoped) and keyed by (module_id, version).
const migrationTrackingTable = "suite_module_migrations"

var migrationFilePattern = regexp.MustCompile(`^(\d+)[_-].*\.sql$`)

// migrationFile is one versioned .sql file in a module's migrations dir.
type migrationFile struct {
	Version int
	Name    string
	Path    string
	SQL     string
}

// readMigrationFiles loads and version-sorts a module's migration files.
// Files must be named <version>_<desc>.sql (numeric prefix). A missing
// directory is not an error — a module may declare no migrations (though
// one that declares resources without a table will fail at first query).
func readMigrationFiles(dir string) ([]migrationFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read migrations dir %s: %w", dir, err)
	}
	files := make([]migrationFile, 0, len(entries))
	seen := map[int]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		m := migrationFilePattern.FindStringSubmatch(name)
		if m == nil {
			if strings.HasSuffix(name, ".sql") {
				return nil, fmt.Errorf("migration %q must be named <version>_<description>.sql", name)
			}
			continue
		}
		version, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, fmt.Errorf("migration %q: bad version: %w", name, err)
		}
		if prev, dup := seen[version]; dup {
			return nil, fmt.Errorf("migration version %d declared twice: %s and %s", version, prev, name)
		}
		seen[version] = name
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		files = append(files, migrationFile{
			Version: version,
			Name:    name,
			Path:    filepath.Join(dir, name),
			SQL:     string(data),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Version < files[j].Version })
	return files, nil
}

// applyMigrations runs a module's pending migrations against db, recording
// each in the tracking table. Each file is applied in its own transaction
// so a failure leaves earlier files committed and the module marked at the
// last good version. Returns the highest applied version.
//
// DB-bound: unit tests skip this path (no DB available); it is verified
// live downstream. The pure file-reading/versioning is covered by tests.
func applyMigrations(ctx context.Context, db MigrationDB, moduleID string, files []migrationFile) (int, error) {
	applied, err := appliedVersions(ctx, db, moduleID)
	if err != nil {
		return 0, err
	}
	highest := 0
	for v := range applied {
		if v > highest {
			highest = v
		}
	}
	for _, f := range files {
		if _, done := applied[f.Version]; done {
			continue
		}
		if err := applyOne(ctx, db, moduleID, f); err != nil {
			return highest, fmt.Errorf("module %q migration %s: %w", moduleID, f.Name, err)
		}
		highest = f.Version
	}
	return highest, nil
}

func appliedVersions(ctx context.Context, db MigrationDB, moduleID string) (map[int]struct{}, error) {
	rows, err := db.Query(ctx,
		`select version from `+migrationTrackingTable+` where module_id = $1`, moduleID)
	if err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	defer rows.Close()
	out := map[int]struct{}{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = struct{}{}
	}
	return out, rows.Err()
}

func applyOne(ctx context.Context, db MigrationDB, moduleID string, f migrationFile) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	// No-arg Exec uses the simple protocol, so a multi-statement migration
	// file runs as one round trip.
	if _, err := tx.Exec(ctx, f.SQL); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`insert into `+migrationTrackingTable+` (module_id, version, name) values ($1, $2, $3)`,
		moduleID, f.Version, f.Name); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
