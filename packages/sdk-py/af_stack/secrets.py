# SPDX-License-Identifier: Apache-2.0

"""``suite.secrets.*`` — per-tenant secret operations.

Maps to the tenant-principal REST surface in
``services/runtime/internal/server/tenant_secrets.go``. These routes ride
the tenant resolver, so the caller's API key (or session) scopes every
operation to its own tenant — a plain tenant bearer key is all that is
required:

==================================  ================================================
SDK call                            Endpoint
==================================  ================================================
:func:`get`                         ``POST   /api/v1/vault/secrets/{key}/reveal``
:func:`reveal`                      ``POST   /api/v1/vault/secrets/{key}/reveal``
:func:`list`                        ``GET    /api/v1/vault/secrets``
:func:`put`                         ``PUT    /api/v1/vault/secrets/{key}``
:func:`delete`                      ``DELETE /api/v1/vault/secrets/{key}``
:func:`rotate`                      ``POST   /api/v1/vault/secrets/{key}/rotate``
==================================  ================================================

(The operator dashboard drives a separate operator-gated surface at
``/api/v1/secrets``; the SDK never touches it.)

The ``list`` and ``get``-metadata endpoints return metadata only — each row
also carries a ``reference`` of the form ``secret:<key>`` that app config
can use instead of the value. Plaintext never leaves the runtime except
through :func:`reveal`, which is audited server-side. :func:`put` and
:func:`rotate` accept the plaintext in the request body; the runtime
encrypts at rest with the configured KEK before persisting.

The :class:`SecretMetadata` and :class:`SecretList` Pydantic models mirror
the ``SecretMetadataSchema`` / ``SecretListSchema`` zod schemas in
``apps/dashboard/src/lib/api.ts``.
"""

from __future__ import annotations

from urllib.parse import quote

from pydantic import BaseModel, ConfigDict, Field

from . import _http


class SecretMetadata(BaseModel):
    """Non-secret view of a stored entry (no plaintext).

    Mirrors ``SecretMetadataSchema`` in ``apps/dashboard/src/lib/api.ts``.
    Plaintext is intentionally absent: list/get responses MUST NOT carry
    the value field. Use :func:`reveal` (audited) to read the plaintext.
    """

    model_config = ConfigDict(extra="allow")

    key: str
    tenant_id: str | None = None
    description: str | None = None
    rotate_after: str | None = None
    created_at: str
    updated_at: str
    # ``secret:<key>`` reference the runtime resolves elsewhere (e.g. an MCP
    # server env value). Present on tenant-surface reads; absent on older
    # runtimes, hence optional.
    reference: str | None = None


class SecretList(BaseModel):
    """Page of secret metadata rows.

    Mirrors ``SecretListSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    secrets: list[SecretMetadata] = Field(default_factory=list)


async def get(key: str) -> str:
    """Return the plaintext value of a secret.

    Equivalent to :func:`reveal`. Kept as the primary read verb because
    most callers want the value, not the metadata. The runtime enforces
    tenant scoping and audits every reveal. Raises
    :class:`af_stack._http.AFStackError` if the caller is not authorised
    or the key does not exist.
    """
    return await reveal(key)


async def reveal(key: str) -> str:
    """Reveal the plaintext value of a secret. Every call is audited."""
    if not key:
        raise ValueError("secret key must be a non-empty string")
    body = await _http.request_json("POST", f"/vault/secrets/{quote(key, safe='')}/reveal")
    if not isinstance(body, dict) or "value" not in body:
        raise _http.AFStackError(
            code="BAD_RESPONSE",
            message=f"reveal endpoint returned no value for key {key!r}",
            status_code=200,
        )
    value = body["value"]
    if not isinstance(value, str):
        raise _http.AFStackError(
            code="BAD_RESPONSE",
            message=f"reveal endpoint returned non-string value for key {key!r}",
            status_code=200,
        )
    return value


async def put(
    key: str,
    value: str,
    *,
    description: str | None = None,
    rotate_after: str | None = None,
) -> SecretMetadata:
    """Create or replace a secret. Returns the persisted metadata.

    ``value`` is the plaintext to encrypt and store. ``description`` is a
    human-readable note shown in the dashboard. ``rotate_after`` is an
    RFC3339 timestamp the dashboard uses to surface rotation reminders —
    it does not trigger anything automatic on the runtime.
    """
    if not key:
        raise ValueError("secret key must be a non-empty string")
    if not isinstance(value, str) or value == "":
        raise ValueError("secret value must be a non-empty string")
    payload: dict[str, object] = {"value": value}
    if description is not None:
        payload["description"] = description
    if rotate_after is not None:
        payload["rotate_after"] = rotate_after
    body = await _http.request_json("PUT", f"/vault/secrets/{quote(key, safe='')}", json=payload)
    return SecretMetadata.model_validate(body or {})


async def delete(key: str) -> bool:
    """Delete a secret by key. Returns ``True`` on success."""
    if not key:
        raise ValueError("secret key must be a non-empty string")
    body = await _http.request_json("DELETE", f"/vault/secrets/{quote(key, safe='')}")
    if not isinstance(body, dict):
        return True
    return bool(body.get("deleted", True))


async def list() -> SecretList:  # noqa: A001 — mirrors the JS `secrets.list()` ergonomics
    """List secret metadata visible to the caller. Values are NOT included."""
    body = await _http.request_json("GET", "/vault/secrets")
    return SecretList.model_validate(body or {"secrets": []})


async def rotate(key: str, value: str) -> SecretMetadata:
    """Rotate a secret to a new value. Returns the updated metadata.

    Functionally equivalent to :func:`put` for an existing key but the
    runtime emits a different audit event (``secret.rotate`` vs
    ``secret.put``) which dashboards and security tooling filter on.
    """
    if not key:
        raise ValueError("secret key must be a non-empty string")
    if not isinstance(value, str) or value == "":
        raise ValueError("secret value must be a non-empty string")
    body = await _http.request_json(
        "POST",
        f"/vault/secrets/{quote(key, safe='')}/rotate",
        json={"value": value},
    )
    return SecretMetadata.model_validate(body or {})


__all__ = [
    "SecretList",
    "SecretMetadata",
    "delete",
    "get",
    "list",
    "put",
    "reveal",
    "rotate",
]
