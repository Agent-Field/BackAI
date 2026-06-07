"""summarize — turn a note body into a TLDR + key points.

Smallest of the three Notable reasoners. Single ``app.ai()`` call with a
Pydantic schema so the caller gets a strongly-typed JSON object. The
LLM gateway records cost automatically because every ``app.ai()`` call
flows through the configured provider via the gateway shim.
"""

from __future__ import annotations

from typing import Any

from pydantic import BaseModel, Field

from main import app


class Summary(BaseModel):
    """Strict output shape — the runtime will retry / repair if the model
    returns something that doesn't validate."""

    tldr: str = Field(..., description="One- or two-sentence summary.")
    key_points: list[str] = Field(
        default_factory=list,
        description="3-5 bullet points capturing the most important takeaways.",
    )


@app.reasoner(tags=["notable", "text"])
async def summarize(payload: dict[str, Any]) -> dict[str, Any]:
    """Summarize one note. Expects ``{note_id, body}``; returns Summary.

    ``note_id`` is opaque to the reasoner — the caller (the notes
    handler) uses it to write the TLDR back to ``notable_notes.tldr``.
    Keeping the write on the handler side means this reasoner stays
    side-effect-free and replayable.
    """
    body = (payload.get("body") or "").strip()
    if not body:
        # Cheap pre-check so we don't burn a model call on an empty note.
        # Returning a structured error keeps the handler's branching simple.
        return {"error": "empty_body", "tldr": "", "key_points": []}

    result: Summary = await app.ai(
        system=(
            "You write tight summaries. Produce a one- or two-sentence "
            "TLDR followed by 3-5 short key points. Do not invent facts."
        ),
        user=body,
        schema=Summary,
    )
    return result.model_dump()
