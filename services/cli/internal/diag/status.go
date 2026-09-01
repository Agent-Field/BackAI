// SPDX-License-Identifier: Apache-2.0

package diag

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/cli/internal/client"
	"github.com/Agent-Field/backai/services/cli/internal/output"
)

// StatusReport is the stable --json schema for `af-stack status`.
type StatusReport struct {
	RuntimeURL string `json:"runtime_url"`
	Reachable  bool   `json:"reachable"`
	Ready      bool   `json:"ready"`
	Mode       string `json:"mode"`
	Authed     bool   `json:"authed"`
	Agents     int    `json:"agents"`
	Detail     string `json:"detail"`
}

// RunStatus prints a compact "is the stack up and what is it configured as"
// snapshot. It always exits 0 — status reports state, it does not gate on it.
func RunStatus(ctx context.Context, c *client.Client, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("af-stack status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "emit the status snapshot as JSON")
	if err := fs.Parse(args); err != nil {
		return output.Usage("status: %v", err)
	}

	root := findRoot()
	rep := StatusReport{
		RuntimeURL: c.BaseURL,
		Mode:       strings.ToLower(readEnvValue(root, "AF_STACK_MODE", "saas")),
		Authed:     c.APIKey != "",
	}

	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if status, _, err := c.Probe(pctx, "/ready"); err == nil {
		rep.Reachable = true
		rep.Ready = status >= 200 && status < 300
	}

	if rep.Reachable {
		var out struct {
			Agents []struct {
				NodeID string `json:"node_id"`
			} `json:"agents"`
		}
		if err := c.Do(pctx, "GET", "/agents", nil, &out); err == nil {
			rep.Agents = len(out.Agents)
		}
	}

	switch {
	case !rep.Reachable:
		rep.Detail = "runtime unreachable — start it with `af-stack dev`"
	case !rep.Ready:
		rep.Detail = "runtime up but not ready (booting or a dependency is down)"
	default:
		rep.Detail = "runtime ready"
	}

	return output.Result(stdout, *asJSON, rep, func(w io.Writer) error {
		fmt.Fprintf(w, "runtime : %s\n", rep.RuntimeURL)
		state := "down"
		switch {
		case rep.Reachable && rep.Ready:
			state = "ready"
		case rep.Reachable:
			state = "not-ready"
		}
		fmt.Fprintf(w, "state   : %s\n", state)
		fmt.Fprintf(w, "mode    : %s\n", rep.Mode)
		fmt.Fprintf(w, "auth    : %s\n", authWord(rep.Authed))
		if rep.Reachable {
			fmt.Fprintf(w, "agents  : %d\n", rep.Agents)
		}
		fmt.Fprintf(w, "%s\n", rep.Detail)
		return nil
	})
}

func authWord(authed bool) string {
	if authed {
		return "AF_STACK_API_KEY set"
	}
	return "no key (anonymous)"
}
