"""``suite.sandbox.*`` — code execution sandbox operations.

Maps to the REST surface defined in ``apps/dashboard/src/lib/api.ts`` under
the Phase 9 Sandbox section:

==================================  ====================================
SDK call                            Endpoint
==================================  ====================================
:func:`run`                         ``POST   /api/v1/sandbox/run``
:func:`list`                        ``GET    /api/v1/sandbox/runs``
:func:`get`                         ``GET    /api/v1/sandbox/runs/{id}``
:func:`stop`                        ``DELETE /api/v1/sandbox/runs/{id}``
:func:`pool`                        ``GET    /api/v1/sandbox/pool``
==================================  ====================================

Sandboxes execute caller-supplied container images with bounded CPU,
memory, network, and time budgets. The runtime persists every run
(stdout/stderr/artifacts) and surfaces structured metrics so callers can
audit, replay, and bill execution.

The :class:`SandboxRun`, :class:`SandboxRunList`, :class:`SandboxPoolStats`,
and :class:`SandboxCapabilities` Pydantic models mirror the zod schemas in
``lib/api.ts`` exactly — keep them in sync when the contract changes.
"""

from __future__ import annotations

from collections.abc import Sequence
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

from . import _http

SandboxStatus = Literal["queued", "running", "done", "failed", "timeout", "killed"]
SandboxNetwork = Literal["open", "restricted", "isolated"]

_ALLOWED_NETWORKS: tuple[str, ...] = ("open", "restricted", "isolated")


class SandboxCapabilities(BaseModel):
    """Static capability descriptor reported by the active sandbox adapter.

    Mirrors ``SandboxCapabilitiesSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    max_timeout_s: int
    supports_gpu: bool
    supports_network: bool
    supports_mounts: bool
    cold_start_ms: int
    adapter: str


class SandboxRun(BaseModel):
    """One sandbox execution row.

    Mirrors ``SandboxRunSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    id: str
    tenant_id: str | None = None
    workspace_id: str | None = None
    adapter: str
    image: str
    command: list[str] = Field(default_factory=list)
    status: SandboxStatus
    exit_code: int | None = None
    duration_s: float | None = None
    cpu_seconds: float | None = None
    memory_peak_mb: float | None = None
    network_bytes_in: int | None = None
    network_bytes_out: int | None = None
    cost_usd: float | None = None
    stdout_url: str | None = None
    stderr_url: str | None = None
    artifacts_url: str | None = None
    started_at: str | None = None
    ended_at: str | None = None
    created_at: str


class SandboxRunList(BaseModel):
    """Paginated list of sandbox runs.

    Mirrors ``SandboxRunListSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    runs: list[SandboxRun] = Field(default_factory=list)
    total: int = 0
    has_more: bool = False


class SandboxPoolStats(BaseModel):
    """Live pool stats for the active sandbox adapter.

    Mirrors ``SandboxPoolStatsSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    adapter: str
    warm: int = 0
    active: int = 0
    queued: int = 0
    total_runs_today: int = 0
    cpu_seconds_today: float = 0.0
    cost_usd_today: float = 0.0
    capabilities: SandboxCapabilities


