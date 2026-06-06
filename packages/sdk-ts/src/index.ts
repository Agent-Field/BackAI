// AF Stack — Suite SDK for TypeScript
//
// The suite SDK exposes the operational verbs an app uses daily:
//
//   import { suite, ctx } from "@af-stack/sdk"
//
//   // Call an agent
//   const result = await suite.agents.call("notable-ai.summarize", { text: "..." })
//
//   // Read tenant context
//   const tenantId = ctx.tenantId
//
// Inside an AgentField agent process, use the AgentField SDK to *define*
// agents. Use this suite SDK to *call* them and to use suite infrastructure.
//
// See https://github.com/Agent-Field/backai for full docs.

export const VERSION = "0.0.1"
