// Package openapi builds an OpenAPI 3.1 spec by aggregating route
// registrations from every handler.
//
// Why we keep it lightweight
//
// Two competing tools could do this work: code-generation (swag, oapi-codegen)
// or runtime reflection over the Go handler structs. Both pull a heavy
// dependency tree and tie our spec to the in-memory schema shape, which
// hurts at module boundaries (Phase 6 onwards modules ship their own
// route handlers without touching server.go). The Builder API in this
// package is the cheapest interface that lets every handler self-describe:
// a single Register call per route, with a static summary string plus an
// optional response component pointer.
//
// The spec validates as OpenAPI 3.1.0 (a strict superset of 3.0 with
// nullable=null/null-tolerant schemas). The validation surface used by
// the spec test is the JSON-Schema "Meta" subset — we don't pull the
// full upstream validator into the build; we instead assert the bare
// minimum structure the dashboard's openapi consumer relies on.
//
// Thread-safety
//
// Register and Build are safe for concurrent use; we lock for both
// reads and writes so a Build() snapshot during boot can't race a
// late Register() from a lazy-loaded module.
package openapi

import (
	"encoding/json"
	"sort"
	"sync"
)

// Spec is the JSON shape served at /openapi.json. Field names match
// OpenAPI 3.1 verbatim.
type Spec struct {
	OpenAPI    string          `json:"openapi"`
	Info       Info            `json:"info"`
	Servers    []Server        `json:"servers,omitempty"`
	Paths      map[string]Path `json:"paths"`
	Components Components      `json:"components,omitempty"`
	Tags       []Tag           `json:"tags,omitempty"`
}

// Info is the standard OpenAPI Info object.
type Info struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version"`
}

// Server is one entry under spec.servers.
type Server struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// Tag groups operations on the dashboard's API explorer.
type Tag struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Path is one entry under spec.paths — keyed by templated URL.
//
// The operations map (get/post/put/delete/patch) is flattened directly
// into the JSON object via a custom MarshalJSON so the wire shape
// matches the OpenAPI spec exactly (each method is a sibling key under
// the path, not nested under "operations").
type Path struct {
	Operations map[string]Operation
}

// MarshalJSON flattens Operations as direct keys.
func (p Path) MarshalJSON() ([]byte, error) {
	if len(p.Operations) == 0 {
		return []byte("{}"), nil
	}
	out := make(map[string]Operation, len(p.Operations))
	for k, v := range p.Operations {
		out[k] = v
	}
	return json.Marshal(out)
}

// Operation is the spec for one method-on-path.
type Operation struct {
	Summary     string              `json:"summary,omitempty"`
	Description string              `json:"description,omitempty"`
	Tags        []string            `json:"tags,omitempty"`
	Parameters  []Parameter         `json:"parameters,omitempty"`
	Responses   map[string]Response `json:"responses,omitempty"`
}

// Parameter is a query / path / header param descriptor.
type Parameter struct {
	Name        string         `json:"name"`
	In          string         `json:"in"` // path, query, header, cookie
	Description string         `json:"description,omitempty"`
	Required    bool           `json:"required,omitempty"`
	Schema      map[string]any `json:"schema,omitempty"`
}

// Response describes one HTTP response code.
type Response struct {
	Description string               `json:"description"`
	Content     map[string]MediaType `json:"content,omitempty"`
}

// MediaType describes the body of a response (or request).
type MediaType struct {
	Schema map[string]any `json:"schema,omitempty"`
}

// Components holds reusable schemas. We seed the standard error
// envelope so every handler can $ref to it instead of redefining the
// shape inline.
type Components struct {
	Schemas map[string]any `json:"schemas,omitempty"`
}

