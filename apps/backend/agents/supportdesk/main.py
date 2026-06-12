"""SupportDesk AgentField agent.

The default BackAI demo uses this agent to make the first support action
visible in AgentField discovery and execution traces. The final customer reply
still goes through BackAI's LLM gateway so tenant cost, API key, and policy
evidence remain visible in the admin dashboard.
"""

from __future__ import annotations

import asyncio
import os
import re
from typing import Any

from agentfield import Agent


NODE_ID = os.getenv("NODE_ID", "supportdesk")

app = Agent(
    node_id=NODE_ID,
    version=os.getenv("AGENT_VERSION", "0.1.0"),
    tags=["support", "first-run", "backai"],
)


def _text(value: Any) -> str:
    if isinstance(value, str):
        return value.strip()
    return ""


@app.reasoner(tags=["entry", "support", "plan"])
async def reply_plan(ticket: str, tenant_id: str | None = None) -> dict[str, Any]:
    """Create a structured support-reply plan from smaller reasoners."""

    issue_task = app.call(f"{NODE_ID}.classify_issue", ticket=ticket)
    facts_task = app.call(f"{NODE_ID}.extract_customer_facts", ticket=ticket)
    issue, facts = await asyncio.gather(issue_task, facts_task)

    category = _text(issue.get("category")) or "general"
    if category == "billing":
        guardrail = await app.call(
            f"{NODE_ID}.refund_guardrail",
            ticket=ticket,
            issue=issue,
            facts=facts,
        )
    else:
        guardrail = await app.call(
            f"{NODE_ID}.resolution_guardrail",
            ticket=ticket,
            issue=issue,
            facts=facts,
        )

    brief = await app.call(
        f"{NODE_ID}.compose_reply_brief",
        ticket=ticket,
        issue=issue,
        facts=facts,
        guardrail=guardrail,
    )

    return {
        "agent": NODE_ID,
        "tenant_id": tenant_id,
        "entry_reasoner": "reply_plan",
        "reasoners": [
            "classify_issue",
            "extract_customer_facts",
            guardrail.get("reasoner", "resolution_guardrail"),
            "compose_reply_brief",
        ],
        "dynamic_branch": category,
        "issue": issue,
        "facts": facts,
        "guardrail": guardrail,
        "brief": brief,
        "confidence": brief.get("confidence", "medium"),
    }


@app.reasoner(tags=["support", "triage"])
async def classify_issue(ticket: str) -> dict[str, Any]:
    """Classify the support issue and pick a handling lane."""

    text = ticket.lower()
    billing_words = ["invoice", "refund", "charge", "billing", "payment", "receipt"]
    access_words = ["login", "password", "access", "account", "sign in", "signin"]
    bug_words = ["bug", "broken", "error", "crash", "failed", "doesn't work"]

    if any(word in text for word in billing_words):
        category = "billing"
        urgency = "medium"
    elif any(word in text for word in access_words):
        category = "access"
        urgency = "high"
    elif any(word in text for word in bug_words):
        category = "technical"
        urgency = "medium"
    else:
        category = "general"
        urgency = "normal"

    return {
        "category": category,
        "urgency": urgency,
        "needs_human_review": category == "billing" and "refund" in text,
        "summary": ticket[:180],
    }


@app.reasoner(tags=["support", "facts"])
async def extract_customer_facts(ticket: str) -> dict[str, Any]:
    """Extract concrete facts the final reply should preserve."""

    emails = re.findall(r"[\w.+-]+@[\w-]+\.[\w.-]+", ticket)
    money = re.findall(r"\$[0-9][0-9,.]*", ticket)
    quoted = re.findall(r'"([^"]+)"', ticket)
    return {
        "emails": emails[:3],
        "amounts": money[:3],
        "quoted_phrases": quoted[:3],
        "mentions_refund": "refund" in ticket.lower(),
        "mentions_invoice": "invoice" in ticket.lower(),
    }


@app.reasoner(tags=["support", "policy"])
async def refund_guardrail(
    ticket: str,
    issue: dict[str, Any],
    facts: dict[str, Any],
) -> dict[str, Any]:
    """Set billing/refund-specific boundaries for the draft."""

    return {
        "reasoner": "refund_guardrail",
        "must_verify": [
            "invoice ID and customer account",
            "refund eligibility and refund window",
            "whether the charge was duplicate, prorated, or tax-related",
        ],
        "do_not_promise": ["refund approval", "account credit", "billing reversal"],
        "allowed_commitment": "acknowledge and route to billing review",
        "handoff": bool(issue.get("needs_human_review") or facts.get("mentions_refund")),
    }


@app.reasoner(tags=["support", "policy"])
async def resolution_guardrail(
    ticket: str,
    issue: dict[str, Any],
    facts: dict[str, Any],
) -> dict[str, Any]:
    """Set non-billing support boundaries for the draft."""

    return {
        "reasoner": "resolution_guardrail",
        "must_verify": ["workspace/account identity", "recent product changes"],
        "do_not_promise": ["root cause before investigation", "SLA outcome"],
        "allowed_commitment": "share next steps and request missing details",
        "handoff": issue.get("urgency") == "high",
    }


@app.reasoner(tags=["support", "brief"])
async def compose_reply_brief(
    ticket: str,
    issue: dict[str, Any],
    facts: dict[str, Any],
    guardrail: dict[str, Any],
) -> dict[str, Any]:
    """Prepare the final LLM prompt brief for BackAI's gateway."""

    next_steps = [
        "thank the customer",
        "summarize the issue in one sentence",
        "state what the team will verify",
    ]
    if guardrail.get("handoff"):
        next_steps.append("make the human-review handoff explicit")
    else:
        next_steps.append("ask for any missing diagnostic details")

    return {
        "tone": "calm, concise, practical",
        "category": issue.get("category", "general"),
        "urgency": issue.get("urgency", "normal"),
        "next_steps": next_steps,
        "constraints": {
            "must_verify": guardrail.get("must_verify", []),
            "do_not_promise": guardrail.get("do_not_promise", []),
            "allowed_commitment": guardrail.get("allowed_commitment", ""),
        },
        "confidence": "high" if len(ticket) > 20 else "low",
    }


if __name__ == "__main__":
    app.run()
