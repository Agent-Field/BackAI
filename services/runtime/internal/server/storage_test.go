// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/hooks"
	"github.com/Agent-Field/backai/services/runtime/internal/storage"
)

// stubStorage is an in-memory implementation of storage.Storage used by the
// handler tests. Not thread-safe — tests are single-goroutine.
type stubStorage struct {
	objects     map[string][]byte
	contentType map[string]string
	caps        storage.Capabilities
}

func newStubStorage() *stubStorage {
	return &stubStorage{
		objects:     map[string][]byte{},
		contentType: map[string]string{},
		caps: storage.Capabilities{
			MaxObjectSizeBytes:   256 << 20,
			SupportsMultipart:    false,
			PresignTTLMaxSeconds: 7 * 24 * 60 * 60,
		},
	}
}

func (s *stubStorage) Upload(ctx context.Context, key string, r io.Reader, opts storage.UploadOpts) (*storage.Object, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	s.objects[key] = body
	s.contentType[key] = opts.ContentType
	return &storage.Object{
		Key:          key,
		Size:         int64(len(body)),
		ContentType:  opts.ContentType,
		LastModified: time.Now().UTC().Truncate(time.Second),
		ETag:         "stub-etag",
	}, nil
}

func (s *stubStorage) Download(ctx context.Context, key string) (io.ReadCloser, *storage.Object, error) {
	body, ok := s.objects[key]
	if !ok {
		return nil, nil, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(body)),
		&storage.Object{
			Key:          key,
			Size:         int64(len(body)),
			ContentType:  s.contentType[key],
			LastModified: time.Now().UTC().Truncate(time.Second),
			ETag:         "stub-etag",
		}, nil
}

func (s *stubStorage) SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return fmt.Sprintf("https://stub.example.com/%s?ttl=%d", key, int(ttl.Seconds())), nil
}

func (s *stubStorage) Delete(ctx context.Context, key string) error {
	delete(s.objects, key)
	delete(s.contentType, key)
	return nil
}

