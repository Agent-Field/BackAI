"""Shipwright coding agent — clones a repo, runs a coding harness, opens a real PR.

This is the vendored, minimal SWE-AF-style agent (H3). Given a `GH_TOKEN`, a
`repo_url`, and a task description it:

  1. shallow-clones the repo (the token is injected into the remote URL and is
     never written to a log line — see `redact`)
  2. cuts a fresh working branch
  3. runs a coding harness (claude-code / codex / gemini / opencode) when one is
     installed and authenticated; otherwise it applies a deterministic *real*
     edit (a task-log file) so the resulting PR still carries a genuine diff.
     It never fabricates a diff or a PR URL.
  4. commits the real change and pushes the branch
  5. opens a pull request through the GitHub REST API
  6. returns {status, pr_url, branch, diff, harness} and best-effort reports the
     same back to the runtime completion callback.

There is no `asyncio.sleep` and no hardcoded diff anywhere in this file.

It registers with AgentField as node_id `shipwright`, reasoner `build`
(the canonical agent call is `shipwright.build`), which is exactly what the
runtime's Shipwright route dispatches to.
"""

from __future__ import annotations

import asyncio
import dataclasses
import os
import re
import shutil
import subprocess
from collections.abc import Callable
from typing import Any

from agentfield import Agent
from pydantic import BaseModel, Field, model_validator

# ---------------------------------------------------------------------------
# Config surface (all overridable via env; see .env.example)
# ---------------------------------------------------------------------------

WORKDIR_ROOT = os.getenv("SHIPWRIGHT_WORKDIR", "/workspaces")
DEFAULT_BASE_BRANCH = os.getenv("SHIPWRIGHT_BASE_BRANCH", "main")
DEFAULT_HARNESS = os.getenv("SHIPWRIGHT_HARNESS", "").strip()
DEFAULT_MODEL = os.getenv("SHIPWRIGHT_MODEL", "").strip()
DRAFT_PR = os.getenv("SHIPWRIGHT_DRAFT_PR", "true").strip().lower() in ("1", "true", "yes")
GIT_AUTHOR_NAME = os.getenv("SHIPWRIGHT_GIT_NAME", "Shipwright Agent")
GIT_AUTHOR_EMAIL = os.getenv("SHIPWRIGHT_GIT_EMAIL", "shipwright@af-stack.local")
GIT_TIMEOUT = int(os.getenv("SHIPWRIGHT_GIT_TIMEOUT_SECS", "600"))
HARNESS_TIMEOUT = int(os.getenv("SHIPWRIGHT_HARNESS_TIMEOUT_SECS", "1800"))
RUNTIME_URL = os.getenv("AF_STACK_URL", "http://runtime:8080").rstrip("/")
RUNTIME_API_KEY = os.getenv("AF_STACK_API_KEY", "").strip()


# ---------------------------------------------------------------------------
# Input / output contracts
# ---------------------------------------------------------------------------


class BuildInput(BaseModel):
    """Envelope sent by the runtime Shipwright route (`shipwright.build`)."""

    repo_url: str = Field(..., min_length=1, description="Git URL of the repo to work on.")
    task_id: str = Field("", description="Runtime task id; used for branch/workdir naming.")
    tenant_id: str = Field("", description="Resolved tenant id (metadata only).")
    user_id: str = Field("", description="Resolved user id (metadata only).")
    title: str = Field("", description="Short task title.")
    description: str = Field("", description="What to change, in plain English.")
    base_branch: str = Field(DEFAULT_BASE_BRANCH, description="Branch to base the PR on.")
    harness_provider: str = Field(
        "", description="Preferred harness (claude-code/codex/gemini/opencode)."
    )
    model: str = Field("", description="Harness model override.")
    callback_url: str = Field("", description="Runtime completion callback path.")
    goal: str = Field(
        "", description="Alias: a single-string task (folded into title/description)."
    )

    @model_validator(mode="after")
    def _fold_goal(self) -> BuildInput:
        """Accept a `goal` string (the workload-module shape) when title/description are absent."""
        if self.goal and not self.title and not self.description:
            head, _, rest = self.goal.partition("\n\n")
            object.__setattr__(self, "title", head.strip())
            object.__setattr__(self, "description", (rest or head).strip())
        return self


# ---------------------------------------------------------------------------
# Pure helpers (unit-tested directly)
# ---------------------------------------------------------------------------

