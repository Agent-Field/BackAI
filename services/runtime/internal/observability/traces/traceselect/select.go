// SPDX-License-Identifier: Apache-2.0

// Package traceselect constructs the active traces.Store from typed config.
package traceselect

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/observability/traces"
	remotetraces "github.com/Agent-Field/backai/services/runtime/internal/observability/traces/adapters/remote"
	tempotraces "github.com/Agent-Field/backai/services/runtime/internal/observability/traces/adapters/tempo"
	"github.com/Agent-Field/backai/services/runtime/internal/observability/traces/empty"
)

func Select(ctx context.Context, cfg config.Config) (traces.Store, error) {
	switch Adapter(cfg) {
	case "empty":
		return empty.New(), nil
	case "tempo":
		return tempotraces.New(ctx, tempotraces.Config{
			BaseURL: cfg.Traces.Tempo.URL,
			Tenant:  cfg.Traces.Tempo.Tenant,
		})
	case "remote":
		return remotetraces.New(ctx, remotetraces.Config{
			BaseURL: cfg.Traces.Remote.URL,
			Token:   cfg.Traces.Remote.Token,
			Timeout: 30 * time.Second,
		})
	default:
		return nil, fmt.Errorf("unsupported traces adapter %q", cfg.Traces.Adapter)
	}
}

func Adapter(cfg config.Config) string {
	adapter := strings.ToLower(strings.TrimSpace(cfg.Traces.Adapter))
	if adapter == "" {
		return "empty"
	}
	return adapter
}

func Name(cfg config.Config, store traces.Store) string {
	switch Adapter(cfg) {
	case "remote":
		if named, ok := store.(interface{ AdapterName() string }); ok {
			return "remote: " + named.AdapterName()
		}
		return "remote"
	case "tempo":
		return "Tempo"
	default:
		return "empty"
	}
}
