"""``suite.admin.*`` — multi-tenancy admin operations.

The admin sub-package mirrors the ``/api/v1/admin/*`` REST surface defined
in ``apps/dashboard/src/lib/api.ts``. It is intentionally separate from
the main SDK so accidental writes from app code are impossible — admin
verbs require admin credentials and are explicitly opt-in::

    from af_stack import suite

    tenants = await suite.admin.tenants.list()
    issued = await suite.admin.keys.issue({"tenant_id": "...", "scopes": []})
    # `issued.value` is shown ONCE — store it immediately.

Endpoints are gated by the runtime's ``multi-tenancy`` module flag. When
disabled, every call here surfaces an :class:`AFStackError` whose ``code``
is ``"MT_DISABLED"`` — handle that as the empty-state in your UI.

See :mod:`af_stack.admin.tenants`, :mod:`.users`, :mod:`.memberships`,
:mod:`.keys`, :mod:`.audit` for the per-namespace contracts.
"""

from __future__ import annotations

from . import audit, keys, memberships, tenants, users

__all__ = [
    "audit",
    "keys",
    "memberships",
    "tenants",
    "users",
]
