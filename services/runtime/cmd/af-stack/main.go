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
	"github.com/Agent-Field/backai/services/runtime/internal/cost"
	"github.com/Agent-Field/backai/services/runtime/internal/db"
	"github.com/Agent-Field/backai/services/runtime/internal/dbstudio"
	"github.com/Agent-Field/backai/services/runtime/internal/hooks"
	"github.com/Agent-Field/backai/services/runtime/internal/jobs"
	"github.com/Agent-Field/backai/services/runtime/internal/llmcache"
	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway"
	"github.com/Agent-Field/backai/services/runtime/internal/logger"
	"github.com/Agent-Field/backai/services/runtime/internal/memory"
	"github.com/Agent-Field/backai/services/runtime/internal/observability"
	"github.com/Agent-Field/backai/services/runtime/internal/ratelimit"
	"github.com/Agent-Field/backai/services/runtime/internal/secrets"
	"github.com/Agent-Field/backai/services/runtime/internal/server"
	"github.com/Agent-Field/backai/services/runtime/internal/storage"
	minioadapter "github.com/Agent-Field/backai/services/runtime/internal/storage/adapters/minio"
	s3adapter "github.com/Agent-Field/backai/services/runtime/internal/storage/adapters/s3"
	"github.com/Agent-Field/backai/services/runtime/internal/tenancy"
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

	// Rate limiter. Always constructed (in-process, no external deps).
	// When DB is available, wire a QuotaSource that reads
	// suite_tenants.quota->>'rpm' so per-tenant overrides apply. The
	// closure swallows DB errors and returns (0, false) so a slow query
	// can never block an inbound request.
	var quotas ratelimit.QuotaSource
	if database != nil && database.Pool != nil {
		pool := database.Pool
		quotas = func(ctx context.Context, tenantID string) (int, bool) {
			if tenantID == "" {
				return 0, false
			}
			lookupCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
			defer cancel()
			var rpm int
			err := pool.QueryRow(lookupCtx, `
				select coalesce(nullif((quota->>'rpm'), '')::int, 0)
				from suite_tenants where id = $1
			`, tenantID).Scan(&rpm)
			if err != nil || rpm <= 0 {
				return 0, false
			}
			return rpm, true
		}
	}
	limiterOpts := []ratelimit.Option{}
	if quotas != nil {
		limiterOpts = append(limiterOpts, ratelimit.WithQuotaSource(quotas))
	}
	limiter := ratelimit.NewInMemory(limiterOpts...)
	log.Info("rate limiter ready", "default_rpm", ratelimit.DefaultRPM)

	// Tenancy manager — required when the multi-tenancy module is enabled,
	// optional otherwise (admin endpoints already gate on the flag).
	var tenancyMgr *tenancy.Manager
	if database != nil && database.Pool != nil {
		var tenErr error
		tenancyMgr, tenErr = tenancy.New(database.Pool, hookEngine, log)
		if tenErr != nil {
			log.Error("tenancy manager init failed", "error", tenErr)
		} else {
			log.Info("tenancy manager ready",
				"multi_tenancy_enabled", cfg.Modules.Enabled["multi-tenancy"])
		}
	}

	// LLM response cache (Phase 7.3). Requires DB; when absent the cache
	// is left nil and the gateway treats every call as a miss. When wired,
	// also spawns a background eviction goroutine on a 5-minute tick so
	// expired entries don't accumulate.
	var llmResponseCache *llmcache.Cache
	if database != nil && database.Pool != nil {
		llmResponseCache, err = llmcache.New(database.Pool, log)
		if err != nil {
			log.Error("llmcache: init failed; LLM cache disabled", "error", err)
			llmResponseCache = nil
		} else {
			go llmResponseCache.RunEviction(ctx, 5*time.Minute)
			log.Info("llmcache: ready", "eviction_interval", "5m")
		}
	} else {
		log.Info("llmcache not configured (no database); LLM gateway will not cache")
	}

	// LLM gateway (Phase 7.1). Routes every /api/v1/llm/* call through
	// AgentField in spirit, but the docker-compose AF instance does not
	// yet expose a built-in `__llm.chat` reasoner — so the MVP path
	// proxies directly to an OpenAI-compatible upstream (OpenRouter by
	// default, OpenAI/Anthropic when their key is set). The public
	// contract still says "routes through AF"; swap to
	// llmgateway.NewAFProvider once AF ships the built-in handler.
	llmGW := buildLLMGateway(log, afClient)

	// Cost ledger + budgets (Phase 7). The Recorder + Budgets are
	// constructed unconditionally so the hook handlers can be
	// registered against the hooks.Engine even when no DB is wired —
	// in that case each method short-circuits to a permissive no-op.
	//
	// Hook registration translates the LLM gateway's payload (a
	// map[string]any) into Recorder.Record / Budgets.HasBudget calls.
	// The gateway (Phase 7.1) lives in a separate package and fires
	// the hooks; this file only supplies the handlers.
	var (
		costRecorder  *cost.Recorder
		costBudgets   *cost.Budgets
		costAggregate *cost.Aggregate
	)
	if database != nil && database.Pool != nil {
		costRecorder = cost.NewRecorder(database.Pool, log)
		costBudgets = cost.NewBudgets(database.Pool, log)
		costAggregate = cost.NewAggregate(database.Pool, log)
		if hookEngine != nil {
			if err := hookEngine.Register(hooks.HookLLMPreCall, cost.PreCallHandler(costBudgets)); err != nil {
				log.Error("cost: pre-call hook registration failed", "error", err)
			}
			if err := hookEngine.Register(hooks.HookLLMPostCall, cost.PostCallHandler(costRecorder)); err != nil {
				log.Error("cost: post-call hook registration failed", "error", err)
			} else {
				log.Info("cost: ledger + budget hooks registered")
			}
		}
	} else {
		log.Info("cost ledger not configured (no database); per-call cost will not be tracked")
	}

	// DB studio (Phase 8.1). The Studio is a read-mostly wrapper over
	// the same pgxpool used by the rest of the runtime; we hand it the
	// pool directly. nil pool -> nil Studio -> /api/v1/db/* returns
	// 503 DB_STUDIO_NOT_CONFIGURED.
	var studioSvc *dbstudio.Studio
	if database != nil && database.Pool != nil {
		studioSvc = dbstudio.New(database.Pool)
	}

	// Suite memory (Phase 8.2). Requires a DB; the embedder is
	// optional — when the LLM gateway has a provider key wired we
	// route embeddings through it, otherwise memory still works for
	// non-vector operations (Get / Put without Embed=true / Delete /
	// List). Search calls 503 when no embedder is configured.
	var memoryStore *memory.Store
	if database != nil && database.Pool != nil {
		var embedder memory.Embedder
		if llmGW != nil {
			embedder = memory.NewLLMGatewayEmbedder(llmGW, "")
		}
		ms, memErr := memory.New(database.Pool, embedder, log)
		if memErr != nil {
			log.Error("memory: init failed", "error", memErr)
		} else {
			memoryStore = ms
			log.Info("memory store ready",
				"embedder", embedder != nil,
				"model", memory.DefaultEmbeddingModel,
			)
		}
	} else {
		log.Info("memory store not configured (no database); /api/v1/memory/* will return 503")
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
		RateLimiter:   limiter,
		Tenancy:       tenancyMgr,
		LLMCache:      llmResponseCache,
		LLMGateway:    llmGW,
		DBStudio:      studioSvc,
		Memory:        memoryStore,
		CostAggregate: costAggregate,
		Budgets:       costBudgets,
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

// buildLLMGateway constructs the Phase-7 LLM gateway.
//
// The public contract says every LLM call routes through AgentField.
// In practice the docker-compose AF instance does not yet expose a
// built-in OpenAI-compat reasoner, so the MVP path is a direct
// upstream proxy. We pick the first provider whose API key is set
// from the environment, preferring OpenRouter (broadest model
// catalogue at lowest cost) when multiple keys are present.
//
// When NO key is set the gateway is still constructed against
// OpenRouter so /api/v1/llm/models returns the static catalog —
// only actual completion calls will fail with a clear
// "no provider key configured" upstream error.
//
// Switch to llmgateway.NewAFProvider(afClient, llmgateway.AFProviderConfig{})
// once AgentField ships the `__llm.chat` reasoner.
func buildLLMGateway(log *slog.Logger, afClient *agentfield.Client) *llmgateway.Gateway {
	// Suppress unused warning until we switch to AF-routed mode.
	_ = afClient

	openrouterKey := os.Getenv("OPENROUTER_API_KEY")
	openaiKey := os.Getenv("OPENAI_API_KEY")
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")

	switch {
	case openrouterKey != "":
		log.Info("llmgateway: provider=openrouter (Phase-7 MVP path; AF-native routing pending)")
		return llmgateway.New(llmgateway.NewOpenAICompatProvider(llmgateway.OpenAICompatConfig{
			ProviderID: "openrouter",
			BaseURL:    "https://openrouter.ai/api/v1",
			APIKey:     openrouterKey,
			ExtraHeaders: map[string]string{
				"HTTP-Referer": "https://github.com/Agent-Field/backai",
				"X-Title":      "AF Stack",
			},
		}))
	case openaiKey != "":
		log.Info("llmgateway: provider=openai (Phase-7 MVP path)")
		return llmgateway.New(llmgateway.NewOpenAICompatProvider(llmgateway.OpenAICompatConfig{
			ProviderID: "openai",
			BaseURL:    "https://api.openai.com/v1",
			APIKey:     openaiKey,
		}))
	case anthropicKey != "":
		// Anthropic's OpenAI-compat endpoint lives at /v1.
		log.Info("llmgateway: provider=anthropic (Phase-7 MVP path)")
		return llmgateway.New(llmgateway.NewOpenAICompatProvider(llmgateway.OpenAICompatConfig{
			ProviderID: "anthropic",
			BaseURL:    "https://api.anthropic.com/v1",
			APIKey:     anthropicKey,
		}))
	default:
		log.Warn("llmgateway: no provider API key found in env " +
			"(OPENROUTER_API_KEY, OPENAI_API_KEY, ANTHROPIC_API_KEY); " +
			"models endpoint will still work, but chat/embeddings/images will fail upstream")
		return llmgateway.New(llmgateway.NewOpenAICompatProvider(llmgateway.OpenAICompatConfig{
			ProviderID: "openrouter",
			BaseURL:    "https://openrouter.ai/api/v1",
		}))
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
