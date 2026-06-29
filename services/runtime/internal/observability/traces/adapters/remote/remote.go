// SPDX-License-Identifier: Apache-2.0

// Package remote implements traces.Store through the traces-v1 HTTP sidecar
// protocol.
package remote

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	adapterremote "github.com/Agent-Field/backai/services/runtime/internal/adapters/remote"
	"github.com/Agent-Field/backai/services/runtime/internal/observability/traces"
)

type Config struct {
	BaseURL string
	Token   string
	Timeout time.Duration
}

type Store struct {
	client *adapterremote.Client
	caps   traces.Capabilities
	name   string
}

var _ traces.Store = (*Store)(nil)

func New(ctx context.Context, cfg Config) (*Store, error) {
	client, err := adapterremote.NewClient(adapterremote.Config{
		BaseURL: cfg.BaseURL,
		Token:   cfg.Token,
		Timeout: cfg.Timeout,
	})
	if err != nil {
		return nil, err
	}
	env, err := client.Capabilities(ctx)
	if err != nil {
		return nil, err
	}
	var caps traces.Capabilities
	if len(env.Capabilities) > 0 {
		if err := json.Unmarshal(env.Capabilities, &caps); err != nil {
			return nil, err
		}
	}
	return &Store{client: client, caps: caps, name: env.Name}, nil
}

func (s *Store) Search(ctx context.Context, f traces.SearchFilter) (traces.SearchResult, error) {
	if s == nil || s.client == nil {
		return traces.SearchResult{}, errors.New("remote traces: nil store")
	}
	resp, err := s.client.Do(ctx, adapterremote.Request{
		Method: http.MethodPost,
		Path:   "/v1/traces/search",
		Body:   filterWireFromFilter(f),
	})
	if err != nil {
		return traces.SearchResult{}, err
	}
	var out searchResultWire
	if err := resp.DecodeJSON(&out); err != nil {
		return traces.SearchResult{}, err
	}
	return traces.SearchResult{Traces: out.Traces, HasMore: out.HasMore}, nil
}

func (s *Store) Get(ctx context.Context, traceID string) (traces.Trace, error) {
	if s == nil || s.client == nil {
		return traces.Trace{}, errors.New("remote traces: nil store")
	}
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return traces.Trace{}, traces.ErrInvalidTraceID
	}
	resp, err := s.client.Do(ctx, adapterremote.Request{
		Method: http.MethodGet,
		Path:   "/v1/traces/" + url.PathEscape(traceID),
	})
	if err != nil {
		if p, ok := adapterremote.AsProblem(err); ok && (p.HTTPStatus == http.StatusNotFound || p.Code == "trace_not_found") {
			return traces.Trace{}, traces.ErrTraceNotFound
		}
		return traces.Trace{}, err
	}
	var out traces.Trace
	if err := resp.DecodeJSON(&out); err != nil {
		return traces.Trace{}, err
	}
	return out, nil
}

func (s *Store) Capabilities() traces.Capabilities {
	if s == nil {
		return traces.Capabilities{}
	}
	return s.caps
}

func (s *Store) AdapterName() string {
	if s == nil || s.name == "" {
		return "remote"
	}
	return s.name
}

func filterWireFromFilter(f traces.SearchFilter) filterWire {
	return filterWire{
		Service:     f.Service,
		Operation:   f.Operation,
		Tag:         f.Tag,
		MinDuration: durationString(f.MinDuration),
		MaxDuration: durationString(f.MaxDuration),
		Status:      f.Status,
		From:        f.From,
		To:          f.To,
		Limit:       f.Limit,
	}
}

func durationString(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return d.String()
}

type filterWire struct {
	Service     string            `json:"service,omitempty"`
	Operation   string            `json:"operation,omitempty"`
	Tag         map[string]string `json:"tag,omitempty"`
	MinDuration string            `json:"min_duration,omitempty"`
	MaxDuration string            `json:"max_duration,omitempty"`
	Status      string            `json:"status,omitempty"`
	From        time.Time         `json:"from,omitempty"`
	To          time.Time         `json:"to,omitempty"`
	Limit       int               `json:"limit,omitempty"`
}

func (f *filterWire) UnmarshalJSON(data []byte) error {
	type alias filterWire
	var raw struct {
		alias
		MinDurationNumber int64 `json:"min_duration_ns,omitempty"`
		MaxDurationNumber int64 `json:"max_duration_ns,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*f = filterWire(raw.alias)
	if raw.MinDurationNumber > 0 {
		f.MinDuration = strconv.FormatInt(raw.MinDurationNumber, 10) + "ns"
	}
	if raw.MaxDurationNumber > 0 {
		f.MaxDuration = strconv.FormatInt(raw.MaxDurationNumber, 10) + "ns"
	}
	return nil
}

type searchResultWire struct {
	Traces  []traces.TraceSummary `json:"traces"`
	HasMore bool                  `json:"has_more,omitempty"`
}
