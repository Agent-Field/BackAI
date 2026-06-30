// SPDX-License-Identifier: Apache-2.0

package initcmd

import (
	"fmt"
	"os"
	"path/filepath"
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

// summariseTemplateScaffold renders the post-scaffold CLI lines for a
// template run. Returns "" for templates that scaffold nothing.
func summariseTemplateScaffold(template string, created []string, skippedExisting bool) string {
	if template != TemplateCodingAgent {
		return ""
	}
	var b strings.Builder
	if len(created) == 0 && skippedExisting {
		b.WriteString(fmt.Sprintf("- coding-agent already present at %s (left unchanged)\n", filepath.ToSlash(codingAgentDir)))
		return b.String()
	}
	b.WriteString(fmt.Sprintf("- scaffolded coding agent at %s (node_id %q)\n", filepath.ToSlash(codingAgentDir), codingAgentNodeID))
	if skippedExisting {
		b.WriteString("  (some files already existed and were left unchanged)\n")
	}
	b.WriteString("- next: store a GitHub token in the GH_TOKEN secret slot, then `af-stack dev`\n")
	return b.String()
}
