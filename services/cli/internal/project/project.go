// SPDX-License-Identifier: Apache-2.0

// Package project implements local fork-management CLI commands.
package project

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
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

	"github.com/Agent-Field/backai/services/cli/internal/checkout"
	"github.com/Agent-Field/backai/services/cli/internal/client"
	"github.com/Agent-Field/backai/services/cli/internal/output"
	"github.com/Agent-Field/backai/services/cli/internal/validate"
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
	noOpen := fs.Bool("no-open", false, "with --detach, do not open the customer app URL in a browser")
	noPreflight := fs.Bool("no-preflight", false, "skip the port preflight/auto-allocation step")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := checkout.Find()
	if err != nil {
		return err
	}

	// Auto-allocate conflict-free host ports (and a stable COMPOSE_PROJECT_NAME)
	// into .env before starting, so multiple BackAI apps run side by side
	// without EADDRINUSE. The script also prints a "what runs where" map so an
	// agent reading stdout knows every service URL without guessing.
	if !*noPreflight {
		if err := runPreflight(ctx, root, stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "port preflight skipped: %v\n", err)
		}
	}

	composeArgs := []string{"compose", "up"}
	if *detach {
		composeArgs = append(composeArgs, "-d")
	}

	// Resolve the host ports the compose stack actually binds so we print
	// (and open) URLs that work. Defaults mirror .env.example; an env var or
	// a value written into .env (e.g. by the port preflight) wins via
	// readEnvValue. The customer app is the "open this first" surface.
	apiPort := readEnvValue(root, "AF_STACK_PORT", "8080")
	dashPort := readEnvValue(root, "AF_STACK_DASHBOARD_PORT", "33000")
	appPort := readEnvValue(root, "AF_STACK_CUSTOMER_APP_PORT", "34000")
	customerURL := "http://localhost:" + appPort

	fmt.Fprintln(stdout, "Local URLs:")
	fmt.Fprintf(stdout, "  Customer app  %s  (open this first)\n", customerURL)
	fmt.Fprintf(stdout, "  Dashboard     http://localhost:%s\n", dashPort)
	fmt.Fprintf(stdout, "  API           http://localhost:%s\n", apiPort)
	fmt.Fprintf(stdout, "  Your apps     AF_STACK_URL=http://localhost:%s\n", apiPort)

	// Only auto-open in detached mode; in the foreground `docker compose up`
	// holds the terminal and the URLs above are already printed.
	if *detach && !*noOpen {
		defer openURL(ctx, customerURL, stderr)
	}
	fmt.Fprintln(stdout, "Starting AF Stack with docker compose...")
	return runCommand(ctx, root, "docker", composeArgs, stdout, stderr)
}

// runPreflight runs scripts/preflight.mjs --fix from the repo root. It
// allocates free host ports into .env and prints the endpoint map. Missing
// node or script is non-fatal — the caller logs and continues.
func runPreflight(ctx context.Context, root string, stdout, stderr io.Writer) error {
	script := filepath.Join(root, "scripts", "preflight.mjs")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("preflight script not found (%s)", script)
	}
	if _, err := exec.LookPath("node"); err != nil {
		return fmt.Errorf("node not installed")
	}
	return runCommand(ctx, root, "node", []string{script, "--fix"}, stdout, stderr)
}

// readEnvValue returns the value of key from <root>/.env, falling back to the
// process environment and then def. Kept intentionally small — it only needs
// to resolve a port for the browser-open URL.
func readEnvValue(root, key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	data, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		return def
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == key {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return def
}

func RunAgent(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("agent: missing subcommand")
	}
	switch args[0] {
	case "new":
		return runAgentNew(args[1:], stdout, stderr)
	case "validate":
		return runAgentValidate(args[1:], stdout)
	default:
		return output.Usage("agent: unknown subcommand %q", args[0])
	}
}

func RunModule(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("module: missing subcommand")
	}
	switch args[0] {
	case "new":
		return runModuleNew(args[1:], stdout, stderr)
	case "validate":
		return runModuleValidate(args[1:], stdout)
	default:
		return output.Usage("module: unknown subcommand %q", args[0])
	}
}

