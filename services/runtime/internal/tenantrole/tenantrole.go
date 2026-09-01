// SPDX-License-Identifier: Apache-2.0

// Package tenantrole is the authorization matrix for a caller's role on
// their OWN tenant. It is deliberately separate from internal/rbac, which
// governs the operator (cross-tenant) console via Casbin.
//
// A tenant membership carries one of five roles (suite_memberships.role):
//
//	owner   — full control, including delete + ownership transfer.
//	admin   — everything except tenant.manage (delete/transfer/settings).
//	member  — a regular user: self-service only.
//	billing — billing access only (checkout, portal, plan) + self-service.
//	viewer  — read-only; self-service only.
//
// The matrix below is a pure lookup with no I/O so the tenant-management
// REST handlers can gate on Can(role, capability) and the whole policy is
// unit-testable without a database. Roles are NOT a linear rank — `billing`
// is orthogonal to the owner>admin>member ladder — so a capability matrix
// (rather than a >= comparison) is the honest model.
package tenantrole

// Role is a suite_memberships.role value.
type Role string

const (
	RoleOwner   Role = "owner"
	RoleAdmin   Role = "admin"
	RoleMember  Role = "member"
	RoleBilling Role = "billing"
	RoleViewer  Role = "viewer"
)

// Capability is a coarse permission the tenant-management surface checks.
type Capability string

const (
	// CapTenantManage covers destructive tenant-level operations: soft-delete,
	// ownership transfer, and tenant settings mutation. Owner only.
	CapTenantManage Capability = "tenant.manage"
	// CapMembersManage covers inviting, revoking, and re-roling members.
	CapMembersManage Capability = "members.manage"
	// CapKeysRead lists the tenant's API keys (metadata only, never secrets).
	CapKeysRead Capability = "keys.read"
	// CapKeysManage mints + revokes API keys (incl. service-account keys).
	CapKeysManage Capability = "keys.manage"
	// CapBillingRead views billing state (plan, subscription status, meters).
	CapBillingRead Capability = "billing.read"
	// CapBillingManage runs checkout / portal / plan changes.
	CapBillingManage Capability = "billing.manage"
	// CapAuditRead reads the tenant-wide audit trail.
	CapAuditRead Capability = "audit.read"
	// CapSelfManage covers self-service on the caller's own principal:
	// listing and revoking one's own sessions. Every member has it.
	CapSelfManage Capability = "self.manage"
)

// matrix maps each role to the capabilities it holds. Absent = denied.
var matrix = map[Role]map[Capability]bool{
	RoleOwner: {
		CapTenantManage:  true,
		CapMembersManage: true,
		CapKeysRead:      true,
		CapKeysManage:    true,
		CapBillingRead:   true,
		CapBillingManage: true,
		CapAuditRead:     true,
		CapSelfManage:    true,
	},
	RoleAdmin: {
		// Everything an owner has EXCEPT tenant.manage (delete/transfer are
		// reserved for the owner).
		CapMembersManage: true,
		CapKeysRead:      true,
		CapKeysManage:    true,
		CapBillingRead:   true,
		CapBillingManage: true,
		CapAuditRead:     true,
		CapSelfManage:    true,
	},
	RoleMember: {
		CapSelfManage: true,
	},
	RoleBilling: {
		// "billing role can access billing only" — plus self-service.
		CapBillingRead:   true,
		CapBillingManage: true,
		CapSelfManage:    true,
	},
	RoleViewer: {
		CapSelfManage: true,
	},
}

// Can reports whether role is permitted to exercise capability on its own
// tenant. Unknown roles hold no capabilities (fail closed).
func Can(role Role, capability Capability) bool {
	caps, ok := matrix[role]
	if !ok {
		return false
	}
	return caps[capability]
}

// CanString is a convenience wrapper for callers holding a bare string role
// (e.g. the value read from suite_memberships).
func CanString(role string, capability Capability) bool {
	return Can(Role(role), capability)
}

// IsValidRole reports whether s names one of the five known roles. Used to
// reject bad input before a membership/invitation write.
func IsValidRole(s string) bool {
	_, ok := matrix[Role(s)]
	return ok
}

// Roles returns the five known roles. Order is stable (owner→viewer) for
// deterministic output in tests and API responses.
func Roles() []Role {
	return []Role{RoleOwner, RoleAdmin, RoleMember, RoleBilling, RoleViewer}
}
