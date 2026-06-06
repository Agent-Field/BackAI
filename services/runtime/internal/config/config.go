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
	Server        ServerConfig         `yaml:"server"`
	Database      DatabaseConfig       `yaml:"database"`
	AgentField    AgentFieldConfig     `yaml:"agentfield"`
	Logging       LoggingConfig        `yaml:"logging"`
	Observability ObservabilityConfig  `yaml:"observability"`
	Modules       ModulesConfig        `yaml:"modules"`
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
	URL             string `yaml:"url"`
	MaxConnections  int    `yaml:"max_connections"`
	MaxIdleConns    int    `yaml:"max_idle_connections"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`
}

// AgentFieldConfig holds the AF control plane connection settings.
type AgentFieldConfig struct {
	URL           string        `yaml:"url"`
	HealthTimeout time.Duration `yaml:"health_timeout"`
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
	return nil
}
