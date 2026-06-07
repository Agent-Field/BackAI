// SPDX-License-Identifier: Apache-2.0

// Package observability wires OpenTelemetry traces + Prometheus metrics
// for the suite runtime.
//
// Traces export via OTLP gRPC to the configured collector. Metrics expose
// at /metrics for Prometheus scraping (mounted into the runtime server).
//
// When OTel is disabled or the collector is unreachable, the runtime keeps
// running — no observability is non-fatal.
package observability

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	tracesdk "go.opentelemetry.io/otel/trace"
)

// Config holds OTel + metrics settings.
type Config struct {
	OTLPEndpoint string // e.g. otel-collector:4317; empty disables tracing
	ServiceName  string
	Version      string
}

// Telemetry holds the wired OTel state. Returned from Setup so main can
// defer Shutdown.
type Telemetry struct {
	TracerProvider *sdktrace.TracerProvider
	Registry       *prometheus.Registry
}

// Setup initialises OTel traces and a Prometheus registry. If OTLPEndpoint
// is empty, tracing is set up with a no-op exporter but propagation still
// works.
func Setup(ctx context.Context, cfg Config) (*Telemetry, error) {
	if cfg.ServiceName == "" {
		cfg.ServiceName = "af-stack"
	}

	// Resource attributes describe what this binary is.
	// Don't merge with resource.Default() (different schema URL); instead
	// construct from scratch with our chosen semconv schema.
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.Version),
	)

	tp, err := buildTracerProvider(ctx, cfg.OTLPEndpoint, res)
	if err != nil {
		return nil, err
	}

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Prometheus registry with Go runtime + process collectors.
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	return &Telemetry{TracerProvider: tp, Registry: reg}, nil
}

func buildTracerProvider(ctx context.Context, endpoint string, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	var opts []sdktrace.TracerProviderOption
	opts = append(opts, sdktrace.WithResource(res))

	if endpoint != "" {
		// gRPC OTLP exporter. Use insecure for local collectors; TLS lands later.
		exp, err := otlptrace.New(ctx, otlptracegrpc.NewClient(
			otlptracegrpc.WithEndpoint(endpoint),
			otlptracegrpc.WithInsecure(),
		))
		if err != nil {
			return nil, fmt.Errorf("observability: OTLP exporter: %w", err)
		}
		opts = append(opts, sdktrace.WithBatcher(exp))
	}

	return sdktrace.NewTracerProvider(opts...), nil
}

// Shutdown flushes pending spans. Bounded by the provided context.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t == nil || t.TracerProvider == nil {
		return nil
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return t.TracerProvider.Shutdown(shutdownCtx)
}

// MetricsHandler returns the http.Handler that serves /metrics. Mount on
// the suite server with `mux.Handle("GET /metrics", t.MetricsHandler())`.
func (t *Telemetry) MetricsHandler() http.Handler {
	if t == nil || t.Registry == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "metrics not configured", http.StatusServiceUnavailable)
		})
	}
	return promhttp.HandlerFor(t.Registry, promhttp.HandlerOpts{})
}

// TraceMiddleware wraps an http.Handler with OTel span creation per request.
// Spans inherit the incoming `traceparent` header for distributed tracing.
func TraceMiddleware(serviceName string) func(http.Handler) http.Handler {
	tracer := otel.Tracer(serviceName)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			propagator := otel.GetTextMapPropagator()
			ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			ctx, span := tracer.Start(ctx, fmt.Sprintf("%s %s", r.Method, r.URL.Path),
				tracesdk.WithSpanKind(tracesdk.SpanKindServer),
				tracesdk.WithAttributes(
					attribute.String("http.method", r.Method),
					attribute.String("http.path", r.URL.Path),
				),
			)
			defer span.End()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}