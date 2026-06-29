// SPDX-License-Identifier: Apache-2.0

package probe

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const PGStatStatementsProbeID = "pg-stat-statements-loaded"

type PGStatStatementsProbe struct {
	Pool     *pgxpool.Pool
	Interval time.Duration
}

func (p *PGStatStatementsProbe) ID() string              { return PGStatStatementsProbeID }
func (p *PGStatStatementsProbe) Slot() string            { return "data" }
func (p *PGStatStatementsProbe) Schedule() time.Duration { return p.Interval }

func (p *PGStatStatementsProbe) Run(ctx context.Context) (Result, error) {
	res := Result{
		ProbeID:    p.ID(),
		Capability: "db.stat_statements_loaded",
		Value:      false,
		Severity:   SeverityUnavailable,
		LastRun:    time.Now().UTC(),
	}
	if p == nil || p.Pool == nil {
		res.Detail = "Postgres is not configured"
		return res, nil
	}
	var one int
	if err := p.Pool.QueryRow(ctx, `select 1 from pg_stat_statements limit 1`).Scan(&one); err != nil {
		res.Detail = err.Error()
		return res, nil
	}
	res.Value = true
	res.Severity = SeverityOK
	res.Detail = "pg_stat_statements is queryable"
	return res, nil
}
