// SPDX-License-Identifier: Apache-2.0

// Package rbac owns AF Stack's role policy. It deliberately protects
// suite/backend resources only; AgentField-owned runs, traces, spans,
// sessions, and memory stay outside this package.
package rbac

import (
	"fmt"
	"net/http"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
)

const (
	RoleOwner = "owner"
	RoleAdmin = "admin"

	ActionRead   = "read"
	ActionWrite  = "write"
	ActionDelete = "delete"

	ResourceAdminTenants     = "admin:tenants"
	ResourceAdminUsers       = "admin:users"
	ResourceAdminMemberships = "admin:memberships"
	ResourceAdminKeys        = "admin:keys"
	ResourceAdminBudgets     = "admin:budgets"
	ResourceAdminAudit       = "admin:audit"
	ResourceAdminPrivacy     = "admin:privacy"
)

const casbinModel = `
[request_definition]
r = sub, obj, act

[policy_definition]
p = sub, obj, act

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && keyMatch2(r.obj, p.obj) && regexMatch(r.act, p.act)
`

// Enforcer wraps Casbin so the rest of the runtime does not depend on
// Casbin request tuple details.
type Enforcer struct {
	casbin *casbin.Enforcer
}

// NewDefault constructs the built-in operator policy:
//   - owner can read/write/delete every admin resource.
//   - admin can read every admin resource and write non-destructive admin
//     resources, but cannot delete tenants, memberships, or API keys.
func NewDefault() *Enforcer {
	m, err := model.NewModelFromString(casbinModel)
	if err != nil {
		panic(fmt.Sprintf("rbac: build model: %v", err))
	}
	e, err := casbin.NewEnforcer(m)
	if err != nil {
		panic(fmt.Sprintf("rbac: build enforcer: %v", err))
	}
	add := func(args ...string) {
		if _, err := e.AddPolicy(args); err != nil {
			panic(fmt.Sprintf("rbac: add policy %v: %v", args, err))
		}
	}

	add(RoleOwner, "admin:*", "read|write|delete")
	add(RoleAdmin, "admin:*", "read")
	add(RoleAdmin, ResourceAdminTenants, "write")
	add(RoleAdmin, ResourceAdminMemberships, "write")
	add(RoleAdmin, ResourceAdminKeys, "write")
	add(RoleAdmin, ResourceAdminBudgets, "write")
	add(RoleAdmin, ResourceAdminPrivacy, "read")

	for _, role := range []string{RoleOwner, RoleAdmin} {
		if _, err := e.AddGroupingPolicy(role, role); err != nil {
			panic(fmt.Sprintf("rbac: add grouping policy %q: %v", role, err))
		}
	}
	return &Enforcer{casbin: e}
}

// Allowed returns true when role can perform action on resource.
func (e *Enforcer) Allowed(role, resource, action string) bool {
	if e == nil || e.casbin == nil {
		return false
	}
	ok, err := e.casbin.Enforce(role, resource, action)
	return err == nil && ok
}

// ActionForMethod maps HTTP verbs onto the coarse RBAC action set.
func ActionForMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return ActionRead
	case http.MethodDelete:
		return ActionDelete
	default:
		return ActionWrite
	}
}
