// SPDX-License-Identifier: Apache-2.0

// Package tenancy holds the multi-tenancy data model + Manager
// implementation for AF Stack Phase 6.
//
// The Manager is a Postgres-backed concrete struct (NOT an interface)
// living in manager.go. Its method surface is the contract the admin
// REST handlers (services/runtime/internal/server/admin.go), SDKs, and
// dashboard pages depend on.
//
// Wire shapes (Tenant, User, Membership, APIKey, IssuedAPIKey,
// AuditEntry, TenantDetail) mirror the zod schemas in
// apps/dashboard/src/lib/api.ts EXACTLY (snake_case JSON tags, nullable
// fields emitted as JSON null via pointer types). Time-valued columns
// here are typed as time.Time / *time.Time; the admin handlers format
// them to RFC3339Nano UTC before serialising to the wire.
//
// Errors:
//
// We expose two layers of typed errors:
//   - ErrNotFound / ErrConflict / ErrInvalid — coarse buckets the admin
//     handlers map to HTTP 404 / 409 / 400 respectively. These are kept
//     for compatibility with existing handler code.
//   - ErrTenantNotFound / ErrSlugTaken / ErrInvalidRole / ErrKeyRevoked /
//     ErrKeyExpired / ErrKeySecretMismatch / ErrInvalidAPIKeyFormat /
//     ErrAPIKeyNotFound / ErrUserNotFound / ErrMembershipNotFound —
//     fine-grained sentinels used by the resolver middleware and
//     integration tests to discriminate failure modes. Each of these
//     wraps one of the coarse buckets via errors.Is, so handlers that
//     check the coarse bucket still work.
package tenancy

import (
	"context"
	"errors"
	"time"
)

// ─── Sentinel errors ──────────────────────────────────────────────────────

var (
	// ErrNotFound is the coarse "lookup miss" bucket. Admin handlers
	// map this to HTTP 404. Use errors.Is(err, ErrNotFound) to catch
	// the more specific ErrTenantNotFound / ErrAPIKeyNotFound / etc.
	ErrNotFound = errors.New("tenancy: not found")
	// ErrConflict is the coarse "uniqueness or state collision" bucket
	// (HTTP 409). Wraps ErrSlugTaken.
	ErrConflict = errors.New("tenancy: conflict")
	// ErrInvalid is the coarse "validation failure" bucket (HTTP 400).
	// Wraps ErrInvalidSlug / ErrInvalidRole / ErrInvalidAPIKeyFormat.
	ErrInvalid = errors.New("tenancy: invalid input")
)

// ErrTenantNotFound is returned by GetTenant/GetTenantDetail/Update/
// Delete when the tenant id has no row. Wraps ErrNotFound.
var ErrTenantNotFound = wrappedErr{base: ErrNotFound, msg: "tenancy: tenant not found"}

// ErrUserNotFound: AddMembership when user_id has no row.
var ErrUserNotFound = wrappedErr{base: ErrNotFound, msg: "tenancy: user not found"}

// ErrMembershipNotFound: RemoveMembership / FirstMembershipFor.
var ErrMembershipNotFound = wrappedErr{base: ErrNotFound, msg: "tenancy: membership not found"}

// ErrAPIKeyNotFound: RevokeKey / VerifyKey when prefix doesn't match.
var ErrAPIKeyNotFound = wrappedErr{base: ErrNotFound, msg: "tenancy: api key not found"}

// ErrSlugTaken: CreateTenant when the slug already belongs to another
// tenant. Wraps ErrConflict.
var ErrSlugTaken = wrappedErr{base: ErrConflict, msg: "tenancy: slug already taken"}

// ErrInvalidSlug: CreateTenant when slug fails the charset/length check.
var ErrInvalidSlug = wrappedErr{base: ErrInvalid, msg: "tenancy: invalid slug"}

// ErrInvalidRole: AddMembership when role is not in the allowed set.
var ErrInvalidRole = wrappedErr{base: ErrInvalid, msg: "tenancy: invalid role"}

// ErrInvalidAPIKeyFormat: VerifyKey when the token doesn't parse.
var ErrInvalidAPIKeyFormat = wrappedErr{base: ErrInvalid, msg: "tenancy: invalid api key format"}

