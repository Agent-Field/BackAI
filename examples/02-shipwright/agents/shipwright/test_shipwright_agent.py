# SPDX-License-Identifier: Apache-2.0
"""Unit tests for the Shipwright coding agent (H3).

These drive the whole clone → harness → commit → push → open-PR flow with
fake subprocess + HTTP runners, so no git, network, or GH_TOKEN is needed.
The contract under test: a real PR URL flows back, the token never appears in
a log line, and nothing is fabricated when there are no changes.
"""

from __future__ import annotations

import asyncio

import main
import pytest
from main import (
    BuildRequest,
    CmdResult,
    GitError,
    HttpResult,
    Shipwright,
    authed_clone_url,
    branch_name,
    parse_repo,
    pr_body,
    pr_title,
    redact,
    resolve_harness,
)

TOKEN = "ghp_SUPERSECRETVALUE123"  # noqa: S105 — fake token for redaction assertions


# --------------------------------------------------------------------------- #
# Pure helpers
# --------------------------------------------------------------------------- #


@pytest.mark.parametrize(
    "url,expected",
    [
        ("https://github.com/acme/widgets", ("github.com", "acme", "widgets")),
        ("https://github.com/acme/widgets.git", ("github.com", "acme", "widgets")),
        ("https://user@github.com/acme/widgets.git/", ("github.com", "acme", "widgets")),
        ("git@github.com:acme/widgets.git", ("github.com", "acme", "widgets")),
        ("github.com/acme/widgets", ("github.com", "acme", "widgets")),
    ],
)
def test_parse_repo_variants(url, expected):
    assert parse_repo(url) == expected


def test_parse_repo_rejects_garbage():
    with pytest.raises(ValueError):
        parse_repo("not-a-url")


def test_authed_clone_url_embeds_and_redacts():
    url = authed_clone_url("https://github.com/acme/widgets", TOKEN)
    assert url == f"https://x-access-token:{TOKEN}@github.com/acme/widgets.git"
    # ssh input is normalised to https so the token can be used.
    assert authed_clone_url("git@github.com:acme/widgets.git", TOKEN).startswith(
        f"https://x-access-token:{TOKEN}@github.com/"
    )
    # redaction hides both the bare token and its embedded form.
    assert TOKEN not in redact(url, TOKEN)


def test_branch_name_is_deterministic():
    assert (
        branch_name("t-abc123def456", "Fix the Null Check!")
        == "shipwright/fix-the-null-check-t-abc123def4"
    )
    assert branch_name("", "Fix") == "shipwright/fix"
    assert branch_name("", "") == "shipwright/task"


def test_pr_title_and_body():
    assert pr_title("fix: nil deref", "") == "fix: nil deref"
    assert pr_title("Add retries", "") == "Shipwright: Add retries"
    # whitespace-only description must not blow up
    assert pr_title("", "   \n  ") == "Shipwright change"
    body = pr_body(
        BuildRequest(repo_url="x", task_id="t-1", title="T", description="D"),
        "1 file changed",
        "codex",
    )
    assert "harness: `codex`" in body and "task: `t-1`" in body and "1 file changed" in body


def test_resolve_harness_none_when_missing(monkeypatch):
    monkeypatch.setattr(main.shutil, "which", lambda _n: None)
    assert resolve_harness("codex") is None


def test_resolve_harness_needs_auth(monkeypatch):
    # only codex is "installed" so the auth gate is what decides.
    monkeypatch.setattr(main.shutil, "which", lambda n: "/usr/bin/codex" if n == "codex" else None)
    for k in ("OPENAI_API_KEY", "OPENROUTER_API_KEY"):
        monkeypatch.delenv(k, raising=False)
    # installed but unauthenticated → not chosen
    assert resolve_harness("codex") is None
    monkeypatch.setenv("OPENAI_API_KEY", "sk-x")
    assert resolve_harness("codex").provider == "codex"


def test_harness_command_omits_empty_model():
    spec = main.HARNESS_SPECS["codex"]
    assert spec.command("/usr/bin/codex", "do it", "") == [
        "/usr/bin/codex",
        "exec",
        "--full-auto",
        "do it",
    ]


# --------------------------------------------------------------------------- #
# Fake runners
# --------------------------------------------------------------------------- #


class FakeRun:
    """Records commands; fakes git + harness results."""

    def __init__(self, status_output="A  SHIPWRIGHT_TASKS/task.md\n", harness_rc=0):
        self.calls: list[list[str]] = []
        self.status_output = status_output
        self.harness_rc = harness_rc

    def __call__(self, cmd, cwd=None, env=None, timeout=None):
        self.calls.append(cmd)
        if cmd[0] == "git":
            args = cmd[1:]
            i = 0
            while i < len(args) and args[i] == "-c":
                i += 2
            verb = args[i] if i < len(args) else ""
            if verb == "status":
                return CmdResult(0, self.status_output, "")
            if verb == "diff":
                return CmdResult(0, " SHIPWRIGHT_TASKS/task.md | 3 +++\n", "")
            return CmdResult(0, "", "")
        return CmdResult(self.harness_rc, "harness output", "harness err")

    def verbs(self):
        out = []
        for cmd in self.calls:
            if cmd[0] != "git":
                out.append("harness")
                continue
            args = cmd[1:]
            i = 0
            while i < len(args) and args[i] == "-c":
                i += 2
            out.append(args[i] if i < len(args) else "")
        return out


class FakeHttp:
    def __init__(self, status=201, body=None):
        self.calls = []
        self.status = status
        self.body = (
            body
            if body is not None
            else {"html_url": "https://github.com/acme/widgets/pull/7", "number": 7}
        )

    def __call__(self, method, url, headers, payload):
        self.calls.append((method, url, headers, payload))
        return HttpResult(self.status, self.body)


