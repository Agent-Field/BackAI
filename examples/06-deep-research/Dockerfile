# Researcher agent image.
#
# Same shape as apps/backend/agents/sample/Dockerfile so operators reading
# both files see the same skeleton. Python 3.12 slim, no shell deps beyond
# certificates + curl. The mock harness package travels with the agent
# code so the demo runs end-to-end even when no real harness binary is
# installed on the host.
#
# This image deliberately does NOT bundle claude-code / codex / gemini /
# opencode. Those are operator-provided. Set RESEARCHER_HARNESS=<provider>
# and mount the binary (or auth files) when you want the real harness.

FROM python:3.12-slim AS base

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
    && rm -rf /var/lib/apt/lists/*

COPY agents/researcher/requirements.txt ./
RUN pip install -r requirements.txt

# Copy agent code. The mocks package needs to land next to main.py so
# `from mocks.harness import mock_investigate` resolves cleanly.
COPY agents/researcher/main.py ./
COPY agents/researcher/mocks/ ./mocks/

ENV NODE_ID=researcher \
    PORT=8090 \
    AGENT_CALLBACK_URL=http://researcher:8090

EXPOSE 8090

CMD ["python", "main.py"]
