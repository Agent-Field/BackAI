// SPDX-License-Identifier: Apache-2.0

"use client"

import { useMemo, useState } from "react"
import {
  CheckCircle2,
  Circle,
  Loader2,
  MessageSquareText,
  Route,
  Send,
  Sparkles,
  UserRound,
} from "lucide-react"
import ReactMarkdown from "react-markdown"
import { Prism as SyntaxHighlighter } from "react-syntax-highlighter"
import { vscDarkPlus } from "react-syntax-highlighter/dist/esm/styles/prism"

import { GuidedTour } from "@/components/guided-tour"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Separator } from "@/components/ui/separator"
import { Textarea } from "@/components/ui/textarea"

const MODEL = process.env.NEXT_PUBLIC_DEFAULT_MODEL ?? "qwen/qwen-2.5-72b-instruct"
const SYSTEM_PROMPT =
  "You are SupportDesk AI, a concise customer support assistant speaking directly to the customer. Help the customer understand the next step, keep the tone clear and calm, and follow the support decision plan when provided. If the customer asks for something policy-specific, explain what the support team may need to verify before taking action."
const ONBOARDING_KEY_STORAGE = "backai:onboarding-api-key"

const SUGGESTED_PROMPTS = [
  {
    title: "Refund request",
    intent: "Billing policy",
    prompt:
      "I think my invoice is wrong and I may have been double charged. Can you help me understand what happens next?",
  },
  {
    title: "Login blocked",
    intent: "Access help",
    prompt:
      "I changed phones and now I cannot sign in. What should I do to recover my account safely?",
  },
  {
    title: "Angry renewal",
    intent: "Retention",
    prompt:
      "My plan renewed automatically and I am frustrated. Can you explain my options before I cancel?",
  },
  {
    title: "Technical issue",
    intent: "Troubleshooting",
    prompt:
      "My exports fail when the CSV is large. What information should I send so support can fix it?",
  },
]

