"""
BackAI Metrics v1 Adapter — Echo Reference Implementation

Minimal adapter for conformance checks. It returns a synthetic `up{}` sample
and empty envelopes for other PromQL.
"""

import os
import time
from datetime import datetime, timezone
from typing import Optional

from fastapi import FastAPI, Header, HTTPException, Query

ADAPTER_TOKEN = os.getenv("BACKAI_ADAPTER_TOKEN", "").strip()
REQUIRE_AUTH = bool(ADAPTER_TOKEN)
START_TIME = time.time()


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


@app.get("/v1/metrics/query")
async def query_metrics(promql: str = Query(...), at: Optional[str] = None, authorization: Optional[str] = Header(None)):
    await verify_token(authorization)
    if promql.strip() == "up{}":
        return {
            "samples": [
                {
                    "metric": {"__name__": "up", "job": "metrics-echo"},
                    "value": 1,
                    "ts": at or now_iso(),
                }
            ]
        }
    return {"samples": []}


@app.get("/v1/metrics/range")
async def range_metrics(
    promql: str = Query(...),
    from_: str = Query(..., alias="from"),
    to: str = Query(...),
    step: str = Query("1m"),
    authorization: Optional[str] = Header(None),
):
    await verify_token(authorization)
    if promql.strip() == "up{}":
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
    return {"series": []}
