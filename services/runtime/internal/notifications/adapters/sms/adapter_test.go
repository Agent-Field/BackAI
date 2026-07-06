// SPDX-License-Identifier: Apache-2.0

package sms

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/backai/services/runtime/internal/notifications"
)

func TestNew_MissingCredentials(t *testing.T) {
	cases := []Config{
		{AuthToken: "t", FromNumber: "+1"},
		{AccountSID: "AC1", FromNumber: "+1"},
		{AccountSID: "AC1", AuthToken: "t"},
	}
	for i, cfg := range cases {
		if _, err := New(context.Background(), cfg); err == nil {
			t.Fatalf("case %d: New() should error on missing credential", i)
		}
	}
}

func TestNew_Configured(t *testing.T) {
	a, err := New(context.Background(), Config{AccountSID: "AC1", AuthToken: "tok", FromNumber: "+15550001111"})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if a.Name() != "twilio" {
		t.Fatalf("Name() = %q, want twilio", a.Name())
	}
	if !a.Configured() {
		t.Fatal("Configured() = false, want true")
	}
}

func TestSend_Success(t *testing.T) {
	var gotPath, gotUser, gotPass, gotTo, gotFrom, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotUser, gotPass, _ = r.BasicAuth()
		_ = r.ParseForm()
		gotTo = r.PostForm.Get("To")
		gotFrom = r.PostForm.Get("From")
		gotBody = r.PostForm.Get("Body")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"sid":"SM123","status":"queued"}`))
	}))
	defer srv.Close()

	a, err := New(context.Background(), Config{
		AccountSID: "AC1", AuthToken: "tok", FromNumber: "+15550001111", BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	id, err := a.Send(context.Background(), notifications.Notification{
		Kind:     notifications.KindSMS,
		Template: "ignored",
		To:       "+15557778888",
		Data:     map[string]any{"body": "your code is 4242"},
	})
	if err != nil {
		t.Fatalf("Send() = %v", err)
	}
	if id != "SM123" {
		t.Fatalf("provider id = %q, want SM123", id)
	}
	if gotPath != "/2010-04-01/Accounts/AC1/Messages.json" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotUser != "AC1" || gotPass != "tok" {
		t.Fatalf("basic auth = %q:%q, want AC1:tok", gotUser, gotPass)
	}
	if gotTo != "+15557778888" || gotFrom != "+15550001111" || gotBody != "your code is 4242" {
		t.Fatalf("form To=%q From=%q Body=%q", gotTo, gotFrom, gotBody)
	}
}

func TestSend_Non2xxUsesTwilioMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":21211,"message":"Invalid 'To' Phone Number"}`))
	}))
	defer srv.Close()

	a, _ := New(context.Background(), Config{
		AccountSID: "AC1", AuthToken: "tok", FromNumber: "+1", BaseURL: srv.URL,
	})
	_, err := a.Send(context.Background(), notifications.Notification{
		Template: "hi", To: "bad",
	})
	if err == nil {
		t.Fatal("Send() with 400 should error")
	}
	if !strings.Contains(err.Error(), "Invalid 'To' Phone Number") {
		t.Fatalf("error = %v, want twilio message", err)
	}
}

func TestSend_MissingRecipient(t *testing.T) {
	a, _ := New(context.Background(), Config{AccountSID: "AC1", AuthToken: "t", FromNumber: "+1"})
	_, err := a.Send(context.Background(), notifications.Notification{Template: "hi", To: "  "})
	if err == nil {
		t.Fatal("Send() with blank To should error")
	}
}

func TestSend_MissingBody(t *testing.T) {
	a, _ := New(context.Background(), Config{AccountSID: "AC1", AuthToken: "t", FromNumber: "+1"})
	_, err := a.Send(context.Background(), notifications.Notification{To: "+15557778888"})
	if err == nil {
		t.Fatal("Send() with no body/template should error")
	}
}
