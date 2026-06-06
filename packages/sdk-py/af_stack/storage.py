"""``suite.storage.*`` — object storage operations.

Maps to the REST surface in ``apps/dashboard/src/lib/api.ts`` under
the Phase 5 Storage section:

==================================  ====================================
SDK call                            Endpoint
==================================  ====================================
:func:`upload`                      ``POST   /api/v1/storage/upload``
:func:`download`                    ``GET    /api/v1/storage/{key}``
:func:`signed_url`                  ``GET    /api/v1/storage/signed-url``
:func:`delete`                      ``DELETE /api/v1/storage/{key}``
:func:`list`                        ``GET    /api/v1/storage``
==================================  ====================================

The :class:`StorageObject`, :class:`StorageList`, and :class:`SignedURL`
Pydantic models mirror the zod schemas in ``lib/api.ts``.
"""

from __future__ import annotations

from typing import IO, Any
from urllib.parse import quote

from pydantic import BaseModel, ConfigDict, Field

from . import _http


class StorageObject(BaseModel):
    """One row of object metadata.

    Mirrors ``StorageObjectSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    key: str
    size: int
    content_type: str | None = None
    last_modified: str
    etag: str | None = None


class StorageList(BaseModel):
    """Paginated list of stored objects.

    Mirrors ``StorageListSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    objects: list[StorageObject] = Field(default_factory=list)
    prefix: str = ""
    next_token: str | None = None


class SignedURL(BaseModel):
    """Short-lived pre-signed URL for direct object access.

    Mirrors ``SignedURLSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    key: str
    url: str
    expires_at: str


async def upload(
    data: bytes | IO[bytes],
    key: str,
    *,
    content_type: str | None = None,
) -> StorageObject:
    """Upload ``data`` to ``key`` and return the persisted object metadata.

    Accepts either raw ``bytes`` or any binary file-like object (the
    contents are read into memory before the request because httpx's
    multipart helper does not yet stream from generic IOs).
    """
    if not key:
        raise ValueError("storage key must be a non-empty string")
    raw = _to_bytes(data)
    # Build a multipart/form-data body. httpx's `files` argument accepts
    # ``(filename, content, content_type)`` tuples; ``data`` carries plain
    # form fields. Together they produce the same envelope the dashboard's
    # ``api.storage.upload`` uses (`key` field + `file` file part).
    files = {
        "file": (key, raw, content_type or "application/octet-stream"),
    }
    form: dict[str, str] = {"key": key}

    client = _http.get_client()
    response = await client.request(
        "POST",
        "/storage/upload",
        files=files,
        data=form,
        headers=_http._build_headers(),  # type: ignore[attr-defined]
        timeout=_http.DEFAULT_TIMEOUT_S,
    )
    _http._raise_for_error(response)  # type: ignore[attr-defined]
    if not response.content:
        raise _http.AFStackError(
            code="BAD_RESPONSE",
            message="upload returned an empty body",
            status_code=response.status_code,
        )
    return StorageObject.model_validate(response.json())


async def download(key: str) -> bytes:
    """Download an object's bytes by key."""
    if not key:
        raise ValueError("storage key must be a non-empty string")
    client = _http.get_client()
    response = await client.request(
        "GET",
        f"/storage/{quote(key, safe='')}",
        headers=_http._build_headers({"accept": "application/octet-stream"}),  # type: ignore[attr-defined]
        timeout=_http.DEFAULT_TIMEOUT_S,
    )
    _http._raise_for_error(response)  # type: ignore[attr-defined]
    return response.content


async def signed_url(key: str, *, ttl_s: int = 3600) -> SignedURL:
    """Return a short-lived pre-signed URL for ``key``.

    The wire endpoint uses ``key`` as a query parameter (not a path
    segment) so a signature can be generated server-side without parsing
    the URL further.
    """
    if not key:
        raise ValueError("storage key must be a non-empty string")
    if ttl_s <= 0:
        raise ValueError("ttl_s must be a positive integer")
    params: dict[str, Any] = {"key": key, "ttl": ttl_s}
    body = await _http.request_json("GET", "/storage/signed-url", params=params)
    return SignedURL.model_validate(body or {})


async def delete(key: str) -> bool:
    """Delete the object at ``key``. Returns ``True`` on success."""
    if not key:
        raise ValueError("storage key must be a non-empty string")
    body = await _http.request_json("DELETE", f"/storage/{quote(key, safe='')}")
    if not isinstance(body, dict):
        return True
    return bool(body.get("deleted", True))


async def list(  # noqa: A001 — mirrors the JS `storage.list()` ergonomics
    *,
    prefix: str = "",
    limit: int = 100,
) -> StorageList:
    """List objects, optionally restricted to keys starting with ``prefix``."""
    params: dict[str, Any] = {"limit": limit}
    if prefix:
        params["prefix"] = prefix
    body = await _http.request_json("GET", "/storage", params=params)
    return StorageList.model_validate(body or {})


def _to_bytes(data: bytes | IO[bytes]) -> bytes:
    if isinstance(data, (bytes, bytearray)):
        return bytes(data)
    read = getattr(data, "read", None)
    if read is None:
        raise TypeError(
            "storage.upload expects bytes or a binary file-like object with read()"
        )
    chunk = read()
    if isinstance(chunk, str):
        raise TypeError(
            "storage.upload requires a binary stream — got text. "
            "Open the file in binary mode (e.g. open(path, 'rb'))."
        )
    return bytes(chunk)


__all__ = [
    "SignedURL",
    "StorageList",
    "StorageObject",
    "delete",
    "download",
    "list",
    "signed_url",
    "upload",
]
