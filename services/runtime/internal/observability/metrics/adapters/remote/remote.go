// SPDX-License-Identifier: Apache-2.0

// Package remote implements metrics.Store through the metrics-v1 HTTP sidecar
// protocol.
package remote

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	adapterremote "github.com/Agent-Field/backai/services/runtime/internal/adapters/remote"
	"github.com/Agent-Field/backai/services/runtime/internal/observability/metrics"
)

type Config struct {
	BaseURL string
	Token   string
	Timeout time.Duration
}

type Store struct {
	client *adapterremote.Client
	caps   metrics.Capabilities
	name   string
}

var _ metrics.Store = (*Store)(nil)

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
	var caps metrics.Capabilities
	if len(env.Capabilities) > 0 {
		if err := json.Unmarshal(env.Capabilities, &caps); err != nil {
			return nil, err
		}
	}
	return &Store{client: client, caps: caps, name: env.Name}, nil
}

func (s *Store) Query(ctx context.Context, promQL string, at time.Time) ([]metrics.InstantSample, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("remote metrics: nil store")
	}
	q := url.Values{"promql": []string{promQL}}
	if !at.IsZero() {
		q.Set("at", at.UTC().Format(time.RFC3339Nano))
	}
	resp, err := s.client.Do(ctx, adapterremote.Request{
		Method: http.MethodGet,
		Path:   "/v1/metrics/query",
		Query:  q,
	})
	if err != nil {
		return nil, err
	}
	var out instantResponse
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, err
	}
	return out.Samples, nil
}

func (s *Store) QueryRange(ctx context.Context, promQL string, from, to time.Time, step time.Duration) ([]metrics.RangeSample, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("remote metrics: nil store")
	}
	q := url.Values{"promql": []string{promQL}}
	if !from.IsZero() {
		q.Set("from", from.UTC().Format(time.RFC3339Nano))
	}
	if !to.IsZero() {
		q.Set("to", to.UTC().Format(time.RFC3339Nano))
	}
	if step > 0 {
		q.Set("step", step.String())
	}
	resp, err := s.client.Do(ctx, adapterremote.Request{
		Method: http.MethodGet,
		Path:   "/v1/metrics/range",
		Query:  q,
	})
	if err != nil {
		return nil, err
	}
	var out rangeResponse
	if err := resp.DecodeJSON(&out); err != nil {
		return nil, err
	}
	return out.Series, nil
}

func (s *Store) Capabilities() metrics.Capabilities {
	if s == nil {
		return metrics.Capabilities{}
	}
	return s.caps
}

func (s *Store) AdapterName() string {
	if s == nil || s.name == "" {
		return "remote"
	}
	return s.name
}

type instantResponse struct {
	Samples []metrics.InstantSample `json:"samples"`
}

type rangeResponse struct {
	Series []metrics.RangeSample `json:"series"`
}
