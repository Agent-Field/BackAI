// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http/httptest"
	"testing"
)

func TestLLMCallerLabels(t *testing.T) {
	tests := []struct {
		name         string
		header       string
		wantAgent    string
		wantReasoner string
	}{
		{name: "empty"},
		{name: "leaf", header: "summarize", wantReasoner: "summarize"},
		{name: "qualified", header: "notable-ai.summarize", wantAgent: "notable-ai", wantReasoner: "summarize"},
		{name: "path", header: "team/agent:reasoner", wantAgent: "team.agent", wantReasoner: "reasoner"},
		{name: "scrub", header: "agent one.reasoner<script>", wantAgent: "agentone", wantReasoner: "reasonerscript"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/v1/llm/chat/completions", nil)
			if tt.header != "" {
				req.Header.Set("X-AF-Reasoner", tt.header)
			}
			agent, reasoner := llmCallerLabels(req)
			if agent != tt.wantAgent || reasoner != tt.wantReasoner {
				t.Fatalf("labels=(%q,%q) want (%q,%q)", agent, reasoner, tt.wantAgent, tt.wantReasoner)
			}
		})
	}
}
