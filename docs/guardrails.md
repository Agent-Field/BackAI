# Gateway Guardrails

BackAI applies PII redaction and moderation at the public LLM gateway
boundary. This is intentionally gateway-local policy: AgentField still
owns AI-stateful runs, memory, spans, traces, and tool-call history.

Covered surfaces:

- `POST /api/v1/llm/chat/completions`
- `POST /api/v1/llm/embeddings`
- `POST /api/v1/embeddings`
- `POST /api/v1/llm/images/generations`
- `POST /api/v1/images/generations`
- `POST /api/v1/audio/speech`
- `POST /api/v1/audio/transcriptions`

## Defaults

Guardrails are enabled by default.

The default PII redactor is regex based and replaces common sensitive
values before they leave the gateway and before model text is returned
to the client:

- email addresses
- US SSNs
- phone numbers
- credit card numbers with a Luhn check
- common API key shapes
- AWS access key IDs

Policy failures use the same OpenAI-compatible error envelope as the
LLM gateway:

```json
{
  "error": {
    "message": "content blocked by gateway moderation policy",
    "code": "CONTENT_BLOCKED",
    "type": "invalid_request_error"
  }
}
```

## Environment

```bash
# Disable all guardrails.
AF_STACK_GUARDRAILS_ENABLED=false

# Disable only PII redaction.
AF_STACK_PII_REDACTION_ENABLED=false

# regex (default) or presidio.
AF_STACK_PII_PROVIDER=regex

# Optional Presidio sidecars.
AF_STACK_PRESIDIO_ANALYZER_URL=http://presidio-analyzer:3000
AF_STACK_PRESIDIO_ANONYMIZER_URL=http://presidio-anonymizer:3000

# Moderation is regex-driven and operator-owned.
AF_STACK_MODERATION_ENABLED=true
AF_STACK_MODERATION_BLOCK_PATTERNS='(?i)blocked phrase,(?i)another policy rule'
```

When `AF_STACK_PII_PROVIDER=presidio`, the runtime calls
`/analyze` on the analyzer URL and `/anonymize` on the anonymizer URL.
If the anonymizer URL is omitted, the analyzer URL is reused. Presidio
provider errors fail closed with `HTTP 503 GUARDRAILS_UNAVAILABLE` so
PII is not leaked during sidecar outages.

## Moderation

Moderation block rules are explicit regular expressions supplied by the
operator. BackAI does not ship a broad content taxonomy by default
because self-hosted teams usually need different policy choices by
industry, region, and customer contract.

Rules run on request text before the provider call and on text responses
before the client receives them. A blocked request never reaches LiteLLM.
