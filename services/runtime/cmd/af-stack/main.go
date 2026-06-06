// af-stack — the suite runtime entry point.
//
// Reads config (yaml + env), opens PG, connects to AF, starts the HTTP
// server, drains on SIGINT/SIGTERM.
//
// Usage:
//
//	af-stack            # serve with default config.yaml + env
//	af-stack version    # print version
//	af-stack -c path/to/config.yaml
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agent-Field/backai/services/runtime/internal/agentfield"
	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/db"
	"github.com/Agent-Field/backai/services/runtime/internal/hooks"
	"github.com/Agent-Field/backai/services/runtime/internal/jobs"
	"github.com/Agent-Field/backai/services/runtime/internal/logger"
	"github.com/Agent-Field/backai/services/runtime/internal/observability"
	"github.com/Agent-Field/backai/services/runtime/internal/secrets"
	"github.com/Agent-Field/backai/services/runtime/internal/server"
	"github.com/Agent-Field/backai/services/runtime/internal/storage"
	minioadapter "github.com/Agent-Field/backai/services/runtime/internal/storage/adapters/minio"
	s3adapter "github.com/Agent-Field/backai/services/runtime/internal/storage/adapters/s3"
)

const version = "0.0.1"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			fmt.Printf("af-stack %s\n", version)
			return
		}
	}

	var configPath string
	flag.StringVar(&configPath, "c", "", "path to config.yaml (default: ./config.yaml if exists)")
	flag.StringVar(&configPath, "config", "", "path to config.yaml (default: ./config.yaml if exists)")
	flag.Parse()

	if configPath == "" {
		if _, err := os.Stat("config.yaml"); err == nil {
			configPath = "config.yaml"
		}
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	log := logger.New(cfg.Logging.Level, cfg.Logging.Format)
	log.Info("af-stack starting",
		"version", version,
		"http_addr", cfg.Server.HTTPAddr,
		"agentfield_url", cfg.AgentField.URL,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Observability: set up OTel + Prometheus. Failures are non-fatal —
	// the runtime keeps working without traces.
	tel, telErr := observability.Setup(ctx, observability.Config{
		OTLPEndpoint: cfg.Observability.OTLPEndpoint,
		ServiceName:  cfg.Observability.ServiceName,
		Version:      version,
	})
	if telErr != nil {
		log.Error("observability setup failed; continuing without traces", "error", telErr)
		tel = nil
	} else {
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := tel.Shutdown(ctx); err != nil {
				log.Error("observability shutdown failed", "error", err)
			}
		}()
		if cfg.Observability.OTLPEndpoint != "" {
			log.Info("OTel tracing enabled", "endpoint", cfg.Observability.OTLPEndpoint)
		}
	}

	// Optional: connect to Postgres. If DATABASE_URL is empty, the runtime
	// still boots — it just reports the DB as not-configured in /health.
	var database *db.DB
	if cfg.Database.URL != "" {
		dbCtx, dbCancel := context.WithTimeout(ctx, 30*time.Second)
		database, err = db.Open(dbCtx, db.Config{
			URL:             cfg.Database.URL,
			MaxConnections:  cfg.Database.MaxConnections,
			MaxIdleConns:    cfg.Database.MaxIdleConns,
			ConnMaxLifetime: cfg.Database.ConnMaxLifetime,
		})
		dbCancel()
		if err != nil {
			log.Error("database connection failed; continuing in degraded mode",
				"url_redacted", "(redacted)", "error", err)
		} else {
			log.Info("database connected")
			migCtx, migCancel := context.WithTimeout(ctx, 60*time.Second)
			if err := database.Migrate(migCtx); err != nil {
				log.Error("migrations failed", "error", err)
				database.Close()
				migCancel()
				os.Exit(1)
			}
			migCancel()
			log.Info("migrations applied")
			defer database.Close()
		}
	} else {
		log.Warn("database URL not configured; running without persistent state")
	}

	// AF client. Always constructed; health is reported through /health.
	afClient := agentfield.New(agentfield.Config{
		URL:            cfg.AgentField.URL,
		RequestTimeout: cfg.AgentField.RequestTimeout,
	})
	// Async probe to log AF reachability at startup.
	go func() {
		probeCtx, probeCancel := context.WithTimeout(ctx, cfg.AgentField.HealthTimeout)
		defer probeCancel()
		if _, err := afClient.Health(probeCtx); err != nil {
			log.Warn("agentfield not reachable at startup",
				"url", afClient.BaseURL(),
				"error", err,
			)
		} else {
			log.Info("agentfield healthy", "url", afClient.BaseURL())
		}
	}()

	// Hook engine. Always constructed; modules register against it.
	hookEngine := hooks.NewEngine(log)

	// Optional: object storage. When AF_STACK_S3_ENDPOINT is empty the
	// adapter is skipped — /api/v1/storage/* will return 503 with a
	// STORAGE_NOT_CONFIGURED envelope rather than crashing the runtime.
	var store storage.Storage
	if cfg.Storage.Endpoint != "" {
		store, err = newStorage(cfg.Storage)
		if err != nil {
			log.Error("storage adapter init failed; continuing without storage",
				"adapter", cfg.Storage.Adapter,
				"endpoint", cfg.Storage.Endpoint,
				"error", err,
			)
			store = nil
		} else {
			bootCtx, bootCancel := context.WithTimeout(ctx, 15*time.Second)
			if err := store.EnsureBucket(bootCtx); err != nil {
				log.Error("storage bucket ensure failed; continuing without storage",
					"bucket", cfg.Storage.Bucket,
					"error", err,
				)
				store = nil
			} else {
				log.Info("storage adapter ready",
					"adapter", cfg.Storage.Adapter,
					"endpoint", cfg.Storage.Endpoint,
					"bucket", cfg.Storage.Bucket,
				)
			}
			bootCancel()
		}
	} else {
		log.Info("storage adapter not configured (AF_STACK_S3_ENDPOINT empty); storage endpoints will return 503")
	}

	// Secrets vault. Requires PG (for the suite_secrets table) and a
	// KEK loaded from AF_STACK_KMS_KEY. When either is missing the
	// vault is left nil and /api/v1/secrets/* returns 503 with a
	// SECRETS_NOT_CONFIGURED envelope.
	var vault *secrets.Vault
	if database != nil && database.Pool != nil {
		cipher, kekErr := secrets.LoadKEK(log)
		if kekErr != nil {
			log.Error("secrets: KEK load failed; secrets vault disabled",
				"error", kekErr,
			)
		} else {
			vault, err = secrets.New(database.Pool, cipher, log)
			if err != nil {
				log.Error("secrets: vault init failed", "error", err)
				vault = nil
			} else {
				log.Info("secrets vault ready",
					"key_id", cipher.KeyID(),
					"dev_mode", cipher.DevMode(),
				)
			}
		}
	} else {
		log.Info("secrets vault not configured (no database); /api/v1/secrets/* will return 503")
	}

	// Jobs (River-backed PG queue). Requires DB; when absent the endpoints
	// return tolerant empty responses.
	var jobsManager *jobs.Manager
	if database != nil && database.Pool != nil {
		jobsManager = startJobsManager(ctx, log, database.Pool)
		if jobsManager != nil {
			defer func() {
				stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer stopCancel()
				if err := jobsManager.Stop(stopCtx); err != nil {
					log.Warn("jobs manager stop returned", "error", err)
				}
			}()
		}
	} else {
		log.Info("jobs manager not configured (no database); /api/v1/jobs/* will return empty responses")
	}

	srv := server.New(cfg, log, server.Deps{
		DB:            database,
		AF:            afClient,
		Telemetry:     tel,
		Hooks:         hookEngine,
		Storage:       store,
		StoragePrefix: cfg.Storage.TenantPrefix,
		Secrets:       vault,
		Jobs:          jobsManager,
	})
	if err := srv.Start(ctx); err != nil {
		log.Error("server stopped with error", "error", err)
		os.Exit(1)
	}
	log.Info("af-stack exited cleanly")
}