// ErrKeyRevoked: VerifyKey matched a row whose revoked_at is set. The
// resolver maps this AND ErrKeySecretMismatch / ErrAPIKeyNotFound to
// HTTP 401, so the client can't distinguish revocation from a wrong
// secret (defence against existence oracles).
var ErrKeyRevoked = errors.New("tenancy: api key revoked")

// ErrKeyExpired: VerifyKey matched a row whose expires_at is past.
var ErrKeyExpired = errors.New("tenancy: api key expired")

// ErrKeySecretMismatch: VerifyKey matched a prefix but bcrypt failed.
var ErrKeySecretMismatch = errors.New("tenancy: api key secret mismatch")

// wrappedErr is a tiny error type whose Is method returns true for its
// `base` sentinel. Lets us define fine-grained errors that the coarse
// buckets recognise without writing manual Unwrap chains.
type wrappedErr struct {
	base error
	msg  string
}

func (e wrappedErr) Error() string { return e.msg }
func (e wrappedErr) Is(target error) bool {
	return target == e.base || target == e
}

// ─── Wire shapes (mirror apps/dashboard/src/lib/api.ts) ───────────────────

// Tenant matches TenantSchema. Settings and Quota are free-form maps that
// must round-trip as JSON objects (never null).
type Tenant struct {
	ID        string                 `json:"id"`
	Slug      string                 `json:"slug"`
	Name      string                 `json:"name"`
	Plan      string                 `json:"plan"`
	Settings  map[string]interface{} `json:"settings"`
	Quota     map[string]interface{} `json:"quota"`
	CreatedAt time.Time              `json:"created_at"`
	DeletedAt *time.Time             `json:"deleted_at"`
}

// User matches UserSchema.
type User struct {
	ID        string     `json:"id"`
	Email     string     `json:"email"`
	Name      *string    `json:"name"`
	AvatarURL *string    `json:"avatar_url"`
	CreatedAt time.Time  `json:"created_at"`
	DeletedAt *time.Time `json:"deleted_at"`
}

// Membership matches MembershipSchema.
type Membership struct {
	TenantID   string     `json:"tenant_id"`
	UserID     string     `json:"user_id"`
	Role       string     `json:"role"`
	InvitedAt  time.Time  `json:"invited_at"`
	AcceptedAt *time.Time `json:"accepted_at"`
}

// APIKey matches APIKeySchema (no plaintext).
//
// The LiteLLM-related fields land here as nullable so existing rows
// (which never went through LiteLLM mirroring) and new rows (which do)
// share one struct. The dashboard surfaces null as "—" and treats
// non-null budget/limits as "enforced upstream by LiteLLM". See
// migration 00022_api_keys_litellm.sql.
type APIKey struct {
	ID       string  `json:"id"`
	TenantID string  `json:"tenant_id"`
	Prefix   string  `json:"prefix"`
	Name     *string `json:"name"`
	// ServiceAccountName labels a key as belonging to a named non-human
	// service account (distinct from the human-facing Name). nil for
	// ordinary user-minted keys. Backed by suite_api_keys.service_account_name
	// (migration 00035_lifecycle.sql).
	ServiceAccountName *string    `json:"service_account_name"`
	Scopes             []string   `json:"scopes"`
	CreatedBy          *string    `json:"created_by"`
	CreatedAt          time.Time  `json:"created_at"`
	LastUsedAt         *time.Time `json:"last_used_at"`
	ExpiresAt          *time.Time `json:"expires_at"`
	RevokedAt          *time.Time `json:"revoked_at"`
	// LiteLLMKeyAlias is the alias we sent to LiteLLM at issuance time.
	// We never store the plaintext LiteLLM secret on this row — the
	// secrets vault holds it under "litellm/key/{api_key_id}". nil
	// when no LiteLLM mapping exists (legacy keys, or LiteLLM was
	// unreachable at issuance).
	LiteLLMKeyAlias *string `json:"litellm_key_alias"`
	// BudgetMaxUSD is the per-key lifetime spend cap. When a LiteLLM
	// virtual key exists it is mirrored upstream; independently, the
	// runtime enforces it from the cost_events ledger in the gateway
	// pre-call hook (402 BUDGET_EXCEEDED) so the cap holds even with a
	// DB-less LiteLLM. Nil = unlimited.
	BudgetMaxUSD *float64 `json:"budget_max_usd"`
	// RateLimitRPM is the per-minute request cap LiteLLM enforces when the
	// key is mirrored upstream. Not enforced runtime-side today (see a
	// key's mirror_status). Nil = unlimited.
	RateLimitRPM *int `json:"rate_limit_rpm"`
	// RateLimitTPM is the per-minute token cap LiteLLM enforces.
	RateLimitTPM *int `json:"rate_limit_tpm"`
}

