// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/llmgateway"
	"github.com/Agent-Field/backai/services/runtime/internal/secrets"
	"github.com/Agent-Field/backai/services/runtime/internal/tenancy"
)

// integrationCredTenant is the tenant scope the operator Integrations
// settings API (internal/server/admin_integrations.go) stores adapter
// credentials under. Boot-time cred resolution MUST read the same scope so
// a value entered in the dashboard is picked up by the factories here.
const integrationCredTenant = tenancy.DefaultTenantID

// credGetter is the subset of *secrets.Vault resolveCred needs. Declared as
// an interface so the resolution logic is unit-testable with a fake.
type credGetter interface {
	Get(ctx context.Context, tenantID, key string) ([]byte, error)
}

// resolveCred returns the credential for an adapter slot field, preferring an
// explicit environment value and falling back to the operator-managed
// integration credential in the secrets vault.
//
// Resolution order:
//  1. envVal, trimmed, if non-empty — env always wins.
//  2. the vault secret at key "integration/{slot}/{field}" (the convention
//     written by admin_integrations.go; see that file for why the separator
//     is '/').
//
// It is deliberately defensive: a nil vault (secrets not configured), a vault
// miss, or any lookup error all yield "" rather than an error — a missing
// credential is a "not configured" state the caller handles, not a fatal.
func resolveCred(vault *secrets.Vault, slot, field, envVal string) string {
	// Guard the typed-nil trap: a nil *secrets.Vault boxed into a credGetter
	// is a non-nil interface, so decide nil-ness on the concrete type here.
	if vault == nil {
		return resolveCredFrom(nil, slot, field, envVal)
	}
	return resolveCredFrom(vault, slot, field, envVal)
}

func resolveCredFrom(g credGetter, slot, field, envVal string) string {
	if v := strings.TrimSpace(envVal); v != "" {
		return v
	}
	if g == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, err := g.Get(ctx, integrationCredTenant, "integration/"+slot+"/"+field)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// chatOnlyProviderClient lifts an llmgateway.Provider (chat + embeddings only)
// into the gateway's ProviderClient by returning a clear "unsupported" error
// for the multimodal verbs (image generation / speech / transcription).
//
// Remote llm-chat adapters implement the narrower Provider interface; the
// multimodal verbs live on a separate adapter slot, so a remote chat backend
// simply doesn't serve them. Wrapping keeps the gateway's single-provider
// constructor intact while surfacing an honest error on those routes.
type chatOnlyProviderClient struct {
	llmgateway.Provider
}

func (c chatOnlyProviderClient) Images(context.Context, llmgateway.ImagesRequest) (llmgateway.ImagesResponse, error) {
	return llmgateway.ImagesResponse{}, fmt.Errorf("llm gateway: adapter %q does not support image generation", c.Name())
}

func (c chatOnlyProviderClient) AudioSpeech(context.Context, llmgateway.AudioSpeechRequest) (llmgateway.RawResponse, error) {
	return llmgateway.RawResponse{}, fmt.Errorf("llm gateway: adapter %q does not support audio speech", c.Name())
}

func (c chatOnlyProviderClient) AudioTranscription(context.Context, llmgateway.AudioTranscriptionRequest) (llmgateway.RawResponse, error) {
	return llmgateway.RawResponse{}, fmt.Errorf("llm gateway: adapter %q does not support audio transcription", c.Name())
}
