// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/db"
	"github.com/Agent-Field/backai/services/runtime/internal/prodcheck"
	"github.com/Agent-Field/backai/services/runtime/internal/secrets"
)

// runProductionPreflight enforces the production operating contract at boot.
//
// It is a no-op unless cfg.ProductionHardening() is true (SaaS mode AND
// AF_STACK_ENV=production). When armed, it verifies the deployment is actually
// hardened — RLS-safe serving role, tenant tables forced-RLS, real KMS key, no
// wildcard credentialed CORS, tenant-isolated storage, isolated sandbox
// network — and calls os.Exit(1) with a stable code per failure if not. A
// mis-hardened production deployment must never accept traffic; documenting the
// requirement is not enough.
func runProductionPreflight(ctx context.Context, cfg config.Config, database *db.DB, storageConfigured bool, log *slog.Logger) {
	if !cfg.ProductionHardening() {
		return
	}
	log.Info("production preflight: AF_STACK_ENV=production + saas mode — verifying operating contract")

	if database == nil || database.Pool == nil {
		log.Error("refusing to start: production requires a database so tenant isolation is enforceable",
			"code", prodcheck.CodeCatalogUnavailable,
			"fix", "set AF_STACK_DATABASE_URL to a Postgres serving role")
		os.Exit(1)
	}

	kmsConfigured := secrets.KMSConfigured()
	cfgInputs := prodcheck.ConfigInputs{
		CORSOrigins:          parseCORSOrigins(os.Getenv("AF_STACK_CORS_ORIGINS")),
		KMSConfigured:        kmsConfigured,
		KMSDevMode:           !kmsConfigured,
		StorageConfigured:    storageConfigured,
		MultiTenancyEnabled:  cfg.Modules.Enabled["multi-tenancy"],
		SandboxNetworkPolicy: cfg.SandboxNetworkPolicy(),
	}

	gatherCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	inputs, err := prodcheck.Gather(gatherCtx, database.Pool, cfgInputs)
	if err != nil {
		log.Error("refusing to start: production preflight could not read the database catalog to verify tenant isolation",
			"code", prodcheck.CodeCatalogUnavailable,
			"error", err)
		database.Close()
		os.Exit(1)
	}

	report := prodcheck.Evaluate(inputs)
	if report.OK() {
		log.Info("production preflight passed", "checks", len(report.Results))
		return
	}
	for _, f := range report.Failures() {
		log.Error("production preflight failed",
			"code", f.Code,
			"check", f.Name,
			"detail", f.Detail,
			"fix", f.Fix)
	}
	log.Error("refusing to start: production operating contract not satisfied",
		"failures", len(report.Failures()),
		"override", "resolve each failure above; there is deliberately no bypass flag for the production preflight")
	database.Close()
	os.Exit(1)
}

// parseCORSOrigins splits the AF_STACK_CORS_ORIGINS env value into individual
// origins, mirroring loadCORSAllowlist in internal/server so the preflight sees
// exactly the operator-supplied credentialed origins.
func parseCORSOrigins(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
