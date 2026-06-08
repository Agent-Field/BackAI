# SPDX-License-Identifier: Apache-2.0

"""``suite.tools.*`` — Model Context Protocol (MCP) server + tool operations.

Maps to the REST surface defined in ``apps/dashboard/src/lib/api.ts`` under
the Phase 11.1 MCP section:

==================================  ========================================
SDK call                            Endpoint
==================================  ========================================
:func:`list_mcp_servers`            ``GET    /api/v1/mcp/servers``
:func:`add_mcp_server`              ``POST   /api/v1/mcp/servers``
:func:`remove_mcp_server`           ``DELETE /api/v1/mcp/servers/{name}``
:func:`enable_mcp_server`           ``PUT    /api/v1/mcp/servers/{name}/enabled``
:func:`list_mcp_tools`              ``GET    /api/v1/mcp/tools[?server=...]``
:func:`call_mcp`                    ``POST   /api/v1/mcp/call``
==================================  ========================================

MCP servers are external processes (stdio) or HTTP/SSE endpoints that
expose tools to agents. The suite runtime hosts a pool of connections
— one per configured server — and proxies tool calls. Per-tenant
isolation: when MT is on, each tenant can opt servers in/out via the
dashboard.

The :class:`MCPServer`, :class:`MCPTool`, and :class:`MCPCallResult`
Pydantic models mirror the zod schemas in ``lib/api.ts`` exactly — keep
them in sync when the contract changes.

.. note::
   This module was originally exposed as a ``Tools`` class with bound
   methods. The class is kept as a thin backward-compat shim and will be
   removed in a future major release — callers should use the
   module-level functions (or the ``suite.tools.<verb>`` access form).
"""

from __future__ import annotations

import warnings
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

from . import _http

MCPTransport = Literal["stdio", "sse"]
MCPServerStatus = Literal["connecting", "ready", "errored", "disabled"]
ToolAdapterID = Literal["browser-use", "searxng", "fs", "exec", "http", "sql"]
NativeToolName = Literal["browser", "search", "fs", "exec", "http", "sql"]

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


class BuiltinToolDefinition(BaseModel):
    """One tool exposed by a built-in AF Stack adapter."""

    model_config = ConfigDict(extra="allow")

    name: str
    description: str = ""
    input_schema: dict[str, Any] = Field(default_factory=dict)


class ToolAdapter(BaseModel):
    """Built-in adapter catalogue row plus tenant enablement."""

    model_config = ConfigDict(extra="allow")

    id: ToolAdapterID
    label: str
    description: str = ""
    enabled: bool = False
    configured: bool = False
    default_enabled: bool = False
    config: dict[str, Any] = Field(default_factory=dict)
    updated_at: str | None = None
    tools: list[BuiltinToolDefinition] = Field(default_factory=list)


class ToolAdapterCallResult(BaseModel):
    """Result from ``call_adapter``."""

    model_config = ConfigDict(extra="allow")

    adapter: ToolAdapterID
    tool: str
    status: str
    result: Any | None = None
    duration_ms: int = 0


# ─── Native tools (#16 — strict-interface adapter set) ───────────────────


class NativeToolVerb(BaseModel):
    """One verb exposed by a native tool (input/output schemas)."""

    model_config = ConfigDict(extra="allow")

    name: str
    description: str = ""
    input_schema: dict[str, Any] = Field(default_factory=dict)
    output_schema: dict[str, Any] | None = None


class NativeToolStatus(BaseModel):
    """Per-tenant view of one native tool: active adapter + enable flag."""

    model_config = ConfigDict(extra="allow")

    tool: NativeToolName
    description: str = ""
    adapter_id: str
    configured: bool = False
    enabled: bool = False
    verbs: list[NativeToolVerb] = Field(default_factory=list)
    config: dict[str, Any] = Field(default_factory=dict)
    updated_at: str | None = None


class InvokeNativeToolResult(BaseModel):
    """Result from ``invoke_native``."""

    model_config = ConfigDict(extra="allow")

    tool: NativeToolName
    verb: str
    result: Any | None = None


# ─── Module-level verbs ───────────────────────────────────────────────────


async def list_mcp_servers() -> list[MCPServer]:
    """Return every configured MCP server visible to the caller.

    When multi-tenancy is on, the runtime scopes the list to the active
    tenant; servers with ``tenant_id is None`` (shared catalogue) appear
    for every tenant.
    """
    body = await _http.request_json("GET", "/mcp/servers")
    payload = body or {"servers": []}
    servers_raw = payload.get("servers") if isinstance(payload, dict) else None
    if not isinstance(servers_raw, list):
        return []
    return [MCPServer.model_validate(row) for row in servers_raw]


