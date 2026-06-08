# SPDX-License-Identifier: Apache-2.0

"""Shipwright AgentField node.

The AF Stack runtime creates durable Shipwright task rows and starts this
AgentField reasoner asynchronously. This process owns the cognitive work:
triage, planning, optional coding-agent harness execution, review, and the
final callback to AF Stack with patch metadata.
"""

from __future__ import annotations

import logging
import os
import re
import shutil
import subprocess
import tempfile
import asyncio
from pathlib import Path
from typing import Literal
from urllib.parse import urlparse

import httpx
from agentfield import Agent, AIConfig
from pydantic import BaseModel, Field

logger = logging.getLogger("shipwright")
logging.basicConfig(
    level=os.getenv("LOG_LEVEL", "INFO").upper(),
    format="%(asctime)s %(levelname)s %(name)s %(message)s",
)


def _select_model() -> str | None:
    override = os.getenv("SHIPWRIGHT_MODEL")
    if override:
        return override
    if os.getenv("OPENROUTER_API_KEY"):
        return "openrouter/google/gemini-2.5-flash"
    if os.getenv("OPENAI_API_KEY"):
        return "openai/gpt-4o-mini"
    if os.getenv("ANTHROPIC_API_KEY"):
        return "anthropic/claude-haiku-4-5-20251001"
    return None


app = Agent(
    node_id=os.getenv("NODE_ID", "shipwright"),
    version=os.getenv("AGENT_VERSION", "0.1.0"),
    ai_config=AIConfig(model=_select_model()) if _select_model() else None,
)


class Triage(BaseModel):
    confident: bool = Field(..., description="Whether the classification is reliable.")
    complexity: Literal["small", "medium", "large"]
    risk: Literal["low", "medium", "high"]
    rationale: str


class ChangePlan(BaseModel):
    confident: bool
    files_to_inspect: list[str] = Field(default_factory=list)
    implementation_steps: list[str] = Field(default_factory=list)
    test_commands: list[str] = Field(default_factory=list)
    branch_name: str


class ExecutionResult(BaseModel):
    confident: bool
    mode: Literal["harness", "ai_patch_sketch"]
    ref: str
    summary: str
    diff_url: str | None = None
    notes: list[str] = Field(default_factory=list)


class ReviewResult(BaseModel):
    confident: bool
    approved: bool
    findings: list[str] = Field(default_factory=list)
    summary: str


class BuildResult(BaseModel):
    task_id: str
    status: Literal["succeeded", "failed"]
    mode: Literal["harness", "ai_patch_sketch"]
    ref: str
    summary: str
    diff_url: str | None = None
    review_findings: list[str] = Field(default_factory=list)


def _harness_binary(provider: str) -> str:
    return {
        "claude-code": "claude",
        "codex": "codex",
        "gemini": "gemini",
        "opencode": "opencode",
    }.get(provider, provider)


def _harness_available(provider: str) -> bool:
    return shutil.which(_harness_binary(provider)) is not None


def _run_git(args: list[str], cwd: Path, env: dict[str, str] | None = None) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["git", *args],
        cwd=cwd,
        env=env,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=True,
        timeout=int(os.getenv("SHIPWRIGHT_GIT_TIMEOUT_S", "120")),
    )


def _safe_branch(raw: str, task_id: str) -> str:
    base = raw.strip() or f"shipwright/{task_id}"
    base = re.sub(r"[^A-Za-z0-9._/-]+", "-", base).strip("-/.")
    if not base.startswith("shipwright/"):
        base = "shipwright/" + base
    return base[:120] or f"shipwright/{task_id}"


def _github_repo(repo_url: str) -> str | None:
    if repo_url.startswith("git@github.com:"):
        path = repo_url.removeprefix("git@github.com:").removesuffix(".git")
        return path if "/" in path else None
    parsed = urlparse(repo_url)
    if parsed.netloc.lower() != "github.com":
        return None
    path = parsed.path.strip("/").removesuffix(".git")
    return path if path.count("/") == 1 else None