// newStorage constructs the storage adapter selected by cfg.Adapter.
//
// minio-go speaks vanilla S3, so both "minio" and "s3" share one client.
// The only difference is the TLS default: minio = off (local docker),
// s3 = on (AWS endpoint).
func newStorage(cfg config.StorageConfig) (storage.Storage, error) {
	switch cfg.Adapter {
	case "", "minio":
		return minioadapter.New(minioadapter.Config{
			Endpoint:  cfg.Endpoint,
			Bucket:    cfg.Bucket,
			AccessKey: cfg.AccessKey,
			SecretKey: cfg.SecretKey,
			Region:    cfg.Region,
		})
	case "s3":
		return s3adapter.New(s3adapter.Config{
			Endpoint:  cfg.Endpoint,
			Bucket:    cfg.Bucket,
			AccessKey: cfg.AccessKey,
			SecretKey: cfg.SecretKey,
			Region:    cfg.Region,
		})
	default:
		return nil, fmt.Errorf("storage: unknown adapter %q", cfg.Adapter)
	}
}

// startJobsManager runs the River migrations idempotently, registers built-in
// handlers, and starts the manager. Returns nil if any step fails — callers
// should fall back to "jobs disabled" rather than crash the runtime.
func startJobsManager(ctx context.Context, log *slog.Logger, pool *pgxpool.Pool) *jobs.Manager {
	migCtx, migCancel := context.WithTimeout(ctx, 60*time.Second)
	defer migCancel()
	if err := jobs.MigrateUp(migCtx, pool); err != nil {
		log.Error("river migrations failed; jobs disabled", "error", err)
		return nil
	}
	mgr, err := jobs.NewManager(jobs.Config{Pool: pool, Logger: log})
	if err != nil {
		log.Error("jobs manager init failed", "error", err)
		return nil
	}
	if err := mgr.RegisterSampleJob(); err != nil {
		log.Error("sample job registration failed", "error", err)
		// non-fatal — manager is still usable
	}
	startCtx, startCancel := context.WithTimeout(ctx, 15*time.Second)
	defer startCancel()
	if err := mgr.Start(startCtx, true, 25); err != nil {
		log.Error("jobs manager start failed", "error", err)
		return nil
	}
	log.Info("jobs manager ready", "queue", "default", "max_workers", 25)
	return mgr
}
