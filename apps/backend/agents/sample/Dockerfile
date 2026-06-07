# Sample agent image — Python 3.12 base with AgentField SDK preinstalled.
#
# This image hosts BOTH the agent code AND the CLI tools that agents invoke
# at runtime (harnesses + MCP runners). The runtime container is distroless
# and intentionally cannot run these tools — they live here, where
# `app.harness(provider="claude-code")` and `app.mcp.call(...)` actually
# execute.
#
# Binaries installed:
#   - uv / uvx    — required by stdio MCP servers (e.g. mcp-server-github)
#   - node / npm  — required by CLI harnesses installed via npm
#   - claude      — Claude Code CLI (provider="claude-code")
#   - codex       — OpenAI Codex CLI (provider="codex")
#   - gemini      — Google Gemini CLI (provider="gemini")
#   - opencode    — OpenCode terminal harness (provider="opencode")
#
# The agent declares its actual availability at startup via shutil.which
# + env-key checks in main.py — only binaries that are present + their
# required env keys set are reported "ready" to the runtime.

FROM python:3.12-slim AS base

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

WORKDIR /app

# System deps: ca-certs (HTTPS), curl (uv installer + healthchecks),
# nodejs+npm (for the CLI harnesses installed via npm), git (some MCP
# servers and CLI tools want it on PATH).
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
        nodejs \
        npm \
    && rm -rf /var/lib/apt/lists/*

# uv + uvx — needed for stdio MCP servers spawned via `uvx <server>`.
# The installer drops both binaries under /root/.local/bin; we move them
# to /usr/local/bin so they're on PATH for the unprivileged agent
# process at runtime.
RUN curl -LsSf https://astral.sh/uv/install.sh | sh \
    && mv /root/.local/bin/uv /usr/local/bin/uv \
    && mv /root/.local/bin/uvx /usr/local/bin/uvx \
    && uv --version \
    && uvx --version

# CLI harnesses. Each install is independent so failures surface
# individually (a 404 on one package doesn't take out the rest). The
# corresponding *_API_KEY env var must also be present at runtime for
# the harness to be reported "ready" by the agent (see main.py).
#
# Use --omit=optional to skip any optional native-binary deps that
# don't apply to a generic linux base image.
RUN npm install -g --omit=optional @anthropic-ai/claude-code || true
RUN npm install -g --omit=optional @openai/codex || true
RUN npm install -g --omit=optional @google/gemini-cli || true
# OpenCode publishes its own binary — fetch it directly. Failing
# softly here keeps the image build green even when the registry blips.
RUN curl -fsSL https://opencode.ai/install | bash 2>/dev/null || true

COPY requirements.txt ./
RUN pip install -r requirements.txt

COPY main.py ./

ENV NODE_ID=sample \
    PORT=8090 \
    AGENT_CALLBACK_URL=http://sample-agent:8090

EXPOSE 8090

CMD ["python", "main.py"]
