// SPDX-License-Identifier: Apache-2.0

package rbac

import "testing"

func TestDefaultPolicyOwnerCanAdminEverything(t *testing.T) {
	e := NewDefault()
	for _, tc := range []struct {
		resource string
		action   string
	}{
		{ResourceAdminTenants, ActionRead},
		{ResourceAdminTenants, ActionWrite},
		{ResourceAdminTenants, ActionDelete},
		{ResourceAdminMemberships, ActionDelete},
		{ResourceAdminKeys, ActionDelete},
		{ResourceAdminBudgets, ActionWrite},
		{ResourceAdminAudit, ActionRead},
		{ResourceAdminPrivacy, ActionDelete},
	} {
		if !e.Allowed(RoleOwner, tc.resource, tc.action) {
			t.Fatalf("owner should allow %s %s", tc.action, tc.resource)
		}
	}
}

func TestDefaultPolicyAdminCanReadAndWriteButNotDelete(t *testing.T) {
	e := NewDefault()
	for _, resource := range []string{
		ResourceAdminTenants,
		ResourceAdminUsers,
		ResourceAdminMemberships,
		ResourceAdminKeys,
		ResourceAdminBudgets,
		ResourceAdminAudit,
		ResourceAdminPrivacy,
	} {
		if !e.Allowed(RoleAdmin, resource, ActionRead) {
			t.Fatalf("admin should read %s", resource)
		}
	}

	for _, resource := range []string{
		ResourceAdminTenants,
		ResourceAdminMemberships,
		ResourceAdminKeys,
		ResourceAdminBudgets,
	} {
		if !e.Allowed(RoleAdmin, resource, ActionWrite) {
			t.Fatalf("admin should write %s", resource)
		}
	}

	for _, resource := range []string{
		ResourceAdminTenants,
		ResourceAdminMemberships,
		ResourceAdminKeys,
		ResourceAdminBudgets,
		ResourceAdminPrivacy,
	} {
		if e.Allowed(RoleAdmin, resource, ActionDelete) {
			t.Fatalf("admin should not delete %s", resource)
		}
	}
}

func TestDefaultPolicyUnknownRoleDenied(t *testing.T) {
	if NewDefault().Allowed("viewer", ResourceAdminTenants, ActionRead) {
		t.Fatal("unknown operator role should be denied")
	}
}
