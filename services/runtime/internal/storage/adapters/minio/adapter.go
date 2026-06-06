// Package minio implements the storage.Storage interface against an
// S3-compatible MinIO or AWS S3 endpoint.
//
// The same code path serves both: the Config.Secure flag (plus the endpoint
// scheme) determines whether TLS is used. When the runtime is configured
// with adapter=minio, Secure defaults to false (local docker). With
// adapter=s3, Secure defaults to true (AWS endpoint).
//
// Pagination uses ListObjects with V2 + StartAfter as the continuation
// cursor. Presigned URLs use S3 PresignedGetObject (max TTL 7 days, the S3
// limit).
package minio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/Agent-Field/backai/services/runtime/internal/storage"
)

// Config holds adapter settings. Populate from env in main.go.
type Config struct {
	// Endpoint is the S3 endpoint (host:port, no scheme), e.g.
	// "minio:9000" or "s3.amazonaws.com". For convenience, a full URL
	// with scheme is also accepted and the scheme picks Secure.
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
	// Secure toggles HTTPS. Leave nil to let the endpoint scheme decide.
	Secure *bool
}

// Adapter implements storage.Storage.
type Adapter struct {
	cli    *minio.Client
	bucket string
	region string
	caps   storage.Capabilities
}

// listMaxPage caps a single List() call's MaxKeys. Mirrors the AWS S3
// hard maximum of 1000 keys per response.
const listMaxPage = 1000

// maxPresignSeconds is S3's hard limit on presigned URL lifetime.
const maxPresignSeconds = 7 * 24 * 60 * 60

// New constructs a MinIO/S3 adapter and verifies the bucket configuration
// is well-formed. The actual bucket creation happens in EnsureBucket().
func New(cfg Config) (*Adapter, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("storage/minio: endpoint required")
	}
	if cfg.Bucket == "" {
		return nil, errors.New("storage/minio: bucket required")
	}

	endpoint, secure := normaliseEndpoint(cfg.Endpoint, cfg.Secure)

	cli, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("storage/minio: new client: %w", err)
	}

	return &Adapter{
		cli:    cli,
		bucket: cfg.Bucket,
		region: cfg.Region,
		caps: storage.Capabilities{
			// 5 TiB is the S3 single-object cap; we don't enforce more
			// here. The handler can still reject earlier for safety.
			MaxObjectSizeBytes:   5 << 40,
			SupportsMultipart:    true,
			PresignTTLMaxSeconds: maxPresignSeconds,
		},
	}, nil
}

// normaliseEndpoint accepts either "host:port" or a full URL. When given a
// URL, the scheme picks Secure unless explicitly overridden.
func normaliseEndpoint(raw string, override *bool) (host string, secure bool) {
	host = raw
	if strings.Contains(raw, "://") {
		if u, err := url.Parse(raw); err == nil {
			host = u.Host
			secure = u.Scheme == "https"
		}
	}
	if override != nil {
		secure = *override
	}
	return host, secure
}

// EnsureBucket creates the bucket if missing. Idempotent.
func (a *Adapter) EnsureBucket(ctx context.Context) error {
	exists, err := a.cli.BucketExists(ctx, a.bucket)
	if err != nil {
		return fmt.Errorf("storage/minio: bucket exists: %w", err)
	}
	if exists {
		return nil
	}
	err = a.cli.MakeBucket(ctx, a.bucket, minio.MakeBucketOptions{Region: a.region})
	if err != nil {
		// Race: another replica may have just created it. Re-check.
		if exists2, _ := a.cli.BucketExists(ctx, a.bucket); exists2 {
			return nil
		}
		return fmt.Errorf("storage/minio: make bucket %q: %w", a.bucket, err)
	}
	return nil
}