// RouteMeta is what handlers pass to Register. Keep it small — adding
// richer schema later doesn't require touching call sites.
type RouteMeta struct {
	// Summary is a one-line description that shows up in the operation
	// list (e.g. "Discover registered AgentField agents").
	Summary string
	// Description is optional long-form (Markdown allowed by spec).
	Description string
	// Tags group operations on the API explorer ("agents", "storage",
	// "secrets", "jobs", "dashboard").
	Tags []string
	// Parameters list path + query params. nil OK.
	Parameters []Parameter
	// OkResponseRef is the component-schema name the 200 (or 201) body
	// $refs. Empty means a generic "OK" response.
	OkResponseRef string
	// OkDescription overrides the default "OK" string for the 2xx response.
	OkDescription string
	// SuppressOK removes the default 2xx response entry (use for 204 /
	// streaming routes where the body shape is binary).
	SuppressOK bool
}

// Builder accumulates Register() calls and produces a Spec via Build().
type Builder struct {
	mu      sync.RWMutex
	info    Info
	servers []Server
	tags    []Tag
	paths   map[string]map[string]Operation // path -> method -> op
	schemas map[string]any
}

// NewBuilder constructs a Builder. title and version are required (per
// OpenAPI 3.1 validation rules); pass them at construction so we can't
// emit a malformed spec by forgetting.
func NewBuilder(title, version string) *Builder {
	b := &Builder{
		info:    Info{Title: title, Version: version},
		paths:   make(map[string]map[string]Operation),
		schemas: make(map[string]any),
	}
	// Seed the standard error envelope schema. Handlers can $ref it.
	b.schemas["ErrorEnvelope"] = map[string]any{
		"type":     "object",
		"required": []string{"error"},
		"properties": map[string]any{
			"error": map[string]any{
				"type":     "object",
				"required": []string{"code", "message"},
				"properties": map[string]any{
					"code":    map[string]any{"type": "string"},
					"message": map[string]any{"type": "string"},
					"details": map[string]any{
						"description": "Free-form per-code detail payload",
					},
				},
			},
		},
	}
	return b
}

// AddServer registers a base URL. Optional — the spec validates without
// servers, but the dashboard's API explorer prefers having one.
func (b *Builder) AddServer(url, description string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.servers = append(b.servers, Server{URL: url, Description: description})
}

// AddTag groups operations on the spec explorer.
func (b *Builder) AddTag(name, description string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tags = append(b.tags, Tag{Name: name, Description: description})
}

// SetDescription sets the Info.Description field.
func (b *Builder) SetDescription(desc string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.info.Description = desc
}

// AddSchema registers a reusable component schema. Existing entries
// with the same key are overwritten (last-write-wins lets test setups
// override the seeded ErrorEnvelope when they need to).
func (b *Builder) AddSchema(name string, schema map[string]any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.schemas[name] = schema
}

// Register adds one method-on-path to the spec.
//
// method is the lowercase HTTP method (per OpenAPI convention: "get",
// "post", "put", "delete", "patch"). Callers may pass uppercase; we
// normalise to lower.
// path is the templated URL using OpenAPI braces ({id} not Go's {id...}).
//
// Calls with an empty method or path are silently ignored so we can
// register routes inside a loop without guarding every entry.
func (b *Builder) Register(method, path string, meta RouteMeta) {
	if method == "" || path == "" {
		return
	}
	method = normaliseMethod(method)
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.paths[path]; !ok {
		b.paths[path] = make(map[string]Operation)
	}

	op := Operation{
		Summary:     meta.Summary,
		Description: meta.Description,
		Tags:        meta.Tags,
		Parameters:  meta.Parameters,
		Responses:   make(map[string]Response),
	}

	// Default 2xx response unless explicitly suppressed.
	if !meta.SuppressOK {
		okDesc := meta.OkDescription
		if okDesc == "" {
			okDesc = "OK"
		}
		resp := Response{Description: okDesc}
		if meta.OkResponseRef != "" {
			resp.Content = map[string]MediaType{
				"application/json": {
					Schema: map[string]any{"$ref": "#/components/schemas/" + meta.OkResponseRef},
				},
			}
		}
		op.Responses["200"] = resp
	}

	// Standard error responses — every handler in this codebase uses
	// the same envelope shape, so we describe it once here rather than
	// in each Register call.
	for _, code := range []string{"400", "401", "403", "404", "429", "500", "502", "503"} {
		op.Responses[code] = Response{
			Description: defaultErrorDesc(code),
			Content: map[string]MediaType{
				"application/json": {
					Schema: map[string]any{"$ref": "#/components/schemas/ErrorEnvelope"},
				},
			},
		}
	}

	b.paths[path][method] = op
}

