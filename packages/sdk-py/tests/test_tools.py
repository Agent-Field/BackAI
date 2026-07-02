# SPDX-License-Identifier: Apache-2.0

"""HTTP-mocked tests for the ``af_stack.tools`` module.

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

from af_stack import suite, tools
from af_stack._http import AFStackError
from af_stack._http import close as http_close
from af_stack.tools import (
    MCPCallResult,
    MCPServer,
    MCPTool,
    ToolAdapter,
    ToolAdapterCallResult,
    Tools,
)

BASE = "http://localhost:8080/api/v1"


def _server_row(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "name": "github",
        "transport": "stdio",
        "command": ["uvx", "mcp-server-github"],
        "url": None,
        "env": {"GITHUB_TOKEN": "secret:github_token"},
        "tenant_id": None,
        "description": "GitHub MCP server",
        "is_enabled": True,
        "status": "ready",
        "last_error": None,
        "tools_count": 12,
        "installed_at": "2026-06-06T10:00:00Z",
        "last_connected_at": "2026-06-06T12:00:00Z",
    }
    base.update(overrides)
    return base


def _tool_row(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "id": "github/search_repos",
        "server": "github",
        "name": "search_repos",
        "description": "Search GitHub repositories",
        "input_schema": {
            "type": "object",
            "properties": {"q": {"type": "string"}},
            "required": ["q"],
        },
    }
    base.update(overrides)
    return base


def _adapter_row(**overrides: object) -> dict[str, object]:
    base: dict[str, object] = {
        "id": "http",
        "label": "HTTP",
        "description": "Make outbound HTTP calls",
        "enabled": True,
        "configured": True,
        "default_enabled": True,
        "config": {},
        "updated_at": None,
        "tools": [
            {
                "name": "request",
                "description": "Perform one HTTP request",
                "input_schema": {"type": "object"},
            }
        ],
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


# ─── list_mcp_servers ─────────────────────────────────────────────────────


async def test_list_mcp_servers_parses_rows() -> None:
    payload = {
        "servers": [
            _server_row(name="github"),
            _server_row(
                name="acme-search",
                transport="sse",
                command=[],
                url="https://mcp.acme.com/sse",
                env={},
                status="connecting",
                last_connected_at=None,
                tools_count=0,
            ),
        ]
    }
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/mcp/servers").mock(
            return_value=httpx.Response(200, json=payload)
        )
        servers = await tools.list_mcp_servers()

    assert route.calls.last.request.method == "GET"
    assert [s.name for s in servers] == ["github", "acme-search"]
    assert servers[0].transport == "stdio"
    assert servers[0].is_enabled is True
    assert servers[0].command == ["uvx", "mcp-server-github"]
    assert servers[1].transport == "sse"
    assert servers[1].url == "https://mcp.acme.com/sse"


async def test_list_mcp_servers_handles_empty_body() -> None:
    with respx.mock(assert_all_called=True) as router:
        router.get(f"{BASE}/mcp/servers").mock(
            return_value=httpx.Response(200, json={"servers": []})
        )
        servers = await tools.list_mcp_servers()
    assert servers == []


# ─── add_mcp_server ───────────────────────────────────────────────────────


async def test_add_mcp_server_stdio_posts_payload() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/mcp/servers").mock(
            return_value=httpx.Response(200, json=_server_row())
        )
        result = await tools.add_mcp_server(
            "github",
            "stdio",
            command=["uvx", "mcp-server-github"],
            env={"GITHUB_TOKEN": "secret:github_token"},
            description="GitHub MCP server",
        )

    assert isinstance(result, MCPServer)
    assert result.name == "github"
    body = json.loads(route.calls.last.request.read().decode())
    assert body["name"] == "github"
    assert body["transport"] == "stdio"
    assert body["command"] == ["uvx", "mcp-server-github"]
    assert body["env"] == {"GITHUB_TOKEN": "secret:github_token"}
    assert body["description"] == "GitHub MCP server"
    # Optional fields must NOT appear when caller didn't supply them.
    assert "url" not in body
    assert "tenant_id" not in body


async def test_add_mcp_server_sse_requires_url() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/mcp/servers").mock(
            return_value=httpx.Response(
                200,
                json=_server_row(
                    name="acme",
                    transport="sse",
                    command=[],
                    url="https://mcp.acme.com/sse",
                ),
            )
        )
        await tools.add_mcp_server(
            "acme",
            "sse",
            url="https://mcp.acme.com/sse",
            tenant_id="t_xyz",
        )

    body = json.loads(route.calls.last.request.read().decode())
    assert body["transport"] == "sse"
    assert body["url"] == "https://mcp.acme.com/sse"
    assert body["tenant_id"] == "t_xyz"


async def test_add_mcp_server_validates_inputs() -> None:
    with pytest.raises(ValueError):
        await tools.add_mcp_server("", "stdio", command=["x"])
    with pytest.raises(ValueError):
        await tools.add_mcp_server("github", "bogus", command=["x"])
    with pytest.raises(ValueError):
        # stdio without command
        await tools.add_mcp_server("github", "stdio")
    with pytest.raises(ValueError):
        # sse without url
        await tools.add_mcp_server("acme", "sse")


async def test_add_mcp_server_raises_on_structured_error() -> None:
    error_body = {
        "error": {
            "code": "MCP_DUPLICATE_NAME",
            "message": "server name already exists",
            "request_id": "req_x",
        }
    }
    with respx.mock(assert_all_called=True) as router:
        router.post(f"{BASE}/mcp/servers").mock(return_value=httpx.Response(409, json=error_body))
        with pytest.raises(AFStackError) as exc:
            await tools.add_mcp_server("github", "stdio", command=["uvx", "mcp-server-github"])
    assert exc.value.code == "MCP_DUPLICATE_NAME"
    assert exc.value.status_code == 409


# ─── remove_mcp_server ────────────────────────────────────────────────────


async def test_remove_mcp_server_hits_delete() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.delete(f"{BASE}/mcp/servers/github").mock(
            return_value=httpx.Response(200, json={"deleted": True})
        )
        result = await tools.remove_mcp_server("github")
    assert result is None
    assert route.calls.last.request.method == "DELETE"


async def test_remove_mcp_server_rejects_empty_name() -> None:
    with pytest.raises(ValueError):
        await tools.remove_mcp_server("")


# ─── enable_mcp_server ────────────────────────────────────────────────────


async def test_enable_mcp_server_puts_flag() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.put(f"{BASE}/mcp/servers/github/enabled").mock(
            return_value=httpx.Response(200, json=_server_row(is_enabled=False, status="disabled"))
        )
        server = await tools.enable_mcp_server("github", False)
    assert server.is_enabled is False
    assert server.status == "disabled"
    body = json.loads(route.calls.last.request.read().decode())
    assert body == {"enabled": False}


async def test_enable_mcp_server_rejects_empty_name() -> None:
    with pytest.raises(ValueError):
        await tools.enable_mcp_server("", True)


# ─── list_mcp_tools ───────────────────────────────────────────────────────


async def test_list_mcp_tools_unfiltered() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/mcp/tools").mock(
            return_value=httpx.Response(
                200,
                json={"tools": [_tool_row(), _tool_row(id="github/star_repo", name="star_repo")]},
            )
        )
        tool_list = await tools.list_mcp_tools()
    assert [t.name for t in tool_list] == ["search_repos", "star_repo"]
    assert isinstance(tool_list[0], MCPTool)
    assert tool_list[0].input_schema == {
        "type": "object",
        "properties": {"q": {"type": "string"}},
        "required": ["q"],
    }
    params = route.calls.last.request.url.params
    assert "server" not in params


async def test_list_mcp_tools_scoped_to_server() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/mcp/tools").mock(
            return_value=httpx.Response(200, json={"tools": [_tool_row()]})
        )
        await tools.list_mcp_tools(server="github")
    params = route.calls.last.request.url.params
    assert params["server"] == "github"


async def test_list_mcp_tools_rejects_empty_server() -> None:
    with pytest.raises(ValueError):
        await tools.list_mcp_tools(server="")


# ─── call_mcp ─────────────────────────────────────────────────────────────


async def test_call_mcp_posts_arguments() -> None:
    call_payload = {
        "content": [
            {"type": "text", "text": "Found 3 repos"},
            {"type": "json", "json": {"count": 3}},
        ],
        "is_error": False,
        "duration_ms": 412,
    }
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/mcp/call").mock(
            return_value=httpx.Response(200, json=call_payload)
        )
        result = await tools.call_mcp("github", "search_repos", {"q": "agentfield"})

    assert isinstance(result, MCPCallResult)
    assert result.is_error is False
    assert result.duration_ms == 412
    assert len(result.content) == 2
    assert result.content[0]["type"] == "text"

    body = json.loads(route.calls.last.request.read().decode())
    assert body == {
        "server": "github",
        "tool": "search_repos",
        "arguments": {"q": "agentfield"},
    }


async def test_call_mcp_omits_arguments_when_none() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/mcp/call").mock(
            return_value=httpx.Response(
                200,
                json={"content": [], "is_error": False, "duration_ms": 12},
            )
        )
        await tools.call_mcp("github", "list_repos")
    body = json.loads(route.calls.last.request.read().decode())
    assert body == {"server": "github", "tool": "list_repos"}


async def test_call_mcp_validates_inputs() -> None:
    with pytest.raises(ValueError):
        await tools.call_mcp("", "search_repos")
    with pytest.raises(ValueError):
        await tools.call_mcp("github", "")


async def test_call_mcp_propagates_error_flag() -> None:
    error_payload = {
        "content": [{"type": "text", "text": "Rate limited"}],
        "is_error": True,
        "duration_ms": 200,
    }
    with respx.mock(assert_all_called=True) as router:
        router.post(f"{BASE}/mcp/call").mock(return_value=httpx.Response(200, json=error_payload))
        result = await tools.call_mcp("github", "search_repos", {"q": "x"})
    assert result.is_error is True
    assert result.content[0]["text"] == "Rate limited"


# ─── built-in adapters ────────────────────────────────────────────────────


async def test_list_adapters_parses_rows() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.get(f"{BASE}/tools/adapters").mock(
            return_value=httpx.Response(200, json={"adapters": [_adapter_row()]})
        )
        adapters = await tools.list_adapters()

    assert isinstance(adapters[0], ToolAdapter)
    assert adapters[0].id == "http"
    assert adapters[0].default_enabled is True
    assert adapters[0].tools[0].input_schema == {"type": "object"}
    assert route.calls.last.request.method == "GET"


async def test_set_adapter_enabled_puts_config() -> None:
    with respx.mock(assert_all_called=True) as router:
        route = router.put(f"{BASE}/tools/adapters/sql/enabled").mock(
            return_value=httpx.Response(200, json=_adapter_row(id="sql", enabled=True))
        )
        adapter = await tools.set_adapter_enabled("sql", True, config={"max_rows": 25})

    assert adapter.id == "sql"
    body = json.loads(route.calls.last.request.read().decode())
    assert body == {"enabled": True, "config": {"max_rows": 25}}


async def test_call_adapter_posts_arguments() -> None:
    payload = {
        "adapter": "http",
        "tool": "request",
        "status": "succeeded",
        "result": {"status_code": 200},
        "duration_ms": 12,
    }
    with respx.mock(assert_all_called=True) as router:
        route = router.post(f"{BASE}/tools/call").mock(
            return_value=httpx.Response(200, json=payload)
        )
        result = await tools.call_adapter("http", "request", {"url": "https://example.com"})

    assert isinstance(result, ToolAdapterCallResult)
    assert result.duration_ms == 12
    assert result.result == {"status_code": 200}
    body = json.loads(route.calls.last.request.read().decode())
    assert body == {
        "adapter": "http",
        "tool": "request",
        "arguments": {"url": "https://example.com"},
    }


# ─── namespace ────────────────────────────────────────────────────────────


async def test_suite_tools_namespace_matches_module() -> None:
    # Module-level form: ``suite.tools`` is the ``af_stack.tools`` module
    # itself; ``tools.<verb>`` resolves to the same function objects.
    assert suite.tools is tools
    assert suite.tools.list_mcp_servers is tools.list_mcp_servers
    assert suite.tools.call_mcp is tools.call_mcp
    assert suite.tools.call_adapter is tools.call_adapter


def test_tools_class_is_deprecated_shim() -> None:
    # The ``Tools`` class is kept for backward compat but is no longer
    # the canonical surface — instantiating it emits a DeprecationWarning
    # and instance methods forward to the module-level functions.
    import warnings

    with warnings.catch_warnings(record=True) as caught:
        warnings.simplefilter("always")
        instance = Tools()
    assert any(issubclass(w.category, DeprecationWarning) for w in caught)
    # Bound methods still resolve to a callable that forwards verbatim.
    assert callable(instance.list_mcp_servers)
    assert callable(instance.call_mcp)
    assert callable(instance.call_adapter)