// Upload streams r to the backend.
func (a *Adapter) Upload(ctx context.Context, key string, r io.Reader, opts storage.UploadOpts) (*storage.Object, error) {
	if key == "" {
		return nil, errors.New("storage/minio: empty key")
	}
	putOpts := minio.PutObjectOptions{
		ContentType:  opts.ContentType,
		UserMetadata: opts.Metadata,
	}
	// size = -1 lets minio-go stream and switch to multipart automatically.
	info, err := a.cli.PutObject(ctx, a.bucket, key, r, -1, putOpts)
	if err != nil {
		return nil, fmt.Errorf("storage/minio: put %q: %w", key, err)
	}
	// PutObject's UploadInfo does not carry ContentType; honour the caller's.
	return &storage.Object{
		Key:          info.Key,
		Size:         info.Size,
		ContentType:  opts.ContentType,
		LastModified: info.LastModified,
		ETag:         info.ETag,
	}, nil
}

// Download streams an object body to the caller.
func (a *Adapter) Download(ctx context.Context, key string) (io.ReadCloser, *storage.Object, error) {
	if key == "" {
		return nil, nil, errors.New("storage/minio: empty key")
	}
	// Stat first so we can return ErrNotFound cleanly before opening a stream.
	stat, err := a.cli.StatObject(ctx, a.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if isNotFound(err) {
			return nil, nil, fmt.Errorf("%w: %s", storage.ErrNotFound, key)
		}
		return nil, nil, fmt.Errorf("storage/minio: stat %q: %w", key, err)
	}
	obj, err := a.cli.GetObject(ctx, a.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("storage/minio: get %q: %w", key, err)
	}
	return obj, &storage.Object{
		Key:          stat.Key,
		Size:         stat.Size,
		ContentType:  stat.ContentType,
		LastModified: stat.LastModified,
		ETag:         stat.ETag,
	}, nil
}

// SignedURL returns a presigned GET URL.
func (a *Adapter) SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if key == "" {
		return "", errors.New("storage/minio: empty key")
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	if max := time.Duration(maxPresignSeconds) * time.Second; ttl > max {
		ttl = max
	}
	u, err := a.cli.PresignedGetObject(ctx, a.bucket, key, ttl, url.Values{})
	if err != nil {
		return "", fmt.Errorf("storage/minio: presign %q: %w", key, err)
	}
	return u.String(), nil
}

// Delete removes an object. Missing keys are not an error.
func (a *Adapter) Delete(ctx context.Context, key string) error {
	if key == "" {
		return errors.New("storage/minio: empty key")
	}
	err := a.cli.RemoveObject(ctx, a.bucket, key, minio.RemoveObjectOptions{})
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("storage/minio: remove %q: %w", key, err)
	}
	return nil
}

// List walks a prefix, paginated by nextToken (which is the lex-greater
// StartAfter cursor — opaque to the caller).
func (a *Adapter) List(ctx context.Context, prefix, nextToken string, limit int) (*storage.ListResult, error) {
	if limit <= 0 || limit > listMaxPage {
		limit = listMaxPage
	}
	opts := minio.ListObjectsOptions{
		Prefix:     prefix,
		Recursive:  true,
		StartAfter: nextToken,
		MaxKeys:    limit,
	}

	out := &storage.ListResult{Prefix: prefix}
	count := 0
	for info := range a.cli.ListObjects(ctx, a.bucket, opts) {
		if info.Err != nil {
			return nil, fmt.Errorf("storage/minio: list: %w", info.Err)
		}
		if count >= limit {
			// We have a page-worth + 1 — set the cursor to the last
			// returned key so the next call resumes after it.
			out.NextToken = out.Objects[len(out.Objects)-1].Key
			break
		}
		out.Objects = append(out.Objects, storage.Object{
			Key:          info.Key,
			Size:         info.Size,
			ContentType:  info.ContentType,
			LastModified: info.LastModified,
			ETag:         info.ETag,
		})
		count++
	}
	// If we broke out via the limit-check, NextToken is already set. If we
	// drained the channel naturally (count <= limit), NextToken stays "".
	if count >= limit && out.NextToken == "" && len(out.Objects) > 0 {
		out.NextToken = out.Objects[len(out.Objects)-1].Key
	}
	return out, nil
}

// Capabilities reports adapter limits.
func (a *Adapter) Capabilities() storage.Capabilities {
	return a.caps
}

// isNotFound reports whether err is a minio "no such key" or "no such bucket".
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	resp := minio.ToErrorResponse(err)
	return resp.Code == "NoSuchKey" || resp.Code == "NoSuchObject" || resp.StatusCode == 404
}