def _req(**kw):
    base = dict(
        repo_url="https://github.com/acme/widgets",
        task_id="t-42",
        title="Add a thing",
        description="Please add a thing.",
        base_branch="main",
    )
    base.update(kw)
    return BuildRequest(**base)


# --------------------------------------------------------------------------- #
# Full flow
# --------------------------------------------------------------------------- #


def test_build_opens_real_pr_via_fallback(tmp_path, monkeypatch):
    monkeypatch.setattr(main, "resolve_harness", lambda _p: None)  # force honest fallback
    run, http = FakeRun(), FakeHttp()
    logs: list[str] = []
    sw = Shipwright(TOKEN, run=run, http=http, workdir_root=str(tmp_path), log=logs.append)

    out = sw.build(_req(callback_url=""))

    assert out["status"] == "ok"
    assert out["pr_url"] == "https://github.com/acme/widgets/pull/7"
    assert out["branch"] == "shipwright/add-a-thing-t-42"
    assert out["harness"] == "file-edit-fallback"
    # git sequence: clone → checkout → add → status → diff → commit → push
    assert run.verbs() == ["clone", "checkout", "add", "status", "diff", "commit", "push"]
    # the fallback wrote a REAL file into the working tree (not a fabricated diff)
    assert (tmp_path / "t-42" / "SHIPWRIGHT_TASKS" / "t-42.md").exists()
    # exactly one PR POST to the GitHub API
    assert len(http.calls) == 1 and http.calls[0][1].endswith("/repos/acme/widgets/pulls")
    assert http.calls[0][3]["head"] == "shipwright/add-a-thing-t-42"


def test_build_never_logs_the_token(tmp_path, monkeypatch):
    monkeypatch.setattr(main, "resolve_harness", lambda _p: None)
    logs: list[str] = []
    sw = Shipwright(
        TOKEN, run=FakeRun(), http=FakeHttp(), workdir_root=str(tmp_path), log=logs.append
    )
    sw.build(_req(callback_url=""))
    assert logs, "expected some log output"
    assert all(TOKEN not in line for line in logs), "token leaked into a log line"


def test_build_no_changes_does_not_open_pr(tmp_path, monkeypatch):
    monkeypatch.setattr(main, "resolve_harness", lambda _p: None)
    run, http = FakeRun(status_output="   \n"), FakeHttp()  # clean tree
    sw = Shipwright(TOKEN, run=run, http=http, workdir_root=str(tmp_path), log=lambda _m: None)
    out = sw.build(_req(callback_url=""))
    assert out["status"] == "no_changes"
    assert http.calls == [], "must not open a PR when there is no diff"


def test_build_uses_harness_when_available(tmp_path, monkeypatch):
    spec = main.HARNESS_SPECS["codex"]
    monkeypatch.setattr(main, "resolve_harness", lambda _p: spec)
    run, http = FakeRun(), FakeHttp()
    sw = Shipwright(TOKEN, run=run, http=http, workdir_root=str(tmp_path), log=lambda _m: None)
    out = sw.build(_req(callback_url=""))
    assert out["status"] == "ok" and out["harness"] == "codex"
    assert "harness" in run.verbs(), "expected the harness binary to be invoked"


def test_build_surfaces_github_failure(tmp_path, monkeypatch):
    monkeypatch.setattr(main, "resolve_harness", lambda _p: None)
    http = FakeHttp(status=422, body={"message": "A pull request already exists"})
    sw = Shipwright(
        TOKEN, run=FakeRun(), http=http, workdir_root=str(tmp_path), log=lambda _m: None
    )
    with pytest.raises(GitError) as ei:
        sw.build(_req(callback_url=""))
    assert "422" in str(ei.value) and "already exists" in str(ei.value)


def test_build_fires_completion_callback(tmp_path, monkeypatch):
    monkeypatch.setattr(main, "resolve_harness", lambda _p: None)
    http = FakeHttp()
    sw = Shipwright(
        TOKEN, run=FakeRun(), http=http, workdir_root=str(tmp_path), log=lambda _m: None
    )
    sw.build(_req(callback_url="/api/v1/shipwright/tasks/t-42/complete"))
    # 1st call = PR creation, 2nd = completion callback carrying the PR url
    assert len(http.calls) == 2
    cb = http.calls[1]
    assert cb[1].endswith("/api/v1/shipwright/tasks/t-42/complete")
    assert cb[3]["ref"] == "https://github.com/acme/widgets/pull/7"


# --------------------------------------------------------------------------- #
# Reasoner guards
# --------------------------------------------------------------------------- #


def test_reasoner_rejects_invalid_payload(monkeypatch):
    monkeypatch.setenv("GH_TOKEN", TOKEN)
    out = asyncio.run(main.build({"title": "no repo url"}))
    assert out["status"] == "invalid_request"


def test_build_input_folds_goal_alias():
    spec = main.BuildInput.model_validate(
        {"repo_url": "https://github.com/acme/widgets", "goal": "Fix the bug\n\nDetails here."}
    )
    assert spec.title == "Fix the bug"
    assert spec.description == "Details here."
    # explicit title/description win over goal
    spec2 = main.BuildInput.model_validate(
        {"repo_url": "x", "title": "T", "description": "D", "goal": "ignored"}
    )
    assert spec2.title == "T" and spec2.description == "D"


def test_reasoner_requires_token(monkeypatch):
    monkeypatch.delenv("GH_TOKEN", raising=False)
    monkeypatch.delenv("GITHUB_TOKEN", raising=False)
    out = asyncio.run(main.build({"repo_url": "https://github.com/acme/widgets"}))
    assert out["status"] == "missing_credential"
