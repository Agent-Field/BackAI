// SPDX-License-Identifier: Apache-2.0

// Package admincmd implements the operator-surface subcommands:
//
//	af-stack keys list|issue|rotate|revoke|spend
//	af-stack agents list
//	af-stack reasoners
//	af-stack logs
//	af-stack errors list|resolve|mute|reopen
//	af-stack audit
//	af-stack sessions list|revoke
//	af-stack runs
//	af-stack tenants list
//	af-stack activity
//
// Every command is a thin wrapper over one runtime REST endpoint via
// client.Do — the same surface the dashboard uses. Auth: set
// AF_STACK_API_KEY to an OPERATOR key (minted on the zero-uuid tenant
// with scope "operator" or "operator:owner" — see `af-stack operator
// key`). Ordinary tenant keys are rejected by the operator gate.
package admincmd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/Agent-Field/backai/services/cli/internal/client"
)

func tab(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
}

func deref(s *string) string {
	if s == nil {
		return "-"
	}
	return *s
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// ─── keys ─────────────────────────────────────────────────────────────────

type apiKeyWire struct {
	ID           string   `json:"id"`
	TenantID     string   `json:"tenant_id"`
	Prefix       string   `json:"prefix"`
	Name         *string  `json:"name"`
	Scopes       []string `json:"scopes"`
	CreatedAt    string   `json:"created_at"`
	LastUsedAt   *string  `json:"last_used_at"`
	ExpiresAt    *string  `json:"expires_at"`
	RevokedAt    *string  `json:"revoked_at"`
	BudgetMaxUSD *float64 `json:"budget_max_usd"`
	LiveSpendUSD *float64 `json:"live_spend_usd"`
	Value        string   `json:"value,omitempty"` // present on issue/rotate only
}

func RunKeys(ctx context.Context, c *client.Client, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("keys: subcommand required: list|issue|rotate|revoke|spend")
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("af-stack keys list", flag.ContinueOnError)
		fs.SetOutput(stderr)
		tenant := fs.String("tenant", "", "filter by tenant id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		path := "/admin/keys"
		if *tenant != "" {
			path += "?tenant=" + *tenant
		}
		var out struct {
			Keys []apiKeyWire `json:"keys"`
		}
		if err := c.Do(ctx, "GET", path, nil, &out); err != nil {
			return err
		}
		tw := tab(stdout)
		fmt.Fprintln(tw, "ID\tPREFIX\tNAME\tTENANT\tSCOPES\tSPEND/BUDGET\tSTATE")
		for _, k := range out.Keys {
			state := "active"
			if k.RevokedAt != nil {
				state = "revoked"
			}
			spend := "-"
			if k.LiveSpendUSD != nil {
				spend = fmt.Sprintf("$%.4f", *k.LiveSpendUSD)
			}
			if k.BudgetMaxUSD != nil {
				spend += fmt.Sprintf("/$%.2f", *k.BudgetMaxUSD)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				k.ID, k.Prefix, deref(k.Name), trunc(k.TenantID, 12),
				strings.Join(k.Scopes, ","), spend, state)
		}
		return tw.Flush()
	case "issue":
		fs := flag.NewFlagSet("af-stack keys issue", flag.ContinueOnError)
		fs.SetOutput(stderr)
		tenant := fs.String("tenant", "", "tenant id (required)")
		name := fs.String("name", "", "key name")
		scopes := fs.String("scopes", "", "comma-separated scopes")
		budget := fs.Float64("budget", 0, "budget_max_usd (0 = none)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *tenant == "" {
			return errors.New("keys issue: --tenant is required")
		}
		body := map[string]any{"tenant_id": *tenant}
		if *name != "" {
			body["name"] = *name
		}
		if *scopes != "" {
			body["scopes"] = strings.Split(*scopes, ",")
		}
		if *budget > 0 {
			body["budget_max_usd"] = *budget
		}
		var out apiKeyWire
		if err := c.Do(ctx, "POST", "/admin/keys", body, &out); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "key id: %s\nprefix: %s\n\n%s\n\nStore this value now — it is shown exactly once.\n",
			out.ID, out.Prefix, out.Value)
		return nil
	case "rotate":
		if len(args) < 2 {
			return errors.New("keys rotate: key id required")
		}
		var out apiKeyWire
		if err := c.Do(ctx, "POST", "/admin/keys/"+args[1]+"/rotate", nil, &out); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "rotated %s\n\n%s\n\nStore this value now — it is shown exactly once.\n", out.ID, out.Value)
		return nil
	case "revoke":
		if len(args) < 2 {
			return errors.New("keys revoke: key id required")
		}
		if err := c.Do(ctx, "DELETE", "/admin/keys/"+args[1], nil, nil); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "revoked %s\n", args[1])
		return nil
	case "spend":
		if len(args) < 2 {
			return errors.New("keys spend: key id required")
		}
		var out map[string]any
		if err := c.Do(ctx, "GET", "/admin/keys/"+args[1]+"/spend", nil, &out); err != nil {
			return err
		}
		return printJSON(stdout, out)
	default:
		return fmt.Errorf("keys: unknown subcommand %q", args[0])
	}
}

