// SPDX-License-Identifier: Apache-2.0

"use client"

import { useState } from "react"
import Link from "next/link"
import { Bot, ExternalLink, GitBranch, Send, Sparkles } from "lucide-react"
import ReactMarkdown from "react-markdown"
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter"
import { vscDarkPlus } from "react-syntax-highlighter/dist/esm/styles/prism"

import { Button } from "@/components/ui/button"
import { GuidedTour } from "@/components/guided-tour"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Textarea } from "@/components/ui/textarea"
import { Badge } from "@/components/ui/badge"

const MODEL = "qwen/qwen-2.5-72b-instruct"
const SYSTEM_PROMPT =
  "You are SupportDesk AI, a concise customer support assistant. Draft helpful, accurate replies for support teams. Keep the tone clear, calm, and practical. Follow the support decision plan when provided. If the customer asks for something policy-specific, mention what the support team should verify before sending."
const ONBOARDING_KEY_STORAGE = "backai:onboarding-api-key"

type Props = {
  tenantId: string
  apiKeyPrefix: string | null
}

type Usage = {
  prompt_tokens?: number
  completion_tokens?: number
  total_tokens?: number
  cost_usd?: number
  response_cost?: number
}

type AgentPlan = {
  agent?: string
  entry_reasoner?: string
  reasoners?: string[]
  dynamic_branch?: string
  confidence?: string
  issue?: {
    category?: string
    urgency?: string
    needs_human_review?: boolean
    summary?: string
  }
  guardrail?: {
    reasoner?: string
    must_verify?: string[]
    do_not_promise?: string[]
    allowed_commitment?: string
    handoff?: boolean
  }
  brief?: {
    tone?: string
    next_steps?: string[]
    constraints?: {
      must_verify?: string[]
      do_not_promise?: string[]
      allowed_commitment?: string
    }
  }
}

function operatorDashboardUrl(tenantId: string, requestId?: string): string {
  // Best-effort: the operator runs the admin dashboard on :33000 (env
  // AF_STACK_DASHBOARD_PORT). NEXT_PUBLIC_OPERATOR_URL overrides this.
  const base =
    process.env.NEXT_PUBLIC_OPERATOR_URL ??
    (typeof window !== "undefined"
      ? window.location.origin.replace(/:34000$/, ":33000").replace(/:34001$/, ":33000")
      : "http://localhost:33000")
  const qs = new URLSearchParams({ tenant: tenantId })
  if (requestId) qs.set("request_id", requestId)
  return `${base}/operate/cost?${qs.toString()}`
}

function agentFieldBaseUrl(): string {
  return (
    process.env.NEXT_PUBLIC_RUNTIME_UI_URL ??
    (typeof window !== "undefined"
      ? window.location.origin.replace(/:34000$/, ":8081").replace(/:34001$/, ":8081")
      : "http://localhost:8081")
  )
}

function agentFieldCatalogUrl(agentId = "supportdesk", reasonerId?: string): string {
  const url = new URL("/api/v1/discovery/capabilities", agentFieldBaseUrl())
  url.searchParams.set("agent_id", agentId)
  if (reasonerId) url.searchParams.set("reasoner", reasonerId)
  return url.toString()
}

function agentFieldExecutionUrl(executionId: string): string {
  return `${agentFieldBaseUrl().replace(/\/$/, "")}/agent-api/executions/${encodeURIComponent(
    executionId,
  )}/details`
}

