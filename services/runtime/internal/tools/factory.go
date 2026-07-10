// SPDX-License-Identifier: Apache-2.0

// factory.go — boot-time wiring of adapters into a Registry.
//
// Reads environment variables to pick which backend implements each
// per-type adapter (browser, search). Single-impl tool types (fs,
// exec, http, sql) don't need a chooser. The factory always returns a
// fully-populated Registry — unconfigured choices show up as stubs.
//
// Env vars
//
//	AF_STACK_TOOL_BROWSER     "browser-use" (default) | "steel" | "playwright"
//	AF_STACK_TOOL_SEARCH      "searxng" (default) | "tavily" | "brave" | "exa" | "duckduckgo"
//	BROWSER_USE_URL           sidecar URL (browser-use adapter)
//	SEARXNG_URL               instance URL (searxng adapter)
//	STEEL_API_KEY             (steel adapter, stub today)
//	PLAYWRIGHT_ENDPOINT       (playwright adapter, stub today)
//	TAVILY_API_KEY            (tavily adapter, stub today)
//	BRAVE_API_KEY             (brave adapter, stub today)
//	EXA_API_KEY               (exa adapter, stub today)
//
// The factory does NOT consume tenant config — per-tenant enable lives
// in the Registry layer above. The factory's job is selecting which
// backend implements each type.
package tools

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/browser"
	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/browser/browseruse"
	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/browser/playwright"
	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/browser/steel"
	execad "github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/exec"
	fsad "github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/fs"
	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/httpfetch"
	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/search"
	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/search/brave"
	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/search/duckduckgo"
	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/search/exa"
	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/search/searxng"
	"github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/search/tavily"
	sqlad "github.com/Agent-Field/backai/services/runtime/internal/tools/adapters/sql"
)

// SandboxRunner is the minimal subset of sandbox.Service the factory
// hands to the fs / exec tool adapters.
type SandboxRunner interface {
	Run(ctx context.Context, spec sandbox.RunSpec) (*sandbox.SandboxRun, error)
}

// FactoryDeps groups the dependencies the factory needs to construct
// adapters. All fields are optional — when omitted, the affected tool
// surfaces as "not configured".
type FactoryDeps struct {
	// Pool is the runtime's pgxpool, used by the SQL tool and by the
	// Registry to read/write `suite_tenant_tools`.
	Pool *pgxpool.Pool
	// Sandbox is the wrapper for the fs / exec tools. When nil, both
	// fs and exec are unconfigured.
	Sandbox SandboxRunner
	// Browser carries browser-tool credentials already resolved by the
	// caller (env overlaid with the dashboard Integrations vault slot).
	// Empty fields fall back to the raw env vars so tests and callers
	// without a vault keep working.
	Browser BrowserCreds
	// Logger for boot diagnostics.
	Logger *slog.Logger
}

// BrowserCreds groups the credentials/endpoints the browser tool's
// adapters consume. All fields optional; empty = not configured.
type BrowserCreds struct {
	BrowserUseURL        string // browser-use sidecar base URL
	SteelAPIKey          string // Steel.dev API key
	BrowserbaseAPIKey    string // Browserbase API key
	BrowserbaseProjectID string // Browserbase project id
	PlaywrightEndpoint   string // raw CDP / playwright websocket endpoint
	AllowPrivate         bool   // permit loopback/RFC-1918 endpoints
}

// withEnvFallback returns creds with empty fields backfilled from the
// process env, preserving the historical env-only construction path.
func (c BrowserCreds) withEnvFallback() BrowserCreds {
	fallback := func(v, env string) string {
		if v != "" {
			return v
		}
		return strings.TrimSpace(os.Getenv(env))
	}
	c.BrowserUseURL = fallback(c.BrowserUseURL, "BROWSER_USE_URL")
	c.SteelAPIKey = fallback(c.SteelAPIKey, "STEEL_API_KEY")
	c.BrowserbaseAPIKey = fallback(c.BrowserbaseAPIKey, "BROWSERBASE_API_KEY")
	c.BrowserbaseProjectID = fallback(c.BrowserbaseProjectID, "BROWSERBASE_PROJECT_ID")
	c.PlaywrightEndpoint = fallback(c.PlaywrightEndpoint, "PLAYWRIGHT_ENDPOINT")
	c.AllowPrivate = c.AllowPrivate || browserAllowPrivate()
	return c
}