type Props = {
  tenantId: string
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

type ChatStatus = "planning" | "drafting" | "complete" | "error"

type TraceStepStatus = "complete" | "active" | "pending" | "error"

type TraceStep = {
  id: string
  title: string
  detail: string
  status: TraceStepStatus
}

type ChatMessage = {
  id: string
  role: "user" | "assistant"
  content: string
  status?: ChatStatus
  plan?: AgentPlan
  usage?: Usage
  requestId?: string
  error?: string
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

function formatLabel(value: string): string {
  return value
    .replace(/^support_/, "")
    .replace(/_/g, " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase())
}

function planChips(plan?: AgentPlan): string[] {
  if (!plan) return []
  const chips = [
    plan.issue?.category ? formatLabel(plan.issue.category) : null,
    plan.dynamic_branch ? `${formatLabel(plan.dynamic_branch)} route` : null,
    plan.issue?.urgency ? `${formatLabel(plan.issue.urgency)} urgency` : null,
    plan.guardrail?.handoff || plan.issue?.needs_human_review ? "Human review" : null,
    plan.confidence ? `${formatLabel(plan.confidence)} confidence` : null,
  ]
  return chips.filter(Boolean) as string[]
}

const STEP_COPY: Record<string, { title: string; detail: string }> = {
  reply_plan: {
    title: "Choose the response route",
    detail: "Decide which checks are needed before answering.",
  },
  classify_issue: {
    title: "Classify the customer issue",
    detail: "Identify the request type, urgency, and review needs.",
  },
  extract_customer_facts: {
    title: "Extract key facts",
    detail: "Pull out account, invoice, access, and evidence signals.",
  },
  billing_policy_review: {
    title: "Review billing policy",
    detail: "Check what support can say about invoices, renewals, and refunds.",
  },
  support_policy_review: {
    title: "Review support policy",
    detail: "Check the right support path and escalation boundary.",
  },
  refund_guardrail: {
    title: "Check refund guardrails",
    detail: "Avoid promising a refund before evidence is verified.",
  },
  billing_evidence_check: {
    title: "Check billing evidence",
    detail: "Look for the proof needed before changing account state.",
  },
  resolution_guardrail: {
    title: "Check resolution limits",
    detail: "Keep the answer helpful without over-committing.",
  },
  response_risk_check: {
    title: "Scan for risky promises",
    detail: "Catch claims that should stay conditional or need handoff.",
  },
  compose_reply_brief: {
    title: "Prepare answer guidance",
    detail: "Turn the checks into guidance for the final answer.",
  },
}

function uniqueReasoners(reasoners: string[] | undefined): string[] {
  const seen = new Set<string>()
  return (reasoners ?? []).filter((reasoner) => {
    if (seen.has(reasoner)) return false
    seen.add(reasoner)
    return true
  })
}

function stepCopy(reasoner: string): { title: string; detail: string } {
  return (
    STEP_COPY[reasoner] ?? {
      title: formatLabel(reasoner),
      detail: "Run the next check before answering the customer.",
    }
  )
}

function workflowTrace(plan: AgentPlan | undefined, status: ChatStatus | undefined): TraceStep[] {
  if (!plan) {
    return [
      {
        id: "read_request",
        title: "Read the customer request",
        detail: "Understand the situation before picking a path.",
        status: status === "error" ? "error" : "active",
      },
      {
        id: "choose_route",
        title: "Choose the response route",
        detail: "Decide which checks the answer needs.",
        status: "pending",
      },
      {
        id: "compose_reply",
        title: "Prepare the answer",
        detail: "Write a clear answer after the checks finish.",
        status: "pending",
      },
    ]
  }

  const reasoners = uniqueReasoners(plan.reasoners)
  const source = reasoners.length > 0 ? reasoners : ["classify_issue", "compose_reply_brief"]
  const steps: TraceStep[] = source.map((reasoner) => {
    const copy = stepCopy(reasoner)
    return {
      id: reasoner,
      title: copy.title,
      detail: copy.detail,
      status: status === "planning" ? "active" : "complete",
    }
  })

  const finalStep: TraceStep = {
    id: "compose_customer_reply",
    title: "Answer the customer",
    detail: plan.dynamic_branch
      ? `Use the ${formatLabel(plan.dynamic_branch).toLowerCase()} route in a calm support tone.`
      : "Use the completed checks in a calm support tone.",
    status:
      status === "complete"
        ? "complete"
        : status === "error"
          ? "error"
          : status === "drafting"
            ? "active"
            : "pending",
  }

  return [...steps, finalStep]
}

function renderPlanGuidance(plan: AgentPlan): string {
  const guidance = [
    plan.issue?.summary ? `Customer issue: ${plan.issue.summary}` : null,
    plan.issue?.category ? `Category: ${formatLabel(plan.issue.category)}` : null,
    plan.issue?.urgency ? `Urgency: ${formatLabel(plan.issue.urgency)}` : null,
    plan.dynamic_branch ? `Response route: ${formatLabel(plan.dynamic_branch)}` : null,
    plan.guardrail?.allowed_commitment
      ? `Allowed commitment: ${plan.guardrail.allowed_commitment}`
      : null,
    plan.guardrail?.must_verify?.length
      ? `Verify before promising: ${plan.guardrail.must_verify.join("; ")}`
      : null,
    plan.guardrail?.do_not_promise?.length
      ? `Do not promise: ${plan.guardrail.do_not_promise.join("; ")}`
      : null,
    plan.brief?.next_steps?.length
      ? `Suggested next steps: ${plan.brief.next_steps.join("; ")}`
      : null,
  ].filter(Boolean)

  return guidance.join("\n")
}

function sanitizeAssistantContent(content: string): string {
  return content
    .replace(/Internal response guidance:[\s\S]*$/i, "")
    .replace(/Private response guidance:[\s\S]*$/i, "")
    .replace(/\n?Customer issue:[\s\S]*$/i, "")
    .replace(/^Customer ticket:\s*/i, "")
    .trim()
}

export function SupportChatClient({ tenantId }: Props) {
  const [draft, setDraft] = useState("")
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [streaming, setStreaming] = useState(false)

  const latestAssistant = useMemo(
    () =>
      messages
        .slice()
        .reverse()
        .find((message) => message.role === "assistant"),
    [messages],
  )

  const tourSteps = [
    {
      element: "[data-tour='support-chat-heading']",
      popover: {
        title: "A customer-facing chat",
        description:
          "The product surface stays simple: sign in, ask for help, and get a useful response.",
        side: "bottom" as const,
        align: "start" as const,
      },
    },
    {
      element: "[data-tour='prompt-suggestions']",
      popover: {
        title: "Start with realistic cases",
        description:
          "These examples are just customer messages. The app decides how much planning and checking the response needs.",
        side: "bottom" as const,
        align: "start" as const,
      },
    },
    {
      element: "[data-tour='chat-thread']",
      popover: {
        title: "Planning stays visible, not technical",
        description:
          "While the assistant works, customers see a natural route and check status without needing technical terminology.",
        side: "top" as const,
        align: "start" as const,
      },
    },
    {
      element: "[data-tour='chat-composer']",
      popover: {
        title: "Type normally",
        description:
          "Free-form chat still works. The same route chips appear only when the response actually has planning data.",
        side: "top" as const,
        align: "start" as const,
      },
    },
  ]

  const patchAssistant = (id: string, updater: (message: ChatMessage) => ChatMessage) => {
    setMessages((current) =>
      current.map((message) =>
        message.id === id && message.role === "assistant" ? updater(message) : message,
      ),
    )
  }

  const handleAsk = async (prompt = draft) => {
    const text = prompt.trim()
    if (!text || streaming) return

    setStreaming(true)
    setDraft("")

    const requestId = createRequestId()
    const userId = `user-${requestId}`
    const assistantId = `assistant-${requestId}`

    setMessages((current) => [
      ...current,
      { id: userId, role: "user", content: text },
      {
        id: assistantId,
        role: "assistant",
        content: "",
        status: "planning",
        requestId,
      },
    ])

    try {
      const headers = new Headers({
        "content-type": "application/json",
        "x-request-id": requestId,
      })
      const apiKey = await ensureOnboardingAPIKey()
      if (apiKey) {
        headers.set("authorization", `Bearer ${apiKey}`)
      }

      const plan = await fetchAgentPlan(headers, text, tenantId)
      patchAssistant(assistantId, (message) => ({
        ...message,
        plan: plan.plan,
        status: "drafting",
      }))

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
              role: "system",
              content: `Private response guidance:\n${renderPlanGuidance(plan.plan)}`,
            },
            { role: "user", content: text },
          ],
        }),
      })

      if (!res.ok || !res.body) {
        let detail = `HTTP ${res.status}`
        try {
          const body = await res.text()
          detail += ` - ${body.slice(0, 200)}`
        } catch {
          // ignore
        }
        patchAssistant(assistantId, (message) => ({
          ...message,
          status: "error",
          error: detail,
        }))
        return
      }

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
              patchAssistant(assistantId, (message) => ({
                ...message,
                content: sanitizeAssistantContent(fullText),
              }))
            }
            if (json.usage) {
              patchAssistant(assistantId, (message) => ({
                ...message,
                usage: {
                  prompt_tokens: json.usage.prompt_tokens,
                  completion_tokens: json.usage.completion_tokens,
                  total_tokens: json.usage.total_tokens,
                  cost_usd: json.usage.cost_usd ?? json.usage.response_cost,
                  response_cost: json.usage.response_cost,
                },
              }))
            }
          } catch {
            // partial chunk; skip
          }
        }
      }

      patchAssistant(assistantId, (message) => ({
        ...message,
        content: sanitizeAssistantContent(fullText) || message.content,
        status: "complete",
      }))
    } catch (err) {
      patchAssistant(assistantId, (message) => ({
        ...message,
        status: "error",
        error: err instanceof Error ? err.message : String(err),
      }))
    } finally {
      setStreaming(false)
    }
  }

  return (
    <div className="mx-auto flex h-full min-h-0 w-full max-w-5xl flex-col gap-4">
      <div className="flex shrink-0 flex-wrap items-start justify-between gap-3">
        <div data-tour="support-chat-heading">
          <h1 className="text-2xl font-semibold tracking-tight">Support Chat</h1>
          <p className="text-muted-foreground max-w-2xl text-sm">
            Ask about refunds, access issues, renewals, or technical problems. The assistant chooses
            the right checks while keeping the conversation simple.
          </p>
        </div>
        <GuidedTour id="customer-support-chat-v1" autoStart steps={tourSteps} />
      </div>

      <Card className="flex min-h-0 flex-1 flex-col overflow-hidden" data-tour="chat-thread">
        <CardHeader className="border-b">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div>
              <CardTitle className="flex items-center gap-2 text-base">
                <MessageSquareText className="size-4" />
                Conversation
              </CardTitle>
              <CardDescription>
                Route and check chips appear when the assistant needs them.
              </CardDescription>
            </div>
            {latestAssistant?.status === "complete" ? (
              <Badge variant="secondary" className="gap-1.5">
                <CheckCircle2 className="size-3" />
                Ready
              </Badge>
            ) : streaming ? (
              <Badge variant="outline" className="gap-1.5">
                <Loader2 className="size-3 animate-spin" />
                Working
              </Badge>
            ) : null}
          </div>
        </CardHeader>
        <CardContent className="flex min-h-0 flex-1 flex-col p-0">
          <ScrollArea className="min-h-0 flex-1 basis-0 overflow-hidden">
            <div className="flex flex-col gap-5 p-4 md:p-6">
              {messages.length === 0 ? (
                <EmptyChatState onSelect={handleAsk} disabled={streaming} />
              ) : (
                messages.map((message) => <ChatBubble key={message.id} message={message} />)
              )}
            </div>
          </ScrollArea>
          <Separator />
          <div className="shrink-0 space-y-4 p-4 md:p-6">
            {messages.length > 0 ? (
              <PromptSuggestions onSelect={handleAsk} disabled={streaming} compact />
            ) : null}
            <div className="flex flex-col gap-3" data-tour="chat-composer">
              <Textarea
                placeholder="Tell us what you need help with..."
                value={draft}
                onChange={(event) => setDraft(event.target.value)}
                rows={3}
                disabled={streaming}
                onKeyDown={(event) => {
                  if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
                    event.preventDefault()
                    void handleAsk()
                  }
                }}
              />
              <div className="flex flex-wrap items-center justify-between gap-2">
                <p className="text-muted-foreground text-xs">
                  Press Cmd+Enter to send. Click a suggestion to run it immediately.
                </p>
                <Button onClick={() => handleAsk()} disabled={streaming || !draft.trim()}>
                  <Send data-icon="inline-start" />
                  {streaming ? "Sending..." : "Send"}
                </Button>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

