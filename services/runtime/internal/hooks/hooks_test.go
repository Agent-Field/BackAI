// SPDX-License-Identifier: Apache-2.0

package hooks

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestRegisterRejectsNil(t *testing.T) {
	e := NewEngine(slog.Default())
	if err := e.Register(HookLLMPreCall, nil); err == nil {
		t.Error("expected nil handler to be rejected")
	}
	if err := e.Register("", func(ctx context.Context, p any) (any, error) { return p, nil }); err == nil {
		t.Error("expected empty point to be rejected")
	}
}

func TestFireWithNoHandlersReturnsPayloadUnchanged(t *testing.T) {
	e := NewEngine(slog.Default())
	out, err := e.Fire(context.Background(), HookLLMPreCall, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello" {
		t.Errorf("expected unchanged payload, got %v", out)
	}
}

func TestChainOfHandlersMutatesPayload(t *testing.T) {
	e := NewEngine(slog.Default())
	_ = e.Register(HookLLMPreCall, func(_ context.Context, p any) (any, error) {
		return p.(string) + "-1", nil
	})
	_ = e.Register(HookLLMPreCall, func(_ context.Context, p any) (any, error) {
		return p.(string) + "-2", nil
	})
	_ = e.Register(HookLLMPreCall, func(_ context.Context, p any) (any, error) {
		return p.(string) + "-3", nil
	})
	out, err := e.Fire(context.Background(), HookLLMPreCall, "x")
	if err != nil {
		t.Fatal(err)
	}
	if out.(string) != "x-1-2-3" {
		t.Errorf("expected chain mutation, got %q", out)
	}
}

func TestErrorShortCircuitsChain(t *testing.T) {
	e := NewEngine(slog.Default())
	called := []int{}
	_ = e.Register(HookLLMPreCall, func(_ context.Context, p any) (any, error) {
		called = append(called, 1)
		return p, nil
	})
	_ = e.Register(HookLLMPreCall, func(_ context.Context, p any) (any, error) {
		called = append(called, 2)
		return p, errors.New("policy violation")
	})
	_ = e.Register(HookLLMPreCall, func(_ context.Context, p any) (any, error) {
		called = append(called, 3)
		return p, nil
	})
	_, err := e.Fire(context.Background(), HookLLMPreCall, "x")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "policy violation") {
		t.Errorf("expected wrapped error, got %v", err)
	}
	if len(called) != 2 {
		t.Errorf("expected handler 3 not to run, called=%v", called)
	}
}

func TestCountAndPoints(t *testing.T) {
	e := NewEngine(slog.Default())
	_ = e.Register(HookLLMPreCall, func(_ context.Context, p any) (any, error) { return p, nil })
	_ = e.Register(HookLLMPreCall, func(_ context.Context, p any) (any, error) { return p, nil })
	_ = e.Register(HookSandboxPreRun, func(_ context.Context, p any) (any, error) { return p, nil })

	if e.Count(HookLLMPreCall) != 2 {
		t.Errorf("expected 2 LLM handlers, got %d", e.Count(HookLLMPreCall))
	}
	if e.Count(HookGatewayPreAuth) != 0 {
		t.Errorf("expected 0 gateway handlers, got %d", e.Count(HookGatewayPreAuth))
	}
	pts := e.Points()
	if len(pts) != 2 {
		t.Errorf("expected 2 points, got %d", len(pts))
	}
}