# SPDX-License-Identifier: Apache-2.0

"""``suite.secrets.*`` — per-tenant secret operations.

Maps to the REST surface in ``services/runtime/internal/server/secrets.go``:

==================================  ================================================
SDK call                            Endpoint
==================================  ================================================
:func:`get`                         ``POST   /api/v1/secrets/{key}/reveal``
:func:`reveal`                      ``POST   /api/v1/secrets/{key}/reveal``
:func:`list`                        ``GET    /api/v1/secrets``
:func:`put`                         ``PUT    /api/v1/secrets/{key}``
:func:`delete`                      ``DELETE /api/v1/secrets/{key}``
:func:`rotate`                      ``POST   /api/v1/secrets/{key}/rotate``
==================================  ================================================

The ``list`` endpoint returns metadata only — plaintext never leaves the
runtime except through :func:`get` / :func:`reveal`, both of which are
audited server-side. :func:`put` and :func:`rotate` accept the plaintext
in the request body; the runtime encrypts at rest with the configured KEK
before persisting.

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
    body = await _http.request_json("POST", f"/secrets/{quote(key, safe='')}/reveal")
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
    body = await _http.request_json("PUT", f"/secrets/{quote(key, safe='')}", json=payload)
    return SecretMetadata.model_validate(body or {})


async def delete(key: str) -> bool:
    """Delete a secret by key. Returns ``True`` on success."""
    if not key:
        raise ValueError("secret key must be a non-empty string")
    body = await _http.request_json("DELETE", f"/secrets/{quote(key, safe='')}")
    if not isinstance(body, dict):
        return True
    return bool(body.get("deleted", True))


async def list() -> SecretList:  # noqa: A001 — mirrors the JS `secrets.list()` ergonomics
    """List secret metadata visible to the caller. Values are NOT included."""
    body = await _http.request_json("GET", "/secrets")
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
        f"/secrets/{quote(key, safe='')}/rotate",
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
