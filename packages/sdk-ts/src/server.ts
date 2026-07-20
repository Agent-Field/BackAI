// SPDX-License-Identifier: Apache-2.0

// Server entry — the privileged superset.
//
//   import { Worker, admin, BackAI } from "@af-stack/sdk/server"
//
// Re-exports the full default surface (including the `admin` namespace) AND
// the pull-based `Worker`, which is intentionally NOT reachable from the
// default (`@af-stack/sdk`) or browser (`@af-stack/sdk/browser`) entries: it
// authenticates with a privileged `jobs:work` key, runs a long-poll loop, and
// installs process signal handlers, so it only belongs in a trusted backend.

export * from "./index.js"

export {
  Worker,
  JobContext,
  PermanentError,
  type Handler,
  type WorkerOptions,
  type LogLine,
} from "./worker.js"