function EmptyChatState({
  onSelect,
  disabled,
}: {
  onSelect: (prompt: string) => void
  disabled: boolean
}) {
  return (
    <div className="grid gap-4 py-2 sm:py-8">
      <div className="mx-auto flex max-w-lg flex-col items-center text-center">
        <div className="bg-primary/10 text-primary mb-3 flex size-10 items-center justify-center rounded-md sm:mb-4 sm:size-11">
          <Sparkles className="size-5" />
        </div>
        <h2 className="text-lg font-semibold tracking-tight sm:text-xl">
          What do you need help with?
        </h2>
        <p className="text-muted-foreground mt-1 text-sm sm:mt-2">
          Pick a realistic customer message or type your own. The assistant will show the route it
          is taking only when there is something useful to show.
        </p>
      </div>
      <PromptSuggestions onSelect={onSelect} disabled={disabled} />
    </div>
  )
}

function PromptSuggestions({
  onSelect,
  disabled,
  compact = false,
}: {
  onSelect: (prompt: string) => void
  disabled: boolean
  compact?: boolean
}) {
  return (
    <div
      className={
        compact
          ? "flex gap-2 overflow-x-auto pb-1"
          : "flex gap-2 overflow-x-auto pb-1 sm:grid sm:grid-cols-2 sm:overflow-visible sm:pb-0"
      }
      data-tour="prompt-suggestions"
    >
      {SUGGESTED_PROMPTS.map((suggestion) => (
        <Button
          key={suggestion.title}
          type="button"
          variant="outline"
          className={
            compact
              ? "h-10 min-w-36 shrink-0 justify-start overflow-hidden px-3 text-left"
              : "h-10 min-w-36 shrink-0 justify-start overflow-hidden px-3 text-left sm:h-auto sm:min-w-0 sm:shrink sm:whitespace-normal sm:p-3"
          }
          disabled={disabled}
          onClick={() => onSelect(suggestion.prompt)}
        >
          <span
            className={compact ? "flex min-w-0 items-center gap-2" : "flex min-w-0 flex-col gap-1"}
          >
            <span className="flex min-w-0 items-center gap-2 text-sm font-medium">
              <Route className="size-3.5 shrink-0" />
              <span className="truncate">{suggestion.title}</span>
            </span>
            {!compact ? (
              <span className="text-muted-foreground hidden text-xs font-normal sm:block">
                {suggestion.intent}
              </span>
            ) : null}
          </span>
        </Button>
      ))}
    </div>
  )
}

