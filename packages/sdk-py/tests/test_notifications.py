"""HTTP-mocked tests for the ``af_stack.notifications`` module.

Endpoint paths and JSON shapes mirror ``apps/dashboard/src/lib/api.ts``
(the canonical contract). The shared httpx client is reset between
tests so each test sees a clean client bound to a deterministic base
URL and API key.
"""

from __future__ import annotations

import json

import httpx
import pytest
import respx

from af_stack import notifications, suite
from af_stack._http import AFStackError
from af_stack._http import close as http_close

BASE = "http://localhost:8080/api/v1"


def _row(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "id": "ntf_123",
        "tenant_id": "00000000-0000-0000-0000-000000000000",
        "kind": "email",
        "adapter": "log",
        "template": "default",
        "to": "user@example.com",
        "from": "noreply@af-stack.local",
        "subject": "Hello",
        "data": {"body": "hi"},
        "status": "queued",
        "provider_message_id": None,
        "attempts": 0,
        "last_error": None,
        "scheduled_at": "2026-06-06T12:00:00Z",
        "sent_at": None,
        "created_at": "2026-06-06T12:00:00Z",
    }
    base.update(overrides)
    return base


@pytest.fixture(autouse=True)
async def reset_http_client(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("AF_STACK_URL", "http://localhost:8080")
    monkeypatch.setenv("AF_STACK_API_KEY", "test-key")
    await http_close()
    yield
    await http_close()


# ─── send ─────────────────────────────────────────────────────────────────


async def test_send_posts_payload_with_defaults() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/notifications").mock(
            return_value=httpx.Response(200, json=_row())
        )
        n = await notifications.send(to="user@example.com")

    assert n.id == "ntf_123"
    assert n.kind == "email"
    assert n.status == "queued"
    assert n.template == "default"
    assert n.from_ == "noreply@af-stack.local"

    body = json.loads(route.calls.last.request.read().decode())
    assert body["to"] == "user@example.com"
    assert body["kind"] == "email"
    assert body["template"] == "default"
    # Optional fields must NOT appear when caller didn't supply them.
    assert "from" not in body
    assert "subject" not in body
    assert "data" not in body
    assert "scheduled_at" not in body


async def test_send_forwards_subject_from_data_and_scheduled() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/notifications").mock(
            return_value=httpx.Response(200, json=_row())
        )
        await notifications.send(
            to="user@example.com",
            template="welcome",
            subject="Welcome!",
            from_="hello@af.dev",
            data={"name": "Ada"},
            scheduled_at="2026-06-07T10:00:00Z",
        )

    body = json.loads(route.calls.last.request.read().decode())
    assert body["subject"] == "Welcome!"
    assert body["from"] == "hello@af.dev"
    assert body["data"] == {"name": "Ada"}
    assert body["scheduled_at"] == "2026-06-07T10:00:00Z"


async def test_send_validates_inputs() -> None:
    with pytest.raises(ValueError):
        await notifications.send(to="")
    with pytest.raises(ValueError):
        await notifications.send(to="x@x", template="")
    with pytest.raises(ValueError):
        await notifications.send(to="x@x", kind="webhook")  # type: ignore[arg-type]


async def test_send_surfaces_structured_error() -> None:
    error_body = {
        "error": {
            "code": "NOTIFICATIONS_NOT_CONFIGURED",
            "message": "notifications module is not configured on this runtime",
            "request_id": "req_x",
        }
    }
    with respx.mock(assert_all_called=True) as router:
        router.post(f"{BASE}/notifications").mock(
            return_value=httpx.Response(503, json=error_body)
        )
        with pytest.raises(AFStackError) as exc:
            await notifications.send(to="user@example.com")
    assert exc.value.code == "NOTIFICATIONS_NOT_CONFIGURED"
    assert exc.value.status_code == 503


# ─── email ────────────────────────────────────────────────────────────────


async def test_email_folds_body_into_data_field() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/notifications").mock(
            return_value=httpx.Response(200, json=_row())
        )
        await notifications.email(
            to="user@example.com",
            subject="Hello there",
            body="welcome to AF Stack",
        )

    body = json.loads(route.calls.last.request.read().decode())
    assert body["kind"] == "email"
    assert body["subject"] == "Hello there"
    assert body["data"]["body"] == "welcome to AF Stack"


async def test_email_requires_subject() -> None:
    with pytest.raises(ValueError):
        await notifications.email("user@example.com", "")


# ─── list ─────────────────────────────────────────────────────────────────


async def test_list_sends_filters_as_query_params() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/notifications").mock(
            return_value=httpx.Response(
                200,
                json={
                    "notifications": [_row(id="a"), _row(id="b")],
                    "total": 2,
                    "has_more": False,
                },
            )
        )
        out = await notifications.list(
            tenant="t_x", status="sent", kind="email", limit=10, offset=20
        )

    assert out.total == 2
    assert [r.id for r in out.notifications] == ["a", "b"]
    params = route.calls.last.request.url.params
    assert params["tenant"] == "t_x"
    assert params["status"] == "sent"
    assert params["kind"] == "email"
    assert params["limit"] == "10"
    assert params["offset"] == "20"


async def test_list_handles_empty_response() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/notifications").mock(
            return_value=httpx.Response(
                200,
                json={"notifications": [], "total": 0, "has_more": False},
            )
        )
        out = await notifications.list()
    assert out.notifications == []
    assert out.total == 0
    assert out.has_more is False


# ─── get ──────────────────────────────────────────────────────────────────


async def test_get_returns_row() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/notifications/ntf_123").mock(
            return_value=httpx.Response(200, json=_row(status="sent"))
        )
        n = await notifications.get("ntf_123")
    assert n.id == "ntf_123"
    assert n.status == "sent"


async def test_get_rejects_empty_id() -> None:
    with pytest.raises(ValueError):
        await notifications.get("")


# ─── stats ────────────────────────────────────────────────────────────────


async def test_stats_returns_kpi_aggregates() -> None:
    payload = {
        "by_status": {"sent": 12, "failed": 1, "queued": 3},
        "by_adapter": [{"adapter": "log", "count": 16}],
        "sent_today": 12,
        "failed_today": 1,
    }
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/notifications/stats").mock(
            return_value=httpx.Response(200, json=payload)
        )
        s = await notifications.stats()
    assert s.by_status == {"sent": 12, "failed": 1, "queued": 3}
    assert len(s.by_adapter) == 1
    assert s.by_adapter[0].adapter == "log"
    assert s.by_adapter[0].count == 16
    assert s.sent_today == 12
    assert s.failed_today == 1


# ─── namespace ────────────────────────────────────────────────────────────


async def test_suite_notifications_namespace_matches_module() -> None:
    assert suite.notifications is notifications
    assert suite.notifications.send is notifications.send
    assert suite.notifications.email is notifications.email
    assert suite.notifications.list is notifications.list
    assert suite.notifications.get is notifications.get
    assert suite.notifications.stats is notifications.stats
