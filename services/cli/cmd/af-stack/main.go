// SPDX-License-Identifier: Apache-2.0

// af-stack — the AF Stack operator CLI.
//
// The CLI is a thin wrapper around the runtime REST API. It exists so
// operators can poke the same surface the dashboard uses, without
// reaching for curl + jq.
//
// Currently shipped subcommands:
//
//	af-stack init --name "DocuChat" --color "#0A66C2"  Brand this fork
//	af-stack dev                                      Start local compose dev loop
//	af-stack agent new <name>                         Scaffold an AgentField agent
//	af-stack module new <id>                          Scaffold a workload module
//	af-stack plugin new <id>                          Scaffold a dashboard plugin
//	af-stack adapter list                             Show active adapter choices
//	af-stack deploy <target>                          Deploy via helm/fly/railway/render
//	af-stack operator create --email <email>          Allow an operator
//	af-stack mcp list                                List configured MCP servers
//	af-stack mcp add <name> --transport ...          Register a new MCP server
//	af-stack mcp remove <name>                       Remove an MCP server
//	af-stack mcp call <server> <tool> [--json ...]   Invoke an MCP tool
//
// Environment:
//
//	AF_STACK_URL       Runtime base URL (default http://localhost:8080)
//	AF_STACK_API_KEY   Bearer token (optional; required against an auth-on runtime)
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Agent-Field/backai/services/cli/internal/client"
	"github.com/Agent-Field/backai/services/cli/internal/initcmd"
	"github.com/Agent-Field/backai/services/cli/internal/mcp"
	"github.com/Agent-Field/backai/services/cli/internal/project"
)

// version is overridden at build time via -ldflags "-X main.version=..."
// (goreleaser stamps the release tag). Keep it a var, not a const, so the
// linker can inject the real version.
var version = "0.0.1"

func main() {
	if err := run(); err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) {
			fmt.Fprintln(os.Stderr, apiErr.Error())
		} else {
			fmt.Fprintln(os.Stderr, "af-stack:", err)
		}
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		writeUsage(os.Stderr)
		return errors.New("missing command")
	}

	cmd := os.Args[1]
	rest := os.Args[2:]

	switch cmd {
	case "version", "--version", "-v":
		fmt.Printf("af-stack %s\n", version)
		return nil
	case "help", "--help", "-h":
		writeUsage(os.Stdout)
		return nil
	case "init":
		return initcmd.Run(rest, os.Stdin, os.Stdout, os.Stderr)
	case "dev":
		ctx, cancel := signal.NotifyContext(
			context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return project.RunDev(ctx, rest, os.Stdout, os.Stderr)
	case "agent":
		return project.RunAgent(rest, os.Stdout, os.Stderr)
	case "module":
		return project.RunModule(rest, os.Stdout, os.Stderr)
	case "plugin":
		return project.RunPlugin(rest, os.Stdout, os.Stderr)
	case "adapter":
		return project.RunAdapter(rest, os.Stdout, os.Stderr)
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
  init       Brand this fork and generate app theme files
  dev        Start docker compose for local development
  agent      Agent scaffold commands
  module     Workload module scaffold commands
  plugin     Dashboard plugin scaffold commands
  adapter    Adapter discovery commands
  deploy     Deploy wrappers for helm/fly/railway/render
  operator   Operator bootstrap commands
  mcp        Model Context Protocol server + tool management
  version    Print the CLI version

Examples:
  af-stack init --name "DocuChat" --color "#0A66C2" --logo ./logo.png
  af-stack dev --detach
  af-stack agent new researcher
  af-stack module new notes
  af-stack plugin new tenant-health
  af-stack adapter list
  af-stack deploy helm
  af-stack operator create --email founder@example.com
  af-stack mcp list
  af-stack mcp add github --transport stdio --command "uvx mcp-server-github" --env GITHUB_TOKEN=secret:github_token
  af-stack mcp call github search_repos --json '{"q":"agentfield"}'

Environment:
  AF_STACK_URL       Runtime base URL (default http://localhost:8080)
  AF_STACK_API_KEY   Bearer token used as authorization (optional)`)
}
