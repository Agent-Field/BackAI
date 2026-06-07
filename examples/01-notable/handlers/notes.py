# SPDX-License-Identifier: Apache-2.0

"""Notable HTTP service — note CRUD + agent invocations + stats.

This runs as its own FastAPI service (``notable-api`` in the compose
file). Phase 13.4's workload-module loader will eventually let us mount
handlers like this directly inside the runtime; until that lands, a
standalone FastAPI app is the cleanest path that still proves the
multi-tenant + billing + agent-orchestration story end to end.

Auth model:
    Every request must carry ``x-af-stack-tenant-id`` and
    ``x-af-stack-user-id`` headers. In a real deployment the dashboard's
    session middleware sets these after better-auth resolves the user;
    in dev the seed script and smoke test set them directly.

What this exercises:
    - multi-tenancy : the ``SET LOCAL app.tenant_id`` dance gives the
      ``notable_notes`` RLS policy the value it compares against, so a
      tenant can never read another tenant's rows.
    - llm-gateway   : agent calls flow ``handler -> AgentField -> gateway
      -> OpenRouter`` and every token gets cost-tracked on the way back.
    - billing       : every successful POST /notes increments the
      ``notable_notes_created`` meter so the dashboard shows real usage.
    - memory        : the suggest_tags reasoner reads/writes per-user tag
      history transparently — we just pass user_id along and the agent
      handles the rest.
"""

from __future__ import annotations

import os
import time
import uuid
from contextlib import asynccontextmanager
from typing import Any, Optional

import httpx
import psycopg
from fastapi import FastAPI, Header, HTTPException, Request
from fastapi.responses import JSONResponse
from psycopg.rows import dict_row
from pydantic import BaseModel, Field

# ─── Config ──────────────────────────────────────────────────────────────

# We connect to the same Postgres the runtime uses. The runtime owns the
# tenancy GUCs; we set them locally on each transaction so the RLS policy
# on notable_notes can read app.tenant_id back via current_setting().
DATABASE_URL = os.environ["NOTABLE_DATABASE_URL"]

# AgentField runtime — used for two things: invoking the 3 Notable
# reasoners, and recording billing meter increments.
RUNTIME_URL = os.getenv("AF_STACK_URL", "http://runtime:8080").rstrip("/")

# Default model is informational here — agents pick their own model.
NOTABLE_MODEL = os.getenv("NOTABLE_MODEL", "openrouter/qwen/qwen-2.5-72b-instruct")


# ─── Lifecycle ───────────────────────────────────────────────────────────

# psycopg's async connection is cheap to open per-request, but a shared
# AsyncClient saves on TLS / TCP setup when we fan out to the runtime.
_runtime_client: Optional[httpx.AsyncClient] = None


@asynccontextmanager
async def _lifespan(app: FastAPI):  # noqa: ARG001
    global _runtime_client
    _runtime_client = httpx.AsyncClient(base_url=RUNTIME_URL, timeout=30.0)
    try:
        yield
    finally:
        await _runtime_client.aclose()
        _runtime_client = None


app = FastAPI(
    title="Notable API",
    version="0.1.0",
    description="Note CRUD + agent endpoints for the Notable example.",
    lifespan=_lifespan,
)


# ─── Tenant resolver ─────────────────────────────────────────────────────


def _require_tenant(
    x_af_stack_tenant_id: Optional[str], x_af_stack_user_id: Optional[str]
) -> tuple[str, str]:
    """Extract + validate the per-request tenant + user.

    In production the dashboard's tenant resolver middleware populates
    these headers from the session cookie. For dev / smoke-tests, the
    caller sets them directly. We reject missing headers with 401 so
    nobody accidentally writes a row without a tenant.
    """
    tenant_id = (x_af_stack_tenant_id or "").strip()
    user_id = (x_af_stack_user_id or "").strip()
    if not tenant_id:
        raise HTTPException(
            status_code=401,
            detail={"code": "NO_TENANT", "message": "x-af-stack-tenant-id is required"},
        )
    if not user_id:
        raise HTTPException(
            status_code=401,
            detail={"code": "NO_USER", "message": "x-af-stack-user-id is required"},
        )
    return tenant_id, user_id


# ─── DB connection helpers ───────────────────────────────────────────────


