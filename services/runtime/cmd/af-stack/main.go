// SPDX-License-Identifier: Apache-2.0

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
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/Agent-Field/backai/services/runtime/internal/activity"
	adapterregistry "github.com/Agent-Field/backai/services/runtime/internal/adapters/registry"
	"github.com/Agent-Field/backai/services/runtime/internal/agentfield"
	"github.com/Agent-Field/backai/services/runtime/internal/approvals"
	"github.com/Agent-Field/backai/services/runtime/internal/billing"
	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/cost"
	"github.com/Agent-Field/backai/services/runtime/internal/crons"
	"github.com/Agent-Field/backai/services/runtime/internal/db"
	"github.com/Agent-Field/backai/services/runtime/internal/dbstudio"
	"github.com/Agent-Field/backai/services/runtime/internal/featureflags"
	"github.com/Agent-Field/backai/services/runtime/internal/guardrails"
	"github.com/Agent-Field/backai/services/runtime/internal/harnesses"
	"github.com/Agent-Field/backai/services/runtime/internal/hooks"
	"github.com/Agent-Field/backai/services/runtime/internal/jobs"
	"github.com/Agent-Field/backai/services/runtime/internal/llmcache"
	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway"
	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway/adapters"
	cartesiaadapter "github.com/Agent-Field/backai/services/runtime/internal/llmgateway/adapters/cartesia"
	elevenlabsadapter "github.com/Agent-Field/backai/services/runtime/internal/llmgateway/adapters/elevenlabs"
	faladapter "github.com/Agent-Field/backai/services/runtime/internal/llmgateway/adapters/fal"
	fluxadapter "github.com/Agent-Field/backai/services/runtime/internal/llmgateway/adapters/flux"
	litellmadapter "github.com/Agent-Field/backai/services/runtime/internal/llmgateway/adapters/litellm"
	"github.com/Agent-Field/backai/services/runtime/internal/logger"
	"github.com/Agent-Field/backai/services/runtime/internal/mcp"
	"github.com/Agent-Field/backai/services/runtime/internal/memory"
	"github.com/Agent-Field/backai/services/runtime/internal/notifications"
	notificationslog "github.com/Agent-Field/backai/services/runtime/internal/notifications/adapters/log"
	notificationsresend "github.com/Agent-Field/backai/services/runtime/internal/notifications/adapters/resend"
	"github.com/Agent-Field/backai/services/runtime/internal/oauth"
	"github.com/Agent-Field/backai/services/runtime/internal/observability"
	"github.com/Agent-Field/backai/services/runtime/internal/probe"
	"github.com/Agent-Field/backai/services/runtime/internal/retention"
	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
	dockersandbox "github.com/Agent-Field/backai/services/runtime/internal/sandbox/adapters/docker"
	e2bsandbox "github.com/Agent-Field/backai/services/runtime/internal/sandbox/adapters/e2b"
	firecrackersandbox "github.com/Agent-Field/backai/services/runtime/internal/sandbox/adapters/firecracker"
	gvisorsandbox "github.com/Agent-Field/backai/services/runtime/internal/sandbox/adapters/gvisor"
	"github.com/Agent-Field/backai/services/runtime/internal/search"
	"github.com/Agent-Field/backai/services/runtime/internal/secrets"
	"github.com/Agent-Field/backai/services/runtime/internal/server"
	"github.com/Agent-Field/backai/services/runtime/internal/shipwright"
	"github.com/Agent-Field/backai/services/runtime/internal/skills"
	"github.com/Agent-Field/backai/services/runtime/internal/storage"
	minioadapter "github.com/Agent-Field/backai/services/runtime/internal/storage/adapters/minio"
	s3adapter "github.com/Agent-Field/backai/services/runtime/internal/storage/adapters/s3"
	"github.com/Agent-Field/backai/services/runtime/internal/tenancy"
	"github.com/Agent-Field/backai/services/runtime/internal/tooladapters"
	"github.com/Agent-Field/backai/services/runtime/internal/tools"
	"github.com/Agent-Field/backai/services/runtime/internal/webhooks"
)

// webhookAgentInvoker bridges the InboundService's AgentInvoker
// contract to the runtime's *agentfield.Client. Lives in main so the
// webhooks package doesn't take an import dependency on agentfield.
type webhookAgentInvoker struct {
	c *agentfield.Client
}

// cronJobEnqueuer bridges the crons.JobEnqueuer contract to the
// runtime's jobs.Manager. Lives in main so the crons package doesn't
// take an import dependency on jobs.
type cronJobEnqueuer struct {
	mgr *jobs.Manager
}

func (c *cronJobEnqueuer) Enqueue(ctx context.Context, name string, args json.RawMessage, tenantID string) error {
	if c == nil || c.mgr == nil {
		return fmt.Errorf("jobs manager not configured")
	}
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	_, err := c.mgr.Enqueue(ctx, name, args, jobs.EnqueueOpts{TenantID: tenantID})
	return err
}

func (w *webhookAgentInvoker) Execute(
	ctx context.Context,
	path string,
	body []byte,
) (webhooks.AgentResponse, error) {
	resp, err := w.c.Execute(ctx, path, body)
	if err != nil {
		return webhooks.AgentResponse{}, err
	}
	return webhooks.AgentResponse{
		StatusCode: resp.StatusCode,
		Body:       resp.Body,
	}, nil
}

const version = "0.0.1"

func caps(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(b)
}