def _auth_clone_url(repo_url: str) -> str:
    token = os.getenv("GH_TOKEN", "").strip()
    repo = _github_repo(repo_url)
    if not token or not repo:
        return repo_url
    return f"https://x-access-token:{token}@github.com/{repo}.git"


def _redact_secrets(text: str) -> str:
    token = os.getenv("GH_TOKEN", "").strip()
    if token:
        text = text.replace(token, "***")
    return text


def _clone_repo(repo_url: str, branch: str, root: Path) -> Path:
    dest = root / "repo"
    _run_git(["clone", "--depth", "1", _auth_clone_url(repo_url), str(dest)], cwd=root)
    _run_git(["checkout", "-b", branch], cwd=dest)
    return dest


def _git_diff(repo_dir: Path) -> str:
    # Include newly-created files without staging real content.
    _run_git(["add", "-N", "."], cwd=repo_dir)
    diff = _run_git(["diff", "--binary"], cwd=repo_dir).stdout
    return diff.strip()


def _write_patch(task_id: str, diff: str) -> str:
    patch_dir = Path(os.getenv("SHIPWRIGHT_PATCH_DIR", "/var/lib/shipwright/patches"))
    patch_dir.mkdir(parents=True, exist_ok=True)
    path = patch_dir / f"{task_id}.patch"
    path.write_text(diff + "\n", encoding="utf-8")
    return path.as_uri()


def _commit_and_push(repo_dir: Path, branch: str, title: str) -> None:
    _run_git(["config", "user.email", os.getenv("SHIPWRIGHT_GIT_EMAIL", "shipwright@af-stack.local")], cwd=repo_dir)
    _run_git(["config", "user.name", os.getenv("SHIPWRIGHT_GIT_NAME", "AF Stack Shipwright")], cwd=repo_dir)
    _run_git(["add", "-A"], cwd=repo_dir)
    _run_git(["commit", "-m", f"Shipwright: {title[:60]}"], cwd=repo_dir)
    _run_git(["push", "-u", "origin", branch], cwd=repo_dir)


async def _create_github_pr(repo_url: str, branch: str, title: str, body: str) -> str | None:
    token = os.getenv("GH_TOKEN", "").strip()
    repo = _github_repo(repo_url)
    if not token or not repo:
        return None
    payload = {
        "title": f"Shipwright: {title}",
        "head": branch,
        "base": os.getenv("SHIPWRIGHT_BASE_BRANCH", "main"),
        "body": body,
        "maintainer_can_modify": True,
        "draft": os.getenv("SHIPWRIGHT_DRAFT_PR", "true").lower() != "false",
    }
    headers = {
        "authorization": f"Bearer {token}",
        "accept": "application/vnd.github+json",
        "x-github-api-version": "2022-11-28",
    }
    async with httpx.AsyncClient(timeout=20) as client:
        resp = await client.post(f"https://api.github.com/repos/{repo}/pulls", json=payload, headers=headers)
        if resp.status_code == 201:
            return str(resp.json().get("html_url") or "")
        if resp.status_code == 422:
            existing = await client.get(
                f"https://api.github.com/repos/{repo}/pulls",
                params={"head": f"{repo.split('/')[0]}:{branch}", "state": "open"},
                headers=headers,
            )
            if existing.status_code == 200 and existing.json():
                return str(existing.json()[0].get("html_url") or "")
        logger.warning("github pr create failed: status=%s body=%s", resp.status_code, resp.text[:500])
    return None


def _harness_budget() -> float | None:
    raw = os.getenv("SHIPWRIGHT_HARNESS_BUDGET_USD", "").strip()
    if not raw:
        return None
    try:
        return float(raw)
    except ValueError:
        return None


def _harness_turns() -> int | None:
    raw = os.getenv("SHIPWRIGHT_HARNESS_MAX_TURNS", "").strip()
    if not raw:
        return None
    try:
        return int(raw)
    except ValueError:
        return None


