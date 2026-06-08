// SPDX-License-Identifier: Apache-2.0

// Package guardrails implements gateway-local PII redaction and
// regex-backed moderation. It owns only request/response policy at the
// AF Stack gateway boundary; AgentField remains the owner of runs,
// spans, traces, memory, and agent tool state.
package guardrails

import (
	"errors"
	"net/http"
	"time"
)

const (
	ProviderRegex    = "regex"
	ProviderPresidio = "presidio"

	CodeContentBlocked        = "CONTENT_BLOCKED"
	CodeGuardrailsUnavailable = "GUARDRAILS_UNAVAILABLE"
)

var (
	ErrContentBlocked        = errors.New("content blocked by moderation rule")
	ErrGuardrailsUnavailable = errors.New("guardrails provider unavailable")
)

type Config struct {
	Enabled               bool
	RedactPII             bool
	Moderate              bool
	Provider              string
	PresidioAnalyzerURL   string
	PresidioAnonymizerURL string
	BlockPatterns         []string
	Timeout               time.Duration
	MaxTextBytes          int
	HTTPClient            *http.Client
}

type Finding struct {
	Type   string `json:"type"`
	Start  int    `json:"start"`
	End    int    `json:"end"`
	Source string `json:"source"`
}

type Result struct {
	Text     string
	Findings []Finding
	Redacted bool
	Blocked  bool
	Rule     string
	Provider string
}

type ModerationError struct {
	Rule string
}

func (e *ModerationError) Error() string {
	if e == nil || e.Rule == "" {
		return ErrContentBlocked.Error()
	}
	return ErrContentBlocked.Error() + ": " + e.Rule
}

func (e *ModerationError) Unwrap() error {
	return ErrContentBlocked
}

type ProviderError struct {
	Provider string
	Err      error
}

func (e *ProviderError) Error() string {
	if e == nil {
		return ErrGuardrailsUnavailable.Error()
	}
	if e.Provider == "" {
		return ErrGuardrailsUnavailable.Error() + ": " + e.Err.Error()
	}
	return ErrGuardrailsUnavailable.Error() + ": " + e.Provider + ": " + e.Err.Error()
}

func (e *ProviderError) Unwrap() error {
	return ErrGuardrailsUnavailable
}
