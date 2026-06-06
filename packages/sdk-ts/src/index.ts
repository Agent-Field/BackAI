// AF Stack — Suite SDK for TypeScript
//
// Two equivalent import styles:
//
//   import { suite, ctx } from "@af-stack/sdk"
//   await suite.agents.call("notable-ai.summarize", { text: "..." })
//
//   import { agents, ctx } from "@af-stack/sdk"
//   await agents.call("notable-ai.summarize", { text: "..." })
//
// Inside an AgentField agent process, use the AgentField SDK to *define*
// agents. Use this suite SDK to *call* them and to use suite infrastructure.
//
// All model calls route through AgentField — this SDK never reaches a model
// provider directly. See docs/sdk-strategy.md.

export const VERSION = "0.0.1"

export { ctx, withCtx, type CtxValues } from "./ctx.js"
export {
  agents,
  call,
  callAsync,
  stream,
  status,
  cancel,
  approve,
  deny,
  pendingApprovals,
  type CallResult,
  type AsyncCallResult,
  type ExecutionStatus,
  type PendingApproval,
  type CallOptions,
  type CallAsyncOptions,
  type ApproveOptions,
  type DenyOptions,
} from "./agents.js"
export { SuiteError, type HttpOptions, type SseEvent } from "./_http.js"

import { agents } from "./agents.js"

/** Top-level namespace: `suite.agents.*` (and future `suite.jobs.*`, etc.). */
export const suite = {
  agents,
} as const
