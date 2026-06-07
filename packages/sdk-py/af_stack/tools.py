"""``suite.tools.*`` — Model Context Protocol (MCP) server + tool operations.

Maps to the REST surface defined in ``apps/dashboard/src/lib/api.ts`` under
the Phase 11.1 MCP section:

==========================================  ========================================
SDK call                                    Endpoint
==========================================  ========================================
:meth:`Tools.list_mcp_servers`              ``GET    /api/v1/mcp/servers``
:meth:`Tools.add_mcp_server`                ``POST   /api/v1/mcp/servers``
:meth:`Tools.remove_mcp_server`             ``DELETE /api/v1/mcp/servers/{name}``
:meth:`Tools.enable_mcp_server`             ``PUT    /api/v1/mcp/servers/{name}/enabled``
:meth:`Tools.list_mcp_tools`                ``GET    /api/v1/mcp/tools[?server=...]``
:meth:`Tools.call_mcp`                      ``POST   /api/v1/mcp/call``
==========================================  ========================================

MCP servers are external processes (stdio) or HTTP/SSE endpoints that
expose tools to agents. The suite runtime hosts a pool of connections
— one per configured server — and proxies tool calls. Per-tenant
isolation: when MT is on, each tenant can opt servers in/out via the
dashboard.

The :class:`MCPServer`, :class:`MCPTool`, and :class:`MCPCallResult`
Pydantic models mirror the zod schemas in ``lib/api.ts`` exactly — keep
them in sync when the contract changes.
"""

from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

from . import _http

MCPTransport = Literal["stdio", "sse"]
MCPServerStatus = Literal["connecting", "ready", "errored", "disabled"]

_ALLOWED_TRANSPORTS: tuple[str, ...] = ("stdio", "sse")


