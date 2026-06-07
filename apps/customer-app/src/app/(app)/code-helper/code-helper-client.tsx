// SPDX-License-Identifier: Apache-2.0

"use client"

import { useState } from "react"
import Link from "next/link"
import { ExternalLink, Send, Sparkles } from "lucide-react"
import ReactMarkdown from "react-markdown"
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter"
import { vscDarkPlus } from "react-syntax-highlighter/dist/esm/styles/prism"

import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Textarea } from "@/components/ui/textarea"
import { Badge } from "@/components/ui/badge"

const MODEL = "qwen/qwen-2.5-72b-instruct"
const SYSTEM_PROMPT =
  "You are SWE-AF, an expert software engineering assistant. Answer the user's code question with a short explanation and a complete, runnable code snippet. Use markdown code blocks."

type Props = {
  tenantId: string
  apiKeyPrefix: string | null
}

type Usage = {
  prompt_tokens?: number
  completion_tokens?: number
  total_tokens?: number
  cost_usd?: number
}

function operatorDashboardUrl(tenantId: string): string {
  // Best-effort: the operator runs the admin dashboard on :33000 (env
  // AF_STACK_DASHBOARD_PORT). NEXT_PUBLIC_OPERATOR_URL overrides this.
  const base =
    process.env.NEXT_PUBLIC_OPERATOR_URL ??
    (typeof window !== "undefined"
      ? window.location.origin
          .replace(/:34000$/, ":33000")
          .replace(/:34001$/, ":33000")
      : "http://localhost:33000")
  return `${base}/operate/cost?tenant=${encodeURIComponent(tenantId)}`
}

export function CodeHelperClient({ tenantId, apiKeyPrefix }: Props) {
  const [question, setQuestion] = useState("")
  const [answer, setAnswer] = useState("")
  const [usage, setUsage] = useState<Usage | null>(null)
  const [streaming, setStreaming] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleAsk = async () => {
    if (!question.trim()) return
    setStreaming(true)
    setError(null)
    setAnswer("")
    setUsage(null)

    try {
      // Same-origin proxy forwards our session cookie. The runtime
      // resolves tenant by session; cost events bill to this tenant.
      const res = await fetch("/api/v1/llm/chat/completions", {
        method: "POST",
        headers: { "content-type": "application/json" },
        credentials: "include",
        body: JSON.stringify({
          model: MODEL,
          max_tokens: 400,
          stream: true,
          messages: [
            { role: "system", content: SYSTEM_PROMPT },
            { role: "user", content: question },
          ],
        }),
      })

      if (!res.ok || !res.body) {
        let detail = `HTTP ${res.status}`
        try {
          const text = await res.text()
          detail += ` — ${text.slice(0, 200)}`
        } catch {
          // ignore
        }
        setError(detail)
        return
      }

      // OpenAI-style server-sent events: lines of "data: {...}\n\n",
      // terminated by "data: [DONE]\n\n". Last data line carries usage
      // when the gateway is configured to emit it.
      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ""
      let fullText = ""
      while (true) {
        const { value, done } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const lines = buffer.split("\n")
        buffer = lines.pop() ?? ""
        for (const raw of lines) {
          const line = raw.trim()
          if (!line.startsWith("data:")) continue
          const data = line.slice("data:".length).trim()
          if (!data || data === "[DONE]") continue
          try {
            const json = JSON.parse(data)
            const delta: string | undefined =
              json.choices?.[0]?.delta?.content ??
              json.choices?.[0]?.message?.content
            if (typeof delta === "string" && delta.length > 0) {
              fullText += delta
              setAnswer(fullText)
            }
            if (json.usage) {
              setUsage({
                prompt_tokens: json.usage.prompt_tokens,
                completion_tokens: json.usage.completion_tokens,
                total_tokens: json.usage.total_tokens,
                cost_usd: json.usage.cost_usd,
              })
            }
          } catch {
            // partial chunk; skip
          }
        }
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setStreaming(false)
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Code Helper</h1>
        <p className="text-muted-foreground text-sm">
          Live demo. Your question goes through the AF Stack LLM gateway. Cost is
          billed to your tenant. Model: <code className="font-mono">{MODEL}</code>
          {apiKeyPrefix ? (
            <>
              {" · key "}
              <code className="font-mono">af_{apiKeyPrefix}_…</code>
            </>
          ) : null}
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Sparkles className="size-4" />
            Ask a code question
          </CardTitle>
          <CardDescription>
            Example: &quot;how do I parse JSON in Go?&quot;
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <Textarea
            placeholder="how do I parse JSON in Go?"
            value={question}
            onChange={(e) => setQuestion(e.target.value)}
            rows={4}
            disabled={streaming}
          />
          <div className="flex items-center gap-2">
            <Button onClick={handleAsk} disabled={streaming || !question.trim()}>
              <Send data-icon="inline-start" />
              {streaming ? "Asking..." : "Ask"}
            </Button>
            {usage?.cost_usd !== undefined ? (
              <Badge variant="outline" className="font-mono text-xs">
                ${usage.cost_usd.toFixed(6)} this call
              </Badge>
            ) : null}
            {usage?.total_tokens !== undefined ? (
              <Badge variant="outline" className="font-mono text-xs">
                {usage.total_tokens} tok
              </Badge>
            ) : null}
          </div>
          {error ? (
            <div className="rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">
              {error}
            </div>
          ) : null}
        </CardContent>
      </Card>

      {answer ? (
        <Card>
          <CardHeader>
            <CardTitle>Answer</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="prose prose-sm dark:prose-invert max-w-none">
              <ReactMarkdown
                components={{
                  code({ className, children, ...props }) {
                    const match = /language-(\w+)/.exec(className ?? "")
                    const lang = match ? match[1] : ""
                    const text = String(children).replace(/\n$/, "")
                    const isBlock = text.includes("\n") || lang.length > 0
                    if (isBlock) {
                      return (
                        <SyntaxHighlighter
                          language={lang || "text"}
                          style={vscDarkPlus}
                          customStyle={{
                            borderRadius: "var(--radius-md)",
                            fontSize: "0.8rem",
                          }}
                          PreTag="div"
                        >
                          {text}
                        </SyntaxHighlighter>
                      )
                    }
                    return (
                      <code
                        className="rounded bg-muted px-1 py-0.5 font-mono text-xs"
                        {...props}
                      >
                        {children}
                      </code>
                    )
                  },
                }}
              >
                {answer}
              </ReactMarkdown>
            </div>
          </CardContent>
        </Card>
      ) : null}

      {(usage || answer) && (
        <div className="text-muted-foreground text-xs">
          <Link
            href={operatorDashboardUrl(tenantId)}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-1 underline-offset-4 hover:underline"
          >
            View this call in the admin dashboard
            <ExternalLink className="size-3" />
          </Link>
        </div>
      )}
    </div>
  )
}
