// SPDX-License-Identifier: Apache-2.0

// Package shipwright stores AF Stack metadata for the Shipwright coding-agent
// factory. AgentField remains the owner of run state, step logs, tool calls,
// spans, traces, and memory.
package shipwright

import (
	"errors"
	"time"
)

const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

var (
	ErrNotFound       = errors.New("shipwright: not found")
	ErrTenantRequired = errors.New("shipwright: tenant required")
	ErrValidation     = errors.New("shipwright: validation failed")
)

type Task struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	UserID      *string   `json:"user_id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	RepoURL     string    `json:"repo_url"`
	Status      string    `json:"status"`
	RunID       *string   `json:"run_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Patch struct {
	TaskID    string    `json:"task_id"`
	Ref       string    `json:"ref"`
	Summary   string    `json:"summary"`
	DiffURL   *string   `json:"diff_url"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateTaskInput struct {
	Title       string
	Description string
	RepoURL     string
	UserID      string
}

type ListTasksInput struct {
	Status string
	Limit  int
	Offset int
}

type ListTasksResult struct {
	Tasks   []Task
	Total   int
	HasMore bool
}

type CompleteTaskInput struct {
	TaskID  string
	Status  string
	Ref     string
	Summary string
	DiffURL string
}