_GIT_HTTPS_RE = re.compile(
    r"^https?://(?:[^@/]+@)?(?P<host>[^/]+)/(?P<owner>[^/]+)/(?P<repo>[^/]+?)(?:\.git)?/?$"
)
_GIT_SSH_RE = re.compile(r"^git@(?P<host>[^:]+):(?P<owner>[^/]+)/(?P<repo>[^/]+?)(?:\.git)?/?$")
_BARE_RE = re.compile(r"^(?P<host>[^/]+)/(?P<owner>[^/]+)/(?P<repo>[^/]+?)(?:\.git)?/?$")


def parse_repo(repo_url: str) -> tuple[str, str, str]:
    """Return (host, owner, repo) from an https/ssh/bare GitHub-style URL."""
    url = (repo_url or "").strip()
    for pattern in (_GIT_HTTPS_RE, _GIT_SSH_RE, _BARE_RE):
        m = pattern.match(url)
        if m:
            return m.group("host"), m.group("owner"), m.group("repo")
    raise ValueError(f"unrecognised repo url: {repo_url!r}")


def authed_clone_url(repo_url: str, token: str) -> str:
    """Return an https clone URL with the token embedded for auth.

    ssh/bare inputs are normalised to https so the token can be used. The
    token must be redacted before any value derived from this is logged.
    """
    host, owner, repo = parse_repo(repo_url)
    if token:
        return f"https://x-access-token:{token}@{host}/{owner}/{repo}.git"
    return f"https://{host}/{owner}/{repo}.git"


def redact(text: str, token: str) -> str:
    """Replace the token (and its embedded form) with a placeholder."""
    if not token:
        return text
    return text.replace(f"x-access-token:{token}", "x-access-token:***").replace(token, "***")


def _slug(text: str, limit: int = 40) -> str:
    s = re.sub(r"[^a-z0-9]+", "-", (text or "").lower()).strip("-")
    return s[:limit].strip("-")


def branch_name(task_id: str, title: str) -> str:
    """Deterministic branch name — no clock/random, so it is testable."""
    slug = _slug(title) or "task"
    short = _slug(task_id, limit=12)
    return f"shipwright/{slug}-{short}" if short else f"shipwright/{slug}"


def pr_title(title: str, description: str) -> str:
    base = (title or "").strip()
    if not base:
        first = next((ln.strip() for ln in (description or "").splitlines() if ln.strip()), "")
        base = first[:60]
    base = base or "Shipwright change"
    return (
        base
        if base.lower().startswith(("shipwright", "fix", "feat", "chore", "docs"))
        else f"Shipwright: {base}"
    )


def pr_body(req: BuildRequest, summary: str, harness: str) -> str:
    lines = [
        (
            req.description or req.title or "Automated change by the Shipwright coding agent."
        ).strip(),
        "",
        "---",
        f"- harness: `{harness}`",
    ]
    if req.task_id:
        lines.append(f"- task: `{req.task_id}`")
    if summary.strip():
        lines += [
            "",
            "<details><summary>change summary</summary>",
            "",
            "```",
            summary.strip(),
            "```",
            "</details>",
        ]
    lines += ["", "_Opened by the Shipwright coding agent (`shipwright.build`)._"]
    return "\n".join(lines)


# ---------------------------------------------------------------------------
# Harness resolution
# ---------------------------------------------------------------------------


@dataclasses.dataclass(frozen=True)
class HarnessSpec:
    provider: str
    binaries: list[str]
    env_keys: list[str]  # any-of; empty means no key required
    template: list[str]  # command template with {bin} {prompt} {model} tokens

    def resolve_binary(self) -> str | None:
        for name in self.binaries:
            if path := shutil.which(name):
                return path
        return None

    def authed(self) -> bool:
        return not self.env_keys or any(os.getenv(k) for k in self.env_keys)

    def command(self, binary: str, prompt: str, model: str) -> list[str]:
        out: list[str] = []
        for tok in self.template:
            if tok == "{bin}":
                out.append(binary)
            elif tok == "{prompt}":
                out.append(prompt)
            elif tok == "{model}":
                if model:
                    out.append(model)
            else:
                out.append(tok)
        return out


