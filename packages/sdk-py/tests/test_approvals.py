# SPDX-License-Identifier: Apache-2.0

from __future__ import annotations

import httpx
import pytest
import respx

from af_stack import approvals, suite
from af_stack._http import close as http_close

BASE = "http://localhost:8080/api/v1"


def _approval_row(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "id": "appr_1",
        "tenant_id": "tenant_1",
        "requested_by": "user_1",
        "kind": "deploy_to_prod",
        "payload": {"service": "api"},
        "status": "pending",
        "decided_by": None,
        "decided_at": None,
        "decision_note": None,
        "created_at": "2026-06-07T12:00:00Z",
        "updated_at": "2026-06-07T12:00:00Z",
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


async def test_request_posts_payload() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/approvals").mock(
            return_value=httpx.Response(202, json=_approval_row())
        )
        result = await approvals.request(
            kind="deploy_to_prod",
            payload={"service": "api"},
        )
    assert result.kind == "deploy_to_prod"
    assert result.payload["service"] == "api"
    body = route.calls.last.request.read().decode()
    assert "deploy_to_prod" in body


async def test_list_get_and_decide() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/approvals").mock(
            return_value=httpx.Response(
                200,
                json={"approvals": [_approval_row()], "total": 1, "has_more": False},
            )
        )
        result = await approvals.list(status="pending", kind="deploy_to_prod", limit=10)
    assert result.approvals[0].requested_by == "user_1"
    assert route.calls.last.request.url.params["status"] == "pending"

    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/approvals/appr_1").mock(
            return_value=httpx.Response(200, json=_approval_row())
        )
        got = await approvals.get("appr_1")
    assert got.id == "appr_1"

    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/approvals/appr_1/decide").mock(
            return_value=httpx.Response(
                200,
                json=_approval_row(status="approved", decided_by="user_2"),
            )
        )
        decided = await approvals.decide(
            "appr_1",
            status="approved",
            decision_note="ok",
        )
    assert decided.status == "approved"
    assert "decision_note" in route.calls.last.request.read().decode()


async def test_request_rejects_empty_kind() -> None:
    with pytest.raises(ValueError):
        await approvals.request(kind="")


async def test_suite_approvals_namespace_matches_module() -> None:
    assert suite.approvals is approvals
    assert suite.approvals.request is approvals.request
