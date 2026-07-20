// SPDX-License-Identifier: Apache-2.0

// Package conncmd implements `af-stack connection` — thin wrappers over the
// runtime connections surface (R5) that lets app operators register and
// inspect external-service credentials without curl:
//
//	af-stack connection add --provider github --kind api_key --name ci
//	af-stack connection add --provider google --kind oauth   --name gcal
//	af-stack connection list
//	af-stack connection remove <id> --yes
//
// A connection stores a credential SERVER-SIDE; app code later calls the
// provider through the handle (POST /connections/{id}/request) and the
// runtime injects the secret. The CLI never sends a credential as an argv
// value (it would leak into shell history): `add --kind api_key` prompts, or
// reads the secret from stdin with --credential-stdin. `list` shows metadata
// + health only — never the credential. Every command honours --json and the
// standard exit codes.
package conncmd

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/Agent-Field/backai/services/cli/internal/client"
	"github.com/Agent-Field/backai/services/cli/internal/output"
)

// newFlagSet builds a ContinueOnError flag set that writes usage to stderr,
// so a parse error is a clean exit-2 rather than a panic to os.Stderr.
func newFlagSet(stderr io.Writer, name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// connection mirrors connections.Connection on the wire. Only metadata +
// derived health are ever present — a credential is never returned.
type connection struct {
	ID               string   `json:"id"`
	Provider         string   `json:"provider"`
	Kind             string   `json:"kind"`
	Name             string   `json:"name"`
	GrantedScopes    []string `json:"granted_scopes"`
	RequestedScopes  []string `json:"requested_scopes"`
	Status           string   `json:"status"`
	TokenExpiry      *string  `json:"token_expiry,omitempty"`
	HasWebhookSecret bool     `json:"has_webhook_secret"`
	CreatedBy        string   `json:"created_by,omitempty"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
	Health           string   `json:"health,omitempty"`
}

// authorizeResponse mirrors the runtime's oauth authorize payload.
type authorizeResponse struct {
	Provider         string `json:"provider"`
	Kind             string `json:"kind"`
	AuthorizationURL string `json:"authorization_url"`
	State            string `json:"state"`
	RedirectURI      string `json:"redirect_uri"`
}

// Run dispatches `af-stack connection <subcommand>`.
func Run(ctx context.Context, c *client.Client, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return output.Usage("connection: subcommand required: add | list | remove")
	}
	switch args[0] {
	case "add", "create":
		return runAdd(ctx, c, args[1:], stdin, stdout, stderr)
	case "list", "ls":
		return runList(ctx, c, args[1:], stdout, stderr)
	case "remove", "rm", "revoke":
		return runRemove(ctx, c, args[1:], stdin, stdout, stderr)
	default:
		return output.Usage("connection: unknown subcommand %q (want add | list | remove)", args[0])
	}
}

func runAdd(ctx context.Context, c *client.Client, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := newFlagSet(stderr, "af-stack connection add")
	provider := fs.String("provider", "", "provider: github | stripe | google | slack | ... (required)")
	kind := fs.String("kind", "", "connection kind: api_key | oauth (required)")
	name := fs.String("name", "", "human label for this connection")
	scopes := fs.String("scopes", "", "comma-separated scopes to request")
	credStdin := fs.Bool("credential-stdin", false, "read the api_key credential from stdin instead of prompting")
	asJSON := fs.Bool("json", false, "emit the runtime response as JSON")
	if _, err := output.ParseArgs(fs, args); err != nil {
		return output.Usage("connection add: %v", err)
	}
	if strings.TrimSpace(*provider) == "" {
		return output.Usage("connection add: --provider is required")
	}
	k := strings.ToLower(strings.TrimSpace(*kind))
	body := map[string]any{"provider": strings.TrimSpace(*provider), "kind": k}
	if strings.TrimSpace(*name) != "" {
		body["name"] = strings.TrimSpace(*name)
	}
	if s := splitScopes(*scopes); len(s) > 0 {
		body["scopes"] = s
	}

	switch k {
	case "api_key":
		cred, err := readCredential(stdin, stderr, *credStdin, *provider)
		if err != nil {
			return err
		}
		body["api_key"] = cred
		var out connection
		if err := c.Do(ctx, "POST", "/connections", body, &out); err != nil {
			return err
		}
		return output.Result(stdout, *asJSON, out, func(w io.Writer) error {
			fmt.Fprintf(w, "connection created: %s\n", out.ID)
			fmt.Fprintf(w, "  provider: %s\n  kind    : %s\n  health  : %s\n",
				out.Provider, out.Kind, orDash(out.Health))
			return nil
		})
	case "oauth":
		var out authorizeResponse
		if err := c.Do(ctx, "POST", "/connections", body, &out); err != nil {
			return err
		}
		return output.Result(stdout, *asJSON, out, func(w io.Writer) error {
			fmt.Fprintf(w, "Authorize %s by opening this URL in a browser:\n\n  %s\n\n",
				out.Provider, out.AuthorizationURL)
			fmt.Fprintln(w, "After you approve, the runtime completes the connection via its callback.")
			return nil
		})
	case "":
		return output.Usage("connection add: --kind is required (api_key | oauth)")
	default:
		return output.Usage("connection add: --kind must be 'api_key' or 'oauth' (got %q)", *kind)
	}
}

func runList(ctx context.Context, c *client.Client, args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet(stderr, "af-stack connection list")
	asJSON := fs.Bool("json", false, "emit the connections as JSON")
	if _, err := output.ParseArgs(fs, args); err != nil {
		return output.Usage("connection list: %v", err)
	}
	var out struct {
		Connections []connection `json:"connections"`
	}
	if err := c.Do(ctx, "GET", "/connections", nil, &out); err != nil {
		return err
	}
	return output.Result(stdout, *asJSON, out, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tPROVIDER\tKIND\tNAME\tSTATUS\tHEALTH")
		for _, conn := range out.Connections {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				conn.ID, conn.Provider, conn.Kind, orDash(conn.Name), conn.Status, orDash(conn.Health))
		}
		return tw.Flush()
	})
}

func runRemove(ctx context.Context, c *client.Client, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := newFlagSet(stderr, "af-stack connection remove")
	yes := fs.Bool("yes", false, "skip the confirmation prompt")
	asJSON := fs.Bool("json", false, "emit the result as JSON")
	positionals, err := output.ParseArgs(fs, args)
	if err != nil {
		return output.Usage("connection remove: %v", err)
	}
	if len(positionals) != 1 {
		return output.Usage("connection remove: exactly one <id> is required (af-stack connection remove <id> [--yes])")
	}
	id := positionals[0]

	if !*yes && !confirm(stdin, stderr, fmt.Sprintf("Remove connection %s? [y/N] ", id)) {
		return output.Result(stdout, *asJSON, map[string]any{"removed": false, "id": id}, func(w io.Writer) error {
			fmt.Fprintln(w, "aborted")
			return nil
		})
	}
	if err := c.Do(ctx, "DELETE", "/connections/"+id, nil, nil); err != nil {
		return err
	}
	return output.Result(stdout, *asJSON, map[string]any{"removed": true, "id": id}, func(w io.Writer) error {
		fmt.Fprintf(w, "removed connection %s\n", id)
		return nil
	})
}

// readCredential returns the api_key credential WITHOUT ever touching argv.
// With --credential-stdin the whole of stdin is the secret; otherwise the
// caller is prompted and a single line is read. Empty is a validation error.
func readCredential(stdin io.Reader, stderr io.Writer, all bool, provider string) (string, error) {
	var cred string
	if all {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return "", output.Fail("connection add: read credential from stdin: %v", err)
		}
		cred = strings.TrimRight(string(b), "\r\n")
	} else {
		fmt.Fprintf(stderr, "Paste the %s API key (input not echoed to history): ", provider)
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", output.Fail("connection add: read credential: %v", err)
		}
		cred = strings.TrimRight(line, "\r\n")
	}
	if strings.TrimSpace(cred) == "" {
		return "", output.Invalid("connection add: an api_key credential is required (pipe it via --credential-stdin or type it at the prompt)")
	}
	return cred, nil
}

// confirm reads a single y/N answer. A non-affirmative answer (including EOF
// on an empty stdin) is treated as "no".
func confirm(stdin io.Reader, stderr io.Writer, prompt string) bool {
	fmt.Fprint(stderr, prompt)
	line, _ := bufio.NewReader(stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

func splitScopes(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
