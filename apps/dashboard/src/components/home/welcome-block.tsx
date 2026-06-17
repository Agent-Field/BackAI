// SPDX-License-Identifier: Apache-2.0

"use client"

import {
  AlertCircle,
  Check,
  Copy,
  Eye,
  EyeOff,
  Loader2,
  RefreshCw,
  X,
} from "lucide-react"
import { useEffect, useState } from "react"

import { Button } from "@/components/ui/button"

const DISMISS_KEY = "backai.welcome.dismissed"
const PLACEHOLDER_KEY = "af_xxxxxxxx_yyyyyyyyyyyyyyyyyyyyyyyy"
const DEFAULT_MODEL = "openrouter/qwen/qwen-2.5-72b-instruct"

// Minimum welcome block. Single click reveals a fresh API key issued
// against the default tenant via /api/v1/admin/keys; the copy buttons
// write to the clipboard. Errors are surfaced explicitly — silent
// failures hid the reason the prior version "did nothing on click".

const CURL_SNIPPET = (key: string, model: string) =>
  `curl -sS http://localhost:8080/api/v1/llm/chat/completions \\
  -H "Authorization: Bearer ${key}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${model}",
    "messages": [{"role":"user","content":"Hello"}],
    "max_tokens": 64
  }'`

export function WelcomeBlock() {
  const [dismissed, setDismissed] = useState(false)
  const [revealed, setRevealed] = useState(false)
  const [apiKey, setApiKey] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [copiedKey, setCopiedKey] = useState(false)
  const [copiedSnippet, setCopiedSnippet] = useState(false)

  useEffect(() => {
    try {
      if (localStorage.getItem(DISMISS_KEY) === "true") setDismissed(true)
    } catch {
      // ignore
    }
  }, [])

  if (dismissed) return null

  const issueKey = async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await fetch("/api/v1/admin/keys", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: "welcome-block",
          tenant_id: "00000000-0000-0000-0000-000000000000",
        }),
      })
      // 401 here means the dashboard middleware passed the request through
      // (it only checks the cookie EXISTS) but the runtime rejected the
      // session token — typically because the cookie is stale across a
      // dashboard restart or a manual session-table wipe. Direct the
      // operator to log in again rather than burying the cause.
      if (res.status === 401) {
        throw new Error(
          "SESSION_STALE: dashboard cookie no longer matches a session " +
            "in the runtime. Sign out and back in to refresh.",
        )
      }
      if (!res.ok) {
        const text = await res.text()
        throw new Error(`HTTP ${res.status}: ${text || res.statusText}`)
      }
      const body = (await res.json()) as { value?: string }
      if (!body.value) throw new Error("Response missing `value` field")
      setApiKey(body.value)
      setRevealed(true)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }

  const handleRevealClick = () => {
    if (apiKey) {
      setRevealed((r) => !r)
      return
    }
    void issueKey()
  }

  const copy = async (text: string, which: "key" | "snippet") => {
    try {
      await navigator.clipboard.writeText(text)
      if (which === "key") {
        setCopiedKey(true)
        setTimeout(() => setCopiedKey(false), 1500)
      } else {
        setCopiedSnippet(true)
        setTimeout(() => setCopiedSnippet(false), 1500)
      }
    } catch (e) {
      setError(
        e instanceof Error
          ? `Clipboard blocked: ${e.message}`
          : "Clipboard unavailable",
      )
    }
  }

  const handleDismiss = () => {
    try {
      localStorage.setItem(DISMISS_KEY, "true")
    } catch {
      // ignore
    }
    setDismissed(true)
  }

  const visibleKey = revealed && apiKey ? apiKey : PLACEHOLDER_KEY
  const snippet = CURL_SNIPPET(apiKey ?? "$BACKAI_API_KEY", DEFAULT_MODEL)

  return (
    <section
      aria-labelledby="welcome-heading"
      className="rounded-md border bg-card"
    >
      <header className="flex items-center justify-between border-b px-row-x py-row-y">
        <h2
          id="welcome-heading"
          className="text-body font-medium text-foreground"
        >
          Welcome to BackAI
        </h2>
        <Button
          variant="ghost"
          size="icon"
          aria-label="Dismiss welcome"
          className="size-7"
          onClick={handleDismiss}
        >
          <X className="size-3.5" aria-hidden />
        </Button>
      </header>

      {error ? (
        <div className="flex items-start gap-inline border-b bg-destructive/5 px-row-x py-row-y text-meta text-destructive">
          <AlertCircle className="size-3.5 shrink-0" aria-hidden />
          <span className="min-w-0 flex-1">{error}</span>
          {error.startsWith("SESSION_STALE") ? (
            <a
              href={`/api/auth/sign-out?redirect_to=${encodeURIComponent(
                "/login?next=/",
              )}`}
              className="text-meta underline hover:no-underline"
            >
              Sign out & back in
            </a>
          ) : (
            <button
              type="button"
              className="text-meta underline hover:no-underline"
              onClick={() => setError(null)}
            >
              dismiss
            </button>
          )}
        </div>
      ) : null}

      <div className="grid gap-stack px-row-x py-tile md:grid-cols-2">
        {/* Dev tenant key */}
        <div className="flex flex-col gap-tile-tight">
          <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
            Dev tenant key
          </span>
          <div className="flex items-center gap-inline rounded-md border bg-background px-row-x py-row-y font-mono text-meta">
            <span className="min-w-0 flex-1 truncate text-foreground">
              {visibleKey}
            </span>
            <Button
              variant="ghost"
              size="icon"
              className="size-7 shrink-0"
              aria-label={
                apiKey
                  ? revealed
                    ? "Hide key"
                    : "Show key"
                  : "Issue and reveal key"
              }
              onClick={handleRevealClick}
              disabled={loading}
              title={
                apiKey
                  ? revealed
                    ? "Hide"
                    : "Show"
                  : "Issue a new key"
              }
            >
              {loading ? (
                <Loader2 className="size-3.5 animate-spin" aria-hidden />
              ) : revealed ? (
                <EyeOff className="size-3.5" aria-hidden />
              ) : (
                <Eye className="size-3.5" aria-hidden />
              )}
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="size-7 shrink-0"
              aria-label="Copy key"
              onClick={() => apiKey && copy(apiKey, "key")}
              disabled={!apiKey}
              title={apiKey ? "Copy key" : "Reveal first"}
            >
              {copiedKey ? (
                <Check className="size-3.5 text-success" aria-hidden />
              ) : (
                <Copy className="size-3.5" aria-hidden />
              )}
            </Button>
            {apiKey ? (
              <Button
                variant="ghost"
                size="icon"
                className="size-7 shrink-0"
                aria-label="Issue new key"
                onClick={() => void issueKey()}
                disabled={loading}
                title="Issue a fresh key (replaces this one in the UI)"
              >
                <RefreshCw className="size-3.5" aria-hidden />
              </Button>
            ) : null}
          </div>
          <span className="text-meta text-muted-foreground">
            One key per click. Issued against the default tenant; rotate
            in <code className="font-mono">/people/keys</code> when ready.
          </span>
        </div>

        {/* Try-it curl snippet */}
        <div className="flex min-w-0 flex-col gap-tile-tight">
          <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
            Try it
          </span>
          <pre className="overflow-x-auto rounded-md border bg-background px-row-x py-row-y font-mono text-meta text-foreground">
            {snippet}
          </pre>
          <div className="flex items-center justify-between gap-inline">
            <span className="min-w-0 truncate text-meta text-muted-foreground">
              {apiKey
                ? "Snippet uses the revealed key."
                : "Reveal the key to bake it into the snippet."}
            </span>
            <Button
              variant="outline"
              size="sm"
              className="h-7 shrink-0 gap-inline text-meta"
              onClick={() => copy(snippet, "snippet")}
            >
              {copiedSnippet ? (
                <Check className="size-3.5 text-success" aria-hidden />
              ) : (
                <Copy className="size-3.5" aria-hidden />
              )}
              {copiedSnippet ? "Copied" : "Copy snippet"}
            </Button>
          </div>
        </div>
      </div>
    </section>
  )
}
