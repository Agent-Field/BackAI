// SPDX-License-Identifier: Apache-2.0

// af-stack — the AF Stack operator CLI.
//
// The CLI is a thin wrapper around the runtime REST API. It exists so
// operators can poke the same surface the dashboard uses, without
// reaching for curl + jq.
//
// Currently shipped subcommands:
//
//	af-stack init my-app                              Scaffold a new app on the stack
//	af-stack init --brand --name "DocuChat"           Re-theme a fork (power-user path)
//	af-stack dev                                      Start local compose dev loop
//	af-stack agent new <name>                         Scaffold an AgentField agent
//	af-stack module new <id>                          Scaffold a workload module
//	af-stack plugin new <id>                          Scaffold a dashboard plugin
//	af-stack adapter list                             Show active adapter choices
//	af-stack deploy <target>                          Deploy via helm/fly/railway/render
//	af-stack operator create --email <email>          Allow an operator
//	af-stack operator key [--owner]                   Mint an operator API key (direct DB)
//	af-stack mcp list                                List configured MCP servers
//	af-stack mcp add <name> --transport ...          Register a new MCP server
//	af-stack mcp remove <name>                       Remove an MCP server
//	af-stack mcp call <server> <tool> [--json ...]   Invoke an MCP tool
//	af-stack job new <name> --lang py|ts             Scaffold a background-worker job
//	af-stack connection add|list|remove              Manage external-service connections
//	af-stack secrets set <key> | list                Manage the per-tenant secrets vault
//	af-stack db diff|push|generate|reset             Runtime + module migrations (goose)
//	af-stack doctor | status | test                  Environment + runtime diagnostics
//
// Operator REST surface (require AF_STACK_API_KEY set to an operator key):
//
//	af-stack keys list|issue|rotate|revoke|spend      Manage tenant API keys
//	af-stack agents list                              Registered agents + reasoners
//	af-stack reasoners                                Per-reasoner analytics
//	af-stack logs                                     Runtime log lines
//	af-stack errors list|resolve|mute|reopen          Error groups
//	af-stack audit                                    Operator audit log
//	af-stack sessions list|revoke                     Live user sessions
//	af-stack runs                                     Recent agent runs
//	af-stack tenants list                             Tenants
//	af-stack activity                                 Customer activity log
//
// Environment:
//
//	AF_STACK_URL       Runtime base URL (default http://localhost:8080)
//	AF_STACK_API_KEY   Bearer token (optional; required against an auth-on runtime)
//	DATABASE_URL       Postgres URL (operator create / operator key only)
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Agent-Field/backai/services/cli/internal/admincmd"
	"github.com/Agent-Field/backai/services/cli/internal/billingcmd"
	"github.com/Agent-Field/backai/services/cli/internal/client"
	"github.com/Agent-Field/backai/services/cli/internal/conncmd"
	"github.com/Agent-Field/backai/services/cli/internal/dbcmd"
	"github.com/Agent-Field/backai/services/cli/internal/diag"
	"github.com/Agent-Field/backai/services/cli/internal/initcmd"
	"github.com/Agent-Field/backai/services/cli/internal/jobcmd"
	"github.com/Agent-Field/backai/services/cli/internal/mcp"
	"github.com/Agent-Field/backai/services/cli/internal/modecmd"
	"github.com/Agent-Field/backai/services/cli/internal/output"
	"github.com/Agent-Field/backai/services/cli/internal/project"
	"github.com/Agent-Field/backai/services/cli/internal/secretcmd"
	"github.com/Agent-Field/backai/services/cli/internal/telemetry"
	"github.com/Agent-Field/backai/services/cli/internal/upgradecmd"
)

// version is overridden at build time via -ldflags "-X main.version=..."
// (goreleaser stamps the release tag). Keep it a var, not a const, so the
// linker can inject the real version.
var version = "0.0.1"

func main() {
	// The global --no-telemetry flag may appear anywhere; strip it before
	// dispatch so subcommand flag parsers never see it.
	optOut, args := extractNoTelemetry(os.Args[1:])

	tel := telemetry.New(version, optOut, os.Stderr)
	cmdName := "help"
	if len(args) > 0 {
		cmdName = args[0]
	}
	start := time.Now()

	err := run(args)

	tel.Emit(context.Background(), cmdName, err == nil, time.Since(start))

	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) {
			fmt.Fprintln(os.Stderr, apiErr.Error())
		} else {
			fmt.Fprintln(os.Stderr, "af-stack:", err)
		}
		// Exit with the stable code the error maps to (see internal/output):
		// scripts and agents branch on *why* a command failed, not just 1.
		os.Exit(output.ExitCode(err))
	}
}

