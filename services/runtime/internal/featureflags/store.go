// SPDX-License-Identifier: Apache-2.0

package featureflags

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

const tracerName = "af-stack/featureflags"

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,127}$`)

var builtinFlags = []Flag{
	{
		Key:         "experimental-cost-forecasts",
		Label:       "Experimental cost forecasts",
		Description: "Show 30-day spend forecasts on the Cost page based on rolling averages.",
		Enabled:     false,
		Source:      "default",
		Metadata:    map[string]any{},
	},
	{
		Key:         "command-palette-recents",
		Label:       "Command palette recents",
		Description: "Remember the last few destinations and show them at the top of command search.",
		Enabled:     true,
		Source:      "default",
		Metadata:    map[string]any{},
	},
	{
		Key:         "verbose-run-logs",
		Label:       "Verbose run logs",
		Description: "Include tool input/output payloads in the inline run log viewer.",
		Enabled:     false,
		Source:      "default",
		Metadata:    map[string]any{},
	},
}

type Store struct {
	pool   *pgxpool.Pool
	log    *slog.Logger
	tracer trace.Tracer
}

func New(pool *pgxpool.Pool, log *slog.Logger) (*Store, error) {
	if pool == nil {
		return nil, errors.New("featureflags: pool required")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Store{pool: pool, log: log, tracer: otel.Tracer(tracerName)}, nil
}

func Defaults() []Flag {
	out := make([]Flag, len(builtinFlags))
	for i, f := range builtinFlags {
		out[i] = cloneFlag(f)
	}
	return out
}

func (s *Store) List(ctx context.Context) (List, error) {
	ctx, span := s.tracer.Start(ctx, "featureflags.list")
	defer span.End()

	tenantID := tenantctx.TenantID(ctx)
	if tenantID == "" {
		return List{Flags: []Flag{}}, ErrTenantRequired
	}

	byKey := map[string]Flag{}
	for _, f := range Defaults() {
		byKey[f.Key] = f
	}

	rows, err := s.pool.Query(ctx, `
		select key, label, description, enabled, metadata, updated_at
		from suite_feature_flags
		where tenant_id = $1
		order by key
	`, tenantID)
	if err != nil {
		span.RecordError(err)
		return List{Flags: []Flag{}}, fmt.Errorf("featureflags: list: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		flag, scanErr := scanFlag(rows)
		if scanErr != nil {
			span.RecordError(scanErr)
			return List{Flags: []Flag{}}, fmt.Errorf("featureflags: scan: %w", scanErr)
		}
		byKey[flag.Key] = flag
	}
	if err := rows.Err(); err != nil {
		return List{Flags: []Flag{}}, fmt.Errorf("featureflags: iter: %w", err)
	}

	flags := make([]Flag, 0, len(byKey))
	for _, f := range byKey {
		flags = append(flags, f)
	}
	sort.SliceStable(flags, func(i, j int) bool {
		return flags[i].Key < flags[j].Key
	})
	return List{Flags: flags}, nil
}

func (s *Store) Set(ctx context.Context, key string, in SetInput) (Flag, error) {
	ctx, span := s.tracer.Start(ctx, "featureflags.set", trace.WithAttributes(
		attribute.String("featureflag.key", key),
		attribute.Bool("featureflag.enabled", in.Enabled),
	))
	defer span.End()

	tenantID := tenantctx.TenantID(ctx)
	if tenantID == "" {
		return Flag{}, ErrTenantRequired
	}
	key = strings.TrimSpace(key)
	if !keyPattern.MatchString(key) {
		return Flag{}, fmt.Errorf("%w: key must match %s", ErrValidation, keyPattern.String())
	}

	base := Flag{Key: key, Source: "db", Metadata: map[string]any{}}
	if found, ok := builtinByKey(key); ok {
		base = found
		base.Source = "db"
	}
	if strings.TrimSpace(in.Label) != "" {
		base.Label = strings.TrimSpace(in.Label)
	}
	if strings.TrimSpace(in.Description) != "" {
		base.Description = strings.TrimSpace(in.Description)
	}
	base.Enabled = in.Enabled
	if in.Metadata != nil {
		base.Metadata = in.Metadata
	}
	metadata, err := encodeMetadata(base.Metadata)
	if err != nil {
		return Flag{}, fmt.Errorf("%w: metadata must be JSON-serializable", ErrValidation)
	}

	userID := tenantctx.UserID(ctx)
	row := s.pool.QueryRow(ctx, `
		insert into suite_feature_flags
			(tenant_id, key, label, description, enabled, metadata, updated_by, created_at, updated_at)
		values
			($1, $2, $3, $4, $5, $6, nullif($7, '')::uuid, now(), now())
		on conflict (tenant_id, key) do update set
			label = excluded.label,
			description = excluded.description,
			enabled = excluded.enabled,
			metadata = excluded.metadata,
			updated_by = excluded.updated_by,
			updated_at = now()
		returning key, label, description, enabled, metadata, updated_at
	`, tenantID, key, base.Label, base.Description, base.Enabled, metadata, userID)

	flag, err := scanFlag(row)
	if err != nil {
		span.RecordError(err)
		return Flag{}, fmt.Errorf("featureflags: set: %w", err)
	}
	return flag, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanFlag(r rowScanner) (Flag, error) {
	var (
		f         Flag
		metaRaw   []byte
		updatedAt time.Time
	)
	if err := r.Scan(&f.Key, &f.Label, &f.Description, &f.Enabled, &metaRaw, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Flag{}, err
		}
		return Flag{}, err
	}
	f.Source = "db"
	f.Metadata = map[string]any{}
	if len(metaRaw) > 0 {
		_ = json.Unmarshal(metaRaw, &f.Metadata)
	}
	if f.Metadata == nil {
		f.Metadata = map[string]any{}
	}
	ts := updatedAt.UTC().Format(time.RFC3339Nano)
	f.UpdatedAt = &ts
	return f, nil
}

func builtinByKey(key string) (Flag, bool) {
	for _, f := range builtinFlags {
		if f.Key == key {
			return cloneFlag(f), true
		}
	}
	return Flag{}, false
}

func cloneFlag(f Flag) Flag {
	out := f
	out.Metadata = map[string]any{}
	for k, v := range f.Metadata {
		out.Metadata[k] = v
	}
	return out
}

func encodeMetadata(m map[string]any) ([]byte, error) {
	if m == nil {
		return []byte(`{}`), nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	if string(b) == "null" {
		return []byte(`{}`), nil
	}
	return b, nil
}
