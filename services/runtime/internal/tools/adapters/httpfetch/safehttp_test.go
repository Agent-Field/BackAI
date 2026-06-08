// SPDX-License-Identifier: Apache-2.0

package httpfetch

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAdapter_ConfiguredAndID(t *testing.T) {
	a := New(5 * time.Second)
	if !a.Configured() {
		t.Errorf("Configured() = false, want true")
	}
	if a.ID() != "safehttp" {
		t.Errorf("ID = %q, want safehttp", a.ID())
	}
}

func TestAdapter_GetEmptyURL(t *testing.T) {
	a := New(0)
	_, err := a.Get(context.Background(), "", nil)
	if !errors.Is(err, ErrInvalidArgs) {
		t.Errorf("Get empty url: err = %v, want ErrInvalidArgs", err)
	}
}

func TestAdapter_GetPrivateBlocked(t *testing.T) {
	a := New(0)
	// 169.254.169.254 is the cloud metadata IP — safehttp must block.
	_, err := a.Get(context.Background(), "http://169.254.169.254/latest/meta-data/", nil)
	if err == nil {
		t.Errorf("expected safehttp block on metadata IP, got no error")
	}
}
