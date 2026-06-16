# BackAI Adapter Protocol Audit — Multimodal / LLM-Chat / Auth

Audit date: 2026-06-15. Scope: protocol v1 drafts vs. real connector APIs that a
third-party adapter author would actually have to bind to. The bar: would a
sane engineer at ElevenLabs / Helicone / Clerk be **blocked** from shipping an
adapter, or just mildly inconvenienced?

Bias: ship-as-is unless something is structurally broken.

---

## Slot 1 — `multimodal-v1`

### Reality check vs. connectors

| Connector | Surface needed | Protocol coverage |
|---|---|---|
| OpenAI TTS / Whisper / DALL-E 3 | `/audio/speech`, `/audio/transcriptions`, `/images/generations` | Full — verbatim shape match |
| ElevenLabs TTS (non-streaming) | text → mp3/pcm with voice id | Covered by `/audio/speech` |
| ElevenLabs streaming TTS | chunked audio over HTTP / WS | **Reserved, not specified** |
| ElevenLabs voice cloning | `POST /voices/add` (multipart, sample audio) | **Not in protocol** |
| ElevenLabs STT (Scribe) | audio → JSON | Covered by `/audio/transcriptions` |
| Cartesia Sonic (HTTP) | text → audio chunks, sub-second TTFB | Covered for non-stream; streaming reserved |
| Cartesia WS realtime | bidirectional WS | **Not in protocol** |
| Flux / BFL | prompt → image | Covered by `/images/generations` |
| fal.ai | sync + queued/webhook jobs | Sync covered; async **not in protocol** |
| Replicate | `POST /predictions` returning a prediction id, webhook on done | Async **not in protocol** |
| Stability AI | image gen/edit | Covered |
| Runway / Tavus / Synthesia (video) | image-to-video, talking-head | **Not in protocol** |
| Anthropic vision | inside chat, not a separate verb | Out of scope (belongs to `llm-chat`) |

### Must-fix (block a real adapter)

None. The four verbs covered (TTS / STT / image-gen / image-edit) are enough
that ElevenLabs (non-stream), Cartesia (non-stream), OpenAI multimodal, Flux,
fal.ai (sync), Stability, and Replicate (sync via `wait` parameter) can all
ship clean adapters today. `model_prefixes` routing plus capability flags
keeps the runtime honest about what each adapter does and doesn't do.

The one wart worth fixing now because it's free: the spec says
`supports_streaming_tts` is "reserved" but provides no protocol behavior.
Recommend either (a) deleting the flag from v1 capabilities to avoid a
declared-but-unusable feature, or (b) documenting that adapters set it to
`false` in v1 regardless of upstream support. Today an honest adapter author
has no way to act on the flag.

### Nice-to-have (defer to v2)

1. **Streaming TTS** — Cartesia/ElevenLabs streaming is real and useful but
   not required for a v1 ship. The audio bytes-out shape already works for
   sub-second non-streamed TTS. Add as `Transfer-Encoding: chunked` audio
   response when streamed; no new endpoint needed.
2. **Video generation verb** — Runway/Tavus/Synthesia are a separate
   category. Add `/v1/video/generations` + `/v1/video/edits` in v2 with its
   own capability flags. Don't shoehorn into images.
3. **Voice cloning / voice CRUD** — ElevenLabs-specific. Most adapters won't
   implement it. Defer; can live as `/v1/voices` with `supports_voice_cloning`
   flag in v2.
4. **Async job pattern** — Replicate, fal queue, long-running video. Add a
   `job_id` + polling/webhook idiom in v2 (`/v1/jobs/{id}`,
   `webhook_url` request field). For v1, adapters wrap async upstreams by
   blocking the response — slow but correct.
5. **WebSocket realtime** — Cartesia/OpenAI Realtime. Different protocol
   class entirely; doesn't belong inside an HTTP adapter contract. Treat as a
   separate slot when needed.
6. **Image-to-video / model-specific endpoints** — covered by (2).

### Verdict: **SHIP AS-IS**

Only do this before tagging v1: clarify or drop `supports_streaming_tts` since
the protocol does not define the response shape for it.

---

## Slot 2 — `llm-chat-v1`

### Reality check vs. connectors

