// SPDX-License-Identifier: Apache-2.0

// customer_lifecycle.go — CUSTOMER-FACING tenant self-management surface.
//
// These routes are the tenant-owner/admin counterpart to the operator-only
// /api/v1/admin/* surface: a signed-in customer manages THEIR OWN tenant —
// API keys, member invitations, ownership, deletion, and their audit trail.
//
// Two route families:
//
//   - /api/v1/me/*          — go THROUGH the tenant resolver (tenant + user
//     bound from the session). Gated by the caller's membership role via the
//     tenantrole capability matrix (viewer/member can't mint keys, billing
//     role sees billing only, tenant.manage is owner-only).
//   - /api/v1/invitations/accept — public-prefixed (bypasses the resolver)
//     because the invitee has NO membership yet and would otherwise be 403'd.
//     Auth is the invite token (capability) plus a session that identifies
//     WHO is accepting; the email must match the invitation.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/audit"
	"github.com/Agent-Field/backai/services/runtime/internal/invitations"
	"github.com/Agent-Field/backai/services/runtime/internal/notifications"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
	"github.com/Agent-Field/backai/services/runtime/internal/tenancy"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
	"github.com/Agent-Field/backai/services/runtime/internal/tenantrole"
)

// registerLifecycleRoutes wires the customer self-management surface. Kept to
// a single call from registerRoutes so the shared server.go stays minimal.
func (s *Server) registerLifecycleRoutes() {
	// Session-principal, role-gated (resolver binds tenant + user).
	s.mux.HandleFunc("GET /api/v1/me/keys", s.handleMeListKeys)
	s.mux.HandleFunc("POST /api/v1/me/keys", s.handleMeCreateKey)
	s.mux.HandleFunc("DELETE /api/v1/me/keys/{id}", s.handleMeRevokeKey)
	s.mux.HandleFunc("GET /api/v1/me/invitations", s.handleMeListInvitations)
	s.mux.HandleFunc("POST /api/v1/me/invitations", s.handleMeCreateInvitation)
	s.mux.HandleFunc("DELETE /api/v1/me/invitations/{id}", s.handleMeRevokeInvitation)
	s.mux.HandleFunc("GET /api/v1/me/sessions", s.handleMeListSessions)
	s.mux.HandleFunc("DELETE /api/v1/me/sessions/{id}", s.handleMeRevokeSession)
	s.mux.HandleFunc("GET /api/v1/me/audit", s.handleMeAudit)
	s.mux.HandleFunc("POST /api/v1/me/transfer-ownership", s.handleMeTransferOwnership)
	s.mux.HandleFunc("DELETE /api/v1/me/tenant", s.handleMeDeleteTenant)

	// Token-based accept (bypasses the resolver via publicPrefixes).
	s.mux.HandleFunc("POST /api/v1/invitations/accept", s.handleAcceptInvitation)

	b := s.openapi
	b.Register("GET", "/api/v1/me/keys", openapi.RouteMeta{Summary: "List the caller tenant's API keys", Tags: []string{"lifecycle"}})
	b.Register("POST", "/api/v1/me/keys", openapi.RouteMeta{Summary: "Mint an API key for the caller's tenant (admin+)", Tags: []string{"lifecycle"}})
	b.Register("DELETE", "/api/v1/me/keys/{id}", openapi.RouteMeta{Summary: "Revoke an API key in the caller's tenant", Tags: []string{"lifecycle"}})
	b.Register("GET", "/api/v1/me/invitations", openapi.RouteMeta{Summary: "List the caller tenant's invitations", Tags: []string{"lifecycle"}})
	b.Register("POST", "/api/v1/me/invitations", openapi.RouteMeta{Summary: "Invite a member to the caller's tenant (admin+)", Tags: []string{"lifecycle"}})
	b.Register("DELETE", "/api/v1/me/invitations/{id}", openapi.RouteMeta{Summary: "Revoke a pending invitation", Tags: []string{"lifecycle"}})
	b.Register("GET", "/api/v1/me/sessions", openapi.RouteMeta{Summary: "List the caller's own active sessions", Tags: []string{"lifecycle"}})
	b.Register("DELETE", "/api/v1/me/sessions/{id}", openapi.RouteMeta{Summary: "Revoke one of the caller's own sessions", Tags: []string{"lifecycle"}})
	b.Register("GET", "/api/v1/me/audit", openapi.RouteMeta{Summary: "Read the caller tenant's audit trail", Tags: []string{"lifecycle"}})
	b.Register("POST", "/api/v1/me/transfer-ownership", openapi.RouteMeta{Summary: "Transfer tenant ownership (owner only)", Tags: []string{"lifecycle"}})
	b.Register("DELETE", "/api/v1/me/tenant", openapi.RouteMeta{Summary: "Soft-delete the caller's tenant (owner only)", Tags: []string{"lifecycle"}})
	b.Register("POST", "/api/v1/invitations/accept", openapi.RouteMeta{Summary: "Accept a membership invitation by token", Tags: []string{"lifecycle"}})
}

