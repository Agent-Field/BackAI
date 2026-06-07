"""AF Stack — Suite SDK for Python.

The suite SDK exposes the operational verbs an app uses daily. Both of
these imports work and are equivalent::

    from af_stack import suite, ctx
    result = await suite.agents.call("notable-ai.summarize", {"text": "..."})

    from af_stack import agents, ctx
    result = await agents.call("notable-ai.summarize", {"text": "..."})

``ctx`` is the request-scoped context (tenant, user, request id) — set by
middleware on every entry point. ``agents`` invokes AgentField via the
suite gateway; all model calls flow through AF (no bypass path).

Inside an AgentField agent process, use the AgentField SDK
(``agentfield.Agent``) to *define* agents. Use this suite SDK to *call*
them and to use suite infrastructure (jobs, secrets, storage,
notifications, billing, sandbox, etc.) — which arrive in later phases.

See https://github.com/Agent-Field/backai for full docs.
"""

from __future__ import annotations

from types import SimpleNamespace

from . import admin, agents, cost, jobs, llm, secrets, storage
from .ctx import RequestContext, bind, ctx, current, reset, scope

__version__ = "0.0.1"

# ``suite`` is a namespace object so users can write the canonical
# ``suite.agents.call(...)`` form. Each module is attached as it lands.
# ``admin`` lives under its own sub-namespace so operational and
# administrative verbs stay visibly separated (see docs/sdk-strategy.md).
suite = SimpleNamespace(
    agents=agents,
    jobs=jobs,
    secrets=secrets,
    storage=storage,
    llm=llm,
    cost=cost,
    admin=SimpleNamespace(
        tenants=admin.tenants,
        users=admin.users,
        memberships=admin.memberships,
        keys=admin.keys,
        audit=admin.audit,
        budgets=admin.budgets,
    ),
)


__all__ = [
    "RequestContext",
    "__version__",
    "admin",
    "agents",
    "bind",
    "cost",
    "ctx",
    "current",
    "jobs",
    "llm",
    "reset",
    "scope",
    "secrets",
    "storage",
    "suite",
]