| Connector | Surface | Protocol coverage |
|---|---|---|
| OpenAI direct | OpenAI chat/embeddings | Identical shape |
| Helicone proxy | OpenAI-shape + `Helicone-*` headers | Body covered; header pass-through documented |
| Portkey | OpenAI-shape + `x-portkey-*` + adapter-side fallbacks | Body + headers covered; `supports_fallback_chains` flag exposes adapter routing |
| OpenLLMetry / Langfuse | side-channel tracing via `traceparent` / `langfuse-*` | Header pass-through documented |
| Together / Groq / OpenRouter / OpenAI-compat OSS | OpenAI shape | Full |
| vLLM / TGI / LocalAI | OpenAI-compat | Full |
| Anthropic direct (Messages API) | Different request body (`messages` w/ system separate, `content` blocks), different streaming events | **Translation required inside adapter** — see below |
| Google Gemini direct | `generateContent`, parts/role differences | Same — adapter translates |
| Vision (image_url parts) | OpenAI multi-content shape | Behavior note present, no example body |
| Reasoning models (o1, R1, Kimi-K2, Claude extended thinking) | `reasoning_content` field | Covered |

### Must-fix (block a real adapter)

None structural. Worth tightening before v1 tag:

1. **`supports_vision=true` has no example.** Spec mentions the OpenAI
   array-of-content-parts shape in §7 but never shows the request body. Add
   one canonical example so an adapter author isn't guessing whether
   `content` can be `string | Array<{type, text|image_url}>`. Conformance
   check should also include "send a vision message, expect 200".

2. **Tool-call response shape isn't shown.** §7 says adapters MUST return
   `tool_calls` per OpenAI spec, but the canonical 200 OK example shows only
   `content`. Add a `tool_calls` example in §1 — adapters wrapping Anthropic /
   Gemini need to know exactly what the runtime expects.

Everything else listed in the prompt is genuinely OK:

- **Anthropic Messages translation** — the protocol is explicitly
  OpenAI-shaped and that's the right call. Adapters wrapping non-compat
  providers translate inside. That's what direct-Anthropic adapters are
  *for*. No protocol change needed.
- **Cost headers** — `X-Backai-Response-Cost-Usd` documented. Helicone's
  own `Helicone-*` headers are forwarded verbatim per §5 extension note.
- **Tracing handoff** — `traceparent`, `langfuse-*`, `x-portkey-*`
  pass-through is documented.
- **Streaming + JSON mode** — both flags exist; combination is implicit and
  works (OpenAI honours it natively, every compat wrapper inherits).
- **Embeddings string vs array** — explicitly documented as accepting both.
- **Reasoning content** — covered with example.
- **Fallback / retry coordination** — `supports_fallback_chains` +
  `fallback_chain_default` capability lets Portkey/LiteLLM advertise their
  own routing so the runtime doesn't double-retry.

### Nice-to-have (defer to v2)

1. **Audio in chat content parts** — OpenAI gpt-4o-audio accepts inline
   audio as a content part. Niche; add a `supports_audio_input` capability
   later.
2. **`prediction` / speculative decoding parameters** — OpenAI-only,
   adapter-side feature; not worth standardizing.
3. **Batch endpoint** (`/v1/batches`) — OpenAI batch jobs. Wholly separate
   async lifecycle; deserves its own slot if we want it.
4. **Server-sent `[ERROR]` mid-stream events** — Anthropic emits typed
   error events mid-stream. Currently spec only mandates `[DONE]`. Document
   how to surface mid-stream errors (most adapters just close the SSE).
5. **Structured outputs (`response_format={type:"json_schema",…}`)** —
   newer than `json_object`. Add `supports_structured_output` flag in v2.

### Verdict: **SHIP AS-IS** — with two doc-only nits

Add a vision content-parts example and a `tool_calls` response example
inside `llm-chat-v1.md` before tagging. Both are doc edits, zero behavior
change.

---

## Slot 3 — `auth-v1`

This is the weakest of the three. Several gaps are real.

### Reality check vs. connectors

| Connector | Real surface | Protocol coverage |
|---|---|---|
| Clerk | Short-lived JWT (60s) + refresh; orgs distinct from users; webhooks; impersonation; sign-out endpoint | Verify ✓, user GET ✓, OAuth ✓; **refresh, sign-out, orgs, webhooks, impersonation all missing** |
| WorkOS | SSO/SAML, directory sync, magic links | OAuth shape OK for SSO bootstrap; **no SAML SP-init flow, no magic link send/verify** |
| Auth0 | OAuth + refresh + revocation + Management API | Verify+OAuth OK; **refresh + revoke missing** |
| Supabase Auth | Email/password, magic links, OAuth, refresh tokens | OAuth OK; **magic link send + password sign-in missing** |
| Stytch | Magic links first-class, OTP, passkeys | **Magic link declared in capabilities but no endpoint** |
| NextAuth.js | Library; auth happens in the host app — verify-only surface from BackAI's side | Fine; `/sessions/verify` is enough |
| better-auth (current default) | Library, same as NextAuth | Fine |

### Must-fix (block a real adapter)

