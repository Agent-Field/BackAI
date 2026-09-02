// SPDX-License-Identifier: Apache-2.0

package project

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/cli/internal/buildinfo"
	"github.com/Agent-Field/backai/services/cli/internal/starter"
)

// Timeouts for the bundled backend. Variables so tests can shorten them.
var (
	// appReadyTimeout bounds the wait for the runtime's /ready after
	// `docker compose up -d` returns; the image pulls happen before that,
	// so this only covers boot + migrations + the AgentField handshake.
	appReadyTimeout = 5 * time.Minute
	// appAgentTimeout bounds the wait for the demo agent to register.
	appAgentTimeout = 90 * time.Second
)

// demoAgentNodeID is the agent the bundled stack registers; the starter's
// first call is <node>.echo.
const demoAgentNodeID = "supportdesk"

// runAppDev is `af-stack dev` inside an app written by `af-stack init
// <name>`: it boots the app's bundled backend from the published images and
// points the app at it. Unlike the checkout flavour it is always detached —
// it returns once the runtime is ready, which is what `npm start`'s prestart
// hook needs.
func runAppDev(ctx context.Context, root string, noPreflight bool, stdout, stderr io.Writer) error {
	written, err := starter.EnsureBackend(root, buildinfo.Version)
	if err != nil {
		return fmt.Errorf("dev: %w", err)
	}
	if len(written) > 0 {
		fmt.Fprintf(stdout, "Wrote the bundled backend: %s\n", strings.Join(written, ", "))
	}

	if _, err := exec.LookPath("docker"); err != nil {
		return errors.New("dev: docker is required to run the bundled backend — install Docker Desktop (or Docker Engine with the compose plugin), start it, and run this again")
	}

	ports := map[string]int{}
	for _, p := range starter.Ports {
		ports[p.Env] = p.Default
	}
	project := starter.ProjectName(root)
	if noPreflight {
		env, err := starter.ReadEnv(root)
		if err != nil {
			return fmt.Errorf("dev: read .env: %w", err)
		}
		for _, p := range starter.Ports {
			if n, convErr := strconv.Atoi(strings.TrimSpace(env[p.Env])); convErr == nil && n > 0 {
				ports[p.Env] = n
			}
		}
		if v := strings.TrimSpace(env["COMPOSE_PROJECT_NAME"]); v != "" {
			project = v
		}
	} else {
		alloc, err := starter.AllocatePorts(root, starter.ComposeOwnsPort)
		if err != nil {
			return fmt.Errorf("dev: port preflight: %w", err)
		}
		ports = alloc.Resolved
		project = alloc.Project
		for _, m := range alloc.Moved {
			fmt.Fprintf(stdout, "Port %d is busy; %s moves to %d (%s in .env).\n", m.From, m.Port.Label, m.To, m.Port.Env)
		}
	}

	apiURL := fmt.Sprintf("http://localhost:%d", ports["AF_STACK_PORT"])

	// Point the app at the runtime before waiting, so even an interrupted
	// first boot leaves .env correct for the next `npm start`.
	urls := map[string]string{"AF_STACK_URL": apiURL}
	if usesViteURL(root) {
		urls["VITE_AF_STACK_URL"] = apiURL
	}
	if err := starter.SetEnv(root, urls, "# Written by `af-stack dev`: the bundled backend's base URL."); err != nil {
		return fmt.Errorf("dev: write .env: %w", err)
	}

	fmt.Fprintf(stdout, "Starting the bundled BackAI backend (compose project %q; the first run pulls the images)...\n", project)
	if err := runCommand(ctx, root, "docker", []string{"compose", "up", "-d"}, stdout, stderr); err != nil {
		return fmt.Errorf("dev: docker compose up: %w", err)
	}

	fmt.Fprintf(stdout, "Waiting for the runtime at %s/ready ", apiURL)
	if err := starter.WaitReady(ctx, apiURL, appReadyTimeout, stdout); err != nil {
		fmt.Fprintln(stdout)
		return fmt.Errorf("dev: %w\n  Inspect with: docker compose logs runtime   (in %s)", err, root)
	}
	fmt.Fprintln(stdout, " ready")

	fmt.Fprintf(stdout, "Waiting for the %s agent to register ", demoAgentNodeID)
	if starter.WaitAgent(ctx, apiURL, demoAgentNodeID, appAgentTimeout, stdout) {
		fmt.Fprintln(stdout, " registered")
	} else {
		fmt.Fprintf(stdout, " not yet — it keeps retrying in the background (docker compose logs %s-agent)\n", demoAgentNodeID)
	}

	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "Bundled backend is up:")
	fmt.Fprintf(stdout, "  API runtime         %s/api/v1\n", apiURL)
	fmt.Fprintf(stdout, "  Your app            AF_STACK_URL=%s  (written to .env)\n", apiURL)
	fmt.Fprintf(stdout, "  Operator dashboard  http://localhost:%d  (operator@af-stack.local / changeme123)\n", ports["AF_STACK_DASHBOARD_PORT"])
	fmt.Fprintf(stdout, "  AgentField UI       http://localhost:%d\n", ports["AGENTFIELD_PORT"])
	fmt.Fprintf(stdout, "  Logs / stop         docker compose logs -f   ·   docker compose down   (in %s)\n", root)
	return nil
}

// usesViteURL reports whether the app reads VITE_AF_STACK_URL (the saas
// template's Vite dev proxy) so dev keeps that key in step too.
func usesViteURL(root string) bool {
	for _, name := range []string{".env", ".env.example"} {
		// #nosec G304 -- files inside the app directory the user is running in.
		data, err := os.ReadFile(filepath.Join(root, name))
		if err == nil && strings.Contains(string(data), "VITE_AF_STACK_URL=") {
			return true
		}
	}
	return false
}