async def run(
    image: str,
    command: Sequence[str],
    *,
    files: dict[str, str] | None = None,
    env: dict[str, str] | None = None,
    timeout_s: int = 300,
    cpu: int = 2,
    memory_gb: int = 4,
    network: SandboxNetwork | str = "restricted",
    allow_egress: Sequence[str] | None = None,
    workspace_id: str | None = None,
) -> SandboxRun:
    """Submit a sandbox execution and return the persisted run row.

    ``image`` is a fully-qualified container image reference (e.g.
    ``python:3.12-slim`` or ``ghcr.io/foo/bar@sha256:...``).
    ``command`` is the ``argv`` to exec inside the container; the runtime
    does **not** wrap it in a shell — pass ``["sh", "-c", "..."]`` if you
    need shell semantics.

    ``files`` materialises caller-supplied files into the container's
    working directory before execution (path → text contents).
    ``env`` injects environment variables.

    ``timeout_s`` (1–86400), ``cpu`` (1–32), ``memory_gb`` (1–64) carry the
    wire defaults from ``SandboxRunInputSchema``; the runtime double-checks
    every bound. ``network`` defaults to ``"restricted"`` — the runtime
    honours ``allow_egress`` only when ``network != "isolated"``.
    """
    if not image:
        raise ValueError("sandbox image must be a non-empty string")
    # NOTE: `list` is shadowed below by our `list()` helper, so isinstance must
    # reach the builtin via its concrete identity (a tuple). The runtime
    # contract requires a non-empty argv with string entries.
    if not hasattr(command, "__iter__") or isinstance(command, (str, bytes)):
        raise ValueError("sandbox command must be a non-empty list of strings")
    command_args = [c for c in command]
    if len(command_args) == 0:
        raise ValueError("sandbox command must be a non-empty list of strings")
    for arg in command_args:
        if not isinstance(arg, str):
            raise ValueError("sandbox command entries must all be strings")
    if timeout_s < 1 or timeout_s > 86400:
        raise ValueError("timeout_s must be between 1 and 86400")
    if cpu < 1 or cpu > 32:
        raise ValueError("cpu must be between 1 and 32")
    if memory_gb < 1 or memory_gb > 64:
        raise ValueError("memory_gb must be between 1 and 64")
    if network not in _ALLOWED_NETWORKS:
        allowed = ", ".join(_ALLOWED_NETWORKS)
        raise ValueError(f"network must be one of: {allowed}; got {network!r}")

    payload: dict[str, Any] = {
        "image": image,
        "command": command_args,
        "timeout_s": timeout_s,
        "cpu": cpu,
        "memory_gb": memory_gb,
        "network": network,
    }
    if files is not None:
        payload["files"] = dict(files)
    if env is not None:
        payload["env"] = dict(env)
    if allow_egress is not None:
        payload["allow_egress"] = [str(host) for host in allow_egress]
    if workspace_id is not None:
        payload["workspace_id"] = workspace_id

    body = await _http.request_json("POST", "/sandbox/run", json=payload)
    return SandboxRun.model_validate(body or {})


async def list(  # noqa: A001 — mirrors the JS `sandbox.list()` ergonomics
    *,
    tenant: str | None = None,
    status: SandboxStatus | str | None = None,
    limit: int = 50,
    offset: int = 0,
) -> SandboxRunList:
    """List sandbox runs with optional filters.

    Filter parameter names match the dashboard API (``tenant``, ``status``).
    """
    if limit <= 0:
        raise ValueError("limit must be a positive integer")
    if offset < 0:
        raise ValueError("offset must be non-negative")
    params: dict[str, Any] = {"limit": limit, "offset": offset}
    if tenant is not None:
        params["tenant"] = tenant
    if status is not None:
        params["status"] = status
    body = await _http.request_json("GET", "/sandbox/runs", params=params)
    return SandboxRunList.model_validate(
        body or {"runs": [], "total": 0, "has_more": False}
    )


async def get(id: str) -> SandboxRun:
    """Fetch a sandbox run by id."""
    if not id:
        raise ValueError("sandbox run id must be a non-empty string")
    body = await _http.request_json("GET", f"/sandbox/runs/{id}")
    return SandboxRun.model_validate(body or {})


async def stop(id: str) -> bool:
    """Stop a running sandbox. Returns ``True`` on success.

    The runtime sends ``SIGTERM`` then ``SIGKILL`` after a grace period;
    the persisted run row's final ``status`` becomes ``"killed"``.
    """
    if not id:
        raise ValueError("sandbox run id must be a non-empty string")
    body = await _http.request_json("DELETE", f"/sandbox/runs/{id}")
    if not isinstance(body, dict):
        return True
    return bool(body.get("stopped", True))


async def pool() -> SandboxPoolStats:
    """Return live pool stats for the active sandbox adapter."""
    body = await _http.request_json("GET", "/sandbox/pool")
    return SandboxPoolStats.model_validate(body or {})


__all__ = [
    "SandboxCapabilities",
    "SandboxNetwork",
    "SandboxPoolStats",
    "SandboxRun",
    "SandboxRunList",
    "SandboxStatus",
    "get",
    "list",
    "pool",
    "run",
    "stop",
]
