# SPDX-License-Identifier: Apache-2.0

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

from . import (
    admin,
    agents,
    approvals,
    audio,
    auth,
    billing,
    cost,
    crons,
    harnesses,
    images,
    jobs,
    llm,
    memory,
    notifications,
    oauth,
    realtime,
    runs,
    sandbox,
    search,
    secrets,
    shipwright,
    storage,
    tools,
    webhooks,
)
from ._http import (
    SUPPORTED_RUNTIME_RANGE as SUPPORTED_RUNTIME,
)
from ._http import (
    AFStackError,
    Transport,
    check_runtime_compat,
)
from .client import BackAI
from .ctx import RequestContext, bind, ctx, current, reset, scope
from .pagination import AsyncPaginator, paginate
from .tools import Tools  # noqa: F401 — backward-compat re-export

__version__ = "0.0.1"

# ``suite`` is a namespace object so users can write the canonical
# ``suite.agents.call(...)`` form. Each module is attached as it lands.
# ``admin`` lives under its own sub-namespace so operational and
# administrative verbs stay visibly separated (see docs/sdk-strategy.md).
suite = SimpleNamespace(
    agents=agents,
    approvals=approvals,
    auth=auth,
    shipwright=shipwright,
    jobs=jobs,
    secrets=secrets,
    storage=storage,
    llm=llm,
    audio=audio,
    images=images,
    cost=cost,
    memory=memory,
    notifications=notifications,
    sandbox=sandbox,
    billing=billing,
    webhooks=webhooks,
    harnesses=harnesses,
    tools=tools,
    search=search,
    crons=crons,
    oauth=oauth,
    realtime=realtime,
    runs=runs,
    admin=SimpleNamespace(
        tenants=admin.tenants,
        users=admin.users,
        memberships=admin.memberships,
        keys=admin.keys,
        audit=admin.audit,
        budgets=admin.budgets,
        skills=admin.skills,
    ),
)


__all__ = [
    "SUPPORTED_RUNTIME",
    "AFStackError",
    "AsyncPaginator",
    "BackAI",
    "RequestContext",
    "Tools",
    "Transport",
    "__version__",
    "admin",
    "agents",
    "approvals",
    "audio",
    "auth",
    "billing",
    "bind",
    "check_runtime_compat",
    "cost",
    "crons",
    "ctx",
    "current",
    "harnesses",
    "images",
    "jobs",
    "llm",
    "memory",
    "notifications",
    "oauth",
    "paginate",
    "realtime",
    "reset",
    "runs",
    "sandbox",
    "scope",
    "search",
    "secrets",
    "shipwright",
    "storage",
    "suite",
    "tools",
    "webhooks",
]