// invitationStore builds a tenant-scoped invitations store on the serving
// pool (the one whose PrepareConn hook binds app.tenant_id for FORCE-RLS).
// Returns nil when there's no DB.
func (s *Server) invitationStore() *invitations.Store {
	if s.db == nil || s.db.Pool == nil {
		return nil
	}
	return invitations.NewStore(s.db.Pool, s.log)
}

// requireTenantCap gates a /api/v1/me/* handler on the caller's membership
// role. Returns the resolved (tenantID, userID) and ok=true only when the
// role holds `cap`. On any failure it has already written the response.
//
// Personal mode is a single-user app with no login and no RBAC — every
// capability is granted (the sole user owns the default tenant), mirroring
// operatorAccessDenied's personal-mode short-circuit.
func (s *Server) requireTenantCap(w http.ResponseWriter, r *http.Request, capab tenantrole.Capability) (tenantID, userID string, ok bool) {
	tenantID = strings.TrimSpace(tenantctx.TenantID(r.Context()))
	if tenantID == "" {
		writeError(w, http.StatusUnauthorized, "TENANT_REQUIRED",
			"a tenant session or API key is required", nil)
		return "", "", false
	}
	if s.personalMode() {
		return tenantID, strings.TrimSpace(tenantctx.UserID(r.Context())), true
	}
	userID = strings.TrimSpace(tenantctx.UserID(r.Context()))
	if userID == "" {
		// These routes manage a tenant on behalf of a human; an API key is
		// not a person and can't be attributed a membership role.
		writeError(w, http.StatusUnauthorized, "SESSION_REQUIRED",
			"this endpoint requires a signed-in user session", nil)
		return "", "", false
	}
	if s.tenancy == nil {
		writeError(w, http.StatusServiceUnavailable, "TENANCY_NOT_CONFIGURED",
			"multi-tenancy is not configured on this runtime", nil)
		return "", "", false
	}
	role, err := s.tenancy.MembershipRole(r.Context(), tenantID, userID)
	if err != nil {
		writeError(w, http.StatusForbidden, "NO_TENANT_MEMBERSHIP",
			"you are not a member of this tenant", nil)
		return "", "", false
	}
	if !tenantrole.CanString(role, capab) {
		writeError(w, http.StatusForbidden, "RBAC_DENIED",
			"your role is not allowed to perform this action", nil)
		return "", "", false
	}
	return tenantID, userID, true
}

// ─── API keys ─────────────────────────────────────────────────────────────

