// SPDX-License-Identifier: Apache-2.0

package jobcmd

// pyTemplate is the Python worker scaffold. {{KIND}} is replaced with the
// slugified job name. It mirrors packages/sdk-py/af_stack/worker.py.
const pyTemplate = `# SPDX-License-Identifier: Apache-2.0

"""{{KIND}} — a pull-based background job worker.

Run it with a tenant API key that carries the ` + "`jobs:work`" + ` scope:

    export AF_STACK_URL=http://localhost:8080
    export AF_STACK_API_KEY=af_live_...      # a key with the jobs:work scope
    python jobs/{{KIND}}.py
"""

from __future__ import annotations

import os

from af_stack.worker import PermanentError, Worker

worker = Worker(
    os.environ.get("AF_STACK_URL", "http://localhost:8080"),
    os.environ["AF_STACK_API_KEY"],
)


@worker.register("{{KIND}}")
def handle(payload, ctx):
    """Handle one "{{KIND}}" job.

    Return a JSON-serialisable value to COMPLETE the job. Raise
    PermanentError to fail WITHOUT a retry (dead-letter); raise any other
    exception to fail retryably (River retries with backoff).
    """
    ctx.log("processing {{KIND}}", job_id=ctx.job_id)
    if ctx.is_canceled():
        return None
    if not payload:
        raise PermanentError("empty payload — nothing to do")
    # TODO: do the work here.
    return {"ok": True, "echo": payload}


if __name__ == "__main__":
    worker.run()
`

// tsTemplate is the TypeScript worker scaffold. It imports the worker
// runtime from the SDK's server entrypoint (@af-stack/sdk/server), the TS
// half of the pull-based worker protocol.
const tsTemplate = `// SPDX-License-Identifier: Apache-2.0
//
// {{KIND}} — a pull-based background job worker.
//
// Run it with a tenant API key that carries the ` + "`jobs:work`" + ` scope:
//
//   export AF_STACK_URL=http://localhost:8080
//   export AF_STACK_API_KEY=af_live_...   # a key with the jobs:work scope
//   npx tsx jobs/{{KIND}}.ts

import { PermanentError, Worker } from "@af-stack/sdk/server";

const worker = new Worker(
  process.env.AF_STACK_URL ?? "http://localhost:8080",
  process.env.AF_STACK_API_KEY ?? "",
);

// Handle one "{{KIND}}" job. Return a JSON-serialisable value to COMPLETE
// the job. Throw PermanentError to fail WITHOUT a retry (dead-letter); throw
// anything else to fail retryably (the runtime retries with backoff).
worker.register("{{KIND}}", async (payload, ctx) => {
  ctx.log("processing {{KIND}}", { jobId: ctx.jobId });
  if (ctx.isCanceled()) return;
  if (payload == null) {
    throw new PermanentError("empty payload — nothing to do");
  }
  // TODO: do the work here.
  return { ok: true, echo: payload };
});

worker.run();
`
