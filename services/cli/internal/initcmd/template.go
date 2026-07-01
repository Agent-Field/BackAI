// SPDX-License-Identifier: Apache-2.0

package initcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Template names accepted by `af-stack init --template`.
const (
	// TemplateNode is the minimal option: rebrand the fork in place and
	// leave the default sample agent. This is the historical behaviour
	// and the default when --template is omitted.
	TemplateNode = "node"

	// TemplateCodingAgent is the hero template: rebrand + scaffold a
	// canonical `coding-agent` AgentField agent (the seam H3 fills with a
	// real clone+harness+PR flow) fed its GitHub credential from a
	// GH_TOKEN secret slot.
	TemplateCodingAgent = "coding-agent"
)

// knownTemplates is the registry of valid --template values, in display
// order. Keep TemplateNode first so it reads as the default.
var knownTemplates = []string{TemplateNode, TemplateCodingAgent}

// validTemplate reports whether name is a known template.
func validTemplate(name string) bool {
	for _, t := range knownTemplates {
		if t == name {
			return true
		}
	}
	return false
}

// codingAgentNodeID is the single canonical node_id for the scaffolded
// coding agent — fixed (not the brand slug) so the runtime route, the
// compose service, and the agent registration all resolve to the same
// name (the node_id reconciliation H3 depends on).
const codingAgentNodeID = "coding-agent"

// codingAgentDir is the scaffold target, relative to the repo root.
var codingAgentDir = filepath.Join("apps", "backend", "agents", codingAgentNodeID)

// scaffoldCodingAgent writes the coding-agent files under root. Existing
// files are left untouched (so re-running init never clobbers edits);
// the returned slice lists the relative paths actually created. The bool
// reports whether any file already existed and was skipped.
func scaffoldCodingAgent(root string) (created []string, skippedExisting bool, err error) {
	files := map[string]string{
		"main.py":          codingAgentMainPy(codingAgentNodeID),
		"Dockerfile":       codingAgentDockerfile(codingAgentNodeID),
		"requirements.txt": codingAgentRequirements(),
		"README.md":        codingAgentReadme(codingAgentNodeID),
	}
	dir := filepath.Join(root, codingAgentDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, false, fmt.Errorf("init: create %s: %w", codingAgentDir, err)
	}
	// Deterministic order so the CLI summary + tests are stable.
	for _, name := range []string{"main.py", "Dockerfile", "requirements.txt", "README.md"} {
		rel := filepath.ToSlash(filepath.Join(codingAgentDir, name))
		dest := filepath.Join(dir, name)
		if exists(dest) {
			skippedExisting = true
			continue
		}
		// #nosec G306 -- scaffolded project source files (main.py, Dockerfile,
		// README) are meant to be world-readable like any checked-in source.
		if err := os.WriteFile(dest, []byte(files[name]), 0o644); err != nil {
			return nil, skippedExisting, fmt.Errorf("init: write %s: %w", rel, err)
		}
		created = append(created, rel)
	}
	return created, skippedExisting, nil
}

// codingAgentMainPy is an honest coding-agent skeleton: it registers
// under the canonical node_id, validates a {task, repo_url} payload, and
// reads its GitHub credential from the GH_TOKEN env (populated from the
// per-tenant secret slot). The clone → harness → push → open-PR step is
// a single clearly-marked seam that H3 fills with the real SWE flow — it
// is NOT a fake-sleep/hardcoded-diff stub; the unfilled step returns a
// structured, explicit "not yet implemented" rather than fabricating an
// artifact.
func codingAgentMainPy(nodeID string) string {
	return fmt.Sprintf(`"""Coding agent — clones a repo, runs a coding harness, opens a PR.

Scaffolded by 'af-stack init --template coding-agent'. This is the hero
template's agent: it registers with AgentField as %[1]q and exposes a single
'run' reasoner that takes a task description and a target repo.

The GitHub credential arrives via the GH_TOKEN environment variable, which
the runtime populates from the tenant's secret slot (store it with
'af-stack mcp add ... --env GITHUB_TOKEN=secret:github_token' or the secrets
API). It is never hardcoded here.

The clone -> harness -> push -> open-PR body is the one seam left for you to
fill (see run_coding_task below). Until then the reasoner validates its
inputs and returns an explicit, structured "not yet implemented" — it does
not fabricate a diff or a PR URL.
"""

from __future__ import annotations

import os
from typing import Any

from agentfield import Agent
from pydantic import BaseModel, Field


app = Agent(
    node_id=os.getenv("NODE_ID", %[1]q),
    version=os.getenv("AGENT_VERSION", "0.1.0"),
)


class CodingTask(BaseModel):
    """Input contract for the coding agent's run reasoner."""

    task: str = Field(..., min_length=1, description="What to change, in plain English.")
    repo_url: str = Field(..., min_length=1, description="Git URL of the repo to work on.")
    base_branch: str = Field("main", description="Branch to base the PR on.")


def github_token() -> str:
    """The repo credential, sourced from the GH_TOKEN secret slot."""
    return os.getenv("GH_TOKEN", "").strip()


async def run_coding_task(task: CodingTask, token: str) -> dict[str, Any]:
    """Clone, run a harness, push a branch, open a PR. (H3 fills this in.)

    Replace this body with the real flow: shallow-clone repo_url using the
    token, run a coding harness (claude-code / codex / gemini) inside the
    runtime sandbox, commit the diff to a fresh branch, push, and open a
    pull request via the GitHub API. Return {"pr_url": ..., "branch": ...}.
    """
    raise NotImplementedError(
        "coding harness not wired yet — implement run_coding_task "
        "(clone -> harness -> push -> open PR)"
    )


@app.reasoner(tags=["coding", "swe"])
async def run(payload: dict[str, Any]) -> dict[str, Any]:
    try:
        spec = CodingTask.model_validate(payload)
    except Exception as exc:  # noqa: BLE001 — surface validation errors to the caller
        return {"status": "invalid_request", "error": str(exc)}

    token = github_token()
    if not token:
        return {
            "status": "missing_credential",
            "error": "GH_TOKEN is not set; store a GitHub token in the tenant secret slot",
        }

    try:
        result = await run_coding_task(spec, token)
    except NotImplementedError as exc:
        return {
            "status": "not_implemented",
            "task": spec.task,
            "repo_url": spec.repo_url,
            "detail": str(exc),
        }
    return {"status": "ok", **result}


if __name__ == "__main__":
    app.run()
`, nodeID)
}

