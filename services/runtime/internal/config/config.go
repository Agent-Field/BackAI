// SPDX-License-Identifier: Apache-2.0

// Package config loads and validates AF Stack runtime configuration.
//
// Precedence: env vars > config.yaml > defaults.
// Validation happens at load time; invalid config returns a clear error.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level runtime configuration.
type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Database      DatabaseConfig      `yaml:"database"`
	AgentField    AgentFieldConfig    `yaml:"agentfield"`
	Logging       LoggingConfig       `yaml:"logging"`
	Observability ObservabilityConfig `yaml:"observability"`
	Modules       ModulesConfig       `yaml:"modules"`
	Storage       StorageConfig       `yaml:"storage"`
	Sandbox       SandboxConfig       `yaml:"sandbox"`
	Logs          LogsConfig          `yaml:"logs"`
	Traces        TracesConfig        `yaml:"traces"`
}

// SandboxConfig holds sandbox-adapter settings. The Adapter field picks
// which implementation main.newSandbox() constructs:
//
//   - "docker"       — local Docker daemon (default; Phase 9.1)
//   - "gvisor"       — local Docker daemon w/ runsc runtime (Phase 9.2)
//   - "firecracker"  — micro-VM via Flintlock (Phase 9.2 scaffold)
//   - "e2b"          — e2b.dev hosted sandboxes (Phase 9.2)
//
// E2BAPIKey / E2BBaseURL are only consumed when Adapter="e2b".
type SandboxConfig struct {
	Adapter    string `yaml:"adapter"`
	E2BAPIKey  string `yaml:"e2b_api_key"`
	E2BBaseURL string `yaml:"e2b_base_url"`
}

// LogsConfig selects the runtime log-store adapter. The ring adapter is the
// zero-config default; Loki and remote adapters are activated only when the
// operator sets AF_STACK_LOGS_ADAPTER and the corresponding URL.
type LogsConfig struct {
	Adapter string           `yaml:"adapter"`
	Loki    LogsLokiConfig   `yaml:"loki"`
	Remote  LogsRemoteConfig `yaml:"remote"`
}

type LogsLokiConfig struct {
	URL    string `yaml:"url"`
	Tenant string `yaml:"tenant"`
}

type LogsRemoteConfig struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
}

// TracesConfig selects the runtime trace-store adapter. The empty adapter is
// the zero-config default; Tempo and remote adapters activate only when the
// operator sets AF_STACK_TRACES_ADAPTER and the corresponding URL.
type TracesConfig struct {
	Adapter string             `yaml:"adapter"`
	Tempo   TracesTempoConfig  `yaml:"tempo"`
	Remote  TracesRemoteConfig `yaml:"remote"`
}

type TracesTempoConfig struct {
	URL    string `yaml:"url"`
	Tenant string `yaml:"tenant"`
}

type TracesRemoteConfig struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
}

