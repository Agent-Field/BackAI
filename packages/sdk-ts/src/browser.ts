// SPDX-License-Identifier: Apache-2.0

// Browser-safe entry — the operational surface WITHOUT the privileged
// namespaces.
//
//   import { BackAI, agents } from "@af-stack/sdk/browser"
//
// This entry deliberately excludes `admin`/operator verbs and the pull-based
// `Worker`: neither belongs in code shipped to an end-user browser (admin
// requires an operator credential; the worker installs process signal
// handlers and long-polls with a privileged key). Enforcement is at the
// module-boundary / type level — those symbols are simply not re-exported
// here, so a browser bundle that imports only from `@af-stack/sdk/browser`
// cannot pull them in.

export const VERSION = "0.0.1"

export { ctx, withCtx, type CtxValues } from "./ctx.js"

export {
  SuiteError,
  SUPPORTED_RUNTIME,
  SUPPORTED_RUNTIME_MAJOR,
  DEFAULT_TIMEOUT_MS,
  DEFAULT_MAX_RETRIES,
  checkRuntimeCompat,
  type HttpOptions,
  type SseEvent,
  type ClientConfig,
} from "./_http.js"

export { BackAI, GOVERNED_NAMESPACES, type BackAIOptions } from "./client.js"
export { AsyncPaginator, paginate, type PageFetcher } from "./pagination.js"

// ─── Operational namespaces (browser-safe) ───────────────────────────────

import { agents } from "./agents.js"
import { approvals } from "./approvals.js"
import { auth } from "./auth.js"
import { billing } from "./billing.js"
import { cost } from "./cost.js"
import { harnesses } from "./harnesses.js"
import { jobs } from "./jobs.js"
import { llm, audio, images } from "./llm.js"
import { memory } from "./memory.js"
import { notifications } from "./notifications.js"
import { oauth } from "./oauth.js"
import { realtime } from "./realtime.js"
import { runs } from "./runs.js"
import { sandbox } from "./sandbox.js"
import { search, searchIndex } from "./search.js"
import { shipwright } from "./shipwright.js"
import { storage } from "./storage.js"
import { tools } from "./tools.js"
import { webhooks } from "./webhooks.js"
import { activity } from "./activity.js"
import { flags } from "./flags.js"
import { secrets } from "./secrets.js"
import { skills } from "./skills.js"

export {
  agents,
  approvals,
  auth,
  billing,
  cost,
  harnesses,
  jobs,
  llm,
  audio,
  images,
  memory,
  notifications,
  oauth,
  realtime,
  runs,
  sandbox,
  search,
  searchIndex,
  shipwright,
  storage,
  tools,
  webhooks,
  activity,
  flags,
  secrets,
  skills,
}

/** Browser-safe top-level namespace (mirrors `suite` but without `admin`). */
export const suite = {
  agents,
  approvals,
  auth,
  shipwright,
  search,
  searchIndex,
  activity,
  flags,
  runs,
  jobs,
  secrets,
  storage,
  llm,
  audio,
  images,
  cost,
  memory,
  sandbox,
  notifications,
  webhooks,
  billing,
  tools,
  harnesses,
  oauth,
} as const
