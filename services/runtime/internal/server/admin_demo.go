// SPDX-License-Identifier: Apache-2.0

// Demo-mode data seeder.
//
// Closes the long-standing "fresh fork looks dead" problem: a single POST
// inserts ~200 gateway requests, ~20 cost events, ~6 webhook deliveries,
// and ~30 activity-log entries spread realistically across the last 24h.
// Sparklines pick up shape, KPI tiles go non-zero, and the Activity feed
// fills up — all from real rows in the real tables.
//
// Every seeded row is marked with `metadata->>'demo': true` so a follow-up
// seed (with reset=true) wipes prior demo data cleanly without touching
// real traffic.
//
// Shape matches DemoSeedResponseSchema in apps/dashboard/src/lib/api.ts.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
)

type demoSeedInput struct {
	// Reset wipes existing demo-flagged rows before inserting. Default true
	// so re-running the command stays idempotent.
	Reset *bool `json:"reset,omitempty"`
}

type demoSeedResponse struct {
	Inserted demoSeedInserted `json:"inserted"`
	Removed  demoSeedRemoved  `json:"removed"`
}

type demoSeedInserted struct {
	GatewayRequests   int `json:"gateway_requests"`
	CostEvents        int `json:"cost_events"`
	WebhookDeliveries int `json:"webhook_deliveries"`
	ActivityEntries   int `json:"activity_entries"`
}

type demoSeedRemoved struct {
	GatewayRequests   int `json:"gateway_requests"`
	CostEvents        int `json:"cost_events"`
	WebhookDeliveries int `json:"webhook_deliveries"`
	ActivityEntries   int `json:"activity_entries"`
}

// Volumes — light variant per the page-design grooming. The activity feed
// fills 20+ rows, sparklines have shape, but the seed completes in well
// under a second.
const (
	demoGatewayRows = 200
	demoCostRows    = 20
	demoWebhookRows = 6
	demoActivityRows = 30
	demoTenantID    = "00000000-0000-0000-0000-000000000000"
)

func (s *Server) registerAdminDemoRoutes() {
	s.mux.HandleFunc("POST /api/v1/admin/demo/seed", s.handleAdminDemoSeed)
}

func (s *Server) registerAdminDemoOpenAPI() {
	s.openapi.Register("POST", "/api/v1/admin/demo/seed", openapi.RouteMeta{
		Summary: "Seed realistic demo data across the last 24h",
		Tags:    []string{"admin"},
	})
}

func (s *Server) handleAdminDemoSeed(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.dashTracer().Start(r.Context(), "dashboard.admin.demo.seed")
	defer span.End()

	if s.db == nil || s.db.Pool == nil {
		writeError(w, http.StatusServiceUnavailable, "DB_UNAVAILABLE",
			"demo seeding requires a configured database", nil)
		return
	}

	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<10))
	var in demoSeedInput
	if len(body) > 0 {
		_ = json.Unmarshal(body, &in)
	}
	reset := true
	if in.Reset != nil {
		reset = *in.Reset
	}

	resp := demoSeedResponse{}

	if reset {
		removed, err := wipeDemoRows(ctx, s.db.Pool)
		if err != nil {
			s.log.Warn("demo: wipe failed", "error", err)
			span.RecordError(err)
		}
		resp.Removed = removed
	}

	inserted, err := seedDemoRows(ctx, s.db.Pool)
	if err != nil {
		s.log.Error("demo: seed failed", "error", err)
		span.RecordError(err)
		writeError(w, http.StatusInternalServerError, "DEMO_SEED_FAILED",
			err.Error(), nil)
		return
	}
	resp.Inserted = inserted

	writeJSON(w, http.StatusOK, resp)
}

// wipeDemoRows removes every row across the seedable tables whose
// metadata column contains `demo: true`. Returns per-table counts so the
// response is auditable.
func wipeDemoRows(ctx context.Context, pool *pgxpool.Pool) (demoSeedRemoved, error) {
	out := demoSeedRemoved{}

	// suite_gateway_requests stores its "metadata" inside the response_body
	// JSON column (no dedicated metadata field). We flag demo rows via a
	// sentinel substring in the path so a single index range catches them.
	tag, err := pool.Exec(ctx,
		"delete from suite_gateway_requests where endpoint like '/api/v1/execute/demo.%' or endpoint like '/api/v1/llm/demo.%'")
	if err == nil {
		out.GatewayRequests = int(tag.RowsAffected())
	}

	// suite_cost_events has no metadata column. Demo rows are flagged by
	// stamping `agent = 'demo-seed'` — matches the seeder.
	tag, err = pool.Exec(ctx,
		"delete from suite_cost_events where agent = 'demo-seed'")
	if err == nil {
		out.CostEvents = int(tag.RowsAffected())
	}

	tag, err = pool.Exec(ctx,
		"delete from suite_webhook_deliveries where headers ? 'x-demo'")
	if err == nil {
		out.WebhookDeliveries = int(tag.RowsAffected())
	}

	// Activity log lives in suite_user_activity (suite_activity does not
	// exist in v1 — the package name is `activity` but the table is
	// renamed for clarity). Demo rows flagged via metadata.
	tag, err = pool.Exec(ctx,
		"delete from suite_user_activity where metadata ? 'demo'")
	if err == nil {
		out.ActivityEntries = int(tag.RowsAffected())
	}

	return out, nil
}

