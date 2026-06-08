# SPDX-License-Identifier: Apache-2.0

"""``suite.oauth.*`` — OAuth-on-behalf-of-user operations.

Lets backend agents act on behalf of the user in third-party APIs
without re-implementing the OAuth dance per provider. The first shipped
providers are Google and GitHub. The runtime owns the dance + token
vault; this SDK is the client surface.

The four verbs:

============================  ====================================================
SDK call                      Endpoint
============================  ====================================================
:func:`authorize_url`         ``POST /api/v1/oauth/{provider}/authorize``
:func:`connected`             ``GET  /api/v1/oauth/connections``
:func:`token`                 ``POST /api/v1/oauth/token`` (internal-only header)
:func:`disconnect`            ``DELETE /api/v1/oauth/{provider}``
============================  ====================================================

:func:`token` is gated by the ``X-AF-Stack-Internal: 1`` header — browser
callers cannot reach it (the header is not in the CORS allow-list). The
runtime returns a freshly-refreshed access token; this SDK never sees a
refresh token.

The :class:`Connection` model carries metadata only — no token bytes.
The plaintext access token ONLY appears in :func:`token` responses.
"""

from __future__ import annotations

from urllib.parse import quote

from pydantic import BaseModel, ConfigDict, Field

from . import _http


class Connection(BaseModel):
    """One stored OAuth grant for the current user.

    Mirrors the Go ``oauth.Connection`` struct in
    ``services/runtime/internal/oauth/interface.go``.
    """

    model_config = ConfigDict(extra="allow")

    provider: str
    scopes: list[str] = Field(default_factory=list)
    connected_at: str
    expires_at: str | None = None


class ConnectionList(BaseModel):
    """Page of connections for the current user."""

    model_config = ConfigDict(extra="allow")

    connections: list[Connection] = Field(default_factory=list)


class AccessToken(BaseModel):
    """Plaintext access token + scopes.

    Only returned by :func:`token`. Refresh tokens NEVER cross the API
    surface — they live inside the secrets vault.
    """

    model_config = ConfigDict(extra="allow")

    provider: str
    user_id: str
    access_token: str
    scopes: list[str] = Field(default_factory=list)
    expires_at: str | None = None


async def authorize_url(
    provider: str,
    *,
    scopes: list[str] | None = None,
    return_to: str | None = None,
) -> str:
    """Return the provider's consent URL the user should visit.

    The runtime signs an HMAC state nonce that round-trips through the
    consent page; on callback it's verified to defeat CSRF.
    ``return_to`` is validated against an allowed-origin list — pass an
    origin on your customer-app (defaults match localhost dev origins).
    """
    if not provider:
        raise ValueError("provider is required")
    payload: dict[str, object] = {}
    if scopes is not None:
        payload["scopes"] = scopes
    if return_to is not None:
        payload["return_to"] = return_to
    body = await _http.request_json(
        "POST",
        f"/oauth/{quote(provider, safe='')}/authorize",
        json=payload,
    )
    if not isinstance(body, dict) or "authorization_url" not in body:
        raise _http.AFStackError(
            code="BAD_RESPONSE",
            message="authorize endpoint returned no authorization_url",
            status_code=200,
        )
    url = body["authorization_url"]
    if not isinstance(url, str):
        raise _http.AFStackError(
            code="BAD_RESPONSE",
            message="authorization_url is not a string",
            status_code=200,
        )
    return url


async def connected() -> ConnectionList:
    """List active OAuth grants for the current user."""
    body = await _http.request_json("GET", "/oauth/connections")
    return ConnectionList.model_validate(body or {"connections": []})


async def token(provider: str, user_id: str | None = None) -> str:
    """Return a fresh access token for ``provider`` (auto-refreshes).

    Internal-only — the runtime gates this endpoint with the
    ``X-AF-Stack-Internal: 1`` header. Workload modules and agents
    using the AF Stack SDK satisfy that header automatically; browser
    callers cannot.

    ``user_id`` overrides the resolved user. Pass it when calling under
    an API key (no user context) to fetch a token on behalf of a
    specific user the agent is serving.
    """
    if not provider:
        raise ValueError("provider is required")
    payload: dict[str, object] = {"provider": provider}
    if user_id is not None:
        payload["user_id"] = user_id
    body = await _http.request_json(
        "POST",
        "/oauth/token",
        json=payload,
        headers={"X-AF-Stack-Internal": "1"},
    )
    tok = AccessToken.model_validate(body or {})
    if not tok.access_token:
        raise _http.AFStackError(
            code="BAD_RESPONSE",
            message=f"token endpoint returned no access_token for {provider!r}",
            status_code=200,
        )
    return tok.access_token


async def disconnect(provider: str, user_id: str | None = None) -> bool:
    """Disconnect the user's grant for ``provider``.

    Best-effort upstream revoke + soft-delete locally. Returns ``True``
    on success.
    """
    if not provider:
        raise ValueError("provider is required")
    path = f"/oauth/{quote(provider, safe='')}"
    if user_id:
        path += f"?user_id={quote(user_id, safe='')}"
    body = await _http.request_json("DELETE", path)
    if not isinstance(body, dict):
        return True
    return bool(body.get("disconnected", True))


__all__ = [
    "AccessToken",
    "Connection",
    "ConnectionList",
    "authorize_url",
    "connected",
    "disconnect",
    "token",
]