// Build returns a deep-enough snapshot of the spec for JSON
// serialisation. The returned Spec is safe to marshal even while
// Register continues to mutate the Builder.
func (b *Builder) Build() Spec {
	b.mu.RLock()
	defer b.mu.RUnlock()

	paths := make(map[string]Path, len(b.paths))
	for p, ops := range b.paths {
		copied := make(map[string]Operation, len(ops))
		for m, op := range ops {
			copied[m] = op
		}
		paths[p] = Path{Operations: copied}
	}

	schemas := make(map[string]any, len(b.schemas))
	for k, v := range b.schemas {
		schemas[k] = v
	}

	// Sort tags + servers so spec output is deterministic for the test
	// suite and HTTP caching.
	tags := append([]Tag(nil), b.tags...)
	sort.SliceStable(tags, func(i, j int) bool { return tags[i].Name < tags[j].Name })
	servers := append([]Server(nil), b.servers...)

	return Spec{
		OpenAPI:    "3.1.0",
		Info:       b.info,
		Servers:    servers,
		Paths:      paths,
		Components: Components{Schemas: schemas},
		Tags:       tags,
	}
}

// PathCount returns the number of registered URL templates. Useful
// for ops dashboards + the spec-coverage test.
func (b *Builder) PathCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.paths)
}

// ─── Package-level default builder ────────────────────────────────────────
//
// Some early handler files call openapi.Register at init or registration
// time — that pre-dates the Builder API. We keep the package-level
// Register as a thin shim onto a default Builder so those call sites
// keep working. Server code that constructs its own Builder via
// NewBuilder() should call Builder.Register / Builder.Build directly.

var defaultBuilder = NewBuilder("AF Stack", "0.1.0")

// Default returns the package-level Builder. Tests can call
// ResetDefault() to clear state between cases. Server code is free to
// build its own instance instead; the default exists purely so legacy
// init-time openapi.Register(...) calls don't lose their data.
func Default() *Builder { return defaultBuilder }

// Register forwards to the default Builder. Equivalent to
// Default().Register(method, path, meta).
func Register(method, path string, meta RouteMeta) {
	defaultBuilder.Register(method, path, meta)
}

// ResetDefault replaces the default Builder with a fresh one. Tests
// only; calling at runtime would silently drop every Register call so
// far.
func ResetDefault() { defaultBuilder = NewBuilder("AF Stack", "0.1.0") }

// normaliseMethod folds the HTTP verb to the lowercase form OpenAPI
// expects in the operation object.
func normaliseMethod(method string) string {
	switch method {
	case "GET", "get":
		return "get"
	case "POST", "post":
		return "post"
	case "PUT", "put":
		return "put"
	case "DELETE", "delete":
		return "delete"
	case "PATCH", "patch":
		return "patch"
	case "OPTIONS", "options":
		return "options"
	case "HEAD", "head":
		return "head"
	}
	// Lowercase anything we didn't anticipate.
	out := make([]byte, len(method))
	for i := 0; i < len(method); i++ {
		c := method[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}

// defaultErrorDesc maps an error status code to a human-readable label
// the dashboard renders in the API explorer.
func defaultErrorDesc(code string) string {
	switch code {
	case "400":
		return "Bad Request — validation failed"
	case "401":
		return "Unauthorised — missing or invalid credentials"
	case "403":
		return "Forbidden — request rejected by policy"
	case "404":
		return "Not Found"
	case "429":
		return "Rate Limited — see Retry-After header"
	case "500":
		return "Internal Server Error"
	case "502":
		return "Bad Gateway — upstream agent unreachable"
	case "503":
		return "Service Unavailable — required module not configured"
	}
	return "Error"
}