class MCPServer(BaseModel):
    """One configured MCP server row.

    Mirrors ``MCPServerSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    name: str
    transport: MCPTransport
    command: list[str] = Field(default_factory=list)
    url: str | None = None
    env: dict[str, str] = Field(default_factory=dict)
    tenant_id: str | None = None
    description: str = ""
    is_enabled: bool = True
    status: MCPServerStatus = "connecting"
    last_error: str | None = None
    tools_count: int = 0
    installed_at: str
    last_connected_at: str | None = None


class MCPTool(BaseModel):
    """One tool exposed by an MCP server.

    Mirrors ``MCPToolSchema`` in ``apps/dashboard/src/lib/api.ts``.
    """

    model_config = ConfigDict(extra="allow")

    id: str
    server: str
    name: str
    description: str | None = None
    input_schema: dict[str, Any] = Field(default_factory=dict)


class MCPCallResult(BaseModel):
    """Result of an MCP tool call.

    Mirrors ``CallMCPResultSchema`` in ``apps/dashboard/src/lib/api.ts``.
    The ``content`` array carries the MCP protocol's typed content parts
    (text / image / json / etc.) — kept opaque so callers can pull
    whichever parts they need.
    """

    model_config = ConfigDict(extra="allow")

    content: list[dict[str, Any]] = Field(default_factory=list)
    is_error: bool = False
    duration_ms: int = 0


class Tools:
    """Namespace object exposing MCP server + tool operations.

    Instances of this class are stateless — every method makes a fresh
    HTTP call through the shared :mod:`af_stack._http` client. The
    singleton instance ``tools`` is the canonical entry point; callers
    rarely need to construct their own.
    """

    async def list_mcp_servers(self) -> list[MCPServer]:
        """Return every configured MCP server visible to the caller.

        When multi-tenancy is on, the runtime scopes the list to the
        active tenant; servers with ``tenant_id is None`` (shared
        catalogue) appear for every tenant.
        """
        body = await _http.request_json("GET", "/mcp/servers")
        payload = body or {"servers": []}
        servers_raw = payload.get("servers") if isinstance(payload, dict) else None
        if not isinstance(servers_raw, list):
            return []
        return [MCPServer.model_validate(row) for row in servers_raw]

    async def add_mcp_server(
        self,
        name: str,
        transport: MCPTransport | str,
        *,
        command: list[str] | None = None,
        url: str | None = None,
        env: dict[str, str] | None = None,
        description: str | None = None,
        tenant_id: str | None = None,
    ) -> MCPServer:
        """Register a new MCP server.

        For ``transport="stdio"`` you must supply ``command``; for
        ``transport="sse"`` you must supply ``url``. The runtime will
        attempt to connect immediately and the returned ``status`` will
        reflect the first probe outcome (``ready`` / ``errored``).

        ``env`` entries may reference secrets vault keys via the
        ``"secret:<key>"`` prefix — the runtime resolves them at process
        spawn time so raw secret values never travel over the wire.
        """
        if not name:
            raise ValueError("mcp server name must be a non-empty string")
        if transport not in _ALLOWED_TRANSPORTS:
            allowed = ", ".join(_ALLOWED_TRANSPORTS)
            raise ValueError(
                f"transport must be one of: {allowed}; got {transport!r}"
            )
        if transport == "stdio":
            if not command:
                raise ValueError("stdio transport requires a non-empty command list")
            for arg in command:
                if not isinstance(arg, str):
                    raise ValueError("command entries must all be strings")
        if transport == "sse":
            if not url:
                raise ValueError("sse transport requires a url")

        payload: dict[str, Any] = {"name": name, "transport": transport}
        if command is not None:
            payload["command"] = list(command)
        if url is not None:
            payload["url"] = url
        if env is not None:
            payload["env"] = dict(env)
        if description is not None:
            payload["description"] = description
        if tenant_id is not None:
            payload["tenant_id"] = tenant_id

        body = await _http.request_json("POST", "/mcp/servers", json=payload)
        return MCPServer.model_validate(body or {})

    async def remove_mcp_server(self, name: str) -> None:
        """Remove an MCP server.

        The runtime closes the underlying connection and the row is
        deleted. In-flight tool calls against the server raise an
        ``MCP_SERVER_GONE`` error envelope.
        """
        if not name:
            raise ValueError("mcp server name must be a non-empty string")
        await _http.request_json("DELETE", f"/mcp/servers/{name}")

    async def enable_mcp_server(self, name: str, enabled: bool) -> MCPServer:
        """Enable or disable an MCP server.

        Disabling closes the connection but preserves the configuration
        — the server can be re-enabled later without re-registering it.
        """
        if not name:
            raise ValueError("mcp server name must be a non-empty string")
        body = await _http.request_json(
            "PUT",
            f"/mcp/servers/{name}/enabled",
            json={"enabled": bool(enabled)},
        )
        return MCPServer.model_validate(body or {})

    async def list_mcp_tools(self, server: str | None = None) -> list[MCPTool]:
        """List MCP tools, optionally scoped to a single server.

        The returned ``input_schema`` field is the raw JSON Schema the
        MCP server declared — forwarded unchanged so agents can use it
        for tool-use prompting.
        """
        params: dict[str, Any] = {}
        if server is not None:
            if not server:
                raise ValueError("server filter must be a non-empty string")
            params["server"] = server
        body = await _http.request_json(
            "GET",
            "/mcp/tools",
            params=params if params else None,
        )
        payload = body or {"tools": []}
        tools_raw = payload.get("tools") if isinstance(payload, dict) else None
        if not isinstance(tools_raw, list):
            return []
        return [MCPTool.model_validate(row) for row in tools_raw]

    async def call_mcp(
        self,
        server: str,
        tool: str,
        arguments: dict[str, Any] | None = None,
    ) -> MCPCallResult:
        """Invoke an MCP tool and return its result.

        ``arguments`` must conform to the tool's ``input_schema``; the
        runtime forwards the dict verbatim to the MCP server. The
        returned :class:`MCPCallResult` carries the protocol's typed
        ``content`` parts (text / image / json) and an ``is_error``
        flag — caller code should check the flag before consuming
        content.
        """
        if not server:
            raise ValueError("mcp server name must be a non-empty string")
        if not tool:
            raise ValueError("mcp tool name must be a non-empty string")
        payload: dict[str, Any] = {"server": server, "tool": tool}
        if arguments is not None:
            payload["arguments"] = dict(arguments)
        body = await _http.request_json("POST", "/mcp/call", json=payload)
        return MCPCallResult.model_validate(body or {})


# Singleton — the canonical ``suite.tools`` entry point.
tools = Tools()


__all__ = [
    "MCPCallResult",
    "MCPServer",
    "MCPServerStatus",
    "MCPTool",
    "MCPTransport",
    "Tools",
    "tools",
]