func codingAgentDockerfile(nodeID string) string {
	return fmt.Sprintf(`FROM python:3.12-slim

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_NO_CACHE_DIR=1

WORKDIR /app
COPY requirements.txt ./
RUN pip install -r requirements.txt
COPY main.py ./

ENV NODE_ID=%s \
    PORT=8090

EXPOSE 8090
CMD ["python", "main.py"]
`, nodeID)
}

func codingAgentRequirements() string {
	// Floor-pinned so a flexible constraint still busts the Docker layer
	// cache when the SDK is bumped.
	return "agentfield>=0.4.0\npydantic>=2.0\n"
}

func codingAgentReadme(nodeID string) string {
	return fmt.Sprintf("# %s\n\n"+
		"The hero coding agent, scaffolded by `af-stack init --template coding-agent`.\n\n"+
		"It registers with AgentField as `%[1]s` and is reached at:\n\n"+
		"```\nPOST /api/v1/agents/%[1]s.run\n{ \"task\": \"fix the failing test\", \"repo_url\": \"https://github.com/you/repo\" }\n```\n\n"+
		"## GitHub credential (GH_TOKEN secret slot)\n\n"+
		"The agent reads its repo credential from `GH_TOKEN`, populated from the\n"+
		"tenant's encrypted secret slot — never hardcoded. Store a token with:\n\n"+
		"```\naf-stack mcp add github --transport stdio \\\n"+
		"  --command \"uvx mcp-server-github\" --env GITHUB_TOKEN=secret:github_token\n```\n\n"+
		"## Filling in the harness\n\n"+
		"`main.py`'s `run_coding_task` is the one seam left to implement: clone the\n"+
		"repo, run a coding harness in the sandbox, push a branch, and open a PR.\n"+
		"Until then `run` returns a structured `not_implemented` rather than a fake\n"+
		"diff.\n\n"+
		"`af-stack dev` brings the agent up alongside the rest of the stack.\n",
		nodeID)
}

// codingAgentComposeService is the compose service block for the coding
// agent, mirroring the shape of the other default-stack agents
// (supportdesk-agent). It carries NO `profiles:` key, so `af-stack dev`
// (plain `docker compose up`) brings it up — the "agent reachable, no
// hand-wiring" half of the H1 contract. Indented two spaces to sit as a
// sibling under the top-level `services:` map.
const codingAgentComposeService = `  coding-agent:
    build:
      context: apps/backend/agents/coding-agent
      dockerfile: Dockerfile
    environment:
      AGENTFIELD_SERVER: http://agentfield:8080
      NODE_ID: coding-agent
      AGENT_CALLBACK_URL: http://coding-agent:8090
      GH_TOKEN: ${GH_TOKEN:-}
      ANTHROPIC_API_KEY: ${ANTHROPIC_API_KEY:-}
      OPENAI_API_KEY: ${OPENAI_API_KEY:-}
      OPENROUTER_API_KEY: ${OPENROUTER_API_KEY:-}
    depends_on:
      agentfield:
        condition: service_started
    restart: unless-stopped
`

var (
	// codingAgentServiceRE detects an already-present coding-agent
	// service (idempotency guard). Two-space indent = a service key.
	codingAgentServiceRE = regexp.MustCompile(`(?m)^  coding-agent:[ \t]*$`)
	// servicesKeyRE matches the top-level services: line including its
	// trailing newline, so the insertion point is the first service line.
	servicesKeyRE = regexp.MustCompile(`(?m)^services:[ \t]*\r?\n`)
)