1. **`supports_magic_links` declared but no flow.** Capability advertises a
   feature with zero endpoints to invoke it. A Stytch or Supabase adapter
   literally cannot expose magic-link sign-in. Add at least:
   - `POST /v1/magic-links/send` (email → email sent)
   - `POST /v1/magic-links/verify` (token → session)
   …or drop the flag from v1 capabilities. **Pick one.**

2. **`supports_mfa` declared but no enrollment / challenge flow.** The
   protocol only exposes a read-only `mfa_verified` flag on verify. There's
   no way for the adapter to enroll a factor or challenge for a code. If
   "MFA support" means "we read a flag", say so explicitly and rename the
   capability `reports_mfa_state`. Otherwise add:
   - `POST /v1/mfa/enroll` (start enrollment, return setup secret/QR)
   - `POST /v1/mfa/verify` (submit code)

3. **No session revocation / sign-out.** Every real-world auth provider
   exposes one. `POST /v1/sessions/revoke` is roughly four lines of spec and
   is required for any adapter whose users expect a working logout button.
   Without it, BackAI logout is implicit (drop the cookie client-side) and
   never tells the upstream. Clerk, Auth0, WorkOS all care.

4. **No refresh token endpoint.** Clerk's session tokens are deliberately
   short-lived (≈60s); the runtime needs a way to exchange a refresh
   credential for a new access token without bouncing the user through
   OAuth again. Add `POST /v1/sessions/refresh` (request: `refresh_token`
   or current session token; response: new `token` + `expires_at`). The
   present "verify only" model forces every Clerk adapter to either short-
   circuit verification or piggyback refresh into `/sessions/verify`, which
   the spec doesn't sanction.

### Nice-to-have (defer to v2)

1. **Org / tenant model.** `tenant_id` is returned but its relationship to
   Clerk's `org_id` (a user can belong to many orgs) is undefined. For v1,
   document that `tenant_id` is whichever active org/workspace the upstream
   considers current; adapter picks. v2 should add
   `GET /v1/users/{id}/tenants` for multi-org listing + a
   `POST /v1/sessions/switch-tenant` switch.

2. **User CRUD.** Create / update / delete user are admin operations; most
   BackAI apps shouldn't expose them directly. Defer behind a future
   `supports_user_admin` capability with `POST /v1/users`,
   `PATCH /v1/users/{id}`, `DELETE /v1/users/{id}`.

3. **Impersonation.** "Sign in as user" is a Clerk/Auth0 admin feature, low
   priority for v1. Add `POST /v1/users/{id}/impersonate` later behind a
   capability flag.

4. **SAML SP-initiated flow.** WorkOS / Auth0 enterprise SSO frequently
   needs SP-initiated SAML (POST binding with RelayState). Today the OAuth
   endpoints cover IdP-initiated OIDC, which gets 80% of "SSO". Defer the
   full SAML dance to v2; document that `supports_sso=true` means
   OIDC-style enterprise connection in v1.

5. **Webhook ingress** (`user.created`, `session.revoked`, `org.updated`
   from the provider). Provider → BackAI is an inversion of the protocol's
   direction; better modeled as a separate `auth-webhooks-v1` mini-protocol
   later. Many startup adapters can survive without it by polling
   `/sessions/verify`.

6. **PKCE / state storage details.** Spec mentions CSRF state but not where
   it lives. In practice the adapter holds it; document explicitly.

### Verdict: **NOT SHIP-AS-IS**

Auth v1 needs three small additions before any real third-party adapter (Clerk,
Stytch, Auth0, Supabase) can ship cleanly:

- Add `POST /v1/sessions/refresh`.
- Add `POST /v1/sessions/revoke`.
- Resolve the `supports_magic_links` and `supports_mfa` capability-vs-endpoint
  mismatch (either add `magic-links/{send,verify}` and `mfa/{enroll,verify}`
  endpoints, or strip the flags from v1).

Everything else — orgs, user CRUD, impersonation, SAML specifics, webhooks —
can wait for v2 with no harm. The current better-auth / NextAuth defaults
still work as the `/sessions/verify` surface is fine for library-style auth.

---

## Summary

| Slot | Verdict | Required before tag |
|---|---|---|
| `multimodal-v1` | Ship as-is | Clarify or drop `supports_streaming_tts` (doc only) |
| `llm-chat-v1` | Ship as-is | Add vision + `tool_calls` example bodies (doc only) |
| `auth-v1` | **Hold** | Add `/sessions/refresh`, `/sessions/revoke`, fix magic-link & MFA capability/endpoint mismatch |

Net effort: three new endpoints in auth (~roughly a half-day of spec + a few
days of adapter conformance work) and two doc edits elsewhere. The
multimodal and llm-chat protocols are in good shape and reflect how their
respective ecosystems actually work.