async def add_mcp_server(
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
    ``transport="sse"`` you must supply ``url``. The runtime will attempt
    to connect immediately and the returned ``status`` will reflect the
    first probe outcome (``ready`` / ``errored``).

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


async def remove_mcp_server(name: str) -> None:
    """Remove an MCP server.

    The runtime closes the underlying connection and the row is deleted.
    In-flight tool calls against the server raise an ``MCP_SERVER_GONE``
    error envelope.
    """
    if not name:
        raise ValueError("mcp server name must be a non-empty string")
    await _http.request_json("DELETE", f"/mcp/servers/{name}")


async def enable_mcp_server(name: str, enabled: bool) -> MCPServer:
    """Enable or disable an MCP server.

    Disabling closes the connection but preserves the configuration —
    the server can be re-enabled later without re-registering it.
    """
    if not name:
        raise ValueError("mcp server name must be a non-empty string")
    body = await _http.request_json(
        "PUT",
        f"/mcp/servers/{name}/enabled",
        json={"enabled": bool(enabled)},
    )
    return MCPServer.model_validate(body or {})


async def list_mcp_tools(server: str | None = None) -> list[MCPTool]:
    """List MCP tools, optionally scoped to a single server.

    The returned ``input_schema`` field is the raw JSON Schema the MCP
    server declared — forwarded unchanged so agents can use it for
    tool-use prompting.
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
    server: str,
    tool: str,
    arguments: dict[str, Any] | None = None,
) -> MCPCallResult:
    """Invoke an MCP tool and return its result.

    ``arguments`` must conform to the tool's ``input_schema``; the
    runtime forwards the dict verbatim to the MCP server. The returned
    :class:`MCPCallResult` carries the protocol's typed ``content``
    parts (text / image / json) and an ``is_error`` flag — caller code
    should check the flag before consuming content.
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


async def list_adapters() -> list[ToolAdapter]:
    """Return built-in adapters and tenant enablement/config.

    This is distinct from MCP servers: these adapters are the AF Stack
    built-ins (HTTP, SQL, exec, fs, SearXNG, browser-use). AgentField still
    owns agent tool-call spans and traces.
    """
    body = await _http.request_json("GET", "/tools/adapters")
    payload = body or {"adapters": []}
    rows = payload.get("adapters") if isinstance(payload, dict) else None
    if not isinstance(rows, list):
        return []
    return [ToolAdapter.model_validate(row) for row in rows]


async def set_adapter_enabled(
    adapter: ToolAdapterID | str,
    enabled: bool,
    *,
    config: dict[str, Any] | None = None,
) -> ToolAdapter:
    """Enable or disable one built-in adapter for the caller tenant."""
    if not adapter:
        raise ValueError("adapter must be a non-empty string")
    payload: dict[str, Any] = {"enabled": bool(enabled)}
    if config is not None:
        payload["config"] = dict(config)
    body = await _http.request_json(
        "PUT",
        f"/tools/adapters/{adapter}/enabled",
        json=payload,
    )
    return ToolAdapter.model_validate(body or {})


async def call_adapter(
    adapter: ToolAdapterID | str,
    tool: str,
    arguments: dict[str, Any] | None = None,
) -> ToolAdapterCallResult:
    """Call an enabled built-in adapter."""
    if not adapter:
        raise ValueError("adapter must be a non-empty string")
    if not tool:
        raise ValueError("tool must be a non-empty string")
    payload: dict[str, Any] = {"adapter": adapter, "tool": tool}
    if arguments is not None:
        payload["arguments"] = dict(arguments)
    body = await _http.request_json("POST", "/tools/call", json=payload)
    return ToolAdapterCallResult.model_validate(body or {})


# ─── Native tool verbs (#16) ──────────────────────────────────────────────


async def list_native() -> list[NativeToolStatus]:
    """Return the six native tools + per-tenant enablement.

    These are distinct from the legacy ``list_adapters`` surface. Native
    tools have a fixed canonical name set (``browser`` / ``search`` /
    ``fs`` / ``exec`` / ``http`` / ``sql``); the active backend per
    type is picked via env (e.g. ``AF_STACK_TOOL_BROWSER``). The verbs
    are accessible to agents under the MCP ``native:<tool>`` namespace.
    """
    body = await _http.request_json("GET", "/tools/native")
    payload = body or {"tools": []}
    rows = payload.get("tools") if isinstance(payload, dict) else None
    if not isinstance(rows, list):
        return []
    return [NativeToolStatus.model_validate(row) for row in rows]


async def set_native_enabled(
    tool: NativeToolName | str,
    enabled: bool,
    *,
    config: dict[str, Any] | None = None,
) -> NativeToolStatus:
    """Enable or disable one native tool for the caller tenant."""
    if not tool:
        raise ValueError("tool must be a non-empty string")
    payload: dict[str, Any] = {"enabled": bool(enabled)}
    if config is not None:
        payload["config"] = dict(config)
    body = await _http.request_json(
        "POST",
        f"/tools/native/{tool}/enable",
        json=payload,
    )
    return NativeToolStatus.model_validate(body or {})


async def invoke_native(
    tool: NativeToolName | str,
    verb: str,
    args: dict[str, Any] | None = None,
) -> InvokeNativeToolResult:
    """Invoke one verb of a native tool.

    This is the direct-call surface used by tests and non-agent
    callers. Agents call native tools through ``app.mcp.call(
    "native:<tool>", "<verb>", {...})`` instead — see the MCP bridge
    documentation for that path.
    """
    if not tool:
        raise ValueError("tool must be a non-empty string")
    if not verb:
        raise ValueError("verb must be a non-empty string")
    payload: dict[str, Any] = {"verb": verb}
    if args is not None:
        payload["args"] = dict(args)
    body = await _http.request_json(
        "POST",
        f"/tools/native/{tool}/invoke",
        json=payload,
    )
    return InvokeNativeToolResult.model_validate(body or {})


# ─── Backward-compat shim ─────────────────────────────────────────────────


class Tools:
    """Deprecated class-based namespace for MCP operations.

    Kept so code that still does ``from af_stack.tools import Tools`` and
    ``tools = Tools()`` keeps working. All methods forward to the
    module-level functions above. New code should use those directly,
    either as ``from af_stack import tools`` or via
    ``suite.tools.<verb>(...)``.

    .. deprecated:: 0.0.2
       The class form will be removed in a future major release.
    """

    def __init__(self) -> None:
        # Instances are stateless; the warning fires once per construction
        # so it appears next to user code rather than at import time.
        warnings.warn(
            "af_stack.tools.Tools is deprecated; use the module-level "
            "functions (`from af_stack import tools; await tools.call_mcp(...)`).",
            DeprecationWarning,
            stacklevel=2,
        )

    async def list_mcp_servers(self) -> list[MCPServer]:
        return await list_mcp_servers()

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
        return await add_mcp_server(
            name,
            transport,
            command=command,
            url=url,
            env=env,
            description=description,
            tenant_id=tenant_id,
        )

    async def remove_mcp_server(self, name: str) -> None:
        return await remove_mcp_server(name)

    async def enable_mcp_server(self, name: str, enabled: bool) -> MCPServer:
        return await enable_mcp_server(name, enabled)

    async def list_mcp_tools(self, server: str | None = None) -> list[MCPTool]:
        return await list_mcp_tools(server)

    async def call_mcp(
        self,
        server: str,
        tool: str,
        arguments: dict[str, Any] | None = None,
    ) -> MCPCallResult:
        return await call_mcp(server, tool, arguments)

    async def list_adapters(self) -> list[ToolAdapter]:
        return await list_adapters()

    async def set_adapter_enabled(
        self,
        adapter: ToolAdapterID | str,
        enabled: bool,
        *,
        config: dict[str, Any] | None = None,
    ) -> ToolAdapter:
        return await set_adapter_enabled(adapter, enabled, config=config)

    async def call_adapter(
        self,
        adapter: ToolAdapterID | str,
        tool: str,
        arguments: dict[str, Any] | None = None,
    ) -> ToolAdapterCallResult:
        return await call_adapter(adapter, tool, arguments)


__all__ = [
    "MCPCallResult",
    "MCPServer",
    "MCPServerStatus",
    "MCPTool",
    "MCPTransport",
    "BuiltinToolDefinition",
    "InvokeNativeToolResult",
    "NativeToolName",
    "NativeToolStatus",
    "NativeToolVerb",
    "Tools",
    "ToolAdapter",
    "ToolAdapterCallResult",
    "ToolAdapterID",
    "add_mcp_server",
    "call_adapter",
    "call_mcp",
    "enable_mcp_server",
    "invoke_native",
    "list_adapters",
    "list_mcp_servers",
    "list_mcp_tools",
    "list_native",
    "remove_mcp_server",
    "set_adapter_enabled",
    "set_native_enabled",
]