// runModuleValidate validates a workload-module directory offline: manifest
// shape + the migration RLS lint. It backs `af-stack module validate <dir>`
// and shares the validate package with `af-stack test`, so the standalone
// command and the gate can never disagree.
func runModuleValidate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("module validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "emit the result as JSON")
	pos, err := output.ParseArgs(fs, args)
	if err != nil {
		return output.Usage("module validate: %v", err)
	}
	if len(pos) != 1 {
		return output.Usage("module validate: exactly one module directory is required")
	}
	dir := pos[0]
	if !exists(dir) {
		return output.NotFound("module validate: %s does not exist", filepath.ToSlash(dir))
	}
	res := validate.ModuleDir(dir)
	if err := output.Result(stdout, *asJSON, res, func(w io.Writer) error {
		return renderValidateResult(w, res)
	}); err != nil {
		return err
	}
	if !res.OK {
		return output.Invalid("module validate: %s failed validation", filepath.ToSlash(dir))
	}
	return nil
}

// runAgentValidate validates an agent scaffold directory offline.
func runAgentValidate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("agent validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	asJSON := fs.Bool("json", false, "emit the result as JSON")
	pos, err := output.ParseArgs(fs, args)
	if err != nil {
		return output.Usage("agent validate: %v", err)
	}
	if len(pos) != 1 {
		return output.Usage("agent validate: exactly one agent directory is required")
	}
	dir := pos[0]
	if !exists(dir) {
		return output.NotFound("agent validate: %s does not exist", filepath.ToSlash(dir))
	}
	res := validate.AgentDir(dir)
	if err := output.Result(stdout, *asJSON, res, func(w io.Writer) error {
		return renderValidateResult(w, res)
	}); err != nil {
		return err
	}
	if !res.OK {
		return output.Invalid("agent validate: %s failed validation", filepath.ToSlash(dir))
	}
	return nil
}

