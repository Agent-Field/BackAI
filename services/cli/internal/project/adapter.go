// SPDX-License-Identifier: Apache-2.0

package project

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

// knownAdapterSlots are the slots that speak the remote-adapter HTTP protocol
// (docs/adapters/PROTOCOL.md + protocols/<slot>-v1.md). `af-stack adapter new`
// scaffolds a sidecar for one of these.
var knownAdapterSlots = []string{
	"sandbox", "storage", "notifications", "secrets", "billing",
	"multimodal", "logs", "traces", "metrics", "errors",
}

func validAdapterSlot(slot string) bool {
	for _, s := range knownAdapterSlots {
		if s == slot {
			return true
		}
	}
	return false
}

// runAdapterNew scaffolds a new remote-adapter sidecar for a slot. The result
// is a self-contained FastAPI project that already implements the universal
// contract (/healthz, /v1/capabilities, /v1/info) and is ready for the
// conformance harness; the developer fills in the per-slot endpoints.
//
//	af-stack adapter new <slot> [name] [--name <name>] [--dir <parent>]
func runAdapterNew(args []string, stdout, stderr io.Writer) error {
	// Leading positionals: <slot> [name]. Pull them off before flag parsing
	// (Go's flag package stops at the first positional).
	pos := []string{}
	i := 0
	for ; i < len(args) && !strings.HasPrefix(args[i], "-"); i++ {
		pos = append(pos, args[i])
	}
	rest := args[i:]

	fs := flag.NewFlagSet("af-stack adapter new", flag.ContinueOnError)
	fs.SetOutput(stderr)
	nameFlag := fs.String("name", "", "adapter project name (directory)")
	dir := fs.String("dir", ".", "parent directory to create the adapter in")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("adapter new: unexpected argument %q", fs.Arg(0))
	}

	if len(pos) == 0 {
		return fmt.Errorf("adapter new: usage: af-stack adapter new <slot> [name]\n  slots: %s",
			strings.Join(knownAdapterSlots, ", "))
	}
	slot := strings.ToLower(strings.TrimSpace(pos[0]))
	if !validAdapterSlot(slot) {
		return fmt.Errorf("adapter new: unknown slot %q (one of: %s)",
			slot, strings.Join(knownAdapterSlots, ", "))
	}

	name := ""
	if len(pos) > 1 {
		name = pos[1]
	}
	if name == "" {
		name = strings.TrimSpace(*nameFlag)
	}
	if name == "" {
		name = slot + "-adapter"
	}
	id := slugify(name)
	target := filepath.Join(*dir, id)
	if exists(target) {
		return fmt.Errorf("adapter new: %s already exists", filepath.ToSlash(target))
	}

	if err := writeFiles(target, adapterTemplate(slot, id)); err != nil {
		return err
	}

	env := "AF_STACK_" + strings.ToUpper(slot) + "_ADAPTER"
	urlEnv := "AF_STACK_" + strings.ToUpper(slot) + "_ADAPTER_URL"
	fmt.Fprintf(stdout, "Created %s adapter scaffold at %s\n\n", slot, filepath.ToSlash(target))
	fmt.Fprintln(stdout, "Next steps:")
	fmt.Fprintf(stdout, "  cd %s\n", filepath.ToSlash(target))
	fmt.Fprintln(stdout, "  pip install -r requirements.txt")
	fmt.Fprintln(stdout, "  uvicorn main:app --port 8090")
	fmt.Fprintf(stdout, "  backai-adapter-conformance --slot %s --url http://localhost:8090\n\n", slot)
	fmt.Fprintln(stdout, "Then point the runtime at it:")
	fmt.Fprintf(stdout, "  %s=remote\n", env)
	fmt.Fprintf(stdout, "  %s=http://your-sidecar:8090\n\n", urlEnv)
	fmt.Fprintf(stdout, "Implement the per-slot endpoints from docs/adapters/protocols/%s-v1.md\n", slot)
	return nil
}

const mdFence = "```"

