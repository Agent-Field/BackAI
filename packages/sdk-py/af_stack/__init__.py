"""AF Stack — Suite SDK for Python.

The suite SDK exposes the operational verbs an app uses daily:

    from af_stack import suite, ctx

    # Call an agent
    result = await suite.agents.call("notable-ai.summarize", {"text": "..."})

    # Read tenant context
    tenant_id = ctx.tenant_id

Inside an AgentField agent process, use the AgentField SDK (``agentfield.Agent``)
to *define* agents. Use this suite SDK to *call* them and to use suite
infrastructure (jobs, secrets, storage, notifications, billing, sandbox, etc.).

See https://github.com/Agent-Field/backai for full docs.
"""

__version__ = "0.0.1"
__all__ = ["__version__"]
