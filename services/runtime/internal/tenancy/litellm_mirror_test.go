// SPDX-License-Identifier: Apache-2.0

package tenancy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// newTestLogger returns a slog.Logger that discards all output. Keeps
// the table-driven tests quiet without losing the ability to debug by
// flipping to os.Stderr.
func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeMirror is a stub LiteLLMMirror that records calls and returns
// preset responses. Used by every tenancy<->LiteLLM unit test below.
type fakeMirror struct {
	mu          sync.Mutex
	configured  bool
	genCalls    []LiteLLMKeyConfig
	deleteCalls []string
	updateCalls []struct {
		alias string
		cfg   LiteLLMKeyConfig
	}
	infoCalls   []string
	spendCalls  []string
	genReturn   LiteLLMKeyInfo
	genErr      error
	infoReturn  LiteLLMKeyInfo
	spendReturn LiteLLMSpend
	deleteErr   error
}

func (f *fakeMirror) Configured() bool { return f.configured }
func (f *fakeMirror) GenerateKey(ctx context.Context, cfg LiteLLMKeyConfig) (LiteLLMKeyInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.genCalls = append(f.genCalls, cfg)
	if f.genErr != nil {
		return LiteLLMKeyInfo{}, f.genErr
	}
	return f.genReturn, nil
}
func (f *fakeMirror) UpdateKey(ctx context.Context, alias string, cfg LiteLLMKeyConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updateCalls = append(f.updateCalls, struct {
		alias string
		cfg   LiteLLMKeyConfig
	}{alias, cfg})
	return nil
}
func (f *fakeMirror) DeleteKey(ctx context.Context, alias string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCalls = append(f.deleteCalls, alias)
	return f.deleteErr
}
func (f *fakeMirror) KeyInfo(ctx context.Context, alias string) (LiteLLMKeyInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.infoCalls = append(f.infoCalls, alias)
	return f.infoReturn, nil
}
func (f *fakeMirror) KeySpend(ctx context.Context, alias string) (LiteLLMSpend, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spendCalls = append(f.spendCalls, alias)
	return f.spendReturn, nil
}

// fakeSink is an in-memory SecretSink stub.
type fakeSink struct {
	mu       sync.Mutex
	stored   map[string][]byte
	putErr   error
	delErr   error
	notFound bool
}

func newFakeSink() *fakeSink { return &fakeSink{stored: map[string][]byte{}} }

func (s *fakeSink) Put(ctx context.Context, tenantID, key string, in SecretPutInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.putErr != nil {
		return s.putErr
	}
	s.stored[tenantID+"|"+key] = []byte(in.Value)
	return nil
}
func (s *fakeSink) Delete(ctx context.Context, tenantID, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.delErr != nil {
		return s.delErr
	}
	delete(s.stored, tenantID+"|"+key)
	return nil
}
func (s *fakeSink) Get(ctx context.Context, tenantID, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.stored[tenantID+"|"+key]
	if !ok || s.notFound {
		return nil, errors.New("secret not found")
	}
	return v, nil
}

func TestLiteLLMAliasFor(t *testing.T) {
	got := LiteLLMAliasFor("tenant-x", "key-y")
	if got != "af-tenant-x-key-y" {
		t.Errorf("alias = %q", got)
	}
}

func TestLiteLLMSecretKey(t *testing.T) {
	got := LiteLLMSecretKey("key-y")
	if got != "litellm/key/key-y" {
		t.Errorf("secret key = %q", got)
	}
	// Must not contain ':' — the vault rejects that character.
	if strings.Contains(got, ":") {
		t.Errorf("secret key uses ':' which the vault rejects")
	}
}

func TestHashLiteLLMSecret(t *testing.T) {
	if got := hashLiteLLMSecret(""); got != "" {
		t.Errorf("empty hash = %q", got)
	}
	a := hashLiteLLMSecret("sk-litellm-abc")
	if len(a) != 64 {
		t.Errorf("hash len = %d, want 64 hex chars", len(a))
	}
	// Determinism.
	if hashLiteLLMSecret("sk-litellm-abc") != a {
		t.Errorf("hash not deterministic")
	}
}

