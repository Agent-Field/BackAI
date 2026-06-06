// Package server — storage.go wires the object-storage REST endpoints that
// power the AF Stack dashboard's Storage tab and any agent that needs file
// I/O.
//
// Every shape returned by these handlers MUST match the zod schemas in
// apps/dashboard/src/lib/api.ts exactly (snake_case field names). The
// dashboard validates responses with safeParse and surfaces SCHEMA_MISMATCH
// errors loudly, so drift here breaks the UI.
//
// When the storage adapter isn't configured (no AF_STACK_S3_ENDPOINT), all
// endpoints return 503 with a STORAGE_NOT_CONFIGURED envelope rather than
// crashing the runtime — Phase 5 keeps the suite usable without MinIO.
//
// OTel: every handler opens a "storage.<route>" server-kind span so traces
// show storage-attributable I/O distinctly from agent forwarding.
package server

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/Agent-Field/backai/services/runtime/internal/hooks"
	"github.com/Agent-Field/backai/services/runtime/internal/storage"
)

// storageTracerName is the OTel tracer name used for storage spans.
const storageTracerName = "af-stack/storage"

// defaultPresignTTL is used when the client doesn't pass ?ttl=.
const defaultPresignTTL = time.Hour

// uploadSizeCap is a defence-in-depth limit on request body size for the
// upload handler. The adapter's Capabilities.MaxObjectSizeBytes is the real
// limit, but very large multipart bodies should be configured per-deployment
// rather than streamed in via the dashboard. Phase 5: 256 MiB.
const uploadSizeCap = int64(256 << 20)

// storageObjectResp matches StorageObjectSchema in apps/dashboard/src/lib/api.ts.
//
// content_type and etag are nullable in the zod schema — we encode "" as
// null with pointer-omit-empty. snake_case names are mandatory.
type storageObjectResp struct {
	Key          string  `json:"key"`
	Size         int64   `json:"size"`
	ContentType  *string `json:"content_type"`
	LastModified string  `json:"last_modified"`
	ETag         *string `json:"etag"`
}

// storageListResp matches StorageListSchema.
type storageListResp struct {
	Objects   []storageObjectResp `json:"objects"`
	Prefix    string              `json:"prefix"`
	NextToken *string             `json:"next_token"`
}

// signedURLResp matches SignedURLSchema.
type signedURLResp struct {
	Key       string `json:"key"`
	URL       string `json:"url"`
	ExpiresAt string `json:"expires_at"`
}

func (s *Server) storTracer() trace.Tracer {
	return otel.Tracer(storageTracerName)
}

// registerStorageRoutes is called from registerRoutes() in server.go.
func (s *Server) registerStorageRoutes() {
	s.mux.HandleFunc("POST /api/v1/storage/upload", s.handleStorageUpload)
	s.mux.HandleFunc("GET /api/v1/storage/signed-url", s.handleStorageSignedURL)
	s.mux.HandleFunc("GET /api/v1/storage", s.handleStorageList)
	// Catch-all by key. ServeMux's pattern parser treats `{key...}` as a
	// wildcard that matches the rest of the path (including slashes), which
	// is exactly what S3-style keys need.
	s.mux.HandleFunc("GET /api/v1/storage/{key...}", s.handleStorageDownload)
	s.mux.HandleFunc("DELETE /api/v1/storage/{key...}", s.handleStorageDelete)
}

// storageReady is the standard 503 envelope when the adapter is unconfigured.
func (s *Server) storageReady(w http.ResponseWriter) bool {
	if s.storage == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			errEnvelope("STORAGE_NOT_CONFIGURED", "storage adapter not configured"))
		return false
	}
	return true
}

// objectKey applies the tenant prefix configured in server.Deps. Phase 5
// keeps multi-tenancy off by default; the prefix is "" and keys pass
// through untouched. When MT lands, the prefix will be derived from the
// request's tenant context.
func (s *Server) objectKey(raw string) string {
	if s.storagePrefix == "" {
		return raw
	}
	if strings.HasSuffix(s.storagePrefix, "/") {
		return s.storagePrefix + raw
	}
	return s.storagePrefix + "/" + raw
}

// stripPrefix removes the tenant prefix from a key returned by the adapter
// before exposing it to the dashboard. Symmetric with objectKey.
func (s *Server) stripPrefix(key string) string {
	if s.storagePrefix == "" {
		return key
	}
	p := s.storagePrefix
	if !strings.HasSuffix(p, "/") {
		p = p + "/"
	}
	return strings.TrimPrefix(key, p)
}

// toResp converts storage.Object → wire format.
func (s *Server) toResp(o storage.Object) storageObjectResp {
	resp := storageObjectResp{
		Key:          s.stripPrefix(o.Key),
		Size:         o.Size,
		LastModified: o.LastModified.UTC().Format(time.RFC3339),
	}
	if o.ContentType != "" {
		ct := o.ContentType
		resp.ContentType = &ct
	}
	if o.ETag != "" {
		// minio-go quotes ETags; the wire schema doesn't care, but strip
		// for tidiness.
		et := strings.Trim(o.ETag, `"`)
		resp.ETag = &et
	}
	return resp
}

