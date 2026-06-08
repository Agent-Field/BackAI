# Multimodal API

AF Stack exposes OpenAI-compatible multimodal endpoints through the
same LLM gateway as chat and embeddings. Calls route to either the
LiteLLM sidecar (for the OpenAI catalog) or a first-party adapter
(ElevenLabs, Cartesia, Flux, fal.ai) based on the model id prefix.

```text
POST /api/v1/audio/speech              ← TTS
POST /api/v1/audio/transcriptions      ← STT
POST /api/v1/audio/translations        ← STT + translate to English
POST /api/v1/images/generations        ← image generation
POST /api/v1/images/edits              ← image edit (multipart)
POST /api/v1/images/variations         ← image variations (multipart)
```

These endpoints do not create AF Stack media state, conversation state,
memory, runs, spans, or traces. Agent state remains owned by
AgentField; AF Stack owns the public gateway, tenancy, policy hooks,
and provider routing.

## Provider Routing

The runtime inspects the `model` parameter to pick the adapter:

| Model prefix              | Verb(s)            | Adapter         | Env key required          |
|---------------------------|--------------------|-----------------|---------------------------|
| `openai/tts-1`, `openai/tts-1-hd` | TTS         | LiteLLM         | `OPENAI_API_KEY`          |
| `openai/whisper-1`        | STT, translate     | LiteLLM         | `OPENAI_API_KEY`          |
| `openai/dall-e-2`, `openai/dall-e-3`, `openai/gpt-image-1` | image gen / edit / variations | LiteLLM | `OPENAI_API_KEY` |
| `elevenlabs/*`            | TTS                | ElevenLabs      | `ELEVENLABS_API_KEY`      |
| `cartesia/*`              | TTS                | Cartesia        | `CARTESIA_API_KEY`        |
| `flux/*`                  | image gen          | Flux (via fal.ai or Replicate) | `FAL_API_KEY` *or* `REPLICATE_API_KEY` |
| `fal/*`                   | image gen          | fal.ai          | `FAL_API_KEY`             |

If a model id matches a prefix whose adapter isn't configured (env key
missing), the runtime returns `503` with a clear message hinting at the
env var to set.

## Cost Tracking

Every multimodal call writes a row to `suite_cost_events` with a
`modality` column (one of `text`, `embedding`, `audio_speech`,
`audio_transcription`, `audio_translation`, `image`, `video`). The
dashboard's cost view breaks spend out by modality.

Per-modality pricing lives in
`services/runtime/internal/pricing/multimodal.go`:

- TTS: `cost_per_million_chars × characters`
- STT: `cost_per_minute × audio_seconds / 60`
- Image: `cost_per_image × n`

## Image Generation

```ts
import { suite } from "@af-stack/sdk"

const response = await suite.images.generate({
  model: "openai/dall-e-3",
  prompt: "a clean architecture diagram for an AI backend",
  responseFormat: "url",
})

const body = await response.json()
```

```python
from af_stack import suite

result = await suite.images.generate(
    model="openai/dall-e-3",
    prompt="a clean architecture diagram for an AI backend",
)
print(result.data[0].url)
```

REST:

```bash
curl -X POST "$AF_STACK_URL/api/v1/images/generations" \
  -H "Authorization: Bearer $AF_STACK_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "openai/dall-e-3",
    "prompt": "a clean architecture diagram for an AI backend",
    "response_format": "url"
  }'
```

## Image Edits + Variations

Both endpoints accept `multipart/form-data` with an `image` file part
(plus a `mask` part for `/images/edits` when masking). LiteLLM tunnels
the body verbatim to OpenAI; first-party adapters that don't support
edits return `ErrUnsupported` and the runtime renders `501`.

```bash
curl -X POST "$AF_STACK_URL/api/v1/images/edits" \
  -H "Authorization: Bearer $AF_STACK_API_KEY" \
  -F model=openai/dall-e-2 \
  -F image=@input.png \
  -F mask=@mask.png \
  -F prompt="add a hat"
```

## Text To Speech

```ts
const response = await suite.audio.speech({
  model: "openai/tts-1",
  input: "Your export is ready.",
  voice: "alloy",
  responseFormat: "mp3",
})
const audio = await response.arrayBuffer()
```

```python
audio_bytes = await suite.audio.speech(
    model="elevenlabs/multilingual",
    input="Welcome to AgentField.",
    voice="Rachel",      # alias → resolved to ElevenLabs voice id
)
with open("welcome.mp3", "wb") as f:
    f.write(audio_bytes)
```

REST:

```bash
curl -X POST "$AF_STACK_URL/api/v1/audio/speech" \
  -H "Authorization: Bearer $AF_STACK_API_KEY" \
  -H "Content-Type: application/json" \
  -o speech.mp3 \
  -d '{
    "model": "cartesia/sonic",
    "input": "Your export is ready.",
    "voice": "<cartesia-voice-id>",
    "response_format": "mp3"
  }'
```

## Transcription + Translation

```ts
const file = new Blob([audioBytes], { type: "audio/mpeg" })
const response = await suite.audio.transcribe({
  model: "openai/whisper-1",
  file,
  filename: "meeting.mp3",
  language: "en",
})
const transcript = await response.json()
```

```python
result = await suite.audio.transcribe(
    model="openai/whisper-1",
    file=open("meeting.mp3", "rb").read(),
    filename="meeting.mp3",
    language="en",
)
print(result.text)
```

Translation works the same way but targets `/audio/translations` so the
output is rendered in English regardless of source language:

```python
result = await suite.audio.translate(
    model="openai/whisper-1",
    file=open("french.mp3", "rb").read(),
    filename="french.mp3",
)
```

## Adding a New Adapter

To wire a new audio/image provider:

1. Create a new package under
   `services/runtime/internal/llmgateway/adapters/<provider>/` with a
   single `.go` file implementing `adapters.MultimodalAdapter`.
2. Recognise the model prefix in `HandlesModel(model string) bool` by
   checking `adapters.HasPrefix(model, "<provider>")`.
3. Add a constructor that reads the provider's env key and returns
   `nil` when unset (so registry membership is opt-in).
4. Wire the new adapter into `buildMultimodal()` in
   `services/runtime/cmd/af-stack/main.go`.
5. Add the model + pricing to `pricing/multimodal.go` and document the
   prefix in the routing table above.

No docker-compose changes are required — adapters speak HTTP.

## Limitations + Roadmap

- Video generation is deferred to a future release.
- ElevenLabs and Cartesia STT (when those providers add it) are not yet
  wired — operators that need them today should route through LiteLLM
  by adding the corresponding model_list entry to
  `apps/backend/litellm-config.yaml`.
- Cost tracking for STT requires the adapter to surface
  `duration_seconds`; LiteLLM does not (yet) expose it for Whisper, so
  STT calls are billed via the fall-through pricing table only when the
  adapter explicitly fills it.