// StorageConfig holds object-storage settings. Populated from env vars
// `AF_STACK_S3_*`. When Endpoint is empty, the storage adapter is not
// wired and /api/v1/storage/* returns 503.
type StorageConfig struct {
	// Adapter picks the wiring path: "minio" (TLS off by default) or "s3"
	// (TLS on). Both go through the same underlying minio-go client.
	Adapter   string `yaml:"adapter"`
	Endpoint  string `yaml:"endpoint"`
	Bucket    string `yaml:"bucket"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
	Region    string `yaml:"region"`
	// TenantPrefix is prepended to every object key. Phase 5 default is
	// "" — multi-tenancy module not enabled. When enabled, set to
	// "tenants/<id>" per-request (server.Deps will be tenant-scoped).
	TenantPrefix string `yaml:"tenant_prefix"`
}

// ModulesConfig declares which suite modules are enabled and which
// workload (domain-specific) modules are loaded alongside them.
//
// The dashboard reads this via GET /api/v1/modules to render the
// modules page and to decide whether multi-tenancy tabs render real
// content or an "Enable multi-tenancy" empty state.
type ModulesConfig struct {
	// Enabled maps module ID -> on/off. When a module is not present in
	// the map, the runtime falls back to a per-module v1 default (see
	// ModuleState.DefaultEnabled).
	Enabled map[string]bool `yaml:"enabled"`
	// Adapters maps module ID -> adapter implementation choice (e.g.
	// "storage" -> "s3", "secrets-vault" -> "env").
	Adapters map[string]string `yaml:"adapters"`
	// WorkloadModules lists domain modules loaded on top of the core
	// suite (e.g. "agent-commerce", "interior-design").
	WorkloadModules []string `yaml:"workload_modules"`
}

// ServerConfig holds HTTP listener settings.
type ServerConfig struct {
	HTTPAddr        string        `yaml:"http_addr"`
	MetricsAddr     string        `yaml:"metrics_addr"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

// DatabaseConfig holds Postgres connection settings.
type DatabaseConfig struct {
	URL             string        `yaml:"url"`
	MaxConnections  int           `yaml:"max_connections"`
	MaxIdleConns    int           `yaml:"max_idle_connections"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`
}

// AgentFieldConfig holds the AF control plane connection settings.
type AgentFieldConfig struct {
	URL            string        `yaml:"url"`
	HealthTimeout  time.Duration `yaml:"health_timeout"`
	RequestTimeout time.Duration `yaml:"request_timeout"`
}

// LoggingConfig holds log output settings.
type LoggingConfig struct {
	Level  string `yaml:"level"`  // debug|info|warn|error
	Format string `yaml:"format"` // json|text
}

// ObservabilityConfig holds OTel + Prometheus settings.
type ObservabilityConfig struct {
	OTLPEndpoint string `yaml:"otlp_endpoint"`
	ServiceName  string `yaml:"service_name"`
	Enabled      bool   `yaml:"enabled"`
}

// Default returns a Config populated with sensible defaults.
func Default() Config {
	return Config{
		Server: ServerConfig{
			HTTPAddr:        ":8080",
			MetricsAddr:     ":9090",
			ShutdownTimeout: 30 * time.Second,
		},
		Database: DatabaseConfig{
			MaxConnections:  25,
			MaxIdleConns:    5,
			ConnMaxLifetime: 30 * time.Minute,
		},
		AgentField: AgentFieldConfig{
			URL:            "http://agentfield:8081",
			HealthTimeout:  5 * time.Second,
			RequestTimeout: 30 * time.Second,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
		Observability: ObservabilityConfig{
			ServiceName: "af-stack",
			Enabled:     true,
		},
		Storage: StorageConfig{
			Adapter: "minio",
			Region:  "us-east-1",
		},
		Sandbox: SandboxConfig{
			Adapter: "docker",
		},
		Logs: LogsConfig{
			Adapter: "ring",
		},
		Traces: TracesConfig{
			Adapter: "empty",
		},
	}
}

// Load reads a YAML config file from disk and applies env overrides.
//
// If path is empty, only defaults + env overrides are used.
// Returns an error if the file exists but cannot be parsed, or if the
// resulting config fails validation.
func Load(path string) (Config, error) {
	cfg := Default()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return Config{}, fmt.Errorf("read config %s: %w", path, err)
		}
		if err == nil {
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return Config{}, fmt.Errorf("parse config %s: %w", path, err)
			}
		}
	}

	applyEnvOverrides(&cfg)

	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// applyEnvOverrides reads AF_STACK_* env vars and applies them to cfg.
