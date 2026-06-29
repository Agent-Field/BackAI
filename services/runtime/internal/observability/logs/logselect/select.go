// SPDX-License-Identifier: Apache-2.0

// Package logselect constructs the active logs.Store from typed config.
package logselect

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/logger"
	"github.com/Agent-Field/backai/services/runtime/internal/observability/logs"
	lokilogs "github.com/Agent-Field/backai/services/runtime/internal/observability/logs/adapters/loki"
	remotelogs "github.com/Agent-Field/backai/services/runtime/internal/observability/logs/adapters/remote"
	ringlogs "github.com/Agent-Field/backai/services/runtime/internal/observability/logs/ring"
)

func Select(ctx context.Context, cfg config.Config, ring *logger.Ring) (logs.Store, error) {
	switch Adapter(cfg) {
	case "ring":
		return ringlogs.New(ring), nil
	case "loki":
		return lokilogs.New(ctx, lokilogs.Config{
			BaseURL: cfg.Logs.Loki.URL,
			Tenant:  cfg.Logs.Loki.Tenant,
		})
	case "remote":
		return remotelogs.New(ctx, remotelogs.Config{
			BaseURL: cfg.Logs.Remote.URL,
			Token:   cfg.Logs.Remote.Token,
			Timeout: 30 * time.Second,
		})
	default:
		return nil, fmt.Errorf("unsupported logs adapter %q", cfg.Logs.Adapter)
	}
}

func Adapter(cfg config.Config) string {
	adapter := strings.ToLower(strings.TrimSpace(cfg.Logs.Adapter))
	if adapter == "" {
		return "ring"
	}
	return adapter
}

func Name(cfg config.Config, store logs.Store) string {
	switch Adapter(cfg) {
	case "remote":
		if named, ok := store.(interface{ AdapterName() string }); ok {
			return "remote: " + named.AdapterName()
		}
		return "remote"
	case "loki":
		return "loki"
	default:
		return "ring buffer"
	}
}
