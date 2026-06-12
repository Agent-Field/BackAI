# SupportDesk agent image.
#
# This is intentionally lighter than the optional harness/coding-agent sample:
# the public first run should register a domain agent quickly, then let users
# inspect its reasoners in the admin console.

FROM python:3.12-slim

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PIP_NO_CACHE_DIR=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt ./
RUN pip install -r requirements.txt

COPY main.py ./

ENV NODE_ID=supportdesk \
    PORT=8090 \
    AGENT_CALLBACK_URL=http://supportdesk-agent:8090

EXPOSE 8090

CMD ["python", "main.py"]
