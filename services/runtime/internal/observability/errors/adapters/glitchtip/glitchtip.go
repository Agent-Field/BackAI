// SPDX-License-Identifier: Apache-2.0

// Package glitchtip implements errors.Store against GlitchTip's
// Sentry-compatible organization issue API.
package glitchtip

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	obserrors "github.com/Agent-Field/backai/services/runtime/internal/observability/errors"
)

const (
	defaultTimeout = 10 * time.Second
	defaultLimit   = 50
	maxLimit       = 100
)

type Config struct {
	BaseURL string
	Org     string
	Token   string
	Timeout time.Duration
	Client  *http.Client
}

type Store struct {
	base  *url.URL
	org   string
	token string
	hc    *http.Client
	caps  obserrors.Capabilities
}

var _ obserrors.Store = (*Store)(nil)

func New(_ context.Context, cfg Config) (*Store, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("glitchtip: base URL required")
	}
	if strings.TrimSpace(cfg.Org) == "" {
		return nil, errors.New("glitchtip: org required")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("glitchtip: token required")
	}
	base, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("glitchtip: parse base URL: %w", err)
	}
	if base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("glitchtip: base URL needs scheme + host: %s", cfg.BaseURL)
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	hc := cfg.Client
	if hc == nil {
		hc = &http.Client{Timeout: timeout}
	}
	return &Store{
		base:  base,
		org:   strings.Trim(strings.TrimSpace(cfg.Org), "/"),
		token: strings.TrimSpace(cfg.Token),
		hc:    hc,
		caps: obserrors.Capabilities{
			SupportsList:     true,
			SupportsGet:      true,
			SupportsMute:     true,
			SupportsResolve:  true,
			SupportsIngest:   true,
			SupportsAlerting: false,
			NativeQueryLang:  "sentry-search",
			RetentionDays:    0,
			Persistence:      obserrors.PersistenceDurable,
			MaxGroupsPerPage: maxLimit,
		},
	}, nil
}

func (s *Store) ListGroups(ctx context.Context, f obserrors.ListFilter) (obserrors.Page, error) {
	if s == nil {
		return obserrors.Page{}, obserrors.ErrNoBackend
	}
	query := url.Values{}
	query.Set("query", sentryQuery(f))
	query.Set("limit", strconv.Itoa(normalizeLimit(f.Limit)))
	if f.Cursor != "" {
		query.Set("cursor", f.Cursor)
	}
	if !f.Since.IsZero() {
		query.Set("statsPeriod", statsPeriod(f.Since, time.Now()))
	}
	resp, err := s.do(ctx, http.MethodGet, "/api/0/organizations/"+url.PathEscape(s.org)+"/issues/", query, nil)
	if err != nil {
		return obserrors.Page{}, err
	}
	defer resp.Body.Close()
	var raw []issueWire
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return obserrors.Page{}, err
	}
	groups := make([]obserrors.Group, 0, len(raw))
	for _, issue := range raw {
		groups = append(groups, issue.toGroup())
	}
	next, hasMore := parseNextCursor(resp.Header.Get("Link"))
	return obserrors.Page{Groups: groups, NextCursor: next, HasMore: hasMore}, nil
}

func (s *Store) GetGroup(ctx context.Context, groupID string) (obserrors.Group, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return obserrors.Group{}, obserrors.ErrInvalidGroupIdentifier
	}
	resp, err := s.do(ctx, http.MethodGet, "/api/0/organizations/"+url.PathEscape(s.org)+"/issues/"+url.PathEscape(groupID)+"/", nil, nil)
	if err != nil {
		var methodNotAllowed methodNotAllowedError
		if errors.As(err, &methodNotAllowed) {
			return s.getGroupFromOrgList(ctx, groupID)
		}
		return obserrors.Group{}, err
	}
	defer resp.Body.Close()
	var raw issueWire
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return obserrors.Group{}, err
	}
	return raw.toGroup(), nil
}

func (s *Store) getGroupFromOrgList(ctx context.Context, groupID string) (obserrors.Group, error) {
	for _, status := range []string{obserrors.StatusOpen, obserrors.StatusMuted, obserrors.StatusResolved} {
		cursor := ""
		for pageCount := 0; pageCount < 20; pageCount++ {
			page, err := s.ListGroups(ctx, obserrors.ListFilter{
				Status: status,
				Cursor: cursor,
				Limit:  maxLimit,
			})
			if err != nil {
				return obserrors.Group{}, err
			}
			for _, group := range page.Groups {
				if group.ID == groupID {
					return group, nil
				}
			}
			if !page.HasMore || page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
	}
	return obserrors.Group{}, obserrors.ErrGroupNotFound
}

func (s *Store) UpdateGroup(ctx context.Context, groupID string, update obserrors.Update) (obserrors.Group, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return obserrors.Group{}, obserrors.ErrInvalidGroupIdentifier
	}
	status, err := obserrors.NormalizeStatus(update.Status)
	if err != nil {
		return obserrors.Group{}, err
	}
	body := map[string]string{"status": toSentryStatus(status)}
	resp, err := s.do(ctx, http.MethodPut, "/api/0/organizations/"+url.PathEscape(s.org)+"/issues/"+url.PathEscape(groupID)+"/", nil, body)
	if err != nil {
		return obserrors.Group{}, err
	}
	defer resp.Body.Close()
	var raw issueWire
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return obserrors.Group{}, err
	}
	return raw.toGroup(), nil
}

