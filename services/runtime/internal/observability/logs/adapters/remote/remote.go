// SPDX-License-Identifier: Apache-2.0

// Package remote implements logs.Store through the logs-v1 HTTP sidecar
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
	"github.com/Agent-Field/backai/services/runtime/internal/observability/logs"
)

type Config struct {
	BaseURL string
	Token   string
	Timeout time.Duration
}

type Store struct {
	client *adapterremote.Client
	caps   logs.Capabilities
	name   string
}

var _ logs.Store = (*Store)(nil)

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
	var caps logs.Capabilities
	if len(env.Capabilities) > 0 {
		if err := json.Unmarshal(env.Capabilities, &caps); err != nil {
			return nil, err
		}
	}
	return &Store{client: client, caps: caps, name: env.Name}, nil
}

func (s *Store) Query(ctx context.Context, f logs.Filter) (logs.Page, error) {
	if s == nil || s.client == nil {
		return logs.Page{}, errors.New("remote logs: nil store")
	}
	resp, err := s.client.Do(ctx, adapterremote.Request{
		Method: http.MethodPost,
		Path:   "/v1/logs/query",
		Body:   filterWireFromFilter(f),
	})
	if err != nil {
		return logs.Page{}, err
	}
	var out pageWire
	if err := resp.DecodeJSON(&out); err != nil {
		return logs.Page{}, err
	}
	return pageFromWire(out), nil
}

func (s *Store) Tail(ctx context.Context, f logs.Filter) (<-chan logs.Entry, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("remote logs: nil store")
	}
	if !s.caps.SupportsTail {
		return nil, logs.ErrUnsupportedCapability
	}
	resp, err := s.client.Do(ctx, adapterremote.Request{
		Method: http.MethodGet,
		Path:   "/v1/logs/tail",
		Query:  queryFromFilter(f),
		Stream: true,
	})
	if err != nil {
		return nil, err
	}
	events := resp.Events(ctx)
	out := make(chan logs.Entry)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				raw := strings.TrimSpace(string(ev.Data))
				if raw == "[DONE]" {
					return
				}
				var entry logs.Entry
				if err := ev.DecodeData(&entry); err != nil {
					continue
				}
				select {
				case <-ctx.Done():
					return
				case out <- entry:
				}
			}
		}
	}()
	return out, nil
}

func (s *Store) Capabilities() logs.Capabilities {
	if s == nil {
		return logs.Capabilities{}
	}
	return s.caps
}

func (s *Store) AdapterName() string {
	if s == nil || s.name == "" {
		return "remote"
	}
	return s.name
}

func filterWireFromFilter(f logs.Filter) filterWire {
	return filterWire{
		Services:      f.Services,
		Levels:        f.Levels,
		TenantID:      f.TenantID,
		RequestID:     f.RequestID,
		TraceID:       f.TraceID,
		Search:        f.Search,
		SearchIsRegex: f.SearchIsRegex,
		From:          f.From,
		To:            f.To,
		Limit:         f.Limit,
		Cursor:        f.Cursor,
	}
}

func queryFromFilter(f logs.Filter) url.Values {
	q := url.Values{}
	for _, service := range f.Services {
		q.Add("service", service)
	}
	for _, level := range f.Levels {
		q.Add("level", level)
	}
	if f.TenantID != "" {
		q.Set("tenant", f.TenantID)
	}
	if f.RequestID != "" {
		q.Set("request_id", f.RequestID)
	}
	if f.TraceID != "" {
		q.Set("trace_id", f.TraceID)
	}
	if f.Search != "" {
		q.Set("search", f.Search)
	}
	if f.SearchIsRegex {
		q.Set("search_is_regex", "true")
	}
	if !f.From.IsZero() {
		q.Set("from", f.From.Format(time.RFC3339Nano))
	}
	if !f.To.IsZero() {
		q.Set("to", f.To.Format(time.RFC3339Nano))
	}
	if f.Limit > 0 {
		q.Set("limit", strconv.Itoa(f.Limit))
	}
	if f.Cursor != "" {
		q.Set("cursor", f.Cursor)
	}
	return q
}

func pageFromWire(p pageWire) logs.Page {
	return logs.Page{Entries: p.Entries, NextCursor: p.NextCursor, HasMore: p.HasMore}
}

type filterWire struct {
	Services      []string  `json:"services,omitempty"`
	Levels        []string  `json:"levels,omitempty"`
	TenantID      string    `json:"tenant_id,omitempty"`
	RequestID     string    `json:"request_id,omitempty"`
	TraceID       string    `json:"trace_id,omitempty"`
	Search        string    `json:"search,omitempty"`
	SearchIsRegex bool      `json:"search_is_regex,omitempty"`
	From          time.Time `json:"from,omitempty"`
	To            time.Time `json:"to,omitempty"`
	Limit         int       `json:"limit,omitempty"`
	Cursor        string    `json:"cursor,omitempty"`
}

type pageWire struct {
	Entries    []logs.Entry `json:"entries"`
	NextCursor string       `json:"next_cursor,omitempty"`
	HasMore    bool         `json:"has_more,omitempty"`
}