// ─── agents ───────────────────────────────────────────────────────────────

func RunAgents(ctx context.Context, c *client.Client, args []string, stdout, _ io.Writer) error {
	if len(args) > 0 && args[0] != "list" {
		return fmt.Errorf("agents: unknown subcommand %q (only: list)", args[0])
	}
	var out struct {
		Agents []struct {
			NodeID    string   `json:"node_id"`
			Version   string   `json:"version"`
			Tags      []string `json:"tags"`
			Reasoners []string `json:"reasoners"`
		} `json:"agents"`
	}
	if err := c.Do(ctx, "GET", "/agents", nil, &out); err != nil {
		return err
	}
	tw := tab(stdout)
	fmt.Fprintln(tw, "AGENT\tVERSION\tREASONERS\tTAGS")
	for _, a := range out.Agents {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			a.NodeID, a.Version, strings.Join(a.Reasoners, ","), strings.Join(a.Tags, ","))
	}
	return tw.Flush()
}

// ─── reasoners ────────────────────────────────────────────────────────────

func RunReasoners(ctx context.Context, c *client.Client, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("af-stack reasoners", flag.ContinueOnError)
	fs.SetOutput(stderr)
	from := fs.String("from", "", "window start (RFC3339, default 24h ago)")
	to := fs.String("to", "", "window end (RFC3339)")
	limit := fs.Int("limit", 100, "max rows")
	if err := fs.Parse(args); err != nil {
		return err
	}
	q := "?limit=" + strconv.Itoa(*limit)
	if *from != "" {
		q += "&from=" + *from
	}
	if *to != "" {
		q += "&to=" + *to
	}
	var out struct {
		Reasoners []struct {
			Agent        string  `json:"agent"`
			Reasoner     string  `json:"reasoner"`
			Calls        int64   `json:"calls"`
			Errors       int64   `json:"errors"`
			ErrorRate    float64 `json:"error_rate"`
			AvgLatencyMS float64 `json:"avg_latency_ms"`
			CostUSD      float64 `json:"cost_usd"`
			LastCalledAt string  `json:"last_called_at"`
		} `json:"reasoners"`
		Window struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"window"`
	}
	if err := c.Do(ctx, "GET", "/reasoners/analytics"+q, nil, &out); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "window %s → %s\n", out.Window.From, out.Window.To)
	tw := tab(stdout)
	fmt.Fprintln(tw, "REASONER\tCALLS\tERRORS\tERR%\tAVG MS\tCOST USD\tLAST CALLED")
	for _, r := range out.Reasoners {
		fmt.Fprintf(tw, "%s.%s\t%d\t%d\t%.1f%%\t%.0f\t$%.4f\t%s\n",
			r.Agent, r.Reasoner, r.Calls, r.Errors, r.ErrorRate*100,
			r.AvgLatencyMS, r.CostUSD, r.LastCalledAt)
	}
	return tw.Flush()
}

// ─── logs ─────────────────────────────────────────────────────────────────

func RunLogs(ctx context.Context, c *client.Client, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("af-stack logs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	level := fs.String("level", "", "min level filter (debug|info|warn|error)")
	service := fs.String("service", "", "service filter")
	search := fs.String("search", "", "substring search")
	limit := fs.Int("limit", 50, "max lines")
	if err := fs.Parse(args); err != nil {
		return err
	}
	q := "?limit=" + strconv.Itoa(*limit)
	if *level != "" {
		q += "&level=" + *level
	}
	if *service != "" {
		q += "&service=" + *service
	}
	if *search != "" {
		q += "&search=" + *search
	}
	var out struct {
		Logs []struct {
			TS      string         `json:"ts"`
			Level   string         `json:"level"`
			Service string         `json:"service"`
			Msg     string         `json:"msg"`
			Fields  map[string]any `json:"fields"`
		} `json:"logs"`
	}
	if err := c.Do(ctx, "GET", "/admin/logs"+q, nil, &out); err != nil {
		return err
	}
	for _, l := range out.Logs {
		extra := ""
		if len(l.Fields) > 0 {
			if b, err := json.Marshal(l.Fields); err == nil {
				extra = " " + string(b)
			}
		}
		fmt.Fprintf(stdout, "%s %-5s %s %s%s\n", l.TS, strings.ToUpper(l.Level), l.Service, l.Msg, extra)
	}
	return nil
}

// ─── errors ───────────────────────────────────────────────────────────────

func RunErrors(ctx context.Context, c *client.Client, args []string, stdout, stderr io.Writer) error {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}
	switch sub {
	case "list":
		fs := flag.NewFlagSet("af-stack errors list", flag.ContinueOnError)
		fs.SetOutput(stderr)
		status := fs.String("status", "", "filter: open|muted|resolved")
		limit := fs.Int("limit", 50, "max groups")
		if err := fs.Parse(args); err != nil {
			return err
		}
		q := "?limit=" + strconv.Itoa(*limit)
		if *status != "" {
			q += "&status=" + *status
		}
		var out struct {
			Groups []struct {
				ID       string `json:"id"`
				Title    string `json:"title"`
				Service  string `json:"service"`
				Status   string `json:"status"`
				Count    int    `json:"count"`
				LastSeen string `json:"last_seen"`
			} `json:"groups"`
			Total int `json:"total"`
		}
		if err := c.Do(ctx, "GET", "/admin/errors"+q, nil, &out); err != nil {
			return err
		}
		tw := tab(stdout)
		fmt.Fprintln(tw, "ID\tSTATUS\tCOUNT\tSERVICE\tLAST SEEN\tTITLE")
		for _, g := range out.Groups {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\n",
				g.ID, g.Status, g.Count, g.Service, g.LastSeen, trunc(g.Title, 80))
		}
		return tw.Flush()
	case "resolve", "mute", "reopen":
		if len(args) < 1 {
			return fmt.Errorf("errors %s: group id required", sub)
		}
		var out map[string]any
		if err := c.Do(ctx, "POST", "/admin/errors/"+args[0]+"/"+sub, nil, &out); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "%s: %s\n", sub, args[0])
		return nil
	default:
		return fmt.Errorf("errors: unknown subcommand %q", sub)
	}
}

// ─── audit ────────────────────────────────────────────────────────────────

func RunAudit(ctx context.Context, c *client.Client, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("af-stack audit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tenant := fs.String("tenant", "", "filter by tenant id")
	action := fs.String("action", "", "filter by action")
	limit := fs.Int("limit", 50, "max entries")
	if err := fs.Parse(args); err != nil {
		return err
	}
	q := "?limit=" + strconv.Itoa(*limit)
	if *tenant != "" {
		q += "&tenant=" + *tenant
	}
	if *action != "" {
		q += "&action=" + *action
	}
	var out struct {
		Entries []struct {
			OccurredAt   string  `json:"occurred_at"`
			Action       string  `json:"action"`
			ResourceType *string `json:"resource_type"`
			ResourceID   *string `json:"resource_id"`
			TenantID     string  `json:"tenant_id"`
			UserID       *string `json:"user_id"`
			APIKeyID     *string `json:"api_key_id"`
		} `json:"entries"`
		Total int `json:"total"`
	}
	if err := c.Do(ctx, "GET", "/admin/audit"+q, nil, &out); err != nil {
		return err
	}
	tw := tab(stdout)
	fmt.Fprintln(tw, "OCCURRED\tACTION\tRESOURCE\tTENANT\tACTOR")
	for _, e := range out.Entries {
		actor := deref(e.UserID)
		if actor == "-" && e.APIKeyID != nil {
			actor = "key:" + *e.APIKeyID
		}
		fmt.Fprintf(tw, "%s\t%s\t%s/%s\t%s\t%s\n",
			e.OccurredAt, e.Action, deref(e.ResourceType), trunc(deref(e.ResourceID), 14),
			trunc(e.TenantID, 12), trunc(actor, 20))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%d of %d entries\n", len(out.Entries), out.Total)
	return nil
}

// ─── sessions ─────────────────────────────────────────────────────────────

func RunSessions(ctx context.Context, c *client.Client, args []string, stdout, stderr io.Writer) error {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}
	switch sub {
	case "list":
		fs := flag.NewFlagSet("af-stack sessions list", flag.ContinueOnError)
		fs.SetOutput(stderr)
		email := fs.String("email", "", "filter by email substring")
		limit := fs.Int("limit", 50, "max sessions")
		if err := fs.Parse(args); err != nil {
			return err
		}
		q := "?limit=" + strconv.Itoa(*limit)
		if *email != "" {
			q += "&email=" + *email
		}
		var out struct {
			Sessions []struct {
				ID         string  `json:"id"`
				Email      string  `json:"email"`
				IsOperator bool    `json:"is_operator"`
				IPAddress  *string `json:"ip_address"`
				CreatedAt  string  `json:"created_at"`
				ExpiresAt  string  `json:"expires_at"`
			} `json:"sessions"`
			Total int `json:"total"`
		}
		if err := c.Do(ctx, "GET", "/admin/sessions"+q, nil, &out); err != nil {
			return err
		}
		tw := tab(stdout)
		fmt.Fprintln(tw, "ID\tEMAIL\tKIND\tIP\tCREATED\tEXPIRES")
		for _, s := range out.Sessions {
			kind := "customer"
			if s.IsOperator {
				kind = "operator"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				s.ID, s.Email, kind, deref(s.IPAddress), s.CreatedAt, s.ExpiresAt)
		}
		return tw.Flush()
	case "revoke":
		if len(args) < 1 {
			return errors.New("sessions revoke: session id required")
		}
		if err := c.Do(ctx, "DELETE", "/admin/sessions/"+args[0], nil, nil); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "revoked session %s\n", args[0])
		return nil
	default:
		return fmt.Errorf("sessions: unknown subcommand %q", sub)
	}
}

// ─── runs ─────────────────────────────────────────────────────────────────

func RunRuns(ctx context.Context, c *client.Client, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("af-stack runs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	agent := fs.String("agent", "", "filter by agent/reasoner label")
	status := fs.String("status", "", "filter: succeeded|failed")
	limit := fs.Int("limit", 25, "max runs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	q := "?limit=" + strconv.Itoa(*limit)
	if *agent != "" {
		q += "&agent=" + *agent
	}
	if *status != "" {
		q += "&status=" + *status
	}
	var out struct {
		Runs []struct {
			ID         string   `json:"id"`
			Agent      string   `json:"agent"`
			Status     string   `json:"status"`
			TenantName *string  `json:"tenant_name"`
			StartedAt  string   `json:"started_at"`
			DurationMS *float64 `json:"duration_ms"`
			CostUSD    *float64 `json:"cost_usd"`
		} `json:"runs"`
		Total int `json:"total"`
	}
	if err := c.Do(ctx, "GET", "/runs"+q, nil, &out); err != nil {
		return err
	}
	tw := tab(stdout)
	fmt.Fprintln(tw, "STARTED\tAGENT\tSTATUS\tTENANT\tMS\tCOST")
	for _, r := range out.Runs {
		ms, cost := "-", "-"
		if r.DurationMS != nil {
			ms = fmt.Sprintf("%.0f", *r.DurationMS)
		}
		if r.CostUSD != nil {
			cost = fmt.Sprintf("$%.4f", *r.CostUSD)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			r.StartedAt, r.Agent, r.Status, deref(r.TenantName), ms, cost)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%d of %d runs\n", len(out.Runs), out.Total)
	return nil
}

// ─── tenants ──────────────────────────────────────────────────────────────

func RunTenants(ctx context.Context, c *client.Client, args []string, stdout, _ io.Writer) error {
	if len(args) > 0 && args[0] != "list" {
		return fmt.Errorf("tenants: unknown subcommand %q (only: list)", args[0])
	}
	var out struct {
		Tenants []struct {
			ID        string  `json:"id"`
			Name      string  `json:"name"`
			Slug      *string `json:"slug"`
			CreatedAt string  `json:"created_at"`
		} `json:"tenants"`
	}
	if err := c.Do(ctx, "GET", "/admin/tenants", nil, &out); err != nil {
		return err
	}
	tw := tab(stdout)
	fmt.Fprintln(tw, "ID\tNAME\tSLUG\tCREATED")
	for _, t := range out.Tenants {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", t.ID, t.Name, deref(t.Slug), t.CreatedAt)
	}
	return tw.Flush()
}

// ─── activity ─────────────────────────────────────────────────────────────

func RunActivity(ctx context.Context, c *client.Client, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("af-stack activity", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tenant := fs.String("tenant", "", "filter by tenant id")
	action := fs.String("action", "", "filter by action")
	limit := fs.Int("limit", 50, "max entries")
	if err := fs.Parse(args); err != nil {
		return err
	}
	q := "?limit=" + strconv.Itoa(*limit)
	if *tenant != "" {
		q += "&tenant=" + *tenant
	}
	if *action != "" {
		q += "&action=" + *action
	}
	var out struct {
		Entries []struct {
			OccurredAt   string  `json:"occurred_at"`
			ActorType    string  `json:"actor_type"`
			Action       string  `json:"action"`
			ResourceType *string `json:"resource_type"`
			ResourceID   *string `json:"resource_id"`
			TenantID     string  `json:"tenant_id"`
		} `json:"entries"`
		Total int `json:"total"`
	}
	if err := c.Do(ctx, "GET", "/admin/activity"+q, nil, &out); err != nil {
		return err
	}
	tw := tab(stdout)
	fmt.Fprintln(tw, "OCCURRED\tACTOR\tACTION\tRESOURCE\tTENANT")
	for _, e := range out.Entries {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s/%s\t%s\n",
			e.OccurredAt, e.ActorType, e.Action,
			deref(e.ResourceType), trunc(deref(e.ResourceID), 14), trunc(e.TenantID, 12))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%d of %d entries\n", len(out.Entries), out.Total)
	return nil
}

func printJSON(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}