// renderValidateResult prints a validate.Result as an aligned human table.
func renderValidateResult(w io.Writer, res *validate.Result) error {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	for _, f := range res.Findings {
		fmt.Fprintf(tw, "  %s\t%s\n", f.Level, f.Message)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if res.OK {
		fmt.Fprintln(w, "\nOK")
	} else {
		fmt.Fprintln(w, "\nFAIL")
	}
	return nil
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

func RunAdapter(ctx context.Context, c *client.Client, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("adapter: missing subcommand")
	}
	switch args[0] {
	case "list", "ls":
		return runAdapterList(ctx, c, args[1:], stdout, stderr)
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
	root, err := checkout.Find()
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

// scaffoldName validates the single positional <name>/<id> argument of the
// scaffold subcommands. Flag-like arguments (e.g. `--help`) must not become
// scaffold directories, and the slug must be non-empty after normalization.
func scaffoldName(cmd string, args []string) (string, error) {
	usage := fmt.Errorf("%s: usage: af-stack %s <name>", cmd, cmd)
	if len(args) != 1 {
		return "", usage
	}
	if strings.HasPrefix(args[0], "-") {
		return "", usage
	}
	// slugify falls back to "app" for unusable input (an init-time
	// convenience); scaffolds must not silently rename, so require at
	// least one usable character up front.
	if !strings.ContainsFunc(strings.ToLower(args[0]), func(r rune) bool {
		return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
	}) {
		return "", fmt.Errorf("%s: name %q contains no usable characters (want [a-z0-9-])", cmd, args[0])
	}
	return slugify(args[0]), nil
}

func runAgentNew(args []string, stdout, _ io.Writer) error {
	id, err := scaffoldName("agent new", args)
	if err != nil {
		return err
	}
	root, err := checkout.Find()
	if err != nil {
		return err
	}
	dir := filepath.Join(root, "apps", "backend", "agents", id)
	if exists(dir) {
		return fmt.Errorf("agent new: %s already exists", filepath.ToSlash(dir))
	}
	files := map[string]string{
		"requirements.txt": "agentfield>=0.1.109\npydantic>=2\n",
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
	id, err := scaffoldName("module new", args)
	if err != nil {
		return err
	}
	root, err := checkout.Find()
	if err != nil {
		return err
	}
	dir := filepath.Join(root, "workload-modules", id)
	if exists(dir) {
		return fmt.Errorf("module new: %s already exists", filepath.ToSlash(dir))
	}
	files := map[string]string{
		"backai.module.yaml":        moduleManifestTemplate(id),
		"migrations/00001_init.sql": moduleMigrationTemplate(id),
		"README.md":                 moduleReadmeTemplate(id),
	}
	if err := writeFiles(dir, files); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Created workload module scaffold at workload-modules/%s\n", id)
	return nil
}

func runPluginNew(args []string, stdout, _ io.Writer) error {
	id, err := scaffoldName("plugin new", args)
	if err != nil {
		return err
	}
	root, err := checkout.Find()
	if err != nil {
		return err
	}
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

// adapterSlotWire mirrors one entry of GET /api/v1/admin/adapters
// (registry.SlotView). The CLI decodes the live runtime registry rather
// than a hand-maintained table, so `adapter list` can never drift from
// what the runtime actually constructs — a stub that errors on every call
// shows up as unhealthy instead of being advertised as ready.
type adapterSlotWire struct {
	Slot             string   `json:"slot"`
	Tier             int      `json:"tier"`
	AvailableBuiltin []string `json:"available_builtin"`
	SwapMethod       string   `json:"swap_method"`
	SwapEnv          string   `json:"swap_env"`
	AdminUI          string   `json:"admin_ui"`
	Active           struct {
		Name      string `json:"name"`
		Version   string `json:"version"`
		Status    string `json:"status"`
		Kind      string `json:"kind"`
		LastError string `json:"last_error"`
	} `json:"active"`
}

type adaptersResponseWire struct {
	Slots []adapterSlotWire `json:"slots"`
}

func runAdapterList(ctx context.Context, c *client.Client, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("af-stack adapter list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "emit the raw runtime adapter registry as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// --json returns the runtime's registry verbatim — the machine-readable
	// truth for agents, no reshaping.
	if *asJSON {
		var raw json.RawMessage
		if err := c.Do(ctx, "GET", "/admin/adapters", nil, &raw); err != nil {
			return adapterListUnavailable(err)
		}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, raw, "", "  "); err != nil {
			_, werr := fmt.Fprintln(stdout, string(raw))
			return werr
		}
		_, err := fmt.Fprintln(stdout, pretty.String())
		return err
	}

	var out adaptersResponseWire
	if err := c.Do(ctx, "GET", "/admin/adapters", nil, &out); err != nil {
		return adapterListUnavailable(err)
	}

	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "AREA\tACTIVE\tSTATUS\tAVAILABLE\tSWAP ENV\tMETHOD")
	for _, s := range out.Slots {
		available := strings.Join(s.AvailableBuiltin, ", ")
		if available == "" {
			available = "-"
		}
		swapEnv := s.SwapEnv
		if swapEnv == "" {
			swapEnv = "-"
		}
		method := s.SwapMethod
		if method == "" {
			method = "-"
		}
		status := s.Active.Status
		if status == "" {
			status = "unknown"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			s.Slot, s.Active.Name, status, available, swapEnv, method)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if len(out.Slots) == 0 {
		fmt.Fprintln(stdout, "no adapter slots reported by the runtime")
		return nil
	}

	// Surface any adapter that constructed but reports an error (e.g. a stub
	// that fails on every call) so the list tells the truth about health.
	for _, s := range out.Slots {
		if e := strings.TrimSpace(s.Active.LastError); e != "" {
			fmt.Fprintf(stdout, "  ! %s/%s: %s\n", s.Slot, s.Active.Name, e)
		}
	}
	return nil
}

// adapterListUnavailable turns a transport/auth failure into actionable
// guidance. It flattens the cause with %v (not %w) on purpose: the top-level
// CLI prints *client.APIError specially, and here we want our guidance shown
// instead of a bare "[FORBIDDEN] ..." line. We never fall back to a fabricated
// static table — a clear error beats a lie.
func adapterListUnavailable(cause error) error {
	return fmt.Errorf("adapter list needs the runtime operator API — set "+
		"AF_STACK_URL (default http://localhost:8080) and AF_STACK_API_KEY to an "+
		"operator key (mint one with `af-stack operator key`), and make sure the "+
		"runtime is running (`af-stack dev`). This list reflects the live runtime, "+
		"not a static table. underlying error: %v", cause)
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
	return fmt.Sprintf(`# %[2]s workload module (declarative — PRD R2).
#
# The runtime discovers this file at
# <WORKLOAD_MODULES_PATH>/%[1]s/backai.module.yaml and auto-generates
# tenant-scoped CRUD from the resources below — no handler code required.
# enabled: false so the scaffold never auto-serves; add %[1]q to
# modules.workload_modules (the enabled list) and restart to turn it on.
id: %[1]s
name: %[2]s
version: 0.1.0
description: %[2]s workload module.
enabled: false
migrations: migrations

resources:
  # Backing table follows the <module>_<resource> convention: %[3]s_items.
  # The migration under migrations/ must create it with id + tenant_id +
  # created_at + updated_at plus these fields, and enable/force RLS with a
  # tenant-isolation policy (statically enforced at load).
  - name: items
    fields:
      - name: title
        type: string
        required: true
      - name: done
        type: bool
        default: false
`, id, title(id), strings.ReplaceAll(id, "-", "_"))
}

func moduleMigrationTemplate(id string) string {
	table := strings.ReplaceAll(id, "-", "_") + "_items"
	return fmt.Sprintf(`-- %[2]s workload module migration.
--
-- The runtime lints this file before applying it: every CREATE TABLE must
-- carry a tenant_id column and be followed by ENABLE + FORCE ROW LEVEL
-- SECURITY and a CREATE POLICY. A tenantless table is refused (the module
-- is disabled; the runtime keeps serving everything else).

create table if not exists %[1]s (
  id         uuid        primary key default gen_random_uuid(),
  tenant_id  uuid        not null,
  title      text        not null,
  done       boolean     not null default false,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create index if not exists %[1]s_tenant_idx on %[1]s (tenant_id, created_at desc);

alter table %[1]s enable row level security;
alter table %[1]s force row level security;

create policy tenant_isolation on %[1]s
  using (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  )
  with check (
    current_setting('app.bypass_rls', true) = 'on'
    or tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid
  );
`, table, title(id))
}

func moduleReadmeTemplate(id string) string {
	return fmt.Sprintf(`# %[2]s workload module

Declarative workload module (PRD R2). The manifest (%[3]s) plus the versioned
SQL under `+"`migrations/`"+` is all the runtime needs to auto-generate
tenant-scoped CRUD — no handler code.

## Routes (once enabled)

Add %[1]q to the enabled workload list (`+"`modules.workload_modules`"+`) and
restart. The `+"`items`"+` resource is backed by table `+"`%[4]s_items`"+`:

| Method   | Path                                   | Action |
| -------- | -------------------------------------- | ------ |
| GET      | /api/v1/workload/%[1]s/items         | list   |
| POST     | /api/v1/workload/%[1]s/items         | create |
| GET      | /api/v1/workload/%[1]s/items/{id}    | get    |
| PATCH    | /api/v1/workload/%[1]s/items/{id}    | update |
| DELETE   | /api/v1/workload/%[1]s/items/{id}    | delete |

Every query is filtered by the resolver-bound tenant; a client can never set
`+"`tenant_id`"+` or reach another tenant's rows. Inspect discovery + migration
state at `+"`GET /api/v1/admin/modules`"+`. See `+"`workload-modules/notes/`"+`
for a fully-worked reference.
`, id, title(id), "backai.module.yaml", strings.ReplaceAll(id, "-", "_"))
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

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"

export default function %sPluginPage() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">%s</h1>
        <p className="text-sm text-muted-foreground">Fork-specific operator view.</p>
      </div>
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
