// SPDX-License-Identifier: Apache-2.0

// Package mcp implements the `af-stack mcp ...` subcommands.
//
// All commands are thin wrappers over the runtime's REST surface (see
// services/runtime/internal/server/mcp.go). The wire contract is the
// same one consumed by the dashboard's lib/api.ts.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/Agent-Field/backai/services/cli/internal/client"
)

// ---------- wire types (mirror dashboard/src/lib/api.ts) ----------

// Server matches MCPServerSchema.
type Server struct {
	Name            string            `json:"name"`
	Transport       string            `json:"transport"`
	Command         []string          `json:"command"`
	URL             *string           `json:"url"`
	Env             map[string]string `json:"env"`
	TenantID        *string           `json:"tenant_id"`
	Description     string            `json:"description"`
	IsEnabled       bool              `json:"is_enabled"`
	Status          string            `json:"status"`
	LastError       *string           `json:"last_error"`
	ToolsCount      int               `json:"tools_count"`
	InstalledAt     string            `json:"installed_at"`
	LastConnectedAt *string           `json:"last_connected_at"`
}

// ServerList matches MCPServerListSchema.
type ServerList struct {
	Servers []Server `json:"servers"`
}

// Tool matches MCPToolSchema.
type Tool struct {
	ID          string                 `json:"id"`
	Server      string                 `json:"server"`
	Name        string                 `json:"name"`
	Description *string                `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// ToolList matches MCPToolListSchema.
type ToolList struct {
	Tools []Tool `json:"tools"`
}

// CreateServerInput matches CreateMCPServerInputSchema.
type CreateServerInput struct {
	Name        string            `json:"name"`
	Transport   string            `json:"transport"`
	Command     []string          `json:"command,omitempty"`
	URL         string            `json:"url,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Description string            `json:"description,omitempty"`
	TenantID    string            `json:"tenant_id,omitempty"`
}

