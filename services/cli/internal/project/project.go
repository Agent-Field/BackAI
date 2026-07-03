// SPDX-License-Identifier: Apache-2.0

// Package project implements local fork-management CLI commands.
package project

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"
	"unicode"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

type commandRunner func(ctx context.Context, dir string, name string, args []string, stdout, stderr io.Writer) error

var runCommand commandRunner = func(ctx context.Context, dir string, name string, args []string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func RunDev(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("af-stack dev", flag.ContinueOnError)
	fs.SetOutput(stderr)
	detach := fs.Bool("detach", false, "run docker compose in detached mode")
	noOpen := fs.Bool("no-open", false, "do not open the dashboard URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := findRepoRoot()
	if err != nil {
		return err
	}
	composeArgs := []string{"compose", "up"}
	if *detach {
		composeArgs = append(composeArgs, "-d")
	}
	if *detach && !*noOpen {
		defer openURL(ctx, "http://localhost:3000", stderr)
	}
	fmt.Fprintln(stdout, "Starting AF Stack with docker compose...")
	return runCommand(ctx, root, "docker", composeArgs, stdout, stderr)
}

func RunAgent(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("agent: missing subcommand")
	}
	switch args[0] {
	case "new":
		return runAgentNew(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("agent: unknown subcommand %q", args[0])
	}
}

func RunModule(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("module: missing subcommand")
	}
	switch args[0] {
	case "new":
		return runModuleNew(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("module: unknown subcommand %q", args[0])
	}
}

func RunPlugin(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("plugin: missing subcommand")
	}
	switch args[0] {
	case "new":
		return runPluginNew(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("plugin: unknown subcommand %q", args[0])
	}
}

func RunAdapter(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("adapter: missing subcommand")
	}
	switch args[0] {
	case "list", "ls":
		return runAdapterList(args[1:], stdout, stderr)
	case "new":
		return runAdapterNew(args[1:], stdout, stderr)
	default:
		return fmt.Errorf("adapter: unknown subcommand %q", args[0])
	}
}

func RunDeploy(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("af-stack deploy", flag.ContinueOnError)
	fs.SetOutput(stderr)
	targetFlag := fs.String("target", "", "deploy target: helm | fly | railway | render")
	if err := fs.Parse(args); err != nil {
		return err
	}
	target := strings.TrimSpace(*targetFlag)
	if target == "" && fs.NArg() > 0 {
		target = fs.Arg(0)
	}
	if target == "" {
		return errors.New("deploy: target is required")
	}
	root, err := findRepoRoot()
	if err != nil {
		return err
	}
	name, cmdArgs, err := deployCommand(target)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Deploying AF Stack to %s...\n", target)
	return runCommand(ctx, root, name, cmdArgs, stdout, stderr)
}

func RunOperator(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("operator: missing subcommand")
	}
	switch args[0] {
	case "create":
		return runOperatorCreate(ctx, args[1:], stdout, stderr)
	case "key":
		return runOperatorKey(ctx, args[1:], stdout, stderr)
	default:
		return fmt.Errorf("operator: unknown subcommand %q", args[0])
	}
}

// runOperatorKey mints an OPERATOR API key by writing suite_api_keys
// directly (same bootstrap posture as `operator create` — it needs
// DATABASE_URL, not a running session). The key is minted on the
// default zero-uuid tenant with scope "operator" (or "operator:owner"
// with --owner); the runtime's operator gate recognises exactly that
// combination (see resolveOperatorBearer in the runtime). Token shape
// mirrors tenancy.IssueKey: af_<15 base32>_<48 base32>, secret
// bcrypt-hashed at cost 12.
func runOperatorKey(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("af-stack operator key", flag.ContinueOnError)
	fs.SetOutput(stderr)
	nameFlag := fs.String("name", "af-stack CLI", "key name")
	ownerFlag := fs.Bool("owner", false, "grant the owner role (adds destructive permissions)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("AF_STACK_DATABASE_URL")
	}
	if dbURL == "" {
		return errors.New("operator key: DATABASE_URL or AF_STACK_DATABASE_URL is required")
	}

	prefix, err := randomBase32(9) // 15 chars
	if err != nil {
		return err
	}
	secret, err := randomBase32(30) // 48 chars
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), 12)
	if err != nil {
		return err
	}
	scope := "operator"
	if *ownerFlag {
		scope = "operator:owner"
	}

	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	const zeroTenant = "00000000-0000-0000-0000-000000000000"
	var id string
	err = conn.QueryRow(ctx, `
		insert into suite_api_keys (tenant_id, prefix, hashed_secret, name, scopes)
		values ($1, $2, $3, $4, $5)
		returning id::text
	`, zeroTenant, prefix, string(hash), strings.TrimSpace(*nameFlag), []string{scope}).Scan(&id)
	if err != nil {
		return fmt.Errorf("operator key: insert: %w", err)
	}

	fmt.Fprintf(stdout, `operator key minted (id %s, scope %s)

  af_%s_%s

Store it now — it is shown exactly once. Use it with:

  export AF_STACK_API_KEY=af_%s_%s
  af-stack keys list
`, id, scope, prefix, secret, prefix, secret)
	return nil
}

