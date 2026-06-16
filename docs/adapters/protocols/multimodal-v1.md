# Multimodal LLM Adapter — Protocol v1

> Inherits from [`PROTOCOL.md`](../PROTOCOL.md).
>
> **Slot:** `multimodal` · **Base path:** `/v1` · **Go interface:**
> `services/runtime/internal/llmgateway/adapters/MultimodalAdapter`

## Purpose

Multimodal adapters handle TTS, STT, image generation, and image
editing — verbs that aren't pure chat completion. The chat-completion
path itself stays with the LLM gateway (LiteLLM) in v1; this protocol
covers everything around it.

Built-in adapters: `litellm` (tunneling adapter for OpenAI-compatible
endpoints), `elevenlabs` (TTS), `cartesia` (TTS), `flux` (image),
`fal` (image).

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/audio/speech` | TTS — text in, audio out |
| `POST` | `/v1/audio/transcriptions` | STT — audio in, text out |
| `POST` | `/v1/audio/translations` | STT + translate to English |
| `POST` | `/v1/images/generations` | Generate images from prompt |
| `POST` | `/v1/images/edits` | Edit an image with a prompt + mask |
| `POST` | `/v1/images/variations` | Produce variations of an image |
| `GET` | `/v1/models` | List models this adapter handles, optionally filtered by verb |
| `GET` | `/v1/capabilities` | Capability declaration |
| `GET` | `/healthz` | Liveness |
| `GET` | `/v1/info` | Optional metadata |

The endpoints intentionally mirror the OpenAI multimodal endpoints so
that adapters can be thin shims over OpenAI-compatible upstreams.

## 1. `POST /v1/audio/speech`

Text-to-speech.

**Request body**:

```json
{
  "model": "openai/tts-1",
  "input": "Hello, world.",
  "voice": "alloy",
  "response_format": "mp3",
  "speed": 1.0
}
```

| Field | Required | Notes |
|---|---|---|
| `model` | yes | Gateway-routable id. Adapter rejects models it doesn't handle with `unsupported_model`. |
| `input` | yes | Text to speak. |
| `voice` | optional | Provider-specific voice id. |
| `response_format` | optional | `mp3`, `opus`, `wav`, `flac`, etc. Adapter falls back to its default. |
| `speed` | optional | 0.25–4.0. 0 or absent → default. |

**Response (200 OK)**: raw audio bytes.

```
Content-Type: audio/mpeg
Content-Length: 12345
X-BackAI-Char-Count: 13
X-BackAI-Model-Used: openai/tts-1
```

`X-BackAI-Char-Count` is the input character count for cost tracking.

## 2. `POST /v1/audio/transcriptions`

Speech-to-text. The request is `multipart/form-data` to allow file
uploads, matching OpenAI's wire shape.

**Request fields** (multipart):

- `file` — the audio bytes
- `model` — gateway-routable model id
- `language` — optional ISO 639-1 hint
- `prompt` — optional priming text
- `response_format` — `json`, `text`, `srt`, `verbose_json`, `vtt`
- `temperature` — optional float

**Response (200 OK)**:

```json
{
  "text": "Hello world.",
  "language": "en",
  "duration": 3.4
}
```

`Content-Type` is `application/json` unless `response_format=text`, in
which case it's `text/plain` and the body is just the transcript.

`X-BackAI-Duration-Seconds: 3.4` is set so the runtime can compute
per-minute cost.

## 3. `POST /v1/audio/translations`

Identical to `/v1/audio/transcriptions` but the response is translated
to English regardless of source language.

## 4. `POST /v1/images/generations`

**Request body**:

```json
{
  "model": "openai/dall-e-3",
  "prompt": "A small red boat on calm water at dawn",
  "n": 1,
  "size": "1024x1024",
  "quality": "standard",
  "style": "vivid",
  "response_format": "url"
}
```

| Field | Required | Notes |
|---|---|---|
| `model` | yes | |
| `prompt` | yes | |
| `n` | optional | Default 1. |
| `size` | optional | Provider-specific (`1024x1024`, etc.). |
| `quality` | optional | `standard` / `hd` etc. |
| `style` | optional | `vivid` / `natural` etc. |
| `response_format` | optional | `url` (default) or `b64_json`. |

**Response (200 OK)**:

```json
{
  "created": 1700000000,
  "data": [
    {
      "url": "https://...",
      "b64_json": "",
      "revised_prompt": "A small red wooden boat ..."
    }
  ]
}
```

Either `url` or `b64_json` is populated per item. `revised_prompt` is
optional (only some providers return one).

## 5. `POST /v1/images/edits`

`multipart/form-data` with fields:

- `image` — original image bytes (PNG)
- `mask` — optional mask image
- `model`, `prompt`, `n`, `size`, `response_format` — same as `/generations`

**Response**: same shape as `/v1/images/generations`.

## 6. `POST /v1/images/variations`

`multipart/form-data` with:

- `image` — source bytes
- `model`, `n`, `size`, `response_format`

**Response**: same shape.

## 7. `GET /v1/models?verb=tts`

List models this adapter handles. Optional `verb` filter:
`tts`, `stt`, `image_generation`, `image_edit`, `image_variation`.

**Response (200 OK)**:

```json
{
  "models": [
    {
      "id": "openai/tts-1",
      "verbs": ["tts"],
      "owner": "openai",
      "context": null,
      "price_per_unit": {"unit": "character", "input_usd": 0.000015}
    },
    {
      "id": "openai/dall-e-3",
      "verbs": ["image_generation"],
      "owner": "openai",
      "price_per_unit": {"unit": "image", "input_usd": 0.04}
    }
  ]
}
```

`price_per_unit.unit` is one of `character`, `second`, `minute`, `image`,
`token`. The runtime uses this for the cost ledger.

## 8. `GET /v1/capabilities`

```json
{
  "name": "elevenlabs",
  "version": "1.0.0",
  "slot": "multimodal",
  "protocol_version": "v1",
  "vendor": "BackAI",
  "capabilities": {
    "supports_tts": true,
    "supports_stt": false,
    "supports_image_generation": false,
    "supports_image_edit": false,
    "supports_image_variation": false,
    "model_prefixes": ["elevenlabs/"],
    "supports_streaming_tts": true,
    "default_voice": "Rachel",
    "max_input_chars": 5000,
    "audio_formats": ["mp3", "pcm_16000"]
  }
}
```

| Key | Type | Meaning |
|---|---|---|
| `supports_tts` / `_stt` / `_image_*` | bool | Verb support flags. The runtime rejects calls to unsupported verbs before dispatch. |
| `model_prefixes` | string[] | Models routed to this adapter by gateway prefix matching. |
| `supports_streaming_tts` | bool | Advisory only. Streaming TTS is NOT a v1 endpoint — `/v1/audio/speech` returns full bytes. Setting this `true` is reserved for v2 protocol when chunked-audio streaming lands. Until then, declare `false` and emit complete audio. |
| `default_voice` | string | Used when caller omits `voice`. |
| `max_input_chars` | int | TTS-specific char cap. |
| `audio_formats` | string[] | Supported `response_format` values for TTS. |

## 9. Error codes

| Code | HTTP | Meaning |
|---|---|---|
| `unsupported_model` | 422 | Model isn't in this adapter's prefix list. |
| `unsupported_verb` | 422 | Verb not declared in capabilities. |
| `input_too_large` | 413 | TTS text or audio exceeds cap. |
| `invalid_audio_format` | 400 | Audio file format not recognized. |
| `invalid_image_format` | 400 | Image not PNG/JPEG/etc. |
| `provider_error` | 502 | Upstream returned an error. |
| `quota_exceeded` | 429 | Upstream throttled. |
| `provider_unavailable` | 503 | Upstream unreachable. |
| `unauthorized` | 401 | Bearer token rejected. |
| `internal_error` | 500 | Catch-all. |

## 10. Behavior notes

- **Pass-through bodies.** Adapters that wrap an OpenAI-compatible
  upstream (like LiteLLM) MAY forward the original request body
  verbatim — the protocol shape is OpenAI-compatible by design.
- **Cost tracking handoff.** Adapters set `X-BackAI-Char-Count`,
  `X-BackAI-Duration-Seconds`, or include token usage in the JSON
  response so the runtime can record cost without re-parsing the body.
- **Streaming TTS.** Reserved; not in v1. Future protocol version will
  add an SSE-style chunked audio response.
- **Image sizing.** Adapters MUST reject unsupported `size` values
  rather than silently rounding.

## 11. Mapping back to the Go interface

| Go method | HTTP call |
|---|---|
| `Name()` | cached from capabilities |
| `HandlesModel(model)` | local check against `capabilities.model_prefixes` |
| `SupportsTTS()` / `STT()` / `Image()` | cached capabilities flags |
| `Speech(ctx, req)` | `POST /v1/audio/speech` |
| `Transcribe(ctx, req)` (with `req.Translate`) | `POST /v1/audio/transcriptions` or `/v1/audio/translations` |
| `Image(ctx, req)` (with `IsEdit` / `IsVariations`) | `POST /v1/images/generations` or `/edits` or `/variations` |

## 12. Conformance checklist

- [ ] `GET /v1/capabilities` declares at least one verb true
- [ ] If `supports_tts=true`: `POST /v1/audio/speech` returns audio bytes for a short text
- [ ] If `supports_stt=true`: `POST /v1/audio/transcriptions` with a known sample returns the expected text
- [ ] If `supports_image_generation=true`: `POST /v1/images/generations` returns at least one item with a URL or b64
- [ ] Calling an unsupported verb returns `422 + unsupported_verb`
- [ ] `GET /v1/models?verb=tts` returns only TTS models
- [ ] Idempotent images request with same key returns same response (cache on adapter side)
- [ ] Bearer auth enforced
