# SPDX-License-Identifier: Apache-2.0

"""Mock harness fallback.

When the configured ``.harness()`` provider (claude-code by default) is
not installed on the host, every sub-investigation routes through here.

This is a degraded path: a single-shot ``.ai()`` call with a structured
Finding schema. The real harness can read URLs, follow citation chains,
and adapt based on intermediate results — none of which we attempt here.

The point of keeping the mock is that the example always runs end-to-end:
operators see the full decompose -> fan-out -> accumulate -> synthesise
data flow even on a fresh box with no harness binaries. When they install
claude-code (or another harness), the runner.py probe flips and the same
sub-investigations get a much deeper treatment.
"""

from __future__ import annotations

from pydantic import BaseModel, Field


class Finding(BaseModel):
    """Mirrors the schema in agents/researcher/main.py so the caller can
    treat the mock's output identically to the real harness's output.

    Kept minimal: a narrative summary plus optional references. Real
    research outputs are richer, but the demo only needs enough shape
    to flow through the synthesis step.
    """

    summary: str = Field(..., description="Concise factual summary.")
    references: list[str] = Field(
        default_factory=list,
        description="Sources/citations the model can attribute (URLs, paper titles).",
    )


async def mock_investigate(app, sub_question: str) -> Finding:
    """Single ``.ai()`` call to stand in for a real harness investigation.

    The system prompt asks the model to be honest about uncertainty
    rather than hallucinate sources — when there's nothing to cite the
    references list comes back empty, which the synthesiser uses to
    lower its overall confidence.
    """
    return await app.ai(
        system=(
            "You are an offline research assistant simulating a deeper "
            "investigation. Given a focused sub-question, return a concise "
            "factual summary (2-4 sentences) and any references you can "
            "name (URLs, paper titles, project names). If you don't have "
            "reliable sources, leave references empty and reflect that "
            "uncertainty in the summary — do not invent citations."
        ),
        user=f"Sub-question: {sub_question}",
        schema=Finding,
    )