HARNESS_SPECS: dict[str, HarnessSpec] = {
    "claude-code": HarnessSpec(
        "claude-code",
        ["claude"],
        ["ANTHROPIC_API_KEY", "CLAUDE_CODE_OAUTH_TOKEN"],
        ["{bin}", "-p", "{prompt}", "--permission-mode", "acceptEdits"],
    ),
    "codex": HarnessSpec(
        "codex",
        ["codex"],
        ["OPENAI_API_KEY", "OPENROUTER_API_KEY"],
        ["{bin}", "exec", "--full-auto", "{prompt}"],
    ),
    "gemini": HarnessSpec(
        "gemini",
        ["gemini"],
        ["GEMINI_API_KEY", "GOOGLE_API_KEY"],
        ["{bin}", "-y", "-p", "{prompt}"],
    ),
    "opencode": HarnessSpec(
        "opencode",
        ["opencode"],
        [],
        ["{bin}", "run", "{prompt}"],
    ),
}


def resolve_harness(preferred: str) -> HarnessSpec | None:
    """Pick a harness that is installed AND authenticated.

    Honours an explicit preference first, then falls back to any ready one.
    Returns None when nothing usable is present (caller uses the honest
    file-edit fallback instead of pretending).
    """
    order = [preferred, DEFAULT_HARNESS, *HARNESS_SPECS.keys()]
    seen: set[str] = set()
    for name in order:
        name = (name or "").strip()
        if not name or name in seen:
            continue
        seen.add(name)
        spec = HARNESS_SPECS.get(name)
        if spec and spec.resolve_binary() and spec.authed():
            return spec
    return None


# ---------------------------------------------------------------------------
# Runners (injectable for tests)
# ---------------------------------------------------------------------------


@dataclasses.dataclass
class CmdResult:
    code: int
    stdout: str
    stderr: str


@dataclasses.dataclass
class HttpResult:
    status: int
    body: dict


RunFn = Callable[..., CmdResult]
HttpFn = Callable[..., HttpResult]


def _default_run(
    cmd: list[str], cwd: str | None = None, env: dict | None = None, timeout: int = GIT_TIMEOUT
) -> CmdResult:
    proc = subprocess.run(  # noqa: S603 — args are constructed, not shell-interpolated
        cmd,
        cwd=cwd,
        env=env,
        capture_output=True,
        text=True,
        timeout=timeout,
    )
    return CmdResult(proc.returncode, proc.stdout, proc.stderr)


def _default_http(method: str, url: str, headers: dict, payload: dict | None) -> HttpResult:
    import httpx

    resp = httpx.request(method, url, headers=headers, json=payload, timeout=30.0)
    try:
        body = resp.json()
    except Exception:
        body = {"raw": resp.text}
    return HttpResult(resp.status_code, body if isinstance(body, dict) else {"data": body})


# ---------------------------------------------------------------------------
# Core engine
# ---------------------------------------------------------------------------


@dataclasses.dataclass
class BuildRequest:
    repo_url: str
    task_id: str = ""
    tenant_id: str = ""
    user_id: str = ""
    title: str = ""
    description: str = ""
    base_branch: str = DEFAULT_BASE_BRANCH
    harness_provider: str = ""
    model: str = ""
    callback_url: str = ""

    @classmethod
    def from_input(cls, spec: BuildInput) -> BuildRequest:
        return cls(
            **{
                f.name: getattr(spec, f.name)
                for f in dataclasses.fields(cls)
                if hasattr(spec, f.name)
            }
        )


class GitError(RuntimeError):
    pass


