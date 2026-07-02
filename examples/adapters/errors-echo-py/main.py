"""Reference errors-v1 adapter.

Run:
    uvicorn main:app --host 0.0.0.0 --port 18091
"""

from __future__ import annotations

from datetime import UTC, datetime
from typing import Any

from fastapi import FastAPI, Header, HTTPException
from pydantic import BaseModel, Field

app = FastAPI(title="BackAI errors echo adapter")


class ListFilter(BaseModel):
    status: str = "open"
    service: str | None = None
    tenant_id: str | None = None
    since: str | None = None
    limit: int = 50
    cursor: str | None = None


class Update(BaseModel):
    status: str


class Group(BaseModel):
    id: str = "echo-error"
    title: str = "Synthetic error group"
    service: str = "errors-echo"
    status: str = "open"
    count: int = 1
    user_count: int = 0
    first_seen: str = Field(default_factory=lambda: datetime.now(UTC).isoformat())
    last_seen: str = Field(default_factory=lambda: datetime.now(UTC).isoformat())
    fingerprint: str = "echo-error"
    culprit: str = "examples/adapters/errors-echo-py/main.py"
    sample_event: dict[str, Any] = Field(default_factory=lambda: {"adapter": "errors-echo"})


GROUP = Group()


@app.get("/healthz")
async def healthz():
    return {"status": "healthy"}


@app.get("/v1/capabilities")
async def capabilities(authorization: str | None = Header(None)):
    return {
        "name": "errors-echo",
        "version": "0.1.0",
        "slot": "errors",
        "protocol_version": "v1",
        "vendor": "backai",
        "homepage": "https://github.com/Agent-Field/backai/examples/adapters/errors-echo-py",
        "capabilities": {
            "supports_list": True,
            "supports_get": True,
            "supports_mute": True,
            "supports_resolve": True,
            "supports_ingest": False,
            "supports_alerting": False,
            "native_query_lang": "echo",
            "retention_days": 0,
            "persistence": "remote",
            "max_groups_per_page": 50,
        },
    }


@app.get("/v1/info")
async def info():
    return {
        "docs": "https://github.com/Agent-Field/backai/blob/main/docs/adapters/protocols/errors-v1.md"
    }


@app.post("/v1/errors/list")
async def list_errors(filter: ListFilter, authorization: str | None = Header(None)):
    if filter.status and filter.status != GROUP.status:
        return {"groups": [], "has_more": False}
    return {"groups": [GROUP.model_dump()], "has_more": False}


@app.get("/v1/errors/{group_id}")
async def get_error(group_id: str, authorization: str | None = Header(None)):
    if group_id != GROUP.id:
        raise HTTPException(
            status_code=404,
            detail={
                "type": "https://docs.backai.dev/errors/errors/not-found",
                "title": "error_group_not_found",
                "status": 404,
                "code": "error_group_not_found",
            },
        )
    return GROUP.model_dump()


@app.patch("/v1/errors/{group_id}")
async def update_error(group_id: str, update: Update, authorization: str | None = Header(None)):
    if group_id != GROUP.id:
        raise HTTPException(status_code=404, detail={"code": "error_group_not_found"})
    if update.status not in {"open", "muted", "resolved"}:
        raise HTTPException(status_code=400, detail={"code": "invalid_error_status"})
    GROUP.status = update.status
    GROUP.last_seen = datetime.now(UTC).isoformat()
    return GROUP.model_dump()
