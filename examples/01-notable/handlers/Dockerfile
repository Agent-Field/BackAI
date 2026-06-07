# Notable API image — FastAPI service that owns the /notable/* routes.
#
# Stays a separate container because Phase 13.4 (workload-module loader)
# isn't landed yet. When it ships, this Dockerfile goes away and
# notes.py moves into workload-modules/notes/.

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

COPY requirements.txt ./
RUN pip install -r requirements.txt

COPY notes.py ./notes.py

ENV NOTABLE_API_PORT=8090

EXPOSE 8090

# uvicorn directly so we get clean reload-less production behaviour in
# dev. Workers=1 keeps memory predictable for the smoke test.
CMD ["uvicorn", "notes:app", "--host", "0.0.0.0", "--port", "8090", "--workers", "1"]
