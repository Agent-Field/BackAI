// SPDX-License-Identifier: Apache-2.0

// Package secretcmd implements `af-stack secrets` — thin wrappers over the
// per-tenant secrets vault (/api/v1/vault/secrets) consumed with a tenant
// API key:
//
//	echo -n "$STRIPE_KEY" | af-stack secrets set stripe --value-stdin
//	af-stack secrets set stripe          # prompts for the value
//	af-stack secrets list
//
// A secret VALUE never travels through argv (it would leak into shell
// history): `set` reads it from stdin (whole stream with --value-stdin, or a
// prompted line otherwise). `list` shows metadata + the `secret:<key>`
// reference only — never the plaintext, which the vault reveals solely
// through the audited /reveal path. Both honour --json and the standard exit
// codes.
package secretcmd

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

// secretMetadata mirrors the tenant vault's metadata wire shape. It carries
// NO plaintext value — only the reference the caller can drop into config.
type secretMetadata struct {
	Key         string  `json:"key"`
	Description *string `json:"description"`
	RotateAfter *string `json:"rotate_after"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	Reference   string  `json:"reference"`
}

func newFlagSet(stderr io.Writer, name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// Run dispatches `af-stack secrets <subcommand>`.
func Run(ctx context.Context, c *client.Client, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return output.Usage("secrets: subcommand required: set | list")
	}
	switch args[0] {
	case "set", "put":
		return runSet(ctx, c, args[1:], stdin, stdout, stderr)
	case "list", "ls":
		return runList(ctx, c, args[1:], stdout, stderr)
	default:
		return output.Usage("secrets: unknown subcommand %q (want set | list)", args[0])
	}
}

func runSet(ctx context.Context, c *client.Client, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := newFlagSet(stderr, "af-stack secrets set")
	valueStdin := fs.Bool("value-stdin", false, "read the whole value from stdin instead of prompting")
	desc := fs.String("description", "", "optional human description")
	asJSON := fs.Bool("json", false, "emit the stored metadata as JSON")
	positionals, err := output.ParseArgs(fs, args)
	if err != nil {
		return output.Usage("secrets set: %v", err)
	}
	if len(positionals) != 1 {
		return output.Usage("secrets set: exactly one <key> is required (af-stack secrets set <key> [--value-stdin])")
	}
	key := positionals[0]

	value, err := readValue(stdin, stderr, *valueStdin, key)
	if err != nil {
		return err
	}
	body := map[string]any{"value": value}
	if strings.TrimSpace(*desc) != "" {
		body["description"] = strings.TrimSpace(*desc)
	}

	var out secretMetadata
	if err := c.Do(ctx, "PUT", "/vault/secrets/"+key, body, &out); err != nil {
		return err
	}
	return output.Result(stdout, *asJSON, out, func(w io.Writer) error {
		fmt.Fprintf(w, "stored secret %q\n", out.Key)
		fmt.Fprintf(w, "  reference : %s\n", out.Reference)
		fmt.Fprintf(w, "  updated_at: %s\n", out.UpdatedAt)
		fmt.Fprintln(w, "Reference it in config as the value above — the plaintext never leaves the vault except via an audited reveal.")
		return nil
	})
}

func runList(ctx context.Context, c *client.Client, args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet(stderr, "af-stack secrets list")
	asJSON := fs.Bool("json", false, "emit the secret metadata as JSON")
	if _, err := output.ParseArgs(fs, args); err != nil {
		return output.Usage("secrets list: %v", err)
	}
	var out struct {
		Secrets []secretMetadata `json:"secrets"`
	}
	if err := c.Do(ctx, "GET", "/vault/secrets", nil, &out); err != nil {
		return err
	}
	return output.Result(stdout, *asJSON, out, func(w io.Writer) error {
		tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "KEY\tUPDATED\tREFERENCE\tDESCRIPTION")
		for _, s := range out.Secrets {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.Key, s.UpdatedAt, s.Reference, deref(s.Description))
		}
		return tw.Flush()
	})
}

// readValue returns the secret value WITHOUT ever touching argv. With
// --value-stdin the whole stream is the value; otherwise the caller is
// prompted and one line is read. Empty is a validation error.
func readValue(stdin io.Reader, stderr io.Writer, all bool, key string) (string, error) {
	var value string
	if all {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return "", output.Fail("secrets set: read value from stdin: %v", err)
		}
		value = strings.TrimRight(string(b), "\r\n")
	} else {
		fmt.Fprintf(stderr, "Enter the value for %q (input not echoed to history): ", key)
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", output.Fail("secrets set: read value: %v", err)
		}
		value = strings.TrimRight(line, "\r\n")
	}
	if value == "" {
		return "", output.Invalid("secrets set: a non-empty value is required (pipe it via --value-stdin or type it at the prompt)")
	}
	return value, nil
}

func deref(s *string) string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return "-"
	}
	return *s
}