function createRequestId(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID()
  }
  return `supportdesk-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

function onboardingAPIKey(): string | null {
  try {
    return window.sessionStorage.getItem(ONBOARDING_KEY_STORAGE)
  } catch {
    return null
  }
}

async function ensureOnboardingAPIKey(): Promise<string | null> {
  const existing = onboardingAPIKey()
  if (existing) return existing

  const res = await fetch("/api/customer/onboarding-key", {
    method: "POST",
    credentials: "include",
  })
  if (!res.ok) return null

  const data = (await res.json()) as { api_key_token?: string }
  if (!data.api_key_token) return null
  try {
    window.sessionStorage.setItem(ONBOARDING_KEY_STORAGE, data.api_key_token)
  } catch {
    // The request can still use the freshly-minted key for this turn.
  }
  return data.api_key_token
}

export function CodeHelperClient({ tenantId, apiKeyPrefix }: Props) {
  const [question, setQuestion] = useState("")
  const [answer, setAnswer] = useState("")
  const [usage, setUsage] = useState<Usage | null>(null)
  const [agentPlan, setAgentPlan] = useState<AgentPlan | null>(null)
  const [agentExecutionId, setAgentExecutionId] = useState<string | null>(null)
  const [lastRequestId, setLastRequestId] = useState<string | null>(null)
  const [streaming, setStreaming] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const tourSteps = [
    {
      element: "[data-tour='supportdesk-heading']",
      popover: {
        title: "Run a real product workflow",
        description:
          "This page feels like a small customer app, but it is exercising the full backend: policy planning, model routing, tenant metering, and admin evidence.",
        side: "bottom" as const,
        align: "start" as const,
      },
    },
    {
      element: "[data-tour='supportdesk-composer']",
      popover: {
        title: "Run the SupportDesk action",
        description:
          "Enter a customer issue and draft a reply. Billing, refund, access, and technical issues each produce a different decision path.",
        side: "bottom" as const,
        align: "start" as const,
      },
    },
    ...(agentPlan
      ? [
          {
            element: "[data-tour='decision-plan']",
            popover: {
              title: "Inspect the decision path",
              description:
                "This panel shows the category, branch, checks, and guardrail that shaped the final response before the model call.",
              side: "top" as const,
              align: "start" as const,
            },
          },
        ]
      : []),
    ...(answer
      ? [
          {
            element: "[data-tour='admin-evidence-link']",
            popover: {
              title: "Open the admin evidence",
              description:
                "This link carries the exact request into the admin dashboard, where operators see tenant, model, tokens, cost, and related backend records.",
              side: "top" as const,
              align: "start" as const,
            },
          },
        ]
      : []),
  ]

  const handleAsk = async () => {
    if (!question.trim()) return
    setStreaming(true)
    setError(null)
    setAnswer("")
    setUsage(null)
    setAgentPlan(null)
    setAgentExecutionId(null)
    const requestId = createRequestId()
    setLastRequestId(requestId)

    try {
      // Same-origin proxy forwards our session cookie. The runtime
      // resolves tenant by API key when the just-issued onboarding key is
      // still available, and falls back to session auth for returning users.
      const headers = new Headers({
        "content-type": "application/json",
        "x-request-id": requestId,
      })
      const apiKey = await ensureOnboardingAPIKey()
      if (apiKey) {
        headers.set("authorization", `Bearer ${apiKey}`)
      }
      const plan = await fetchAgentPlan(headers, question, tenantId)
      setAgentPlan(plan.plan)
      setAgentExecutionId(plan.executionId)

      const res = await fetch("/api/v1/llm/chat/completions", {
        method: "POST",
        headers,
        credentials: "include",
        body: JSON.stringify({
          model: MODEL,
          max_tokens: 400,
          stream: true,
          messages: [
            { role: "system", content: SYSTEM_PROMPT },
            {
              role: "user",
              content: [
                `Customer ticket:\n${question}`,
                "",
                "Support decision plan:",
                JSON.stringify(plan.plan, null, 2),
              ].join("\n"),
            },
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
              json.choices?.[0]?.delta?.content ?? json.choices?.[0]?.message?.content
            if (typeof delta === "string" && delta.length > 0) {
              fullText += delta
              setAnswer(fullText)
            }
            if (json.usage) {
              setUsage({
                prompt_tokens: json.usage.prompt_tokens,
                completion_tokens: json.usage.completion_tokens,
                total_tokens: json.usage.total_tokens,
                cost_usd: json.usage.cost_usd ?? json.usage.response_cost,
                response_cost: json.usage.response_cost,
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
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div data-tour="supportdesk-heading">
          <h1 className="text-2xl font-semibold tracking-tight">Support Desk</h1>
          <p className="text-muted-foreground text-sm">
            Draft customer replies through the BackAI backend. Cost is billed to your tenant and
            visible in admin. Model: <code className="font-mono">{MODEL}</code>
            {apiKeyPrefix ? (
              <>
                {" · key "}
                <code className="font-mono">af_{apiKeyPrefix}_…</code>
              </>
            ) : null}
          </p>
        </div>
        <GuidedTour id="customer-supportdesk-v1" autoStart steps={tourSteps} />
      </div>

      <Card data-tour="supportdesk-composer">
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Sparkles className="size-4" />
            Draft a support reply
          </CardTitle>
          <CardDescription>
            Example: &quot;A customer says their invoice is wrong and wants a refund.&quot;
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <Textarea
            placeholder="A customer says their invoice is wrong and wants a refund."
            value={question}
            onChange={(e) => setQuestion(e.target.value)}
            rows={4}
            disabled={streaming}
          />
          <div className="flex items-center gap-2">
            <Button onClick={handleAsk} disabled={streaming || !question.trim()}>
              <Send data-icon="inline-start" />
              {streaming ? "Drafting..." : "Draft reply"}
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
            <CardTitle>Draft reply</CardTitle>
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
                      <code className="rounded bg-muted px-1 py-0.5 font-mono text-xs" {...props}>
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

      {agentPlan ? (
        <Card data-tour="decision-plan">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <GitBranch className="size-4" />
              Decision plan
            </CardTitle>
            <CardDescription>
              SupportDesk first classifies the issue, extracts facts, chooses a policy branch, and
              then sends a brief to the BackAI gateway.
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-4 md:grid-cols-[1fr_1.2fr]">
            <div className="space-y-2">
              <div className="flex flex-wrap gap-2">
                <Badge variant="secondary" className="gap-1.5">
                  <Bot className="size-3" />
                  {agentPlan.agent ?? "supportdesk"}
                </Badge>
                <Badge variant="outline">{agentPlan.dynamic_branch ?? "general"} branch</Badge>
                <Badge variant="outline">{agentPlan.confidence ?? "medium"} confidence</Badge>
              </div>
              {agentExecutionId ? (
                <p className="text-muted-foreground text-xs">
                  Execution trace{" "}
                  <Link
                    href={agentFieldExecutionUrl(agentExecutionId)}
                    target="_blank"
                    rel="noreferrer"
                    className="font-mono underline-offset-4 hover:underline"
                  >
                    {agentExecutionId}
                  </Link>
                </p>
              ) : null}
              <p className="text-muted-foreground text-sm">
                Category:{" "}
                <span className="text-foreground">{agentPlan.issue?.category ?? "general"}</span>
                {" · "}Urgency:{" "}
                <span className="text-foreground">{agentPlan.issue?.urgency ?? "normal"}</span>
              </p>
            </div>
            <div className="space-y-3">
              <div>
                <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                  Reasoner path
                </div>
                <div className="mt-2 flex flex-wrap gap-1.5">
                  {(agentPlan.reasoners ?? []).map((reasoner) => (
                    <Badge
                      key={reasoner}
                      variant="outline"
                      render={
                        <Link
                          href={agentFieldCatalogUrl(agentPlan.agent ?? "supportdesk", reasoner)}
                          target="_blank"
                          rel="noreferrer"
                          title={`Open ${agentPlan.agent ?? "supportdesk"}.${reasoner} in the runtime catalog`}
                        >
                          {reasoner}
                        </Link>
                      }
                    />
                  ))}
                </div>
              </div>
              <div>
                <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                  Guardrail
                </div>
                <p className="mt-1 text-sm">
                  {agentPlan.guardrail?.allowed_commitment ??
                    "Share next steps and request missing details."}
                </p>
              </div>
            </div>
          </CardContent>
        </Card>
      ) : null}

      {(usage || answer) && (
        <div className="text-muted-foreground text-xs" data-tour="admin-evidence-link">
          <Link
            href={operatorDashboardUrl(tenantId, lastRequestId ?? undefined)}
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

async function fetchAgentPlan(
  headers: Headers,
  ticket: string,
  tenantId: string,
): Promise<{ plan: AgentPlan; executionId: string | null }> {
  const res = await fetch("/api/v1/agents/supportdesk.reply_plan", {
    method: "POST",
    headers,
    credentials: "include",
    body: JSON.stringify({
      input: {
        ticket,
        tenant_id: tenantId,
      },
    }),
  })

  if (!res.ok) {
    let detail = `HTTP ${res.status}`
    try {
      const text = await res.text()
      detail += ` — ${text.slice(0, 200)}`
    } catch {
      // ignore
    }
    throw new Error(`Support decision plan failed: ${detail}`)
  }

  const body = await res.json()
  const executionId = res.headers.get("x-execution-id") ?? body.execution_id ?? null
  const plan = (body.output ?? body.result ?? body) as AgentPlan
  return { plan, executionId }
}
