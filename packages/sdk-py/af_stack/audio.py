# SPDX-License-Identifier: Apache-2.0

"""``suite.audio.*`` — OpenAI-compatible audio (TTS + STT) operations.

These wrap the runtime's multimodal endpoints (item #14). The wire
shape matches the OpenAI Python SDK so callers can also point the
``openai`` client at ``http://localhost:8080/api/v1`` and use
``client.audio.speech.create(...)`` / ``client.audio.transcriptions.create(...)``
directly.

==========================  ================================================
SDK call                    Endpoint
==========================  ================================================
:func:`speech`              ``POST /api/v1/audio/speech``
:func:`transcribe`          ``POST /api/v1/audio/transcriptions``
:func:`translate`           ``POST /api/v1/audio/translations``
==========================  ================================================

Models are gateway-routable ids that pick the upstream provider:

  - ``openai/tts-1`` / ``openai/tts-1-hd``  → LiteLLM → OpenAI
  - ``openai/whisper-1``                    → LiteLLM → OpenAI
  - ``elevenlabs/multilingual``,
    ``elevenlabs/turbo``, ``elevenlabs/flash`` → ElevenLabs adapter
  - ``cartesia/sonic``, ``cartesia/sonic-multilingual`` → Cartesia adapter

Returns audio bytes for :func:`speech`; structured ``TranscriptionResult``
for the STT verbs (matches OpenAI's response shape).
"""

from __future__ import annotations

from typing import IO, Any

import httpx
from pydantic import BaseModel, ConfigDict

from . import _http


class TranscriptionResult(BaseModel):
    """Result of a transcribe / translate call.

    Mirrors OpenAI's ``audio.transcription`` response shape. Extra
    fields the upstream may add (``language``, ``segments``,
    ``duration``) are preserved via the ``extra='allow'`` config.
    """

    model_config = ConfigDict(extra="allow")

    text: str


async def speech(
    model: str,
    input: str,  # noqa: A002 — mirrors OpenAI's ``input`` parameter
    *,
    voice: str = "alloy",
    response_format: str = "mp3",
    speed: float | None = None,
    **extra: Any,
) -> bytes:
    """Generate speech audio from text. Returns raw audio bytes.

    The ``response_format`` default ("mp3") matches OpenAI; some
    first-party adapters override this (Cartesia → wav). The caller
    sees the bytes regardless of format and can dispatch on the
    response content-type via :func:`speech_with_meta` if needed.
    """
    if not model:
        raise ValueError("model must be a non-empty string")
    if not input:
        raise ValueError("input must be a non-empty string")
    payload: dict[str, Any] = {
        "model": model,
        "input": input,
        "voice": voice,
        "response_format": response_format,
    }
    if speed is not None:
        payload["speed"] = speed
    for k, v in extra.items():
        if v is not None:
            payload[k] = v
    resp_bytes, _ = await _post_for_bytes("/audio/speech", payload)
    return resp_bytes


async def speech_with_meta(
    model: str,
    input: str,  # noqa: A002
    *,
    voice: str = "alloy",
    response_format: str = "mp3",
    speed: float | None = None,
    **extra: Any,
) -> tuple[bytes, str]:
    """Variant of :func:`speech` that returns ``(bytes, content_type)``."""
    if not model:
        raise ValueError("model must be a non-empty string")
    if not input:
        raise ValueError("input must be a non-empty string")
    payload: dict[str, Any] = {
        "model": model,
        "input": input,
        "voice": voice,
        "response_format": response_format,
    }
    if speed is not None:
        payload["speed"] = speed
    for k, v in extra.items():
        if v is not None:
            payload[k] = v
    return await _post_for_bytes("/audio/speech", payload)


async def transcribe(
    model: str,
    file: bytes | IO[bytes],
    *,
    filename: str = "audio.wav",
    language: str | None = None,
    prompt: str | None = None,
    response_format: str | None = None,
    temperature: float | None = None,
    **extra: Any,
) -> TranscriptionResult:
    """Transcribe an audio file. ``file`` is the raw bytes or a file-like."""
    return await _stt_call(
        "/audio/transcriptions",
        model,
        file,
        filename=filename,
        language=language,
        prompt=prompt,
        response_format=response_format,
        temperature=temperature,
        extra=extra,
    )


async def translate(
    model: str,
    file: bytes | IO[bytes],
    *,
    filename: str = "audio.wav",
    prompt: str | None = None,
    response_format: str | None = None,
    temperature: float | None = None,
    **extra: Any,
) -> TranscriptionResult:
    """Transcribe + translate audio to English."""
    return await _stt_call(
        "/audio/translations",
        model,
        file,
        filename=filename,
        prompt=prompt,
        response_format=response_format,
        temperature=temperature,
        extra=extra,
    )


# ─── helpers ──────────────────────────────────────────────────────────────


async def _post_for_bytes(path: str, payload: dict[str, Any]) -> tuple[bytes, str]:
    client = _http.get_client()
    response = await client.request(
        "POST",
        path,
        json=payload,
        headers=_http._build_headers({"accept": "*/*"}),
    )
    _http._raise_for_error(response)
    return response.content, response.headers.get("content-type", "")


async def _stt_call(
    path: str,
    model: str,
    file: bytes | IO[bytes],
    *,
    filename: str = "audio.wav",
    language: str | None = None,
    prompt: str | None = None,
    response_format: str | None = None,
    temperature: float | None = None,
    extra: dict[str, Any] | None = None,
) -> TranscriptionResult:
    if not model:
        raise ValueError("model must be a non-empty string")
    if file is None:
        raise ValueError("file is required")
    file_bytes = file if isinstance(file, (bytes, bytearray)) else file.read()
    files: dict[str, Any] = {"file": (filename, bytes(file_bytes), "application/octet-stream")}
    data: dict[str, Any] = {"model": model}
    if language is not None:
        data["language"] = language
    if prompt is not None:
        data["prompt"] = prompt
    if response_format is not None:
        data["response_format"] = response_format
    if temperature is not None:
        data["temperature"] = str(temperature)
    if extra:
        for k, v in extra.items():
            if v is not None:
                data[k] = v

    client = _http.get_client()
    headers = {k: v for k, v in _http._build_headers().items() if k != "accept"}
    headers["accept"] = "application/json"
    response = await client.request("POST", path, data=data, files=files, headers=headers)
    _http._raise_for_error(response)
    body = response.json()
    if isinstance(body, dict):
        return TranscriptionResult.model_validate(body)
    if isinstance(body, str):
        return TranscriptionResult(text=body)
    return TranscriptionResult(text=str(body))


# Re-bind httpx so static analyzers see the import as used in error paths
_ = httpx


__all__ = [
    "TranscriptionResult",
    "speech",
    "speech_with_meta",
    "transcribe",
    "translate",
]
