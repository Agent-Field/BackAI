# SPDX-License-Identifier: Apache-2.0

"""``suite.secrets.*`` — per-tenant secret reads.

The main SDK exposes a single operation: read the plaintext value of a
secret the caller is authorised to see. Reveals route through the runtime
endpoint documented in ``apps/dashboard/src/lib/api.ts``:

==================================  ====================================
SDK call                            Endpoint
==================================  ====================================
:func:`get`                         ``POST /api/v1/secrets/{key}/reveal``
==================================  ====================================

The admin verbs (``list`` / ``set`` / ``delete`` / ``rotate``) live in the
post-v1 admin SDK. They are intentionally absent from this module — keep
the main SDK read-only.
"""

# TODO(admin-sdk): admin operations — list, set, delete, rotate — move to
# ``af_stack.admin.secrets`` once the admin SDK ships. Keep them out of the
# main SDK so accidental writes from app code are impossible.

from __future__ import annotations

from urllib.parse import quote

from . import _http


async def get(key: str) -> str:
    """Return the plaintext value of a secret.

    The runtime enforces tenant scoping and audits every reveal. Raises
    :class:`af_stack._http.AFStackError` if the caller is not authorised
    or the key does not exist.
    """
    if not key:
        raise ValueError("secret key must be a non-empty string")
    body = await _http.request_json(
        "POST", f"/secrets/{quote(key, safe='')}/reveal"
    )
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


__all__ = ["get"]