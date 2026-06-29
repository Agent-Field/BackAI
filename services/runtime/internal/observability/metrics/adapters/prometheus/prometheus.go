// SPDX-License-Identifier: Apache-2.0

// Package prometheus implements metrics.Store against Prometheus' HTTP API.
package prometheus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/observability/metrics"
)

const (
	defaultTimeout       = 10 * time.Second
	defaultMaxSeries     = 5000
	readinessPath        = "/-/ready"
	instantQueryPath     = "/api/v1/query"
	rangeQueryPath       = "/api/v1/query_range"
	statusConfigPath     = "/api/v1/status/config"
	cadvisorCPUQuery     = `container_cpu_usage_seconds_total{}`
	cadvisorMemoryQuery  = `container_memory_usage_bytes{}`
	cadvisorStartedQuery = `container_start_time_seconds{}`
)

type Config struct {
	BaseURL    string
	HTTPClient *http.Client
	Timeout    time.Duration
}

type Store struct {
	baseURL string
	hc      *http.Client
	timeout time.Duration
	caps    metrics.Capabilities
}

var _ metrics.Store = (*Store)(nil)

func New(ctx context.Context, cfg Config) (*Store, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, errors.New("prometheus: base URL required")
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("prometheus: invalid base URL %q", cfg.BaseURL)
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: defaultTimeout}
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	s := &Store{
		baseURL: base,
		hc:      hc,
		timeout: timeout,
		caps: metrics.Capabilities{
			SupportsInstantQuery: true,
			SupportsRangeQuery:   true,
			NativeQueryLang:      "promql",
			MaxSeriesPerQuery:    defaultMaxSeries,
		},
	}
	readyCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	if err := s.ready(readyCtx); err != nil {
		return nil, err
	}
	s.probe(ctx)
	return s, nil
}

func (s *Store) Query(ctx context.Context, promQL string, at time.Time) ([]metrics.InstantSample, error) {
	promQL = strings.TrimSpace(promQL)
	if promQL == "" {
		return nil, errors.New("prometheus: promql is required")
	}
	q := url.Values{"query": []string{promQL}}
	if !at.IsZero() {
		q.Set("time", at.UTC().Format(time.RFC3339Nano))
	}
	var env responseEnvelope
	if err := s.getJSON(ctx, instantQueryPath, q, &env); err != nil {
		return nil, err
	}
	if env.Status != "success" {
		return nil, env.errorOrDefault()
	}
	if env.Data.ResultType != "vector" {
		return nil, fmt.Errorf("%w: instant query returned %s", metrics.ErrUnsupportedShape, env.Data.ResultType)
	}
	out := make([]metrics.InstantSample, 0, len(env.Data.Result))
	for _, item := range env.Data.Result {
		value, ts, err := decodeValue(item.Value)
		if err != nil {
			return nil, err
		}
		out = append(out, metrics.InstantSample{Metric: item.Metric, Value: value, TS: ts})
	}
	return out, nil
}

func (s *Store) QueryRange(ctx context.Context, promQL string, from, to time.Time, step time.Duration) ([]metrics.RangeSample, error) {
	promQL = strings.TrimSpace(promQL)
	if promQL == "" {
		return nil, errors.New("prometheus: promql is required")
	}
	if from.IsZero() || to.IsZero() {
		return nil, errors.New("prometheus: from and to are required")
	}
	if !to.After(from) {
		return nil, errors.New("prometheus: to must be after from")
	}
	if step <= 0 {
		return nil, errors.New("prometheus: step must be positive")
	}
	q := url.Values{
		"query": []string{promQL},
		"start": []string{from.UTC().Format(time.RFC3339Nano)},
		"end":   []string{to.UTC().Format(time.RFC3339Nano)},
		"step":  []string{formatStep(step)},
	}
	var env responseEnvelope
	if err := s.getJSON(ctx, rangeQueryPath, q, &env); err != nil {
		return nil, err
	}
	if env.Status != "success" {
		return nil, env.errorOrDefault()
	}
	if env.Data.ResultType != "matrix" {
		return nil, fmt.Errorf("%w: range query returned %s", metrics.ErrUnsupportedShape, env.Data.ResultType)
	}
	out := make([]metrics.RangeSample, 0, len(env.Data.Result))
	for _, item := range env.Data.Result {
		values := make([]metrics.TimedValue, 0, len(item.Values))
		for _, raw := range item.Values {
			value, ts, err := decodeValue(raw)
			if err != nil {
				return nil, err
			}
			values = append(values, metrics.TimedValue{TS: ts, Value: value})
		}
		out = append(out, metrics.RangeSample{Metric: item.Metric, Values: values})
	}
	return out, nil
}