func (s *Server) handleMeListKeys(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := s.requireTenantCap(w, r, tenantrole.CapKeysRead)
	if !ok {
		return
	}
	if s.tenancy == nil {
		writeJSON(w, http.StatusOK, map[string]any{"keys": []tenancy.APIKey{}})
		return
	}
	keys, err := s.tenancy.ListKeys(r.Context(), tenancy.ListKeysOpts{TenantID: tenantID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "could not list keys", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

type createMeKeyInput struct {
	Name               string   `json:"name"`
	ServiceAccountName string   `json:"service_account_name"`
	Scopes             []string `json:"scopes"`
	ExpiresAt          *string  `json:"expires_at"`
}

func (s *Server) handleMeCreateKey(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, ok := s.requireTenantCap(w, r, tenantrole.CapKeysManage)
	if !ok {
		return
	}
	in, ok := decodeLifecycleBody[createMeKeyInput](w, r)
	if !ok {
		return
	}
	if in.Scopes == nil {
		in.Scopes = []string{}
	}
	// Operator-plane scopes are owner-only and never mintable through this
	// customer surface — reuse the same guard the admin key route uses.
	if s.operatorPlaneScopeDenied(w, r, in.Scopes) {
		return
	}
	issueIn := tenancy.IssueAPIKeyInput{
		TenantID:           tenantID,
		Name:               strings.TrimSpace(in.Name),
		ServiceAccountName: strings.TrimSpace(in.ServiceAccountName),
		Scopes:             in.Scopes,
		CreatedBy:          userID,
	}
	if in.ExpiresAt != nil && strings.TrimSpace(*in.ExpiresAt) != "" {
		exp, perr := time.Parse(time.RFC3339, strings.TrimSpace(*in.ExpiresAt))
		if perr != nil {
			writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
				"expires_at must be an RFC3339 timestamp", nil)
			return
		}
		issueIn.ExpiresAt = &exp
	}
	if s.tenancy == nil {
		writeError(w, http.StatusServiceUnavailable, "TENANCY_NOT_CONFIGURED",
			"multi-tenancy is not configured on this runtime", nil)
		return
	}
	issued, err := s.tenancy.IssueKey(r.Context(), issueIn)
	if err != nil {
		writeTenancyError(w, err)
		return
	}
	s.audit.Write(r.Context(), r, auditEventKeyCreate(tenantID, issued.ID))
	writeJSON(w, http.StatusCreated, issued)
}

func (s *Server) handleMeRevokeKey(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := s.requireTenantCap(w, r, tenantrole.CapKeysManage)
	if !ok {
		return
	}
	id, ok := validUUIDParam(w, r.PathValue("id"))
	if !ok {
		return
	}
	if s.tenancy == nil {
		writeError(w, http.StatusServiceUnavailable, "TENANCY_NOT_CONFIGURED",
			"multi-tenancy is not configured on this runtime", nil)
		return
	}
	// Ownership guard: RevokeKey is keyed by id alone, so confirm the key
	// belongs to the caller's tenant before touching it — otherwise a caller
	// could revoke another tenant's key by guessing its uuid.
	if !s.keyBelongsToTenant(r.Context(), tenantID, id) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "no such key in this tenant", nil)
		return
	}
	if err := s.tenancy.RevokeKey(r.Context(), id); err != nil {
		writeTenancyError(w, err)
		return
	}
	s.audit.Write(r.Context(), r, auditEventKeyRevoke(tenantID, id))
	writeJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

// keyBelongsToTenant reports whether key id is one of tenantID's keys.
func (s *Server) keyBelongsToTenant(ctx context.Context, tenantID, id string) bool {
	keys, err := s.tenancy.ListKeys(ctx, tenancy.ListKeysOpts{TenantID: tenantID, IncludeRevoked: true})
	if err != nil {
		return false
	}
	for _, k := range keys {
		if k.ID == id {
			return true
		}
	}
	return false
}

// ─── Invitations ──────────────────────────────────────────────────────────

func (s *Server) handleMeListInvitations(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := s.requireTenantCap(w, r, tenantrole.CapMembersManage)
	if !ok {
		return
	}
	store := s.invitationStore()
	if store == nil {
		writeJSON(w, http.StatusOK, map[string]any{"invitations": []invitations.Invitation{}})
		return
	}
	list, err := store.List(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "could not list invitations", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invitations": list})
}

type createInvitationInput struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

func (s *Server) handleMeCreateInvitation(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, ok := s.requireTenantCap(w, r, tenantrole.CapMembersManage)
	if !ok {
		return
	}
	in, ok := decodeLifecycleBody[createInvitationInput](w, r)
	if !ok {
		return
	}
	email := strings.TrimSpace(in.Email)
	role := strings.TrimSpace(in.Role)
	if email == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "email is required", nil)
		return
	}
	if !tenantrole.IsValidRole(role) {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"role must be one of owner, admin, member, billing, viewer", nil)
		return
	}
	store := s.invitationStore()
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "LIFECYCLE_NOT_CONFIGURED",
			"invitations are not configured on this runtime", nil)
		return
	}
	inv, token, err := store.Create(r.Context(), invitations.CreateInput{
		TenantID:  tenantID,
		Email:     email,
		Role:      role,
		InvitedBy: userID,
	})
	if err != nil {
		writeInvitationError(w, err)
		return
	}
	s.sendInvitationEmail(r.Context(), tenantID, inv, token)
	s.audit.Write(r.Context(), r, auditEventInvitationCreate(tenantID, inv.ID, email))
	// The one-time accept token is returned so the inviter can share a link
	// even if email delivery is unavailable; it is never persisted plaintext.
	writeJSON(w, http.StatusCreated, map[string]any{"invitation": inv, "token": token})
}

