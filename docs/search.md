# Search API

BackAI ships a tenant-scoped app-data search index for product records
and workload-module data.

This is separate from AgentField state. Do not use this table for agent
memory, sessions, runs, spans, or traces; those stay in AgentField. Use
Search for records your application owns: documents, tickets, notes,
catalog items, help-center pages, audit-friendly knowledge records, and
module-specific rows.

## Stack

- Storage: Postgres table `suite_search_documents`
- Full-text: Postgres generated `tsvector` + GIN index
- Vector: pgvector `vector(1536)` + HNSW index
- Hybrid: runtime merges FTS and vector candidates with reciprocal-rank
  scoring
- Isolation: `tenant_id` plus Postgres RLS policy

FTS works with only Postgres. `mode: "vector"` and vector-enhanced
`mode: "hybrid"` require the runtime LLM gateway embedder to be
configured. If no embedder is available, hybrid falls back to FTS so
search remains useful in local and low-cost deployments.

## TypeScript

```ts
import { suite } from "@af-stack/sdk"

await suite.searchIndex.upsert("doc_123", {
  namespace: "docs",
  title: "SOC 2 data retention",
  body: "Customer exports are retained for 30 days...",
  metadata: { source: "handbook", plan: "enterprise" },
  embed: true,
})

const result = await suite.search("retention exports", {
  mode: "hybrid",
  namespace: "docs",
  metadataFilter: { plan: "enterprise" },
  limit: 5,
})

for (const hit of result.hits) {
  console.log(hit.document.key, hit.score)
}
```

The shorthand form matches the roadmap API:

```ts
await suite.search("postgres backups", "fts")
await suite.search("semantic query", "vector")
await suite.search("best default", "hybrid")
```

## REST

Index or update a document:

```bash
curl -X PUT "$AF_STACK_URL/api/v1/search/documents" \
  -H "Authorization: Bearer $AF_STACK_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "namespace": "docs",
    "key": "doc_123",
    "title": "SOC 2 data retention",
    "body": "Customer exports are retained for 30 days.",
    "metadata": {"plan": "enterprise"},
    "embed": true
  }'
```

Search:

```bash
curl -X POST "$AF_STACK_URL/api/v1/search" \
  -H "Authorization: Bearer $AF_STACK_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "retention exports",
    "mode": "hybrid",
    "namespace": "docs",
    "metadata_filter": {"plan": "enterprise"},
    "limit": 5
  }'
```

Delete:

```bash
curl -X DELETE \
  "$AF_STACK_URL/api/v1/search/documents/docs/doc_123" \
  -H "Authorization: Bearer $AF_STACK_API_KEY"
```