// BuildRegistry constructs the runtime's tool Registry: reads env,
// picks adapters per type, registers six Tool entries, and returns
// the populated Registry.
//
// Returns an error when the operator explicitly named an adapter via
// env but the adapter is not configured (e.g. STEEL_API_KEY missing).
// Falls back to the default adapter when the env var is unset.
func BuildRegistry(deps FactoryDeps) (*Registry, error) {
	log := deps.Logger
	if log == nil {
		log = slog.Default()
	}
	r := NewRegistry(deps.Pool, log)

	// Browser
	browserChoice := strings.ToLower(strings.TrimSpace(os.Getenv("AF_STACK_TOOL_BROWSER")))
	if browserChoice == "" {
		browserChoice = "browser-use"
	}
	browserAdapter, err := pickBrowserAdapter(browserChoice, deps.Browser.withEnvFallback())
	if err != nil {
		return nil, err
	}
	r.Register(NewBrowserTool(browserAdapter))
	log.Info("tools: browser adapter selected",
		"adapter", browserAdapter.ID(),
		"configured", browserAdapter.Configured())

	// Search
	searchChoice := strings.ToLower(strings.TrimSpace(os.Getenv("AF_STACK_TOOL_SEARCH")))
	if searchChoice == "" {
		searchChoice = "searxng"
	}
	searchAdapter, err := pickSearchAdapter(searchChoice)
	if err != nil {
		return nil, err
	}
	r.Register(NewSearchTool(searchAdapter))
	log.Info("tools: search adapter selected",
		"adapter", searchAdapter.ID(),
		"configured", searchAdapter.Configured())

	// FS
	fsAdapter := fsad.New(deps.Sandbox)
	r.Register(NewFSTool(fsAdapter))

	// Exec
	execAdapter := execad.New(deps.Sandbox)
	r.Register(NewExecTool(execAdapter))

	// HTTP
	httpAdapter := httpfetch.New(0) // default 30s timeout
	r.Register(NewHTTPTool(httpAdapter))

	// SQL
	sqlAdapter := sqlad.New(deps.Pool)
	r.Register(NewSQLTool(sqlAdapter))

	log.Info("tools: registry built", "count", len(r.tools))
	return r, nil
}

// browserAllowPrivate reports whether the browser-use sidecar may live
// on a loopback / RFC-1918 address (docker-compose service name,
// localhost). Off unless AF_STACK_BROWSER_ALLOW_PRIVATE is truthy,
// mirroring AF_STACK_WEBHOOK_ALLOW_PRIVATE.
func browserAllowPrivate() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("AF_STACK_BROWSER_ALLOW_PRIVATE")))
	return v == "1" || v == "true" || v == "yes"
}

// pickBrowserAdapter returns the BrowserAdapter chosen via env, built
// from the resolved creds (env overlaid with Integrations vault values).
// If the operator explicitly named a non-default adapter (anything other
// than browser-use) that has no credentials, returns an error — we don't
// silently fall back.
func pickBrowserAdapter(choice string, creds BrowserCreds) (browser.Adapter, error) {
	switch choice {
	case "browser-use", "browseruse", "":
		return browseruse.New(creds.BrowserUseURL, creds.AllowPrivate), nil
	case "steel":
		ad := steel.New(creds.SteelAPIKey)
		if creds.SteelAPIKey == "" {
			return ad, fmt.Errorf("tools: AF_STACK_TOOL_BROWSER=steel but no Steel API key (STEEL_API_KEY or Integrations → browser)")
		}
		return ad, nil
	case "playwright":
		ad := playwright.New(creds.PlaywrightEndpoint)
		if creds.PlaywrightEndpoint == "" {
			return ad, fmt.Errorf("tools: AF_STACK_TOOL_BROWSER=playwright but no endpoint (PLAYWRIGHT_ENDPOINT or Integrations → browser)")
		}
		return ad, nil
	default:
		return nil, fmt.Errorf("tools: unknown browser adapter %q", choice)
	}
}

// pickSearchAdapter returns the SearchAdapter chosen via env.
func pickSearchAdapter(choice string) (search.Adapter, error) {
	switch choice {
	case "searxng", "":
		url := strings.TrimSpace(os.Getenv("SEARXNG_URL"))
		return searxng.New(url), nil
	case "tavily":
		key := strings.TrimSpace(os.Getenv("TAVILY_API_KEY"))
		ad := tavily.New(key)
		if key == "" {
			return ad, fmt.Errorf("tools: AF_STACK_TOOL_SEARCH=tavily but TAVILY_API_KEY is empty")
		}
		return ad, nil
	case "brave":
		key := strings.TrimSpace(os.Getenv("BRAVE_API_KEY"))
		ad := brave.New(key)
		if key == "" {
			return ad, fmt.Errorf("tools: AF_STACK_TOOL_SEARCH=brave but BRAVE_API_KEY is empty")
		}
		return ad, nil
	case "exa":
		key := strings.TrimSpace(os.Getenv("EXA_API_KEY"))
		ad := exa.New(key)
		if key == "" {
			return ad, fmt.Errorf("tools: AF_STACK_TOOL_SEARCH=exa but EXA_API_KEY is empty")
		}
		return ad, nil
	case "duckduckgo", "ddg":
		return duckduckgo.New(), nil
	default:
		return nil, fmt.Errorf("tools: unknown search adapter %q", choice)
	}
}

// Compile-time check: our registry type's tools field is map[ToolName]Tool.
var _ Tool = (*BrowserTool)(nil)
var _ Tool = (*SearchTool)(nil)
var _ Tool = (*FSTool)(nil)
var _ Tool = (*ExecTool)(nil)
var _ Tool = (*HTTPTool)(nil)
var _ Tool = (*SQLTool)(nil)

// Sentinel keeping the errors import alive across refactors.
var _ = errors.New