// adapterTemplate returns the relative-path -> contents map for a remote
// adapter sidecar skeleton for the given slot.
func adapterTemplate(slot, id string) map[string]string {
	slotUpper := strings.ToUpper(slot)

	main := `"""` + slot + ` remote adapter (skeleton).

Implements the universal BackAI adapter contract (/healthz, /v1/capabilities,
/v1/info). Fill in the per-slot endpoints from:
    docs/adapters/protocols/` + slot + `-v1.md

Run:
    uvicorn main:app --host 0.0.0.0 --port 8090
"""

from __future__ import annotations

from typing import Optional

from fastapi import FastAPI, Header

app = FastAPI(title="` + id + ` (` + slot + ` adapter)")

ADAPTER_NAME = "` + id + `"
ADAPTER_VERSION = "0.1.0"


@app.get("/healthz")
async def healthz():
    """Liveness probe. Return 200 once the adapter can serve requests."""
    return {"status": "healthy"}


@app.get("/v1/capabilities")
async def capabilities(authorization: Optional[str] = Header(None)):
    """Declare what this adapter supports. The runtime only routes to the
    capabilities you advertise here — declaring a capability you don't
    implement will fail the conformance harness, so keep this honest."""
    return {
        "name": ADAPTER_NAME,
        "version": ADAPTER_VERSION,
        "slot": "` + slot + `",
        "protocol_version": "v1",
        "vendor": "you",
        # TODO: set the supports_* flags for the endpoints you implement.
        # See docs/adapters/protocols/` + slot + `-v1.md for the full list.
        "capabilities": {},
    }


@app.get("/v1/info")
async def info():
    return {
        "docs": "https://github.com/Agent-Field/backai/blob/main/docs/adapters/protocols/` + slot + `-v1.md",
    }


# TODO: implement the ` + slot + `-v1 endpoints below. Until then the conformance
# harness will pass the common contract but report the slot endpoints as
# unimplemented. Each endpoint should honor the Authorization bearer token if
# AF_STACK_` + slotUpper + `_ADAPTER_TOKEN is configured on the runtime side.
`

	requirements := `fastapi>=0.111,<1.0
uvicorn[standard]>=0.30,<1.0
pydantic>=2.0,<3.0
`

	gitignore := `__pycache__/
*.pyc
.venv/
`

	dockerfile := `FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE 8090
CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8090"]
`

	readme := "# " + id + "\n\n" +
		"A custom **" + slot + "** adapter for the BackAI connection system. It speaks the\n" +
		"remote-adapter HTTP protocol, so the runtime can use it with no code changes —\n" +
		"you point a slot at this sidecar with one env var.\n\n" +
		"## Run it\n\n" +
		mdFence + "bash\n" +
		"pip install -r requirements.txt\n" +
		"uvicorn main:app --port 8090\n" +
		mdFence + "\n\n" +
		"## Verify it against the contract\n\n" +
		mdFence + "bash\n" +
		"backai-adapter-conformance --slot " + slot + " --url http://localhost:8090\n" +
		mdFence + "\n\n" +
		"## Wire it into the runtime\n\n" +
		"Set these on the runtime (no rebuild needed):\n\n" +
		mdFence + "bash\n" +
		"AF_STACK_" + slotUpper + "_ADAPTER=remote\n" +
		"AF_STACK_" + slotUpper + "_ADAPTER_URL=http://your-sidecar:8090\n" +
		"AF_STACK_" + slotUpper + "_ADAPTER_TOKEN=optional-bearer-token\n" +
		mdFence + "\n\n" +
		"## Implement the endpoints\n\n" +
		"`main.py` already implements the universal contract (`/healthz`,\n" +
		"`/v1/capabilities`, `/v1/info`). Fill in the per-slot endpoints from\n" +
		"[`docs/adapters/protocols/" + slot + "-v1.md`](https://github.com/Agent-Field/backai/blob/main/docs/adapters/protocols/" + slot + "-v1.md)\n" +
		"and declare each one in the `capabilities` map as you go.\n"

	return map[string]string{
		"main.py":          main,
		"requirements.txt": requirements,
		".gitignore":       gitignore,
		"Dockerfile":       dockerfile,
		"README.md":        readme,
	}
}