func (s *Server) handleMeRevokeInvitation(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := s.requireTenantCap(w, r, tenantrole.CapMembersManage)
	if !ok {
		return
	}
	id, ok := validUUIDParam(w, r.PathValue("id"))
	if !ok {
		return
	}
	store := s.invitationStore()
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "LIFECYCLE_NOT_CONFIGURED",
			"invitations are not configured on this runtime", nil)
		return
	}
	if err := store.Revoke(r.Context(), tenantID, id); err != nil {
		writeInvitationError(w, err)
		return
	}
	s.audit.Write(r.Context(), r, auditEventInvitationRevoke(tenantID, id))
	writeJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

type acceptInvitationInput struct {
	Token string `json:"token"`
}

func (s *Server) handleAcceptInvitation(w http.ResponseWriter, r *http.Request) {
	// This route bypasses the tenant resolver, so resolve the session user
	// directly — the invitee has no membership yet.
	userID, email, err := s.resolveSessionUser(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "SESSION_REQUIRED",
			"sign in to accept an invitation", nil)
		return
	}
	in, ok := decodeLifecycleBody[acceptInvitationInput](w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(in.Token) == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "token is required", nil)
		return
	}
	store := s.invitationStore()
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "LIFECYCLE_NOT_CONFIGURED",
			"invitations are not configured on this runtime", nil)
		return
	}
	inv, err := store.AcceptByToken(r.Context(), in.Token, userID, email)
	if err != nil {
		writeInvitationError(w, err)
		return
	}
	// Grant the membership the invitation carries. AddMembership is an
	// idempotent upsert, so a retried accept converges.
	if s.tenancy != nil {
		if _, aerr := s.tenancy.AddMembership(r.Context(), inv.TenantID, userID, inv.Role); aerr != nil {
			s.log.Warn("lifecycle: invitation accepted but membership grant failed",
				"invitation", inv.ID, "tenant", inv.TenantID, "user", userID, "error", aerr)
			writeError(w, http.StatusInternalServerError, "MEMBERSHIP_GRANT_FAILED",
				"invitation accepted but membership could not be created", nil)
			return
		}
	}
	s.audit.Write(r.Context(), r, auditEventInvitationAccept(inv.TenantID, inv.ID, userID))
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": inv.TenantID,
		"role":      inv.Role,
	})
}

// ─── Ownership + deletion ─────────────────────────────────────────────────

type transferOwnershipInput struct {
	NewOwnerUserID string `json:"new_owner_user_id"`
}

func (s *Server) handleMeTransferOwnership(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := s.requireTenantCap(w, r, tenantrole.CapTenantManage)
	if !ok {
		return
	}
	in, ok := decodeLifecycleBody[transferOwnershipInput](w, r)
	if !ok {
		return
	}
	newOwner, ok := validUUIDParam(w, in.NewOwnerUserID)
	if !ok {
		return
	}
	if s.tenancy == nil {
		writeError(w, http.StatusServiceUnavailable, "TENANCY_NOT_CONFIGURED",
			"multi-tenancy is not configured on this runtime", nil)
		return
	}
	if err := s.tenancy.TransferOwnership(r.Context(), tenantID, newOwner); err != nil {
		writeTenancyError(w, err)
		return
	}
	s.audit.Write(r.Context(), r, auditEventOwnershipTransfer(tenantID, newOwner))
	writeJSON(w, http.StatusOK, map[string]bool{"transferred": true})
}