function ChatBubble({ message }: { message: ChatMessage }) {
  const isAssistant = message.role === "assistant"
  return (
    <div className={isAssistant ? "flex gap-3" : "flex justify-end gap-3"}>
      {isAssistant ? (
        <Avatar className="mt-1">
          <AvatarFallback>AI</AvatarFallback>
        </Avatar>
      ) : null}
      <div
        className={
          isAssistant
            ? "max-w-[82%] space-y-3"
            : "bg-primary text-primary-foreground max-w-[82%] rounded-lg px-4 py-3 text-sm"
        }
      >
        {isAssistant ? <AssistantMessage message={message} /> : message.content}
      </div>
      {!isAssistant ? (
        <Avatar className="mt-1">
          <AvatarFallback>
            <UserRound className="size-4" />
          </AvatarFallback>
        </Avatar>
      ) : null}
    </div>
  )
}

function AssistantMessage({ message }: { message: ChatMessage }) {
  const chips = planChips(message.plan)
  const trace = workflowTrace(message.plan, message.status)
  return (
    <>
      <div className="rounded-lg border bg-muted/35 px-4 py-3">
        <PlanningState status={message.status} chips={chips} trace={trace} />
        {message.error ? (
          <div className="mt-3 rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">
            {message.error}
          </div>
        ) : null}
        {message.content ? (
          <div className="prose prose-sm dark:prose-invert mt-4 max-w-none">
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
              {message.content}
            </ReactMarkdown>
          </div>
        ) : null}
      </div>
    </>
  )
}

