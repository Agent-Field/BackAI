// SPDX-License-Identifier: Apache-2.0

package push

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

func TestNew_MissingCredentials(t *testing.T) {
	if _, err := New(context.Background(), Config{AccessToken: "tok"}); err == nil {
		t.Fatal("New() with no project id should error")
	}
	if _, err := New(context.Background(), Config{ProjectID: "proj"}); err == nil {
		t.Fatal("New() with no access token should error")
	}
}

func TestNew_Configured(t *testing.T) {
	a, err := New(context.Background(), Config{ProjectID: "proj", AccessToken: "tok"})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if a.Name() != "fcm" {
		t.Fatalf("Name() = %q, want fcm", a.Name())
	}
	if !a.Configured() {
		t.Fatal("Configured() = false, want true")
	}
}

func TestSend_Success(t *testing.T) {
	var gotPath, gotAuth string
	var gotMsg fcmMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotMsg)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"projects/proj/messages/0:456"}`))
	}))
	defer srv.Close()

	a, err := New(context.Background(), Config{
		ProjectID: "proj", AccessToken: "tok", BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	id, err := a.Send(context.Background(), notifications.Notification{
		Kind:     notifications.KindPush,
		Template: "ignored",
		To:       "device-token-abc",
		Subject:  strPtr("New message"),
		Data:     map[string]any{"body": "You have a reply", "thread": "42"},
	})
	if err != nil {
		t.Fatalf("Send() = %v", err)
	}
	if id != "projects/proj/messages/0:456" {
		t.Fatalf("provider id = %q", id)
	}
	if gotPath != "/v1/projects/proj/messages:send" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("auth = %q, want Bearer tok", gotAuth)
	}
	if gotMsg.Message.Token != "device-token-abc" {
		t.Fatalf("token = %q", gotMsg.Message.Token)
	}
	if gotMsg.Message.Notification == nil ||
		gotMsg.Message.Notification.Title != "New message" ||
		gotMsg.Message.Notification.Body != "You have a reply" {
		t.Fatalf("notification = %+v", gotMsg.Message.Notification)
	}
	if gotMsg.Message.Data["thread"] != "42" {
		t.Fatalf("data = %v, want thread=42", gotMsg.Message.Data)
	}
	if _, ok := gotMsg.Message.Data["body"]; ok {
		t.Fatal("data should not contain the reserved body key")
	}
}

func TestSend_Non2xxUsesFCMMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"status":"NOT_FOUND","message":"Requested entity was not found."}}`))
	}))
	defer srv.Close()

	a, _ := New(context.Background(), Config{ProjectID: "proj", AccessToken: "tok", BaseURL: srv.URL})
	_, err := a.Send(context.Background(), notifications.Notification{
		Template: "hi", To: "stale-token",
	})
	if err == nil {
		t.Fatal("Send() with 404 should error")
	}
	if !strings.Contains(err.Error(), "Requested entity was not found.") {
		t.Fatalf("error = %v, want fcm message", err)
	}
}

func TestSend_MissingToken(t *testing.T) {
	a, _ := New(context.Background(), Config{ProjectID: "proj", AccessToken: "tok"})
	_, err := a.Send(context.Background(), notifications.Notification{Template: "hi", To: "  "})
	if err == nil {
		t.Fatal("Send() with blank To should error")
	}
}