// randomBase32 returns a lower-case base32 string with bytesN bytes of
// entropy — the same encoding tenancy.randomToken uses.
func randomBase32(bytesN int) (string, error) {
	buf := make([]byte, bytesN)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return strings.ToLower(
		base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)), nil
}

func runAgentNew(args []string, stdout, _ io.Writer) error {
	if len(args) != 1 {
		return errors.New("agent new: usage: af-stack agent new <name>")
	}
	root, err := findRepoRoot()
	if err != nil {
		return err
	}
	id := slugify(args[0])
	dir := filepath.Join(root, "apps", "backend", "agents", id)
	if exists(dir) {
		return fmt.Errorf("agent new: %s already exists", filepath.ToSlash(dir))
	}
	files := map[string]string{
		"requirements.txt": "agentfield>=0.4.0\npydantic>=2\n",
		"main.py":          agentTemplate(id),
		"Dockerfile":       agentDockerfileTemplate(id),
		"README.md":        fmt.Sprintf("# %s agent\n\nInvoked as `%s.echo` and `%s.summarize`.\n", title(id), id, id),
	}
	if err := writeFiles(dir, files); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Created agent scaffold at apps/backend/agents/%s\n", id)
	return nil
}

func runModuleNew(args []string, stdout, _ io.Writer) error {
	if len(args) != 1 {
		return errors.New("module new: usage: af-stack module new <id>")
	}
	root, err := findRepoRoot()
	if err != nil {
		return err
	}
	id := slugify(args[0])
	dir := filepath.Join(root, "workload-modules", id)
	if exists(dir) {
		return fmt.Errorf("module new: %s already exists", filepath.ToSlash(dir))
	}
	files := map[string]string{
		"manifest.yaml":              moduleManifestTemplate(id),
		"migrations/00001_init.sql":  moduleMigrationTemplate(id),
		"handlers/routes.go.example": moduleHandlerTemplate(id),
		"README.md":                  fmt.Sprintf("# %s workload module\n\nRoutes mount under `/workload/%s`.\n", title(id), id),
	}
	if err := writeFiles(dir, files); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Created workload module scaffold at workload-modules/%s\n", id)
	return nil
}

func runPluginNew(args []string, stdout, _ io.Writer) error {
	if len(args) != 1 {
		return errors.New("plugin new: usage: af-stack plugin new <id>")
	}
	root, err := findRepoRoot()
	if err != nil {
		return err
	}
	id := slugify(args[0])
	dir := filepath.Join(root, "apps", "dashboard", "plugins", id)
	if exists(dir) {
		return fmt.Errorf("plugin new: %s already exists", filepath.ToSlash(dir))
	}
	files := map[string]string{
		"plugin.ts": pluginManifestTemplate(id),
		"page.tsx":  pluginPageTemplate(id),
	}
	if err := writeFiles(dir, files); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Created dashboard plugin scaffold at apps/dashboard/plugins/%s\n", id)
	return nil
}