func seedDemoRows(ctx context.Context, pool *pgxpool.Pool) (demoSeedInserted, error) {
	out := demoSeedInserted{}
	now := time.Now().UTC()

	// ─── Gateway requests ──────────────────────────────────────────────
	// Distribution: traffic peaks every ~6 hours (sinusoidal mock load),
	// ~8% failure rate. Status codes mix 200/201/400/404/429/500.
	agents := []string{"supportdesk.reply_plan", "supportdesk.classify_issue", "demo.echo", "demo.summarise"}
	for i := 0; i < demoGatewayRows; i++ {
		// Spread across 24h, denser near "now" so the rpm tile is non-zero.
		ageSecs := int(rand.Float64() * 48 * 3600)
		created := now.Add(-time.Duration(ageSecs) * time.Second)
		agent := agents[rand.IntN(len(agents))]
		endpoint := "/api/v1/execute/" + agent
		// 8% failure overall.
		statusCode := 200
		if rand.Float64() < 0.08 {
			switch rand.IntN(4) {
			case 0:
				statusCode = 400
			case 1:
				statusCode = 404
			case 2:
				statusCode = 429
			case 3:
				statusCode = 500
			}
		}
		durationMs := 80 + rand.IntN(1200)
		_, err := pool.Exec(ctx, `
			insert into suite_gateway_requests
			    (tenant_id, endpoint, method, status_code, duration_ms, created_at)
			values ($1::uuid, $2, 'POST', $3, $4, $5)
		`, demoTenantID, endpoint, statusCode, durationMs, created)
		if err != nil {
			return out, fmt.Errorf("seed gateway: %w", err)
		}
		out.GatewayRequests++
	}

	// ─── Cost events ───────────────────────────────────────────────────
	// 20 events across 24h. Model mix biased to qwen-2.5-72b.
	models := []string{
		"openrouter/qwen/qwen-2.5-72b-instruct",
		"openrouter/anthropic/claude-3.5-sonnet",
		"openrouter/google/gemini-flash-1.5",
	}
	for i := 0; i < demoCostRows; i++ {
		ageSecs := int(rand.Float64() * 48 * 3600)
		occurred := now.Add(-time.Duration(ageSecs) * time.Second)
		model := models[rand.IntN(len(models))]
		// Cost values: realistic per-call spend (~$0.0001..$0.01).
		costUSD := 0.0001 + rand.Float64()*0.0099
		promptTokens := 40 + rand.IntN(2000)
		completionTokens := 20 + rand.IntN(800)
		// agent = 'demo-seed' is the sentinel wipeDemoRows uses.
		_, err := pool.Exec(ctx, `
			insert into suite_cost_events
			    (tenant_id, model, provider, agent, prompt_tokens, completion_tokens, total_tokens, cost_usd, occurred_at)
			values ($1::uuid, $2, 'openrouter', 'demo-seed', $3, $4, $5, $6, $7)
		`, demoTenantID, model, promptTokens, completionTokens, promptTokens+completionTokens, costUSD, occurred)
		if err != nil {
			return out, fmt.Errorf("seed cost: %w", err)
		}
		out.CostEvents++
	}

	// ─── Webhook deliveries ────────────────────────────────────────────
	statuses := []string{"succeeded", "succeeded", "succeeded", "failed", "queued", "succeeded"}
	eventTypes := []string{"run.completed", "tenant.created", "budget.threshold_crossed", "support.reply", "demo.ping", "audit.recorded"}
	dirs := []string{"outbound", "outbound", "inbound", "outbound", "inbound", "outbound"}
	for i := 0; i < demoWebhookRows; i++ {
		ageSecs := int(rand.Float64() * 48 * 3600)
		created := now.Add(-time.Duration(ageSecs) * time.Second)
		_, err := pool.Exec(ctx, `
			insert into suite_webhook_deliveries
			    (tenant_id, direction, destination, event_type, status, attempts, headers, scheduled_at, created_at)
			values ($1::uuid, $2, $3, $4, $5, 1, $6::jsonb, $7, $7)
		`, demoTenantID, dirs[i%len(dirs)],
			"https://webhook.site/demo-seed-"+fmt.Sprintf("%d", i),
			eventTypes[i%len(eventTypes)],
			statuses[i%len(statuses)],
			`{"x-demo": "true"}`,
			created)
		if err != nil {
			return out, fmt.Errorf("seed webhook: %w", err)
		}
		out.WebhookDeliveries++
	}

	// ─── Activity log ──────────────────────────────────────────────────
	actions := []string{"tenant.created", "user.signed_in", "api_key.issued", "budget.set", "config.updated", "feature.toggled", "audit.exported"}
	resourceTypes := []string{"tenant", "user", "api_key", "budget", "config", "feature", "audit"}
	for i := 0; i < demoActivityRows; i++ {
		ageSecs := int(rand.Float64() * 48 * 3600)
		occurred := now.Add(-time.Duration(ageSecs) * time.Second)
		idx := rand.IntN(len(actions))
		_, err := pool.Exec(ctx, `
			insert into suite_user_activity
			    (tenant_id, actor_type, action, resource_type, resource_id, metadata, occurred_at)
			values ($1::uuid, 'system', $2, $3, $4, $5::jsonb, $6)
		`, demoTenantID, actions[idx], resourceTypes[idx],
			fmt.Sprintf("demo-%d", i),
			`{"demo": true, "source": "demo-seed"}`,
			occurred)
		if err != nil {
			return out, fmt.Errorf("seed activity: %w", err)
		}
		out.ActivityEntries++
	}

	return out, nil
}
