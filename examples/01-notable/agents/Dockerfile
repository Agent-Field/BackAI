# Notable agents image — three reasoners in one Python process.
#
# Why one process: each reasoner is small (a single .ai() call + a tiny
# bit of glue). Running three OS processes would triple the AF
# registration churn and the docker-compose footprint for zero benefit
# in dev. In production you'd split them per-service if you wanted
# separate autoscaling, but a single uvicorn-style worker is the right
# default for "showing what a real SaaS looks like."

FROM python:3.12-slim AS base

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

WORKDIR /app

# ca-certificates: HTTPS to OpenRouter / Anthropic / OpenAI.
# curl: lets docker-compose hit /healthz without bundling wget.
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt ./
RUN pip install -r requirements.txt

# Each reasoner lives in its own subdir for legibility. The entrypoint
# imports all three so a single `python main.py` registers all of them.
COPY summarize/main.py ./summarize/main.py
COPY suggest_tags/main.py ./suggest_tags/main.py
COPY todo_completer/main.py ./todo_completer/main.py
COPY main.py ./main.py

ENV NODE_ID=notable-ai \
    AGENT_CALLBACK_URL=http://notable-agents:8090 \
    PORT=8090

EXPOSE 8090

CMD ["python", "main.py"]
