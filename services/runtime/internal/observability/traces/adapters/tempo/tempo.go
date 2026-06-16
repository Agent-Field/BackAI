// SPDX-License-Identifier: Apache-2.0

// Package tempo implements traces.Store using Grafana Tempo's HTTP API.
package tempo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/observability/traces"
)

const maxResultsPerQuery = 1000

type Config struct {
	BaseURL    string
	Tenant     string
	HTTPClient *http.Client
}

type Store struct {
	baseURL string
	tenant  string
	client  *http.Client
	caps    traces.Capabilities
}

var _ traces.Store = (*Store)(nil)

func New(ctx context.Context, cfg Config) (*Store, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, errors.New("tempo traces: url required")
	}
	if _, err := url.ParseRequestURI(base); err != nil {
		return nil, fmt.Errorf("tempo traces: parse url: %w", err)
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	s := &Store{
		baseURL: base,
		tenant:  strings.TrimSpace(cfg.Tenant),
		client:  client,
		caps: traces.Capabilities{
			SupportsTagSearch:  true,
			RetentionHours:     0,
			MaxResultsPerQuery: maxResultsPerQuery,
		},
	}
	version, err := s.probeVersion(ctx)
	if err == nil && versionAtLeast(version, 2, 0) {
		s.caps.SupportsTraceQL = true
		s.caps.NativeQueryLang = "traceql"
	}
	return s, nil
}