func (s *stubStorage) List(ctx context.Context, prefix, nextToken string, limit int) (*storage.ListResult, error) {
	keys := make([]string, 0, len(s.objects))
	for k := range s.objects {
		if strings.HasPrefix(k, prefix) && k > nextToken {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := &storage.ListResult{Prefix: prefix}
	for i, k := range keys {
		if i >= limit {
			out.NextToken = out.Objects[len(out.Objects)-1].Key
			break
		}
		out.Objects = append(out.Objects, storage.Object{
			Key:          k,
			Size:         int64(len(s.objects[k])),
			ContentType:  s.contentType[k],
			LastModified: time.Now().UTC().Truncate(time.Second),
			ETag:         "stub-etag",
		})
	}
	return out, nil
}

func (s *stubStorage) EnsureBucket(ctx context.Context) error { return nil }
func (s *stubStorage) Capabilities() storage.Capabilities     { return s.caps }

func newStorageServer(t *testing.T, store storage.Storage, eng *hooks.Engine, prefix string) *Server {
	t.Helper()
	cfg := config.Default()
	return New(cfg, slog.Default(), Deps{
		Storage:       store,
		Hooks:         eng,
		StoragePrefix: prefix,
	})
}

// uploadRequest builds a multipart POST.
func uploadRequest(t *testing.T, key, filename, contentType, body string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("key", key)
	hdr := make(map[string][]string)
	hdr["Content-Disposition"] = []string{
		fmt.Sprintf(`form-data; name="file"; filename=%q`, filename),
	}
	hdr["Content-Type"] = []string{contentType}
	fw, err := mw.CreatePart(hdr)
	if err != nil {
		t.Fatalf("multipart part: %v", err)
	}
	_, _ = fw.Write([]byte(body))
	_ = mw.Close()

	req := httptest.NewRequest("POST", "/api/v1/storage/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// ─── Tests ────────────────────────────────────────────────────────────────

func TestStorageUpload503WhenNotConfigured(t *testing.T) {
	srv := newStorageServer(t, nil, nil, "")
	req := uploadRequest(t, "test.txt", "test.txt", "text/plain", "hello")
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "STORAGE_NOT_CONFIGURED") {
		t.Errorf("expected STORAGE_NOT_CONFIGURED in body, got %s", rec.Body.String())
	}
}

func TestStorageUploadHappyPath(t *testing.T) {
	store := newStubStorage()
	srv := newStorageServer(t, store, nil, "")
	req := uploadRequest(t, "docs/readme.md", "readme.md", "text/markdown", "# hi")
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["key"] != "docs/readme.md" {
		t.Errorf("key = %v, want docs/readme.md", out["key"])
	}
	if out["size"].(float64) != 4 {
		t.Errorf("size = %v, want 4", out["size"])
	}
	if out["content_type"] != "text/markdown" {
		t.Errorf("content_type = %v, want text/markdown", out["content_type"])
	}
	// Wire-shape check: nullable fields are present (not omitted).
	if _, ok := out["etag"]; !ok {
		t.Errorf("expected etag field in response (nullable but present)")
	}
	if _, ok := out["last_modified"]; !ok {
		t.Errorf("expected last_modified field in response")
	}
	// Stored under the raw key (no prefix configured).
	if _, ok := store.objects["docs/readme.md"]; !ok {
		t.Errorf("object not stored under expected key; store=%v", store.objects)
	}
}

func TestStorageUploadAppliesTenantPrefix(t *testing.T) {
	store := newStubStorage()
	srv := newStorageServer(t, store, nil, "tenants/default")
	req := uploadRequest(t, "uploads/x.bin", "x.bin", "application/octet-stream", "data")
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, ok := store.objects["tenants/default/uploads/x.bin"]; !ok {
		t.Errorf("object not stored under prefixed key; store=%v", store.objects)
	}
	// Response key should be the unprefixed form (what the dashboard sent).
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["key"] != "uploads/x.bin" {
		t.Errorf("response key = %v, want uploads/x.bin (prefix stripped)", out["key"])
	}
}

func TestStorageUploadRejectsBadKey(t *testing.T) {
	store := newStubStorage()
	srv := newStorageServer(t, store, nil, "")
	cases := []string{"", "../escape", "leading/", "/abs", "has\x00null"}
	for _, k := range cases {
		req := uploadRequest(t, k, "x", "text/plain", "x")
		rec := httptest.NewRecorder()
		srv.mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("key=%q: expected 400, got %d", k, rec.Code)
		}
	}
}

func TestStorageUploadHookCanAbort(t *testing.T) {
	store := newStubStorage()
	eng := hooks.NewEngine(slog.Default())
	called := 0
	_ = eng.Register(hooks.HookStoragePreUpload, func(ctx context.Context, payload any) (any, error) {
		called++
		m, _ := payload.(map[string]any)
		if m["key"] != "blocked.txt" {
			t.Errorf("hook got unexpected key: %v", m["key"])
		}
		return payload, errors.New("forbidden by guard")
	})
	srv := newStorageServer(t, store, eng, "")
	req := uploadRequest(t, "blocked.txt", "blocked.txt", "text/plain", "data")
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if called != 1 {
		t.Errorf("hook called %d times, want 1", called)
	}
	if _, ok := store.objects["blocked.txt"]; ok {
		t.Errorf("upload should not have stored object after hook abort")
	}
}

func TestStorageUploadHookSeesSizeHintAndContentType(t *testing.T) {
	store := newStubStorage()
	eng := hooks.NewEngine(slog.Default())
	var gotPayload map[string]any
	_ = eng.Register(hooks.HookStoragePreUpload, func(ctx context.Context, payload any) (any, error) {
		gotPayload, _ = payload.(map[string]any)
		return payload, nil
	})
	srv := newStorageServer(t, store, eng, "")
	req := uploadRequest(t, "ok.png", "ok.png", "image/png", "PNGDATA")
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if gotPayload == nil {
		t.Fatal("hook payload was nil")
	}
	if gotPayload["content_type"] != "image/png" {
		t.Errorf("content_type = %v, want image/png", gotPayload["content_type"])
	}
	if _, ok := gotPayload["size_hint"]; !ok {
		t.Errorf("size_hint missing from payload: %v", gotPayload)
	}
}

func TestStorageSignedURLDefaultTTL(t *testing.T) {
	store := newStubStorage()
	srv := newStorageServer(t, store, nil, "")
	req := httptest.NewRequest("GET", "/api/v1/storage/signed-url?key=foo.txt", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	for _, field := range []string{"key", "url", "expires_at"} {
		if _, ok := out[field]; !ok {
			t.Errorf("missing field %q in signed-url response", field)
		}
	}
	if out["key"] != "foo.txt" {
		t.Errorf("key = %v, want foo.txt", out["key"])
	}
}

func TestStorageSignedURLClampsTTL(t *testing.T) {
	store := newStubStorage()
	srv := newStorageServer(t, store, nil, "")
	// 2 weeks > 7-day cap → adapter cap should clamp it.
	req := httptest.NewRequest("GET", "/api/v1/storage/signed-url?key=foo&ttl=1209600", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	url, _ := out["url"].(string)
	// stub url echoes the ttl seconds the handler passed → should be 7d.
	if !strings.Contains(url, "ttl=604800") {
		t.Errorf("expected ttl clamped to 7 days, got url=%s", url)
	}
}

func TestStorageSignedURLBadTTL(t *testing.T) {
	store := newStubStorage()
	srv := newStorageServer(t, store, nil, "")
	req := httptest.NewRequest("GET", "/api/v1/storage/signed-url?key=foo&ttl=abc", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestStorageSignedURLMissingKey(t *testing.T) {
	store := newStubStorage()
	srv := newStorageServer(t, store, nil, "")
	req := httptest.NewRequest("GET", "/api/v1/storage/signed-url", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestStorageDownloadStreamsBody(t *testing.T) {
	store := newStubStorage()
	store.objects["hello.txt"] = []byte("world")
	store.contentType["hello.txt"] = "text/plain"
	srv := newStorageServer(t, store, nil, "")
	req := httptest.NewRequest("GET", "/api/v1/storage/hello.txt", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "text/plain" {
		t.Errorf("Content-Type = %q, want text/plain", rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != "world" {
		t.Errorf("body = %q, want world", rec.Body.String())
	}
}

func TestStorageDownloadNestedKey(t *testing.T) {
	store := newStubStorage()
	store.objects["a/b/c.bin"] = []byte("nested")
	srv := newStorageServer(t, store, nil, "")
	req := httptest.NewRequest("GET", "/api/v1/storage/a/b/c.bin", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "nested" {
		t.Errorf("body = %q, want nested", rec.Body.String())
	}
}

func TestStorageDownloadResizeTransform(t *testing.T) {
	store := newStubStorage()
	store.objects["avatar.png"] = testPNG(t, 8, 4)
	store.contentType["avatar.png"] = "image/png"
	srv := newStorageServer(t, store, nil, "")
	req := httptest.NewRequest("GET", "/api/v1/storage/avatar.png?width=4&format=png", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", got)
	}
	img, _, err := image.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode transformed image: %v", err)
	}
	if got := img.Bounds().Dx(); got != 4 {
		t.Fatalf("width = %d, want 4", got)
	}
	if got := img.Bounds().Dy(); got != 2 {
		t.Fatalf("height = %d, want 2", got)
	}
}

func TestStorageDownloadThumbnailDefaults(t *testing.T) {
	store := newStubStorage()
	store.objects["wide.png"] = testPNG(t, 16, 8)
	store.contentType["wide.png"] = "image/png"
	srv := newStorageServer(t, store, nil, "")
	req := httptest.NewRequest("GET", "/api/v1/storage/wide.png?transform=thumbnail&height=4", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	img, _, err := image.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode transformed image: %v", err)
	}
	if got := img.Bounds().Dx(); got != 4 {
		t.Fatalf("width = %d, want 4", got)
	}
	if got := img.Bounds().Dy(); got != 2 {
		t.Fatalf("height = %d, want 2", got)
	}
}

func TestStorageDownloadTransformRejectsNonImage(t *testing.T) {
	store := newStubStorage()
	store.objects["hello.txt"] = []byte("world")
	store.contentType["hello.txt"] = "text/plain"
	srv := newStorageServer(t, store, nil, "")
	req := httptest.NewRequest("GET", "/api/v1/storage/hello.txt?width=10", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected 415, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStorageDownloadNotFound(t *testing.T) {
	store := newStubStorage()
	srv := newStorageServer(t, store, nil, "")
	req := httptest.NewRequest("GET", "/api/v1/storage/missing.txt", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 10), G: uint8(y * 10), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestStorageDownloadAppliesTenantPrefix(t *testing.T) {
	store := newStubStorage()
	store.objects["tenants/default/secret.bin"] = []byte("classified")
	srv := newStorageServer(t, store, nil, "tenants/default")
	req := httptest.NewRequest("GET", "/api/v1/storage/secret.bin", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "classified" {
		t.Errorf("body = %q, want classified", rec.Body.String())
	}
}

func TestStorageDelete(t *testing.T) {
	store := newStubStorage()
	store.objects["doomed.txt"] = []byte("bye")
	srv := newStorageServer(t, store, nil, "")
	req := httptest.NewRequest("DELETE", "/api/v1/storage/doomed.txt", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"deleted":true`) {
		t.Errorf("body = %s, want deleted:true", rec.Body.String())
	}
	if _, ok := store.objects["doomed.txt"]; ok {
		t.Errorf("object not removed from stub")
	}
}

func TestStorageList(t *testing.T) {
	store := newStubStorage()
	store.objects["a/1.txt"] = []byte("x")
	store.objects["a/2.txt"] = []byte("yy")
	store.objects["b/3.txt"] = []byte("zzz")
	srv := newStorageServer(t, store, nil, "")
	req := httptest.NewRequest("GET", "/api/v1/storage?prefix=a/&limit=10", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	objects, _ := out["objects"].([]any)
	if len(objects) != 2 {
		t.Errorf("got %d objects, want 2: %v", len(objects), out)
	}
	if out["prefix"] != "a/" {
		t.Errorf("prefix = %v, want a/", out["prefix"])
	}
	// next_token MUST be present in the response (zod allows null but
	// requires the key — nil pointer with json:""... encodes as null).
	if _, ok := out["next_token"]; !ok {
		t.Errorf("expected next_token field (nullable but present)")
	}
}

func TestStorageListPagination(t *testing.T) {
	store := newStubStorage()
	for _, name := range []string{"a/01.txt", "a/02.txt", "a/03.txt", "a/04.txt"} {
		store.objects[name] = []byte("x")
	}
	srv := newStorageServer(t, store, nil, "")
	req := httptest.NewRequest("GET", "/api/v1/storage?prefix=a/&limit=2", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if tok, ok := out["next_token"].(string); !ok || tok == "" {
		t.Errorf("expected next_token string on partial page, got %v", out["next_token"])
	}
	objects, _ := out["objects"].([]any)
	if len(objects) != 2 {
		t.Errorf("page size = %d, want 2", len(objects))
	}
}

func TestStorageListWithTenantPrefix(t *testing.T) {
	store := newStubStorage()
	store.objects["tenants/default/a/1.txt"] = []byte("x")
	store.objects["tenants/default/a/2.txt"] = []byte("y")
	store.objects["tenants/other/a/3.txt"] = []byte("z")
	srv := newStorageServer(t, store, nil, "tenants/default")
	req := httptest.NewRequest("GET", "/api/v1/storage?prefix=a/", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	objects, _ := out["objects"].([]any)
	if len(objects) != 2 {
		t.Errorf("expected 2 objects after tenant scoping, got %d", len(objects))
	}
	// Returned keys should be unprefixed (what the dashboard sent).
	for _, o := range objects {
		m := o.(map[string]any)
		key, _ := m["key"].(string)
		if strings.HasPrefix(key, "tenants/") {
			t.Errorf("key not stripped of tenant prefix: %s", key)
		}
	}
}

func TestStorageDelete503WhenNotConfigured(t *testing.T) {
	srv := newStorageServer(t, nil, nil, "")
	req := httptest.NewRequest("DELETE", "/api/v1/storage/anything.bin", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestValidObjectKey(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"foo.txt", true},
		{"a/b/c.txt", true},
		{"tenants/default/uploads/x.bin", true},
		{"with spaces.txt", true},
		{"with+plus.txt", true},
		{"", false},
		{"/abs", false},
		{"trailing/", false},
		{"../up", false},
		{"a/../b", false},
		{"has\x00null", false},
		{strings.Repeat("a", 1025), false},
	}
	for _, c := range cases {
		if got := validObjectKey(c.in); got != c.want {
			t.Errorf("validObjectKey(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
