// SPDX-License-Identifier: Apache-2.0

// Package s3 implements the storage.Storage interface against AWS S3.
//
// minio-go speaks vanilla S3 — there is no separate code path. This package
// wraps the minio adapter with TLS defaulted ON, which is the only
// adapter-level difference between MinIO (local docker, plaintext) and AWS
// S3 (always HTTPS).
//
// Pick this adapter when the runtime config has adapters: { storage: s3 }.
package s3

import (
	miniopkg "github.com/Agent-Field/backai/services/runtime/internal/storage/adapters/minio"
)

// Config mirrors the MinIO adapter's Config but defaults Secure to true.
type Config struct {
	Endpoint  string // e.g. s3.amazonaws.com or a regional endpoint
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
}

// New returns a minio.Adapter configured for HTTPS S3.
//
// The returned *minio.Adapter implements storage.Storage directly — no
// wrapping needed.
func New(cfg Config) (*miniopkg.Adapter, error) {
	secure := true
	return miniopkg.New(miniopkg.Config{
		Endpoint:  cfg.Endpoint,
		Bucket:    cfg.Bucket,
		AccessKey: cfg.AccessKey,
		SecretKey: cfg.SecretKey,
		Region:    cfg.Region,
		Secure:    &secure,
	})
}