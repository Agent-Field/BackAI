"""Shipwright workload module — task queue + agent dispatch.

This sidecar service owns:
  - POST   /tasks            Create + enqueue a task
  - GET    /tasks            List the caller's tasks (newest first)
  - GET    /tasks/{id}       Detail with steps + diff preview
  - POST   /tasks/{id}/cancel Cancel a queued/running task
  - GET    /stats            Operator-side metrics (cross-tenant)

The handler accepts x-af-stack-tenant-id + x-af-stack-user-id from the
runtime's tenant resolver. It calls the AgentField stub agent
(`shipwright-v2.execute_task`) via the runtime's agents gateway, which routes
through AgentField -> the customer's LiteLLM virtual key.

The agent runs synchronously (~10s for the stub). The handler runs the
call in a background asyncio task so the POST returns immediately. The
customer-app polls GET /tasks/{id} for progress.
"""

from __future__ import annotations

import asyncio
import json
import os
import uuid
from contextlib import asynccontextmanager
from datetime import datetime, timezone
from typing import Any

import httpx
import psycopg
from fastapi import FastAPI, Header, HTTPException
from psycopg.rows import dict_row
from pydantic import BaseModel, Field


DATABASE_URL = os.environ["SHIPWRIGHT_DATABASE_URL"]
RUNTIME_URL = os.getenv("AF_STACK_URL", "http://runtime:8080").rstrip("/")
AGENTFIELD_URL = os.getenv("AGENTFIELD_URL", "http://agentfield:8080").rstrip("/")

# SHIPWRIGHT_AGENT controls which agent the workload module dispatches to.
#   - shipwright-v2.execute_task : the iteration stub (sync, ~10s, no keys)
#   - swe-planner.build          : the real SWE-AF library (async, minutes,
#                                  needs ANTHROPIC_API_KEY + GH_TOKEN)
AGENT_NAME = os.getenv("SHIPWRIGHT_AGENT", "swe-planner.build")

# SHIPWRIGHT_MODE controls the call shape:
#   - sync  : POST /api/v1/agents/<name> and wait for the response inline
#             (fine for the stub; bad for real builds that take minutes)
#   - async : POST /api/v1/agents/async/<name> and poll executions/<id>
#             (the only shape that works for real SWE-AF builds)
AGENT_MODE = os.getenv("SHIPWRIGHT_MODE", "async").lower()

# Auto-pick mode if user explicitly named the stub.
if AGENT_NAME == "shipwright-v2.execute_task" and os.getenv("SHIPWRIGHT_MODE") is None:
    AGENT_MODE = "sync"


_runtime_client: httpx.AsyncClient | None = None


@asynccontextmanager
async def _lifespan(_app: FastAPI):
    global _runtime_client
    _runtime_client = httpx.AsyncClient(base_url=RUNTIME_URL, timeout=600.0)
    try:
        yield
    finally:
        await _runtime_client.aclose()
        _runtime_client = None


app = FastAPI(
    title="Shipwright API",
    version="0.1.0",
    description="Autonomous code-agent task queue.",
    lifespan=_lifespan,
)


def _require_tenant(tenant: str | None, user: str | None) -> tuple[str, str]:
    tenant = (tenant or "").strip()
    user = (user or "").strip()
    if not tenant:
        raise HTTPException(401, {"code": "NO_TENANT"})
    if not user:
        raise HTTPException(401, {"code": "NO_USER"})
    return tenant, user


@asynccontextmanager
async def _tenant_conn(tenant_id: str):
    async with await psycopg.AsyncConnection.connect(DATABASE_URL,
                                                      row_factory=dict_row) as conn:
        async with conn.transaction():
            await conn.execute(
                "SELECT set_config('app.tenant_id', %s, true)", (tenant_id,)
            )
            yield conn


@asynccontextmanager
async def _admin_conn():
    async with await psycopg.AsyncConnection.connect(DATABASE_URL,
                                                      row_factory=dict_row) as conn:
        async with conn.transaction():
            await conn.execute(
                "SELECT set_config('app.bypass_rls', 'on', true)"
            )
            yield conn


# ─── Request / response models ───────────────────────────────────────────


class CreateTaskRequest(BaseModel):
    issue_url: str = Field(..., min_length=1, max_length=2048)
    title: str = Field(..., min_length=1, max_length=200)
    description: str = Field("", max_length=4000)


class StepModel(BaseModel):
    idx: int
    title: str
    status: str
    detail: str | None = None
    started_at: str | None = None
    finished_at: str | None = None


