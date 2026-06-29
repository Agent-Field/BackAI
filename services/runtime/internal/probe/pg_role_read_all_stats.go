// SPDX-License-Identifier: Apache-2.0

package probe

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const PGRoleReadAllStatsProbeID = "pg-role-read-all-stats"

type PGRoleReadAllStatsProbe struct {
	Pool     *pgxpool.Pool
	Interval time.Duration
}

func (p *PGRoleReadAllStatsProbe) ID() string              { return PGRoleReadAllStatsProbeID }
func (p *PGRoleReadAllStatsProbe) Slot() string            { return "data" }
func (p *PGRoleReadAllStatsProbe) Schedule() time.Duration { return p.Interval }

func (p *PGRoleReadAllStatsProbe) Run(ctx context.Context) (Result, error) {
	res := Result{
		ProbeID:    p.ID(),
		Capability: "db.role_has_read_all_stats",
		Value:      false,
		Severity:   SeverityUnavailable,
		LastRun:    time.Now().UTC(),
	}
	if p == nil || p.Pool == nil {
		res.Detail = "Postgres is not configured"
		return res, nil
	}
	var ok bool
	if err := p.Pool.QueryRow(ctx, `select pg_has_role(current_user, 'pg_read_all_stats', 'usage')`).Scan(&ok); err != nil {
		res.Detail = err.Error()
		return res, nil
	}
	res.Value = ok
	if ok {
		res.Severity = SeverityOK
		res.Detail = "runtime role has pg_read_all_stats"
	} else {
		res.Detail = "runtime role does not have pg_read_all_stats"
	}
	return res, nil
}
