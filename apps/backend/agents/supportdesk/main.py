"""SupportDesk AgentField agent.

The default BackAI demo uses this agent to make the first support action
visible in AgentField discovery and execution traces. The final customer reply
still goes through BackAI's LLM gateway so tenant cost, API key, and policy
evidence remain visible in the admin dashboard.
"""

from __future__ import annotations

import asyncio
import logging
import os
import re
from typing import Any

from agentfield import Agent, AgentRouter


def _init_sentry() -> None:
    dsn = os.getenv("SENTRY_DSN", "").strip()
    if not dsn:
        return
    import sentry_sdk
    from sentry_sdk.integrations.logging import LoggingIntegration

    sentry_sdk.init(
        dsn=dsn,
        environment=os.getenv("SENTRY_ENVIRONMENT") or os.getenv("AF_STACK_ENV") or "development",
        release=os.getenv("SENTRY_RELEASE") or os.getenv("AGENT_VERSION") or "dev",
        traces_sample_rate=0.0,
        integrations=[
            LoggingIntegration(level=logging.INFO, event_level=logging.ERROR),
        ],
    )


_init_sentry()


NODE_ID = os.getenv("NODE_ID", "supportdesk")

app = Agent(
    node_id=NODE_ID,
    version=os.getenv("AGENT_VERSION", "0.1.0"),
    tags=["support", "first-run", "backai"],
)

router = AgentRouter(tags=["support"])


def _text(value: Any) -> str:
    if isinstance(value, str):
        return value.strip()
    return ""


def _agent_reasoner(name: str) -> str:
    return f"{app.node_id}.{name}"


@router.reasoner(tags=["entry", "plan"])
async def reply_plan(
    ticket: str,
    tenant_id: str | None = None,
    model: str | None = None,
) -> dict[str, Any]:
    """Create a structured support-reply plan from smaller reasoners."""

    issue_task = app.call(_agent_reasoner("classify_issue"), ticket=ticket, model=model)
    facts_task = app.call(
        _agent_reasoner("extract_customer_facts"), ticket=ticket, model=model
    )
    issue, facts = await asyncio.gather(issue_task, facts_task)

    category = _text(issue.get("category")) or "general"
    if category == "billing":
        policy_review = await app.call(
            _agent_reasoner("billing_policy_review"),
            ticket=ticket,
            issue=issue,
            facts=facts,
            model=model,
        )
    else:
        policy_review = await app.call(
            _agent_reasoner("support_policy_review"),
            ticket=ticket,
            issue=issue,
            facts=facts,
            model=model,
        )

    brief = await app.call(
        _agent_reasoner("compose_reply_brief"),
        ticket=ticket,
        issue=issue,
        facts=facts,
        policy_review=policy_review,
        model=model,
    )
    reasoner_path = [
        "classify_issue",
        "extract_customer_facts",
        policy_review.get("reasoner", "support_policy_review"),
        *policy_review.get("children", []),
        "compose_reply_brief",
    ]

    return {
        "agent": app.node_id,
        "tenant_id": tenant_id,
        "entry_reasoner": "reply_plan",
        "reasoners": reasoner_path,
        "graph_depth": 3,
        "dynamic_branch": category,
        "issue": issue,
        "facts": facts,
        "policy_review": policy_review,
        "guardrail": policy_review.get("guardrail", policy_review),
        "brief": brief,
        "confidence": brief.get("confidence", "medium"),
    }


@router.reasoner(tags=["triage"])
async def classify_issue(ticket: str, model: str | None = None) -> dict[str, Any]:
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


@router.reasoner(tags=["facts"])
async def extract_customer_facts(
    ticket: str, model: str | None = None
) -> dict[str, Any]:
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


@router.reasoner(tags=["policy", "billing"])
async def billing_policy_review(
    ticket: str,
    issue: dict[str, Any],
    facts: dict[str, Any],
    model: str | None = None,
) -> dict[str, Any]:
    """Review a billing ticket through nested policy and evidence reasoners."""

    guardrail_task = app.call(
        _agent_reasoner("refund_guardrail"),
        ticket=ticket,
        issue=issue,
        facts=facts,
        model=model,
    )
    evidence_task = app.call(
        _agent_reasoner("billing_evidence_check"),
        ticket=ticket,
        issue=issue,
        facts=facts,
        model=model,
    )
    guardrail, evidence = await asyncio.gather(guardrail_task, evidence_task)
    return {
        "reasoner": "billing_policy_review",
        "children": ["refund_guardrail", "billing_evidence_check"],
        "guardrail": guardrail,
        "evidence": evidence,
        "handoff": bool(guardrail.get("handoff") or evidence.get("missing_verification")),
    }


