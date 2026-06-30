# SPDX-License-Identifier: Apache-2.0

"""HTTP-mocked tests for the ``af_stack.storage`` module.

Multipart uploads are exercised through respx route-matchers that inspect
the request body, so we exercise the real httpx multipart serialisation
path (no test-only forks).
"""

from __future__ import annotations

import io

import httpx
import pytest
import respx

from af_stack import storage, suite
from af_stack._http import AFStackError
from af_stack._http import close as http_close

BASE = "http://localhost:8080/api/v1"


@pytest.fixture(autouse=True)
async def reset_http_client(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AF_STACK_URL", "http://localhost:8080")
    monkeypatch.setenv("AF_STACK_API_KEY", "test-key")
    await http_close()
    yield
    await http_close()


# ─── upload ────────────────────────────────────────────────────────────────


async def test_upload_sends_multipart_and_parses_object() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/storage/upload").mock(
            return_value=httpx.Response(
                200,
                json={
                    "key": "reports/2026/q2.pdf",
                    "size": 11,
                    "content_type": "application/pdf",
                    "last_modified": "2026-06-06T12:00:00Z",
                    "etag": "abc123",
                },
            )
        )
        obj = await storage.upload(
            b"hello world",
            "reports/2026/q2.pdf",
            content_type="application/pdf",
        )

    assert obj.key == "reports/2026/q2.pdf"
    assert obj.size == 11
    assert obj.content_type == "application/pdf"
    assert obj.etag == "abc123"

    request = route.calls.last.request
    # httpx serialises multipart with a boundary in content-type.
    content_type = request.headers["content-type"]
    assert content_type.startswith("multipart/form-data")
    body = request.read()
    assert b"hello world" in body
    assert b'name="file"' in body
    assert b'name="key"' in body
    assert b"reports/2026/q2.pdf" in body
    assert b"application/pdf" in body


async def test_upload_accepts_file_like_object() -> None:
    buf = io.BytesIO(b"file-contents")
    with respx.mock(assert_all_called=True) as router:
        router.post(f"{BASE}/storage/upload").mock(
            return_value=httpx.Response(
                200,
                json={
                    "key": "logs/today.txt",
                    "size": 13,
                    "content_type": "text/plain",
                    "last_modified": "2026-06-06T12:00:00Z",
                    "etag": None,
                },
            )
        )
        obj = await storage.upload(buf, "logs/today.txt", content_type="text/plain")
    assert obj.key == "logs/today.txt"
    assert obj.size == 13


async def test_upload_rejects_empty_key() -> None:
    with pytest.raises(ValueError):
        await storage.upload(b"x", "")


async def test_upload_rejects_text_streams() -> None:
    # Text streams would produce wrong byte counts; force callers to open
    # files in binary mode.
    with pytest.raises(TypeError):
        await storage.upload(io.StringIO("oops"), "k")  # type: ignore[arg-type]


# ─── download ──────────────────────────────────────────────────────────────


async def test_download_returns_raw_bytes() -> None:
    payload = b"\x00\x01\x02 file body \xff"
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/storage/reports%2Fq2.pdf").mock(
            return_value=httpx.Response(
                200,
                content=payload,
                headers={"content-type": "application/pdf"},
            )
        )
        data = await storage.download("reports/q2.pdf")
    assert data == payload
    assert route.calls.last.request.headers["accept"] == "application/octet-stream"


# ─── signed_url ────────────────────────────────────────────────────────────


async def test_signed_url_passes_key_and_ttl_as_query() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/storage/signed-url").mock(
            return_value=httpx.Response(
                200,
                json={
                    "key": "exports/run-1.csv",
                    "url": "https://signed.example.com/abc?sig=xyz",
                    "expires_at": "2026-06-06T13:00:00Z",
                },
            )
        )
        signed = await storage.signed_url("exports/run-1.csv", ttl_s=7200)

    assert signed.key == "exports/run-1.csv"
    assert signed.url.startswith("https://signed.example.com/")
    assert signed.expires_at == "2026-06-06T13:00:00Z"

    params = route.calls.last.request.url.params
    assert params["key"] == "exports/run-1.csv"
    assert params["ttl"] == "7200"


async def test_signed_url_rejects_non_positive_ttl() -> None:
    with pytest.raises(ValueError):
        await storage.signed_url("k", ttl_s=0)
    with pytest.raises(ValueError):
        await storage.signed_url("k", ttl_s=-5)


# ─── delete ────────────────────────────────────────────────────────────────


async def test_delete_returns_true_on_success() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.delete(f"{BASE}/storage/junk.txt").mock(
            return_value=httpx.Response(200, json={"deleted": True})
        )
        assert await storage.delete("junk.txt") is True


async def test_delete_returns_false_when_runtime_reports_no_op() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.delete(f"{BASE}/storage/missing.txt").mock(
            return_value=httpx.Response(200, json={"deleted": False})
        )
        assert await storage.delete("missing.txt") is False


# ─── list ──────────────────────────────────────────────────────────────────


async def test_list_returns_objects_and_paging() -> None:
    payload = {
        "objects": [
            {
                "key": "reports/q1.pdf",
                "size": 1234,
                "content_type": "application/pdf",
                "last_modified": "2026-04-01T12:00:00Z",
                "etag": "e1",
            },
            {
                "key": "reports/q2.pdf",
                "size": 5678,
                "content_type": "application/pdf",
                "last_modified": "2026-06-01T12:00:00Z",
                "etag": "e2",
            },
        ],
        "prefix": "reports/",
        "next_token": "page_2",
    }
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/storage").mock(return_value=httpx.Response(200, json=payload))
        listing = await storage.list(prefix="reports/", limit=10)

    assert listing.prefix == "reports/"
    assert listing.next_token == "page_2"
    assert [o.key for o in listing.objects] == ["reports/q1.pdf", "reports/q2.pdf"]

    params = route.calls.last.request.url.params
    assert params["prefix"] == "reports/"
    assert params["limit"] == "10"


async def test_list_omits_empty_prefix() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/storage").mock(
            return_value=httpx.Response(
                200,
                json={"objects": [], "prefix": "", "next_token": None},
            )
        )
        await storage.list()
    params = route.calls.last.request.url.params
    assert "prefix" not in params
    assert params["limit"] == "100"


# ─── errors ────────────────────────────────────────────────────────────────


async def test_upload_raises_structured_error() -> None:
    error_body = {
        "error": {
            "code": "STORAGE_FULL",
            "message": "tenant quota exceeded",
            "request_id": "req_x",
        }
    }
    with respx.mock(assert_all_called=True) as router:
        router.post(f"{BASE}/storage/upload").mock(
            return_value=httpx.Response(413, json=error_body)
        )
        with pytest.raises(AFStackError) as exc:
            await storage.upload(b"x", "k")
    assert exc.value.code == "STORAGE_FULL"
    assert exc.value.status_code == 413


async def test_suite_storage_namespace_matches_module() -> None:
    assert suite.storage is storage
    assert suite.storage.upload is storage.upload
