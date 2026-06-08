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
AGENT_NAME = os.getenv("SHIPWRIGHT_AGENT", "shipwright-v2.execute_task")


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


async def _drive_task(task_id: str, tenant_id: str, user_id: str,
                       issue_url: str, title: str, description: str):
    """Run the agent in the background and persist results to the DB."""
    assert _runtime_client is not None

    # Mark task as running.
    async with _tenant_conn(tenant_id) as conn:
        await conn.execute(
            """UPDATE shipwright_tasks
               SET status = 'running', started_at = now()
               WHERE id = %s""",
            (task_id,),
        )

    try:
        resp = await _runtime_client.post(
            f"/api/v1/agents/{AGENT_NAME}",
            json={
                "input": {
                    "payload": {
                        "issue_url": issue_url,
                        "title": title,
                        "description": description,
                    }
                }
            },
            headers={
                "x-af-stack-tenant-id": tenant_id,
                "x-af-stack-user-id": user_id,
            },
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
    output = body.get("result") or {}
    status = output.get("status", "completed")
    summary = output.get("summary", "")
    diff_preview = output.get("diff_preview")
    steps = output.get("steps") or []

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
