// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadMigrationFiles_SortsByVersion(t *testing.T) {
	dir := t.TempDir()
	// Write out of order; expect version-sorted output.
	writeFile(t, dir, "00002_second.sql", "select 2;")
	writeFile(t, dir, "00010_tenth.sql", "select 10;")
	writeFile(t, dir, "00001_first.sql", "select 1;")
	writeFile(t, dir, "notes.txt", "ignored") // non-sql ignored

	files, err := readMigrationFiles(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 migrations, got %d", len(files))
	}
	want := []int{1, 2, 10}
	for i, f := range files {
		if f.Version != want[i] {
			t.Fatalf("file %d version = %d, want %d (unsorted?)", i, f.Version, want[i])
		}
	}
}

func TestReadMigrationFiles_MissingDirIsNil(t *testing.T) {
	files, err := readMigrationFiles(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("missing dir should not error, got: %v", err)
	}
	if files != nil {
		t.Fatalf("expected nil files, got %v", files)
	}
}

func TestReadMigrationFiles_RejectsBadNames(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "no_version_prefix.sql", "select 1;")
	if _, err := readMigrationFiles(dir); err == nil {
		t.Fatal("expected error for un-versioned .sql file name")
	}
}

func TestReadMigrationFiles_RejectsDuplicateVersion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "00001_a.sql", "select 1;")
	writeFile(t, dir, "00001_b.sql", "select 2;")
	if _, err := readMigrationFiles(dir); err == nil {
		t.Fatal("expected error for duplicate migration version")
	}
}

func TestApplyMigrations_DBBound(t *testing.T) {
	t.Skip("applyMigrations requires a live Postgres (transactions + tracking table); verified live downstream")
}
