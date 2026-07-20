// SPDX-License-Identifier: Apache-2.0

package tenantrole

import "testing"

// Contract: the role matrix must enforce these caller-observable rules on a
// tenant-management surface. Each case is written from intent, not from the
// table's shape.
func TestCan_Matrix(t *testing.T) {
	cases := []struct {
		name string
		role Role
		cap  Capability
		want bool
	}{
		// Owner is all-powerful.
		{"owner deletes tenant", RoleOwner, CapTenantManage, true},
		{"owner mints keys", RoleOwner, CapKeysManage, true},
		{"owner manages billing", RoleOwner, CapBillingManage, true},
		{"owner manages members", RoleOwner, CapMembersManage, true},

		// Admin: everything except destructive tenant ops.
		{"admin cannot delete/transfer tenant", RoleAdmin, CapTenantManage, false},
		{"admin mints keys", RoleAdmin, CapKeysManage, true},
		{"admin lists keys", RoleAdmin, CapKeysRead, true},
		{"admin manages members", RoleAdmin, CapMembersManage, true},
		{"admin manages billing", RoleAdmin, CapBillingManage, true},
		{"admin reads audit", RoleAdmin, CapAuditRead, true},

		// Viewer: read-only, self-service only. The pinned acceptance case.
		{"viewer CANNOT mint keys", RoleViewer, CapKeysManage, false},
		{"viewer cannot read keys", RoleViewer, CapKeysRead, false},
		{"viewer cannot manage members", RoleViewer, CapMembersManage, false},
		{"viewer cannot manage billing", RoleViewer, CapBillingManage, false},
		{"viewer can self-manage", RoleViewer, CapSelfManage, true},

		// Billing role: billing only (+ self). The pinned acceptance case.
		{"billing manages billing", RoleBilling, CapBillingManage, true},
		{"billing reads billing", RoleBilling, CapBillingRead, true},
		{"billing CANNOT mint keys", RoleBilling, CapKeysManage, false},
		{"billing cannot manage members", RoleBilling, CapMembersManage, false},
		{"billing cannot manage tenant", RoleBilling, CapTenantManage, false},
		{"billing cannot read audit", RoleBilling, CapAuditRead, false},
		{"billing can self-manage", RoleBilling, CapSelfManage, true},

		// Member: self only.
		{"member cannot mint keys", RoleMember, CapKeysManage, false},
		{"member cannot manage billing", RoleMember, CapBillingManage, false},
		{"member can self-manage", RoleMember, CapSelfManage, true},

		// Unknown role holds nothing (fail closed).
		{"unknown role denied", Role("root"), CapSelfManage, false},
		{"empty role denied", Role(""), CapKeysManage, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Can(c.role, c.cap); got != c.want {
				t.Errorf("Can(%q, %q) = %v, want %v", c.role, c.cap, got, c.want)
			}
		})
	}
}

// Contract: only the owner may perform tenant.manage — no other role.
func TestCan_TenantManageIsOwnerOnly(t *testing.T) {
	for _, r := range Roles() {
		want := r == RoleOwner
		if got := Can(r, CapTenantManage); got != want {
			t.Errorf("Can(%q, tenant.manage) = %v, want %v", r, got, want)
		}
	}
}

// Contract: CanString bridges the raw suite_memberships.role string.
func TestCanString(t *testing.T) {
	if !CanString("owner", CapKeysManage) {
		t.Error(`CanString("owner", keys.manage) should be true`)
	}
	if CanString("viewer", CapKeysManage) {
		t.Error(`CanString("viewer", keys.manage) should be false`)
	}
}

// Contract: 'billing' is now a valid role; garbage is not.
func TestIsValidRole(t *testing.T) {
	for _, r := range []string{"owner", "admin", "member", "billing", "viewer"} {
		if !IsValidRole(r) {
			t.Errorf("IsValidRole(%q) = false, want true", r)
		}
	}
	for _, r := range []string{"", "root", "superuser", "Owner"} {
		if IsValidRole(r) {
			t.Errorf("IsValidRole(%q) = true, want false", r)
		}
	}
}

func TestRoles_StableOrder(t *testing.T) {
	got := Roles()
	want := []Role{RoleOwner, RoleAdmin, RoleMember, RoleBilling, RoleViewer}
	if len(got) != len(want) {
		t.Fatalf("Roles() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Roles()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
