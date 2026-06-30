"""
BackAI Sandbox v1 Adapter — Echo Reference Implementation

A minimal, non-functional adapter that echoes back sandbox specs instead of
running containers. Demonstrates the protocol shape end-to-end.
"""

import json
import os
import time
import uuid
from datetime import UTC, datetime

from fastapi import FastAPI, Header, HTTPException, Request
from fastapi.responses import StreamingResponse
from pydantic import BaseModel, Field

# ============================================================================
# Config & Helpers
# ============================================================================

ADAPTER_TOKEN = os.getenv("BACKAI_ADAPTER_TOKEN", "").strip()
REQUIRE_AUTH = bool(ADAPTER_TOKEN)
START_TIME = time.time()


def get_now_iso() -> str:
    return datetime.now(UTC).isoformat(timespec="milliseconds").replace("+00:00", "Z")


async def verify_token(authorization: str | None = Header(None)):
    if not REQUIRE_AUTH:
        return
    if not authorization or not authorization.startswith("Bearer "):
        raise HTTPException(status_code=401, detail="Missing or invalid Authorization header")
    if authorization[7:] != ADAPTER_TOKEN:
        raise HTTPException(status_code=401, detail="Invalid token")


# ============================================================================
# Models
# ============================================================================


class RunSpec(BaseModel):
    id: str
    tenant_id: str | None = None
    workspace_id: str | None = None
    image: str
    command: list[str]
    files: dict[str, str] | None = None
    env: dict[str, str] | None = None
    timeout_s: int
    cpu: int
    memory_gb: int
    network: str
    allow_egress: list[str] | None = None


class TerminalResult(BaseModel):
    status: str = "done"
    exit_code: int = 0
    duration_s: int = 1
    cpu_seconds: float = 0.0
    memory_peak_mb: int = 0
    network_bytes_in: int = 0
    network_bytes_out: int = 0
    stdout_url: str = ""
    stderr_url: str = ""
    artifacts_url: str = ""
    started_at: str = Field(default_factory=get_now_iso)
    ended_at: str = Field(default_factory=get_now_iso)


class HealthResponse(BaseModel):
    status: str = "healthy"
    started_at: str = Field(
        default_factory=lambda: datetime.fromtimestamp(START_TIME, tz=UTC)
        .isoformat(timespec="milliseconds")
        .replace("+00:00", "Z")
    )
    uptime_seconds: int = Field(default_factory=lambda: int(time.time() - START_TIME))
    dependencies: list[dict] = Field(default_factory=list)


class CapabilityResponse(BaseModel):
    name: str = "echo"
    version: str = "1.0.0"
    slot: str = "sandbox"
    protocol_version: str = "v1"
    vendor: str = "BackAI"
    homepage: str = "https://github.com/Agent-Field/backai/examples/adapters/sandbox-echo-py"
    capabilities: dict = Field(
        default_factory=lambda: {
            "max_timeout_s": 3600,
            "supports_gpu": False,
            "supports_network": True,
            "supports_mounts": True,
            "supports_streaming": True,
            "cold_start_ms": 10,
            "image_pull_required": False,
            "max_cpu": 64,
            "max_memory_gb": 128,
            "network_modes": ["open", "restricted", "isolated"],
            "allow_egress_supported": False,
            "artifacts_upload": False,
        }
    )


class InfoResponse(BaseModel):
    admin_ui: str = ""
    docs: str = (
        "https://github.com/Agent-Field/backai/blob/main/docs/adapters/protocols/sandbox-v1.md"
    )
    support_email: str = ""


class PoolStats(BaseModel):
    adapter: str = "echo"
    warm: int = 0
    active: int = 0
    queued: int = 0
    total_runs_today: int = 0
    cpu_seconds_today: float = 0.0
    cost_usd_today: float = 0.0


# ============================================================================
# Idempotency Cache (10 minute TTL)
# ============================================================================


class IdempotencyCache:
    def __init__(self):
        self.cache = {}
        self.expiry = {}

    def get(self, method: str, path: str, key: str):
        cache_key = (method, path, key)
        if cache_key in self.cache and time.time() < self.expiry[cache_key]:
            return self.cache[cache_key]
        if cache_key in self.cache:
            del self.cache[cache_key]
            del self.expiry[cache_key]
        return None

    def set(self, method: str, path: str, key: str, value):
        cache_key = (method, path, key)
        self.cache[cache_key] = value
        self.expiry[cache_key] = time.time() + 600


