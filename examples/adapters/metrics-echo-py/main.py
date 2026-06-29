"""
BackAI Metrics v1 Adapter — Echo Reference Implementation

Minimal adapter for conformance checks. It returns a synthetic `up{}` sample
and empty envelopes for other PromQL.
"""

import os
import time
from datetime import datetime, timezone
from typing import Optional

from fastapi import FastAPI, Header, HTTPException
from pydantic import BaseModel, Field

ADAPTER_TOKEN = os.getenv("BACKAI_ADAPTER_TOKEN", "").strip()
REQUIRE_AUTH = bool(ADAPTER_TOKEN)
START_TIME = time.time()


class QueryRequest(BaseModel):
    query: str
    time: Optional[str] = None


class RangeRequest(BaseModel):
    query: str
    from_: str = Field(alias="from")
    to: str
    step: str = "1m"


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="milliseconds").replace("+00:00", "Z")


async def verify_token(authorization: Optional[str] = Header(None)):
    if not REQUIRE_AUTH:
        return
    if not authorization or not authorization.startswith("Bearer "):
        raise HTTPException(status_code=401, detail="Missing Authorization header")
    if authorization[7:] != ADAPTER_TOKEN:
        raise HTTPException(status_code=401, detail="Invalid token")


app = FastAPI(title="BackAI Metrics v1 Echo Adapter")


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
        "name": "metrics-echo",
        "version": "1.0.0",
        "slot": "metrics",
        "protocol_version": "v1",
        "vendor": "BackAI",
        "homepage": "https://github.com/Agent-Field/backai/examples/adapters/metrics-echo-py",
        "capabilities": {
            "supports_instant_query": True,
            "supports_range_query": True,
            "supports_container_metrics": False,
            "native_query_lang": "promql",
            "retention_hours": 0,
            "max_series_per_query": 100,
        },
    }


@app.get("/v1/info")
async def info(authorization: Optional[str] = Header(None)):
    await verify_token(authorization)
    return {"docs": "https://github.com/Agent-Field/backai/blob/main/docs/adapters/protocols/metrics-v1.md"}


@app.post("/v1/metrics/query")
async def post_query_metrics(body: QueryRequest, authorization: Optional[str] = Header(None)):
    await verify_token(authorization)
    return instant_envelope(body.query, body.time)


@app.post("/v1/metrics/query_range")
async def post_range_metrics(body: RangeRequest, authorization: Optional[str] = Header(None)):
    await verify_token(authorization)
    return range_envelope(body.query, body.from_, body.to)


def instant_envelope(query: str, at: Optional[str]):
    if query.strip() != "up{}":
        return {"samples": []}
    return {
        "samples": [
            {
                "metric": {"__name__": "up", "job": "metrics-echo"},
                "value": 1,
                "ts": at or now_iso(),
            }
        ]
    }


def range_envelope(query: str, from_: str, to: str):
    if query.strip() != "up{}":
        return {"series": []}
    return {
        "series": [
            {
                "metric": {"__name__": "up", "job": "metrics-echo"},
                "values": [
                    {"ts": from_, "value": 1},
                    {"ts": to, "value": 1},
                ],
            }
        ]
    }
