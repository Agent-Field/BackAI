# SPDX-License-Identifier: Apache-2.0

"""``suite.auth.*`` — identity introspection for the current credential.

Auth lifecycle (sign-up / sign-in / reset) is owned by the app layer
(better-auth in the customer-app). The SDK exposes the read-only "who am I":
which tenant / user / key the current credential resolves to.

    GET /api/v1/auth/whoami
"""

from __future__ import annotations

from pydantic import BaseModel, ConfigDict

from . import _http


class WhoAmI(BaseModel):
    """Identity the current credential resolves to.

    Mirrors the ``whoamiResponse`` shape in
    ``services/runtime/internal/server/auth.go``. ``authenticated`` is
    ``False`` (and the ids empty) for an unauthenticated call.
    """

    model_config = ConfigDict(extra="allow")

    authenticated: bool = False
    tenant_id: str = ""
    user_id: str = ""
    api_key_id: str = ""


async def whoami() -> WhoAmI:
    """Resolve the tenant / user / key the current credential maps to."""
    body = await _http.request_json("GET", "/auth/whoami")
    return WhoAmI.model_validate(body or {})


__all__ = ["WhoAmI", "whoami"]