@router.reasoner(tags=["policy", "support"])
async def support_policy_review(
    ticket: str,
    issue: dict[str, Any],
    facts: dict[str, Any],
    model: str | None = None,
) -> dict[str, Any]:
    """Review a non-billing ticket through nested support policy checks."""

    guardrail_task = app.call(
        _agent_reasoner("resolution_guardrail"),
        ticket=ticket,
        issue=issue,
        facts=facts,
        model=model,
    )
    risk_task = app.call(
        _agent_reasoner("response_risk_check"),
        ticket=ticket,
        issue=issue,
        facts=facts,
        model=model,
    )
    guardrail, risk = await asyncio.gather(guardrail_task, risk_task)
    return {
        "reasoner": "support_policy_review",
        "children": ["resolution_guardrail", "response_risk_check"],
        "guardrail": guardrail,
        "risk": risk,
        "handoff": bool(guardrail.get("handoff") or risk.get("needs_human_review")),
    }


@router.reasoner(tags=["policy", "billing"])
async def refund_guardrail(
    ticket: str,
    issue: dict[str, Any],
    facts: dict[str, Any],
    model: str | None = None,
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


@router.reasoner(tags=["facts", "billing"])
async def billing_evidence_check(
    ticket: str,
    issue: dict[str, Any],
    facts: dict[str, Any],
    model: str | None = None,
) -> dict[str, Any]:
    """Identify billing proof points that the final draft must not invent."""

    return {
        "reasoner": "billing_evidence_check",
        "has_amount": bool(facts.get("amounts")),
        "has_invoice": bool(facts.get("mentions_invoice")),
        "missing_verification": not (
            bool(facts.get("amounts")) and bool(facts.get("mentions_invoice"))
        ),
        "evidence_summary": "amount and invoice mentioned"
        if facts.get("amounts") and facts.get("mentions_invoice")
        else "billing details need verification",
    }


@router.reasoner(tags=["policy", "support"])
async def resolution_guardrail(
    ticket: str,
    issue: dict[str, Any],
    facts: dict[str, Any],
    model: str | None = None,
) -> dict[str, Any]:
    """Set non-billing support boundaries for the draft."""

    return {
        "reasoner": "resolution_guardrail",
        "must_verify": ["workspace/account identity", "recent product changes"],
        "do_not_promise": ["root cause before investigation", "SLA outcome"],
        "allowed_commitment": "share next steps and request missing details",
        "handoff": issue.get("urgency") == "high",
    }


@router.reasoner(tags=["risk", "support"])
async def response_risk_check(
    ticket: str,
    issue: dict[str, Any],
    facts: dict[str, Any],
    model: str | None = None,
) -> dict[str, Any]:
    """Check whether the response should avoid decisive product claims."""

    text = ticket.lower()
    sensitive_terms = ["legal", "security", "breach", "refund", "cancel"]
    matched_terms = [term for term in sensitive_terms if term in text]
    return {
        "reasoner": "response_risk_check",
        "matched_terms": matched_terms,
        "needs_human_review": bool(matched_terms) or issue.get("urgency") == "high",
    }


@router.reasoner(tags=["brief"])
async def compose_reply_brief(
    ticket: str,
    issue: dict[str, Any],
    facts: dict[str, Any],
    policy_review: dict[str, Any],
    model: str | None = None,
) -> dict[str, Any]:
    """Prepare the final LLM prompt brief for BackAI's gateway."""

    guardrail = policy_review.get("guardrail", policy_review)
    next_steps = [
        "thank the customer",
        "summarize the issue in one sentence",
        "state what the team will verify",
    ]
    if policy_review.get("handoff") or guardrail.get("handoff"):
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


app.include_router(router)


if __name__ == "__main__":
    app.run()
