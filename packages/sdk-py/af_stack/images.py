# SPDX-License-Identifier: Apache-2.0

"""``suite.images.*`` — OpenAI-compatible image generation.

These wrap the runtime's multimodal endpoints (item #14). The wire
shape matches the OpenAI Python SDK so ``client.images.generate(...)``
works against ``http://localhost:8080/api/v1``.

==========================  ================================================
SDK call                    Endpoint
==========================  ================================================
:func:`generate`            ``POST /api/v1/images/generations``
:func:`edit`                ``POST /api/v1/images/edits``        (multipart)
:func:`variations`          ``POST /api/v1/images/variations``   (multipart)
==========================  ================================================

Models route to the right adapter from the prefix:

  - ``openai/dall-e-2`` / ``openai/dall-e-3`` / ``openai/gpt-image-1``
        → LiteLLM → OpenAI
  - ``flux/schnell`` / ``flux/dev`` / ``flux/pro`` / ``flux/pro-ultra``
        → Flux adapter (via fal.ai when FAL_API_KEY set, else Replicate)
  - ``fal/<model>``                  → fal.ai adapter
"""

from __future__ import annotations

from typing import IO, Any

from pydantic import BaseModel, ConfigDict, Field

from . import _http


class ImageItem(BaseModel):
    """One image in the response ``data[]``.

    Either ``url`` or ``b64_json`` is populated depending on the
    request's ``response_format`` (default ``url``).
    """

    model_config = ConfigDict(extra="allow")

    url: str | None = None
    b64_json: str | None = None
    revised_prompt: str | None = None


class ImageGenerationResult(BaseModel):
    """Response from ``generate`` / ``edit`` / ``variations``.

    Mirrors the OpenAI ``images.generate`` response shape (
    ``{"created": int, "data": [...]}``).
    """

    model_config = ConfigDict(extra="allow")

    created: int = 0
    data: list[ImageItem] = Field(default_factory=list)


async def generate(
    model: str,
    prompt: str,
    *,
    n: int = 1,
    size: str = "1024x1024",
    quality: str | None = None,
    style: str | None = None,
    response_format: str = "url",
    user: str | None = None,
    **extra: Any,
) -> ImageGenerationResult:
    """Generate images from a prompt."""
    if not model:
        raise ValueError("model must be a non-empty string")
    if not prompt:
        raise ValueError("prompt must be a non-empty string")
    payload: dict[str, Any] = {
        "model": model,
        "prompt": prompt,
        "n": n,
        "size": size,
        "response_format": response_format,
    }
    if quality is not None:
        payload["quality"] = quality
    if style is not None:
        payload["style"] = style
    if user is not None:
        payload["user"] = user
    for k, v in extra.items():
        if v is not None:
            payload[k] = v
    body = await _http.request_json("POST", "/images/generations", json=payload)
    return ImageGenerationResult.model_validate(body or {"data": []})


async def edit(
    model: str,
    image: bytes | IO[bytes],
    prompt: str,
    *,
    mask: bytes | IO[bytes] | None = None,
    image_filename: str = "image.png",
    mask_filename: str = "mask.png",
    n: int = 1,
    size: str = "1024x1024",
    response_format: str = "url",
    **extra: Any,
) -> ImageGenerationResult:
    """Edit an image given a prompt (and optional mask) via multipart upload."""
    if not model:
        raise ValueError("model must be a non-empty string")
    if not prompt:
        raise ValueError("prompt must be a non-empty string")
    image_bytes = image if isinstance(image, (bytes, bytearray)) else image.read()

    files: dict[str, Any] = {
        "image": (image_filename, bytes(image_bytes), "image/png"),
    }
    if mask is not None:
        mask_bytes = mask if isinstance(mask, (bytes, bytearray)) else mask.read()
        files["mask"] = (mask_filename, bytes(mask_bytes), "image/png")
    data: dict[str, Any] = {
        "model": model,
        "prompt": prompt,
        "n": str(n),
        "size": size,
        "response_format": response_format,
    }
    for k, v in extra.items():
        if v is not None:
            data[k] = v if isinstance(v, str) else str(v)
    return await _multipart_post("/images/edits", data, files)


async def variations(
    model: str,
    image: bytes | IO[bytes],
    *,
    image_filename: str = "image.png",
    n: int = 1,
    size: str = "1024x1024",
    response_format: str = "url",
    **extra: Any,
) -> ImageGenerationResult:
    """Generate variations of an image via multipart upload."""
    if not model:
        raise ValueError("model must be a non-empty string")
    image_bytes = image if isinstance(image, (bytes, bytearray)) else image.read()
    files: dict[str, Any] = {
        "image": (image_filename, bytes(image_bytes), "image/png"),
    }
    data: dict[str, Any] = {
        "model": model,
        "n": str(n),
        "size": size,
        "response_format": response_format,
    }
    for k, v in extra.items():
        if v is not None:
            data[k] = v if isinstance(v, str) else str(v)
    return await _multipart_post("/images/variations", data, files)


# ─── helpers ──────────────────────────────────────────────────────────────


async def _multipart_post(
    path: str, data: dict[str, Any], files: dict[str, Any]
) -> ImageGenerationResult:
    client = _http.get_client()
    headers = {k: v for k, v in _http._build_headers().items() if k != "accept"}
    headers["accept"] = "application/json"
    response = await client.request("POST", path, data=data, files=files, headers=headers)
    _http._raise_for_error(response)
    body = response.json() if response.content else {"data": []}
    return ImageGenerationResult.model_validate(body)


__all__ = [
    "ImageGenerationResult",
    "ImageItem",
    "edit",
    "generate",
    "variations",
]
