// SPDX-License-Identifier: Apache-2.0

package activity

import "time"

type ActorType string

const (
	ActorUser      ActorType = "user"
	ActorAPIKey    ActorType = "api_key"
	ActorSystem    ActorType = "system"
	ActorAnonymous ActorType = "anonymous"
)

func (a ActorType) Valid() bool {
	switch a {
	case "", ActorUser, ActorAPIKey, ActorSystem, ActorAnonymous:
		return true
	}
	return false
}

type Entry struct {
	ID           string         `json:"id"`
	TenantID     string         `json:"tenant_id"`
	UserID       *string        `json:"user_id"`
	APIKeyID     *string        `json:"api_key_id"`
	ActorType    ActorType      `json:"actor_type"`
	Action       string         `json:"action"`
	ResourceType *string        `json:"resource_type"`
	ResourceID   *string        `json:"resource_id"`
	Metadata     map[string]any `json:"metadata"`
	IP           *string        `json:"ip"`
	UserAgent    *string        `json:"user_agent"`
	OccurredAt   string         `json:"occurred_at"`
}

type LogInput struct {
	UserID       string         `json:"user_id,omitempty"`
	ActorType    ActorType      `json:"actor_type,omitempty"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resource_type,omitempty"`
	ResourceID   string         `json:"resource_id,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	OccurredAt   *time.Time     `json:"occurred_at,omitempty"`
	IP           string         `json:"-"`
	UserAgent    string         `json:"-"`
}

type ListFilter struct {
	UserID       string
	Action       string
	ResourceType string
	ResourceID   string
	From         *time.Time
	To           *time.Time
	Limit        int
	Offset       int
}

type Page struct {
	Entries []Entry `json:"entries"`
	Total   int     `json:"total"`
	HasMore bool    `json:"has_more"`
}
