# Embeddings API

AF Stack exposes OpenAI-compatible embeddings at:

```text
POST /api/v1/embeddings
```

The endpoint is routed through the same LLM gateway path as chat:
AgentField policy and hooks first, then LiteLLM provider routing, then
cost and audit metadata. It is for product embeddings, search indexing,
recommendations, clustering, and other app-data use cases.

This does not create a second memory or vector-store primitive. Agent
memory, runs, spans, traces, and workflow state stay in AgentField.

## TypeScript

```ts
import { suite } from "@af-stack/sdk"

const response = await suite.llm.embed({
  model: "text-embedding-3-small",
  input: "customer asked about SOC2 exports",
})

const body = await response.json()
const vector = body.data[0].embedding
```

Batch input uses the standard OpenAI shape:

```ts
await suite.llm.embed({
  model: "text-embedding-3-small",
  input: ["first document", "second document"],
  dimensions: 512,
})
```

## REST

```bash
curl -X POST "$AF_STACK_URL/api/v1/embeddings" \
  -H "Authorization: Bearer $AF_STACK_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "text-embedding-3-small",
    "input": "customer asked about SOC2 exports"
  }'
```

The response is OpenAI-shaped:

```json
{
  "object": "list",
  "model": "text-embedding-3-small",
  "data": [
    {
      "object": "embedding",
      "index": 0,
      "embedding": [0.0123, -0.0456]
    }
  ],
  "usage": {
    "prompt_tokens": 8,
    "total_tokens": 8
  }
}
```

## Compatibility path

`POST /api/v1/llm/embeddings` remains available for OpenAI-compatible
clients configured with `baseURL="$AF_STACK_URL/api/v1/llm"`. New suite
SDK code should prefer `/api/v1/embeddings`.
