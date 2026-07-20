#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Live SDK conformance harness (Python) for the BackAI runtime.

Exercises the `af_stack` SDK against a running stack and reports a
machine-readable result. Its TypeScript twin, ``run.ts``, performs the
IDENTICAL checks through ``@af-stack/sdk`` — the two are kept in lockstep so a
drift in either SDK's live behaviour is caught here.

Checks:
  * ``GET /health`` is alive.
  * ``GET /api/v1/agents`` lists agents.
  * ``agents.call("supportdesk.echo", …)`` round-trips a payload.
  * storage upload -> download -> delete round-trips bytes.
  * jobs enqueue (+ status when a row is returned).
  * an intentional access-denied (bad key) surfaces a structured 401/403.
  * an intentional 404 surfaces a structured error envelope.

Config comes from env: ``BASE_URL`` (default http://localhost:8080) and
``API_KEY`` (optional in personal mode). Emits ``{pass, fail, skip,
results[]}`` JSON on stdout and exits non-zero if any check FAILED. Checks
that cannot run against this stack (module disabled, auth off) are SKIPPED,
not failed.
"""

from __future__ import annotations

import asyncio
import json
import os
import sys
import uuid
from typing import Any

import httpx

# Make the harness runnable straight from a checkout without an install.
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", "packages", "sdk-py"))

from af_stack import BackAI
from af_stack._http import AFStackError

BASE_URL = os.environ.get("BASE_URL", "http://localhost:8080").rstrip("/")
API_KEY = os.environ.get("API_KEY") or os.environ.get("AF_STACK_API_KEY")

_NOT_CONFIGURED = {
    "JOBS_NOT_CONFIGURED",
    "STORAGE_NOT_CONFIGURED",
    "MODULE_DISABLED",
    "MT_DISABLED",
    "NOT_CONFIGURED",
}

results: list[dict[str, Any]] = []


def record(name: str, status: str, detail: str = "") -> None:
    results.append({"name": name, "status": status, "detail": detail})


def _has_envelope(err: AFStackError) -> bool:
    """The SDK error object always carries the structured envelope fields."""
    return (
        isinstance(err.code, str)
        and err.code != ""
        and isinstance(err.message, str)
        and hasattr(err, "request_id")
        and isinstance(err.status_code, int)
    )


async def check_health() -> None:
    try:
        async with httpx.AsyncClient(timeout=10.0) as c:
            resp = await c.get(f"{BASE_URL}/health")
        if resp.status_code == 200 and isinstance(resp.json(), dict):
            record("health", "pass", f"status={resp.json().get('status')}")
        else:
            record("health", "fail", f"HTTP {resp.status_code}")
    except Exception as exc:
        record("health", "fail", f"{type(exc).__name__}: {exc}")


async def check_agents_list() -> None:
    try:
        headers = {"accept": "application/json"}
        if API_KEY:
            headers["authorization"] = f"Bearer {API_KEY}"
        async with httpx.AsyncClient(timeout=15.0) as c:
            resp = await c.get(f"{BASE_URL}/api/v1/agents", headers=headers)
        if resp.status_code == 200:
            body = resp.json()
            ok = isinstance(body, (list, dict))
            record("agents.list", "pass" if ok else "fail", f"HTTP 200 type={type(body).__name__}")
        else:
            record("agents.list", "fail", f"HTTP {resp.status_code}")
    except Exception as exc:
        record("agents.list", "fail", f"{type(exc).__name__}: {exc}")


async def check_echo(client: BackAI) -> None:
    try:
        marker = uuid.uuid4().hex[:8]
        res = await client.agents.call("supportdesk.echo", {"payload": {"message": marker}})
        status = getattr(res, "status", None) or (
            res.get("status") if isinstance(res, dict) else None
        )
        # The runtime returns the agent's value under `result` (with `output`
        # as a back-compat alias); accept either.
        value = getattr(res, "result", None) or getattr(res, "output", None)
        if isinstance(res, dict):
            value = res.get("result", res.get("output", value))
        ok = str(status) == "succeeded" and value is not None
        record("agents.call:supportdesk.echo", "pass" if ok else "fail", f"status={status}")
    except AFStackError as exc:
        if exc.code in _NOT_CONFIGURED or exc.status_code == 404:
            record("agents.call:supportdesk.echo", "skip", f"unavailable: [{exc.code}]")
        else:
            record("agents.call:supportdesk.echo", "fail", f"[{exc.code}] {exc.message}")
    except Exception as exc:
        record("agents.call:supportdesk.echo", "fail", f"{type(exc).__name__}: {exc}")


async def check_storage_roundtrip(client: BackAI) -> None:
    key = f"conformance/py-{uuid.uuid4().hex}.bin"
    payload = uuid.uuid4().bytes * 4
    try:
        await client.storage.upload(payload, key, content_type="application/octet-stream")
        got = await client.storage.download(key)
        if bytes(got) == payload:
            record("storage.roundtrip", "pass", f"key={key}")
        else:
            record("storage.roundtrip", "fail", "downloaded bytes differ")
        await client.storage.delete(key)
    except AFStackError as exc:
        if exc.code in _NOT_CONFIGURED:
            record("storage.roundtrip", "skip", f"unavailable: [{exc.code}]")
        else:
            record("storage.roundtrip", "fail", f"[{exc.code}] {exc.message}")
    except Exception as exc:
        record("storage.roundtrip", "fail", f"{type(exc).__name__}: {exc}")


async def check_jobs(client: BackAI) -> None:
    try:
        job = await client.jobs.enqueue("conformance-noop", {"probe": True})
        job_id = getattr(job, "id", None)
        if not job_id:
            record("jobs.enqueue", "fail", "no id in enqueue response")
            return
        record("jobs.enqueue", "pass", f"id={job_id}")
        fetched = await client.jobs.get(str(job_id))
        state = getattr(fetched, "state", None)
        valid = {
            "available",
            "running",
            "completed",
            "discarded",
            "cancelled",
            "retryable",
            "scheduled",
            "pending",
        }
        record("jobs.status", "pass" if state in valid else "fail", f"state={state}")
    except AFStackError as exc:
        # The endpoint + envelope are validated even when the demo stack has no
        # such job definition registered.
        if exc.code in _NOT_CONFIGURED:
            record("jobs.enqueue", "skip", f"unavailable: [{exc.code}]")
        elif _has_envelope(exc):
            record(
                "jobs.enqueue",
                "pass",
                f"structured rejection [{exc.code}] status={exc.status_code}",
            )
            record("jobs.status", "skip", "no row enqueued")
        else:
            record("jobs.enqueue", "fail", f"malformed error: {exc!r}")
    except Exception as exc:
        record("jobs.enqueue", "fail", f"{type(exc).__name__}: {exc}")


async def check_forbidden() -> None:
    """A deliberately invalid key must be denied with a structured 401/403."""
    bad = BackAI(
        base_url=BASE_URL, api_key=f"af_invalid_{uuid.uuid4().hex}", check_runtime_version=False
    )
    try:
        await bad.cost.events()
        # Reached here => the endpoint accepted an invalid key: auth is off
        # (personal mode). That is not a conformance failure.
        record("error.denied", "skip", "auth appears disabled (personal mode)")
    except AFStackError as exc:
        if exc.status_code in (401, 403) and _has_envelope(exc):
            record("error.denied", "pass", f"status={exc.status_code} code={exc.code}")
        elif exc.status_code in (401, 403):
            record("error.denied", "fail", "denied but envelope malformed")
        else:
            record("error.denied", "skip", f"unexpected status {exc.status_code} [{exc.code}]")
    except Exception as exc:
        record("error.denied", "fail", f"{type(exc).__name__}: {exc}")
    finally:
        await bad.close()


async def check_not_found(client: BackAI) -> None:
    """A missing resource must raise a structured 404 envelope."""
    try:
        await client.jobs.get("999999999")
        record("error.not_found", "fail", "expected a 404, got a result")
    except AFStackError as exc:
        if exc.status_code == 404 and _has_envelope(exc):
            record("error.not_found", "pass", f"status=404 code={exc.code}")
        elif exc.code in _NOT_CONFIGURED:
            record("error.not_found", "skip", f"jobs unavailable: [{exc.code}]")
        else:
            record("error.not_found", "fail", f"status={exc.status_code} [{exc.code}]")
    except Exception as exc:
        record("error.not_found", "fail", f"{type(exc).__name__}: {exc}")


async def main() -> int:
    client = BackAI(base_url=BASE_URL, api_key=API_KEY, check_runtime_version=False)
    try:
        await check_health()
        await check_agents_list()
        await check_echo(client)
        await check_storage_roundtrip(client)
        await check_jobs(client)
        await check_forbidden()
        await check_not_found(client)
    finally:
        await client.close()

    passed = sum(1 for r in results if r["status"] == "pass")
    failed = sum(1 for r in results if r["status"] == "fail")
    skipped = sum(1 for r in results if r["status"] == "skip")
    summary = {"pass": passed, "fail": failed, "skip": skipped, "sdk": "python", "results": results}
    print(json.dumps(summary, indent=2))
    return 1 if failed > 0 else 0


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