func (s *Store) Capabilities() obserrors.Capabilities {
	if s == nil {
		return obserrors.Capabilities{}
	}
	return s.caps
}

func (s *Store) do(ctx context.Context, method, path string, query url.Values, body any) (*http.Response, error) {
	u := *s.base.JoinPath(strings.TrimPrefix(path, "/"))
	if query != nil {
		u.RawQuery = query.Encode()
	}
	var reader io.Reader
	if body != nil {
		var buf strings.Builder
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, err
		}
		reader = strings.NewReader(buf.String())
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	resp, err := s.hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusNotFound {
		return nil, obserrors.ErrGroupNotFound
	}
	if resp.StatusCode == http.StatusMethodNotAllowed {
		return nil, methodNotAllowedError{method: method, path: path}
	}
	return nil, fmt.Errorf("glitchtip %s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
}

type methodNotAllowedError struct {
	method string
	path   string
}

func (e methodNotAllowedError) Error() string {
	return "glitchtip " + e.method + " " + e.path + ": method not allowed"
}

func sentryQuery(f obserrors.ListFilter) string {
	status, err := obserrors.NormalizeStatus(f.Status)
	if err != nil {
		status = obserrors.StatusOpen
	}
	parts := []string{"is:" + toSentryStatus(status)}
	if f.Service != "" {
		parts = append(parts, "logger:"+f.Service)
	}
	if f.TenantID != "" {
		parts = append(parts, "tenant_id:"+f.TenantID)
	}
	return strings.Join(parts, " ")
}

func toSentryStatus(status string) string {
	switch status {
	case obserrors.StatusMuted:
		return "ignored"
	case obserrors.StatusResolved:
		return "resolved"
	default:
		return "unresolved"
	}
}

func fromSentryStatus(status string) string {
	switch status {
	case "ignored":
		return obserrors.StatusMuted
	case "resolved":
		return obserrors.StatusResolved
	default:
		return obserrors.StatusOpen
	}
}

func parseNextCursor(header string) (string, bool) {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, `rel="next"`) || !strings.Contains(part, `results="true"`) {
			continue
		}
		for _, segment := range strings.Split(part, ";") {
			segment = strings.TrimSpace(segment)
			if strings.HasPrefix(segment, `cursor="`) && strings.HasSuffix(segment, `"`) {
				return strings.Trim(segment[len("cursor="):], `"`), true
			}
		}
	}
	return "", false
}

func statsPeriod(since, now time.Time) string {
	if since.IsZero() || now.Before(since) {
		return "14d"
	}
	hours := int(now.Sub(since).Hours())
	switch {
	case hours <= 24:
		return "24h"
	case hours <= 7*24:
		return "7d"
	default:
		return "14d"
	}
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

type issueWire struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Culprit     string         `json:"culprit"`
	Logger      string         `json:"logger"`
	Status      string         `json:"status"`
	Count       flexibleInt    `json:"count"`
	UserCount   int            `json:"userCount"`
	FirstSeen   time.Time      `json:"firstSeen"`
	LastSeen    time.Time      `json:"lastSeen"`
	Permalink   string         `json:"permalink"`
	LatestEvent map[string]any `json:"latestEvent"`
	Project     struct {
		Slug string `json:"slug"`
	} `json:"project"`
}

func (i issueWire) toGroup() obserrors.Group {
	service := i.Logger
	if service == "" {
		service = i.Project.Slug
	}
	count := int(i.Count)
	if count == 0 {
		count = 1
	}
	return obserrors.Group{
		ID:          i.ID,
		Title:       defaultString(i.Title, "GlitchTip issue"),
		Service:     defaultString(service, "glitchtip"),
		Status:      fromSentryStatus(i.Status),
		Count:       count,
		UserCount:   i.UserCount,
		FirstSeen:   i.FirstSeen.UTC(),
		LastSeen:    i.LastSeen.UTC(),
		Fingerprint: i.ID,
		Permalink:   i.Permalink,
		Culprit:     i.Culprit,
		SampleEvent: i.LatestEvent,
	}
}

type flexibleInt int

func (f *flexibleInt) UnmarshalJSON(data []byte) error {
	var n int
	if err := json.Unmarshal(data, &n); err == nil {
		*f = flexibleInt(n)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		*f = 0
		return nil
	}
	parsed, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	*f = flexibleInt(parsed)
	return nil
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