// ─── POST /api/v1/storage/upload ──────────────────────────────────────────

// handleStorageUpload accepts a multipart form with fields `key` and `file`.
//
// Flow:
//  1. Parse multipart with bounded memory + total-size cap.
//  2. Fire hooks.HookStoragePreUpload with {key, size_hint, content_type}.
//     A handler may abort by returning an error → upload is rejected.
//  3. Stream the file part to the adapter under the tenant-prefixed key.
//  4. Respond with StorageObjectSchema-shaped JSON.
func (s *Server) handleStorageUpload(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.storTracer().Start(r.Context(), "storage.upload",
		trace.WithSpanKind(trace.SpanKindServer),
	)
	defer span.End()

	if !s.storageReady(w) {
		span.SetStatus(codes.Error, "storage not configured")
		return
	}

	// Bound the total body size before parsing so a hostile client can't
	// fill memory with multipart preamble.
	r.Body = http.MaxBytesReader(w, r.Body, uploadSizeCap)

	// Parse only headers + small form fields into memory; large file parts
	// stay on disk via the multipart Reader below.
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "parse multipart")
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("BAD_REQUEST", "could not parse multipart form: "+err.Error()))
		return
	}

	rawKey := strings.TrimSpace(r.FormValue("key"))
	if rawKey == "" {
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("VALIDATION_FAILED", "form field `key` is required"))
		return
	}
	if !validObjectKey(rawKey) {
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("VALIDATION_FAILED", "invalid object key"))
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("VALIDATION_FAILED", "form field `file` is required: "+err.Error()))
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	storeKey := s.objectKey(rawKey)

	span.SetAttributes(
		attribute.String("storage.key", storeKey),
		attribute.Int64("storage.size_hint", header.Size),
		attribute.String("storage.content_type", contentType),
	)

	// Pre-upload hook. Handlers receive the prefixed key + size hint and
	// can short-circuit the upload (e.g. quota/PII guards).
	if s.hooks != nil {
		payload := map[string]any{
			"key":          storeKey,
			"size_hint":    header.Size,
			"content_type": contentType,
		}
		if _, err := s.hooks.Fire(ctx, hooks.HookStoragePreUpload, payload); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "hook aborted upload")
			writeJSON(w, http.StatusForbidden,
				errEnvelope("UPLOAD_REJECTED", err.Error()))
			return
		}
	}

	obj, err := s.storage.Upload(ctx, storeKey, file, storage.UploadOpts{
		ContentType: contentType,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "upload failed")
		s.log.Error("storage upload failed", "key", storeKey, "error", err)
		writeJSON(w, http.StatusBadGateway,
			errEnvelope("STORAGE_UPLOAD_FAILED", err.Error()))
		return
	}

	writeJSON(w, http.StatusOK, s.toResp(*obj))
}

// ─── GET /api/v1/storage/signed-url ───────────────────────────────────────

// handleStorageSignedURL returns a time-limited presigned GET URL.
//
// Query params:
//
//	key — required
//	ttl — seconds, default 3600, max 604800 (S3's 7-day limit)
func (s *Server) handleStorageSignedURL(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.storTracer().Start(r.Context(), "storage.signed_url",
		trace.WithSpanKind(trace.SpanKindServer),
	)
	defer span.End()

	if !s.storageReady(w) {
		return
	}

	rawKey := strings.TrimSpace(r.URL.Query().Get("key"))
	if rawKey == "" {
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("VALIDATION_FAILED", "query param `key` is required"))
		return
	}
	if !validObjectKey(rawKey) {
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("VALIDATION_FAILED", "invalid object key"))
		return
	}

	ttl := defaultPresignTTL
	if v := r.URL.Query().Get("ttl"); v != "" {
		secs, err := strconv.Atoi(v)
		if err != nil || secs <= 0 {
			writeJSON(w, http.StatusBadRequest,
				errEnvelope("VALIDATION_FAILED", "ttl must be a positive integer (seconds)"))
			return
		}
		ttl = time.Duration(secs) * time.Second
	}
	caps := s.storage.Capabilities()
	if max := time.Duration(caps.PresignTTLMaxSeconds) * time.Second; max > 0 && ttl > max {
		ttl = max
	}

	storeKey := s.objectKey(rawKey)
	span.SetAttributes(
		attribute.String("storage.key", storeKey),
		attribute.Float64("storage.ttl_s", ttl.Seconds()),
	)

	url, err := s.storage.SignedURL(ctx, storeKey, ttl)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "presign failed")
		writeJSON(w, http.StatusBadGateway,
			errEnvelope("STORAGE_PRESIGN_FAILED", err.Error()))
		return
	}

	writeJSON(w, http.StatusOK, signedURLResp{
		Key:       rawKey,
		URL:       url,
		ExpiresAt: time.Now().UTC().Add(ttl).Format(time.RFC3339),
	})
}

// ─── GET /api/v1/storage/{key...} ─────────────────────────────────────────

