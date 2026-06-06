//go:build integration

// adapter_test.go is a real-MinIO integration test.
//
// Run with:
//
//	docker compose up -d minio
//	AF_STACK_S3_ENDPOINT=localhost:9000 \
//	AF_STACK_S3_BUCKET=af-stack-test \
//	AF_STACK_S3_ACCESS_KEY=minio \
//	AF_STACK_S3_SECRET_KEY=minio-secret \
//	go test -tags=integration ./services/runtime/internal/storage/adapters/minio/...
//
// Unit-level coverage (no MinIO required) lives in
// services/runtime/internal/storage/storage_test.go via the in-memory stub.
package minio_test

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/storage"
	miniopkg "github.com/Agent-Field/backai/services/runtime/internal/storage/adapters/minio"
)

func newRealAdapter(t *testing.T) *miniopkg.Adapter {
	t.Helper()
	endpoint := os.Getenv("AF_STACK_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("AF_STACK_S3_ENDPOINT not set; skipping integration test")
	}
	bucket := os.Getenv("AF_STACK_S3_BUCKET")
	if bucket == "" {
		bucket = "af-stack-test"
	}
	a, err := miniopkg.New(miniopkg.Config{
		Endpoint:  endpoint,
		Bucket:    bucket,
		AccessKey: os.Getenv("AF_STACK_S3_ACCESS_KEY"),
		SecretKey: os.Getenv("AF_STACK_S3_SECRET_KEY"),
		Region:    os.Getenv("AF_STACK_S3_REGION"),
	})
	if err != nil {
		t.Fatalf("new minio adapter: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.EnsureBucket(ctx); err != nil {
		t.Fatalf("ensure bucket: %v", err)
	}
	return a
}

func TestMinIORoundTrip(t *testing.T) {
	a := newRealAdapter(t)
	ctx := context.Background()
	key := "integration-test/" + t.Name() + ".txt"

	obj, err := a.Upload(ctx, key, strings.NewReader("hello minio"),
		storage.UploadOpts{ContentType: "text/plain"})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if obj.Key != key {
		t.Errorf("upload returned key %q, want %q", obj.Key, key)
	}

	rc, dobj, err := a.Download(ctx, key)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if string(body) != "hello minio" {
		t.Errorf("body = %q, want hello minio", body)
	}
	if dobj.ContentType != "text/plain" {
		t.Errorf("content type = %q, want text/plain", dobj.ContentType)
	}

	url, err := a.SignedURL(ctx, key, 30*time.Second)
	if err != nil {
		t.Fatalf("signed url: %v", err)
	}
	if !strings.Contains(url, key) {
		t.Errorf("signed url %q does not reference key %q", url, key)
	}

	list, err := a.List(ctx, "integration-test/", "", 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, o := range list.Objects {
		if o.Key == key {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("listed objects did not include %q (got %d)", key, len(list.Objects))
	}

	if err := a.Delete(ctx, key); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, _, err := a.Download(ctx, key); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("download after delete should be ErrNotFound, got %v", err)
	}
	// Delete is idempotent.
	if err := a.Delete(ctx, key); err != nil {
		t.Errorf("delete idempotent: %v", err)
	}
}
