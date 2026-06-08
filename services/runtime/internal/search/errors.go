// SPDX-License-Identifier: Apache-2.0

package search

import "errors"

var (
	ErrNotFound              = errors.New("search: document not found")
	ErrValidation            = errors.New("search: validation failed")
	ErrTenantRequired        = errors.New("search: tenant required")
	ErrEmbedderNotConfigured = errors.New("search: embedder not configured")
	ErrEmbeddingDimMismatch  = errors.New("search: embedding dimension mismatch")
)
