# AF Stack CLI Telemetry

The `af-stack` CLI can send **anonymous, opt-out** usage telemetry to help the
maintainers understand which commands matter and prioritize the roadmap. This
document describes — exactly — what is and isn't sent, and how to turn it off.

## What is sent

When telemetry is active, the CLI sends **one event per command invocation**.
The event is a small JSON object with only these fields:

| Field         | Example                | Meaning                                   |
| ------------- | ---------------------- | ----------------------------------------- |
| `schema`      | `af-stack.cli/v1`      | Payload version                           |
| `command`     | `init`                 | The subcommand name **only**              |
| `cli_version` | `0.0.1`                | CLI version                               |
| `os`          | `linux`                | `runtime.GOOS`                            |
| `arch`        | `amd64`                | `runtime.GOARCH`                          |
| `success`     | `true`                 | Whether the command exited 0              |
| `duration_ms` | `200`                  | Wall-clock time, **bucketed** (coarse)    |
| `anon_id`     | `59c244db46a9a179…`    | Random per-machine id (see below)         |
| `ts`          | `2026-06-29T19:53:20Z` | UTC timestamp (RFC3339)                   |

The full event looks like:

```json
{
  "schema": "af-stack.cli/v1",
  "command": "init",
  "cli_version": "0.0.1",
  "os": "linux",
  "arch": "amd64",
  "success": true,
  "duration_ms": 200,
  "anon_id": "59c244db46a9a1792e798ad5b22ee527",
  "ts": "2026-06-29T19:53:20Z"
}
```

## What is NEVER sent

- **No command arguments or flag values.** Only the bare subcommand name, and
  it is sanitized — anything other than a known `[a-z-]` token collapses to
  `unknown`, so an argument can never leak in via the `command` field.
- **No file paths, working directory, project names, or file contents.**
- **No environment variable values.**
- **No usernames, emails, IP-identifying data, hostnames, or any PII.**
- **No precise timing.** `duration_ms` is bucketed (nearest 10 ms under 100 ms,
  100 ms under 1 s, 1 s above) so it can't fingerprint a specific run.

The `anon_id` is a random 128-bit value generated on first use and stored at
`~/.af-stack/anonymous_id`. It is **not** derived from any machine, user, or
network identifier — it only lets the maintainers distinguish "10 runs from one
machine" from "10 different machines". Delete the file to rotate it.

## The sink is off by default

Telemetry only sends anywhere if a collection endpoint is configured:

- `AF_STACK_TELEMETRY_URL` (environment), or
- a build-time default baked in via
  `-ldflags "-X …/telemetry.DefaultURL=https://…"`.

In the open-source build, **no endpoint is set, so nothing is ever sent.** When
no endpoint is configured the CLI makes no network calls for telemetry at all.

## How to opt out

Any one of these fully disables telemetry — no network calls, no first-run
notice:

- Pass `--no-telemetry` on any command:
  `af-stack init my-app --no-telemetry`
- Set the environment variable: `AF_STACK_TELEMETRY=0` (also accepts
  `false`, `off`, `no`).

## First-run notice

The first time telemetry is active on a machine, the CLI prints a one-time
notice to stderr explaining the above and pointing here, then records a marker
at `~/.af-stack/telemetry-notice` so it isn't shown again.
