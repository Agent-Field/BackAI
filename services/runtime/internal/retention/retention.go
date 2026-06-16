// SPDX-License-Identifier: Apache-2.0

package retention

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"time"
)

type Policy struct {
	Table       string
	RetainDays  int
	OrderColumn string
	BatchSize   int
	Rollover    *RolloverSpec
}

type RolloverSpec struct {
	Table          string
	BucketDuration time.Duration
	AggregateSQL   string
}

type Registry struct {
	policies []Policy
}

type Report struct {
	Table        string        `json:"table"`
	RowsDeleted  int64         `json:"rows_deleted"`
	RowsRolledUp int64         `json:"rows_rolled_up"`
	Duration     time.Duration `json:"duration"`
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Register(p Policy) {
	if r == nil {
		return
	}
	r.policies = append(r.policies, p)
}

func (r *Registry) Policies() []Policy {
	if r == nil {
		return nil
	}
	out := make([]Policy, len(r.policies))
	copy(out, r.policies)
	return out
}

func (r *Registry) Run(ctx context.Context, db *sql.DB) (Report, error) {
	if r == nil || db == nil {
		return Report{}, nil
	}
	start := time.Now()
	total := Report{}
	for _, p := range r.policies {
		report, err := runPolicy(ctx, db, p)
		if err != nil {
			return total, err
		}
		total.Table = report.Table
		total.RowsDeleted += report.RowsDeleted
		total.RowsRolledUp += report.RowsRolledUp
	}
	total.Duration = time.Since(start)
	return total, nil
}

func runPolicy(ctx context.Context, db *sql.DB, p Policy) (Report, error) {
	if err := validatePolicy(p); err != nil {
		return Report{}, err
	}
	batch := p.BatchSize
	if batch <= 0 {
		batch = 1000
	}
	report := Report{Table: p.Table}
	cutoff := time.Now().UTC().AddDate(0, 0, -p.RetainDays)
	for {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		q := fmt.Sprintf(`
			delete from %s
			 where ctid in (
			   select ctid from %s
			    where %s < $1
			    order by %s
			    limit %d
			 )
		`, p.Table, p.Table, p.OrderColumn, p.OrderColumn, batch)
		res, err := db.ExecContext(ctx, q, cutoff)
		if err != nil {
			return report, err
		}
		n, _ := res.RowsAffected()
		report.RowsDeleted += n
		if n == 0 || n < int64(batch) {
			return report, nil
		}
	}
}

var identRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validatePolicy(p Policy) error {
	if !identRE.MatchString(p.Table) {
		return fmt.Errorf("retention: invalid table %q", p.Table)
	}
	if !identRE.MatchString(p.OrderColumn) {
		return fmt.Errorf("retention: invalid order column %q", p.OrderColumn)
	}
	if p.RetainDays <= 0 {
		return fmt.Errorf("retention: retain days must be positive")
	}
	if p.BatchSize < 0 {
		return fmt.Errorf("retention: batch size must be non-negative")
	}
	return nil
}
