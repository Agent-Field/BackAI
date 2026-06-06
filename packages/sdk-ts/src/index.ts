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
export {
  jobs,
  enqueue as enqueueJob,
  get as getJob,
  retry as retryJob,
  list as listJobs,
  JobSchema,
  JobListSchema,
  JobStateSchema,
  type Job,
  type JobList,
  type JobState,
  type EnqueueJobOptions,
  type ListJobsOptions,
} from "./jobs.js"
export { secrets, get as getSecret } from "./secrets.js"
export {
  storage,
  upload as uploadObject,
  download as downloadObject,
  signedURL as signedObjectURL,
  delete as deleteObject,
  list as listObjects,
  StorageObjectSchema,
  StorageListSchema,
  SignedURLSchema,
  type StorageObject,
  type StorageList,
  type SignedURL,
  type UploadOptions,
  type ListStorageOptions,
} from "./storage.js"

import { agents } from "./agents.js"
import { jobs } from "./jobs.js"
import { secrets } from "./secrets.js"
import { storage } from "./storage.js"

/** Top-level namespace: `suite.agents.*`, `suite.jobs.*`, `suite.secrets.*`, `suite.storage.*`. */
export const suite = {
  agents,
  jobs,
  secrets,
  storage,
} as const