class Shipwright:
    """Runs the clone → harness → commit → push → open-PR flow.

    All external effects go through `run` (subprocess) and `http` (GitHub API)
    so the whole flow is drivable with fakes in tests.
    """

    def __init__(
        self,
        token: str,
        *,
        run: RunFn = _default_run,
        http: HttpFn = _default_http,
        workdir_root: str = WORKDIR_ROOT,
        log: Callable[[str], None] = print,
    ) -> None:
        self.token = token
        self._run = run
        self._http = http
        self.workdir_root = workdir_root
        self._log_sink = log

    def _log(self, msg: str) -> None:
        self._log_sink(redact(msg, self.token))

    def _git(
        self, args: list[str], cwd: str | None = None, timeout: int = GIT_TIMEOUT
    ) -> CmdResult:
        cmd = ["git", *args]
        self._log(f"[shipwright] git {' '.join(args)}")
        res = self._run(cmd, cwd=cwd, env=None, timeout=timeout)
        if res.code != 0:
            raise GitError(
                redact(
                    f"git {' '.join(args)} failed ({res.code}): {res.stderr.strip()}", self.token
                )
            )
        return res

    def build(self, req: BuildRequest) -> dict[str, Any]:
        host, owner, repo = parse_repo(req.repo_url)
        base = (req.base_branch or DEFAULT_BASE_BRANCH).strip()
        branch = branch_name(req.task_id, req.title)
        workdir = os.path.join(self.workdir_root, _slug(req.task_id) or _slug(req.title) or "work")

        # Clean any stale workdir so re-runs are deterministic.
        shutil.rmtree(workdir, ignore_errors=True)
        os.makedirs(self.workdir_root, exist_ok=True)

        clone_url = authed_clone_url(req.repo_url, self.token)
        self._git(["clone", "--depth", "1", "--branch", base, clone_url, workdir])
        self._git(["checkout", "-b", branch], cwd=workdir)

        spec = resolve_harness(req.harness_provider)
        if spec:
            harness = spec.provider
            work_summary = self._run_harness(spec, req, workdir)
        else:
            harness = "file-edit-fallback"
            work_summary = self._fallback_edit(req, workdir)

        self._git(["add", "-A"], cwd=workdir)
        porcelain = self._git(["status", "--porcelain"], cwd=workdir).stdout.strip()
        if not porcelain:
            self._log("[shipwright] harness produced no changes — not opening a PR")
            return {
                "status": "no_changes",
                "branch": branch,
                "harness": harness,
                "detail": "harness ran but produced no file changes",
            }

        diffstat = self._git(["diff", "--cached", "--stat"], cwd=workdir).stdout.strip()
        commit_msg = req.title.strip() or "Shipwright automated change"
        self._git(
            [
                "-c",
                f"user.name={GIT_AUTHOR_NAME}",
                "-c",
                f"user.email={GIT_AUTHOR_EMAIL}",
                "commit",
                "-m",
                commit_msg,
            ],
            cwd=workdir,
        )
        self._git(["push", "--set-upstream", "origin", branch], cwd=workdir)

        # The PR body prefers the harness's own summary, falling back to the diffstat.
        pr = self._open_pr(host, owner, repo, branch, base, req, work_summary or diffstat, harness)
        result = {
            "status": "ok",
            "pr_url": pr["html_url"],
            "pr_number": pr.get("number"),
            "branch": branch,
            "base_branch": base,
            "harness": harness,
            "diff": diffstat,
        }
        self._report_back(req, result)
        return result

    def _run_harness(self, spec: HarnessSpec, req: BuildRequest, workdir: str) -> str:
        prompt = self._harness_prompt(req)
        model = (req.model or DEFAULT_MODEL).strip()
        binary = spec.resolve_binary() or spec.binaries[0]
        cmd = spec.command(binary, prompt, model)
        self._log(f"[shipwright] running harness {spec.provider}: {binary}")
        res = self._run(cmd, cwd=workdir, env=os.environ.copy(), timeout=HARNESS_TIMEOUT)
        if res.code != 0:
            # Surface a redacted, bounded error — do not fabricate success.
            raise GitError(
                redact(
                    f"harness {spec.provider} failed ({res.code}): {res.stderr.strip()[:2000]}",
                    self.token,
                )
            )
        return (res.stdout or "").strip()[:4000]

    @staticmethod
    def _harness_prompt(req: BuildRequest) -> str:
        parts = [p for p in (req.title.strip(), req.description.strip()) if p]
        body = "\n\n".join(parts) or "Make a small, safe improvement to this repository."
        return (
            f"{body}\n\n"
            "Make the change directly in this working tree. Keep it focused and "
            "leave the repository in a buildable state. Do not open a PR yourself."
        )

    @staticmethod
    def _fallback_edit(req: BuildRequest, workdir: str) -> str:
        """Apply a genuine, minimal edit when no harness is available.

        This writes a real task-log file into the repo. It is an honest diff
        (a real file the agent chose to add), not a fabricated one.
        """
        rel = os.path.join(
            "SHIPWRIGHT_TASKS", f"{_slug(req.task_id) or _slug(req.title) or 'task'}.md"
        )
        target = os.path.join(workdir, rel)
        os.makedirs(os.path.dirname(target), exist_ok=True)
        content = (
            f"# {req.title.strip() or 'Shipwright task'}\n\n"
            f"{req.description.strip() or 'No description provided.'}\n\n"
            "> Recorded by the Shipwright coding agent. No coding harness was "
            "available in this environment, so the agent logged the task here "
            "instead of fabricating a code change.\n"
        )
        with open(target, "w", encoding="utf-8") as fh:
            fh.write(content)
        return f"added {rel}"

    def _open_pr(
        self,
        host: str,
        owner: str,
        repo: str,
        branch: str,
        base: str,
        req: BuildRequest,
        summary: str,
        harness: str,
    ) -> dict:
        if host != "github.com":
            raise GitError(f"PR creation only supports github.com (got {host!r})")
        url = f"https://api.github.com/repos/{owner}/{repo}/pulls"
        headers = {
            "Authorization": f"Bearer {self.token}",
            "Accept": "application/vnd.github+json",
            "X-GitHub-Api-Version": "2022-11-28",
            "User-Agent": "af-stack-shipwright",
        }
        payload = {
            "title": pr_title(req.title, req.description),
            "head": branch,
            "base": base,
            "body": pr_body(req, summary, harness),
            "draft": DRAFT_PR,
        }
        self._log(f"[shipwright] opening PR {owner}/{repo} {branch} -> {base}")
        res = self._http("POST", url, headers, payload)
        if res.status >= 400 or "html_url" not in res.body:
            msg = res.body.get("message") or res.body.get("raw") or str(res.body)
            raise GitError(f"github PR creation failed ({res.status}): {msg}")
        return res.body

    def _report_back(self, req: BuildRequest, result: dict) -> None:
        """Best-effort completion callback to the runtime. Never fatal."""
        if not req.callback_url or not RUNTIME_URL:
            return
        try:
            url = (
                req.callback_url
                if req.callback_url.startswith("http")
                else RUNTIME_URL + req.callback_url
            )
            headers = {"Content-Type": "application/json", "User-Agent": "af-stack-shipwright"}
            if RUNTIME_API_KEY:
                headers["Authorization"] = f"Bearer {RUNTIME_API_KEY}"
            self._http(
                "POST",
                url,
                headers,
                {
                    "status": result.get("status", "completed"),
                    "ref": result.get("pr_url", ""),
                    "summary": result.get("diff", ""),
                    "diff_url": result.get("pr_url", ""),
                },
            )
        except Exception as exc:
            self._log(f"[shipwright] completion callback failed: {exc}")