// CallInput matches CallMCPInputSchema.
type CallInput struct {
	Server    string                 `json:"server"`
	Tool      string                 `json:"tool"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// CallResult matches CallMCPResultSchema.
type CallResult struct {
	Content    []map[string]interface{} `json:"content"`
	IsError    bool                     `json:"is_error"`
	DurationMS int                      `json:"duration_ms"`
}

// ---------- Run dispatches `af-stack mcp <subcommand>` ----------

// Run handles the `af-stack mcp <subcommand>` family. `args` is the
// argv slice AFTER the leading `mcp` token (so args[0] is the
// subcommand name, e.g. "list").
func Run(ctx context.Context, c *client.Client, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		writeUsage(stderr)
		return errors.New("mcp: missing subcommand")
	}
	switch args[0] {
	case "list", "ls":
		return runList(ctx, c, args[1:], stdout)
	case "add":
		return runAdd(ctx, c, args[1:], stdout)
	case "remove", "rm":
		return runRemove(ctx, c, args[1:], stdout)
	case "call":
		return runCall(ctx, c, args[1:], stdout)
	case "help", "--help", "-h":
		writeUsage(stdout)
		return nil
	default:
		writeUsage(stderr)
		return fmt.Errorf("mcp: unknown subcommand %q", args[0])
	}
}

func writeUsage(w io.Writer) {
	fmt.Fprintln(w, `af-stack mcp — Model Context Protocol server + tool management

Usage:
  af-stack mcp list
  af-stack mcp add <name> --transport stdio --command "uvx mcp-server-github" [--env KEY=VAL ...] [--description "..."] [--tenant <id>]
  af-stack mcp add <name> --transport sse --url https://mcp.acme.com/sse [--description "..."] [--tenant <id>]
  af-stack mcp remove <name>
  af-stack mcp call <server> <tool> [--json '{"arg":"value"}']

Examples:
  af-stack mcp add github --transport stdio --command "uvx mcp-server-github" --env GITHUB_TOKEN=secret:github_token
  af-stack mcp call github search_repos --json '{"q":"agentfield"}'`)
}

// ---------- list ----------

func runList(ctx context.Context, c *client.Client, _ []string, stdout io.Writer) error {
	var list ServerList
	if err := c.Do(ctx, "GET", "/mcp/servers", nil, &list); err != nil {
		return err
	}
	if len(list.Servers) == 0 {
		fmt.Fprintln(stdout, "No MCP servers configured.")
		return nil
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tTRANSPORT\tSTATUS\tENABLED\tTOOLS\tLAST CONNECTED")
	for _, s := range list.Servers {
		lastConnected := "—"
		if s.LastConnectedAt != nil && *s.LastConnectedAt != "" {
			lastConnected = *s.LastConnectedAt
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%t\t%d\t%s\n",
			s.Name, s.Transport, s.Status, s.IsEnabled, s.ToolsCount, lastConnected)
	}
	return tw.Flush()
}

// ---------- add ----------

// envFlag implements flag.Value so callers can pass --env KEY=VAL
// multiple times.
type envFlag map[string]string

func (e *envFlag) String() string {
	if e == nil || len(*e) == 0 {
		return ""
	}
	parts := make([]string, 0, len(*e))
	for k, v := range *e {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ",")
}

func (e *envFlag) Set(raw string) error {
	idx := strings.Index(raw, "=")
	if idx <= 0 {
		return fmt.Errorf("env flag must be KEY=VALUE (got %q)", raw)
	}
	key := strings.TrimSpace(raw[:idx])
	val := raw[idx+1:]
	if key == "" {
		return fmt.Errorf("env key must be non-empty (got %q)", raw)
	}
	if *e == nil {
		*e = envFlag{}
	}
	(*e)[key] = val
	return nil
}

func runAdd(ctx context.Context, c *client.Client, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("mcp add: missing server name")
	}
	name := args[0]

	fs := flag.NewFlagSet("af-stack mcp add", flag.ContinueOnError)
	transport := fs.String("transport", "stdio", "transport: stdio | sse")
	command := fs.String("command", "", `stdio command (e.g. "uvx mcp-server-github")`)
	url := fs.String("url", "", "sse endpoint url")
	description := fs.String("description", "", "human-readable description")
	tenant := fs.String("tenant", "", "tenant id (omit for shared servers)")
	envs := envFlag{}
	fs.Var(&envs, "env", "environment KEY=VALUE (repeat for multiple)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	input := CreateServerInput{
		Name:      name,
		Transport: *transport,
	}
	if *description != "" {
		input.Description = *description
	}
	if *tenant != "" {
		input.TenantID = *tenant
	}
	if len(envs) > 0 {
		input.Env = envs
	}

	switch *transport {
	case "stdio":
		if *command == "" {
			return errors.New("mcp add: stdio transport requires --command")
		}
		input.Command = strings.Fields(*command)
	case "sse":
		if *url == "" {
			return errors.New("mcp add: sse transport requires --url")
		}
		input.URL = *url
	default:
		return fmt.Errorf("mcp add: unknown transport %q (want stdio | sse)", *transport)
	}

	var server Server
	if err := c.Do(ctx, "POST", "/mcp/servers", input, &server); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Added MCP server %q (transport=%s status=%s)\n",
		server.Name, server.Transport, server.Status)
	return nil
}

// ---------- remove ----------

func runRemove(ctx context.Context, c *client.Client, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("mcp remove: missing server name")
	}
	name := args[0]
	if err := c.Do(ctx, "DELETE", "/mcp/servers/"+name, nil, nil); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Removed MCP server %q\n", name)
	return nil
}

// ---------- call ----------

func runCall(ctx context.Context, c *client.Client, args []string, stdout io.Writer) error {
	if len(args) < 2 {
		return errors.New("mcp call: usage: af-stack mcp call <server> <tool> [--json '{...}']")
	}
	server := args[0]
	tool := args[1]

	fs := flag.NewFlagSet("af-stack mcp call", flag.ContinueOnError)
	jsonArgs := fs.String("json", "", "JSON arguments object (defaults to {})")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}

	input := CallInput{Server: server, Tool: tool}
	if *jsonArgs != "" {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(*jsonArgs), &parsed); err != nil {
			return fmt.Errorf("mcp call: --json is not valid JSON: %w", err)
		}
		input.Arguments = parsed
	}

	var result CallResult
	if err := c.Do(ctx, "POST", "/mcp/call", input, &result); err != nil {
		return err
	}

	// Pretty-print: surface error state up top, then the typed content
	// parts, then the duration. This is a CLI consumer that wants to
	// pipe / grep results — keep it stable.
	if result.IsError {
		fmt.Fprintln(stdout, "[error]")
	}
	out, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("mcp call: marshal result: %w", err)
	}
	fmt.Fprintln(stdout, string(out))
	if result.IsError {
		return errors.New("mcp call: server returned is_error=true")
	}
	return nil
}