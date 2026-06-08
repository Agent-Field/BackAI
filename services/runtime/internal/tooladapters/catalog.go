// SPDX-License-Identifier: Apache-2.0

package tooladapters

func Catalog(config Config) []AdapterDefinition {
	defs := []AdapterDefinition{
		{
			ID: AdapterBrowserUse, Label: "browser-use",
			Description: "Drive a configured browser-use worker for web automation.",
			Configured:  config.BrowserUseURL != "", DefaultEnabled: false,
			Tools: []ToolDefinition{{
				Name: "run", Description: "Run one browser automation task through the browser-use worker.",
				InputSchema: objectSchema(map[string]any{
					"task":      map[string]any{"type": "string"},
					"timeout_s": map[string]any{"type": "integer", "minimum": 1, "maximum": 300},
				}, []string{"task"}),
			}},
		},
		{
			ID: AdapterSearXNG, Label: "SearXNG",
			Description: "Search the web through a configured SearXNG instance.",
			Configured:  config.SearXNGURL != "", DefaultEnabled: false,
			Tools: []ToolDefinition{{
				Name: "search", Description: "Query SearXNG and return JSON search results.",
				InputSchema: objectSchema(map[string]any{
					"q":          map[string]any{"type": "string"},
					"categories": map[string]any{"type": "string"},
					"language":   map[string]any{"type": "string"},
					"time_range": map[string]any{"type": "string"},
				}, []string{"q"}),
			}},
		},
		{
			ID: AdapterFS, Label: "Filesystem",
			Description: "Operate on an ephemeral sandbox workspace, never the host filesystem.",
			Configured:  config.SandboxAvailable, DefaultEnabled: false,
			Tools: []ToolDefinition{
				{
					Name: "read", Description: "Read a file from caller-provided sandbox files.",
					InputSchema: objectSchema(map[string]any{
						"path":  map[string]any{"type": "string"},
						"files": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
					}, []string{"path", "files"}),
				},
				{
					Name: "write", Description: "Validate/write content into an ephemeral sandbox path and echo metadata.",
					InputSchema: objectSchema(map[string]any{
						"path":    map[string]any{"type": "string"},
						"content": map[string]any{"type": "string"},
					}, []string{"path", "content"}),
				},
			},
		},
		{
			ID: AdapterExec, Label: "Exec",
			Description: "Run short-lived commands through the configured sandbox adapter.",
			Configured:  config.SandboxAvailable, DefaultEnabled: false,
			Tools: []ToolDefinition{{
				Name: "run", Description: "Execute a sandboxed command.",
				InputSchema: objectSchema(map[string]any{
					"image":        map[string]any{"type": "string"},
					"command":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"files":        map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
					"env":          map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
					"timeout_s":    map[string]any{"type": "integer", "minimum": 1, "maximum": 300},
					"network":      map[string]any{"type": "string", "enum": []string{"isolated", "restricted", "open"}},
					"allow_egress": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				}, []string{"command"}),
			}},
		},
		{
			ID: AdapterHTTP, Label: "HTTP",
			Description: "Make outbound HTTP calls with SSRF protection.",
			Configured:  true, DefaultEnabled: true,
			Tools: []ToolDefinition{{
				Name: "request", Description: "Perform one JSON/text HTTP request.",
				InputSchema: objectSchema(map[string]any{
					"method":    map[string]any{"type": "string"},
					"url":       map[string]any{"type": "string"},
					"headers":   map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
					"body":      map[string]any{},
					"timeout_s": map[string]any{"type": "integer", "minimum": 1, "maximum": 60},
				}, []string{"url"}),
			}},
		},
		{
			ID: AdapterSQL, Label: "SQL",
			Description: "Run guarded read-only SQL against the suite Postgres database.",
			Configured:  config.DatabaseAvailable, DefaultEnabled: false,
			Tools: []ToolDefinition{{
				Name: "query", Description: "Run a read-only SELECT/WITH/EXPLAIN query with a row cap.",
				InputSchema: objectSchema(map[string]any{
					"sql":      map[string]any{"type": "string"},
					"args":     map[string]any{"type": "array"},
					"max_rows": map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
				}, []string{"sql"}),
			}},
		},
	}
	return defs
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}
