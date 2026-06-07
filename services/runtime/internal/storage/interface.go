// SPDX-License-Identifier: Apache-2.0

// Package storage defines the object-storage abstraction used by the suite
// runtime.
//
// The interface is intentionally small: upload, download, signed URLs, list,
// delete, plus a bootstrap method (EnsureBucket) and a Capabilities report
// so callers can adapt to adapter limits.
//
// Two adapters live under adapters/: minio (the local-dev MinIO server) and
// s3 (AWS S3). Both speak the S3 protocol, so the underlying client is the
// same (`minio-go`); the adapter choice only changes TLS defaults and the
// adapter label used in logs/metrics.
//
// Object keys are forward-slash separated, e.g. `tenants/default/uploads/foo.png`.
// Adapters do not enforce key shape; that's the caller's responsibility
// (server handlers prepend the tenant prefix when multi-tenancy is on).
package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrNotFound is returned by Download when the requested key does not exist.
//
// Adapters should wrap their backend's "no such key" error with this sentinel
// so callers can use errors.Is(err, ErrNotFound) for branching.
var ErrNotFound = errors.New("storage: object not found")

// Object describes a single stored object's metadata.
//
// Adapters populate ContentType and ETag when the backend reports them;
// the empty string means "unknown" (the dashboard renders this as null).
type Object struct {
	Key          string
	Size         int64
	ContentType  string
	LastModified time.Time
	ETag         string
}

// UploadOpts carries optional metadata for an upload call.
type UploadOpts struct {
	ContentType string
	Metadata    map[string]string
}

// ListResult is a single page of a list operation.
//
// NextToken is the opaque continuation cursor; an empty string means the
// caller has reached the last page.
type ListResult struct {
	Objects   []Object
	Prefix    string
	NextToken string
}

// Capabilities reports adapter-level limits. Callers (e.g. the upload
// handler) can use these to reject oversize inputs before streaming.
type Capabilities struct {
	MaxObjectSizeBytes   int64
	SupportsMultipart    bool
	PresignTTLMaxSeconds int
}

// Storage is the abstract object-storage backend.
//
// All methods take a context and respect cancellation. Implementations must
// be safe for concurrent use from multiple goroutines.
type Storage interface {
	// Upload streams r to the backend under key. Returns the stored object
	// metadata (size, etag, last-modified) as the backend reports it.
	Upload(ctx context.Context, key string, r io.Reader, opts UploadOpts) (*Object, error)

	// Download fetches the object body. The caller MUST close the returned
	// reader. The Object pointer carries metadata (size, content-type).
	// Returns a wrapped ErrNotFound when the key is missing.
	Download(ctx context.Context, key string) (io.ReadCloser, *Object, error)

	// SignedURL returns a time-limited URL the client can hit directly. ttl
	// is bounded by Capabilities.PresignTTLMaxSeconds.
	SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error)

	// Delete removes the object. Idempotent: deleting a missing key is not
	// an error.
	Delete(ctx context.Context, key string) error

	// List returns up to limit objects under prefix, starting from
	// nextToken. An empty nextToken in the response means the listing is
	// complete. Adapters cap limit at a reasonable internal max.
	List(ctx context.Context, prefix, nextToken string, limit int) (*ListResult, error)

	// EnsureBucket creates the bucket if it does not already exist. Called
	// once at startup. Idempotent.
	EnsureBucket(ctx context.Context) error

	// Capabilities reports adapter-level limits.
	Capabilities() Capabilities
}