# SPDX-License-Identifier: Apache-2.0

"""``suite.admin.skills.*`` — install / list / attach AF skillkit bundles.

Maps to the Phase 11.3 skills endpoints in
``apps/dashboard/src/lib/api.ts``:

==================================  =========================================
SDK call                            Endpoint
==================================  =========================================
:func:`list`                        ``GET    /api/v1/skills``
:func:`install`                     ``POST   /api/v1/skills``
:func:`uninstall`                   ``DELETE /api/v1/skills/{id}``
:func:`attach`                      ``POST   /api/v1/skills/attach``
==================================  =========================================

A Skill is an AF skillkit bundle: a manifest declaring prompt overlays,
optional tool bindings, target harnesses, and tags. Install reads the
manifest from its source (registry URL, local path, or
``"embedded:<name>"``) and persists a row in ``suite_skills``; attach
binds the skill onto a specific agent (or harness id).

The endpoints surface 503 with ``code == "SKILLS_NOT_CONFIGURED"`` when
the runtime has no database wired — handle that as the empty-state in
your UI.
"""

from __future__ import annotations

from typing import Any
from urllib.parse import quote

from .. import _http
from ._models import (
    AttachSkillInput,
    InstallSkillInput,
    Skill,
    SkillList,
)


async def list(  # noqa: A001 — mirrors the JS ``skills.list()`` ergonomics
    *,
    tenant: str | None = None,
    harness: str | None = None,
) -> SkillList:
    """List installed skills with optional ``tenant`` and ``harness`` filters.

    ``tenant`` accepts a tenant id, the literal ``"shared"`` (matches
    only the cross-tenant bundles), or ``None`` to defer scoping to the
    server-side RLS policy.

    ``harness`` filters by membership in the skill's harnesses list;
    a value of ``"any"`` short-circuits and returns every skill.
    """
    params: dict[str, Any] = {}
    if tenant is not None:
        params["tenant"] = tenant
    if harness is not None:
        params["harness"] = harness
    body = await _http.request_json("GET", "/skills", params=params or None)
    return SkillList.model_validate(body or {"skills": []})


async def install(
    source: str,
    tenant_id: str | None = None,
) -> Skill:
    """Install a skill from a source string.

    ``source`` accepts three formats:

    * ``af-skill://<vendor>/<name>@<version>`` — registry URL
    * ``/abs/path`` or ``./relative`` — local manifest dir / file
    * ``embedded:<name>`` — runtime-bundled skill (e.g. ``embedded:default``)

    ``tenant_id`` scopes the install to a single tenant; omit to install
    as a shared bundle visible to every tenant.
    """
    if not isinstance(source, str) or not source:
        raise ValueError("`source` must be a non-empty string")
    payload = InstallSkillInput(source=source, tenant_id=tenant_id).model_dump(
        exclude_none=True
    )
    body = await _http.request_json("POST", "/skills", json=payload)
    return Skill.model_validate(body or {})


async def uninstall(skill_id: str) -> None:
    """Uninstall a skill by id. Attachments cascade-delete server-side."""
    if not isinstance(skill_id, str) or not skill_id:
        raise ValueError("`skill_id` must be a non-empty string")
    await _http.request_json(
        "DELETE", f"/skills/{quote(skill_id, safe='')}"
    )


async def attach(skill_id: str, agent: str) -> None:
    """Attach an installed skill onto an agent.

    The runtime persists the (``skill_id``, ``agent``) pair in
    ``suite_skill_attachments``. Re-attaching the same pair is a no-op.
    """
    if not isinstance(skill_id, str) or not skill_id:
        raise ValueError("`skill_id` must be a non-empty string")
    if not isinstance(agent, str) or not agent:
        raise ValueError("`agent` must be a non-empty string")
    payload = AttachSkillInput(skill_id=skill_id, agent=agent).model_dump(
        exclude_none=True
    )
    await _http.request_json("POST", "/skills/attach", json=payload)


__all__ = ["attach", "install", "list", "uninstall"]