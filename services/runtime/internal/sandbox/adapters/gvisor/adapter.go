// Package gvisor implements the sandbox.Sandbox interface against a Docker
// daemon configured with the gVisor (runsc) OCI runtime.
//
// gVisor is a userspace kernel that intercepts syscalls inside a Docker
// container, providing much stronger isolation than vanilla runc at the
// cost of ~10-30% performance and some syscall-compatibility quirks. It's
// the right default for multi-tenant workloads where Firecracker is
// overkill but plain Docker is too leaky.
//
// HOST REQUIREMENTS
//
// gVisor must be installed on the Docker host and registered as a runtime
// in the daemon config. On Debian/Ubuntu:
//
//	# 1. Install the runsc binary
//	apt-get install runsc        # or: curl https://gvisor.dev/install.sh | bash
//
//	# 2. Register it as a Docker runtime in /etc/docker/daemon.json
//	#
//	#   {
//	#     "runtimes": {
//	#       "runsc": { "path": "/usr/bin/runsc" }
//	#     }
//	#   }
//
//	# 3. Restart Docker so the new runtime is picked up
//	systemctl restart docker
//
// Once that's done, this adapter just forwards every call to the underlying
// Docker adapter with HostConfig.Runtime forced to "runsc". The container
// API, log streaming, resource limits, and cleanup are all identical to
// the plain-docker code path — gVisor only changes what's inside the
// container, not how the daemon manages it.
//
// PICK THIS ADAPTER WHEN
//
//   - You need stronger isolation than the runc default (untrusted code).
//   - You don't yet need micro-VM isolation (Firecracker).
//   - You're OK with a small performance tax in exchange for blast-radius
//     reduction on a kernel CVE.
//
// Set in config.yaml as `sandbox: { adapter: gvisor }` or via
// AF_STACK_SANDBOX_ADAPTER=gvisor.
package gvisor

import (
	"github.com/Agent-Field/backai/services/runtime/internal/sandbox"
	dockeradapter "github.com/Agent-Field/backai/services/runtime/internal/sandbox/adapters/docker"
)

// runscRuntime is the OCI runtime name registered in the Docker daemon
// config that maps to the gVisor `runsc` binary. This must match the
// daemon.json runtimes key — see host-requirements doc-comment above.
const runscRuntime = "runsc"

// adapterName is the label the dashboard sees in Capabilities().Adapter.
const adapterName = "gvisor"

// Config mirrors the Docker adapter's Config minus the Runtime field —
// gvisor always sets that to "runsc".
type Config struct {
	// Host is the Docker daemon socket. Empty = pick up DOCKER_HOST.
	Host string
}

// New constructs a gVisor sandbox adapter.
//
// Under the hood this is just a Docker adapter with Runtime="runsc" forced
// into the container-create call and the adapter label switched to "gvisor"
// so dashboards and metrics correctly attribute work to the gvisor pool.
//
// Returns sandbox.Sandbox (the interface) rather than *dockeradapter.Adapter
// directly so callers can't accidentally call adapter-specific methods like
// Config() and bypass our runtime override.
func New(cfg Config) (sandbox.Sandbox, error) {
	return dockeradapter.New(dockeradapter.Config{
		Host:        cfg.Host,
		Runtime:     runscRuntime,
		AdapterName: adapterName,
	})
}
