// SPDX-License-Identifier: Apache-2.0

package server

import "testing"

// Contract: the label recorded for an LLM gateway run is the caller
// identity from X-AF-Reasoner ("agent.reasoner"), degrading gracefully
// when only one part is present, and empty when the header is absent
// (the Runs view then falls back to agentFromEndpoint).
func TestLLMRunLabel(t *testing.T) {
	cases := []struct {
		agent, reasoner, want string
	}{
		{"courtsim", "stage_briefs", "courtsim.stage_briefs"},
		{"", "stage_briefs", "stage_briefs"},
		{"courtsim", "", "courtsim"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := llmRunLabel(c.agent, c.reasoner); got != c.want {
			t.Errorf("llmRunLabel(%q,%q) = %q want %q", c.agent, c.reasoner, got, c.want)
		}
	}
}