@asynccontextmanager
async def _tenant_conn(tenant_id: str):
    """Yield a connection with the tenancy GUC set for this tenant.

    Using SET LOCAL ties the value to the surrounding transaction, so a
    pooled connection can't leak it to the next caller. psycopg's
    async transaction context wraps everything in a tx for us.
    """
    async with await psycopg.AsyncConnection.connect(
        DATABASE_URL, row_factory=dict_row
    ) as conn:
        async with conn.transaction():
            # We escape the tenant id explicitly — current_setting is
            # plain SQL and a malicious value would be a bug surface.
            # psycopg's literal binding via .execute() handles this.
            await conn.execute(
                "SELECT set_config('app.tenant_id', %s, true)", (tenant_id,)
            )
            yield conn


@asynccontextmanager
async def _admin_conn():
    """Yield a connection with bypass_rls on — for cross-tenant reads
    in the stats endpoint."""
    async with await psycopg.AsyncConnection.connect(
        DATABASE_URL, row_factory=dict_row
    ) as conn:
        async with conn.transaction():
            await conn.execute(
                "SELECT set_config('app.bypass_rls', 'on', true)"
            )
            yield conn


# ─── AgentField + billing client helpers ─────────────────────────────────


async def _call_agent(
    name: str, payload: dict[str, Any], tenant_id: str
) -> dict[str, Any]:
    """Invoke an AgentField reasoner via the runtime gateway.

    The runtime's tenant resolver reads ``x-af-stack-tenant-id`` and
    propagates it to the LLM gateway so cost lands on the right tenant.
    """
    assert _runtime_client is not None
    resp = await _runtime_client.post(
        f"/agents/{name}",
        json={"input": payload},
        headers={"x-af-stack-tenant-id": tenant_id},
    )
    if resp.status_code != 200:
        # Surface the AF error envelope verbatim — the caller can decide
        # how to display it.
        raise HTTPException(status_code=502, detail={"agent_error": resp.text})
    body = resp.json() or {}
    return body.get("output") or {}


async def _meter(name: str, qty: float, tenant_id: str) -> None:
    """Record qty units of meter ``name`` for ``tenant_id``.

    Phase 10.5 will add a public ``POST /api/v1/billing/meter`` endpoint
    that wraps ``billing.Service.Meter`` exactly the way the runtime's
    internal sandbox/LLM call sites already do. Until that lands, we
    write straight to ``suite_usage_meters`` using the same UPSERT shape
    the Go store uses — the table is the contract.

    We swallow failures so a transient billing outage doesn't reject a
    user-facing write. The worst case is a missed meter row, which an
    operator can backfill from the audit log.
    """
    if not name or qty < 0 or not tenant_id:
        return
    # Month-bucket the period like billing.PeriodBoundary(BucketMonth)
    # does on the Go side. UTC, first of month, +1 month.
    from datetime import datetime, timezone

    now = datetime.now(timezone.utc)
    period_start = datetime(now.year, now.month, 1, tzinfo=timezone.utc)
    # Add one calendar month — replicate the Go AddDate(0,1,0) shape.
    if now.month == 12:
        period_end = datetime(now.year + 1, 1, 1, tzinfo=timezone.utc)
    else:
        period_end = datetime(now.year, now.month + 1, 1, tzinfo=timezone.utc)

    try:
        async with await psycopg.AsyncConnection.connect(
            DATABASE_URL
        ) as conn:
            async with conn.transaction():
                # Bypass RLS — suite_usage_meters is a platform table
                # and writes to it are operator-level, not tenant-level.
                await conn.execute(
                    "SELECT set_config('app.bypass_rls', 'on', true)"
                )
                await conn.execute(
                    """
                    INSERT INTO suite_usage_meters
                        (meter, tenant_id, period_start, period_end, quantity, cost_usd)
                    VALUES (%s, %s, %s, %s, %s, NULL)
                    ON CONFLICT (meter, tenant_id, period_start) DO UPDATE SET
                        quantity = suite_usage_meters.quantity + EXCLUDED.quantity
                    """,
                    (name, tenant_id, period_start, period_end, qty),
                )
    except psycopg.Error:
        # Best-effort; dashboard surfaces "billing not configured"
        # already if the module is off.
        return


# ─── Wire shapes ─────────────────────────────────────────────────────────


class NoteCreate(BaseModel):
    title: str = Field(..., min_length=1, max_length=512)
    body: str = Field(default="")
    tags: list[str] = Field(default_factory=list)


class Note(BaseModel):
    id: str
    tenant_id: str
    user_id: str
    title: str
    body: str
    tags: list[str]
    tldr: str | None
    created_at: str
    updated_at: str


class NoteList(BaseModel):
    notes: list[Note]
    total: int


