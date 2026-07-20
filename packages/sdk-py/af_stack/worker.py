# SPDX-License-Identifier: Apache-2.0

"""``af_stack.worker`` — run language-neutral background jobs in Python.

A :class:`Worker` is a long-running process that PULLS remote (python) job
definitions from the AF Stack runtime and executes them. It is the Python
half of the pull-based worker protocol (PRD R3); the runtime side lives at
``/api/v1/jobs/worker/*`` and keeps River as the durable queue.

    from af_stack.worker import Worker

    worker = Worker("http://localhost:8080", "af_live_...")

    @worker.register("resize-image")
    def resize(payload, ctx):
        ctx.log("resizing", url=payload["url"])
        if ctx.is_canceled():
            return
        return {"thumbnail": do_resize(payload["url"])}

    worker.run()  # blocks; drains gracefully on SIGTERM/SIGINT

Handlers receive ``(payload, ctx)`` where ``ctx`` is a :class:`JobContext`
carrying ``tenant_id``, ``job_id``, ``attempt``, ``deadline``, a structured
:meth:`JobContext.log` helper, and :meth:`JobContext.is_canceled`.

Failure semantics:
  * a handler that returns normally -> the job COMPLETES with its return value.
  * a handler that raises :class:`PermanentError` -> the job FAILS permanently
    (River will not retry — it is dead-lettered).
  * a handler that raises any other exception -> the job FAILS *retryably*
    (River retries with backoff, up to the job's max attempts).

The worker authenticates with a tenant API key that carries the ``jobs:work``
scope.
"""

from __future__ import annotations

import logging
import signal
import threading
import uuid
from collections.abc import Callable
from typing import Any

import httpx

logger = logging.getLogger("af_stack.worker")

# Handler signature: (payload, ctx) -> optional JSON-serialisable result.
Handler = Callable[[dict[str, Any], "JobContext"], Any]

WORKER_PREFIX = "/api/v1/jobs/worker"


class PermanentError(Exception):
    """Raise from a handler to fail the job WITHOUT a retry (dead-letter)."""


class JobContext:
    """Per-job context handed to a handler.

    ``is_canceled`` reflects the most recent heartbeat: when the runtime
    reports the job was cancelled, a well-behaved handler should stop work
    and return promptly.
    """

    def __init__(
        self,
        *,
        worker: Worker,
        tenant_id: str,
        job_id: str,
        attempt: int,
        deadline: str | None,
    ) -> None:
        self.tenant_id = tenant_id
        self.job_id = job_id
        self.attempt = attempt
        self.deadline = deadline
        self._worker = worker
        self._canceled = threading.Event()

    def is_canceled(self) -> bool:
        """True once the runtime has reported this job cancelled."""
        return self._canceled.is_set()

    def log(self, message: str, *, level: str = "info", **fields: Any) -> None:
        """Attach a structured log line to this job's run.

        Best-effort: a transport failure is logged locally and swallowed so a
        logging hiccup never fails the job.
        """
        self._worker._send_logs(
            self.job_id,
            self.attempt,
            [{"level": level, "message": message, "fields": fields or {}}],
        )