// IssuedAPIKey matches IssuedAPIKeySchema = APIKey + value (one-time reveal).
type IssuedAPIKey struct {
	APIKey
	Value string `json:"value"`
}

// MirrorRotationError means RotateKey completed the local system-of-record
// rotation but failed while mirroring that change to LiteLLM virtual keys.
// Callers must still return the one-time plaintext replacement key.
type MirrorRotationError struct {
	Err error
}

func (e *MirrorRotationError) Error() string {
	if e == nil || e.Err == nil {
		return "tenancy: litellm mirror rotation failed"
	}
	return "tenancy: litellm mirror rotation failed: " + e.Err.Error()
}

func (e *MirrorRotationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// TenantDetailMember matches TenantDetailSchema.members[].
type TenantDetailMember struct {
	User User   `json:"user"`
	Role string `json:"role"`
}

// TenantDetailUsage matches TenantDetailSchema.usage.
type TenantDetailUsage struct {
	Requests30D  int64   `json:"requests_30d"`
	CostUSD30D   float64 `json:"cost_usd_30d"`
	StorageBytes int64   `json:"storage_bytes"`
	SecretsCount int     `json:"secrets_count"`
}

// TenantDetail matches TenantDetailSchema.
type TenantDetail struct {
	Tenant  Tenant               `json:"tenant"`
	Members []TenantDetailMember `json:"members"`
	APIKeys []APIKey             `json:"api_keys"`
	Usage   TenantDetailUsage    `json:"usage"`
}

// ─── Phase 12.1 drilldown shapes ──────────────────────────────────────────
//
// TenantDrilldown is the per-tenant "everything we know" payload backing
// /api/v1/admin/tenants/{id}/drilldown. It's a strict superset of
// TenantDetail: members carry last_active_at, usage carries 24-bucket
// sparklines, and the payload also includes recent runs, recent webhook
// deliveries, and an optional billing snapshot.

// DrilldownMember is TenantDetail.members[] + last_active_at.
type DrilldownMember struct {
	User         User       `json:"user"`
	Role         string     `json:"role"`
	LastActiveAt *time.Time `json:"last_active_at"`
}

// DrilldownUsage extends TenantDetailUsage with 24-bucket sparklines.
type DrilldownUsage struct {
	Requests30D      int64     `json:"requests_30d"`
	CostUSD30D       float64   `json:"cost_usd_30d"`
	StorageBytes     int64     `json:"storage_bytes"`
	SecretsCount     int       `json:"secrets_count"`
	CostSparkline    []float64 `json:"cost_sparkline"`
	RequestSparkline []float64 `json:"request_sparkline"`
}

// DrilldownRun is one row of the "recent runs" table on the drilldown page.
type DrilldownRun struct {
	ID         string    `json:"id"`
	Agent      string    `json:"agent"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"started_at"`
	DurationMS int64     `json:"duration_ms"`
	CostUSD    float64   `json:"cost_usd"`
}

// DrilldownWebhook is one row of the "recent webhooks" table.
type DrilldownWebhook struct {
	ID        string    `json:"id"`
	Direction string    `json:"direction"`
	EventType string    `json:"event_type"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// DrilldownBilling is the optional billing snapshot. nil pointer = no row
// in suite_billing_customers (the dashboard shows the "free plan" copy).
type DrilldownBilling struct {
	Plan               string     `json:"plan"`
	SubscriptionStatus *string    `json:"subscription_status"`
	CurrentPeriodEnd   *time.Time `json:"current_period_end"`
	TrialEndsAt        *time.Time `json:"trial_ends_at"`
}

// TenantDrilldown is the full per-tenant aggregate returned by the
// /api/v1/admin/tenants/{id}/drilldown endpoint.
type TenantDrilldown struct {
	Tenant         Tenant             `json:"tenant"`
	Members        []DrilldownMember  `json:"members"`
	APIKeys        []APIKey           `json:"api_keys"`
	Usage          DrilldownUsage     `json:"usage"`
	RecentRuns     []DrilldownRun     `json:"recent_runs"`
	RecentWebhooks []DrilldownWebhook `json:"recent_webhooks"`
	Billing        *DrilldownBilling  `json:"billing"`
}

// AuditEntry matches AuditEntrySchema.
type AuditEntry struct {
	ID           string                 `json:"id"`
	TenantID     *string                `json:"tenant_id"`
	UserID       *string                `json:"user_id"`
	APIKeyID     *string                `json:"api_key_id"`
	Action       string                 `json:"action"`
	ResourceType *string                `json:"resource_type"`
	ResourceID   *string                `json:"resource_id"`
	Metadata     map[string]interface{} `json:"metadata"`
	OccurredAt   time.Time              `json:"occurred_at"`
}

// AuditPage is the result of Manager.ListAudit (matches AuditListSchema).
type AuditPage struct {
	Entries []AuditEntry
	Total   int
	HasMore bool
}

// ─── Inputs ───────────────────────────────────────────────────────────────

// CreateTenantInput matches CreateTenantInputSchema. Used by the
// CreateTenant(ctx, in) shape; the slug-name-plan positional variant
// in manager.go is a convenience wrapper.
type CreateTenantInput struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Plan string `json:"plan,omitempty"`
}

// UpdateTenantInput matches UpdateTenantInputSchema. Each pointer field
// is nil when omitted by the caller.
type UpdateTenantInput struct {
	Name     *string                `json:"name,omitempty"`
	Plan     *string                `json:"plan,omitempty"`
	Settings map[string]interface{} `json:"settings,omitempty"`
	Quota    map[string]interface{} `json:"quota,omitempty"`
}

// IssueAPIKeyInput matches IssueAPIKeyInputSchema.
type IssueAPIKeyInput struct {
	TenantID string `json:"tenant_id"`
	Name     string `json:"name,omitempty"`
	// ServiceAccountName, when set, marks the key as a service-account
	// credential (persisted to suite_api_keys.service_account_name).
	ServiceAccountName string     `json:"service_account_name,omitempty"`
	Scopes             []string   `json:"scopes"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	// CreatedBy is the user id to record on the row. Optional — the
	// admin handlers don't yet wire a principal but the SDK will.
	CreatedBy string `json:"-"`
	// BudgetMaxUSD, when non-nil, becomes the LiteLLM virtual key's
	// max_budget. Nil = unlimited. LiteLLM enforces this upstream and
	// returns 429 when exceeded.
	BudgetMaxUSD *float64 `json:"budget_max_usd,omitempty"`
	// RateLimitRPM, when non-nil, becomes the LiteLLM virtual key's
	// rpm_limit. Nil = unlimited.
	RateLimitRPM *int `json:"rate_limit_rpm,omitempty"`
	// RateLimitTPM, when non-nil, becomes the LiteLLM virtual key's
	// tpm_limit (tokens-per-minute). Nil = unlimited.
	RateLimitTPM *int `json:"rate_limit_tpm,omitempty"`
}

// AuditFilter scopes ListAudit.
type AuditFilter struct {
	TenantID string
	Action   string
	From     *time.Time
	To       *time.Time
	Limit    int
	Offset   int
}

// ListUsersOpts is the structured form of ListUsers's filters.
type ListUsersOpts struct {
	TenantID string
	Search   string
	Limit    int
	Offset   int
}

// ListMembershipsOpts is the structured form of ListMemberships's filters.
type ListMembershipsOpts struct {
	TenantID string
	UserID   string
	Limit    int
	Offset   int
}

// ListKeysOpts is the structured form of ListKeys's filters.
type ListKeysOpts struct {
	TenantID       string
	IncludeRevoked bool
	Limit          int
	Offset         int
}

// ListTenantsOpts filters ListTenants. IncludeDeleted defaults false so
// soft-deleted tenants stay hidden unless callers opt in.
type ListTenantsOpts struct {
	IncludeDeleted bool
	Limit          int
	Offset         int
}

// ─── Compile-time check ──────────────────────────────────────────────────

// Ensure context is referenced — the Manager methods take a context as
// their first parameter. Keeping the import is defensive against future
// refactors that move methods to other files.
var _ = context.Background
