// SPDX-License-Identifier: Apache-2.0

package search

import "encoding/json"

const (
	DefaultNamespace = "default"
	EmbeddingDim     = 1536
)

type Mode string

const (
	ModeFTS    Mode = "fts"
	ModeVector Mode = "vector"
	ModeHybrid Mode = "hybrid"
)

func (m Mode) Valid() bool {
	switch m {
	case "", ModeFTS, ModeVector, ModeHybrid:
		return true
	}
	return false
}

func (m Mode) Canonical() Mode {
	if m == "" {
		return ModeHybrid
	}
	return m
}

type Document struct {
	TenantID     string         `json:"tenant_id"`
	Namespace    string         `json:"namespace"`
	Key          string         `json:"key"`
	Title        string         `json:"title"`
	Body         string         `json:"body"`
	Metadata     map[string]any `json:"metadata"`
	HasEmbedding bool           `json:"has_embedding"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
}

type UpsertInput struct {
	Namespace string         `json:"namespace,omitempty"`
	Key       string         `json:"key"`
	Title     string         `json:"title,omitempty"`
	Body      string         `json:"body,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Embed     bool           `json:"embed,omitempty"`
}

type DeleteInput struct {
	Namespace string
	Key       string
}

type Request struct {
	Query          string         `json:"query"`
	Mode           Mode           `json:"mode,omitempty"`
	Namespace      string         `json:"namespace,omitempty"`
	MetadataFilter map[string]any `json:"metadata_filter,omitempty"`
	Limit          int            `json:"limit,omitempty"`
	Offset         int            `json:"offset,omitempty"`
}

type Hit struct {
	Document   Document `json:"document"`
	Score      float64  `json:"score"`
	FTSRank    *float64 `json:"fts_rank,omitempty"`
	Similarity *float64 `json:"similarity,omitempty"`
}

type Result struct {
	Hits       []Hit `json:"hits"`
	Mode       Mode  `json:"mode"`
	DurationMS int64 `json:"duration_ms"`
}

func normaliseMetadata(m map[string]any) ([]byte, error) {
	if m == nil {
		return []byte(`{}`), nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	if string(b) == "null" {
		return []byte(`{}`), nil
	}
	return b, nil
}
