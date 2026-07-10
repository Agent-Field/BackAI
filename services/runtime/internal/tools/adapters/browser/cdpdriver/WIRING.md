# Wiring the CDP browser adapters into the factory

This branch deliberately does **not** touch
`services/runtime/internal/tools/factory.go` (concurrent work in the main
tree owns it). Until the factory is updated, `internal/tools` does not
compile on this branch: the `steel` and `playwright` constructors gained
parameters. This file is the exact wiring the factory session needs.

## Constructors

```go
// playwright — a CDP/Playwright websocket endpoint used directly.
// Browserless cloud: wss://chrome.browserless.io?token=KEY
// Self-hosted browserless: ws://host:3000 (needs allowPrivate)
playwright.New(endpoint string, allowPrivate bool) *playwright.Adapter
// Configured() == (endpoint != "")

// steel — Steel.dev hosted browsers (or self-hosted steel-browser).
// baseURL "" defaults to https://api.steel.dev.
steel.New(apiKey, baseURL string, allowPrivate bool) *steel.Adapter
// Configured() == (apiKey != "")

// browserbase — Browserbase hosted browsers. NEW adapter package:
// .../adapters/browser/browserbase
browserbase.New(apiKey, projectID string, allowPrivate bool) *browserbase.Adapter
// Configured() == (apiKey != "" && projectID != "")
```

All three satisfy `browser.Adapter` and additionally expose `Close()`,
which tears down live CDP sessions and (for Steel) releases the
provider-side sessions — Steel bills per session-minute. If the factory
grows a shutdown path, call `Close()` there; otherwise the providers'
own idle timeouts are the backstop.

## Env vars

| Adapter       | `AF_STACK_TOOL_BROWSER` | Env                                                        |
| ------------- | ----------------------- | ---------------------------------------------------------- |
| playwright    | `playwright`            | `PLAYWRIGHT_ENDPOINT` (ws/wss URL)                         |
| steel         | `steel`                 | `STEEL_API_KEY`, optional `STEEL_BASE_URL`                 |
| browserbase   | `browserbase`           | `BROWSERBASE_API_KEY`, `BROWSERBASE_PROJECT_ID`            |

`allowPrivate` for every adapter comes from the existing
`browserAllowPrivate()` helper (`AF_STACK_BROWSER_ALLOW_PRIVATE`), same
as browseruse. It gates both loopback/RFC-1918 CDP endpoints (checked in
`cdpdriver.CheckEndpoint`, mirroring safehttp's blocklist) and, for
steel, private REST base URLs.

## Suggested factory cases

```go
case "steel":
    key := strings.TrimSpace(os.Getenv("STEEL_API_KEY"))
    ad := steel.New(key, os.Getenv("STEEL_BASE_URL"), browserAllowPrivate())
    if key == "" {
        return ad, fmt.Errorf("tools: AF_STACK_TOOL_BROWSER=steel but STEEL_API_KEY is empty")
    }
    return ad, nil
case "playwright":
    endpoint := strings.TrimSpace(os.Getenv("PLAYWRIGHT_ENDPOINT"))
    ad := playwright.New(endpoint, browserAllowPrivate())
    if endpoint == "" {
        return ad, fmt.Errorf("tools: AF_STACK_TOOL_BROWSER=playwright but PLAYWRIGHT_ENDPOINT is empty")
    }
    return ad, nil
case "browserbase":
    key := strings.TrimSpace(os.Getenv("BROWSERBASE_API_KEY"))
    project := strings.TrimSpace(os.Getenv("BROWSERBASE_PROJECT_ID"))
    ad := browserbase.New(key, project, browserAllowPrivate())
    if key == "" || project == "" {
        return ad, fmt.Errorf("tools: AF_STACK_TOOL_BROWSER=browserbase but BROWSERBASE_API_KEY/BROWSERBASE_PROJECT_ID is empty")
    }
    return ad, nil
```

## Behavior notes

- Sessions: one live chromedp context per caller `session_id` (empty
  string = default session), reused across verbs, reaped after 5 idle
  minutes. Timeouts: 30s navigate, 10s other verbs.
- `navigate` returns the main-document HTTP status when CDP surfaces it
  (`chromedp.RunResponse`); `data:` URLs and cached pages report 0.
- The driver dials with `chromedp.NoModifyURL` — required for hosted
  providers whose connect URLs carry auth in the query string.
