// SPDX-License-Identifier: Apache-2.0

package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/notifications"
)

func strPtr(s string) *string { return &s }

func TestNew_MissingWebhook(t *testing.T) {
	_, err := New(context.Background(), Config{WebhookURL: "  "})
	if err == nil {
		t.Fatal("New() with blank webhook should error")
	}
}

func TestNew_Configured(t *testing.T) {
	a, err := New(context.Background(), Config{WebhookURL: "https://hooks.slack.com/services/x"})
	if err != nil {
		t.Fatalf("New() = %v, want nil", err)
	}
	if a.Name() != "slack" {
		t.Fatalf("Name() = %q, want slack", a.Name())
	}
	if !a.Configured() {
		t.Fatal("Configured() = false, want true")
	}
}

func TestSend_Success(t *testing.T) {
	var got slackRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q, want application/json", ct)
		}
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &got); err != nil {
			t.Errorf("bad body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	a, err := New(context.Background(), Config{WebhookURL: srv.URL})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	id, err := a.Send(context.Background(), notifications.Notification{
		Kind:     notifications.KindLog,
		Template: "ignored",
		To:       "#alerts",
		Subject:  strPtr("Deploy finished"),
		Data:     map[string]any{"body": "v1.2.3 is live"},
	})
	if err != nil {
		t.Fatalf("Send() = %v, want nil", err)
	}
	if id != "" {
		t.Fatalf("provider id = %q, want empty (webhooks have no id)", id)
	}
	if !strings.Contains(got.Text, "Deploy finished") || !strings.Contains(got.Text, "v1.2.3 is live") {
		t.Fatalf("text = %q, want subject and body folded in", got.Text)
	}
}

func TestSend_BodyFallsBackToTemplate(t *testing.T) {
	var got slackRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a, _ := New(context.Background(), Config{WebhookURL: srv.URL})
	_, err := a.Send(context.Background(), notifications.Notification{
		Template: "raw template text",
		To:       "#alerts",
	})
	if err != nil {
		t.Fatalf("Send() = %v", err)
	}
	if got.Text != "raw template text" {
		t.Fatalf("text = %q, want template fallback", got.Text)
	}
}

func TestSend_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid_payload"))
	}))
	defer srv.Close()

	a, _ := New(context.Background(), Config{WebhookURL: srv.URL})
	_, err := a.Send(context.Background(), notifications.Notification{
		Template: "hi", To: "#alerts",
	})
	if err == nil {
		t.Fatal("Send() with 400 should error")
	}
	if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "invalid_payload") {
		t.Fatalf("error = %v, want status + body", err)
	}
}

func TestSend_EmptyText(t *testing.T) {
	a, _ := New(context.Background(), Config{WebhookURL: "https://hooks.slack.com/services/x"})
	_, err := a.Send(context.Background(), notifications.Notification{To: "#alerts"})
	if err == nil {
		t.Fatal("Send() with no subject/body/template should error")
	}
}
