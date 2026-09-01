// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Agent-Field/backai/services/runtime/internal/tenantctx"
)

// Querier is the minimal DB surface the CRUD handlers need. *pgxpool.Pool
// satisfies it. Kept narrow so tests can inject a fake and so nothing but
// tenant-scoped queries can be issued through it. Every connection the
// pool hands out has app.tenant_id bound from the request context (see
// db.Open's PrepareConn hook), so RLS is the backstop even if a builder
// ever forgot the explicit tenant_id filter — but the builders always
// include it too (defence in depth).
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

const (
	defaultListLimit = 50
	maxListLimit     = 200
	maxBodyBytes     = 1 << 20
)

// resourceHandler serves tenant-scoped CRUD for one resource of one
// module. It is constructed by Manager.mount and closed over per route.
type resourceHandler struct {
	moduleID  string
	resource  Resource
	table     string
	fieldByNm map[string]Field
	db        Querier
	resp      Responder
}

func newResourceHandler(moduleID string, res Resource, db Querier, resp Responder) *resourceHandler {
	byName := make(map[string]Field, len(res.Fields))
	for _, f := range res.Fields {
		byName[f.Name] = f
	}
	return &resourceHandler{
		moduleID:  moduleID,
		resource:  res,
		table:     TableName(moduleID, res.Name),
		fieldByNm: byName,
		db:        db,
		resp:      resp,
	}
}

// tenantOrFail returns the resolved tenant id, or writes 401 and returns
// false. The tenant is ALWAYS taken from the resolver-populated context,
// never from the request body or query string.
func (h *resourceHandler) tenantOrFail(ctx context.Context, w http.ResponseWriter) (string, bool) {
	tenant := strings.TrimSpace(tenantctx.TenantID(ctx))
	if tenant == "" {
		h.resp.writeError(w, http.StatusUnauthorized, "TENANT_REQUIRED",
			"tenant context is required", nil)
		return "", false
	}
	return tenant, true
}

func (h *resourceHandler) dbReady(w http.ResponseWriter) bool {
	if h.db == nil {
		h.resp.writeError(w, http.StatusServiceUnavailable, "MODULE_DB_NOT_CONFIGURED",
			"workload module storage is not configured on this runtime", nil)
		return false
	}
	return true
}

func (h *resourceHandler) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.dbReady(w) {
		return
	}
	tenant, ok := h.tenantOrFail(ctx, w)
	if !ok {
		return
	}
	limit, offset := parsePaging(r.URL.Query().Get("limit"), r.URL.Query().Get("offset"))

	fields := h.resource.FieldNames()
	rows, err := h.db.Query(ctx, buildListSQL(h.table, fields), tenant, limit, offset)
	if err != nil {
		h.dbError(w, err)
		return
	}
	items, err := scanRows(rows, selectColumnNames(fields), h.fieldByNm)
	if err != nil {
		h.resp.writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
		return
	}

	var total int
	countRows, err := h.db.Query(ctx, buildCountSQL(h.table), tenant)
	if err == nil {
		if countRows.Next() {
			_ = countRows.Scan(&total)
		}
		countRows.Close()
	}

	h.resp.writeJSON(w, http.StatusOK, map[string]any{
		"items":    items,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
		"has_more": offset+len(items) < total,
	})
}

func (h *resourceHandler) create(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.dbReady(w) {
		return
	}
	tenant, ok := h.tenantOrFail(ctx, w)
	if !ok {
		return
	}
	body, ok := h.decodeBody(w, r)
	if !ok {
		return
	}

	fields := h.resource.FieldNames()
	args := make([]any, 0, len(fields)+1)
	args = append(args, tenant) // $1 is ALWAYS the resolved tenant.
	for _, f := range h.resource.Fields {
		val, err := h.valueForCreate(f, body)
		if err != nil {
			h.resp.writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", err.Error(), nil)
			return
		}
		args = append(args, val)
	}

	rows, err := h.db.Query(ctx, buildInsertSQL(h.table, fields), args...)
	if err != nil {
		h.dbError(w, err)
		return
	}
	item, err := scanOne(rows, selectColumnNames(fields), h.fieldByNm)
	if err != nil {
		h.resp.writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
		return
	}
	h.resp.writeJSON(w, http.StatusCreated, item)
}