function PlanningState({
  status,
  chips,
  trace,
}: {
  status?: ChatStatus
  chips: string[]
  trace: TraceStep[]
}) {
  const isWorking = status === "planning" || status === "drafting"
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant={isWorking ? "outline" : "secondary"} className="gap-1.5">
          {isWorking ? (
            <Loader2 className="size-3 animate-spin" />
          ) : (
            <CheckCircle2 className="size-3" />
          )}
          {status === "planning"
            ? "Planning response"
            : status === "drafting"
              ? "Preparing answer"
              : status === "error"
                ? "Needs attention"
                : "Response ready"}
        </Badge>
        {chips.map((chip) => (
          <Badge key={chip} variant="outline">
            {chip}
          </Badge>
        ))}
      </div>
      <ReasoningTrace steps={trace} />
    </div>
  )
}

function ReasoningTrace({ steps }: { steps: TraceStep[] }) {
  return (
    <div className="rounded-md border bg-background/80 p-3">
      <div className="flex items-center justify-between gap-3">
        <div>
          <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            Decision path
          </p>
          <p className="text-xs text-muted-foreground">
            Structured checks before the answer is shown.
          </p>
        </div>
        <Badge variant="outline" className="shrink-0 text-[10px]">
          {steps.length} steps
        </Badge>
      </div>
      <ol className="mt-4">
        {steps.map((step, index) => (
          <ReasoningTraceStep key={step.id} step={step} isLast={index === steps.length - 1} />
        ))}
      </ol>
    </div>
  )
}

function ReasoningTraceStep({ step, isLast }: { step: TraceStep; isLast: boolean }) {
  const isActive = step.status === "active"
  const isComplete = step.status === "complete"
  const isError = step.status === "error"

  return (
    <li className="relative grid grid-cols-[1.25rem_1fr] gap-3 pb-4 last:pb-0">
      {!isLast ? (
        <span className="absolute left-[0.6rem] top-6 bottom-0 w-px bg-border" aria-hidden />
      ) : null}
      <span
        className={[
          "relative z-10 mt-0.5 flex size-5 items-center justify-center rounded-full border bg-background",
          isActive ? "border-primary text-primary shadow-[0_0_0_4px_var(--accent)]" : "",
          isComplete ? "border-primary bg-primary text-primary-foreground" : "",
          isError ? "border-destructive text-destructive" : "",
          step.status === "pending" ? "border-border text-muted-foreground" : "",
        ].join(" ")}
        aria-current={isActive ? "step" : undefined}
      >
        {isActive ? (
          <Loader2 className="size-3 animate-spin" />
        ) : isComplete ? (
          <CheckCircle2 className="size-3" />
        ) : (
          <Circle className="size-2.5" />
        )}
      </span>
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <p className="text-sm font-medium leading-5">{step.title}</p>
          {isActive ? (
            <span className="rounded-md bg-primary/10 px-1.5 py-0.5 text-[10px] font-medium text-primary">
              running
            </span>
          ) : null}
        </div>
        <p className="mt-0.5 text-xs leading-5 text-muted-foreground">{step.detail}</p>
      </div>
    </li>
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
      detail += ` - ${text.slice(0, 200)}`
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
