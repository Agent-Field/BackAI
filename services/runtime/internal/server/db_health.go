// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
)

type dbHealthResponse struct {
	Available     bool             `json:"available"`
	Reason        string           `json:"reason,omitempty"`
	Connections   dbConnections    `json:"connections"`
	CacheHitRatio float64          `json:"cache_hit_ratio"`
	SlowQueries   []dbSlowQuery    `json:"slow_queries"`
	LargestTables []dbLargestTable `json:"largest_tables"`
	VacuumStatus  []dbVacuumStatus `json:"vacuum_status"`
	Locks         []dbLock         `json:"locks"`
	CheckedAt     string           `json:"checked_at"`
}

type dbConnections struct {
	Active int `json:"active"`
	Idle   int `json:"idle"`
	Max    int `json:"max"`
}

type dbSlowQuery struct {
	Query   string  `json:"query"`
	Calls   int64   `json:"calls"`
	MeanMS  float64 `json:"mean_ms"`
	TotalMS float64 `json:"total_ms"`
}

type dbLargestTable struct {
	Schema    string `json:"schema"`
	Table     string `json:"table"`
	SizeBytes int64  `json:"size_bytes"`
	RowCount  int64  `json:"row_count"`
}

type dbVacuumStatus struct {
	Table      string  `json:"table"`
	LastVacuum *string `json:"last_vacuum"`
	DeadTuples int64   `json:"dead_tuples"`
}

type dbLock struct {
	PID      int    `json:"pid"`
	Mode     string `json:"mode"`
	Granted  bool   `json:"granted"`
	Relation string `json:"relation,omitempty"`
	AgeMS    int64  `json:"age_ms"`
}

func (s *Server) registerDBHealthRoutes() {
	s.mux.HandleFunc("GET /api/v1/admin/db/health", s.handleAdminDBHealth)
}

func (s *Server) registerDBHealthOpenAPI() {
	s.openapi.Register("GET", "/api/v1/admin/db/health", openapi.RouteMeta{
		Summary: "Database health and pg_stat summaries", Tags: []string{"admin", "db"},
	})
}

func (s *Server) handleAdminDBHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	empty := dbHealthResponse{
		Available:     false,
		Connections:   dbConnections{},
		SlowQueries:   []dbSlowQuery{},
		LargestTables: []dbLargestTable{},
		VacuumStatus:  []dbVacuumStatus{},
		Locks:         []dbLock{},
		CheckedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
	if s.db == nil || s.db.Pool == nil {
		empty.Reason = "database is not configured"
		writeJSON(w, http.StatusOK, empty)
		return
	}

	out := empty
	out.Available = true
	out.Connections = s.connectionStats(ctx)
	out.CacheHitRatio = s.cacheHitRatio(ctx)
	out.LargestTables = s.largestTables(ctx)
	out.VacuumStatus = s.vacuumStatus(ctx)
	out.Locks = s.lockStatus(ctx)

	ok, reason := s.pgStatStatementsAvailable(ctx)
	if !ok {
		out.Available = false
		out.Reason = reason
		writeJSON(w, http.StatusOK, out)
		return
	}
	out.SlowQueries = s.slowQueries(ctx)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) pgStatStatementsAvailable(ctx context.Context) (bool, string) {
	var preload string
	_ = s.db.Pool.QueryRow(ctx, `show shared_preload_libraries`).Scan(&preload)
	if !strings.Contains(preload, "pg_stat_statements") {
		return false, "pg_stat_statements not loaded; set shared_preload_libraries=pg_stat_statements and restart Postgres"
	}
	if ok := s.pgReadAllStatsAvailable(ctx); !ok {
		return false, "pg_read_all_stats is not granted to the runtime DB role; grant it so pg_stat_* panels show complete cross-session data"
	}
	var installed bool
	if err := s.db.Pool.QueryRow(ctx, `
		select exists (select 1 from pg_extension where extname = 'pg_stat_statements')
	`).Scan(&installed); err != nil || !installed {
		return false, "pg_stat_statements extension is not installed"
	}
	var probe int
	if err := s.db.Pool.QueryRow(ctx, `select 1 from pg_stat_statements limit 1`).Scan(&probe); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return true, ""
		}
		return false, "pg_stat_statements is installed but not queryable: " + err.Error()
	}
	return true, ""
}