func (s *Store) Search(ctx context.Context, f traces.SearchFilter) (traces.SearchResult, error) {
	if s == nil {
		return traces.SearchResult{}, errors.New("tempo traces: nil store")
	}
	limit := normalizeLimit(f.Limit)
	q := url.Values{}
	if s.caps.SupportsTraceQL {
		q.Set("q", searchFilterToTraceQL(f))
	} else {
		q.Set("tags", searchFilterToTags(f))
	}
	if !f.From.IsZero() {
		q.Set("start", strconv.FormatInt(f.From.UTC().Unix(), 10))
	}
	if !f.To.IsZero() {
		q.Set("end", strconv.FormatInt(f.To.UTC().Unix(), 10))
	}
	if f.MinDuration > 0 {
		q.Set("minDuration", f.MinDuration.String())
	}
	if f.MaxDuration > 0 {
		q.Set("maxDuration", f.MaxDuration.String())
	}
	q.Set("limit", strconv.Itoa(limit))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/api/search?"+q.Encode(), nil)
	if err != nil {
		return traces.SearchResult{}, err
	}
	s.applyTenant(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return traces.SearchResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return traces.SearchResult{}, fmt.Errorf("tempo search HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var out searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return traces.SearchResult{}, fmt.Errorf("decode tempo search: %w", err)
	}
	results := summariesFromSearch(out.Traces)
	return traces.SearchResult{Traces: results, HasMore: len(results) >= limit}, nil
}

func (s *Store) Get(ctx context.Context, traceID string) (traces.Trace, error) {
	if s == nil {
		return traces.Trace{}, errors.New("tempo traces: nil store")
	}
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return traces.Trace{}, traces.ErrInvalidTraceID
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/api/traces/"+url.PathEscape(traceID), nil)
	if err != nil {
		return traces.Trace{}, err
	}
	s.applyTenant(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return traces.Trace{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return traces.Trace{}, traces.ErrTraceNotFound
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return traces.Trace{}, fmt.Errorf("tempo trace HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return traces.Trace{}, err
	}
	trace, err := decodeTrace(traceID, body)
	if err != nil {
		return traces.Trace{}, err
	}
	return trace, nil
}

func (s *Store) Capabilities() traces.Capabilities {
	if s == nil {
		return traces.Capabilities{}
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
		return fmt.Errorf("tempo ready HTTP %d", resp.StatusCode)
	}
	return nil
}

func (s *Store) probeVersion(ctx context.Context) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/status/version", nil)
	if err != nil {
		return "", err
	}
	s.applyTenant(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("tempo version HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", err
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err == nil {
		for _, key := range []string{"version", "Version"} {
			if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
				return value, nil
			}
		}
	}
	if version := versionFromText(string(body)); version != "" {
		return version, nil
	}
	return "", errors.New("tempo version missing")
}

func (s *Store) applyTenant(req *http.Request) {
	if s != nil && s.tenant != "" {
		req.Header.Set("X-Scope-OrgID", s.tenant)
	}
}

func searchFilterToTags(f traces.SearchFilter) string {
	pairs := make([]string, 0, 4+len(f.Tag))
	if f.Service != "" {
		pairs = append(pairs, "service.name="+quoteLogfmtValue(f.Service))
	}
	if f.Operation != "" {
		pairs = append(pairs, "operation="+quoteLogfmtValue(f.Operation))
	}
	if f.Status != "" {
		pairs = append(pairs, "status="+quoteLogfmtValue(f.Status))
	}
	keys := make([]string, 0, len(f.Tag))
	for k := range f.Tag {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		pairs = append(pairs, key+"="+quoteLogfmtValue(f.Tag[key]))
	}
	return strings.Join(pairs, " ")
}

func searchFilterToTraceQL(f traces.SearchFilter) string {
	parts := make([]string, 0, 4+len(f.Tag))
	if f.Service != "" {
		parts = append(parts, `resource.service.name = `+strconv.Quote(f.Service))
	}
	if f.Operation != "" {
		parts = append(parts, `name = `+strconv.Quote(f.Operation))
	}
	if f.Status != "" {
		parts = append(parts, `status = `+traceQLStatus(f.Status))
	}
	keys := make([]string, 0, len(f.Tag))
	for k := range f.Tag {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		parts = append(parts, traceQLAttribute(key)+` = `+strconv.Quote(f.Tag[key]))
	}
	if len(parts) == 0 {
		return "{}"
	}
	return "{ " + strings.Join(parts, " && ") + " }"
}

func traceQLStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "error", "failed":
		return "error"
	case "ok", "success":
		return "ok"
	default:
		return strconv.Quote(status)
	}
}

var traceQLIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

func traceQLAttribute(key string) string {
	key = strings.TrimSpace(key)
	if traceQLIdentifier.MatchString(key) {
		return "span." + key
	}
	return `span[` + strconv.Quote(key) + `]`
}

func quoteLogfmtValue(v string) string {
	if v == "" || strings.ContainsAny(v, " \t\r\n\"=") {
		return strconv.Quote(v)
	}
	return v
}

func summariesFromSearch(in []searchTrace) []traces.TraceSummary {
	out := make([]traces.TraceSummary, 0, len(in))
	for _, tr := range in {
		id := firstNonEmpty(tr.TraceID, tr.TraceIDAlt)
		if id == "" {
			continue
		}
		start := timeFromUnixNano(firstNonZeroInt64(int64(tr.StartTimeUnixNano), int64(tr.StartTimeUnixNanoAlt)))
		duration := durationFromSearch(tr)
		spanCount := tr.SpanCount
		if spanCount == 0 {
			spanCount = int(tr.SpanSet.Spans)
		}
		status := normalizeStatus(firstNonEmpty(tr.Status, tr.RootSpanStatus))
		out = append(out, traces.TraceSummary{
			TraceID:       id,
			RootService:   firstNonEmpty(tr.RootServiceName, tr.RootService),
			RootOperation: firstNonEmpty(tr.RootTraceName, tr.RootOperation, tr.Name),
			StartTime:     start,
			Duration:      duration,
			SpanCount:     spanCount,
			Status:        status,
		})
	}
	return out
}

func durationFromSearch(tr searchTrace) time.Duration {
	if tr.DurationMs > 0 {
		return time.Duration(tr.DurationMs) * time.Millisecond
	}
	if tr.DurationNano > 0 {
		return time.Duration(tr.DurationNano)
	}
	return 0
}

func decodeTrace(fallbackTraceID string, body []byte) (traces.Trace, error) {
	var payload tracePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return traces.Trace{}, fmt.Errorf("decode tempo trace: %w", err)
	}
	traceID := firstNonEmpty(payload.TraceID, fallbackTraceID)
	spans := make([]traces.Span, 0)
	for _, batch := range payload.allBatches() {
		resourceAttrs := attributesToMap(batch.Resource.Attributes)
		service := stringFromMap(resourceAttrs, "service.name", "service", "job")
		for _, group := range batch.allScopeSpans() {
			for _, rawSpan := range group.Spans {
				span := spanFromOTLP(rawSpan, resourceAttrs, service)
				if traceID == "" {
					traceID = firstNonEmpty(rawSpan.TraceID, rawSpan.TraceIDAlt)
				}
				spans = append(spans, span)
			}
		}
	}
	if traceID == "" {
		return traces.Trace{}, traces.ErrTraceNotFound
	}
	return traces.Trace{TraceID: traceID, Spans: spans}, nil
}

