# Template: Python workload module (FastAPI sidecar pattern).
#
# This is the CURRENT canonical pattern (matches examples/01-notable).
# The Go in-runtime workload-module loader is on the Phase 13.4 roadmap;
# until it lands, ship workload modules as standalone FastAPI services
# that run alongside the runtime in docker-compose.
#
# Drop a copy into examples/<your-app>/handlers/handler.py and:
#   1. Replace YOUR_MODULE with your module id (lowercase, kebab-case).
#   2. Replace YOUR_DOMAIN_TABLE with your table name.
#   3. Add your routes in the marked section.
#
# Required env (read at startup):
#   <YOUR_MODULE>_DATABASE_URL  — connection string to the af-stack
#                                  Postgres. The runtime owns tenancy +
#                                  RLS; this service sets the GUC per
#                                  request.
#   AF_STACK_URL                — http://runtime:8080 (Docker DNS) or
#                                  localhost:38080 (host)
#
# Auth model — REQUIRED:
#   Every request must carry x-af-stack-tenant-id + x-af-stack-user-id
#   headers. In production these come from the runtime's tenant resolver
#   middleware (it parses the API key / session cookie and forwards
#   them). In dev / smoke-tests the caller sets them directly. Reject
#   missing headers with 401.

from __future__ import annotations

import os
from contextlib import asynccontextmanager
from typing import Any

import httpx
import psycopg
from fastapi import FastAPI, Header, HTTPException
from psycopg.rows import dict_row
from pydantic import BaseModel


# ─── Config ──────────────────────────────────────────────────────────────

DATABASE_URL = os.environ["YOUR_MODULE_DATABASE_URL"]
RUNTIME_URL = os.getenv("AF_STACK_URL", "http://runtime:8080").rstrip("/")


# ─── Lifecycle ───────────────────────────────────────────────────────────

_runtime_client: httpx.AsyncClient | None = None


@asynccontextmanager
async def _lifespan(_app: FastAPI):
    global _runtime_client
    _runtime_client = httpx.AsyncClient(base_url=RUNTIME_URL, timeout=30.0)
    try:
        yield
    finally:
        await _runtime_client.aclose()
        _runtime_client = None


app = FastAPI(
    title="YOUR_MODULE API",
    version="0.1.0",
    lifespan=_lifespan,
)


# ─── Tenant resolver (REQUIRED for every route that touches data) ────────


def _require_tenant(
    tenant_id: str | None, user_id: str | None
) -> tuple[str, str]:
    """Extract + validate the per-request tenant + user."""
    tenant_id = (tenant_id or "").strip()
    user_id = (user_id or "").strip()
    if not tenant_id:
        raise HTTPException(401, {"code": "NO_TENANT", "message": "missing tenant"})
    if not user_id:
        raise HTTPException(401, {"code": "NO_USER", "message": "missing user"})
    return tenant_id, user_id


@asynccontextmanager
async def _tenant_conn(tenant_id: str):
    """Yield a DB connection with the RLS GUC set for this tenant.

    SET LOCAL ties the value to the surrounding transaction, so a pooled
    connection can't leak it to the next caller. ALWAYS go through this
    helper for any query that hits tenant-scoped tables.
    """
    async with await psycopg.AsyncConnection.connect(
        DATABASE_URL, row_factory=dict_row
    ) as conn:
        async with conn.transaction():
            await conn.execute(
                "SELECT set_config('app.tenant_id', %s, true)", (tenant_id,)
            )
            yield conn


# ─── Call AF Stack runtime / AgentField via the suite gateway ────────────


async def _call_agent(name: str, payload: dict, tenant_id: str) -> dict:
    """Invoke an AgentField reasoner via the runtime's agents gateway."""
    assert _runtime_client is not None
    resp = await _runtime_client.post(
        f"/api/v1/agents/{name}",
        json={"input": payload},
        headers={"x-af-stack-tenant-id": tenant_id},
    )
    if resp.status_code != 200:
        raise HTTPException(502, {"agent_error": resp.text})
    return (resp.json() or {}).get("output") or {}


# ─── EDIT: define your request/response models ───────────────────────────


class CreateRequest(BaseModel):
    title: str
    body: str = ""


class Item(BaseModel):
    id: str
    title: str
    body: str
    created_at: str


# ─── EDIT: define your routes ────────────────────────────────────────────


@app.post("/items", response_model=Item)
async def create_item(
    req: CreateRequest,
    x_af_stack_tenant_id: str | None = Header(default=None),
    x_af_stack_user_id: str | None = Header(default=None),
):
    tenant_id, user_id = _require_tenant(x_af_stack_tenant_id, x_af_stack_user_id)
    async with _tenant_conn(tenant_id) as conn:
        row = await (
            await conn.execute(
                """INSERT INTO YOUR_DOMAIN_TABLE (tenant_id, user_id, title, body)
                   VALUES (%s, %s, %s, %s)
                   RETURNING id, title, body, created_at::text""",
                (tenant_id, user_id, req.title, req.body),
            )
        ).fetchone()
        return row


@app.get("/items", response_model=list[Item])
async def list_items(
    x_af_stack_tenant_id: str | None = Header(default=None),
    x_af_stack_user_id: str | None = Header(default=None),
):
    tenant_id, _ = _require_tenant(x_af_stack_tenant_id, x_af_stack_user_id)
    async with _tenant_conn(tenant_id) as conn:
        rows = await (
            await conn.execute(
                """SELECT id, title, body, created_at::text
                   FROM YOUR_DOMAIN_TABLE
                   ORDER BY created_at DESC
                   LIMIT 100"""
            )
        ).fetchall()
        return rows


# ─── Optional: trigger an agent for some action ──────────────────────────


@app.post("/items/{item_id}/process")
async def process_item(
    item_id: str,
    x_af_stack_tenant_id: str | None = Header(default=None),
    x_af_stack_user_id: str | None = Header(default=None),
):
    tenant_id, _ = _require_tenant(x_af_stack_tenant_id, x_af_stack_user_id)
    async with _tenant_conn(tenant_id) as conn:
        row = await (
            await conn.execute(
                "SELECT title, body FROM YOUR_DOMAIN_TABLE WHERE id = %s",
                (item_id,),
            )
        ).fetchone()
        if not row:
            raise HTTPException(404, "not found")

    # Invoke your agent — the call routes through the suite gateway →
    # AgentField → LiteLLM. Cost lands on this tenant automatically.
    result = await _call_agent(
        "REPLACE_ME.your_reasoner",  # node_id.reasoner-name
        {"title": row["title"], "body": row["body"]},
        tenant_id,
    )
    return {"item_id": item_id, "result": result}
