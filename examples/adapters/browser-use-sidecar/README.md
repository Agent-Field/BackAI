# Browser sidecar — Playwright reference implementation

A minimal HTTP sidecar that gives the runtime's native `browser` tool a
real headless Chromium. It speaks the exact contract the `browser-use`
adapter expects (see `services/runtime/internal/tools/adapters/browser/browseruse`):

| Endpoint | Body | Returns |
| --- | --- | --- |
| `POST /navigate` | `{url, session_id}` | `{title, url, status_code}` |
| `POST /extract-text` | `{session_id}` | `{text}` |
| `POST /screenshot` | `{session_id}` | `{screenshot_base64}` (PNG) |
| `POST /click` | `{selector, session_id}` | `{}` |
| `POST /fill` | `{selector, value, session_id}` | `{}` |

Each `session_id` maps to an isolated browser context; the empty string is
the shared default session.

## Run inside docker-compose (recommended)

The stack ships this sidecar behind the opt-in `browser` compose profile
(the image bundles Chromium, so it is not part of the default boot):

```bash
# .env
AF_STACK_TOOL_BROWSER=browser-use
BROWSER_USE_URL=http://browser-sidecar:8000
AF_STACK_BROWSER_ALLOW_PRIVATE=true   # sidecar lives on the compose network

docker compose --profile browser up -d
```

`AF_STACK_BROWSER_ALLOW_PRIVATE` is required because the runtime's SSRF
guard blocks loopback/RFC-1918 destinations by default — a compose
service name resolves to a private address. Leave it unset when the
sidecar runs on a public host.

## Run standalone

```bash
docker build -t backai-browser-sidecar .
docker run --rm -p 8000:8000 backai-browser-sidecar
curl -s -X POST localhost:8000/navigate \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com"}'
```

## Enable the tool for a tenant

The native tool is hidden until it is both configured (env above) and
enabled: `POST /api/v1/tools/native/browser/enable`. After that, agents
reach it via `app.mcp.call("native:browser", "navigate", {...})` and the
SDKs via `suite.tools.invoke_native("browser", ...)`.