func (s *Server) pgReadAllStatsAvailable(ctx context.Context) bool {
	var ok bool
	err := s.db.Pool.QueryRow(ctx, `
		select coalesce(pg_has_role(current_user, 'pg_read_all_stats', 'member'), false)
	`).Scan(&ok)
	return err == nil && ok
}

func (s *Server) connectionStats(ctx context.Context) dbConnections {
	var out dbConnections
	_ = s.db.Pool.QueryRow(ctx, `
		select
		  count(*) filter (where state = 'active')::int,
		  count(*) filter (where state = 'idle')::int,
		  current_setting('max_connections')::int
		from pg_stat_activity
	`).Scan(&out.Active, &out.Idle, &out.Max)
	return out
}

func (s *Server) cacheHitRatio(ctx context.Context) float64 {
	var ratio float64
	_ = s.db.Pool.QueryRow(ctx, `
		select case when sum(heap_blks_hit + heap_blks_read) = 0 then 0
		            else sum(heap_blks_hit)::float8 / sum(heap_blks_hit + heap_blks_read)::float8
		       end
		  from pg_statio_user_tables
	`).Scan(&ratio)
	return ratio
}

func (s *Server) slowQueries(ctx context.Context) []dbSlowQuery {
	rows, err := s.db.Pool.Query(ctx, `
		select left(query, 500), calls::bigint, mean_exec_time::float8, total_exec_time::float8
		  from pg_stat_statements
		 where query not ilike '%pg_stat_statements%'
		 order by mean_exec_time desc
		 limit 10
	`)
	if err != nil {
		return []dbSlowQuery{}
	}
	defer rows.Close()
	out := []dbSlowQuery{}
	for rows.Next() {
		var q dbSlowQuery
		if err := rows.Scan(&q.Query, &q.Calls, &q.MeanMS, &q.TotalMS); err == nil {
			out = append(out, q)
		}
	}
	return out
}

func (s *Server) largestTables(ctx context.Context) []dbLargestTable {
	rows, err := s.db.Pool.Query(ctx, `
		select schemaname, relname,
		       pg_total_relation_size(format('%I.%I', schemaname, relname))::bigint,
		       n_live_tup::bigint
		  from pg_stat_user_tables
		 order by pg_total_relation_size(format('%I.%I', schemaname, relname)) desc
		 limit 10
	`)
	if err != nil {
		return []dbLargestTable{}
	}
	defer rows.Close()
	out := []dbLargestTable{}
	for rows.Next() {
		var t dbLargestTable
		if err := rows.Scan(&t.Schema, &t.Table, &t.SizeBytes, &t.RowCount); err == nil {
			out = append(out, t)
		}
	}
	return out
}

func (s *Server) vacuumStatus(ctx context.Context) []dbVacuumStatus {
	rows, err := s.db.Pool.Query(ctx, `
		select relname,
		       greatest(last_vacuum, last_autovacuum),
		       n_dead_tup::bigint
		  from pg_stat_user_tables
		 order by n_dead_tup desc
		 limit 10
	`)
	if err != nil {
		return []dbVacuumStatus{}
	}
	defer rows.Close()
	out := []dbVacuumStatus{}
	for rows.Next() {
		var (
			table string
			last  *time.Time
			dead  int64
		)
		if err := rows.Scan(&table, &last, &dead); err != nil {
			continue
		}
		var lastPtr *string
		if last != nil {
			s := last.UTC().Format(time.RFC3339Nano)
			lastPtr = &s
		}
		out = append(out, dbVacuumStatus{Table: table, LastVacuum: lastPtr, DeadTuples: dead})
	}
	return out
}

func (s *Server) lockStatus(ctx context.Context) []dbLock {
	rows, err := s.db.Pool.Query(ctx, `
		select l.pid, l.mode, l.granted, coalesce(c.relname, ''),
		       extract(epoch from (now() - a.query_start)) * 1000
		  from pg_locks l
		  left join pg_class c on c.oid = l.relation
		  left join pg_stat_activity a on a.pid = l.pid
		 where not l.granted
		 order by a.query_start asc nulls last
		 limit 20
	`)
	if err != nil {
		return []dbLock{}
	}
	defer rows.Close()
	out := []dbLock{}
	for rows.Next() {
		var l dbLock
		var age float64
		if err := rows.Scan(&l.PID, &l.Mode, &l.Granted, &l.Relation, &age); err == nil {
			l.AgeMS = int64(age)
			out = append(out, l)
		}
	}
	return out
}