// extractNoTelemetry removes the global --no-telemetry flag from args and
// reports whether it was present.
func extractNoTelemetry(args []string) (bool, []string) {
	optOut := false
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--no-telemetry" {
			optOut = true
			continue
		}
		out = append(out, a)
	}
	return optOut, out
}

func run(args []string) error {
	if len(args) < 1 {
		writeUsage(os.Stderr)
		return errors.New("missing command")
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "version", "--version", "-v":
		fmt.Printf("af-stack %s\n", version)
		return nil
	case "help", "--help", "-h":
		writeUsage(os.Stdout)
		return nil
	case "init":
		return initcmd.Run(rest, os.Stdin, os.Stdout, os.Stderr)
	case "upgrade":
		return upgradecmd.Run(rest, os.Stdin, os.Stdout, os.Stderr)
	case "dev":
		ctx, cancel := signal.NotifyContext(
			context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return project.RunDev(ctx, rest, os.Stdout, os.Stderr)
	case "mode":
		return modecmd.Run(rest, os.Stdout, os.Stderr)
	case "agent":
		return project.RunAgent(rest, os.Stdout, os.Stderr)
	case "module":
		return project.RunModule(rest, os.Stdout, os.Stderr)
	case "plugin":
		return project.RunPlugin(rest, os.Stdout, os.Stderr)
	case "adapter":
		ctx, cancel := signal.NotifyContext(
			context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		c := client.New()
		return project.RunAdapter(ctx, c, rest, os.Stdout, os.Stderr)
	case "deploy":
		ctx, cancel := signal.NotifyContext(
			context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return project.RunDeploy(ctx, rest, os.Stdout, os.Stderr)
	case "operator":
		ctx, cancel := signal.NotifyContext(
			context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return project.RunOperator(ctx, rest, os.Stdout, os.Stderr)
	case "mcp":
		ctx, cancel := signal.NotifyContext(
			context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		c := client.New()
		return mcp.Run(ctx, c, rest, os.Stdout, os.Stderr)
	case "billing":
		ctx, cancel := signal.NotifyContext(
			context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		c := client.New()
		return billingcmd.Run(ctx, c, rest, os.Stdout, os.Stderr)
	case "job":
		// Pure local scaffold — no runtime, no key.
		return jobcmd.Run(rest, os.Stdout, os.Stderr)
	case "connection", "connections":
		ctx, cancel := signal.NotifyContext(
			context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		c := client.New()
		return conncmd.Run(ctx, c, rest, os.Stdin, os.Stdout, os.Stderr)
	case "secrets", "secret":
		ctx, cancel := signal.NotifyContext(
			context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		c := client.New()
		return secretcmd.Run(ctx, c, rest, os.Stdin, os.Stdout, os.Stderr)
	case "db":
		ctx, cancel := signal.NotifyContext(
			context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return dbcmd.Run(ctx, rest, os.Stdout, os.Stderr)
	case "doctor":
		ctx, cancel := signal.NotifyContext(
			context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return diag.RunDoctor(ctx, client.New(), rest, os.Stdout, os.Stderr)
	case "status":
		ctx, cancel := signal.NotifyContext(
			context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return diag.RunStatus(ctx, client.New(), rest, os.Stdout, os.Stderr)
	case "test":
		ctx, cancel := signal.NotifyContext(
			context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return diag.RunTest(ctx, client.New(), rest, os.Stdout, os.Stderr)
	case "keys", "agents", "reasoners", "logs", "errors", "audit",
		"sessions", "runs", "tenants", "activity":
		// Operator REST surface. Requires AF_STACK_API_KEY set to an
		// operator key — mint one with `af-stack operator key`.
		ctx, cancel := signal.NotifyContext(
			context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		c := client.New()
		switch cmd {
		case "keys":
			return admincmd.RunKeys(ctx, c, rest, os.Stdout, os.Stderr)
		case "agents":
			return admincmd.RunAgents(ctx, c, rest, os.Stdout, os.Stderr)
		case "reasoners":
			return admincmd.RunReasoners(ctx, c, rest, os.Stdout, os.Stderr)
		case "logs":
			return admincmd.RunLogs(ctx, c, rest, os.Stdout, os.Stderr)
		case "errors":
			return admincmd.RunErrors(ctx, c, rest, os.Stdout, os.Stderr)
		case "audit":
			return admincmd.RunAudit(ctx, c, rest, os.Stdout, os.Stderr)
		case "sessions":
			return admincmd.RunSessions(ctx, c, rest, os.Stdout, os.Stderr)
		case "runs":
			return admincmd.RunRuns(ctx, c, rest, os.Stdout, os.Stderr)
		case "tenants":
			return admincmd.RunTenants(ctx, c, rest, os.Stdout, os.Stderr)
		case "activity":
			return admincmd.RunActivity(ctx, c, rest, os.Stdout, os.Stderr)
		}
		return nil
	default:
		writeUsage(os.Stderr)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func writeUsage(w *os.File) {
	fmt.Fprintln(w, `af-stack — AF Stack operator CLI

Usage:
  af-stack <command> [args...]

Commands:
  init       Scaffold a new app (af-stack init <name>); flags-only re-themes this fork
  upgrade    Pull the latest upstream AF Stack into this fork (--check for a dry run)
  dev        Start docker compose for local development
  mode       Switch personal (auth+billing off) ⇄ saas (af-stack mode [personal|saas])
  agent      Agent scaffold commands
  module     Workload module scaffold commands
  plugin     Dashboard plugin scaffold commands
  adapter    Adapter discovery commands
  deploy     Deploy wrappers for helm/fly/railway/render
  operator   Operator bootstrap commands (create, key)
  mcp        Model Context Protocol server + tool management
  billing    Set up Stripe billing: plans + pricing (agent-first)
  job        Scaffold a background-worker job (af-stack job new <name> --lang py|ts)
  connection External-service connections: add | list | remove
  secrets    Per-tenant secrets vault: set | list
  db         Migrations (goose): diff | push | generate | reset
  doctor     Environment + runtime health checks
  status     Compact "is the stack up and how is it configured" snapshot
  test       Shippable-fork gates (manifests, migrations, ...)
  version    Print the CLI version

Operator commands (need AF_STACK_API_KEY = operator key; mint one with
`+"`af-stack operator key`"+`):
  keys       API keys: list | issue | rotate | revoke | spend
  agents     Registered agents: list
  reasoners  Per-reasoner cost/latency/error analytics
  logs       Runtime logs (filters: --level --service --search)
  errors     Error groups: list | resolve | mute | reopen
  audit      Operator audit log
  sessions   Live user sessions: list | revoke
  runs       Recent agent runs
  tenants    Tenants: list
  activity   Customer activity log (cross-tenant)

Examples:
  af-stack init my-app                              # scaffold a new project that consumes the stack
  af-stack init --template coding-agent             # in-checkout: rebrand + scaffold the hero coding agent
  af-stack init --name "DocuChat" --color "#0A66C2" # in-checkout: re-theme this fork
  af-stack upgrade --check                          # what would an upgrade bring? (commits, migrations, conflicts)
  af-stack upgrade                                  # backup DB, merge upstream, print rebuild steps
  af-stack dev --detach
  af-stack mode personal                            # single-user app: no login, no billing
  af-stack mode saas                                # back to multi-tenant SaaS
  af-stack agent new researcher
  af-stack adapter list
  af-stack deploy helm
  af-stack operator create --email founder@example.com
  af-stack operator key --owner        # mint an operator API key (needs DATABASE_URL)
  af-stack keys issue --tenant <uuid> --name ci
  af-stack job new resize-image --lang py           # scaffold jobs/resize-image.py
  af-stack connection add --provider github --kind api_key --name ci
  echo -n "$STRIPE_KEY" | af-stack secrets set stripe --value-stdin
  af-stack status --json                            # machine-readable stack snapshot
  af-stack logs --level error --tail 100 --json
  af-stack reasoners --limit 20
  af-stack sessions list
  af-stack mcp call github search_repos --json '{"q":"agentfield"}'

Global flags:
  --no-telemetry     Disable anonymous usage telemetry for this run

Environment:
  AF_STACK_URL            Runtime base URL (default http://localhost:8080)
  AF_STACK_API_KEY        Bearer token used as authorization (operator key for admin commands)
  DATABASE_URL            Postgres URL (operator create / operator key only)
  AF_STACK_TELEMETRY=0    Disable anonymous usage telemetry (see TELEMETRY.md)
  AF_STACK_TELEMETRY_URL  Telemetry collection endpoint (unset = telemetry off)`)
}
