// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// defaultMigrationsDir is the runtime's goose migration directory, resolved
// relative to the repo root (the tool is normally invoked from there).
const defaultMigrationsDir = "services/runtime/internal/db/migrations"

func main() {
	dir := defaultMigrationsDir
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}

	findings, err := lintDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrationlint: %v\n", err)
		os.Exit(2)
	}

	if len(findings) == 0 {
		fmt.Printf("migrationlint: OK (%s)\n", dir)
		return
	}
	for _, f := range findings {
		fmt.Fprintf(os.Stderr, "%s:%d [%s] %s\n", f.File, f.Line, f.Code, f.Message)
	}
	fmt.Fprintf(os.Stderr, "migrationlint: %d finding(s)\n", len(findings))
	os.Exit(1)
}

// lintDir lints every *.sql file in dir and returns all findings sorted by
// file then line.
func lintDir(dir string) ([]Finding, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir %s: %w", dir, err)
	}
	var all []Finding
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".sql" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		all = append(all, LintContent(e.Name(), string(data))...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].File != all[j].File {
			return all[i].File < all[j].File
		}
		return all[i].Line < all[j].Line
	})
	return all, nil
}
