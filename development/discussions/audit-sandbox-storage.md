# Adapter Protocol Audit — sandbox-v1 / storage-v1

Audited against the real APIs of: e2b, Modal, Daytona, Replit (sandbox);
AWS S3, Cloudflare R2, Backblaze B2, GCS, Azure Blob, MinIO (storage).

The framing question for each gap: **can a real third-party adapter
implement the protocol without us changing it?** Theoretical
deficiencies don't count.

---

## Sandbox — must-fix gaps

**None.** A startup can write an e2b / Modal / Daytona / Replit adapter
against `sandbox-v1.md` as-is. Justification per connector:

- **e2b**: `Sandbox.create()` → `commands.run()` → `kill()` is their
  persistent flow, but they expose `runCode` / `Sandbox.create(...).then(s
  => s.commands.run(cmd).then(() => s.kill()))` for one-shot. An adapter
  wraps that into `POST /v1/runs` cleanly. Files map to e2b's `files.write`
  before exec. Streaming maps to e2b's `onStdout`/`onStderr` callbacks.
- **Modal**: `Sandbox.create(image=Image.from_registry(...), cpu=N,
  memory=N*1024)` → `sb.exec(cmd, stdout=PIPE)` → `sb.wait()` → `sb.terminate()`.
  All one-shot fields map. `files` becomes `image.add_local_file` or a
  pre-exec `sb.exec(["sh","-c","cat > /work/x"])`-style write.
- **Daytona**: forces a per-run workspace create+destroy, which costs
  ~10–30s cold-start, but the spec covers it (`cold_start_ms` capability
  exists). Functionally implementable.
- **Replit Ghostwriter sandbox**: one-shot exec maps directly.
- **Docker/gVisor/Firecracker** already work in-tree — reference proof.

The protocol is deliberately one-shot. That's the right v1 scope; it
matches Lambda-style invocation, which is what BackAI runtime actually
needs today.

## Sandbox — nice-to-have gaps (defer to v2)

- **Persistent sessions (multi-exec same container).** Required for
  notebook/REPL agents (e2b's killer feature, Modal's `sb.exec` loop).
  Today the runtime doesn't expose REPL semantics, so deferring is fine.
  Add `POST /v1/sessions` + `POST /v1/sessions/{id}/exec` in v2.
- **GPU type selector.** `supports_gpu: bool` is binary; Modal/e2b
  accept `gpu="A100"|"H100"|"T4"`. Add `gpu_type` request field and
  `gpu_types: []string` capability in v2. Until BackAI has a GPU
  pricing/billing story this is moot.
- **Region selector.** Modal accepts `region="us-east"`. Multi-region
  scheduling isn't in BackAI's runtime today.
- **Large file upload (out-of-band).** `files` is inline JSON
  (base64 for binary). Fine for scripts; broken for >1 MB models or
  datasets. v2 should add `files_url: ["s3://..."]` or a pre-upload
  step. Workaround today: caller pre-uploads to storage and the run
  curls it.
- **Stdin streaming.** No request field for interactive stdin.
  Real CLIs sometimes need it (`python -i`, `bash`). Defer — BackAI
  runtime never does interactive.
- **Container exec while running.** e2b/Modal expose this for
  debugging. Operator nice-to-have; not blocking.
- **Custom networking** (VPC, private DNS, sidecars). Enterprise-only;
  out of scope for a v1 third-party plug-in surface.
- **Snapshot/restore** (Modal, Firecracker). Performance optimization;
  not blocking.

---

## Storage — must-fix gaps

**One real concern, two judgment calls.**

### MUST FIX: multipart upload semantics

The protocol declares a `supports_multipart` capability but the
behavior section says "the runtime will use single-part PUT for v1."
That's a contradiction the adapter author can't resolve:

- HTTP `PUT /v1/objects/{key}` with a 50 GB body across the public
  internet **will fail** in practice — proxies, TLS rekey, idle
  timeouts. R2 caps single-PUT at 5 GB; S3 at 5 GB; B2 at 5 GB native
  (200 GB via large-file API); GCS XML PUT at 5 TB but practically <5 GB.
- If the runtime really never sends >5 GB through `PUT
  /v1/objects/{key}`, then **delete the `supports_multipart` capability
  in v1** to remove ambiguity. If the runtime ever wants to stream a
  multi-GB artifact, the protocol must add either:
  - `POST /v1/objects/{key}/multipart` initiate/part/complete/abort
    endpoints (S3-shaped, all real backends already implement), **or**
  - mandate `signed-url?method=PUT` and let the runtime upload
    directly to the backend, bypassing the adapter for the body.

