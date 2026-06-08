# Template: Dockerfile for a workload module (Python sidecar pattern).
#
# Drop into examples/<your-app>/handlers/Dockerfile.
# Match the requirements.txt to your handler.py imports.

FROM python:3.12-slim

WORKDIR /app

# System deps — minimal: psycopg needs libpq, httpx needs ca-certs.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        libpq5 \
        ca-certificates && \
    rm -rf /var/lib/apt/lists/*

# Python deps
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# App code
COPY handler.py .

# Runs as non-root for safety
RUN useradd -m -u 1000 app
USER app

EXPOSE 8000

CMD ["uvicorn", "handler:app", "--host", "0.0.0.0", "--port", "8000"]