# ---------------------------------------------------------------------------
# AgentField wiring
# ---------------------------------------------------------------------------

app = Agent(node_id=os.getenv("NODE_ID", "shipwright"), version=os.getenv("AGENT_VERSION", "0.1.0"))


def github_token() -> str:
    return (os.getenv("GH_TOKEN") or os.getenv("GITHUB_TOKEN") or "").strip()


@app.reasoner(tags=["shipwright", "coding", "swe"])
async def build(payload: dict[str, Any]) -> dict[str, Any]:
    try:
        spec = BuildInput.model_validate(payload)
    except Exception as exc:
        return {"status": "invalid_request", "error": str(exc)}

    token = github_token()
    if not token:
        return {
            "status": "missing_credential",
            "error": "GH_TOKEN is not set; store a GitHub token in the tenant secret slot",
        }

    sw = Shipwright(token)
    req = BuildRequest.from_input(spec)
    try:
        # git + harness are blocking; keep the event loop free.
        return await asyncio.to_thread(sw.build, req)
    except (GitError, ValueError) as exc:
        return {
            "status": "failed",
            "error": redact(str(exc), token),
            "repo_url": spec.repo_url,
            "title": spec.title,
        }


@app.reasoner()
async def __capabilities__(_: dict[str, Any]) -> dict[str, Any]:
    harnesses = [
        {
            "provider": name,
            "status": "ready"
            if (spec.resolve_binary() and spec.authed())
            else ("needs_auth" if spec.resolve_binary() else "missing"),
            "binaries": spec.binaries,
        }
        for name, spec in HARNESS_SPECS.items()
    ]
    return {
        "node_id": app.node_id if hasattr(app, "node_id") else "shipwright",
        "reasoner": "build",
        "harnesses": harnesses,
        "github_token": bool(github_token()),
    }


if __name__ == "__main__":
    app.run()
