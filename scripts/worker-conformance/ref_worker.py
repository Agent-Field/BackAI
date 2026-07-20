#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Reference pull-worker (Python) for the worker-conformance suite.

Registers a handler for every vector `kind` in ``spec.json`` — keyed by the
vector's ``behavior`` — and runs the ``af_stack.worker.Worker`` lease loop.
``run.sh`` starts this process, enqueues the vector jobs, and asserts their
terminal states. Its TypeScript twin, ``ref_worker.ts``, is byte-for-byte
equivalent in behaviour so the same vectors pass against either worker.

Env: ``BASE_URL`` (default http://localhost:8080), ``API_KEY`` (a tenant key
with the ``jobs:work`` scope), ``SPEC`` (path to spec.json).
"""

from __future__ import annotations

import json
import os
import sys
import time
from pathlib import Path
from typing import Any

# Runnable straight from a checkout without an install.
sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "packages" / "sdk-py"))

from af_stack.worker import JobContext, PermanentError, Worker

BASE_URL = os.environ.get("BASE_URL", "http://localhost:8080").rstrip("/")
API_KEY = os.environ.get("API_KEY") or os.environ.get("AF_STACK_API_KEY") or ""
SPEC = os.environ.get("SPEC") or str(Path(__file__).with_name("spec.json"))


def _deep_equal(a: Any, b: Any) -> bool:
    return json.dumps(a, sort_keys=True) == json.dumps(b, sort_keys=True)


def make_handler(behavior: str, expected_payload: dict[str, Any]):
    if behavior == "complete":

        def handler(payload: dict[str, Any], ctx: JobContext) -> dict[str, Any]:
            ctx.log("conf: complete", fields={"kind": ctx.job_id})
            return {"ok": True}

    elif behavior == "retry_then_complete":

        def handler(payload: dict[str, Any], ctx: JobContext) -> dict[str, Any]:
            if ctx.attempt < 2:
                raise RuntimeError(f"retryable failure on attempt {ctx.attempt}")
            return {"ok": True, "attempt": ctx.attempt}

    elif behavior == "permanent":

        def handler(payload: dict[str, Any], ctx: JobContext) -> dict[str, Any]:
            raise PermanentError("permanent failure — do not retry")

    elif behavior == "roundtrip":

        def handler(payload: dict[str, Any], ctx: JobContext) -> dict[str, Any]:
            if not _deep_equal(payload, expected_payload):
                raise PermanentError(
                    f"payload roundtrip mismatch: got {payload!r} want {expected_payload!r}"
                )
            return {"ok": True, "echoed": payload}

    elif behavior == "slow_complete":

        def handler(payload: dict[str, Any], ctx: JobContext) -> dict[str, Any]:
            sleep_ms = int(payload.get("sleep_ms", 0))
            deadline = time.monotonic() + sleep_ms / 1000.0
            while time.monotonic() < deadline:
                if ctx.is_canceled():
                    return {"ok": False, "canceled": True}
                time.sleep(0.1)
            return {"ok": True, "slept_ms": sleep_ms}

    else:
        raise ValueError(f"unknown behavior {behavior!r}")

    return handler


def main() -> int:
    if not API_KEY:
        print("ref_worker.py: API_KEY is required", file=sys.stderr)
        return 2
    spec = json.loads(Path(SPEC).read_text(encoding="utf-8"))
    wcfg = spec.get("worker", {})
    worker = Worker(
        BASE_URL,
        API_KEY,
        lease_ttl=int(wcfg.get("lease_ttl", 6)),
        heartbeat_interval=int(wcfg.get("heartbeat_interval", 2)),
        poll_wait=int(wcfg.get("poll_wait", 5)),
        worker_id="conf-ref-py",
    )
    for vec in spec["vectors"]:
        worker.register(vec["kind"])(make_handler(vec["behavior"], vec.get("payload", {})))
    print(f"ref_worker.py: leasing kinds {worker.kinds()} against {BASE_URL}", file=sys.stderr)
    worker.run()  # blocks; drains on SIGTERM/SIGINT
    return 0


if __name__ == "__main__":
    sys.exit(main())