func TestSpend_RemainingUSD(t *testing.T) {
	max := 10.0
	s := LiteLLMSpend{TotalUSD: 3, MaxBudgetUSD: &max}
	if r, ok := s.RemainingUSD(); !ok || r != 7 {
		t.Errorf("Remaining = %v / %v", r, ok)
	}
	s = LiteLLMSpend{TotalUSD: 12, MaxBudgetUSD: &max}
	if r, ok := s.RemainingUSD(); !ok || r != 0 {
		t.Errorf("Remaining over = %v / %v", r, ok)
	}
	s = LiteLLMSpend{TotalUSD: 5}
	if _, ok := s.RemainingUSD(); ok {
		t.Errorf("expected !ok when no budget")
	}
}

func TestWithLiteLLM(t *testing.T) {
	m := &Manager{}
	mirror := &fakeMirror{configured: true}
	sink := newFakeSink()
	m.WithLiteLLM(mirror, sink)
	if m.LiteLLMMirror() != mirror {
		t.Errorf("mirror not stored")
	}
	if m.SecretSink() != sink {
		t.Errorf("sink not stored")
	}
}

func TestIssueLiteLLMKey_HappyPath(t *testing.T) {
	budget := 250.0
	rpm := 60
	mirror := &fakeMirror{
		configured: true,
		genReturn: LiteLLMKeyInfo{
			Key:   "sk-litellm-xyz",
			Alias: "af-tenant-1-key-1",
		},
	}
	sink := newFakeSink()
	m := &Manager{secrets: sink, litellm: mirror, log: nil}
	if m.log == nil {
		// Manager methods rely on m.log; populate with default.
		m.log = newTestLogger()
	}

	alias, hash, err := m.issueLiteLLMKey(context.Background(), "tenant-1", "key-1", IssueAPIKeyInput{
		BudgetMaxUSD: &budget,
		RateLimitRPM: &rpm,
	})
	if err != nil {
		t.Fatalf("issueLiteLLMKey: %v", err)
	}
	if alias != "af-tenant-1-key-1" {
		t.Errorf("alias = %q", alias)
	}
	if hash == "" || len(hash) != 64 {
		t.Errorf("hash = %q", hash)
	}
	// Mirror saw the right config.
	if len(mirror.genCalls) != 1 {
		t.Fatalf("genCalls len = %d", len(mirror.genCalls))
	}
	got := mirror.genCalls[0]
	if got.MaxBudgetUSD != 250 || got.RPMLimit != 60 {
		t.Errorf("mirror saw %+v", got)
	}
	if got.TenantID != "tenant-1" {
		t.Errorf("tenant_id forwarded = %q", got.TenantID)
	}
	// Sink saw the secret stored under the right key.
	stored, err := sink.Get(context.Background(), "tenant-1", LiteLLMSecretKey("key-1"))
	if err != nil {
		t.Fatalf("sink Get: %v", err)
	}
	if string(stored) != "sk-litellm-xyz" {
		t.Errorf("stored secret = %q", stored)
	}
}

func TestIssueLiteLLMKey_NotConfigured(t *testing.T) {
	mirror := &fakeMirror{configured: false}
	m := &Manager{litellm: mirror, log: newTestLogger()}
	alias, hash, err := m.issueLiteLLMKey(context.Background(), "tenant-1", "key-1", IssueAPIKeyInput{})
	if err != nil {
		t.Fatalf("issueLiteLLMKey: %v", err)
	}
	if alias != "" || hash != "" {
		t.Errorf("expected empty alias/hash when not configured")
	}
	if len(mirror.genCalls) != 0 {
		t.Errorf("mirror was called despite Configured()=false")
	}
}

func TestIssueLiteLLMKey_GenerateError(t *testing.T) {
	mirror := &fakeMirror{
		configured: true,
		genErr:     errors.New("rate limited"),
	}
	sink := newFakeSink()
	m := &Manager{litellm: mirror, secrets: sink, log: newTestLogger()}
	_, _, err := m.issueLiteLLMKey(context.Background(), "tenant-1", "key-1", IssueAPIKeyInput{})
	if err == nil {
		t.Fatal("expected error")
	}
	if len(sink.stored) != 0 {
		t.Errorf("vault wrote despite generate failure")
	}
}

