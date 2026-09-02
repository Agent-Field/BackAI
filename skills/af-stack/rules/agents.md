# Agents — writing AgentField agents on AF Stack

Where: `apps/backend/agents/<your-agent-name>/`
Language: Python
SDK: `from agentfield import Agent` (the AgentField SDK; not the AF Stack
suite SDK)

## The shape

```
apps/backend/agents/<name>/
├── Dockerfile        # base image + harnesses + MCP runners + your deps
├── main.py           # Agent definition + reasoners
├── pyproject.toml    # or requirements.txt
└── README.md         # optional but recommended
```

The agent registers with the AgentField control plane on startup. The
runtime gateway forwards `POST /api/v1/agents/<node_id>.<reasoner-name>`
to the agent.

Scaffold one with `af-stack agent new <name>`, then check it offline — no
Docker, no runtime, no operator key:

```bash
af-stack agent validate apps/backend/agents/<name>   # add --json
```

It takes a **directory path** (a bare agent id exits 4) and checks the
`main.py` entry point. Exit 0 = valid, 5 = validation failed, 4 = the
directory doesn't exist, 2 = bad args.

## The Agent + reasoner pattern

```python
from agentfield import Agent
from pydantic import BaseModel

app = Agent(node_id="my-agent")  # unique per agent

class Result(BaseModel):
    tldr: str
    key_points: list[str]

@app.reasoner(tags=["text"])
async def summarize(payload: dict) -> dict:
    result = await app.ai(
        system="Summarize in 1 sentence then 3 bullets.",
        user=payload["text"],
        schema=Result,
    )
    return result.model_dump()

if __name__ == "__main__":
    app.run()
```

That's the whole shape. Add reasoners as you need them.

## `.ai()` vs `.harness()` — the decision tree

This is the most important decision in agent design.

```
Does this reasoner need to ...

├─ Read/navigate a document or large input?       → .harness()
├─ Process more than ~3,000 tokens of input?      → .harness()
├─ Make multi-turn decisions (read X, then Y)?    → .harness()
├─ Spawn sub-agents with dynamic prompts?         → .harness()
├─ Produce output > 4 fields or narrative text?   → .harness()
│
└─ ALL of the above are false?
   ├─ Fast classification (< 500 tokens in/out)?  → .ai()
   ├─ Simple routing decision (enum output)?      → .ai()
   └─ Otherwise                                   → .harness()
```

**The rule**: when in doubt, use `.harness()`. Reserve `.ai()` for
gates, classifiers, and routing decisions.

### `.ai()` example — gate / classifier

```python
class IntakeResult(BaseModel):
    contract_type: str
    parties: list[str]
    complexity: str  # "low" | "medium" | "high"
    confident: bool  # ← MUST have a fallback flag

intake = await app.ai(
    prompt="Classify this contract from the first 2-3 pages.",
    input={"text": first_pages},
    schema=IntakeResult,
)

if not intake.confident:
    # Escalate to .harness() — input was too complex for .ai()
    intake = await app.call(
        "my-project.intake_harness",
        input={"document": full_document, "partial_intake": intake.dict()},
    )
```

### `.harness()` example — multi-step work

```python
review = await app.harness(provider="claude-code").run(
    prompt="Review this PR. Focus on security. Generate inline comments.",
    sandbox=dict(
        image="node:20-alpine",
        setup=[
            f"git clone {repo_url} /work",
            f"git checkout {pr_branch}",
        ],
        timeout_s=120,
    ),
    schema=ReviewResult,
)
```

The harness drives a CLI agent (`claude-code` / `codex` / `gemini` /
`opencode`) installed in this agent's Dockerfile. The CLI can navigate
the codebase, run tests, read references — things `.ai()` can't.

## Memory scopes — when to use which

AgentField has 4 scopes. Hierarchical resolution: `app.memory.get(key)`
without an explicit scope tries `workflow → session → actor → global`.

| Scope | Cleared when | Typical key shapes | Use for |
|---|---|---|---|
| `Workflow` | Run completes | `scratch:step_3_result`, `intermediate_findings` | Agent step state, scratch space |
| `Session` | Session ends | `chat:turn_5`, `context_window` | Chat history, conversation state |
| `Actor` | Manually (per-user) | `prefs:tone`, `history:resolved_cases` | Per-user preferences, persistent history |
| `Global` | Manually | `prompt_templates`, `shared_facts` | Shared knowledge across everything |

`scope_id` is required for Actor / Session / Workflow. For Actor, it's
usually a user_id; for Session, a conversation_id; for Workflow, the
run_id.

