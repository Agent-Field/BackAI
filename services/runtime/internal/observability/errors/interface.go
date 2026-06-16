// SPDX-License-Identifier: Apache-2.0

// Package errors defines the runtime errors adapter contract.
package errors

import (
	"context"
	stderrors "errors"
	"time"
)

var (
	ErrNoBackend              = stderrors.New("errors_no_backend")
	ErrGroupNotFound          = stderrors.New("error_group_not_found")
	ErrUnsupportedCapability  = stderrors.New("unsupported_capability")
	ErrUnsupportedBackend     = stderrors.New("unsupported_errors_backend")
	ErrInvalidStatus          = stderrors.New("invalid_error_status")
	ErrInvalidGroupIdentifier = stderrors.New("invalid_error_group_id")
)

const (
	StatusOpen     = "open"
	StatusMuted    = "muted"
	StatusResolved = "resolved"

	PersistenceVolatile = "volatile"
	PersistenceDurable  = "durable"
	PersistenceRemote   = "remote"
)

// Group is one normalized error group surfaced to operators.
type Group struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Service     string         `json:"service"`
	Status      string         `json:"status"`
	Count       int            `json:"count"`
	UserCount   int            `json:"user_count,omitempty"`
	FirstSeen   time.Time      `json:"first_seen"`
	LastSeen    time.Time      `json:"last_seen"`
	Fingerprint string         `json:"fingerprint,omitempty"`
	Permalink   string         `json:"permalink,omitempty"`
	Culprit     string         `json:"culprit,omitempty"`
	SampleEvent map[string]any `json:"sample_event,omitempty"`
}

// ListFilter controls admin-facing error group list queries.
type ListFilter struct {
	Status   string
	Service  string
	TenantID string
	Since    time.Time
	Limit    int
	Cursor   string
}

// Page is one window of grouped errors.
type Page struct {
	Groups     []Group `json:"groups"`
	NextCursor string  `json:"next_cursor,omitempty"`
	HasMore    bool    `json:"has_more"`
}

// Update is a state transition request for one group.
type Update struct {
	Status string `json:"status"`
}

// Capabilities describes the active errors adapter.
type Capabilities struct {
	SupportsList     bool   `json:"supports_list"`
	SupportsGet      bool   `json:"supports_get"`
	SupportsMute     bool   `json:"supports_mute"`
	SupportsResolve  bool   `json:"supports_resolve"`
	SupportsIngest   bool   `json:"supports_ingest"`
	SupportsAlerting bool   `json:"supports_alerting"`
	NativeQueryLang  string `json:"native_query_lang"`
	RetentionDays    int    `json:"retention_days"`
	Persistence      string `json:"persistence"`
	MaxGroupsPerPage int    `json:"max_groups_per_page"`
}

// Store is the errors adapter contract. Implementations must keep outbound
// calls bounded and honor context cancellation.
type Store interface {
	ListGroups(ctx context.Context, filter ListFilter) (Page, error)
	GetGroup(ctx context.Context, groupID string) (Group, error)
	UpdateGroup(ctx context.Context, groupID string, update Update) (Group, error)
	Capabilities() Capabilities
}

func NormalizeStatus(status string) (string, error) {
	switch status {
	case "", StatusOpen:
		return StatusOpen, nil
	case StatusMuted:
		return StatusMuted, nil
	case StatusResolved:
		return StatusResolved, nil
	default:
		return "", ErrInvalidStatus
	}
}