func (h *resourceHandler) get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.dbReady(w) {
		return
	}
	tenant, ok := h.tenantOrFail(ctx, w)
	if !ok {
		return
	}
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	fields := h.resource.FieldNames()
	rows, err := h.db.Query(ctx, buildGetSQL(h.table, fields), tenant, id)
	if err != nil {
		h.dbError(w, err)
		return
	}
	item, err := scanOne(rows, selectColumnNames(fields), h.fieldByNm)
	if errors.Is(err, errNoRows) {
		h.notFound(w)
		return
	}
	if err != nil {
		h.resp.writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
		return
	}
	h.resp.writeJSON(w, http.StatusOK, item)
}

func (h *resourceHandler) update(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.dbReady(w) {
		return
	}
	tenant, ok := h.tenantOrFail(ctx, w)
	if !ok {
		return
	}
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	body, ok := h.decodeBody(w, r)
	if !ok {
		return
	}

	// Only fields present in the body are updated (PATCH semantics), in
	// manifest order for a stable placeholder sequence.
	present := make([]string, 0, len(h.resource.Fields))
	args := make([]any, 0, len(h.resource.Fields)+2)
	args = append(args, tenant) // $1
	for _, f := range h.resource.Fields {
		raw, in := body[f.Name]
		if !in {
			continue
		}
		val, err := coerceValue(f.Type, raw)
		if err != nil {
			h.resp.writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
				"field "+f.Name+": "+err.Error(), nil)
			return
		}
		if val == nil && f.Required {
			h.resp.writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
				"field "+f.Name+" is required and cannot be null", nil)
			return
		}
		present = append(present, f.Name)
		args = append(args, val)
	}
	if len(present) == 0 {
		h.resp.writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
			"no updatable fields supplied", nil)
		return
	}
	args = append(args, id) // trailing id placeholder

	fields := h.resource.FieldNames()
	rows, err := h.db.Query(ctx, buildUpdateSQL(h.table, present, fields), args...)
	if err != nil {
		h.dbError(w, err)
		return
	}
	item, err := scanOne(rows, selectColumnNames(fields), h.fieldByNm)
	if errors.Is(err, errNoRows) {
		h.notFound(w)
		return
	}
	if err != nil {
		h.resp.writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
		return
	}
	h.resp.writeJSON(w, http.StatusOK, item)
}

func (h *resourceHandler) delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !h.dbReady(w) {
		return
	}
	tenant, ok := h.tenantOrFail(ctx, w)
	if !ok {
		return
	}
	id, ok := h.idParam(w, r)
	if !ok {
		return
	}
	tag, err := h.db.Exec(ctx, buildDeleteSQL(h.table), tenant, id)
	if err != nil {
		h.dbError(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		h.notFound(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// valueForCreate resolves the argument to bind for field f on create:
// the request value (coerced), else the manifest default (coerced), else
// nil — unless the field is required, which is a 400.
func (h *resourceHandler) valueForCreate(f Field, body map[string]any) (any, error) {
	if raw, ok := body[f.Name]; ok {
		val, err := coerceValue(f.Type, raw)
		if err != nil {
			return nil, errors.New("field " + f.Name + ": " + err.Error())
		}
		if val == nil && f.Required {
			return nil, errors.New("field " + f.Name + " is required")
		}
		return val, nil
	}
	if f.Default != nil {
		return coerceValue(f.Type, f.Default)
	}
	if f.Required {
		return nil, errors.New("field " + f.Name + " is required")
	}
	return nil, nil
}

func (h *resourceHandler) decodeBody(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		h.resp.writeError(w, http.StatusBadRequest, "BAD_REQUEST", "could not read body", nil)
		return nil, false
	}
	body := map[string]any{}
	if len(strings.TrimSpace(string(raw))) > 0 {
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.UseNumber()
		if err := dec.Decode(&body); err != nil {
			h.resp.writeError(w, http.StatusBadRequest, "VALIDATION_FAILED",
				"invalid JSON body: "+err.Error(), nil)
			return nil, false
		}
	}
	return body, true
}

func (h *resourceHandler) idParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		h.resp.writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "id is required", nil)
		return "", false
	}
	return id, true
}

func (h *resourceHandler) notFound(w http.ResponseWriter) {
	h.resp.writeError(w, http.StatusNotFound, "NOT_FOUND", "resource not found", nil)
}

func (h *resourceHandler) dbError(w http.ResponseWriter, err error) {
	h.resp.writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
}

func parsePaging(limitStr, offsetStr string) (limit, offset int) {
	limit = defaultListLimit
	if v, err := strconv.Atoi(strings.TrimSpace(limitStr)); err == nil {
		limit = v
	}
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	if v, err := strconv.Atoi(strings.TrimSpace(offsetStr)); err == nil && v > 0 {
		offset = v
	}
	return limit, offset
}
