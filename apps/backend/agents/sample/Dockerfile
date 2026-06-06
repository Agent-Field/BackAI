# Sample agent image — Python 3.12 base with AgentField SDK preinstalled.
#
# Multi-stage to keep the final image small. OpenCode CLI is bundled so the
# 60-second quickstart works without a separate install step.

FROM python:3.12-slim AS base

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

WORKDIR /app

# System deps: ca-certs (for HTTPS to AF + LLM providers), curl for healthchecks
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt ./
RUN pip install -r requirements.txt

COPY main.py ./

ENV NODE_ID=sample \
    PORT=8090 \
    AGENT_CALLBACK_URL=http://sample-agent:8090

EXPOSE 8090

CMD ["python", "main.py"]