class TaskSummary(BaseModel):
    id: str
    title: str
    issue_url: str
    status: str
    created_at: str
    started_at: str | None = None
    finished_at: str | None = None


class TaskDetail(TaskSummary):
    description: str
    result_summary: str | None = None
    diff_preview: str | None = None
    error: str | None = None
    steps: list[StepModel]


# ─── Background agent driver ─────────────────────────────────────────────


def _build_payload(issue_url: str, title: str, description: str) -> dict:
    """Translate the customer-app form into the configured agent's input.

    Two reasoner shapes are supported:

      - shipwright-v2.execute_task — takes {"payload": {issue_url, title,
        description}} and returns the canonical step result.
      - swe-planner.build — the real SWE-AF. Takes {"goal", "repo_url",
        "additional_context", ...}. We derive goal from title +
        description and pass the issue_url as repo_url (SWE-AF clones it
        if it looks like a repo).
    """
    if AGENT_NAME.endswith(".build") or AGENT_NAME.startswith("swe-planner"):
        # SWE-AF reasoner signature
        goal = title
        if description:
            goal = f"{title}\n\n{description}"
        return {"goal": goal, "repo_url": issue_url, "artifacts_dir": ".artifacts"}
    # Stub reasoner signature
    return {"payload": {"issue_url": issue_url, "title": title,
                         "description": description}}


async def _drive_task(task_id: str, tenant_id: str, user_id: str,
                       issue_url: str, title: str, description: str):
    """Run the agent in the background and persist results to the DB.

    Branches on AGENT_MODE:
      - sync  : single POST, wait inline
      - async : POST to /async/, poll executions/{id} every 5s
    """
    assert _runtime_client is not None

    async with _tenant_conn(tenant_id) as conn:
        await conn.execute(
            """UPDATE shipwright_tasks
               SET status = 'running', started_at = now()
               WHERE id = %s""",
            (task_id,),
        )

    headers = {
        "x-af-stack-tenant-id": tenant_id,
        "x-af-stack-user-id": user_id,
    }
    input_payload = _build_payload(issue_url, title, description)

    if AGENT_MODE == "async":
        await _drive_async(task_id, tenant_id, input_payload, headers)
    else:
        await _drive_sync(task_id, tenant_id, input_payload, headers)


async def _drive_sync(task_id: str, tenant_id: str, input_payload: dict,
                       headers: dict):
    """Sync agent call. Good for the stub (~10s). Bad for real builds."""
    assert _runtime_client is not None
    try:
        resp = await _runtime_client.post(
            f"/api/v1/agents/{AGENT_NAME}",
            json={"input": input_payload},
            headers=headers,
        )
    except Exception as exc:  # noqa: BLE001
        await _record_failure(task_id, tenant_id, f"agent call failed: {exc}")
        return

    if resp.status_code != 200:
        await _record_failure(
            task_id, tenant_id,
            f"agent returned {resp.status_code}: {resp.text[:500]}",
        )
        return

    body = resp.json() or {}
    result = body.get("result") or {}
    await _persist_result(task_id, tenant_id, result)


