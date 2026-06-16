// SPDX-License-Identifier: Apache-2.0

// Package remote implements errors.Store through the errors-v1 HTTP sidecar
// protocol.
package remote

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	adapterremote "github.com/Agent-Field/backai/services/runtime/internal/adapters/remote"
	obserrors "github.com/Agent-Field/backai/services/runtime/internal/observability/errors"
)

type Config struct {
	BaseURL string
	Token   string
	Timeout time.Duration
}

type Store struct {
	client *adapterremote.Client
	caps   obserrors.Capabilities
	name   string
}

var _ obserrors.Store = (*Store)(nil)

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
	var caps obserrors.Capabilities
	if len(env.Capabilities) > 0 {
		if err := json.Unmarshal(env.Capabilities, &caps); err != nil {
			return nil, err
		}
	}
	return &Store{client: client, caps: caps, name: env.Name}, nil
}

func (s *Store) ListGroups(ctx context.Context, f obserrors.ListFilter) (obserrors.Page, error) {
	if s == nil || s.client == nil {
		return obserrors.Page{}, errors.New("remote errors: nil store")
	}
	resp, err := s.client.Do(ctx, adapterremote.Request{
		Method: http.MethodPost,
		Path:   "/v1/errors/list",
		Body:   filterWire(f),
	})
	if err != nil {
		return obserrors.Page{}, err
	}
	var out obserrors.Page
	if err := resp.DecodeJSON(&out); err != nil {
		return obserrors.Page{}, err
	}
	return out, nil
}

func (s *Store) GetGroup(ctx context.Context, groupID string) (obserrors.Group, error) {
	if s == nil || s.client == nil {
		return obserrors.Group{}, errors.New("remote errors: nil store")
	}
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return obserrors.Group{}, obserrors.ErrInvalidGroupIdentifier
	}
	resp, err := s.client.Do(ctx, adapterremote.Request{
		Method: http.MethodGet,
		Path:   "/v1/errors/" + url.PathEscape(groupID),
	})
	if err != nil {
		if p, ok := adapterremote.AsProblem(err); ok && (p.HTTPStatus == http.StatusNotFound || p.Code == "error_group_not_found") {
			return obserrors.Group{}, obserrors.ErrGroupNotFound
		}
		return obserrors.Group{}, err
	}
	var out obserrors.Group
	if err := resp.DecodeJSON(&out); err != nil {
		return obserrors.Group{}, err
	}
	return out, nil
}

func (s *Store) UpdateGroup(ctx context.Context, groupID string, update obserrors.Update) (obserrors.Group, error) {
	if s == nil || s.client == nil {
		return obserrors.Group{}, errors.New("remote errors: nil store")
	}
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return obserrors.Group{}, obserrors.ErrInvalidGroupIdentifier
	}
	resp, err := s.client.Do(ctx, adapterremote.Request{
		Method: http.MethodPatch,
		Path:   "/v1/errors/" + url.PathEscape(groupID),
		Body:   update,
	})
	if err != nil {
		if p, ok := adapterremote.AsProblem(err); ok {
			switch {
			case p.HTTPStatus == http.StatusNotFound || p.Code == "error_group_not_found":
				return obserrors.Group{}, obserrors.ErrGroupNotFound
			case p.HTTPStatus == http.StatusUnprocessableEntity || p.Code == "unsupported_capability":
				return obserrors.Group{}, obserrors.ErrUnsupportedCapability
			}
		}
		return obserrors.Group{}, err
	}
	var out obserrors.Group
	if err := resp.DecodeJSON(&out); err != nil {
		return obserrors.Group{}, err
	}
	return out, nil
}

func (s *Store) Capabilities() obserrors.Capabilities {
	if s == nil {
		return obserrors.Capabilities{}
	}
	return s.caps
}

func (s *Store) AdapterName() string {
	if s == nil || s.name == "" {
		return "remote"
	}
	return s.name
}

func filterWire(f obserrors.ListFilter) map[string]any {
	out := map[string]any{}
	if f.Status != "" {
		out["status"] = f.Status
	}
	if f.Service != "" {
		out["service"] = f.Service
	}
	if f.TenantID != "" {
		out["tenant_id"] = f.TenantID
	}
	if !f.Since.IsZero() {
		out["since"] = f.Since.UTC().Format(time.RFC3339Nano)
	}
	if f.Limit > 0 {
		out["limit"] = f.Limit
	}
	if f.Cursor != "" {
		out["cursor"] = f.Cursor
	}
	return out
}