// handleStorageDownload streams the object body to the client.
//
// Sets Content-Type to whatever the adapter reports (or
// application/octet-stream as fallback). Sets Content-Length when known.
func (s *Server) handleStorageDownload(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.storTracer().Start(r.Context(), "storage.download",
		trace.WithSpanKind(trace.SpanKindServer),
	)
	defer span.End()

	if !s.storageReady(w) {
		return
	}

	rawKey := r.PathValue("key")
	if rawKey == "" {
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("VALIDATION_FAILED", "key required in path"))
		return
	}
	if !validObjectKey(rawKey) {
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("VALIDATION_FAILED", "invalid object key"))
		return
	}

	storeKey := s.objectKey(rawKey)
	span.SetAttributes(attribute.String("storage.key", storeKey))

	body, obj, err := s.storage.Download(ctx, storeKey)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSON(w, http.StatusNotFound,
				errEnvelope("NOT_FOUND", "object not found"))
			return
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "download failed")
		writeJSON(w, http.StatusBadGateway,
			errEnvelope("STORAGE_DOWNLOAD_FAILED", err.Error()))
		return
	}
	defer body.Close()

	ct := obj.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	if obj.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(obj.Size, 10))
	}
	if obj.ETag != "" {
		w.Header().Set("ETag", obj.ETag)
	}
	w.Header().Set("Last-Modified", obj.LastModified.UTC().Format(http.TimeFormat))
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, body); err != nil {
		// Headers already sent — log only.
		s.log.Warn("storage download stream interrupted", "key", storeKey, "error", err)
	}
}

// ─── DELETE /api/v1/storage/{key...} ──────────────────────────────────────

// handleStorageDelete removes an object. Always returns {deleted: true} on
// success (idempotent — missing keys are not an error).
func (s *Server) handleStorageDelete(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.storTracer().Start(r.Context(), "storage.delete",
		trace.WithSpanKind(trace.SpanKindServer),
	)
	defer span.End()

	if !s.storageReady(w) {
		return
	}

	rawKey := r.PathValue("key")
	if rawKey == "" {
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("VALIDATION_FAILED", "key required in path"))
		return
	}
	if !validObjectKey(rawKey) {
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("VALIDATION_FAILED", "invalid object key"))
		return
	}

	storeKey := s.objectKey(rawKey)
	span.SetAttributes(attribute.String("storage.key", storeKey))

	if err := s.storage.Delete(ctx, storeKey); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "delete failed")
		writeJSON(w, http.StatusBadGateway,
			errEnvelope("STORAGE_DELETE_FAILED", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ─── GET /api/v1/storage ──────────────────────────────────────────────────

// handleStorageList returns a single page of objects under an optional
// prefix.
//
// Query params:
//
//	prefix     — optional, filters by key prefix
//	next_token — optional, opaque continuation cursor
//	limit      — optional, 1..1000, default 100
func (s *Server) handleStorageList(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.storTracer().Start(r.Context(), "storage.list",
		trace.WithSpanKind(trace.SpanKindServer),
	)
	defer span.End()

	if !s.storageReady(w) {
		return
	}

	q := r.URL.Query()
	rawPrefix := q.Get("prefix")
	nextToken := q.Get("next_token")

	limit := 100
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			writeJSON(w, http.StatusBadRequest,
				errEnvelope("VALIDATION_FAILED", "limit must be a positive integer"))
			return
		}
		if n > 1000 {
			n = 1000
		}
		limit = n
	}

	storePrefix := s.objectKey(rawPrefix)
	span.SetAttributes(
		attribute.String("storage.prefix", storePrefix),
		attribute.Int("storage.limit", limit),
	)

	// The continuation cursor is stored already-prefixed (because the
	// adapter is what generated it). If a client passes a raw cursor, we
	// still prepend the tenant prefix when MT is on.
	cursor := nextToken
	if cursor != "" && s.storagePrefix != "" && !strings.HasPrefix(cursor, s.storagePrefix) {
		cursor = s.objectKey(cursor)
	}

	res, err := s.storage.List(ctx, storePrefix, cursor, limit)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "list failed")
		writeJSON(w, http.StatusBadGateway,
			errEnvelope("STORAGE_LIST_FAILED", err.Error()))
		return
	}

	resp := storageListResp{
		Objects: make([]storageObjectResp, 0, len(res.Objects)),
		Prefix:  rawPrefix,
	}
	for _, o := range res.Objects {
		resp.Objects = append(resp.Objects, s.toResp(o))
	}
	if res.NextToken != "" {
		token := s.stripPrefix(res.NextToken)
		resp.NextToken = &token
	}
	writeJSON(w, http.StatusOK, resp)
}

// validObjectKey enforces a minimal sanity check on caller-supplied keys.
//
// Allow: alphanumerics, plus `_ - . / + space`. Disallow: NUL, `..` segments
// (path traversal), absolute paths (leading `/`), and trailing `/`.
func validObjectKey(key string) bool {
	if key == "" || len(key) > 1024 {
		return false
	}
	if strings.HasPrefix(key, "/") || strings.HasSuffix(key, "/") {
		return false
	}
	if strings.Contains(key, "\x00") {
		return false
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}