async def _drive_async(task_id: str, tenant_id: str, input_payload: dict,
                        headers: dict):
    """Async agent call. Posts to /async/, polls executions/{id} every 5s.

    This is the only shape that works for real SWE-AF builds (which can
    run for minutes / hours and emit incremental status as they go).
    """
    assert _runtime_client is not None

    try:
        resp = await _runtime_client.post(
            f"/api/v1/agents/async/{AGENT_NAME}",
            json={"input": input_payload},
            headers=headers,
        )
    except Exception as exc:  # noqa: BLE001
        await _record_failure(task_id, tenant_id, f"async dispatch failed: {exc}")
        return

    if resp.status_code not in (200, 202):
        await _record_failure(
            task_id, tenant_id,
            f"async dispatch returned {resp.status_code}: {resp.text[:500]}",
        )
        return

    body = resp.json() or {}
    execution_id = body.get("execution_id")
    run_id = body.get("run_id")
    if not execution_id:
        await _record_failure(
            task_id, tenant_id,
            f"no execution_id in async dispatch response: {resp.text[:300]}",
        )
        return

    async with _tenant_conn(tenant_id) as conn:
        await conn.execute(
            """UPDATE shipwright_tasks SET execution_id = %s, run_id = %s
               WHERE id = %s""",
            (execution_id, run_id, task_id),
        )

    # Poll AgentField's execution endpoint directly. AgentField stores the
    # canonical execution state and doesn't require af-stack auth (it has
    # its own DID-based auth which AgentFieldSDK handles, but for read-only
    # status the public GET is open inside the docker network).
    poll_interval_s = 2.0  # tighter for fast-failing builds
    max_wait_s = 60 * 60
    elapsed = 0.0

    async with httpx.AsyncClient(base_url=AGENTFIELD_URL,
                                  timeout=10.0) as af:
        while elapsed < max_wait_s:
            await asyncio.sleep(poll_interval_s)
            elapsed += poll_interval_s
            try:
                poll = await af.get(f"/api/v1/executions/{execution_id}")
            except Exception as exc:  # noqa: BLE001
                print(f"[shipwright-api] poll error (will retry): {exc}",
                      flush=True)
                continue

            if poll.status_code != 200:
                print(
                    f"[shipwright-api] AgentField poll {poll.status_code} "
                    f"for {execution_id} (elapsed {elapsed:.0f}s)",
                    flush=True,
                )
                continue

            exec_body = poll.json() or {}
            status = (exec_body.get("status") or "").lower()

            if status in ("succeeded", "completed", "failed", "cancelled",
                          "error"):
                result = exec_body.get("result") or exec_body.get("output") or {}
                if status in ("succeeded", "completed"):
                    await _persist_result(task_id, tenant_id, result)
                else:
                    err = (
                        exec_body.get("error")
                        or exec_body.get("error_message")
                        or "agent execution failed"
                    )
                    print(
                        f"[shipwright-api] task {task_id} → {status}: "
                        f"{str(err)[:200]}",
                        flush=True,
                    )
                    await _record_failure(task_id, tenant_id, str(err))
                return

    await _record_failure(
        task_id, tenant_id,
        f"execution timed out after {int(max_wait_s/60)} minutes",
    )


async def _persist_result(task_id: str, tenant_id: str, result: dict):
    """Write the agent's structured result back to the DB.

    Handles both reasoner shapes:
      - stub returns {status, summary, diff_preview, steps}
      - SWE-AF returns the BuildResult schema (build_id, repos, pr_url, etc).
        We coerce that to our shape so the UI doesn't need to know which
        agent produced it.
    """
    # Stub shape
    status = result.get("status", "completed")
    summary = result.get("summary")
    diff_preview = result.get("diff_preview")
    steps = result.get("steps") or []

    # SWE-AF shape — translate.
    if not summary and result.get("pr_url"):
        summary = (
            f"Build completed. PR: {result['pr_url']}\n"
            f"Build id: {result.get('build_id', '—')}"
        )
    elif not summary and result.get("repos"):
        repos = result.get("repos") or []
        prs = [r.get("pr_url") for r in repos if r.get("pr_url")]
        summary = (
            f"Build completed across {len(repos)} repo(s)."
            + (f" PRs: {', '.join(prs)}" if prs else "")
        )

    if not diff_preview and result.get("diff"):
        diff_preview = result.get("diff")

    # Derive a step list from SWE-AF's plan if we have it.
    if not steps and result.get("plan"):
        plan = result.get("plan") or {}
        tasks = plan.get("tasks") or []
        steps = [
            {
                "idx": i + 1,
                "title": t.get("title", f"Task {i+1}"),
                "status": t.get("status", "completed"),
                "detail": t.get("description") or t.get("summary"),
            }
            for i, t in enumerate(tasks)
        ]

    if status == "succeeded":
        status = "completed"

    async with _tenant_conn(tenant_id) as conn:
        await conn.execute(
            """UPDATE shipwright_tasks
               SET status = %s,
                   result_summary = %s,
                   diff_preview = %s,
                   finished_at = now()
               WHERE id = %s""",
            (status, summary, diff_preview, task_id),
        )
        for step in steps:
            await conn.execute(
                """INSERT INTO shipwright_steps
                   (task_id, tenant_id, idx, title, status, detail,
                    started_at, finished_at)
                   VALUES (%s, %s, %s, %s, %s, %s, now(), now())
                   ON CONFLICT (task_id, idx) DO UPDATE SET
                       status = excluded.status,
                       detail = excluded.detail,
                       finished_at = excluded.finished_at""",
                (
                    task_id, tenant_id, step["idx"], step["title"],
                    step.get("status", "completed"), step.get("detail"),
                ),
            )


async def _record_failure(task_id: str, tenant_id: str, msg: str):
    async with _tenant_conn(tenant_id) as conn:
        await conn.execute(
            """UPDATE shipwright_tasks
               SET status = 'failed', error = %s, finished_at = now()
               WHERE id = %s""",
            (msg, task_id),
        )


# ─── Routes ──────────────────────────────────────────────────────────────


@app.get("/health")
async def health():
    return {"ok": True}