**Recommendation:** pick the second — it's strictly simpler, every
backend supports it, and the adapter never has to proxy bytes. Document
that the runtime MUST use `signed-url` for objects >`single_put_max_bytes`,
and replace `supports_multipart` with `single_put_max_bytes` (int).

### JUDGMENT CALL: batch delete

S3/R2/B2/GCS all expose batch delete (S3 `DeleteObjects` is up to 1000
keys per call, 1 round-trip). Azure does it via batch API. The protocol
forces one `DELETE` per key.

- For tenant offboarding (delete 100k objects) this is 100k HTTP calls
  to the adapter, which then translates each to one backend call —
  ~100× slower than batched. That's not theoretical: BackAI will hit it
  the first time a tenant churns or a workspace is purged.
- **But:** v1 BackAI doesn't have any code path that deletes >10
  objects in one operation today. So it's a real gap only when the
  runtime grows tenant-purge / workspace-purge features.

**Verdict:** ship-as-is for v1, add `POST /v1/objects:batchDelete`
when the first bulk-delete code path lands runtime-side. Adapters can
pre-implement it without protocol blessing.

### JUDGMENT CALL: server-side copy

S3 `CopyObject`, GCS `rewrite`, R2 copy, Azure `Copy Blob` — all
support copying within a bucket without the client downloading and
re-uploading bytes. The protocol has no copy endpoint, so the runtime
must `GET` then `PUT`, paying bandwidth twice.

- Hot path? Only if BackAI has workspace cloning / snapshotting /
  versioning, which it doesn't yet.
- **Verdict:** defer to v2, add `POST /v1/objects/{key}/copy` with
  `{ "destination": "new/key" }`.

## Storage — nice-to-have gaps (defer to v2)

- **Pre-signed POST (browser direct upload with policy).** S3-flavored
  policy-based POST allows enforcing content-length, content-type, and
  key-prefix constraints from the client. Today BackAI uses presigned
  PUT, which is fine for server-to-server but limited for browser
  uploads with constraints. Defer until the customer-facing UI needs it.
- **Object metadata mutation post-upload.** Only settable at PUT time
  (via `X-BackAI-Metadata-*` headers). S3 forces copy-onto-self for
  this — basically every backend has the same limitation. Acceptable.
- **Object versioning.** S3/GCS/Azure all support versioning. Not in
  protocol. BackAI doesn't model versions today.
- **Object tags** (distinct from metadata; queryable in S3/Azure).
  Defer.
- **Lifecycle / CORS / replication APIs.** Capability bools exist as
  *informational only* — explicitly documented as "no API in v1." This
  is honest, not a gap.
- **Conditional requests** (`If-Match` / `If-None-Match` on PUT for
  optimistic concurrency). Not in protocol. Real backends support it.
  No BackAI code path needs it today.
- **Storage class selector** (S3 STANDARD vs INTELLIGENT_TIERING vs
  GLACIER, B2 hot/archive). Cost optimization, not correctness. Defer.
- **Checksum integrity.** Protocol returns ETag but doesn't define
  whether it's MD5, SHA256, or backend-opaque. S3 ETag for multipart
  isn't MD5. Minor — document that ETag is opaque, and add a separate
  `Content-MD5` / `x-amz-checksum-sha256` request header passthrough
  in v2 for callers that need integrity verification.
- **Range writes / appends.** Azure Append Blob, GCS resumable
  uploads. Not in protocol. Niche.

---

## Verdict

**Sandbox-v1: ship as-is.** The one-shot model is clearly scoped, the
capability flags are honest about what the adapter does and doesn't
support, and every real connector (e2b, Modal, Daytona, Replit,
Firecracker) can implement it without protocol changes. Persistent
sessions and GPU types are real product gaps but become relevant when
BackAI's runtime exposes those features — not before.

**Storage-v1: ship after one fix.** Resolve the multipart contradiction
before shipping:

1. **Remove `supports_multipart` from the capability schema**, or
2. **Replace it with `single_put_max_bytes: int`** and document that
   the runtime MUST switch to `signed-url?method=PUT` for objects
   larger than that — leaving the actual large-upload mechanism to
   the backend's native presigned upload.

Option 2 is the cleaner answer and doesn't add any new endpoints. It
also eliminates the implicit assumption that the adapter will proxy
multi-GB bytes (which would make it a bandwidth bottleneck).

Everything else — batch delete, server-side copy, pre-signed POST,
versioning, tags, lifecycle APIs, checksums — is correctly deferred to
v2. A first storage adapter (R2, B2, GCS) can be written today; the
protocol gaps only show up when BackAI grows specific runtime features
(bulk purge, snapshotting, browser-direct upload).

### One-line summary

- **Sandbox:** ship.
- **Storage:** ship, after deleting or redefining the `supports_multipart`
  capability so it isn't a promise the protocol can't keep.
