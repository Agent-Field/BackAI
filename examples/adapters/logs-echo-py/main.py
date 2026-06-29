"""
BackAI Logs v1 Adapter — Echo Reference Implementation

Minimal adapter for conformance checks. It returns a synthetic log line from
query and streams one synthetic line over SSE for tail.
"""

import json
import os
import time
from datetime import datetime, timezone
from typing import Optional

from fastapi import FastAPI, Header, HTTPException
from fastapi.responses import JSONResponse, StreamingResponse
from pydantic import BaseModel, Field

ADAPTER_TOKEN = os.getenv("BACKAI_ADAPTER_TOKEN", "").strip()
REQUIRE_AUTH = bool(ADAPTER_TOKEN)
START_TIME = time.time()
SUPPORTS_TAIL = os.getenv("BACKAI_LOGS_SUPPORTS_TAIL", "true").lower() not in {"0", "false", "no"}


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="milliseconds").replace("+00:00", "Z")


async def verify_token(authorization: Optional[str] = Header(None)):
    if not REQUIRE_AUTH:
        return
    if not authorization or not authorization.startswith("Bearer "):
        raise HTTPException(status_code=401, detail="Missing Authorization header")
    if authorization[7:] != ADAPTER_TOKEN:
        raise HTTPException(status_code=401, detail="Invalid token")


class LogEntry(BaseModel):
    ts: str = Field(default_factory=now_iso)
    level: str = "info"
    service: str = "logs-echo"
    msg: str = "synthetic log line from logs-echo-py"
    agent: Optional[str] = None
    tenant_id: Optional[str] = None
    request_id: Optional[str] = None
    trace_id: Optional[str] = None
    fields: dict = Field(default_factory=dict)


class LogFilter(BaseModel):
    services: list[str] = Field(default_factory=list)
    levels: list[str] = Field(default_factory=list)
    tenant_id: Optional[str] = None
    request_id: Optional[str] = None
    trace_id: Optional[str] = None
    search: Optional[str] = None
    search_is_regex: bool = False
    limit: int = 200
    cursor: Optional[str] = None


class LogPage(BaseModel):
    entries: list[LogEntry]
    next_cursor: str = ""
    has_more: bool = False


app = FastAPI(title="BackAI Logs v1 Echo Adapter")


@app.get("/health")
@app.get("/healthz")
async def healthz(authorization: Optional[str] = Header(None)):
    await verify_token(authorization)
    return {
        "status": "healthy",
        "started_at": datetime.fromtimestamp(START_TIME, tz=timezone.utc).isoformat(timespec="milliseconds").replace("+00:00", "Z"),
        "uptime_seconds": int(time.time() - START_TIME),
        "dependencies": [],
    }


@app.get("/v1/capabilities")
async def capabilities(authorization: Optional[str] = Header(None)):
    await verify_token(authorization)
    return {
        "name": "logs-echo",
        "version": "1.0.0",
        "slot": "logs",
        "protocol_version": "v1",
        "vendor": "BackAI",
        "homepage": "https://github.com/Agent-Field/backai/examples/adapters/logs-echo-py",
        "capabilities": {
            "supports_tail": SUPPORTS_TAIL,
            "supports_full_text": True,
            "supports_regex_search": False,
            "supports_trace_id": True,
            "native_query_lang": "",
            "retention_days": 0,
            "max_entries_per_page": 1000,
        },
    }


@app.get("/v1/info")
async def info(authorization: Optional[str] = Header(None)):
    await verify_token(authorization)
    return {"docs": "https://github.com/Agent-Field/backai/blob/main/docs/adapters/protocols/logs-v1.md"}


@app.post("/v1/logs/query")
async def query_logs(filter: LogFilter, authorization: Optional[str] = Header(None)):
    await verify_token(authorization)
    entry = LogEntry(
        level=(filter.levels[0] if filter.levels else "info"),
        service=(filter.services[0] if filter.services else "logs-echo"),
        msg=filter.search or "synthetic log line from logs-echo-py",
        tenant_id=filter.tenant_id,
        request_id=filter.request_id,
        trace_id=filter.trace_id,
        fields={"adapter": "logs-echo"},
    )
    return LogPage(entries=[entry])


@app.get("/v1/logs/tail")
async def tail_logs(authorization: Optional[str] = Header(None)):
    await verify_token(authorization)
    if not SUPPORTS_TAIL:
        return JSONResponse(
            status_code=422,
            content={
                "type": "https://docs.backai.dev/errors/logs/unsupported-capability",
                "title": "Unsupported capability",
                "status": 422,
                "detail": "tail is disabled",
                "code": "unsupported_capability",
            },
        )

    async def events():
        entry = LogEntry(fields={"adapter": "logs-echo"})
        yield f"data: {entry.model_dump_json()}\n\n"
        yield "data: [DONE]\n\n"

    return StreamingResponse(events(), media_type="text/event-stream")