@app.post("/tasks", response_model=TaskDetail)
async def create_task(
    req: CreateTaskRequest,
    x_af_stack_tenant_id: str | None = Header(default=None),
    x_af_stack_user_id: str | None = Header(default=None),
):
    tenant_id, user_id = _require_tenant(x_af_stack_tenant_id, x_af_stack_user_id)
    task_id = str(uuid.uuid4())
    async with _tenant_conn(tenant_id) as conn:
        await conn.execute(
            """INSERT INTO shipwright_tasks
               (id, tenant_id, user_id, issue_url, title, description, status)
               VALUES (%s, %s, %s, %s, %s, %s, 'queued')""",
            (task_id, tenant_id, user_id, req.issue_url, req.title,
             req.description),
        )

    # Kick off the agent asynchronously. Don't await — POST returns now.
    asyncio.create_task(
        _drive_task(task_id, tenant_id, user_id, req.issue_url, req.title,
                    req.description)
    )

    return await _load_task(task_id, tenant_id)


@app.get("/tasks", response_model=list[TaskSummary])
async def list_tasks(
    limit: int = 50,
    x_af_stack_tenant_id: str | None = Header(default=None),
    x_af_stack_user_id: str | None = Header(default=None),
):
    tenant_id, _ = _require_tenant(x_af_stack_tenant_id, x_af_stack_user_id)
    async with _tenant_conn(tenant_id) as conn:
        rows = await (
            await conn.execute(
                """SELECT id::text, title, issue_url, status,
                          created_at::text, started_at::text, finished_at::text
                   FROM shipwright_tasks
                   ORDER BY created_at DESC
                   LIMIT %s""",
                (limit,),
            )
        ).fetchall()
        return [TaskSummary(**row) for row in rows]


@app.get("/tasks/{task_id}", response_model=TaskDetail)
async def get_task(
    task_id: str,
    x_af_stack_tenant_id: str | None = Header(default=None),
    x_af_stack_user_id: str | None = Header(default=None),
):
    tenant_id, _ = _require_tenant(x_af_stack_tenant_id, x_af_stack_user_id)
    return await _load_task(task_id, tenant_id)


@app.post("/tasks/{task_id}/cancel", response_model=TaskDetail)
async def cancel_task(
    task_id: str,
    x_af_stack_tenant_id: str | None = Header(default=None),
    x_af_stack_user_id: str | None = Header(default=None),
):
    tenant_id, _ = _require_tenant(x_af_stack_tenant_id, x_af_stack_user_id)
    async with _tenant_conn(tenant_id) as conn:
        result = await conn.execute(
            """UPDATE shipwright_tasks
               SET status = 'cancelled', finished_at = now()
               WHERE id = %s AND status IN ('queued', 'running')""",
            (task_id,),
        )
        if result.rowcount == 0:
            raise HTTPException(409, {"code": "NOT_CANCELLABLE"})
    return await _load_task(task_id, tenant_id)


@app.get("/stats")
async def stats(
    x_af_stack_tenant_id: str | None = Header(default=None),
):
    # Operator view — cross-tenant. Doesn't require tenant header.
    async with _admin_conn() as conn:
        rows = await (
            await conn.execute(
                """SELECT status, COUNT(*) AS n
                   FROM shipwright_tasks
                   WHERE created_at > now() - interval '24 hours'
                   GROUP BY status"""
            )
        ).fetchall()
        by_status = {row["status"]: row["n"] for row in rows}

        row = await (
            await conn.execute(
                """SELECT COUNT(*) AS n FROM shipwright_tasks"""
            )
        ).fetchone()
        total = (row or {}).get("n", 0)
    return {"total": total, "by_status_24h": by_status}


async def _load_task(task_id: str, tenant_id: str) -> TaskDetail:
    async with _tenant_conn(tenant_id) as conn:
        row = await (
            await conn.execute(
                """SELECT id::text, title, issue_url, status, description,
                          result_summary, diff_preview, error,
                          created_at::text, started_at::text, finished_at::text
                   FROM shipwright_tasks
                   WHERE id = %s""",
                (task_id,),
            )
        ).fetchone()
        if not row:
            raise HTTPException(404, {"code": "NOT_FOUND"})

        steps_rows = await (
            await conn.execute(
                """SELECT idx, title, status, detail,
                          started_at::text, finished_at::text
                   FROM shipwright_steps
                   WHERE task_id = %s
                   ORDER BY idx""",
                (task_id,),
            )
        ).fetchall()
        steps = [StepModel(**r) for r in steps_rows]

    return TaskDetail(**row, steps=steps)