func runOperatorCreate(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("af-stack operator create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	emailFlag := fs.String("email", "", "operator email")
	nameFlag := fs.String("name", "", "operator display name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	email := strings.TrimSpace(*emailFlag)
	if email == "" && fs.NArg() > 0 {
		email = strings.TrimSpace(fs.Arg(0))
	}
	if email == "" || !strings.Contains(email, "@") {
		return errors.New("operator create: --email is required")
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("AF_STACK_DATABASE_URL")
	}
	if dbURL == "" {
		return errors.New("operator create: DATABASE_URL or AF_STACK_DATABASE_URL is required")
	}

	conn, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS suite_operators (
		  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
		  user_id text UNIQUE,
		  email text UNIQUE NOT NULL,
		  name text,
		  role text NOT NULL DEFAULT 'owner' CHECK (role IN ('owner','admin')),
		  created_at timestamptz NOT NULL DEFAULT now()
		)
	`); err != nil {
		return err
	}
	_, err = conn.Exec(ctx, `
		INSERT INTO suite_users (email, name)
		VALUES ($1, NULLIF($2, ''))
		ON CONFLICT (email) DO UPDATE
		  SET name = COALESCE(NULLIF(EXCLUDED.name, ''), suite_users.name)
	`, email, strings.TrimSpace(*nameFlag))
	if err != nil {
		return err
	}
	_, err = conn.Exec(ctx, `
		INSERT INTO suite_operators (user_id, email, name, role)
		VALUES (
		  (SELECT id FROM "user" WHERE lower(email) = lower($1) LIMIT 1),
		  $1,
		  NULLIF($2, ''),
		  'owner'
		)
		ON CONFLICT (email) DO UPDATE
		  SET user_id = COALESCE(suite_operators.user_id, EXCLUDED.user_id),
		      name = COALESCE(EXCLUDED.name, suite_operators.name)
	`, email, strings.TrimSpace(*nameFlag))
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Operator %s is allowed for dashboard access.\n", email)
	return nil
}

func runAdapterList(_ []string, stdout, _ io.Writer) error {
	// READY lists only the adapters the runtime actually constructs today (see
	// services/runtime/cmd/af-stack/main.go: newStorage/newSandbox/
	// buildNotificationsAdapter and internal/billing NewClientFromEnv). PLANNED
	// mirrors the "Planned" tables in the docs/adapters/*.md pages — selecting
	// one of those falls back to the default with a warning, so the CLI must
	// not present them as working choices.
	rows := []struct {
		Area    string
		Env     string
		Default string
		Ready   string
		Planned string
		Docs    string
	}{
		{"Storage", "AF_STACK_S3_ADAPTER", "minio", "minio, s3", "r2, gcs, azure-blob", "docs/adapters/storage.md"},
		{"Sandbox", "AF_STACK_SANDBOX_ADAPTER", "docker", "docker, gvisor, firecracker, e2b, remote", "", "docs/adapters/sandbox.md"},
		{"Notifications", "AF_STACK_NOTIFICATIONS_ADAPTER", "log", "log, resend", "postmark, sendgrid, ses, mailgun", "docs/adapters/notifications.md"},
		{"Billing", "AF_STACK_BILLING_ADAPTER", "stripe", "stripe, lago, none", "", "docs/adapters/billing.md"},
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "AREA\tACTIVE\tENV\tREADY\tPLANNED\tDOCS")
	for _, row := range rows {
		active := strings.TrimSpace(os.Getenv(row.Env))
		if active == "" {
			active = row.Default
		}
		planned := row.Planned
		if planned == "" {
			planned = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", row.Area, active, row.Env, row.Ready, planned, row.Docs)
	}
	return tw.Flush()
}

func deployCommand(target string) (string, []string, error) {
	switch strings.ToLower(target) {
	case "helm":
		return "helm", []string{"upgrade", "--install", "af-stack", "./deploy/helm/af-stack"}, nil
	case "fly", "flyio":
		return "fly", []string{"deploy", "-c", "deploy/fly/fly.toml"}, nil
	case "railway":
		return "railway", []string{"up"}, nil
	case "render":
		return "render", []string{"deploy"}, nil
	default:
		return "", nil, fmt.Errorf("deploy: unknown target %q (want helm | fly | railway | render)", target)
	}
}

func openURL(ctx context.Context, url string, stderr io.Writer) {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{url}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		name, args = "xdg-open", []string{url}
	}
	if err := runCommand(ctx, "", name, args, io.Discard, stderr); err != nil {
		fmt.Fprintf(stderr, "open browser: %v\n", err)
	}
}

func writeFiles(root string, files map[string]string) error {
	for rel, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if exists(filepath.Join(wd, "package.json")) &&
			exists(filepath.Join(wd, "apps", "dashboard")) &&
			exists(filepath.Join(wd, "apps", "customer-app")) {
			return wd, nil
		}
		next := filepath.Dir(wd)
		if next == wd {
			return "", errors.New("must run from inside an AF Stack checkout")
		}
		wd = next
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func slugify(input string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(input) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case unicode.IsSpace(r) || r == '-' || r == '_' || r == '.':
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "app"
	}
	return out
}

func title(id string) string {
	parts := strings.Split(id, "-")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func agentTemplate(id string) string {
	return fmt.Sprintf(`"""%[1]s AgentField agent."""

from __future__ import annotations

import os
from typing import Any

from agentfield import Agent, AIConfig
from pydantic import BaseModel


def select_model() -> str | None:
    if os.getenv("%[2]s_AGENT_MODEL"):
        return os.getenv("%[2]s_AGENT_MODEL")
    if os.getenv("OPENROUTER_API_KEY"):
        return "openrouter/qwen/qwen-2.5-72b-instruct"
    if os.getenv("ANTHROPIC_API_KEY"):
        return "anthropic/claude-haiku-4-5-20251001"
    if os.getenv("OPENAI_API_KEY"):
        return "openai/gpt-4o-mini"
    return None


MODEL = select_model()

app = Agent(
    node_id=os.getenv("NODE_ID", "%[1]s"),
    version=os.getenv("AGENT_VERSION", "0.1.0"),
    ai_config=AIConfig(model=MODEL) if MODEL else None,
)


@app.reasoner(tags=["echo"])
async def echo(payload: dict[str, Any]) -> dict[str, Any]:
    return {"echoed": payload}


class Summary(BaseModel):
    tldr: str
    next_steps: list[str]


if MODEL is not None:

    @app.reasoner(tags=["text"])
    async def summarize(payload: dict[str, Any]) -> dict[str, Any]:
        text = payload.get("text") or payload.get("content") or ""
        if not text:
            return {"error": "missing text"}
        result = await app.ai(
            system="Summarize the text and return three practical next steps.",
            user=text,
            schema=Summary,
        )
        return result.model_dump()


if __name__ == "__main__":
    app.run()
`, id, strings.ToUpper(strings.ReplaceAll(id, "-", "_")))
}

func agentDockerfileTemplate(id string) string {
	return fmt.Sprintf(`FROM python:3.12-slim

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_NO_CACHE_DIR=1

WORKDIR /app
COPY requirements.txt ./
RUN pip install -r requirements.txt
COPY main.py ./

ENV NODE_ID=%s \
    PORT=8090

EXPOSE 8090
CMD ["python", "main.py"]
`, id)
}

func moduleManifestTemplate(id string) string {
	return fmt.Sprintf(`id: %s
name: %s
version: 0.1.0
description: %s workload module.

routes:
  - method: POST
    path: /events
    handler: %s.CreateEvent
`, id, title(id), title(id), id)
}

func moduleMigrationTemplate(id string) string {
	table := strings.ReplaceAll(id, "-", "_") + "_events"
	return fmt.Sprintf(`-- SPDX-License-Identifier: Apache-2.0

CREATE TABLE IF NOT EXISTS %s (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  kind text NOT NULL,
  payload jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);
`, table)
}

func moduleHandlerTemplate(id string) string {
	return fmt.Sprintf(`// SPDX-License-Identifier: Apache-2.0

// Rename this file to routes.go when the workload handler package is enabled
// in your fork.
package %s

// Route: POST /workload/%s/events
`, strings.ReplaceAll(id, "-", "_"), id)
}

func pluginManifestTemplate(id string) string {
	return fmt.Sprintf(`// SPDX-License-Identifier: Apache-2.0

import { Sparkles } from "lucide-react"

import { definePlugin } from "@/lib/plugins"

export default definePlugin({
  id: "%s",
  label: "%s",
  icon: Sparkles,
  iconName: "Sparkles",
  description: "Fork-specific operator view.",
  group: "build",
  version: "0.1.0",
})
`, id, title(id))
}

func pluginPageTemplate(id string) string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, `// SPDX-License-Identifier: Apache-2.0

import { PageHeader } from "@/components/layout/page-header"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"

export default function %sPluginPage() {
  return (
    <div className="space-y-6">
      <PageHeader title="%s" description="Fork-specific operator view." />
      <Card>
        <CardHeader>
          <CardTitle>Custom metric</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="text-3xl font-semibold tracking-tight">0</div>
        </CardContent>
      </Card>
    </div>
  )
}
`, strings.ReplaceAll(title(id), " ", ""), title(id))
	return buf.String()
}
