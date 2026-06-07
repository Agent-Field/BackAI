// mcp_factory.go — wires the stdio / sse adapters into mcp.Pool.
//
// Lives in main because the mcp package itself can't import its own
// adapter sub-packages (those packages depend on mcp for the Adapter
// interface — pulling them back in would be an import cycle). The
// factory is injected at Pool construction time via PoolConfig.Factory.
package main

import (
	"fmt"
	"log/slog"

	"github.com/Agent-Field/backai/services/runtime/internal/mcp"
	"github.com/Agent-Field/backai/services/runtime/internal/mcp/adapters/sse"
	"github.com/Agent-Field/backai/services/runtime/internal/mcp/adapters/stdio"
)

// newMCPAdapterFactory returns the production AdapterFactory.
//
// stdio: spawn the row's Command with resolvedEnv merged on top of a
//        minimal passthrough (PATH, HOME, ...).
// sse  : open <row.url>/sse and route POSTs to <row.url>/messages.
//        resolvedEnv is forwarded as request headers — this is how the
//        dashboard can attach Bearer tokens (env key "Authorization"
//        with value "secret:<key>").
func newMCPAdapterFactory() mcp.AdapterFactory {
	return func(srv mcp.Server, resolvedEnv map[string]string, log *slog.Logger) (mcp.Adapter, error) {
		switch srv.Transport {
		case mcp.TransportStdio:
			if len(srv.Command) == 0 {
				return nil, fmt.Errorf("mcp: stdio row %q has empty command", srv.Name)
			}
			return stdio.New(stdio.Config{
				Name:    srv.Name,
				Command: srv.Command,
				Env:     resolvedEnv,
				Logger:  log,
			}), nil
		case mcp.TransportSSE:
			url := ""
			if srv.URL != nil {
				url = *srv.URL
			}
			if url == "" {
				return nil, fmt.Errorf("mcp: sse row %q has empty url", srv.Name)
			}
			return sse.New(sse.Config{
				Name:    srv.Name,
				URL:     url,
				Headers: resolvedEnv,
				Logger:  log,
			}), nil
		default:
			return nil, fmt.Errorf("mcp: unknown transport %q", srv.Transport)
		}
	}
}