// wireCodingAgentCompose injects the coding-agent service into
// docker-compose.yml at root, immediately after the top-level services:
// key. Idempotent: returns (false, nil) when a coding-agent service is
// already present or there is no compose file. Returns (true, nil) when
// it writes the service. Textual insertion (not a yaml round-trip) keeps
// the operator's comments, anchors, and formatting intact.
func wireCodingAgentCompose(root string) (wired bool, err error) {
	path := filepath.Join(root, "docker-compose.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("init: read docker-compose.yml: %w", err)
	}
	content := string(raw)
	if codingAgentServiceRE.MatchString(content) {
		return false, nil // already wired
	}
	loc := servicesKeyRE.FindStringIndex(content)
	if loc == nil {
		return false, fmt.Errorf("init: docker-compose.yml has no top-level services: key to wire coding-agent into")
	}
	insertAt := loc[1]
	updated := content[:insertAt] + codingAgentComposeService + content[insertAt:]
	// #nosec G306 -- docker-compose.yml is world-readable project source.
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return false, fmt.Errorf("init: update docker-compose.yml: %w", err)
	}
	return true, nil
}

// mtEnvKeyRE matches an existing (active or commented-out is ignored —
// this requires a real assignment) multi-tenancy flag in a .env file.
var mtEnvKeyRE = regexp.MustCompile(`(?m)^\s*AF_STACK_MODULE_MULTI_TENANCY\s*=`)

// heroEnvBlock turns multi-tenancy on for the scaffolded app so
// per-tenant API-key minting and isolation work on the first
// `af-stack dev` (H1's "multi-tenancy ON"). The GH_TOKEN secret slot is
// already carried through docker-compose.yml + documented in
// .env.example, so it isn't duplicated here.
const heroEnvBlock = "\n# Hero flow (coding-agent template): multi-tenancy on so per-tenant\n" +
	"# API-key minting + isolation work out of the box. Set false for single-tenant.\n" +
	"AF_STACK_MODULE_MULTI_TENANCY=true\n"

// ensureHeroEnv makes the scaffolded fork's .env enable multi-tenancy,
// without ever overriding an explicit operator setting. Returns the
// action taken: "created" (no .env existed — seeded from .env.example
// when present), "appended" (an .env existed but had no MT flag),
// or "present" (an MT flag already existed and was left untouched).
func ensureHeroEnv(root string) (string, error) {
	path := filepath.Join(root, ".env")
	if exists(path) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("init: read .env: %w", err)
		}
		if mtEnvKeyRE.Match(raw) {
			return "present", nil // respect the operator's explicit choice
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return "", fmt.Errorf("init: open .env: %w", err)
		}
		defer f.Close()
		if _, err := f.WriteString(heroEnvBlock); err != nil {
			return "", fmt.Errorf("init: append .env: %w", err)
		}
		return "appended", nil
	}
	// No .env yet — seed from .env.example (which carries the full var set
	// with sane defaults) when present, then ensure the MT flag.
	var content string
	examplePath := filepath.Join(root, ".env.example")
	if exists(examplePath) {
		b, err := os.ReadFile(examplePath)
		if err != nil {
			return "", fmt.Errorf("init: read .env.example: %w", err)
		}
		content = string(b)
	}
	if !mtEnvKeyRE.MatchString(content) {
		content += heroEnvBlock
	}
	// #nosec G306 -- .env is world-readable project scaffolding (no real
	// secrets; the hero placeholders are filled in by the user afterwards).
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("init: write .env: %w", err)
	}
	return "created", nil
}

// summariseCodingAgentScaffold renders the post-scaffold CLI lines for
// the coding-agent template run.
func summariseCodingAgentScaffold(created []string, skippedExisting, wired bool, envAction string) string {
	var b strings.Builder
	if len(created) == 0 && skippedExisting {
		b.WriteString(fmt.Sprintf("- coding-agent already present at %s (left unchanged)\n", filepath.ToSlash(codingAgentDir)))
	} else {
		b.WriteString(fmt.Sprintf("- scaffolded coding agent at %s (node_id %q)\n", filepath.ToSlash(codingAgentDir), codingAgentNodeID))
		if skippedExisting {
			b.WriteString("  (some files already existed and were left unchanged)\n")
		}
	}
	if wired {
		b.WriteString("- wired coding-agent into docker-compose.yml (default stack)\n")
	} else {
		b.WriteString("- coding-agent already present in docker-compose.yml (left unchanged)\n")
	}
	switch envAction {
	case "created":
		b.WriteString("- created .env with multi-tenancy on (per-tenant key minting works)\n")
	case "appended":
		b.WriteString("- enabled multi-tenancy in .env (per-tenant key minting works)\n")
	case "present":
		b.WriteString("- .env already sets AF_STACK_MODULE_MULTI_TENANCY (left unchanged)\n")
	}
	b.WriteString("- next: store a GitHub token in the GH_TOKEN secret slot, then `af-stack dev`\n")
	return b.String()
}
