// SPDX-License-Identifier: Apache-2.0

"use client"

import { Check, Copy, Eye, EyeOff, X } from "lucide-react"
import { useEffect, useState } from "react"

import { Button } from "@/components/ui/button"

const DISMISS_KEY = "backai.welcome.dismissed"

// Minimum welcome block per the critique:
//   - Dev tenant key (issued by /api/v1/admin/keys when the operator
//     clicks "Reveal" — or read from /tmp by `./demo-traffic.sh setup`).
//   - Try-it curl snippet that uses the revealed key.
//   - Dismissible (localStorage) — restorable via Platform → Settings later.
//
// Lang-toggle (Python/TS) is deferred. Curl-only for now.

const CURL_SNIPPET = (key: string, model: string) =>
  `curl -sS http://localhost:8080/api/v1/llm/chat/completions \\
  -H "Authorization: Bearer ${key}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${model}",
    "messages": [{"role":"user","content":"Hello"}],
    "max_tokens": 64
  }'`

const PLACEHOLDER_KEY = "af_xxxxxxxx_yyyyyyyyyyyyyyyyyyyyyyyy"
const DEFAULT_MODEL = "openrouter/qwen/qwen-2.5-72b-instruct"

export function WelcomeBlock() {
  const [dismissed, setDismissed] = useState(false)
  const [revealed, setRevealed] = useState(false)
  const [copied, setCopied] = useState(false)
  const [apiKey, setApiKey] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    try {
      if (localStorage.getItem(DISMISS_KEY) === "true") {
        setDismissed(true)
      }
    } catch {
      // ignore — non-fatal
    }
  }, [])

  if (dismissed) return null

  const handleReveal = async () => {
    if (apiKey) {
      setRevealed((r) => !r)
      return
    }
    setLoading(true)
    try {
      const res = await fetch("/api/v1/admin/keys", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          name: "welcome-block",
          tenant_id: "00000000-0000-0000-0000-000000000000",
        }),
      })
      if (!res.ok) throw new Error(`http ${res.status}`)
      const body = await res.json()
      setApiKey(body.value ?? body.secret ?? body.key ?? null)
      setRevealed(true)
    } catch {
      // Leave key null — UI shows the placeholder.
    } finally {
      setLoading(false)
    }
  }

  const handleCopy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // ignore — secure-context restriction
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
        <h2 id="welcome-heading" className="text-body font-medium text-foreground">
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
      <div className="grid gap-stack px-row-x py-tile md:grid-cols-2">
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
              className="size-7"
              aria-label={revealed ? "Hide key" : "Reveal key"}
              onClick={handleReveal}
              disabled={loading}
            >
              {revealed ? (
                <EyeOff className="size-3.5" aria-hidden />
              ) : (
                <Eye className="size-3.5" aria-hidden />
              )}
            </Button>
            {apiKey ? (
              <Button
                variant="ghost"
                size="icon"
                className="size-7"
                aria-label="Copy key"
                onClick={() => handleCopy(apiKey)}
              >
                {copied ? (
                  <Check className="size-3.5 text-success" aria-hidden />
                ) : (
                  <Copy className="size-3.5" aria-hidden />
                )}
              </Button>
            ) : null}
          </div>
          <span className="text-meta text-muted-foreground">
            One key per click. Issued against the default tenant; rotate
            in <code className="font-mono">/people/keys</code> when ready.
          </span>
        </div>
        <div className="flex flex-col gap-tile-tight">
          <span className="text-eyebrow uppercase tracking-wide text-muted-foreground">
            Try it
          </span>
          <pre className="overflow-x-auto rounded-md border bg-background px-row-x py-row-y font-mono text-meta text-foreground">
            {snippet}
          </pre>
          <div className="flex items-center justify-between">
            <span className="text-meta text-muted-foreground">
              Replace <code>{"$BACKAI_API_KEY"}</code> with the revealed
              key.
            </span>
            <Button
              variant="ghost"
              size="sm"
              className="h-7 gap-inline text-meta"
              onClick={() => handleCopy(snippet)}
            >
              {copied ? (
                <Check className="size-3.5 text-success" aria-hidden />
              ) : (
                <Copy className="size-3.5" aria-hidden />
              )}
              Copy
            </Button>
          </div>
        </div>
      </div>
    </section>
  )
}
