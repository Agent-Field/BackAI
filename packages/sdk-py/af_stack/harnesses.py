# SPDX-License-Identifier: Apache-2.0

"""``suite.harnesses.*`` — Phase 11.4 CLI harness probes.

Maps to the REST surface defined in ``apps/dashboard/src/lib/api.ts``
under the Phase 11.4 Harnesses section:

==================================  ====================================
SDK call                            Endpoint
==================================  ====================================
:func:`list`                        ``GET   /api/v1/harnesses``
:func:`get`                         ``GET   /api/v1/harnesses/{provider}``
:func:`probe`                       ``POST  /api/v1/harnesses/{provider}/probe``
==================================  ====================================

Harnesses are CLI tools agents shell out to (Claude Code, Codex, Gemini,
OpenCode). The runtime probes each one on disk (``exec.LookPath`` +
``--version`` + required env vars) and returns a flat ``Harness`` row
per provider. This SDK module is read-only — the runtime exposes no
"install" verb (installation is left to the operator's OS package
manager).

The :class:`Harness` and :class:`HarnessList` Pydantic models mirror the
zod schemas in ``lib/api.ts`` exactly — keep them in sync when the
contract changes.
"""

from __future__ import annotations

from typing import Literal
from urllib.parse import quote

from pydantic import BaseModel, ConfigDict, Field

from . import _http

HarnessProvider = Literal["claude-code", "codex", "gemini", "opencode"]
HarnessStatus = Literal["ready", "needs_auth", "missing", "errored"]

_ALLOWED_PROVIDERS: tuple[str, ...] = (
    "claude-code",
    "codex",
    "gemini",
    "opencode",
)


class Harness(BaseModel):
    """One probed CLI harness.

    Mirrors ``HarnessSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    provider: HarnessProvider
    is_installed: bool
    binary_path: str | None = None
    version: str | None = None
    status: HarnessStatus
    last_error: str | None = None
    required_env: list[str] = Field(default_factory=list)
    install_doc_url: str | None = None


class HarnessList(BaseModel):
    """List of probed harnesses (always all four providers, in canonical order).

    Mirrors ``HarnessListSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    harnesses: list[Harness] = Field(default_factory=list)


# ─── Operations ───────────────────────────────────────────────────────────


def _check_provider(provider: str) -> None:
    if provider not in _ALLOWED_PROVIDERS:
        allowed = ", ".join(_ALLOWED_PROVIDERS)
        raise ValueError(
            f"`provider` must be one of: {allowed}; got {provider!r}"
        )


async def list() -> HarnessList:  # noqa: A001 — mirrors the JS API
    """List every supported harness with its current probe status.

    The runtime caches probe results in memory; this endpoint returns
    the cached value. Use :func:`probe` to force a fresh check.
    """
    body = await _http.request_json("GET", "/harnesses")
    return HarnessList.model_validate(body or {"harnesses": []})


async def get(provider: HarnessProvider | str) -> Harness:
    """Fetch the cached probe result for a single provider."""
    _check_provider(provider)
    body = await _http.request_json(
        "GET", f"/harnesses/{quote(provider, safe='')}"
    )
    return Harness.model_validate(body or {})


async def probe(provider: HarnessProvider | str) -> Harness:
    """Force a fresh probe of the named harness and return the result."""
    _check_provider(provider)
    body = await _http.request_json(
        "POST", f"/harnesses/{quote(provider, safe='')}/probe"
    )
    return Harness.model_validate(body or {})


__all__ = [
    "Harness",
    "HarnessList",
    "HarnessProvider",
    "HarnessStatus",
    "get",
    "list",
    "probe",
]