//
// These take precedence over the YAML file.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("AF_STACK_HTTP_ADDR"); v != "" {
		cfg.Server.HTTPAddr = v
	}
	if v := os.Getenv("AF_STACK_METRICS_ADDR"); v != "" {
		cfg.Server.MetricsAddr = v
	}
	if v := os.Getenv("AF_STACK_DATABASE_URL"); v != "" {
		cfg.Database.URL = v
	}
	if v := os.Getenv("AF_STACK_AGENTFIELD_URL"); v != "" {
		cfg.AgentField.URL = v
	}
	// AgentField historically uses AGENTFIELD_SERVER for the URL — accept it
	// so existing AgentField docs work unchanged.
	if v := os.Getenv("AGENTFIELD_SERVER"); v != "" && cfg.AgentField.URL == "" {
		cfg.AgentField.URL = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.Logging.Level = strings.ToLower(v)
	}
	if v := os.Getenv("LOG_FORMAT"); v != "" {
		cfg.Logging.Format = strings.ToLower(v)
	}
	if v := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); v != "" {
		cfg.Observability.OTLPEndpoint = v
	}
	if v := os.Getenv("OTEL_SERVICE_NAME"); v != "" {
		cfg.Observability.ServiceName = v
	}

	// Module enable flags. Per-module env vars override the YAML map so
	// docker-compose can flip modules without a config volume mount. Name
	// pattern: AF_STACK_MODULE_<UPPER_SNAKE> where the suffix is the module
	// id with `-` swapped for `_`. e.g. AF_STACK_MODULE_MULTI_TENANCY=true.
	for _, k := range os.Environ() {
		eq := strings.IndexByte(k, '=')
		if eq <= 0 {
			continue
		}
		name, value := k[:eq], k[eq+1:]
		if !strings.HasPrefix(name, "AF_STACK_MODULE_") {
			continue
		}
		moduleID := strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(name, "AF_STACK_MODULE_"), "_", "-"))
		if moduleID == "" {
			continue
		}
		on := value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
		if cfg.Modules.Enabled == nil {
			cfg.Modules.Enabled = make(map[string]bool)
		}
		cfg.Modules.Enabled[moduleID] = on
	}

	// Storage. AF_STACK_S3_* mirrors the .env.example contract.
	if v := os.Getenv("AF_STACK_S3_ADAPTER"); v != "" {
		cfg.Storage.Adapter = strings.ToLower(v)
	}
	if v := os.Getenv("AF_STACK_S3_ENDPOINT"); v != "" {
		cfg.Storage.Endpoint = v
	}
	if v := os.Getenv("AF_STACK_S3_BUCKET"); v != "" {
		cfg.Storage.Bucket = v
	}
	if v := os.Getenv("AF_STACK_S3_ACCESS_KEY"); v != "" {
		cfg.Storage.AccessKey = v
	}
	if v := os.Getenv("AF_STACK_S3_SECRET_KEY"); v != "" {
		cfg.Storage.SecretKey = v
	}
	if v := os.Getenv("AF_STACK_S3_REGION"); v != "" {
		cfg.Storage.Region = v
	}
	if v := os.Getenv("AF_STACK_S3_TENANT_PREFIX"); v != "" {
		cfg.Storage.TenantPrefix = v
	}

	// Sandbox adapter selection + e2b credentials.
	if v := os.Getenv("AF_STACK_SANDBOX_ADAPTER"); v != "" {
		cfg.Sandbox.Adapter = strings.ToLower(v)
	}
	if v := os.Getenv("E2B_API_KEY"); v != "" {
		cfg.Sandbox.E2BAPIKey = v
	}
	if v := os.Getenv("AF_STACK_E2B_BASE_URL"); v != "" {
		cfg.Sandbox.E2BBaseURL = v
	}

	// Logs adapter slot. Keep selection on _ADAPTER to match the rest of
	// the adapter registry; backend-specific URLs live under the same prefix.
	if v := os.Getenv("AF_STACK_LOGS_ADAPTER"); v != "" {
		cfg.Logs.Adapter = strings.ToLower(v)
	}
	if v := os.Getenv("AF_STACK_LOGS_LOKI_URL"); v != "" {
		cfg.Logs.Loki.URL = v
	}
	if v := os.Getenv("AF_STACK_LOGS_LOKI_TENANT"); v != "" {
		cfg.Logs.Loki.Tenant = v
	}
	if v := os.Getenv("AF_STACK_LOGS_ADAPTER_URL"); v != "" {
		cfg.Logs.Remote.URL = v
	}
	if v := os.Getenv("AF_STACK_LOGS_ADAPTER_TOKEN"); v != "" {
		cfg.Logs.Remote.Token = v
	}

	if v := os.Getenv("AF_STACK_TRACES_ADAPTER"); v != "" {
		cfg.Traces.Adapter = strings.ToLower(v)
	}
	if v := os.Getenv("AF_STACK_TRACES_TEMPO_URL"); v != "" {
		cfg.Traces.Tempo.URL = v
	}
	if v := os.Getenv("AF_STACK_TRACES_TEMPO_TENANT"); v != "" {
		cfg.Traces.Tempo.Tenant = v
	}
	if v := os.Getenv("AF_STACK_TRACES_ADAPTER_URL"); v != "" {
		cfg.Traces.Remote.URL = v
	}
	if v := os.Getenv("AF_STACK_TRACES_ADAPTER_TOKEN"); v != "" {
		cfg.Traces.Remote.Token = v
	}
}

// validate returns an error if the config is in a non-runnable state.
func validate(cfg Config) error {
	if cfg.Server.HTTPAddr == "" {
		return fmt.Errorf("server.http_addr is required")
	}
	if cfg.AgentField.URL == "" {
		return fmt.Errorf("agentfield.url is required (set AGENTFIELD_SERVER or AF_STACK_AGENTFIELD_URL)")
	}
	switch cfg.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("logging.level must be debug|info|warn|error, got %q", cfg.Logging.Level)
	}
	switch cfg.Logging.Format {
	case "json", "text":
	default:
		return fmt.Errorf("logging.format must be json|text, got %q", cfg.Logging.Format)
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Logs.Adapter)) {
	case "", "ring":
	case "loki":
		if strings.TrimSpace(cfg.Logs.Loki.URL) == "" {
			return fmt.Errorf("logs.loki.url is required when logs.adapter=loki (set AF_STACK_LOGS_LOKI_URL)")
		}
	case "remote":
		if strings.TrimSpace(cfg.Logs.Remote.URL) == "" {
			return fmt.Errorf("logs.remote.url is required when logs.adapter=remote (set AF_STACK_LOGS_ADAPTER_URL)")
		}
	default:
		return fmt.Errorf("logs.adapter must be ring|loki|remote, got %q", cfg.Logs.Adapter)
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Traces.Adapter)) {
	case "", "empty":
	case "tempo":
		if strings.TrimSpace(cfg.Traces.Tempo.URL) == "" {
			return fmt.Errorf("traces.tempo.url is required when traces.adapter=tempo (set AF_STACK_TRACES_TEMPO_URL)")
		}
	case "remote":
		if strings.TrimSpace(cfg.Traces.Remote.URL) == "" {
			return fmt.Errorf("traces.remote.url is required when traces.adapter=remote (set AF_STACK_TRACES_ADAPTER_URL)")
		}
	default:
		return fmt.Errorf("traces.adapter must be empty|tempo|remote, got %q", cfg.Traces.Adapter)
	}
	return nil
}
