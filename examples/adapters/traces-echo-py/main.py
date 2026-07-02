"""
BackAI Traces v1 Adapter — Echo Reference Implementation

Minimal adapter for conformance checks. It returns one synthetic trace from
search and a stable trace detail for /v1/traces/trace_echo.
"""

import os
import time
from datetime import UTC, datetime

from fastapi import FastAPI, Header, HTTPException
from fastapi.responses import JSONResponse
from pydantic import BaseModel, Field

ADAPTER_TOKEN = os.getenv("BACKAI_ADAPTER_TOKEN", "").strip()
REQUIRE_AUTH = bool(ADAPTER_TOKEN)
START_TIME = time.time()


def now_iso() -> str:
    return datetime.now(UTC).isoformat(timespec="milliseconds").replace("+00:00", "Z")


async def verify_token(authorization: str | None = Header(None)):
    if not REQUIRE_AUTH:
        return
    if not authorization or not authorization.startswith("Bearer "):
        raise HTTPException(status_code=401, detail="Missing Authorization header")
    if authorization[7:] != ADAPTER_TOKEN:
        raise HTTPException(status_code=401, detail="Invalid token")


class SearchFilter(BaseModel):
    service: str | None = None
    operation: str | None = None
    tag: dict[str, str] = Field(default_factory=dict)
    min_duration: str | None = None
    max_duration: str | None = None
    status: str | None = None
    from_: str | None = Field(default=None, alias="from")
    to: str | None = None
    limit: int = 20


class TraceSummary(BaseModel):
    trace_id: str = "trace_echo"
    root_service: str = "traces-echo"
    root_operation: str = "synthetic trace"
    start_time: str = Field(default_factory=now_iso)
    duration_ms: int = 42
    span_count: int = 1
    status: str = "ok"


class SpanEvent(BaseModel):
    ts: str = Field(default_factory=now_iso)
    name: str = "echo.event"
    attributes: dict = Field(default_factory=dict)


class Span(BaseModel):
    span_id: str = "0000000000000001"
    parent_span_id: str = ""
    service: str = "traces-echo"
    operation: str = "synthetic trace"
    start_time: str = Field(default_factory=now_iso)
    duration_ms: int = 42
    status: str = "ok"
    attributes: dict = Field(default_factory=lambda: {"adapter": "traces-echo"})
    events: list[SpanEvent] = Field(default_factory=lambda: [SpanEvent()])


app = FastAPI(title="BackAI Traces v1 Echo Adapter")


@app.get("/health")
@app.get("/healthz")
async def healthz(authorization: str | None = Header(None)):
    await verify_token(authorization)
    return {
        "status": "healthy",
        "started_at": datetime.fromtimestamp(START_TIME, tz=UTC)
        .isoformat(timespec="milliseconds")
        .replace("+00:00", "Z"),
        "uptime_seconds": int(time.time() - START_TIME),
        "dependencies": [],
    }


@app.get("/v1/capabilities")
async def capabilities(authorization: str | None = Header(None)):
    await verify_token(authorization)
    return {
        "name": "traces-echo",
        "version": "1.0.0",
        "slot": "traces",
        "protocol_version": "v1",
        "vendor": "BackAI",
        "homepage": "https://github.com/Agent-Field/backai/examples/adapters/traces-echo-py",
        "capabilities": {
            "supports_traceql": True,
            "supports_tag_search": True,
            "native_query_lang": "traceql",
            "retention_hours": 0,
            "max_results_per_query": 1000,
        },
    }


@app.get("/v1/info")
async def info(authorization: str | None = Header(None)):
    await verify_token(authorization)
    return {
        "docs": "https://github.com/Agent-Field/backai/blob/main/docs/adapters/protocols/traces-v1.md"
    }


@app.post("/v1/traces/search")
async def search_traces(filter: SearchFilter, authorization: str | None = Header(None)):
    await verify_token(authorization)
    service = filter.service or "traces-echo"
    operation = filter.operation or "synthetic trace"
    status = filter.status or "ok"
    return {
        "traces": [
            TraceSummary(root_service=service, root_operation=operation, status=status).model_dump()
        ],
        "has_more": False,
    }


@app.get("/v1/traces/{trace_id}")
async def get_trace(trace_id: str, authorization: str | None = Header(None)):
    await verify_token(authorization)
    if trace_id != "trace_echo":
        return JSONResponse(
            status_code=404,
            content={
                "type": "https://docs.backai.dev/errors/traces/not-found",
                "title": "Trace not found",
                "status": 404,
                "detail": "trace not found",
                "code": "trace_not_found",
            },
        )
    return {"trace_id": "trace_echo", "spans": [Span().model_dump()]}
