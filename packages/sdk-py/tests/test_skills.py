# SPDX-License-Identifier: Apache-2.0

"""HTTP-mocked tests for ``af_stack.admin.skills``.

Endpoint paths and JSON shapes mirror the canonical contract in
``apps/dashboard/src/lib/api.ts`` (Phase 11.3 Skills section).
"""

from __future__ import annotations

import httpx
import pytest
import respx

from af_stack import suite
from af_stack._http import AFStackError
from af_stack._http import close as http_close
from af_stack.admin import skills

BASE = "http://localhost:8080/api/v1"


def _skill_row(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "id": "af-skill://acme/security-audit@1.2.0",
        "name": "security-audit",
        "version": "1.2.0",
        "source": "af-skill://acme/security-audit@1.2.0",
        "description": "Adversarial security review overlay.",
        "harnesses": ["claude-code", "codex"],
        "tags": ["security", "review"],
        "tenant_id": None,
        "installed_at": "2026-06-07T12:00:00Z",
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


# ─── list ────────────────────────────────────────────────────────────────


async def test_list_returns_skills() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/skills").mock(
            return_value=httpx.Response(
                200,
                json={
                    "skills": [
                        _skill_row(),
                        _skill_row(id="embedded:default", name="default"),
                    ]
                },
            )
        )
        result = await skills.list()
    assert [s.id for s in result.skills] == [
        "af-skill://acme/security-audit@1.2.0",
        "embedded:default",
    ]
    assert result.skills[0].harnesses == ["claude-code", "codex"]
    assert result.skills[0].tenant_id is None


async def test_list_passes_filters_as_query() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/skills").mock(
            return_value=httpx.Response(200, json={"skills": []})
        )
        await skills.list(tenant="t-acme", harness="claude-code")
    qs = route.calls.last.request.url.query.decode()
    assert "tenant=t-acme" in qs
    assert "harness=claude-code" in qs


async def test_list_returns_empty_on_no_rows() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/skills").mock(
            return_value=httpx.Response(200, json={"skills": []})
        )
        result = await skills.list()
    assert result.skills == []


# ─── install ─────────────────────────────────────────────────────────────


async def test_install_posts_source_and_returns_skill() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/skills").mock(
            return_value=httpx.Response(200, json=_skill_row())
        )
        result = await skills.install("af-skill://acme/security-audit@1.2.0")
    body = route.calls.last.request.read().decode()
    assert "af-skill" in body
    assert result.name == "security-audit"
    assert result.version == "1.2.0"


async def test_install_includes_tenant_id_when_provided() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/skills").mock(
            return_value=httpx.Response(
                200, json=_skill_row(tenant_id="t-acme")
            )
        )
        result = await skills.install("embedded:default", tenant_id="t-acme")
    body = route.calls.last.request.read().decode()
    assert "t-acme" in body
    assert result.tenant_id == "t-acme"


async def test_install_rejects_empty_source() -> None:
    with pytest.raises(ValueError):
        await skills.install("")


async def test_install_surface_503_as_error() -> None:
    err_body = {
        "error": {
            "code": "SKILLS_NOT_CONFIGURED",
            "message": "skills module is not configured on this runtime",
        }
    }
    with respx.mock(assert_all_called=True) as router:
        router.post(f"{BASE}/skills").mock(
            return_value=httpx.Response(503, json=err_body)
        )
        with pytest.raises(AFStackError) as exc:
            await skills.install("embedded:default")
    assert exc.value.code == "SKILLS_NOT_CONFIGURED"
    assert exc.value.status_code == 503


# ─── uninstall ───────────────────────────────────────────────────────────


async def test_uninstall_issues_delete() -> None:
    skill_id = "af-skill://acme/security-audit@1.2.0"
    with respx.mock(assert_all_called=True) as router:
        # The id is URL-encoded — verify the encoded path matches.
        from urllib.parse import quote
        encoded = quote(skill_id, safe="")
        router.delete(f"{BASE}/skills/{encoded}").mock(
            return_value=httpx.Response(200, json={"deleted": True})
        )
        await skills.uninstall(skill_id)


async def test_uninstall_rejects_empty_id() -> None:
    with pytest.raises(ValueError):
        await skills.uninstall("")


# ─── attach ──────────────────────────────────────────────────────────────


async def test_attach_posts_payload() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/skills/attach").mock(
            return_value=httpx.Response(200, json={"attached": True})
        )
        await skills.attach(
            "af-skill://acme/security-audit@1.2.0",
            "acme.review",
        )
    body = route.calls.last.request.read().decode()
    assert "skill_id" in body
    assert "agent" in body
    assert "acme.review" in body


async def test_attach_rejects_empty_args() -> None:
    with pytest.raises(ValueError):
        await skills.attach("", "acme.review")
    with pytest.raises(ValueError):
        await skills.attach("embedded:default", "")


# ─── namespace ───────────────────────────────────────────────────────────


async def test_suite_admin_skills_namespace() -> None:
    assert suite.admin.skills is skills
    assert suite.admin.skills.install is skills.install
    assert suite.admin.skills.list is skills.list
    assert suite.admin.skills.uninstall is skills.uninstall
    assert suite.admin.skills.attach is skills.attach