func (s *Server) handleMeDeleteTenant(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := s.requireTenantCap(w, r, tenantrole.CapTenantManage)
	if !ok {
		return
	}
	if s.tenancy == nil {
		writeError(w, http.StatusServiceUnavailable, "TENANCY_NOT_CONFIGURED",
			"multi-tenancy is not configured on this runtime", nil)
		return
	}
	if err := s.tenancy.DeleteTenant(r.Context(), tenantID); err != nil {
		writeTenancyError(w, err)
		return
	}
	s.audit.Write(r.Context(), r, auditEventTenantDelete(tenantID))
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ─── Sessions ─────────────────────────────────────────────────────────────

type meSessionWire struct {
	ID        string  `json:"id"`
	CreatedAt string  `json:"created_at"`
	ExpiresAt string  `json:"expires_at"`
	IPAddress *string `json:"ip_address"`
	UserAgent *string `json:"user_agent"`
	Current   bool    `json:"current"`
}

func (s *Server) handleMeListSessions(w http.ResponseWriter, r *http.Request) {
	_, userID, ok := s.requireTenantCap(w, r, tenantrole.CapSelfManage)
	if !ok {
		return
	}
	// No DB, or personal mode (no better-auth user) → nothing to list.
	if s.db == nil || s.db.Pool == nil || strings.TrimSpace(userID) == "" {
		writeJSON(w, http.StatusOK, map[string]any{"sessions": []meSessionWire{}})
		return
	}
	currentToken := betterAuthSessionToken(r)
	rows, err := s.db.Pool.Query(r.Context(), `
		select s."id", s."createdAt", s."expiresAt", s."ipAddress", s."userAgent",
		       (s."token" = $2) as current
		  from "session" s
		  join "user" u on u."id" = s."userId"
		  join suite_users su on lower(su.email) = lower(u."email")
		 where su.id = $1 and s."expiresAt" > now()
		 order by s."createdAt" desc
	`, userID, currentToken)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "could not list sessions", nil)
		return
	}
	defer rows.Close()
	out := []meSessionWire{}
	for rows.Next() {
		var (
			sw                   meSessionWire
			createdAt, expiresAt time.Time
		)
		if err := rows.Scan(&sw.ID, &createdAt, &expiresAt, &sw.IPAddress, &sw.UserAgent, &sw.Current); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL", "could not read sessions", nil)
			return
		}
		sw.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		sw.ExpiresAt = expiresAt.UTC().Format(time.RFC3339Nano)
		out = append(out, sw)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) handleMeRevokeSession(w http.ResponseWriter, r *http.Request) {
	tenantID, userID, ok := s.requireTenantCap(w, r, tenantrole.CapSelfManage)
	if !ok {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "id is required", nil)
		return
	}
	if s.db == nil || s.db.Pool == nil || strings.TrimSpace(userID) == "" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "no such session", nil)
		return
	}
	// Scope the delete to the caller's own sessions (join through email) so a
	// caller can only revoke sessions that belong to them.
	tag, err := s.db.Pool.Exec(r.Context(), `
		delete from "session"
		 where "id" = $1
		   and "userId" in (
		     select u."id" from "user" u
		     join suite_users su on lower(su.email) = lower(u."email")
		     where su.id = $2
		   )
	`, id, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "could not revoke session", nil)
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "no such session", nil)
		return
	}
	s.audit.Write(r.Context(), r, audit.Event{Action: "session.revoke", ResourceType: "session", ResourceID: id,
		Metadata: map[string]any{"tenant_id": tenantID, "user_id": userID}})
	writeJSON(w, http.StatusOK, map[string]bool{"revoked": true})
}

// ─── Audit trail ──────────────────────────────────────────────────────────

func (s *Server) handleMeAudit(w http.ResponseWriter, r *http.Request) {
	tenantID, _, ok := s.requireTenantCap(w, r, tenantrole.CapAuditRead)
	if !ok {
		return
	}
	if s.tenancy == nil {
		writeJSON(w, http.StatusOK, map[string]any{"entries": []tenancy.AuditEntry{}, "total": 0, "has_more": false})
		return
	}
	page, err := s.tenancy.ListAudit(r.Context(), tenancy.AuditFilter{TenantID: tenantID, Limit: 100})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", "could not read audit trail", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries":  page.Entries,
		"total":    page.Total,
		"has_more": page.HasMore,
	})
}

// ─── Helpers ──────────────────────────────────────────────────────────────

// resolveSessionUser resolves a better-auth session to the suite_users id +
// email WITHOUT requiring a tenant membership (unlike resolveSession). Used
// by the accept flow, where the invitee has no membership yet.
func (s *Server) resolveSessionUser(ctx context.Context, r *http.Request) (userID, email string, err error) {
	if s.db == nil || s.db.Pool == nil {
		return "", "", errNoSession
	}
	token := betterAuthSessionToken(r)
	if token == "" {
		return "", "", errNoSession
	}
	err = s.db.Pool.QueryRow(ctx, `
		select su.id::text, u."email"
		from "session" s
		join "user" u on u."id" = s."userId"
		join suite_users su on lower(su.email) = lower(u."email") and su.deleted_at is null
		where s."token" = $1 and s."expiresAt" > now()
		limit 1
	`, token).Scan(&userID, &email)
	if err != nil {
		return "", "", errNoSession
	}
	return userID, email, nil
}

