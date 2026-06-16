// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
)

func main() {
	if len(os.Args) < 3 || os.Args[1] != "config" || os.Args[2] != "validate" {
		fmt.Fprintln(os.Stderr, "Usage: backai config validate [-f backai.config.yaml]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("config validate", flag.ExitOnError)
	path := fs.String("f", config.DefaultFeatureConfigPath, "path to backai.config.yaml")
	fs.StringVar(path, "file", config.DefaultFeatureConfigPath, "path to backai.config.yaml")
	_ = fs.Parse(os.Args[3:])

	data, err := os.ReadFile(*path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ Layer 1 (schema)        — read %s: %v\n", *path, err)
		os.Exit(1)
	}
	cfg, issues, err := config.ParseFeatureConfig(data, os.Getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ Layer 1 (schema)        — %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Layer 1 (schema)        — %s parses cleanly.\n", *path)
	errors := 0
	for _, issue := range issues {
		if issue.Level == config.ValidationErrorLevel {
			errors++
		}
	}
	if errors == 0 {
		fmt.Printf("✓ Layer 2 (dependencies)  — %d features enabled, 0 conflicts.\n", config.CountEnabledFeatures(cfg.Features))
		return
	}
	fmt.Printf("✗ Layer 2 (dependencies)  — %d issue(s).\n", errors)
	for _, issue := range issues {
		fmt.Printf("  - features.%s: %s\n", issue.Feature, issue.Message)
		if issue.Remediation != "" {
			fmt.Printf("    Remediation: %s\n", issue.Remediation)
		}
	}
	os.Exit(1)
}