@app.reasoner()
async def triage_task(title: str, description: str, repo_url: str, model: str | None = None) -> dict:
    result = await app.ai(
        system=(
            "You triage software change requests for an autonomous coding agent. "
            "Classify size and delivery risk. Be conservative when repo context is missing."
        ),
        user=f"Title: {title}\nRepo: {repo_url}\n\nDescription:\n{description}",
        schema=Triage,
        model=model,
    )
    if not result.confident:
        result.risk = "high"
        result.rationale = "Low-confidence triage; operator review recommended. " + result.rationale
    return result.model_dump()


@app.reasoner()
async def plan_change(
    title: str,
    description: str,
    repo_url: str,
    triage: dict,
    model: str | None = None,
) -> dict:
    result = await app.ai(
        system=(
            "You are a senior engineer planning a small, reviewable code change. "
            "Return concrete files to inspect, implementation steps, and test commands. "
            "Do not invent repository facts; mark confidence false if context is insufficient."
        ),
        user=(
            f"Title: {title}\nRepo: {repo_url}\nTriage: {triage}\n\n"
            f"Description:\n{description}"
        ),
        schema=ChangePlan,
        model=model,
    )
    if not result.confident:
        result.test_commands = result.test_commands or ["make test"]
    if not result.branch_name:
        result.branch_name = "shipwright/task"
    return result.model_dump()


@app.reasoner()
async def execute_plan(
    task_id: str,
    title: str,
    description: str,
    repo_url: str,
    plan: dict,
    harness_provider: str | None = None,
    model: str | None = None,
) -> dict:
    provider = (harness_provider or os.getenv("SHIPWRIGHT_HARNESS", "codex")).strip()
    branch = _safe_branch(str(plan.get("branch_name") or ""), task_id)
    prompt = (
        "You are Shipwright, an autonomous coding agent. The repository is "
        "already cloned at your working directory. Make the smallest safe "
        "change for this task, run relevant tests, and leave the edited files "
        "in the working tree. Do not create commits; the orchestrator will "
        "capture the diff and create the PR.\n\n"
        f"Task id: {task_id}\nTitle: {title}\nRepo: {repo_url}\n"
        f"Branch: {branch}\nPlan: {plan}\n\nDescription:\n{description}"
    )
    if provider and _harness_available(provider):
        try:
            with tempfile.TemporaryDirectory(prefix=f"shipwright-{task_id}-") as tmp:
                repo_dir = await asyncio.to_thread(_clone_repo, repo_url, branch, Path(tmp))
                raw = await app.harness(
                    prompt=prompt,
                    provider=provider,
                    model=model,
                    cwd=str(repo_dir),
                    max_budget_usd=_harness_budget(),
                    max_turns=_harness_turns(),
                    permission_mode=os.getenv("SHIPWRIGHT_HARNESS_PERMISSION_MODE", "auto"),
                )
                text = getattr(raw, "text", str(raw))
                if getattr(raw, "is_error", False):
                    raise RuntimeError(getattr(raw, "error_message", "") or text)
                diff = await asyncio.to_thread(_git_diff, repo_dir)
                notes = [f"harness_provider={provider}"]
                diff_url = None
                if diff:
                    diff_url = await asyncio.to_thread(_write_patch, task_id, diff)
                    notes.append(f"patch={diff_url}")
                    if os.getenv("GH_TOKEN", "").strip() and _github_repo(repo_url):
                        await asyncio.to_thread(_commit_and_push, repo_dir, branch, title)
                        pr_url = await _create_github_pr(
                            repo_url,
                            branch,
                            title,
                            body=(
                                f"Shipwright task `{task_id}`\n\n"
                                f"{description}\n\n"
                                f"Harness provider: `{provider}`\n\n"
                                f"Summary:\n{text[:4000]}"
                            ),
                        )
                        if pr_url:
                            diff_url = pr_url
                            notes.append(f"github_pr={pr_url}")
                else:
                    notes.append("No working-tree diff was produced by the harness.")
            return ExecutionResult(
                confident=bool(diff),
                mode="harness",
                ref=branch,
                summary=text[:1200],
                diff_url=diff_url,
                notes=notes,
            ).model_dump()
        except Exception as exc:  # pragma: no cover - host harness dependent
            logger.warning("harness failed, falling back to ai patch sketch: %s", _redact_secrets(str(exc)))

    fallback = await app.ai(
        system=(
            "You are producing a patch sketch because no coding harness is available. "
            "Do not claim files were edited. Return the intended branch/ref, a summary, "
            "and notes a human or harness can execute next."
        ),
        user=prompt,
        schema=ExecutionResult,
        model=model,
    )
    fallback.mode = "ai_patch_sketch"
    fallback.ref = fallback.ref or branch
    if not fallback.confident:
        fallback.notes.append("Low-confidence patch sketch; run a real harness before merge.")
    return fallback.model_dump()


