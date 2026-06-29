// SPDX-License-Identifier: Apache-2.0

package remote

// Header names defined by docs/adapters/PROTOCOL.md §4-5. Centralized so
// every shim and every test refers to the same string and a typo
// anywhere is caught at compile time.
const (
	HeaderRequestID      = "X-Backai-Request-Id"
	HeaderIdempotencyKey = "X-Backai-Idempotency-Key"
	HeaderTenantID       = "X-Backai-Tenant-Id"
	HeaderRuntimeVersion = "X-Backai-Runtime-Version"
	HeaderContentType    = "X-Backai-Content-Type"

	// Slot-specific response signals that the runtime uses for cost
	// accounting after a multimodal call. Defined here so any slot can
	// reference them but they're only set by the multimodal protocol.
	HeaderCharCount       = "X-Backai-Char-Count"
	HeaderDurationSeconds = "X-Backai-Duration-Seconds"
	HeaderModelUsed       = "X-Backai-Model-Used"

	// MIME used for RFC 7807 error envelopes. The runtime currently
	// accepts application/json too as a fallback.
	MIMEProblemJSON = "application/problem+json"
)

// RuntimeVersion is the X-BackAI-Runtime-Version value sent on every
// adapter call. Bumped per release. Adapters that surface this in their
// logs help operators correlate runtime versions with adapter behaviour
// during upgrades.
const RuntimeVersion = "1.0.0"
