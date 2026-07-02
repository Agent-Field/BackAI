# SPDX-License-Identifier: Apache-2.0

"""``suite.admin.keys.*`` — per-tenant API key management.

Maps to:

==================================  =========================================
SDK call                            Endpoint
==================================  =========================================
:func:`list`                        ``GET    /api/v1/admin/keys``
:func:`issue`                       ``POST   /api/v1/admin/keys``
:func:`revoke`                      ``DELETE /api/v1/admin/keys/{id}``
==================================  =========================================

.. warning::
    :func:`issue` is the **only** code path that returns the plaintext
    secret. The runtime stores a bcrypt hash; subsequent ``list`` /
    ``get`` calls expose the prefix only. Persist
    :attr:`IssuedAPIKey.value` immediately — there is no recovery.
"""

from __future__ import annotations

from typing import Any
from urllib.parse import quote

from .. import _http
from ._models import APIKeyList, IssueAPIKeyInput, IssuedAPIKey


async def list(  # noqa: A001 — mirrors the JS ``keys.list()`` ergonomics
    *,
    tenant: str | None = None,
) -> APIKeyList:
    """List API keys, optionally scoped to a tenant id."""
    params: dict[str, Any] = {}
    if tenant:
        params["tenant"] = tenant
    body = await _http.request_json(
        "GET",
        "/admin/keys",
        params=params if params else None,
    )
    return APIKeyList.model_validate(body or {"keys": []})


async def issue(input: IssueAPIKeyInput | dict[str, Any]) -> IssuedAPIKey:  # noqa: A002
    """Issue a new API key and return the one-time-reveal record.

    The returned :class:`IssuedAPIKey` carries the plaintext ``value``
    in addition to the metadata. The runtime stores the bcrypt hash —
    if ``value`` is lost, the key must be revoked and re-issued.
    """
    payload = _as_dict(input)
    if not payload.get("tenant_id"):
        raise ValueError("input.tenant_id must be a non-empty string")
    payload.setdefault("scopes", [])
    body = await _http.request_json("POST", "/admin/keys", json=payload)
    return IssuedAPIKey.model_validate(body or {})


async def revoke(id: str) -> bool:  # noqa: A002
    """Revoke an API key. Returns ``True`` on success.

    Revocation is irreversible. Re-issuance produces a new prefix +
    plaintext; old tokens stop authenticating immediately.
    """
    if not id:
        raise ValueError("api key id must be a non-empty string")
    body = await _http.request_json("DELETE", f"/admin/keys/{quote(id, safe='')}")
    if not isinstance(body, dict):
        return True
    return bool(body.get("revoked", True))


def _as_dict(value: Any) -> dict[str, Any]:
    """Coerce an :class:`IssueAPIKeyInput` or a dict into a wire dict.

    ``None`` fields are dropped so we don't accidentally send
    ``"expires_at": null`` (the server treats that differently from
    "omitted" in a partial update).
    """
    if isinstance(value, IssueAPIKeyInput):
        return value.model_dump(exclude_none=True)
    if isinstance(value, dict):
        return {k: v for k, v in value.items() if v is not None}
    raise TypeError(f"input must be IssueAPIKeyInput or dict, got {type(value).__name__}")


__all__ = ["issue", "list", "revoke"]