func spanFromOTLP(raw otlpSpan, resourceAttrs map[string]any, resourceService string) traces.Span {
	attrs := mergeMaps(resourceAttrs, attributesToMap(raw.Attributes))
	service := firstNonEmpty(stringFromMap(attrs, "service.name", "service", "job"), resourceService, "tempo")
	start := timeFromUnixNano(parseInt64(raw.StartTimeUnixNano))
	end := timeFromUnixNano(parseInt64(raw.EndTimeUnixNano))
	duration := time.Duration(0)
	if !start.IsZero() && !end.IsZero() && end.After(start) {
		duration = end.Sub(start)
	}
	parent := firstNonEmpty(raw.ParentSpanID, raw.ParentSpanIDAlt)
	if parent == "0000000000000000" {
		parent = ""
	}
	events := make([]traces.SpanEvent, 0, len(raw.Events))
	for _, ev := range raw.Events {
		events = append(events, traces.SpanEvent{
			TS:         timeFromUnixNano(parseInt64(ev.TimeUnixNano)),
			Name:       ev.Name,
			Attributes: attributesToMap(ev.Attributes),
		})
	}
	return traces.Span{
		SpanID:       firstNonEmpty(raw.SpanID, raw.SpanIDAlt),
		ParentSpanID: parent,
		Service:      service,
		Operation:    raw.Name,
		StartTime:    start,
		Duration:     duration,
		Status:       statusFromOTLP(raw.Status),
		Attributes:   attrs,
		Events:       events,
	}
}

func attributesToMap(attrs []otlpAttribute) map[string]any {
	out := make(map[string]any, len(attrs))
	for _, attr := range attrs {
		if attr.Key == "" {
			continue
		}
		out[attr.Key] = valueFromAnyValue(attr.Value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func valueFromAnyValue(v anyValue) any {
	switch {
	case v.StringValue != "":
		return v.StringValue
	case v.IntValue != "":
		if n, err := strconv.ParseInt(v.IntValue, 10, 64); err == nil {
			return n
		}
		return v.IntValue
	case v.DoubleValue != 0:
		return v.DoubleValue
	case v.BoolValue != nil:
		return *v.BoolValue
	case len(v.ArrayValue.Values) > 0:
		out := make([]any, 0, len(v.ArrayValue.Values))
		for _, item := range v.ArrayValue.Values {
			out = append(out, valueFromAnyValue(item))
		}
		return out
	case len(v.KVListValue.Values) > 0:
		out := make(map[string]any, len(v.KVListValue.Values))
		for _, item := range v.KVListValue.Values {
			out[item.Key] = valueFromAnyValue(item.Value)
		}
		return out
	default:
		return nil
	}
}

func statusFromOTLP(status otlpStatus) string {
	if status.Code != "" {
		return normalizeStatus(status.Code)
	}
	return "unset"
}

func normalizeStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ok", "status_code_ok":
		return "ok"
	case "error", "failed", "status_code_error":
		return "error"
	default:
		return "unset"
	}
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > maxResultsPerQuery {
		return maxResultsPerQuery
	}
	return limit
}

func versionAtLeast(version string, major, minor int) bool {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}
	gotMajor, err := strconv.Atoi(numberPrefix(parts[0]))
	if err != nil {
		return false
	}
	gotMinor, err := strconv.Atoi(numberPrefix(parts[1]))
	if err != nil {
		return false
	}
	if gotMajor != major {
		return gotMajor > major
	}
	return gotMinor >= minor
}

var tempoVersionText = regexp.MustCompile(`version\s+v?([0-9]+\.[0-9]+(?:\.[0-9]+)?)`)

func versionFromText(raw string) string {
	if match := tempoVersionText.FindStringSubmatch(raw); len(match) == 2 {
		return match[1]
	}
	return ""
}