# ─── Routes ──────────────────────────────────────────────────────────────


@app.get("/healthz")
async def healthz() -> dict[str, Any]:
    """Liveness probe — no DB / runtime touch so compose can start us
    before everything else is ready."""
    return {"ok": True, "service": "notable-api"}


@app.post("/notable/notes", response_model=Note, status_code=201)
async def create_note(
    body: NoteCreate,
    x_af_stack_tenant_id: Optional[str] = Header(default=None),
    x_af_stack_user_id: Optional[str] = Header(default=None),
) -> Note:
    """Create a note. Meters one ``notable_notes_created`` unit.

    The RLS policy enforces tenant_id; we pass the tenant id explicitly
    so the policy's WITH CHECK clause matches. (Setting app.tenant_id
    isn't enough on its own — the column value still has to equal the
    GUC, which is the policy's whole point.)
    """
    tenant_id, user_id = _require_tenant(x_af_stack_tenant_id, x_af_stack_user_id)
    note_id = str(uuid.uuid4())
    async with _tenant_conn(tenant_id) as conn:
        row = (
            await (
                await conn.execute(
                    """
                    INSERT INTO notable_notes (id, tenant_id, user_id, title, body, tags)
                    VALUES (%s, %s, %s, %s, %s, %s)
                    RETURNING *
                    """,
                    (note_id, tenant_id, user_id, body.title, body.body, body.tags),
                )
            ).fetchone()
        )
    # Meter outside the transaction — billing is its own concern.
    await _meter("notable_notes_created", 1, tenant_id)
    return _row_to_note(row)


@app.get("/notable/notes", response_model=NoteList)
async def list_notes(
    x_af_stack_tenant_id: Optional[str] = Header(default=None),
    x_af_stack_user_id: Optional[str] = Header(default=None),
    limit: int = 50,
    offset: int = 0,
) -> NoteList:
    """List notes for the current tenant, newest first."""
    tenant_id, _ = _require_tenant(x_af_stack_tenant_id, x_af_stack_user_id)
    # The composite index (tenant_id, updated_at DESC) keeps this O(limit).
    async with _tenant_conn(tenant_id) as conn:
        rows = (
            await (
                await conn.execute(
                    """
                    SELECT * FROM notable_notes
                    ORDER BY updated_at DESC
                    LIMIT %s OFFSET %s
                    """,
                    (limit, offset),
                )
            ).fetchall()
        )
        total = (
            await (
                await conn.execute("SELECT count(*) AS n FROM notable_notes")
            ).fetchone()
        )["n"]
    return NoteList(notes=[_row_to_note(r) for r in rows], total=total)


@app.get("/notable/notes/{note_id}", response_model=Note)
async def get_note(
    note_id: str,
    x_af_stack_tenant_id: Optional[str] = Header(default=None),
    x_af_stack_user_id: Optional[str] = Header(default=None),
) -> Note:
    tenant_id, _ = _require_tenant(x_af_stack_tenant_id, x_af_stack_user_id)
    async with _tenant_conn(tenant_id) as conn:
        row = (
            await (
                await conn.execute(
                    "SELECT * FROM notable_notes WHERE id = %s", (note_id,)
                )
            ).fetchone()
        )
    if not row:
        raise HTTPException(status_code=404, detail={"code": "NOT_FOUND"})
    return _row_to_note(row)


@app.patch("/notable/notes/{note_id}/summarize", response_model=Note)
async def summarize_note(
    note_id: str,
    x_af_stack_tenant_id: Optional[str] = Header(default=None),
    x_af_stack_user_id: Optional[str] = Header(default=None),
) -> Note:
    """Invoke the summarize agent and store its TLDR back to the row.

    Two-step pattern: read -> agent -> write. We don't hold the DB
    connection across the agent call because the LLM round-trip can take
    seconds and we don't want to pin a connection from the pool that
    long.
    """
    tenant_id, _ = _require_tenant(x_af_stack_tenant_id, x_af_stack_user_id)
    note = await get_note(note_id, x_af_stack_tenant_id, x_af_stack_user_id)

    result = await _call_agent(
        "notable-ai.summarize",
        {"note_id": note.id, "body": note.body},
        tenant_id=tenant_id,
    )
    tldr = (result or {}).get("tldr") or ""

    async with _tenant_conn(tenant_id) as conn:
        row = (
            await (
                await conn.execute(
                    "UPDATE notable_notes SET tldr = %s WHERE id = %s RETURNING *",
                    (tldr, note_id),
                )
            ).fetchone()
        )
    if not row:
        raise HTTPException(status_code=404, detail={"code": "NOT_FOUND"})
    return _row_to_note(row)


