// SPDX-License-Identifier: Apache-2.0

// storage_test.go covers the interface contract via an in-memory stub and
// holds a compile-time check that the stub implements storage.Storage. The
// real-MinIO integration test lives in adapters/minio/adapter_test.go behind
// the `integration` build tag.
package storage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/storage"
)

// compile-time check: stub implements Storage.
var _ storage.Storage = (*stubStorage)(nil)

// stubStorage is an in-memory storage adapter used by handler + interface
// tests. Not thread-safe — tests are single-goroutine.
type stubStorage struct {
	objects     map[string][]byte
	contentType map[string]string
	caps        storage.Capabilities
	// signedURLBase is what SignedURL returns; tests inspect it.
	signedURLBase string
}

func newStub() *stubStorage {
	return &stubStorage{
		objects:     map[string][]byte{},
		contentType: map[string]string{},
		caps: storage.Capabilities{
			MaxObjectSizeBytes:   256 << 20,
			SupportsMultipart:    false,
			PresignTTLMaxSeconds: 7 * 24 * 60 * 60,
		},
		signedURLBase: "https://stub.example.com/",
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
	return s.signedURLBase + key, nil
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
	// sort lexicographically for deterministic results
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
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

// TestStubRoundTrip exercises the contract via the stub. Real-adapter
// coverage lives behind the `integration` build tag — see
// adapters/minio/adapter_test.go.
func TestStubRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := newStub()

	if err := st.EnsureBucket(ctx); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}

	// Upload
	obj, err := st.Upload(ctx, "a/b/c.txt", strings.NewReader("hello world"),
		storage.UploadOpts{ContentType: "text/plain"})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if obj.Size != 11 || obj.ContentType != "text/plain" {
		t.Errorf("unexpected obj: %+v", obj)
	}

	// List sees it
	listed, err := st.List(ctx, "a/", "", 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed.Objects) != 1 || listed.Objects[0].Key != "a/b/c.txt" {
		t.Errorf("List returned %+v", listed.Objects)
	}

	// SignedURL
	url, err := st.SignedURL(ctx, "a/b/c.txt", 5*time.Minute)
	if err != nil {
		t.Fatalf("SignedURL: %v", err)
	}
	if !strings.Contains(url, "a/b/c.txt") {
		t.Errorf("signed url missing key: %s", url)
	}

	// Download
	rc, dobj, err := st.Download(ctx, "a/b/c.txt")
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if string(got) != "hello world" {
		t.Errorf("download body = %q, want hello world", got)
	}
	if dobj.Size != 11 {
		t.Errorf("download size = %d, want 11", dobj.Size)
	}

	// Delete
	if err := st.Delete(ctx, "a/b/c.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := st.Download(ctx, "a/b/c.txt"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Download after Delete should return ErrNotFound, got %v", err)
	}

	// Delete is idempotent
	if err := st.Delete(ctx, "a/b/c.txt"); err != nil {
		t.Errorf("Delete idempotent: %v", err)
	}
}

func TestStubListPagination(t *testing.T) {
	ctx := context.Background()
	st := newStub()
	for i := 0; i < 5; i++ {
		_, _ = st.Upload(ctx, "x/"+string(rune('a'+i))+".bin",
			strings.NewReader("data"), storage.UploadOpts{ContentType: "application/octet-stream"})
	}
	page1, err := st.List(ctx, "x/", "", 2)
	if err != nil {
		t.Fatalf("List page1: %v", err)
	}
	if len(page1.Objects) != 2 {
		t.Fatalf("page1 want 2 objects, got %d", len(page1.Objects))
	}
	if page1.NextToken == "" {
		t.Fatalf("page1 should have next_token")
	}
	page2, err := st.List(ctx, "x/", page1.NextToken, 2)
	if err != nil {
		t.Fatalf("List page2: %v", err)
	}
	if len(page2.Objects) != 2 {
		t.Errorf("page2 want 2 objects, got %d", len(page2.Objects))
	}
}

func TestCapabilities(t *testing.T) {
	st := newStub()
	c := st.Capabilities()
	if c.MaxObjectSizeBytes <= 0 {
		t.Errorf("MaxObjectSizeBytes should be positive, got %d", c.MaxObjectSizeBytes)
	}
	if c.PresignTTLMaxSeconds <= 0 {
		t.Errorf("PresignTTLMaxSeconds should be positive, got %d", c.PresignTTLMaxSeconds)
	}
}