func numberPrefix(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if r < '0' || r > '9' {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

func timeFromUnixNano(n int64) time.Time {
	if n <= 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}

func parseInt64(raw string) int64 {
	n, _ := strconv.ParseInt(raw, 10, 64)
	return n
}

func firstNonZeroInt64(values ...int64) int64 {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func stringFromMap(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key]; ok && v != nil {
			return fmt.Sprint(v)
		}
	}
	return ""
}

func mergeMaps(base, overlay map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(overlay))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range overlay {
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

type searchResponse struct {
	Traces []searchTrace `json:"traces"`
}

type searchTrace struct {
	TraceID              string        `json:"traceID"`
	TraceIDAlt           string        `json:"trace_id"`
	RootServiceName      string        `json:"rootServiceName"`
	RootService          string        `json:"root_service"`
	RootTraceName        string        `json:"rootTraceName"`
	RootOperation        string        `json:"root_operation"`
	Name                 string        `json:"name"`
	StartTimeUnixNano    flexibleInt64 `json:"startTimeUnixNano"`
	StartTimeUnixNanoAlt flexibleInt64 `json:"start_time_unix_nano"`
	DurationMs           flexibleInt64 `json:"durationMs"`
	DurationNano         int64         `json:"durationNano"`
	SpanCount            int           `json:"spanCount"`
	SpanSet              searchSpanSet `json:"spanSet"`
	Status               string        `json:"status"`
	RootSpanStatus       string        `json:"rootSpanStatus"`
}

type searchSpanSet struct {
	Spans flexibleCount `json:"spans"`
}

type flexibleInt64 int64

func (n *flexibleInt64) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	raw = strings.Trim(raw, `"`)
	if raw == "" || raw == "null" {
		*n = 0
		return nil
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return err
	}
	*n = flexibleInt64(parsed)
	return nil
}

type flexibleCount int

func (n *flexibleCount) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*n = 0
		return nil
	}
	if strings.HasPrefix(raw, "[") {
		var items []json.RawMessage
		if err := json.Unmarshal(data, &items); err != nil {
			return err
		}
		*n = flexibleCount(len(items))
		return nil
	}
	raw = strings.Trim(raw, `"`)
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return err
	}
	*n = flexibleCount(parsed)
	return nil
}

type tracePayload struct {
	TraceID             string       `json:"traceID"`
	Batches             []traceBatch `json:"batches"`
	ResourceSpans       []traceBatch `json:"resourceSpans"`
	ResourceSpansLegacy []traceBatch `json:"resource_spans"`
}

func (p tracePayload) allBatches() []traceBatch {
	out := make([]traceBatch, 0, len(p.Batches)+len(p.ResourceSpans)+len(p.ResourceSpansLegacy))
	out = append(out, p.Batches...)
	out = append(out, p.ResourceSpans...)
	out = append(out, p.ResourceSpansLegacy...)
	return out
}

type traceBatch struct {
	Resource                    otlpResource     `json:"resource"`
	ScopeSpans                  []scopeSpanGroup `json:"scopeSpans"`
	InstrumentationLibrarySpans []scopeSpanGroup `json:"instrumentationLibrarySpans"`
	ScopeSpansLegacy            []scopeSpanGroup `json:"scope_spans"`
}

func (b traceBatch) allScopeSpans() []scopeSpanGroup {
	out := make([]scopeSpanGroup, 0, len(b.ScopeSpans)+len(b.InstrumentationLibrarySpans)+len(b.ScopeSpansLegacy))
	out = append(out, b.ScopeSpans...)
	out = append(out, b.InstrumentationLibrarySpans...)
	out = append(out, b.ScopeSpansLegacy...)
	return out
}

type otlpResource struct {
	Attributes []otlpAttribute `json:"attributes"`
}

type scopeSpanGroup struct {
	Spans []otlpSpan `json:"spans"`
}

type otlpSpan struct {
	TraceID           string          `json:"traceID"`
	TraceIDAlt        string          `json:"traceId"`
	SpanID            string          `json:"spanID"`
	SpanIDAlt         string          `json:"spanId"`
	ParentSpanID      string          `json:"parentSpanID"`
	ParentSpanIDAlt   string          `json:"parentSpanId"`
	Name              string          `json:"name"`
	StartTimeUnixNano string          `json:"startTimeUnixNano"`
	EndTimeUnixNano   string          `json:"endTimeUnixNano"`
	Attributes        []otlpAttribute `json:"attributes"`
	Events            []otlpEvent     `json:"events"`
	Status            otlpStatus      `json:"status"`
}

type otlpEvent struct {
	TimeUnixNano string          `json:"timeUnixNano"`
	Name         string          `json:"name"`
	Attributes   []otlpAttribute `json:"attributes"`
}

type otlpStatus struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type otlpAttribute struct {
	Key   string   `json:"key"`
	Value anyValue `json:"value"`
}

type anyValue struct {
	StringValue string  `json:"stringValue"`
	IntValue    string  `json:"intValue"`
	DoubleValue float64 `json:"doubleValue"`
	BoolValue   *bool   `json:"boolValue"`
	ArrayValue  struct {
		Values []anyValue `json:"values"`
	} `json:"arrayValue"`
	KVListValue struct {
		Values []otlpAttribute `json:"values"`
	} `json:"kvlistValue"`
}