idempotency_cache = IdempotencyCache()

# ============================================================================
# FastAPI App
# ============================================================================

app = FastAPI(title="BackAI Sandbox v1 Echo Adapter")
_runs = {}


@app.get("/healthz")
async def healthz(authorization: str | None = Header(None)):
    await verify_token(authorization)
    return HealthResponse()


@app.get("/v1/capabilities")
async def capabilities(authorization: str | None = Header(None)):
    await verify_token(authorization)
    return CapabilityResponse()


@app.get("/v1/info")
async def info(authorization: str | None = Header(None)):
    await verify_token(authorization)
    return InfoResponse()


@app.post("/v1/runs")
async def post_runs(
    spec: RunSpec,
    authorization: str | None = Header(None),
    x_backai_idempotency_key: str | None = Header(None),
    x_backai_request_id: str | None = Header(None),
):
    await verify_token(authorization)

    if x_backai_idempotency_key:
        cached = idempotency_cache.get("POST", "/v1/runs", x_backai_idempotency_key)
        if cached:
            return cached

    result = TerminalResult()
    _runs[spec.id] = {"result": result, "stdout": f"echo: {' '.join(spec.command)}\n"}

    if x_backai_idempotency_key:
        idempotency_cache.set("POST", "/v1/runs", x_backai_idempotency_key, result)

    return result


@app.post("/v1/runs/stream")
async def post_runs_stream(
    spec: RunSpec,
    authorization: str | None = Header(None),
    x_backai_idempotency_key: str | None = Header(None),
    x_backai_request_id: str | None = Header(None),
):
    await verify_token(authorization)

    command_str = " ".join(spec.command)
    result = TerminalResult()
    _runs[spec.id] = {"result": result, "stdout": f"echo: {command_str}\n"}

    async def event_generator():
        log_event = {
            "ts": get_now_iso(),
            "stream": "stdout",
            "text": f"echoing: {command_str}\n",
        }
        yield f"data: {json.dumps(log_event)}\n\n"

        term_event = {
            "event": "terminated",
            "status": result.status,
            "exit_code": result.exit_code,
            "duration_s": result.duration_s,
            "cpu_seconds": result.cpu_seconds,
            "memory_peak_mb": result.memory_peak_mb,
            "network_bytes_in": result.network_bytes_in,
            "network_bytes_out": result.network_bytes_out,
            "stdout_url": result.stdout_url,
            "stderr_url": result.stderr_url,
            "artifacts_url": result.artifacts_url,
            "started_at": result.started_at,
            "ended_at": result.ended_at,
        }
        yield f"data: {json.dumps(term_event)}\n\n"

    return StreamingResponse(event_generator(), media_type="text/event-stream")


@app.get("/v1/runs/{run_id}")
async def get_run(
    run_id: str,
    authorization: str | None = Header(None),
    x_backai_request_id: str | None = Header(None),
):
    await verify_token(authorization)
    if run_id in _runs:
        return _runs[run_id]["result"]
    return TerminalResult()


@app.delete("/v1/runs/{run_id}", status_code=204)
async def delete_run(
    run_id: str,
    authorization: str | None = Header(None),
    x_backai_request_id: str | None = Header(None),
):
    await verify_token(authorization)
    pass


@app.get("/v1/pool")
async def pool(authorization: str | None = Header(None)):
    await verify_token(authorization)
    return PoolStats()


@app.exception_handler(HTTPException)
async def http_exception_handler(request: Request, exc: HTTPException):
    request_id = request.headers.get("X-BackAI-Request-Id", str(uuid.uuid4()))
    code_map = {
        401: "unauthorized",
        400: "invalid_spec",
        404: "run_not_found",
        422: "unsupported_capability",
        429: "quota_exceeded",
        503: "adapter_unavailable",
        500: "internal_error",
    }
    code = code_map.get(exc.status_code, "internal_error")
    return {
        "type": f"https://docs.backai.dev/errors/sandbox/{code}",
        "title": exc.detail or "An error occurred",
        "status": exc.status_code,
        "detail": exc.detail or "An error occurred",
        "code": code,
        "request_id": request_id,
        "retry_after_seconds": 0,
    }, exc.status_code


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(app, host="0.0.0.0", port=8090)  # noqa: S104 - example dev server