func (s *Store) Capabilities() metrics.Capabilities {
	if s == nil {
		return metrics.Capabilities{}
	}
	return s.caps
}

func (s *Store) probe(ctx context.Context) {
	probeCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	s.caps.RetentionHours = s.retentionHours(probeCtx)
	s.caps.SupportsContainer = s.hasCadvisor(probeCtx)
}

func (s *Store) ready(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+readinessPath, nil)
	if err != nil {
		return err
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("prometheus: ready returned %d", resp.StatusCode)
	}
	return nil
}

func (s *Store) retentionHours(ctx context.Context) int {
	var out struct {
		Status string `json:"status"`
		Data   struct {
			YAML string `json:"yaml"`
		} `json:"data"`
	}
	if err := s.getJSON(ctx, statusConfigPath, nil, &out); err != nil || out.Status != "success" {
		return 0
	}
	raw := strings.ToLower(out.Data.YAML)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "retention.time") && !strings.Contains(line, "storage.tsdb.retention.time") {
			continue
		}
		_, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if hours := parseRetentionHours(value); hours > 0 {
			return hours
		}
	}
	return 0
}

func (s *Store) hasCadvisor(ctx context.Context) bool {
	now := time.Now().UTC()
	for _, query := range []string{cadvisorCPUQuery, cadvisorMemoryQuery, cadvisorStartedQuery} {
		samples, err := s.Query(ctx, query, now)
		if err == nil && len(samples) > 0 {
			return true
		}
	}
	return false
}

func (s *Store) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	if s == nil || s.hc == nil {
		return errors.New("prometheus: nil store")
	}
	reqCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	u := s.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("prometheus: %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return err
	}
	return nil
}

type responseEnvelope struct {
	Status    string       `json:"status"`
	ErrorType string       `json:"errorType,omitempty"`
	Error     string       `json:"error,omitempty"`
	Data      responseData `json:"data"`
}

func (e responseEnvelope) errorOrDefault() error {
	msg := strings.TrimSpace(e.Error)
	if msg == "" {
		msg = "prometheus query failed"
	}
	if e.ErrorType != "" {
		msg = e.ErrorType + ": " + msg
	}
	return errors.New(msg)
}

type responseData struct {
	ResultType string         `json:"resultType"`
	Result     []responseItem `json:"result"`
}

type responseItem struct {
	Metric map[string]string `json:"metric"`
	Value  rawValue          `json:"value"`
	Values []rawValue        `json:"values"`
}

type rawValue []json.RawMessage

func decodeValue(raw rawValue) (float64, time.Time, error) {
	if len(raw) != 2 {
		return 0, time.Time{}, errors.New("prometheus: sample value must have timestamp and value")
	}
	var tsFloat float64
	if err := json.Unmarshal(raw[0], &tsFloat); err != nil {
		return 0, time.Time{}, err
	}
	var valueText string
	if err := json.Unmarshal(raw[1], &valueText); err != nil {
		return 0, time.Time{}, err
	}
	value, err := strconv.ParseFloat(valueText, 64)
	if err != nil {
		return 0, time.Time{}, err
	}
	sec, frac := math.Modf(tsFloat)
	ts := time.Unix(int64(sec), int64(frac*1e9)).UTC()
	return value, ts, nil
}

func formatStep(step time.Duration) string {
	if step%time.Second == 0 {
		return strconv.FormatInt(int64(step/time.Second), 10) + "s"
	}
	return strconv.FormatFloat(step.Seconds(), 'f', -1, 64)
}

func parseRetentionHours(raw string) int {
	raw = strings.Trim(strings.TrimSpace(raw), `"`)
	if raw == "" {
		return 0
	}
	if strings.HasSuffix(raw, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(raw, "d"))
		if err == nil && days > 0 {
			return days * 24
		}
	}
	if d, err := time.ParseDuration(raw); err == nil {
		return int(d.Hours())
	}
	return 0
}