class Worker:
    """A pull-based remote job worker.

    Parameters
    ----------
    base_url:
        Runtime root, e.g. ``http://localhost:8080``.
    api_key:
        A tenant API key with the ``jobs:work`` scope.
    lease_ttl:
        Seconds a lease is held before it must be renewed (default 30).
    heartbeat_interval:
        Seconds between heartbeats while a handler runs (default 10).
    poll_wait:
        Long-poll seconds the runtime holds a lease request open (default 25).
    worker_id:
        Stable id for this worker instance; a random one is generated if
        omitted.
    client:
        Optional pre-built ``httpx.Client`` (mainly for tests).
    """

    def __init__(
        self,
        base_url: str,
        api_key: str,
        *,
        lease_ttl: int = 30,
        heartbeat_interval: int = 10,
        poll_wait: int = 25,
        worker_id: str | None = None,
        client: httpx.Client | None = None,
    ) -> None:
        if not base_url:
            raise ValueError("base_url is required")
        if not api_key:
            raise ValueError("api_key is required")
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.lease_ttl = lease_ttl
        self.heartbeat_interval = heartbeat_interval
        self.poll_wait = poll_wait
        self.worker_id = worker_id or f"py-{uuid.uuid4().hex[:12]}"
        self._handlers: dict[str, Handler] = {}
        self._stop = threading.Event()
        self._owns_client = client is None
        self._client = client or httpx.Client(
            headers={"authorization": f"Bearer {api_key}"},
            timeout=httpx.Timeout(poll_wait + 10.0),
        )

    # ----- registration -------------------------------------------------

    def register(self, kind: str) -> Callable[[Handler], Handler]:
        """Decorator registering ``fn`` as the handler for ``kind``."""

        def deco(fn: Handler) -> Handler:
            if not kind:
                raise ValueError("kind must be a non-empty string")
            self._handlers[kind] = fn
            return fn

        return deco

    def kinds(self) -> list[str]:
        """The kinds this worker will lease."""
        return list(self._handlers.keys())

    # ----- lifecycle ----------------------------------------------------

    def stop(self) -> None:
        """Ask the run loop to drain and exit after the current job."""
        self._stop.set()

    def run(self, *, install_signal_handlers: bool = True) -> None:
        """Block, leasing + executing jobs until :meth:`stop` (or SIGTERM).

        On SIGTERM/SIGINT the worker stops leasing new work and returns once
        the in-flight job (if any) finishes — a graceful drain.
        """
        if not self._handlers:
            raise RuntimeError("no handlers registered; call worker.register(...) first")
        if install_signal_handlers:
            self._install_signal_handlers()
        logger.info("worker started worker_id=%s kinds=%s", self.worker_id, self.kinds())
        try:
            while not self._stop.is_set():
                attempt = self._lease_once()
                if attempt is None:
                    continue
                self._process(attempt)
        finally:
            logger.info("worker draining worker_id=%s", self.worker_id)
            if self._owns_client:
                self._client.close()

    # ----- protocol steps (individually testable) -----------------------

    def _lease_once(self) -> dict[str, Any] | None:
        """Long-poll for one attempt. Returns the attempt dict or None."""
        body = self._post(
            "/lease",
            {
                "kinds": self.kinds(),
                "worker_id": self.worker_id,
                "lease_ttl_seconds": self.lease_ttl,
                "wait_seconds": self.poll_wait,
            },
        )
        if not isinstance(body, dict):
            return None
        return body.get("job")

    def _process(self, attempt: dict[str, Any]) -> None:
        """Run the handler for one leased attempt and report the outcome."""
        kind = str(attempt.get("kind", ""))
        job_id = str(attempt.get("job_id", ""))
        att_num = int(attempt.get("attempt", 1))
        handler = self._handlers.get(kind)
        if handler is None:
            # We leased a kind we can't run (shouldn't happen — we only ask
            # for our kinds). Fail retryably so another worker can pick it up.
            self._fail(job_id, att_num, f"no handler for kind {kind!r}", retryable=True)
            return

        ctx = JobContext(
            worker=self,
            tenant_id=str(attempt.get("tenant_id", "")),
            job_id=job_id,
            attempt=att_num,
            deadline=attempt.get("deadline"),
        )
        heart = _Heartbeat(self, ctx)
        heart.start()
        try:
            result = handler(attempt.get("payload") or {}, ctx)
        except PermanentError as exc:
            heart.stop()
            self._fail(job_id, att_num, str(exc) or "permanent failure", retryable=False)
            return
        except Exception as exc:  # noqa: BLE001 — report any handler error to the runtime
            heart.stop()
            logger.exception("handler error kind=%s job_id=%s", kind, job_id)
            self._fail(job_id, att_num, str(exc) or exc.__class__.__name__, retryable=True)
            return
        finally:
            heart.stop()

        if ctx.is_canceled():
            # The runtime already finalised the job as cancelled; reporting a
            # result would be a no-op 409. Just drop it.
            logger.info("job canceled; skipping report job_id=%s", job_id)
            return
        self._complete(job_id, att_num, result)

    def _complete(self, job_id: str, attempt: int, result: Any) -> None:
        self._post(
            "/complete",
            {
                "job_id": job_id,
                "attempt": attempt,
                "worker_id": self.worker_id,
                "result": result if result is not None else {},
            },
            swallow=True,
        )

    def _fail(self, job_id: str, attempt: int, error: str, *, retryable: bool) -> None:
        self._post(
            "/fail",
            {
                "job_id": job_id,
                "attempt": attempt,
                "worker_id": self.worker_id,
                "error": error,
                "retryable": retryable,
            },
            swallow=True,
        )

    def _send_heartbeat(self, ctx: JobContext) -> bool:
        """Send one heartbeat. Returns True if the job was reported cancelled."""
        body = self._post(
            "/heartbeat",
            {
                "job_id": ctx.job_id,
                "attempt": ctx.attempt,
                "worker_id": self.worker_id,
                "lease_ttl_seconds": self.lease_ttl,
            },
            swallow=True,
        )
        canceled = isinstance(body, dict) and bool(body.get("canceled"))
        if canceled:
            ctx._canceled.set()
        return canceled

    def _send_logs(self, job_id: str, attempt: int, lines: list[dict[str, Any]]) -> None:
        self._post(
            "/logs",
            {"job_id": job_id, "attempt": attempt, "lines": lines},
            swallow=True,
        )

    # ----- transport ----------------------------------------------------

    def _post(self, path: str, body: dict[str, Any], *, swallow: bool = False) -> Any:
        url = f"{self.base_url}{WORKER_PREFIX}{path}"
        try:
            resp = self._client.post(url, json=body)
            resp.raise_for_status()
            if not resp.content:
                return None
            return resp.json()
        except Exception:  # noqa: BLE001
            if swallow:
                logger.warning("worker request failed path=%s", path, exc_info=True)
                return None
            raise

    def _install_signal_handlers(self) -> None:
        def handler(signum: int, _frame: Any) -> None:
            logger.info("received signal %s; draining", signum)
            self.stop()

        for sig in (signal.SIGTERM, signal.SIGINT):
            try:
                signal.signal(sig, handler)
            except (ValueError, OSError):
                # Not on the main thread (e.g. under a test runner) — skip.
                pass


class _Heartbeat:
    """Background heartbeat loop for one in-flight job.

    Runs on its own daemon thread, pinging the runtime every
    ``heartbeat_interval`` seconds so the lease doesn't expire, and flipping
    ``ctx._canceled`` when the runtime reports cancellation.
    """

    def __init__(self, worker: Worker, ctx: JobContext) -> None:
        self._worker = worker
        self._ctx = ctx
        self._stop = threading.Event()
        self._thread: threading.Thread | None = None

    def start(self) -> None:
        self._thread = threading.Thread(target=self._run, daemon=True)
        self._thread.start()

    def stop(self) -> None:
        self._stop.set()
        if self._thread is not None:
            self._thread.join(timeout=2.0)
            self._thread = None

    def _run(self) -> None:
        interval = max(1, self._worker.heartbeat_interval)
        while not self._stop.wait(interval):
            self._worker._send_heartbeat(self._ctx)


__all__ = ["JobContext", "PermanentError", "Worker"]
