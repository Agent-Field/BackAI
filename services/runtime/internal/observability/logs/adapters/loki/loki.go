// SPDX-License-Identifier: Apache-2.0

// Package loki implements logs.Store using Grafana Loki's HTTP API.
package loki

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"gopkg.in/yaml.v3"

	"github.com/Agent-Field/backai/services/runtime/internal/observability/logs"
)

const maxEntriesPerPage = 5000

type Config struct {
	BaseURL    string
	Tenant     string
	HTTPClient *http.Client
}

type Store struct {
	baseURL string
	tenant  string
	client  *http.Client
	dialer  *websocket.Dialer
	caps    logs.Capabilities
}

var _ logs.Store = (*Store)(nil)

func New(ctx context.Context, cfg Config) (*Store, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, errors.New("loki logs: url required")
	}
	if _, err := url.ParseRequestURI(base); err != nil {
		return nil, fmt.Errorf("loki logs: parse url: %w", err)
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	s := &Store{
		baseURL: base,
		tenant:  strings.TrimSpace(cfg.Tenant),
		client:  client,
		dialer:  websocket.DefaultDialer,
		caps: logs.Capabilities{
			SupportsTail:        true,
			SupportsFullText:    true,
			SupportsRegexSearch: true,
			SupportsTraceID:     true,
			NativeQueryLang:     "logql",
			RetentionDays:       0,
			MaxEntriesPerPage:   maxEntriesPerPage,
		},
	}
	s.caps.RetentionDays = s.probeRetention(ctx)
	return s, nil
}