@app.reasoner()
async def review_patch(
    title: str,
    description: str,
    plan: dict,
    execution: dict,
    model: str | None = None,
) -> dict:
    result = await app.ai(
        system=(
            "You are reviewing the Shipwright output before AF Stack records completion. "
            "Approve only when the output is coherent, testable, and scoped to the task."
        ),
        user=(
            f"Title: {title}\nDescription:\n{description}\n\n"
            f"Plan:\n{plan}\n\nExecution:\n{execution}"
        ),
        schema=ReviewResult,
        model=model,
    )
    if not result.confident:
        result.approved = False
        result.findings.append("Reviewer confidence was low.")
    return result.model_dump()


async def _callback(task_id: str, result: BuildResult) -> None:
    base = os.getenv("AF_STACK_URL", "").rstrip("/")
    if not base:
        return
    headers = {"content-type": "application/json"}
    api_key = os.getenv("AF_STACK_API_KEY")
    if api_key:
        headers["authorization"] = f"Bearer {api_key}"
    async with httpx.AsyncClient(timeout=10) as client:
        await client.post(
            f"{base}/api/v1/shipwright/tasks/{task_id}/complete",
            json={
                "status": result.status,
                "ref": result.ref,
                "summary": result.summary,
                "diff_url": result.diff_url,
            },
            headers=headers,
        )


@app.reasoner(tags=["entry"])
async def build(
    task_id: str,
    tenant_id: str,
    title: str,
    description: str,
    repo_url: str,
    user_id: str | None = None,
    harness_provider: str | None = None,
    model: str | None = None,
    callback_url: str | None = None,
) -> dict:
    triage = await app.call(
        f"{app.node_id}.triage_task",
        title=title,
        description=description,
        repo_url=repo_url,
        model=model,
    )
    plan = await app.call(
        f"{app.node_id}.plan_change",
        title=title,
        description=description,
        repo_url=repo_url,
        triage=triage,
        model=model,
    )
    execution = await app.call(
        f"{app.node_id}.execute_plan",
        task_id=task_id,
        title=title,
        description=description,
        repo_url=repo_url,
        plan=plan,
        harness_provider=harness_provider,
        model=model,
    )
    review = await app.call(
        f"{app.node_id}.review_patch",
        title=title,
        description=description,
        plan=plan,
        execution=execution,
        model=model,
    )

    approved = bool(review.get("approved"))
    status: Literal["succeeded", "failed"] = "succeeded" if approved else "failed"
    result = BuildResult(
        task_id=task_id,
        status=status,
        mode=execution.get("mode", "ai_patch_sketch"),
        ref=execution.get("ref") or f"shipwright/{task_id}",
        summary=execution.get("summary") or review.get("summary") or "Shipwright completed.",
        diff_url=execution.get("diff_url"),
        review_findings=list(review.get("findings") or []),
    )
    await _callback(task_id, result)
    return result.model_dump()


if __name__ == "__main__":
    app.run()
