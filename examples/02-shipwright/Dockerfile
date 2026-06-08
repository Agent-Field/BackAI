FROM node:20-bookworm-slim AS base

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    VIRTUAL_ENV=/opt/venv \
    PATH="/opt/venv/bin:/usr/local/bin:$PATH"

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
        python3 \
        python3-venv \
    && rm -rf /var/lib/apt/lists/*

ARG CODEX_CLI_VERSION=0.137.0
ARG GEMINI_CLI_VERSION=0.45.2
ARG CLAUDE_CODE_VERSION=2.1.168
RUN npm install -g \
        @openai/codex@${CODEX_CLI_VERSION} \
        @google/gemini-cli@${GEMINI_CLI_VERSION} \
        @anthropic-ai/claude-code@${CLAUDE_CODE_VERSION} \
    && codex --version \
    && gemini --version \
    && claude --version

COPY agents/shipwright/requirements.txt ./
RUN python3 -m venv "$VIRTUAL_ENV"
RUN pip install -r requirements.txt

COPY agents/shipwright/main.py ./main.py

ENV NODE_ID=shipwright \
    PORT=8090 \
    AGENT_CALLBACK_URL=http://shipwright-agent:8090

EXPOSE 8090

CMD ["python", "main.py"]