func (s *Store) Query(ctx context.Context, f logs.Filter) (logs.Page, error) {
	if s == nil {
		return logs.Page{}, errors.New("loki logs: nil store")
	}
	limit := normalizeLimit(f.Limit)
	q := url.Values{}
	q.Set("query", filterToLogQL(f))
	q.Set("direction", "backward")
	q.Set("limit", strconv.Itoa(limit+1))
	if !f.From.IsZero() {
		q.Set("start", strconv.FormatInt(f.From.UTC().UnixNano(), 10))
	}
	if !f.To.IsZero() {
		q.Set("end", strconv.FormatInt(f.To.UTC().UnixNano(), 10))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/loki/api/v1/query_range?"+q.Encode(), nil)
	if err != nil {
		return logs.Page{}, err
	}
	s.applyTenant(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return logs.Page{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return logs.Page{}, fmt.Errorf("loki query_range HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var lr queryRangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return logs.Page{}, fmt.Errorf("decode loki query_range: %w", err)
	}
	entries := entriesFromStreams(lr.Data.Result)
	hasMore := len(entries) > limit
	if hasMore {
		entries = entries[:limit]
	}
	next := ""
	if hasMore && len(entries) > 0 {
		next = strconv.FormatInt(entries[len(entries)-1].TS.UnixNano(), 10)
	}
	return logs.Page{Entries: entries, HasMore: hasMore, NextCursor: next}, nil
}

func (s *Store) Tail(ctx context.Context, f logs.Filter) (<-chan logs.Entry, error) {
	if s == nil {
		return nil, errors.New("loki logs: nil store")
	}
	q := url.Values{}
	q.Set("query", filterToLogQL(f))
	q.Set("delay_for", "0")
	q.Set("limit", strconv.Itoa(normalizeLimit(f.Limit)))
	if !f.From.IsZero() {
		q.Set("start", strconv.FormatInt(f.From.UTC().UnixNano(), 10))
	}
	u, err := url.Parse(s.baseURL)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/loki/api/v1/tail"
	u.RawQuery = q.Encode()
	headers := http.Header{}
	if s.tenant != "" {
		headers.Set("X-Scope-OrgID", s.tenant)
	}
	conn, resp, err := s.dialer.DialContext(ctx, u.String(), headers)
	if err != nil {
		if resp != nil && resp.Body != nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
			_ = resp.Body.Close()
			return nil, fmt.Errorf("loki tail HTTP %d: %s: %w", resp.StatusCode, strings.TrimSpace(string(body)), err)
		}
		return nil, err
	}
	out := make(chan logs.Entry)
	go func() {
		defer close(out)
		defer conn.Close()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			_, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var frame tailFrame
			if err := json.Unmarshal(payload, &frame); err != nil {
				continue
			}
			for _, entry := range entriesFromStreams(frame.Streams) {
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

func (s *Store) Probe(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/ready", nil)
	if err != nil {
		return err
	}
	s.applyTenant(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("loki ready HTTP %d", resp.StatusCode)
	}
	return nil
}

func (s *Store) probeRetention(ctx context.Context) int {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/config", nil)
	if err != nil {
		return 0
	}
	s.applyTenant(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return 0
	}
	return retentionDaysFromConfig(body)
}

func (s *Store) applyTenant(req *http.Request) {
	if s != nil && s.tenant != "" {
		req.Header.Set("X-Scope-OrgID", s.tenant)
	}
}

func filterToLogQL(f logs.Filter) string {
	labels := []string{}
	if len(f.Services) > 0 {
		quoted := make([]string, 0, len(f.Services))
		for _, svc := range f.Services {
			if svc = strings.TrimSpace(svc); svc != "" {
				quoted = append(quoted, regexp.QuoteMeta(svc))
			}
		}
		if len(quoted) > 0 {
			labels = append(labels, `service=~"`+strings.Join(quoted, "|")+`"`)
		}
	}
	if f.TenantID != "" {
		labels = append(labels, `tenant_id="`+escapeLabelValue(f.TenantID)+`"`)
	}
	if f.TraceID != "" {
		labels = append(labels, `trace_id="`+escapeLabelValue(f.TraceID)+`"`)
	}
	if len(labels) == 0 {
		labels = append(labels, `service=~".+"`)
	}
	expr := "{" + strings.Join(labels, ",") + "}"
	if f.Search != "" {
		if f.SearchIsRegex {
			expr += ` |~ ` + strconv.Quote(f.Search)
		} else {
			expr += ` |= ` + strconv.Quote(f.Search)
		}
	}
	needsLogfmt := len(f.Levels) > 0 || f.RequestID != ""
	if needsLogfmt {
		expr += " | logfmt"
	}
	if len(f.Levels) > 0 {
		levels := make([]string, 0, len(f.Levels))
		for _, lvl := range f.Levels {
			if lvl = strings.TrimSpace(lvl); lvl != "" {
				levels = append(levels, regexp.QuoteMeta(lvl))
			}
		}
		if len(levels) > 0 {
			expr += ` | level=~"` + strings.Join(levels, "|") + `"`
		}
	}
	if f.RequestID != "" {
		expr += ` | request_id="` + escapeLabelValue(f.RequestID) + `"`
	}
	return expr
}

func entriesFromStreams(streams []streamResult) []logs.Entry {
	out := make([]logs.Entry, 0)
	for _, stream := range streams {
		for _, value := range stream.Values {
			if len(value) != 2 {
				continue
			}
			ts := parseLokiTS(value[0])
			entry := entryFromLine(ts, value[1], stream.Stream)
			out = append(out, entry)
		}
	}
	return out
}

func entryFromLine(ts time.Time, line string, labels map[string]string) logs.Entry {
	entry := logs.Entry{
		TS:      ts.UTC(),
		Level:   coerceLevel(labels["level"]),
		Service: firstNonEmpty(labels["service"], labels["app"], labels["job"], "loki"),
		Msg:     line,
		Fields:  map[string]any{},
	}
	for k, v := range labels {
		entry.Fields[k] = v
	}
	var obj map[string]any
	if json.Unmarshal([]byte(line), &obj) == nil {
		applyFields(&entry, obj)
	} else {
		applyLogfmt(&entry, parseLogfmt(line))
	}
	if len(entry.Fields) == 0 {
		entry.Fields = nil
	}
	if entry.Level == "" {
		entry.Level = "info"
	}
	return entry
}

func applyFields(entry *logs.Entry, fields map[string]any) {
	for k, v := range fields {
		entry.Fields[k] = v
		switch k {
		case "ts", "time":
			if s, ok := v.(string); ok {
				if ts, err := time.Parse(time.RFC3339Nano, s); err == nil {
					entry.TS = ts.UTC()
				}
			}
		case "level":
			entry.Level = coerceLevel(fmt.Sprint(v))
		case "service":
			entry.Service = fmt.Sprint(v)
		case "msg", "message":
			entry.Msg = fmt.Sprint(v)
		case "agent":
			entry.Agent = fmt.Sprint(v)
		case "tenant_id", "tenantID":
			entry.TenantID = fmt.Sprint(v)
		case "request_id", "requestID":
			entry.RequestID = fmt.Sprint(v)
		case "trace_id", "traceID":
			entry.TraceID = fmt.Sprint(v)
		}
	}
}

func applyLogfmt(entry *logs.Entry, fields map[string]string) {
	for k, v := range fields {
		entry.Fields[k] = v
		switch k {
		case "level":
			entry.Level = coerceLevel(v)
		case "service":
			entry.Service = v
		case "msg", "message":
			entry.Msg = v
		case "agent":
			entry.Agent = v
		case "tenant_id", "tenantID":
			entry.TenantID = v
		case "request_id", "requestID":
			entry.RequestID = v
		case "trace_id", "traceID":
			entry.TraceID = v
		}
	}
}

func parseLogfmt(line string) map[string]string {
	out := map[string]string{}
	for len(line) > 0 {
		line = strings.TrimLeft(line, " ")
		if line == "" {
			break
		}
		keyEnd := strings.IndexByte(line, '=')
		if keyEnd <= 0 {
			break
		}
		key := line[:keyEnd]
		line = line[keyEnd+1:]
		value := ""
		if strings.HasPrefix(line, `"`) {
			var err error
			value, line, err = consumeQuoted(line)
			if err != nil {
				break
			}
		} else {
			valueEnd := strings.IndexByte(line, ' ')
			if valueEnd < 0 {
				value = line
				line = ""
			} else {
				value = line[:valueEnd]
				line = line[valueEnd+1:]
			}
		}
		out[key] = value
	}
	return out
}

func consumeQuoted(s string) (string, string, error) {
	for i := 1; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == '"' {
			v, err := strconv.Unquote(s[:i+1])
			if err != nil {
				return "", "", err
			}
			return v, s[i+1:], nil
		}
	}
	return "", "", errors.New("unterminated quoted value")
}

func parseLokiTS(raw string) time.Time {
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return time.Unix(0, n).UTC()
	}
	if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return ts.UTC()
	}
	return time.Now().UTC()
}

func retentionDaysFromConfig(body []byte) int {
	var cfg map[string]any
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		return 0
	}
	if v, ok := findNested(cfg, "limits_config", "retention_period"); ok {
		return durationToDays(fmt.Sprint(v))
	}
	if v, ok := findNested(cfg, "compactor", "retention_enabled"); ok && fmt.Sprint(v) == "false" {
		return 0
	}
	return 0
}

func findNested(m map[string]any, path ...string) (any, bool) {
	var cur any = m
	for _, part := range path {
		next, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = next[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func durationToDays(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "0" || raw == "0s" {
		return 0
	}
	if strings.HasSuffix(raw, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(raw, "d"))
		if err == nil && n > 0 {
			return n
		}
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0
	}
	return int(d.Hours() / 24)
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 200
	}
	if limit > maxEntriesPerPage {
		return maxEntriesPerPage
	}
	return limit
}

func escapeLabelValue(v string) string {
	return strings.ReplaceAll(strings.ReplaceAll(v, `\`, `\\`), `"`, `\"`)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func coerceLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug", "info", "warn", "error":
		return strings.ToLower(strings.TrimSpace(level))
	case "fatal":
		return "error"
	default:
		return "info"
	}
}

type queryRangeResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string         `json:"resultType"`
		Result     []streamResult `json:"result"`
	} `json:"data"`
}

type tailFrame struct {
	Streams []streamResult `json:"streams"`
}

type streamResult struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}
