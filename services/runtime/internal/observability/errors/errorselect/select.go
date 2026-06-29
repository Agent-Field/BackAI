// SPDX-License-Identifier: Apache-2.0

// Package errorselect constructs the active errors.Store from typed config.
package errorselect

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	obserrors "github.com/Agent-Field/backai/services/runtime/internal/observability/errors"
	"github.com/Agent-Field/backai/services/runtime/internal/observability/errors/adapters/glitchtip"
	remoteerrors "github.com/Agent-Field/backai/services/runtime/internal/observability/errors/adapters/remote"
	"github.com/Agent-Field/backai/services/runtime/internal/observability/errors/logfilter"
	"github.com/Agent-Field/backai/services/runtime/internal/observability/logs"
)

func Select(ctx context.Context, cfg config.Config, logStore logs.Store) (obserrors.Store, error) {
	switch Adapter(cfg) {
	case "logfilter":
		return logfilter.New(logStore), nil
	case "glitchtip":
		return glitchtip.New(ctx, glitchtip.Config{
			BaseURL: cfg.Errors.GlitchTip.URL,
			Org:     cfg.Errors.GlitchTip.Org,
			Token:   cfg.Errors.GlitchTip.Token,
			Timeout: 30 * time.Second,
		})
	case "remote":
		return remoteerrors.New(ctx, remoteerrors.Config{
			BaseURL: cfg.Errors.Remote.URL,
			Token:   cfg.Errors.Remote.Token,
			Timeout: 30 * time.Second,
		})
	default:
		return nil, fmt.Errorf("unsupported errors adapter %q", cfg.Errors.Adapter)
	}
}

func Adapter(cfg config.Config) string {
	adapter := strings.ToLower(strings.TrimSpace(cfg.Errors.Adapter))
	if adapter == "" {
		adapter = strings.ToLower(strings.TrimSpace(cfg.Errors.Backend))
	}
	if adapter == "" {
		return "logfilter"
	}
	return adapter
}

func Name(cfg config.Config, store obserrors.Store) string {
	switch Adapter(cfg) {
	case "remote":
		if named, ok := store.(interface{ AdapterName() string }); ok {
			return "remote: " + named.AdapterName()
		}
		return "remote"
	case "glitchtip":
		return "GlitchTip"
	default:
		return "log-filter"
	}
}