```python
# Actor-scope: remember user preference
await app.memory.set("tone_preference", "concise", scope="actor", scope_id=user_id)

# Session-scope: chat history (cleared when session ends)
await app.memory.set(f"turn:{n}", message, scope="session", scope_id=conv_id)

# Workflow-scope: scratch space within a single run
await app.memory.set("partial_result", data, scope="workflow", scope_id=run_id)
```

For vector memory: `app.memory.set_vector(...)` + `app.memory.similarity_search(...)`.

## Structured outputs (Pydantic)

Always use Pydantic schemas for `.ai()` and `.harness()` outputs. Why:

- The runtime / SDK returns JSON; Pydantic validates back on the calling
  side. Mismatches caught immediately.
- The LLM is constrained to the schema (via tool calling under the
  hood). Cleaner outputs.
- Documentation: the schema IS the contract.

```python
class Finding(BaseModel):
    severity: Literal["info", "warning", "critical"]
    line: int
    message: str

class ReviewResult(BaseModel):
    findings: list[Finding]
    summary: str
    tests_passed: bool | None
```

## The capabilities reasoner (`__capabilities__`)

Every agent MUST have a `__capabilities__` reasoner. The runtime queries
it at boot to learn what harnesses + MCP runners live in this
container. The Build → Agents dashboard depends on it.

Don't delete or rename this reasoner. The schema must match
`services/runtime/internal/harnesses/interface.go` — see
`apps/backend/agents/sample/main.py` for the canonical implementation
(copy it into your agent and only change `_HARNESS_SPECS` if your
container has a different set of binaries).

## Streaming

For long-running reasoners (harness invocation, multi-step), stream
progress back to the caller:

```python
@app.reasoner(tags=["streaming"])
async def long_task(payload: dict):
    async for event in app.harness("claude-code").stream(...):
        yield {"type": "progress", "data": event}
    yield {"type": "complete", "data": final_result}
```

The runtime's SSE endpoint at `/api/v1/agents/stream/<node>.<reasoner>`
forwards these events to the caller.

## Cancellation

Long-running reasoners should check for cancellation:

```python
@app.reasoner(tags=["cancellable"])
async def long_task(payload: dict):
    for step in steps:
        if app.is_cancelled():
            return {"status": "cancelled", "completed_steps": step}
        ...
```

## Dockerfile patterns

```dockerfile
# AgentField python base ships with the SDK pre-installed.
FROM agentfield/python-base:latest

# Harnesses — install only those you use.
RUN npm install -g @anthropic-ai/claude-code     # ANTHROPIC_API_KEY
# RUN npm install -g @openai/codex                # OPENAI_API_KEY
# RUN npm install -g @google/gemini-cli           # GEMINI_API_KEY
# RUN curl -fsSL https://opencode.ai/install | sh

# MCP runners — install uvx + npx if you use stdio MCP servers.
RUN pip install uv  # uvx ships with uv
# Node + npx is already in agentfield/python-base

# Your Python deps
COPY pyproject.toml /app/
COPY main.py /app/
WORKDIR /app
RUN pip install -e .

CMD ["python", "main.py"]
```

## Anti-patterns

| Anti-pattern | Why it's wrong | Correct pattern |
|---|---|---|
| `import anthropic; client = anthropic.Anthropic(...)` | Bypasses LiteLLM, no cost attribution, no per-tenant budgets | `app.ai(...)` or `suite.llm.chat(...)` |
| Storing chat history in a Postgres table | Duplicates AgentField Session memory | `app.memory.set(scope="session", scope_id=conversation_id, ...)` |
| Writing your own vector store | AgentField has pgvector | `app.memory.set_vector(...)` + `similarity_search(...)` |
| Putting tools in the runtime | Tools live in the agent container or as MCP servers | Declare in `__capabilities__`, call via `app.mcp.call(...)` or harness |
| Hardcoding model names everywhere | Models change | Take from env or config; pass to `.ai(model=...)` |
| `.ai()` with a 50-page document | Won't fit in context | Use `.harness()` — it can navigate |
| `.harness()` for "classify this 3-line input" | Overkill, slow | Use `.ai()` |
| Letting `.ai()` fail without a fallback | Edge cases crash the pipeline | Add `confident: bool` to schema; escalate to `.harness()` |
| Using `Workflow` scope for chat history | Cleared at end of run | Use `Session` scope |
| Forgetting `__capabilities__` | Build → Agents dashboard shows the agent as missing tools | Always include |

## When in doubt

Read the AgentField multi-reasoner guidance in `docs/agentfield-integration.md`
and the [`agentfield-multi-reasoner-builder` skill](https://github.com/Agent-Field/agentfield)
for the canonical multi-reasoner architecture. Or follow the
"map the human process" approach: write down how a human expert would
do this task, then map each step to a reasoner.
