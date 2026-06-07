"""todo_completer — find unchecked TODOs in a note and suggest completions.

Smallest possible "agentic UX" reasoner: regex-extracts ``- [ ]`` lines,
asks the model for a 1-line completion per TODO, returns the pairs. The
UI renders these as inline suggestions next to each checkbox.

We do the regex extraction in Python — not via the LLM — because (a) it's
deterministic, (b) it's free, and (c) the LLM is for *intelligence*
(completing tasks), not for *parsing* (which line is a TODO).
"""

from __future__ import annotations

import re
from typing import Any

from pydantic import BaseModel, Field

from main import app

# Matches Markdown unchecked task lines, capturing the task text.
#   "- [ ] write the README"
#   "* [ ]  call mom"
#   "  - [ ] ship it"
_TODO_RE = re.compile(r"^\s*[-*]\s+\[\s\]\s+(.+?)\s*$", re.MULTILINE)

# Max number of TODOs we'll attempt per call. Cheap guard against a
# pathological 500-TODO note tanking latency + cost.
_MAX_TODOS = 20


class Completion(BaseModel):
    """One suggestion per extracted TODO line."""

    line: str = Field(..., description="The original TODO line, verbatim.")
    suggestion: str = Field(
        ...,
        description="A single-line suggestion for completing the task.",
    )


class CompletionList(BaseModel):
    completions: list[Completion] = Field(default_factory=list)


@app.reasoner(tags=["notable", "todos"])
async def todo_completer(payload: dict[str, Any]) -> dict[str, Any]:
    """Find ``- [ ]`` lines in ``body`` and propose completions.

    Input  : ``{body: str}``
    Output : ``{completions: [{line, suggestion}, ...]}``
    """
    body = payload.get("body") or ""
    todos = _TODO_RE.findall(body)[:_MAX_TODOS]
    if not todos:
        return {"completions": []}

    # Build a compact prompt: numbered list of TODOs, one per line. The
    # schema forces the model to keep the order + count consistent, and
    # the runtime will retry on validation failure.
    numbered = "\n".join(f"{i + 1}. {t}" for i, t in enumerate(todos))

    result: CompletionList = await app.ai(
        system=(
            "You are a helpful assistant inside a note-taking app. The "
            "user has listed unfinished tasks. For each numbered task, "
            "produce one short, actionable next step (one sentence, "
            "imperative voice). Do not add tasks. Preserve the input "
            "order and count exactly."
        ),
        user=(
            "Here are the unfinished tasks:\n\n"
            f"{numbered}\n\n"
            "Return one completion per task."
        ),
        schema=CompletionList,
    )

    # Defensive zip: trim model output to the input length so a bad
    # response never produces more rows than the user actually had.
    pairs: list[dict[str, str]] = []
    for original, comp in zip(todos, result.completions):
        pairs.append({"line": original, "suggestion": comp.suggestion})
    return {"completions": pairs}