func TestIssueLiteLLMKey_VaultErrorRollsBack(t *testing.T) {
	mirror := &fakeMirror{
		configured: true,
		genReturn:  LiteLLMKeyInfo{Key: "sk-litellm-xyz", Alias: "af-1-1"},
	}
	sink := newFakeSink()
	sink.putErr = errors.New("vault is on fire")
	m := &Manager{litellm: mirror, secrets: sink, log: newTestLogger()}
	_, _, err := m.issueLiteLLMKey(context.Background(), "tenant-1", "key-1", IssueAPIKeyInput{})
	if err == nil {
		t.Fatal("expected error")
	}
	// Mirror should have been told to delete the just-created key.
	if len(mirror.deleteCalls) != 1 {
		t.Errorf("delete calls = %d, want 1 (rollback)", len(mirror.deleteCalls))
	}
}

func TestRevokeLiteLLMKey_SkipsWhenNotConfigured(t *testing.T) {
	mirror := &fakeMirror{configured: false}
	sink := newFakeSink()
	_ = sink.Put(context.Background(), "tenant-1", LiteLLMSecretKey("key-1"), SecretPutInput{Value: "x"})
	m := &Manager{litellm: mirror, secrets: sink, log: newTestLogger()}
	m.revokeLiteLLMKey(context.Background(), "tenant-1", "key-1", "af-tenant-1-key-1")
	// Mirror not called (Configured=false).
	if len(mirror.deleteCalls) != 0 {
		t.Errorf("delete called despite Configured()=false")
	}
	// Vault entry still cleared.
	if _, err := sink.Get(context.Background(), "tenant-1", LiteLLMSecretKey("key-1")); err == nil {
		t.Errorf("vault entry should have been deleted")
	}
}

func TestRevokeLiteLLMKey_BestEffort(t *testing.T) {
	mirror := &fakeMirror{
		configured: true,
		deleteErr:  errors.New("upstream 502"),
	}
	sink := newFakeSink()
	m := &Manager{litellm: mirror, secrets: sink, log: newTestLogger()}
	// Should NOT panic or propagate the error.
	m.revokeLiteLLMKey(context.Background(), "tenant-1", "key-1", "af-tenant-1-key-1")
	if len(mirror.deleteCalls) != 1 {
		t.Errorf("delete calls = %d", len(mirror.deleteCalls))
	}
}

func TestLiteLLMKeyForAPIKey_MissingReturnsEmpty(t *testing.T) {
	sink := newFakeSink()
	m := &Manager{secrets: sink, log: newTestLogger()}
	got, err := m.LiteLLMKeyForAPIKey(context.Background(), "tenant-1", "key-missing")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got != "" {
		t.Errorf("got = %q, want empty", got)
	}
}

func TestLiteLLMKeyForAPIKey_PresentReturnsSecret(t *testing.T) {
	sink := newFakeSink()
	_ = sink.Put(context.Background(), "tenant-1", LiteLLMSecretKey("key-1"), SecretPutInput{Value: "sk-litellm-abc"})
	m := &Manager{secrets: sink, log: newTestLogger()}
	got, err := m.LiteLLMKeyForAPIKey(context.Background(), "tenant-1", "key-1")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "sk-litellm-abc" {
		t.Errorf("got = %q", got)
	}
}

func TestLiteLLMKeyForAPIKey_NilManagerOrSink(t *testing.T) {
	// Nil manager.
	var nilm *Manager
	got, err := nilm.LiteLLMKeyForAPIKey(context.Background(), "t", "k")
	if got != "" || err != nil {
		t.Errorf("nil manager: got=%q err=%v", got, err)
	}
	// Manager with no sink.
	m := &Manager{log: newTestLogger()}
	got, err = m.LiteLLMKeyForAPIKey(context.Background(), "t", "k")
	if got != "" || err != nil {
		t.Errorf("no sink: got=%q err=%v", got, err)
	}
}

func TestIsSecretNotFoundErr(t *testing.T) {
	if isSecretNotFoundErr(nil) {
		t.Errorf("nil err should not match")
	}
	if !isSecretNotFoundErr(errors.New("secret not found")) {
		t.Errorf("'secret not found' should match")
	}
	if !isSecretNotFoundErr(errors.New("secrets: not found")) {
		t.Errorf("'secrets: not found' should match")
	}
	if isSecretNotFoundErr(errors.New("unrelated error")) {
		t.Errorf("unrelated err should not match")
	}
}