func defaulted(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func staticStatus(status adapterregistry.Status) adapterregistry.Probe {
	return func(context.Context) (adapterregistry.Status, error) {
		return status, nil
	}
}

func buildAdapterRegistry(
	cfg config.Config,
	database *db.DB,
	afClient *agentfield.Client,
	store storage.Storage,
	vault *secrets.Vault,
	jobsManager *jobs.Manager,
	llmGW *llmgateway.Gateway,
	litellmAdmin *llmgateway.LiteLLMAdmin,
	sandboxSvc *sandbox.Service,
	notificationsSvc *notifications.Service,
	webhooksSvc *webhooks.Service,
	billingSvc *billing.Service,
) *adapterregistry.Registry {
	r := adapterregistry.New()

	storageName := defaulted(cfg.Storage.Adapter, "minio")
	storageStatus := adapterregistry.StatusUnhealthy
	storageKind := adapterregistry.KindNone
	storageCaps := map[string]any{"contract_pending": true}
	if store != nil {
		storageKind = adapterregistry.KindBuiltin
		storageStatus = adapterregistry.StatusHealthy
		c := store.Capabilities()
		storageCaps = map[string]any{
			"max_object_size_bytes":     c.MaxObjectSizeBytes,
			"supports_multipart":        c.SupportsMultipart,
			"presign_ttl_max_seconds":   c.PresignTTLMaxSeconds,
			"supports_signed_uploads":   c.PresignTTLMaxSeconds > 0,
			"supports_range_requests":   false,
			"supports_metadata_headers": false,
		}
	}
	r.Register(adapterregistry.Slot{
		ID:               "storage",
		Tier:             adapterregistry.Tier1,
		Kind:             storageKind,
		Name:             storageName,
		Capabilities:     caps(storageCaps),
		AvailableBuiltin: []string{"minio", "s3"},
		SwapMethod:       "env_var",
		SwapEnv:          "AF_STACK_S3_ADAPTER",
		AdminUI:          os.Getenv("AF_STACK_MINIO_CONSOLE_URL"),
		Probe:            staticStatus(storageStatus),
	})

	sandboxName := defaulted(cfg.Sandbox.Adapter, "docker")
	sandboxStatus := adapterregistry.StatusUnhealthy
	sandboxKind := adapterregistry.KindNone
	sandboxCaps := map[string]any{"contract_pending": true}
	if sandboxSvc != nil && sandboxSvc.AdapterAvailable() {
		sandboxKind = adapterregistry.KindBuiltin
		sandboxStatus = adapterregistry.StatusHealthy
		sandboxCaps = map[string]any{
			"max_timeout_s":      sandboxSvc.Capabilities().MaxTimeoutS,
			"supports_gpu":       sandboxSvc.Capabilities().SupportsGPU,
			"supports_network":   sandboxSvc.Capabilities().SupportsNetwork,
			"supports_mounts":    sandboxSvc.Capabilities().SupportsMounts,
			"supports_streaming": true,
			"cold_start_ms":      sandboxSvc.Capabilities().ColdStartMS,
		}
	}
	r.Register(adapterregistry.Slot{
		ID:               "sandbox",
		Tier:             adapterregistry.Tier1,
		Kind:             sandboxKind,
		Name:             sandboxName,
		Capabilities:     caps(sandboxCaps),
		AvailableBuiltin: []string{"docker", "gvisor", "firecracker", "e2b"},
		SwapMethod:       "env_var",
		SwapEnv:          "AF_STACK_SANDBOX_ADAPTER",
		Probe:            staticStatus(sandboxStatus),
	})

	llmName := "unset"
	llmStatus := adapterregistry.StatusUnhealthy
	if llmGW != nil {
		llmName = llmGW.ProviderName()
		llmStatus = adapterregistry.StatusHealthy
	}
	keyMgmtMode := llmgateway.KeyMgmtUnknown
	keyMgmtErr := ""
	virtualKeysActive := false
	if litellmAdmin != nil {
		keyMgmtMode, keyMgmtErr = litellmAdmin.KeyManagement()
		virtualKeysActive = litellmAdmin.VirtualKeysActive()
	}
	llmCaps := map[string]any{
		"supports_chat":        llmGW != nil,
		"supports_embeddings":  llmGW != nil,
		"virtual_keys_active":  virtualKeysActive,
		"key_management_mode":  string(keyMgmtMode),
		"spend_tracking_exact": virtualKeysActive,
		"contract_pending":     true,
	}
	if keyMgmtErr != "" {
		llmCaps["key_management_error"] = keyMgmtErr
	}
	r.Register(adapterregistry.Slot{
		ID:           "llm-chat",
		Tier:         adapterregistry.Tier1,
		Kind:         adapterregistry.KindBuiltin,
		Name:         llmName,
		SwapMethod:   "env_var",
		SwapEnv:      "AF_STACK_LLM_GATEWAY_ADAPTER",
		AdminUI:      os.Getenv("AF_STACK_LITELLM_URL"),
		Capabilities: caps(llmCaps),
		Probe:        staticStatus(llmStatus),
	})

	r.Register(adapterregistry.Slot{
		ID:         "multimodal",
		Tier:       adapterregistry.Tier1,
		Kind:       adapterregistry.KindBuiltin,
		Name:       "first-party + litellm",
		SwapMethod: "env_var",
		SwapEnv:    "AF_STACK_MULTIMODAL_ADAPTER",
		Capabilities: caps(map[string]any{
			"supports_tts":              true,
			"supports_stt":              true,
			"supports_image_generation": true,
			"contract_pending":          true,
		}),
		Probe: staticStatus(llmStatus),
	})

	notificationName := "none"
	notificationKind := adapterregistry.KindNone
	notificationStatus := adapterregistry.StatusUnhealthy
	channels := []string{}
	if notificationsSvc != nil && notificationsSvc.AdapterAvailable() {
		notificationName = notificationsSvc.AdapterName()
		notificationKind = adapterregistry.KindBuiltin
		notificationStatus = adapterregistry.StatusHealthy
		switch notificationName {
		case "resend":
			channels = []string{"email"}
		case "log":
			channels = []string{"log"}
		default:
			channels = []string{notificationName}
		}
	}
	r.Register(adapterregistry.Slot{
		ID:               "notifications",
		Tier:             adapterregistry.Tier1,
		Kind:             notificationKind,
		Name:             notificationName,
		AvailableBuiltin: []string{"log", "resend"},
		SwapMethod:       "env_var",
		SwapEnv:          "AF_STACK_NOTIFICATIONS_ADAPTER",
		Capabilities: caps(map[string]any{
			"channels":                      channels,
			"supports_html":                 notificationName == "resend",
			"supports_templates":            false,
			"supports_cc_bcc":               false,
			"tracks_delivery_status":        false,
			"supports_retry":                false,
			"supports_metadata_passthrough": true,
			"contract_pending":              true,
		}),
		Probe: staticStatus(notificationStatus),
	})

	billingName := "none"
	billingStatus := adapterregistry.StatusUnhealthy
	if billingSvc != nil {
		billingName = billingSvc.AdapterName()
		billingStatus = adapterregistry.StatusHealthy
	}
	r.Register(adapterregistry.Slot{
		ID:               "billing",
		Tier:             adapterregistry.Tier1,
		Kind:             adapterregistry.KindBuiltin,
		Name:             billingName,
		AvailableBuiltin: []string{"stripe", "lago", "none"},
		SwapMethod:       "env_var",
		SwapEnv:          "AF_STACK_BILLING_ADAPTER",
		Capabilities: caps(map[string]any{
			"supports_customers":            billingSvc != nil,
			"supports_customer_portal":      billingSvc != nil && billingName != "none",
			"supports_metered_billing":      billingSvc != nil,
			"supports_webhook_verification": billingSvc != nil && billingName == "stripe",
			"contract_pending":              true,
		}),
		Probe: staticStatus(billingStatus),
	})

	secretsStatus := adapterregistry.StatusUnhealthy
	secretsKind := adapterregistry.KindNone
	if vault != nil {
		secretsStatus = adapterregistry.StatusHealthy
		secretsKind = adapterregistry.KindBuiltin
	}
	r.Register(adapterregistry.Slot{
		ID:         "secrets",
		Tier:       adapterregistry.Tier1,
		Kind:       secretsKind,
		Name:       "envelope-local",
		SwapMethod: "env_var",
		SwapEnv:    "AF_STACK_SECRETS_ADAPTER",
		Capabilities: caps(map[string]any{
			"supports_rotation":  vault != nil,
			"supports_reveal":    vault != nil,
			"supports_metadata":  vault != nil,
			"audit_log_revealed": true,
			"kms_backend":        defaulted(os.Getenv("AF_STACK_KMS_PROVIDER"), "local"),
			"contract_pending":   true,
		}),
		Probe: staticStatus(secretsStatus),
	})

	r.Register(adapterregistry.Slot{
		ID:         "auth",
		Tier:       adapterregistry.Tier1,
		Kind:       adapterregistry.KindBuiltin,
		Name:       "better-auth",
		SwapMethod: "env_var",
		SwapEnv:    "AF_STACK_AUTH_ADAPTER",
		Capabilities: caps(map[string]any{
			"supports_oauth_providers":     []string{"google", "github", "microsoft"},
			"supports_magic_links":         true,
			"supports_passwordless":        false,
			"supports_mfa":                 false,
			"supports_sso":                 false,
			"supports_token_introspection": true,
			"supports_session_enumeration": false,
			"contract_pending":             true,
		}),
		Probe: staticStatus(adapterregistry.StatusHealthy),
	})

	databaseStatus := adapterregistry.StatusUnhealthy
	if database != nil && database.Pool != nil {
		databaseStatus = adapterregistry.StatusHealthy
	}
	r.Register(adapterregistry.Slot{
		ID:         "database",
		Tier:       adapterregistry.Tier2,
		Kind:       adapterregistry.KindBuiltin,
		Name:       "postgres",
		SwapMethod: "connection_string",
		SwapEnv:    "AF_STACK_DATABASE_URL",
		Capabilities: caps(map[string]any{
			"supports_pgvector": true,
			"supports_rls":      true,
		}),
		Probe: staticStatus(databaseStatus),
	})

	reasoningStatus := adapterregistry.StatusUnhealthy
	if afClient != nil {
		reasoningStatus = adapterregistry.StatusHealthy
	}
	r.Register(adapterregistry.Slot{
		ID:         "reasoning",
		Tier:       adapterregistry.Tier3,
		Kind:       adapterregistry.KindBuiltin,
		Name:       "agentfield",
		SwapMethod: "none",
		AdminUI:    os.Getenv("AF_STACK_AGENTFIELD_URL"),
		Capabilities: caps(map[string]any{
			"supports_runs":         afClient != nil,
			"supports_capabilities": afClient != nil,
		}),
		Probe: staticStatus(reasoningStatus),
	})

	queueStatus := adapterregistry.StatusUnhealthy
	if jobsManager != nil {
		queueStatus = adapterregistry.StatusHealthy
	}
	r.Register(adapterregistry.Slot{
		ID:         "job-queue",
		Tier:       adapterregistry.Tier3,
		Kind:       adapterregistry.KindBuiltin,
		Name:       "river",
		SwapMethod: "none",
		Capabilities: caps(map[string]any{
			"supports_retries":       jobsManager != nil,
			"supports_cron_dispatch": jobsManager != nil,
		}),
		Probe: staticStatus(queueStatus),
	})

	webhooksStatus := adapterregistry.StatusUnhealthy
	if webhooksSvc != nil && webhooksSvc.Configured() {
		webhooksStatus = adapterregistry.StatusHealthy
	}
	r.Register(adapterregistry.Slot{
		ID:         "webhooks",
		Tier:       adapterregistry.Tier3,
		Kind:       adapterregistry.KindBuiltin,
		Name:       "svix",
		SwapMethod: "env_var",
		SwapEnv:    "AF_STACK_SVIX_URL",
		AdminUI:    os.Getenv("AF_STACK_SVIX_URL"),
		Capabilities: caps(map[string]any{
			"supports_outbound": webhooksSvc != nil && webhooksSvc.Configured(),
			"supports_inbound":  webhooksSvc != nil,
		}),
		Probe: staticStatus(webhooksStatus),
	})

	return r
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			fmt.Printf("af-stack %s\n", version)
			return
		case "harness":
			runHarnessCmd(os.Args[2:])
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
	featureCfg, featureIssues, featureErr := config.LoadFeatureConfig(config.DefaultFeatureConfigPath)
	if featureErr != nil {
		lean, _ := config.PresetFeatures(config.PresetLean)
		featureCfg = config.FeatureConfig{Preset: config.PresetLean, Features: lean}
		featureIssues = []config.ValidationError{{
			Feature:     "backai.config",
			Level:       config.ValidationErrorLevel,
			Message:     featureErr.Error(),
			Remediation: "Fix backai.config.yaml or copy backai.config.yaml.example.",
		}}
	}

	// Log ring buffer + slog wiring. The ring tees every structured log
	// record into a fixed-capacity in-memory buffer the dashboard's
	// /operate/logs tab reads from. Strictly process-local; multi-process
	// deployments need a real aggregator.
	logRing := logger.NewRing(logger.RingSize, "af-stack")
	log := logger.NewWithRing(cfg.Logging.Level, cfg.Logging.Format, logRing)
	log.Info("af-stack starting",
		"version", version,
		"http_addr", cfg.Server.HTTPAddr,
		"agentfield_url", cfg.AgentField.URL,
	)

	// Phase 14.3 graceful shutdown.
	//
	// We use TWO contexts here so the shutdown sequence can be
	// orchestrated explicitly:
	//
	//   workersCtx — cancelled to stop background workers (notifications,
	//                webhooks, crons, MCP refresh, llmcache eviction).
	//                Cancelled by the SIGTERM handler AFTER the HTTP
	//                drain completes, so workers don't go away while
	//                in-flight requests are still hitting them.
	//   sigCtx     — driven by signal.NotifyContext; we observe it via
	//                a separate goroutine that runs the drain sequence
	//                end-to-end and then cancels workersCtx.
	//
	// rootCtx (alias of workersCtx) is what gets passed into the
	// startup probes + worker goroutines below; the shutdown handler
	// below is the only thing that cancels it.
	sigCtx, sigCancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer sigCancel()
	workersCtx, workersCancel := context.WithCancel(context.Background())
	defer workersCancel()
	ctx := workersCtx

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
			statsCtx, statsCancel := context.WithTimeout(ctx, 5*time.Second)
			warnIfPGStatsRoleMissing(statsCtx, database.Pool, log)
			statsCancel()
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

	// Sandbox adapter (Phase 9). Constructed unconditionally; init
	// failures are non-fatal — the runtime keeps booting and the
	// sandbox endpoints surface 503 SANDBOX_NOT_CONFIGURED.
	//
	// For the docker adapter we additionally Ping the daemon: a
	// machine without a reachable Docker socket leaves sb=nil so the
	// dashboard renders "Sandbox module disabled" rather than failing
	// every Run call at the first ContainerCreate.
	sb, sbErr := newSandbox(cfg.Sandbox, store, log)
	if sbErr != nil {
		log.Error("sandbox adapter init failed; continuing without sandbox",
			"adapter", cfg.Sandbox.Adapter,
			"error", sbErr,
		)
		sb = nil
	}
	if sb != nil {
		if dockerAdapter, ok := sb.(*dockersandbox.Adapter); ok {
			pingCtx, pingCancel := context.WithTimeout(ctx, 3*time.Second)
			if pingErr := dockerAdapter.Ping(pingCtx); pingErr != nil {
				log.Warn("sandbox: docker daemon not reachable; sandbox disabled",
					"error", pingErr)
				sb = nil
			}
			pingCancel()
		}
	}
	if sb != nil {
		caps := sb.Capabilities()
		log.Info("sandbox adapter ready",
			"adapter", caps.Adapter,
			"max_timeout_s", caps.MaxTimeoutS,
			"supports_gpu", caps.SupportsGPU,
			"cold_start_ms", caps.ColdStartMS,
		)
	}

	// Secrets vault. Requires PG (for the suite_secrets table) and a
	// cipher loaded from AF_STACK_KMS_PROVIDER. The default provider is
	// env, which preserves the AF_STACK_KMS_KEY path; cloud providers
	// unwrap a data key through AWS/GCP/Azure KMS at boot. When either
	// database or cipher setup is missing, /api/v1/secrets/* returns
	// 503 with a SECRETS_NOT_CONFIGURED envelope.
	var vault *secrets.Vault
	if database != nil && database.Pool != nil {
		cipher, kekErr := secrets.LoadCipher(ctx, log)
		if kekErr != nil {
			log.Error("secrets: KMS load failed; secrets vault disabled",
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

	// Rate limiting is enforced upstream by LiteLLM via per-virtual-key
	// rpm_limit / tpm_limit on each suite_api_keys row (issued through
	// tenancy.IssueAPIKey, see internal/llmgateway/litellm_admin.go +
	// internal/tenancy/litellm_mirror.go). The runtime no longer runs a
	// local token-bucket — 429s flow back from LiteLLM with the standard
	// Retry-After / X-RateLimit-* headers and the LLM handler proxies
	// them through to the client.

	// LiteLLM admin client — talks to the master-key-protected /key/* +
	// /spend/* surface on the same sidecar the gateway calls. At boot we
	// probe key management once and cache the mode: stateless LiteLLM is
	// supported for chat routing, while virtual-key mirroring activates
	// only when LiteLLM reports a connected key DB.
	litellmAdmin := buildLiteLLMAdmin(log)
	probeReg := probe.NewRegistry(log)
	litellmURL, litellmMasterKey := litellmEnv()
	probeReg.Register(probe.NewLiteLLMVirtualKeysProbe(litellmURL, litellmMasterKey, 5*time.Minute))
	probeReg.Register(probe.NewLiteLLMSpendTrackingProbe(litellmURL, litellmMasterKey, 5*time.Minute))
	if database != nil && database.Pool != nil {
		probeReg.Register(&probe.PGStatStatementsProbe{Pool: database.Pool, Interval: 5 * time.Minute})
		probeReg.Register(&probe.PGRoleReadAllStatsProbe{Pool: database.Pool, Interval: 5 * time.Minute})
	}
	litellmAdmin.WithProbeRegistry(probeReg)
	{
		probeCtx, probeCancel := context.WithTimeout(ctx, 10*time.Second)
		probeReg.RunAll(probeCtx)
		probeCancel()
		mode, probeErr := litellmAdmin.KeyManagement()
		if probeErr != "" {
			log.Warn("litellm key-management probe failed",
				"mode", mode,
				"virtual_keys_active", litellmAdmin.VirtualKeysActive(),
				"error", probeErr)
		} else {
			log.Info("litellm key-management probe complete",
				"mode", mode,
				"virtual_keys_active", litellmAdmin.VirtualKeysActive())
		}
		go probeReg.StartScheduled(ctx)
	}
	if database != nil && database.Pool != nil {
		if poller := llmgateway.NewProviderHealthPoller(database.Pool, litellmURL, litellmMasterKey, log); poller != nil && poller.Configured() {
			go poller.Run(ctx, 5*time.Minute)
			log.Info("llm provider health poller started", "interval", "5m")
		}
		retentionReg := retention.NewRegistry()
		retentionReg.Register(retention.Policy{
			Table:       "suite_provider_health_log",
			RetainDays:  30,
			OrderColumn: "observed_at",
			BatchSize:   1000,
		})
		sqlDB := stdlib.OpenDBFromPool(database.Pool)
		systemCrons := crons.NewSystemScheduler(log)
		if err := systemCrons.RegisterSystem("retention.daily", "0 3 * * *", func(runCtx context.Context) error {
			retentionCtx, cancel := context.WithTimeout(runCtx, 5*time.Minute)
			defer cancel()
			report, err := retentionReg.Run(retentionCtx, sqlDB)
			if err != nil {
				return err
			}
			if report.RowsDeleted > 0 || report.RowsRolledUp > 0 {
				log.Info("retention run complete",
					"table", report.Table,
					"rows_deleted", report.RowsDeleted,
					"rows_rolled_up", report.RowsRolledUp,
					"duration", report.Duration)
			}
			return nil
		}); err != nil {
			log.Warn("retention system cron registration failed", "error", err)
		} else {
			go systemCrons.Run(ctx)
			go func() {
				if err := systemCrons.RunNow(ctx, "retention.daily"); err != nil {
					log.Warn("retention startup run failed", "error", err)
				}
			}()
		}
	}

	// Tenancy manager — required when the multi-tenancy module is enabled,
	// optional otherwise (admin endpoints already gate on the flag).
	//
	// WithLiteLLM hands the mirror + secrets sink to the manager so
	// IssueAPIKey can mint a LiteLLM virtual key per AF Stack api key
	// and stash the LiteLLM secret in the vault. Both are no-ops when
	// the underlying admin / vault isn't configured.
	var tenancyMgr *tenancy.Manager
	if database != nil && database.Pool != nil {
		var tenErr error
		tenancyMgr, tenErr = tenancy.New(database.Pool, hookEngine, log)
		if tenErr != nil {
			log.Error("tenancy manager init failed", "error", tenErr)
		} else {
			var sink tenancy.SecretSink
			if vault != nil {
				sink = tenancySecretSink{v: vault}
			}
			tenancyMgr.WithLiteLLM(llmgateway.NewTenancyMirror(litellmAdmin), sink)
			log.Info("tenancy manager ready",
				"multi_tenancy_enabled", cfg.Modules.Enabled["multi-tenancy"],
				"litellm_mirror", litellmAdmin.Configured(),
				"litellm_virtual_keys_active", litellmAdmin.VirtualKeysActive(),
				"litellm_secrets_sink", sink != nil,
			)
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

	// LLM gateway (Phase 7.1). Customers point the OpenAI SDK at
	// /api/v1/llm; the runtime forwards each call to a LiteLLM Proxy
	// sidecar (image ghcr.io/berriai/litellm:main-stable) which fans
	// out to 100+ providers based on apps/backend/litellm-config.yaml.
	// AF Stack keeps tenant resolution, cost ledger, budgets, cache,
	// and pre/post-call hooks on this side.
	llmGW := buildLLMGateway(log, afClient)

	// Gateway guardrails (Phase 2 completeness). This runs at the AF
	// Stack LLM gateway boundary only: request/response text can be
	// redacted or blocked before it leaves/enters the app, while
	// AgentField keeps ownership of AI-stateful runs, spans, traces,
	// memory, and tool-call history.
	guardrailsSvc, guardrailsErr := buildGuardrails(log)
	if guardrailsErr != nil {
		log.Error("guardrails: config error", "error", guardrailsErr)
		os.Exit(1)
	}

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
		// LiteLLM is the source of truth for cumulative spend (item #22).
		// Hand the admin client to the aggregator so the dashboard reads
		// live totals from /spend/keys; the suite_cost_events sum is now
		// audit-only.
		if litellmAdmin.VirtualKeysActive() {
			costAggregate.WithLiteLLM(llmgateway.NewCostSpendReader(litellmAdmin))
		}
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

	// App-data search (Phase 2 completeness). This indexes product and
	// workload-module records, not AgentField memory/runs/traces. FTS
	// works with only Postgres; vector/hybrid mode uses the same LLM
	// gateway embedder when provider credentials are configured.
	var searchStore *search.Store
	if database != nil && database.Pool != nil {
		var embedder search.Embedder
		if llmGW != nil {
			embedder = memory.NewLLMGatewayEmbedder(llmGW, "")
		}
		ss, searchErr := search.New(database.Pool, embedder, log)
		if searchErr != nil {
			log.Error("search: init failed", "error", searchErr)
		} else {
			searchStore = ss
			log.Info("search store ready",
				"embedder", embedder != nil,
				"model", memory.DefaultEmbeddingModel,
			)
		}
	} else {
		log.Info("search store not configured (no database); /api/v1/search will return 503")
	}

	// User activity log (Phase 2 completeness). This is product/customer
	// activity for the app built on the stack, separate from admin audit
	// and AgentField state.
	var activityStore *activity.Store
	if database != nil && database.Pool != nil {
		as, activityErr := activity.New(database.Pool, log)
		if activityErr != nil {
			log.Error("activity: init failed", "error", activityErr)
		} else {
			activityStore = as
			log.Info("activity store ready")
		}
	} else {
		log.Info("activity store not configured (no database); /api/v1/activity will return 503")
	}

	// Feature flags (Phase 2 completeness). GET can fall back to built-in
	// defaults without a store; writes require the DB-backed store.
	var featureFlagStore *featureflags.Store
	if database != nil && database.Pool != nil {
		fs, flagErr := featureflags.New(database.Pool, log)
		if flagErr != nil {
			log.Error("feature flags: init failed", "error", flagErr)
		} else {
			featureFlagStore = fs
			log.Info("feature flag store ready")
		}
	} else {
		log.Info("feature flag store not configured (no database); /api/v1/config/flags writes will return 503")
	}

	// Shipwright (Phase 3 Tier 1). The runtime stores customer task /
	// patch metadata only; AgentField owns the coding-agent execution
	// state, harness tool calls, logs, spans, traces, and memory.
	var shipwrightStore *shipwright.Store
	if database != nil && database.Pool != nil {
		shipwrightStore = shipwright.NewWithPool(database.Pool)
		log.Info("shipwright store ready",
			"agent_call", strings.TrimSpace(os.Getenv("AF_STACK_SHIPWRIGHT_AGENT_CALL")))
	} else {
		log.Info("shipwright store not configured (no database); /api/v1/shipwright/* will return 503")
	}

	var approvalsStore *approvals.Store
	if database != nil && database.Pool != nil {
		approvalsStore = approvals.NewWithPool(database.Pool)
		log.Info("approvals store ready")
	} else {
		log.Info("approvals store not configured (no database); /api/v1/approvals/* will return 503")
	}

	// Sandbox service composes adapter + recorder (DB) + cost recorder.
	// adapter==nil leaves sandboxSvc=nil; the server tolerates a missing
	// sandbox service across all endpoints (reads degrade to empty
	// pages; mutating endpoints return 503).
	var sandboxSvc *sandbox.Service
	if sb != nil {
		var sandboxRecorder *sandbox.Recorder
		if database != nil && database.Pool != nil {
			sandboxRecorder = sandbox.NewRecorder(database.Pool, log)
		}
		sandboxSvc = sandbox.NewService(sb, sandboxRecorder, costRecorder, log)
	}

	// Built-in agent tool adapters (Phase 2 completeness). The service
	// stores tenant enablement/config in AF Stack and dispatches
	// stateless calls through existing primitives. AgentField remains
	// the owner of tool-call spans/traces inside agent runs.
	var toolAdapterSvc *tooladapters.Service
	if database != nil && database.Pool != nil {
		searxngURL := strings.TrimSpace(os.Getenv("AF_STACK_SEARXNG_URL"))
		browserUseURL := strings.TrimSpace(os.Getenv("AF_STACK_BROWSER_USE_URL"))
		toolAdapterSvc = tooladapters.New(database.Pool, sandboxSvc, tooladapters.Config{
			SearXNGURL:    searxngURL,
			BrowserUseURL: browserUseURL,
		}, log)
		log.Info("tool adapters ready",
			"searxng_configured", searxngURL != "",
			"browser_use_configured", browserUseURL != "",
			"sandbox_configured", sandboxSvc != nil,
		)
	} else {
		log.Info("tool adapters not configured (no database); /api/v1/tools/* will return empty/503")
	}

	// Strict-interface native tool registry (#16 — internal/tools/).
	// Sits alongside the legacy tooladapters service. The factory reads
	// env (AF_STACK_TOOL_BROWSER, AF_STACK_TOOL_SEARCH, BROWSER_USE_URL,
	// SEARXNG_URL, STEEL_API_KEY, etc.) to pick adapters; per-tenant
	// enable lives in suite_tenant_tools.
	var toolsRegistry *tools.Registry
	{
		var pool *pgxpool.Pool
		if database != nil {
			pool = database.Pool
		}
		reg, regErr := tools.BuildRegistry(tools.FactoryDeps{
			Pool:    pool,
			Sandbox: sandboxSvc,
			Logger:  log,
		})
		if regErr != nil {
			log.Error("native tools: registry build failed", "error", regErr)
		} else {
			toolsRegistry = reg
			log.Info("native tools ready", "count", len(reg.All()))
		}
	}

	// OAuth-on-behalf-of-user (Phase 2 completeness). OAuth grants are
	// AF Stack app-auth state: the DB stores metadata/refs, while token
	// bytes live in the encrypted secrets vault. Agents retrieve tokens
	// through the gateway when they need to call third-party APIs as a
	// user; AgentField still owns AI run state.
	oauthFactory := oauth.NewFactoryFromEnv()
	var oauthManager *oauth.Manager
	if database != nil && database.Pool != nil && vault != nil {
		om, oauthErr := oauth.NewManager(database.Pool, vault, log)
		if oauthErr != nil {
			log.Error("oauth: manager init failed", "error", oauthErr)
		} else {
			oauthManager = om
			log.Info("oauth: manager ready", "providers", len(oauthFactory.List()))
		}
	} else {
		log.Info("oauth: not configured (requires database + secrets vault); /api/v1/oauth token operations return 503")
	}

	// Notifications (Phase 10.1). The adapter choice comes from
	// AF_STACK_NOTIFICATIONS_ADAPTER (default "log"). When the chosen
	// adapter can't be constructed (e.g. resend with no API key) we
	// fall back to the log adapter rather than crash — operators see
	// "log" in the dashboard and can flip the env var to switch.
	var notificationsSvc *notifications.Service
	{
		adapter := buildNotificationsAdapter(log)
		if database != nil && database.Pool != nil {
			notificationsSvc = notifications.NewService(database.Pool, adapter, log)
		} else {
			notificationsSvc = notifications.NewService(nil, adapter, log)
			log.Info("notifications: running without DB; rows will not persist")
		}
		// Worker drains queued rows every 2s. It no-ops gracefully when
		// the adapter or DB are missing — Run() reports its own state.
		worker := notifications.NewWorker(notificationsSvc, log)
		go worker.Run(ctx)
	}

	// Webhooks (Phase 10.2 inbound + Svix-backed outbound). The facade
	// composes:
	//   - DeliveryStore  : INBOUND deliveries only. The legacy outbound
	//                      rows were dropped in migration 00015 when we
	//                      moved outbound to Svix.
	//   - EndpointStore  : Phase 10.2 inbound endpoint CRUD.
	//   - InboundService : verifies HMAC, dedups, forwards to the
	//                      endpoint's forward_to target (http(s)://... or
	//                      af://agents/<name>).
	//   - OutboundService: thin proxy to the Svix sidecar. Talks to
	//                      AF_STACK_SVIX_URL. Returns 503 when the URL
	//                      is unset.
	//
	// A nil DB pool leaves every inbound store at HasPool()==false; the
	// server surfaces 503 / empty pages accordingly. The outbound side
	// is independent of the DB — it only needs the Svix URL.
	var webhooksSvc *webhooks.Service
	{
		var (
			deliveryStore *webhooks.DeliveryStore
			endpointStore *webhooks.EndpointStore
			inboundSvc    *webhooks.InboundService
		)
		if database != nil && database.Pool != nil {
			deliveryStore = webhooks.NewDeliveryStore(database.Pool, log)
			endpointStore = webhooks.NewEndpointStore(database.Pool, log)
			// InboundService needs the secrets vault for HMAC verify
			// and the agentfield client for af://agents/... forwards.
			// vault / afClient may be nil — InboundService logs a
			// warning when a secret-ref endpoint is hit without a
			// vault and refuses af:// forwards without an agents
			// client.
			var (
				vaultRef  webhooks.SecretLookup
				agentsRef webhooks.AgentInvoker
			)
			if vault != nil {
				vaultRef = vault
			}
			if afClient != nil {
				agentsRef = &webhookAgentInvoker{c: afClient}
			}
			inboundSvc = webhooks.NewInboundService(webhooks.InboundDeps{
				Endpoints:  endpointStore,
				Deliveries: deliveryStore,
				Vault:      vaultRef,
				Agents:     agentsRef,
				Logger:     log,
			})
		} else {
			log.Info("webhooks: no database; /webhooks/in/* + endpoint CRUD will return 503")
		}

		// Outbound: thin Svix proxy. Config is read from env; if
		// AF_STACK_SVIX_URL is unset the service stays unconfigured and
		// /api/v1/webhooks/send returns 503.
		outboundSvc := webhooks.NewOutboundService(deliveryStore, log)
		if outboundSvc.Configured() {
			log.Info("webhooks: outbound proxy wired to svix",
				"svix_url", os.Getenv("AF_STACK_SVIX_URL"))
		} else {
			log.Info("webhooks: AF_STACK_SVIX_URL unset; /api/v1/webhooks/send will return 503")
		}

		webhooksSvc = webhooks.NewService(deliveryStore, outboundSvc, endpointStore, inboundSvc, log)
	}

	// Billing (Phase 10.4). Composes:
	//   - billing.Store  : reads/writes suite_billing_customers + suite_usage_meters.
	//   - billing.Client : selected by AF_STACK_BILLING_ADAPTER
	//                      (stripe | lago | none), with deterministic
	//                      stubs when provider credentials are unset.
	//   - billing.Service: per-tenant verbs (meter, has_budget, portal_link)
	//                      + the Stripe webhook dispatcher when active.
	//
	// nil DB pool leaves billing.Store at HasPool()==false; reads serve
	// empty / synthesised rows, writes silently drop. Stub adapters always
	// return deterministic placeholder values so the dashboard renders
	// something useful even without real provider keys.
	var billingSvc *billing.Service
	{
		var billingStore *billing.Store
		if database != nil && database.Pool != nil {
			billingStore = billing.NewStore(database.Pool, log)
		}
		billingClient := billing.NewClientFromEnv(log)
		billingSvc = billing.NewService(billingStore, billingClient, billing.NewMeterRegistry(), log)
	}

	// Skills (Phase 11.3). The Store + Installer are independent: the
	// Store needs a DB (no pool → mutating endpoints return 503 and
	// reads return an empty list), the Installer is stateless and is
	// always constructed so the install handler can parse + read
	// manifests even if persistence later fails.
	var skillsStore *skills.Store
	if database != nil && database.Pool != nil {
		skillsStore = skills.NewStore(database.Pool, log)
		log.Info("skills: store ready")
	} else {
		log.Info("skills: store not configured (no database); /api/v1/skills mutations will return 503")
	}
	skillsInstaller := skills.NewInstaller()

	// Harnesses (Phase 11.4). Probe-only — the runtime checks whether
	// the four supported CLI harnesses (Claude Code, Codex, Gemini,
	// OpenCode) are installed and reachable, then caches the result in
	// memory. We warm the cache once at boot so the dashboard's first
	// list render is instant; the user re-runs probes via the
	// dashboard's per-card Probe button.
	// CapabilitiesProvider (Phase 17 refactor): harness availability comes
	// from the AF agent layer. The agentfield client implements
	// FetchAgentCapabilities so the prober aggregates across agents
	// instead of probing the runtime's own PATH.
	var harnessCapProvider harnesses.CapabilitiesProvider
	if afClient != nil {
		harnessCapProvider = afClient
	}
	harnessesSvc := harnesses.NewService(harnessCapProvider, log)
	{
		warmCtx, warmCancel := context.WithTimeout(ctx, 30*time.Second)
		_ = harnessesSvc.ListAll(warmCtx)
		warmCancel()
		log.Info("harnesses: probe cache warmed",
			"providers", len(harnesses.AllProviders))
	}

	// MCP (Phase 11.1). Requires DB for the suite_mcp_servers table
	// (no pool ⇒ Pool.Configured() reports false; mutating endpoints
	// surface 503). Pool.Reconcile() at boot opens connections for
	// every enabled row, and a background goroutine refreshes the tool
	// catalogue every 5 minutes. The secrets vault is passed through
	// as a SecretReader so env values prefixed "secret:<key>" resolve
	// before any stdio child is spawned.
	var mcpStore *mcp.Store
	var mcpPool *mcp.Pool
	if database != nil && database.Pool != nil {
		mcpStore = mcp.NewStore(database.Pool, log)
		var secretReader mcp.SecretReader
		if vault != nil {
			secretReader = vault
		}
		mcpPool = mcp.NewPool(mcpStore, secretReader, mcp.PoolConfig{
			Logger:  log,
			Factory: newMCPAdapterFactory(),
		})
		reconCtx, reconCancel := context.WithTimeout(ctx, 30*time.Second)
		if err := mcpPool.Reconcile(reconCtx); err != nil {
			log.Warn("mcp: initial reconcile failed; pool still running",
				"error", err)
		}
		reconCancel()
		go mcpPool.RunRefreshLoop(ctx)
		defer mcpPool.Shutdown()
		log.Info("mcp: pool ready", "refresh", "5m")
	} else {
		log.Info("mcp: not configured (no database); /api/v1/mcp/* mutations will return 503")
	}

	// Crons (Phase 12.2). The Store reads/writes suite_crons; the
	// Scheduler ticks once a minute and dispatches due rows via the
	// jobs manager. We construct both unconditionally so the dashboard
	// still renders the empty state when no DB / no jobs are wired.
	var cronsStore *crons.Store
	if database != nil && database.Pool != nil {
		cronsStore = crons.NewStore(database.Pool, log)
		log.Info("crons: store ready")
		if jobsManager != nil {
			scheduler := crons.NewScheduler(crons.SchedulerConfig{
				Store:    cronsStore,
				Enqueuer: &cronJobEnqueuer{mgr: jobsManager},
				Logger:   log,
			})
			if scheduler != nil {
				go scheduler.Run(ctx)
			}
		} else {
			log.Info("crons: scheduler not started (no jobs manager); CRUD still works")
		}
	} else {
		log.Info("crons: store not configured (no database); /api/v1/crons mutations will return 503")
	}

	adapterRegistry := buildAdapterRegistry(
		cfg,
		database,
		afClient,
		store,
		vault,
		jobsManager,
		llmGW,
		litellmAdmin,
		sandboxSvc,
		notificationsSvc,
		webhooksSvc,
		billingSvc,
	)
	probeReg.WithAdapterRegistry(adapterRegistry)

	srv := server.New(cfg, log, server.Deps{
		DB:              database,
		AF:              afClient,
		Telemetry:       tel,
		Hooks:           hookEngine,
		Storage:         store,
		StoragePrefix:   cfg.Storage.TenantPrefix,
		Secrets:         vault,
		Jobs:            jobsManager,
		Tenancy:         tenancyMgr,
		LLMCache:        llmResponseCache,
		LLMGateway:      llmGW,
		Guardrails:      guardrailsSvc,
		DBStudio:        studioSvc,
		Memory:          memoryStore,
		Search:          searchStore,
		Activity:        activityStore,
		FeatureFlags:    featureFlagStore,
		CostAggregate:   costAggregate,
		Budgets:         costBudgets,
		Sandbox:         sandboxSvc,
		Notifications:   notificationsSvc,
		Webhooks:        webhooksSvc,
		Billing:         billingSvc,
		Skills:          skillsStore,
		SkillsInstaller: skillsInstaller,
		Harnesses:       harnessesSvc,
		MCP:             mcpPool,
		MCPStore:        mcpStore,
		Crons:           cronsStore,
		ToolAdapters:    toolAdapterSvc,
		AdapterRegistry: adapterRegistry,
		ProbeRegistry:   probeReg,
		FeatureConfig:   featureCfg,
		FeatureWarnings: featureIssues,
		ToolsRegistry:   toolsRegistry,
		OAuthManager:    oauthManager,
		OAuthFactory:    oauthFactory,
		Shipwright:      shipwrightStore,
		Approvals:       approvalsStore,
		LogRing:         logRing,
		Version:         version,
	})

	// Phase 14.3: ordered graceful shutdown.
	//
	// Listener runs on its own goroutine so the main goroutine can
	// orchestrate the multi-step shutdown without racing the listener.
	//
	// Sequence on SIGTERM/SIGINT:
	//   1. drain.Start() — /ready flips to 503, new requests rejected
	//      with 503 + Connection: close. Existing requests run to
	//      completion or until drain timeout.
	//   2. httpServer.Shutdown(ctx) — stop accepting new connections,
	//      close idle ones, wait for active conns to finish.
	//   3. Cancel workersCtx — every background worker (notifications,
	//      crons scheduler, MCP refresh, llmcache eviction) returns from
	//      its select-on-ctx.Done() loop. Outbound webhook delivery is
	//      owned by the Svix sidecar (OSS-AUDIT #2).
	//   4. mcpPool.Shutdown() — close every external MCP adapter
	//      connection (stdio child processes, SSE clients).
	//   5. jobs manager Stop() / DB Close / storage.Close — handled by
	//      their deferred close functions registered above.
	//
	// drain timeout is taken from cfg.Server.ShutdownTimeout (default
	// 30s; overridable via AF_STACK_SHUTDOWN_TIMEOUT). Steps 2-5 share
	// the same budget — if step 1 burns the full window, steps 2-5
	// proceed with whatever remains (at minimum 1s each).
	drainTimeout := cfg.Server.ShutdownTimeout
	if drainTimeout <= 0 {
		drainTimeout = 30 * time.Second
	}

	// Start listener.
	listenerErr := make(chan error, 1)
	go func() {
		log.Info("http server listening", "addr", cfg.Server.HTTPAddr)
		err := srv.HTTPServer().ListenAndServe()
		if err != nil && err.Error() != "http: Server closed" {
			listenerErr <- err
			return
		}
		listenerErr <- nil
	}()

	// Mark ready — at this point all migrations applied, workers
	// spawned, listener up. /ready now returns 200.
	srv.MarkReady()
	log.Info("af-stack ready", "drain_timeout", drainTimeout)

	// Wait for either listener error or signal.
	select {
	case err := <-listenerErr:
		if err != nil {
			log.Error("http listener failed", "error", err)
			workersCancel()
			os.Exit(1)
		}
	case <-sigCtx.Done():
		log.Info("draining mode entered", "signal_received", true)

		// Step 1: drain HTTP.
		drainCtx, drainCancel := context.WithTimeout(context.Background(), drainTimeout)
		drainErr := srv.Drain().Start(drainCtx)
		drainCancel()
		if drainErr != nil {
			log.Warn("drain timed out with in-flight requests; proceeding",
				"active_requests", srv.Drain().Active(),
				"error", drainErr,
			)
		} else {
			log.Info("drain complete; all in-flight requests finished",
				"active_requests", srv.Drain().Active())
		}

		// Step 2: http server shutdown — stops accepting new
		// connections, closes idle ones, gives active conns up to
		// the ctx budget to finish writing their response.
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), drainTimeout)
		if err := srv.HTTPServer().Shutdown(shutdownCtx); err != nil {
			log.Warn("http shutdown returned error", "error", err)
		}
		shutdownCancel()

		// Step 3: stop background workers via context cancel.
		// Workers read ctx.Done() in their select loops and exit
		// promptly (notifications: next tick; crons: next minute;
		// MCP refresh: next tick; llmcache eviction: next 5min tick
		// — but the goroutine itself returns from ctx.Done
		// immediately). Outbound webhook delivery is owned by the Svix
		// sidecar (OSS-AUDIT #2), so there's no AF Stack worker to
		// drain on the outbound path.
		log.Info("stopping background workers")
		workersCancel()

		// Step 4: explicit MCP pool close — Shutdown() is NOT
		// driven by context cancel; it tears down adapter
		// connections + stdio child processes. Safe to call after
		// workersCancel since the refresh loop has already
		// returned.
		if mcpPool != nil {
			log.Info("closing MCP adapter connections")
			mcpPool.Shutdown()
		}

		// Step 5: jobs / DB / storage / observability close via
		// their deferred functions registered above. We exit
		// normally here so the defers fire in reverse order:
		//   - defer mcpPool.Shutdown (already called above; idempotent)
		//   - defer jobsManager.Stop
		//   - defer database.Close
		//   - defer tel.Shutdown
		// storage adapter has no Close() in the storage.Storage
		// interface; nothing to do at the runtime level.

		log.Info("shutdown complete")
		return
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

// newSandbox constructs the sandbox adapter selected by cfg.Adapter.
//
// Returns (nil, nil) when the e2b adapter is selected but the API key is
// missing — that's a known "not configured" state that should leave the
// sandbox endpoints serving 503, not crash the runtime. Any other error
// (unknown adapter name, etc.) is fatal at the caller's discretion.
//
// See docs/sandbox-adapters.md for the per-adapter trade-offs.
func newSandbox(cfg config.SandboxConfig, store storage.Storage, log *slog.Logger) (sandbox.Sandbox, error) {
	switch cfg.Adapter {
	case "", "docker":
		return dockersandbox.New(dockersandbox.Config{
			Storage: store,
			Logger:  log,
		})
	case "gvisor":
		return gvisorsandbox.New(gvisorsandbox.Config{})
	case "firecracker":
		return firecrackersandbox.New(firecrackersandbox.Config{})
	case "e2b":
		if cfg.E2BAPIKey == "" {
			// Not configured rather than misconfigured; surface a clear
			// nil result so the caller logs "not configured" rather than
			// "init failed".
			return nil, nil
		}
		return e2bsandbox.New(e2bsandbox.Config{
			APIKey:  cfg.E2BAPIKey,
			BaseURL: cfg.E2BBaseURL,
		})
	default:
		return nil, fmt.Errorf("sandbox: unknown adapter %q (want docker|gvisor|firecracker|e2b)", cfg.Adapter)
	}
}

// buildNotificationsAdapter picks the configured notifications adapter
// from env. Defaults to the log adapter, which is infallible and the
// right choice for dev/CI. The Resend adapter requires RESEND_API_KEY.
//
// When AF_STACK_NOTIFICATIONS_ADAPTER selects an adapter that can't be
// constructed (e.g. resend with no key), we log a warning and fall
// back to the log adapter — better to keep the dashboard's tab live
// than crash the runtime.
func buildNotificationsAdapter(log *slog.Logger) notifications.Adapter {
	choice := strings.ToLower(strings.TrimSpace(os.Getenv("AF_STACK_NOTIFICATIONS_ADAPTER")))
	switch choice {
	case "", "log":
		log.Info("notifications: adapter=log (dev default; set AF_STACK_NOTIFICATIONS_ADAPTER=resend to switch)")
		return notificationslog.New(log)
	case "resend":
		key := strings.TrimSpace(os.Getenv("RESEND_API_KEY"))
		if key == "" {
			log.Warn("notifications: adapter=resend selected but RESEND_API_KEY empty; falling back to log adapter")
			return notificationslog.New(log)
		}
		adapter, err := notificationsresend.New(notificationsresend.Config{
			APIKey:      key,
			DefaultFrom: os.Getenv("AF_STACK_NOTIFICATIONS_FROM"),
		})
		if err != nil {
			log.Warn("notifications: adapter=resend init failed; falling back to log adapter", "error", err)
			return notificationslog.New(log)
		}
		log.Info("notifications: adapter=resend")
		return adapter
	default:
		log.Warn("notifications: unknown adapter; falling back to log",
			"choice", choice)
		return notificationslog.New(log)
	}
}

// buildGuardrails constructs the gateway-local PII/moderation layer.
//
// Defaults:
//   - enabled
//   - regex PII redaction
//   - moderation enabled, with operator-supplied block regexes
//
// Presidio wiring:
//   - AF_STACK_PII_PROVIDER=presidio
//   - AF_STACK_PRESIDIO_ANALYZER_URL=http://presidio-analyzer:3000
//   - AF_STACK_PRESIDIO_ANONYMIZER_URL=http://presidio-anonymizer:3000
func buildGuardrails(log *slog.Logger) (*guardrails.Service, error) {
	if envBool("AF_STACK_GUARDRAILS_ENABLED", true) == false {
		log.Info("guardrails: disabled by AF_STACK_GUARDRAILS_ENABLED=false")
		return nil, nil
	}
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("AF_STACK_PII_PROVIDER")))
	if provider == "" {
		provider = guardrails.ProviderRegex
	}
	svc, err := guardrails.New(guardrails.Config{
		Enabled:               true,
		RedactPII:             envBool("AF_STACK_PII_REDACTION_ENABLED", true),
		Moderate:              envBool("AF_STACK_MODERATION_ENABLED", true),
		Provider:              provider,
		PresidioAnalyzerURL:   os.Getenv("AF_STACK_PRESIDIO_ANALYZER_URL"),
		PresidioAnonymizerURL: os.Getenv("AF_STACK_PRESIDIO_ANONYMIZER_URL"),
		BlockPatterns:         splitEnvList(os.Getenv("AF_STACK_MODERATION_BLOCK_PATTERNS")),
		Timeout:               3 * time.Second,
	}, log)
	if err != nil {
		return nil, err
	}
	log.Info("guardrails: ready",
		"provider", svc.Provider(),
		"pii_redaction", envBool("AF_STACK_PII_REDACTION_ENABLED", true),
		"moderation", envBool("AF_STACK_MODERATION_ENABLED", true),
		"moderation_rules", len(splitEnvList(os.Getenv("AF_STACK_MODERATION_BLOCK_PATTERNS"))),
	)
	return svc, nil
}

func envBool(key string, def bool) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if raw == "" {
		return def
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func splitEnvList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n'
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}

// buildLLMGateway constructs the LLM gateway.
//
// The runtime no longer hand-rolls a client per upstream provider —
// it forwards every /api/v1/llm/* call to a LiteLLM Proxy sidecar
// (image `ghcr.io/berriai/litellm:main-stable`) which handles 100+
// providers (OpenRouter, OpenAI, Anthropic, Google, Mistral, DeepSeek,
// Groq, Cohere, Bedrock, ...) via its standard config.
//
// AF Stack keeps tenant resolution, the cost ledger, budgets, cache,
// and hooks on its side; LiteLLM is purely upstream routing.
//
// Wiring:
//   - AF_STACK_LITELLM_URL — sidecar URL (default http://litellm:4000
//     for docker-compose; set to http://localhost:4000 for bare-metal).
//   - LITELLM_MASTER_KEY  — internal sidecar bearer token shared with
//     the LiteLLM container at compose-time; customers never see it.
//     Defaults to "sk-litellm-dev" so a fresh checkout boots without
//     extra config — override in production.
//
// afClient is kept in the signature for forward-compat (future
// AF-native fallback) but is no longer used by the gateway itself.
func buildLLMGateway(log *slog.Logger, afClient *agentfield.Client) *llmgateway.Gateway {
	_ = afClient // reserved for forward-compat

	demoMode := strings.ToLower(strings.TrimSpace(os.Getenv("AF_STACK_DEMO_MODE")))
	if strings.EqualFold(strings.TrimSpace(os.Getenv("AF_STACK_LLM_PROVIDER")), "demo") ||
		demoMode == "true" ||
		(demoMode == "auto" && !hasLLMProviderKey()) {
		log.Info("llm gateway: demo provider enabled")
		return llmgateway.New(llmgateway.NewDemoProvider())
	}

	url, masterKey := litellmEnv()

	log.Info("llm gateway: litellm sidecar", "url", url)
	provider := llmgateway.NewLiteLLMProvider(llmgateway.LiteLLMConfig{
		ProviderID: "litellm",
		BaseURL:    url,
		MasterKey:  masterKey,
	})
	gateway := llmgateway.New(provider)
	multimodal := buildMultimodal(log, provider)
	if multimodal != nil {
		gateway = gateway.WithMultimodal(multimodal)
	}
	return gateway
}

func hasLLMProviderKey() bool {
	keys := []string{
		"OPENROUTER_API_KEY",
		"OPENAI_API_KEY",
		"ANTHROPIC_API_KEY",
		"GEMINI_API_KEY",
		"MISTRAL_API_KEY",
		"DEEPSEEK_API_KEY",
		"GROQ_API_KEY",
	}
	for _, key := range keys {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
}

// buildMultimodal assembles the first-party multimodal adapter registry
// + the LiteLLM fallback. Each first-party adapter checks its provider
// key at construction; missing key → adapter excluded → models that
// route to it return a clear 503 ("configure ELEVENLABS_API_KEY", etc.).
func buildMultimodal(log *slog.Logger, provider *llmgateway.LiteLLMProvider) *llmgateway.Multimodal {
	// First-party adapters. nil entries are silently skipped by the
	// registry so the order here doubles as the routing priority.
	registry := adapters.NewRegistry(
		elevenlabsadapter.NewFromEnv(),
		cartesiaadapter.NewFromEnv(),
		fluxadapter.NewFromEnv(),
		faladapter.NewFromEnv(),
	)
	// Log which first-party providers are live so the operator sees the
	// status at boot.
	for _, name := range registry.Names() {
		log.Info("multimodal adapter ready", "adapter", name)
	}
	// LiteLLM fallback always present (provider is non-nil here).
	fallback := litellmadapter.New(provider)
	mm := llmgateway.NewMultimodal(registry, fallback)
	log.Info("multimodal facade ready",
		"first_party_adapters", len(registry.Names()),
		"fallback", fallback.Name())
	return mm
}

// buildLiteLLMAdmin constructs the LiteLLM admin client used by tenancy
// (item #22 — virtual keys). It points at the same sidecar as the
// gateway but talks to the master-key-protected /key/* + /spend/*
// surface. Returns an admin instance even when env isn't set — the
// instance's Configured() reports false and callers skip mirroring.
func buildLiteLLMAdmin(log *slog.Logger) *llmgateway.LiteLLMAdmin {
	url, masterKey := litellmEnv()
	admin := llmgateway.NewLiteLLMAdmin(url, masterKey)
	if admin.Configured() {
		log.Info("litellm admin client ready", "url", url)
	} else {
		log.Info("litellm admin client not configured; tenancy will skip virtual-key mirroring")
	}
	return admin
}

// litellmEnv reads the sidecar URL + master key with the same defaults
// as the legacy gateway helper. Kept in one place so buildLLMGateway
// and buildLiteLLMAdmin can never drift.
func litellmEnv() (url, masterKey string) {
	url = os.Getenv("AF_STACK_LITELLM_URL")
	if url == "" {
		url = "http://litellm:4000"
	}
	masterKey = os.Getenv("LITELLM_MASTER_KEY")
	if masterKey == "" {
		masterKey = "sk-litellm-dev"
	}
	return url, masterKey
}

func warnIfPGStatsRoleMissing(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) {
	if pool == nil {
		return
	}
	var ok bool
	if err := pool.QueryRow(ctx, `
		select coalesce(pg_has_role(current_user, 'pg_read_all_stats', 'member'), false)
	`).Scan(&ok); err != nil {
		log.Warn("postgres stats visibility probe failed", "error", err)
		return
	}
	if !ok {
		log.Warn("postgres stats visibility is reduced; grant pg_read_all_stats to the runtime DB role for complete DB health and search-index stats")
	}
}

// tenancySecretSink adapts *secrets.Vault to the tenancy.SecretSink
// interface. The tenancy package can't import secrets directly without
// reversing the dep arrow (secrets is a leaf); this adapter is the
// minimum-surface bridge. Used by item #22 to store the LiteLLM secret
// alongside each suite_api_keys row.
type tenancySecretSink struct{ v *secrets.Vault }

// Put stores the LiteLLM secret under (tenantID, key).
func (s tenancySecretSink) Put(ctx context.Context, tenantID, key string, in tenancy.SecretPutInput) error {
	if s.v == nil {
		return fmt.Errorf("secrets vault not configured")
	}
	_, err := s.v.Put(ctx, tenantID, key, secrets.PutInput{
		Value:       in.Value,
		Description: in.Description,
	})
	return err
}

// Delete removes the secret. Best-effort: a missing row returns nil so
// the caller can treat re-revoke as idempotent.
func (s tenancySecretSink) Delete(ctx context.Context, tenantID, key string) error {
	if s.v == nil {
		return nil
	}
	return s.v.Delete(ctx, tenantID, key)
}

// Get returns the decrypted secret. Used by the LLM gateway at request
// time to pick the per-tenant LiteLLM key (falls back to master key
// when this returns empty).
func (s tenancySecretSink) Get(ctx context.Context, tenantID, key string) ([]byte, error) {
	if s.v == nil {
		return nil, fmt.Errorf("secrets vault not configured")
	}
	return s.v.Get(ctx, tenantID, key)
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