@app.patch("/notable/notes/{note_id}/suggest-tags", response_model=Note)
async def suggest_tags_for_note(
    note_id: str,
    x_af_stack_tenant_id: Optional[str] = Header(default=None),
    x_af_stack_user_id: Optional[str] = Header(default=None),
) -> Note:
    """Ask the suggest_tags agent for tags, union them onto the row."""
    tenant_id, user_id = _require_tenant(x_af_stack_tenant_id, x_af_stack_user_id)
    note = await get_note(note_id, x_af_stack_tenant_id, x_af_stack_user_id)

    result = await _call_agent(
        "notable-ai.suggest_tags",
        {"body": note.body, "user_id": user_id, "tenant_id": tenant_id},
        tenant_id=tenant_id,
    )
    suggested = [t for t in (result or {}).get("tags") or [] if isinstance(t, str)]
    merged: list[str] = []
    seen: set[str] = set()
    for t in suggested + note.tags:
        if t and t not in seen:
            merged.append(t)
            seen.add(t)

    async with _tenant_conn(tenant_id) as conn:
        row = (
            await (
                await conn.execute(
                    "UPDATE notable_notes SET tags = %s WHERE id = %s RETURNING *",
                    (merged, note_id),
                )
            ).fetchone()
        )
    if not row:
        raise HTTPException(status_code=404, detail={"code": "NOT_FOUND"})
    return _row_to_note(row)


@app.get("/notable/stats")
async def stats(
    x_af_stack_tenant_id: Optional[str] = Header(default=None),
) -> dict[str, Any]:
    """Per-tenant note count + a token-usage snapshot from the billing
    module. The dashboard plugin reads this.

    This endpoint deliberately bypasses RLS via the admin connection
    because operators want to see cross-tenant numbers. In production
    you'd put dashboard auth in front of it.
    """
    async with _admin_conn() as conn:
        rows = (
            await (
                await conn.execute(
                    """
                    SELECT tenant_id, count(*) AS note_count
                    FROM notable_notes
                    GROUP BY tenant_id
                    ORDER BY note_count DESC
                    """
                )
            ).fetchall()
        )

    # Token usage comes from the runtime's billing meters list. We don't
    # try to be clever about period boundaries — the dashboard picks the
    # window.
    assert _runtime_client is not None
    token_usd_by_tenant: dict[str, float] = {}
    try:
        meters_resp = await _runtime_client.get("/api/v1/billing/meters")
        if meters_resp.status_code == 200:
            for m in (meters_resp.json() or {}).get("meters") or []:
                tid = m.get("tenant_id") or ""
                cost = m.get("cost_usd") or 0
                if tid and isinstance(cost, (int, float)):
                    token_usd_by_tenant[tid] = token_usd_by_tenant.get(tid, 0.0) + float(cost)
    except httpx.RequestError:
        pass

    return {
        "tenants": [
            {
                "tenant_id": r["tenant_id"],
                "note_count": int(r["note_count"]),
                "cost_usd": token_usd_by_tenant.get(r["tenant_id"], 0.0),
            }
            for r in rows
        ],
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }


# ─── Internals ───────────────────────────────────────────────────────────


def _row_to_note(row: dict[str, Any]) -> Note:
    return Note(
        id=str(row["id"]),
        tenant_id=row["tenant_id"],
        user_id=row["user_id"],
        title=row["title"],
        body=row["body"],
        tags=list(row.get("tags") or []),
        tldr=row.get("tldr"),
        created_at=row["created_at"].isoformat(),
        updated_at=row["updated_at"].isoformat(),
    )


@app.exception_handler(HTTPException)
async def _http_exception_handler(_request: Request, exc: HTTPException) -> JSONResponse:
    """Pin the error envelope shape so the dashboard can rely on it."""
    detail = exc.detail
    if isinstance(detail, dict):
        body = {"error": detail}
    else:
        body = {"error": {"code": "ERROR", "message": str(detail)}}
    return JSONResponse(status_code=exc.status_code, content=body)


if __name__ == "__main__":  # pragma: no cover
    import uvicorn

    uvicorn.run(
        "notes:app",
        host="0.0.0.0",
        port=int(os.getenv("NOTABLE_API_PORT", "8090")),
        log_level=os.getenv("LOG_LEVEL", "info"),
    )