// sendInvitationEmail dispatches the invite through the notifications
// subsystem. Best-effort: a failure here never blocks issuing the invitation
// (the inviter still gets the one-time token to share manually).
func (s *Server) sendInvitationEmail(ctx context.Context, tenantID string, inv invitations.Invitation, token string) {
	if s.notifications == nil {
		return
	}
	_, err := s.notifications.Send(ctx, notificationsSendInvitation(tenantID, inv, token))
	if err != nil {
		s.log.Warn("lifecycle: invitation email send failed",
			"invitation", inv.ID, "tenant", tenantID, "error", err)
	}
}

// writeInvitationError maps invitation sentinels onto the response envelope.
func writeInvitationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, invitations.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "invitation not found", nil)
	case errors.Is(err, invitations.ErrExpired):
		writeError(w, http.StatusGone, "INVITATION_EXPIRED", "invitation has expired", nil)
	case errors.Is(err, invitations.ErrNotPending):
		writeError(w, http.StatusConflict, "INVITATION_NOT_PENDING",
			"invitation has already been accepted or revoked", nil)
	case errors.Is(err, invitations.ErrEmailMismatch):
		writeError(w, http.StatusForbidden, "INVITATION_EMAIL_MISMATCH",
			"this invitation was issued to a different email", nil)
	case errors.Is(err, invitations.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL", "operation failed", nil)
	}
}

// decodeLifecycleBody reads + JSON-decodes a small request body into T.
func decodeLifecycleBody[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var out T
	raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return out, false
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return out, true // empty body is allowed; fields default
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "invalid JSON body: "+err.Error(), nil)
		return out, false
	}
	return out, true
}

// ─── Audit + notification builders ────────────────────────────────────────

func auditEventKeyCreate(tenantID, keyID string) audit.Event {
	return audit.Event{Action: "api_key.create", ResourceType: "api_key", ResourceID: keyID,
		Metadata: map[string]any{"tenant_id": tenantID, "source": "customer_self_service"}}
}

func auditEventKeyRevoke(tenantID, keyID string) audit.Event {
	return audit.Event{Action: "api_key.revoke", ResourceType: "api_key", ResourceID: keyID,
		Metadata: map[string]any{"tenant_id": tenantID, "source": "customer_self_service"}}
}

func auditEventInvitationCreate(tenantID, invID, email string) audit.Event {
	return audit.Event{Action: "invitation.create", ResourceType: "invitation", ResourceID: invID,
		Metadata: map[string]any{"tenant_id": tenantID, "email": email}}
}

func auditEventInvitationRevoke(tenantID, invID string) audit.Event {
	return audit.Event{Action: "invitation.revoke", ResourceType: "invitation", ResourceID: invID,
		Metadata: map[string]any{"tenant_id": tenantID}}
}

func auditEventInvitationAccept(tenantID, invID, userID string) audit.Event {
	return audit.Event{Action: "invitation.accept", ResourceType: "invitation", ResourceID: invID,
		Metadata: map[string]any{"tenant_id": tenantID, "user_id": userID}}
}

func auditEventOwnershipTransfer(tenantID, newOwner string) audit.Event {
	return audit.Event{Action: "tenant.transfer_ownership", ResourceType: "tenant", ResourceID: tenantID,
		Metadata: map[string]any{"tenant_id": tenantID, "new_owner_user_id": newOwner}}
}

func auditEventTenantDelete(tenantID string) audit.Event {
	return audit.Event{Action: "tenant.delete", ResourceType: "tenant", ResourceID: tenantID,
		Metadata: map[string]any{"tenant_id": tenantID, "source": "customer_self_service"}}
}

// notificationsSendInvitation builds the invite email payload. The accept
// token rides in the template data so the email can render an accept link.
func notificationsSendInvitation(tenantID string, inv invitations.Invitation, token string) notifications.SendInput {
	return notifications.SendInput{
		TenantID: tenantID,
		Kind:     notifications.KindEmail,
		Template: "tenant_invitation",
		To:       inv.Email,
		Subject:  "You've been invited to a workspace",
		Data: map[string]any{
			"tenant_id":     tenantID,
			"role":          inv.Role,
			"accept_token":  token,
			"invitation_id": inv.ID,
			"expires_at":    inv.ExpiresAt.Format(time.RFC3339),
		},
	}
}
