// SPDX-License-Identifier: Apache-2.0

// Package approvals stores tenant-scoped human decision gates for AF Stack
// workloads. It does not duplicate AgentField run approval state.
package approvals

import (
	"errors"
	"time"
)

const (
	StatusPending   = "pending"
	StatusApproved  = "approved"
	StatusDenied    = "denied"
	StatusCancelled = "cancelled"
)

var (
	ErrNotFound       = errors.New("approvals: not found")
	ErrTenantRequired = errors.New("approvals: tenant required")
	ErrValidation     = errors.New("approvals: validation failed")
)

type Approval struct {
	ID           string         `json:"id"`
	TenantID     string         `json:"tenant_id"`
	RequestedBy  *string        `json:"requested_by"`
	Kind         string         `json:"kind"`
	Payload      map[string]any `json:"payload"`
	Status       string         `json:"status"`
	DecidedBy    *string        `json:"decided_by"`
	DecidedAt    *time.Time     `json:"decided_at"`
	DecisionNote *string        `json:"decision_note"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type RequestInput struct {
	Kind        string
	Payload     map[string]any
	RequestedBy string
}

type DecideInput struct {
	ID           string
	Status       string
	DecisionNote string
	DecidedBy    string
}

type ListInput struct {
	Status string
	Kind   string
	Limit  int
	Offset int
}

type ListResult struct {
	Approvals []Approval `json:"approvals"`
	Total     int        `json:"total"`
	HasMore   bool       `json:"has_more"`
}
