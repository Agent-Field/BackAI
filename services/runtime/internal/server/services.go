// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	adapterregistry "github.com/Agent-Field/backai/services/runtime/internal/adapters/registry"
	"github.com/Agent-Field/backai/services/runtime/internal/openapi"
)

type adminService struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Kind     string  `json:"kind"`
	Status   string  `json:"status"`
	Version  string  `json:"version,omitempty"`
	Host     string  `json:"host,omitempty"`
	Port     *int    `json:"port"`
	AdminURL *string `json:"admin_url"`
	Purpose  string  `json:"purpose"`
	Checked  string  `json:"checked_at"`
}

type adminServicesResponse struct {
	Services []adminService `json:"services"`
}

func (s *Server) registerAdminServicesRoutes() {
	s.mux.HandleFunc("GET /api/v1/admin/services", s.handleAdminServices)
}

func (s *Server) registerAdminServicesOpenAPI() {
	s.openapi.Register("GET", "/api/v1/admin/services", openapi.RouteMeta{
		Summary: "List connected backing services", Tags: []string{"admin"},
	})
}

func (s *Server) handleAdminServices(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	checked := time.Now().UTC().Format(time.RFC3339Nano)
	services := []adminService{
		{
			ID:      "runtime",
			Name:    "BackAI runtime",
			Kind:    "runtime",
			Status:  "healthy",
			Version: s.version,
			Host:    strings.TrimPrefix(s.cfg.Server.HTTPAddr, ":"),
			Port:    intPtr(portFromAddr(s.cfg.Server.HTTPAddr, 8080)),
			Purpose: "runtime API and admin middleware",
			Checked: checked,
		},
	}

	dbStatus := "offline"
	dbVersion := ""
	if s.db != nil && s.db.Pool != nil {
		if err := s.db.Health(ctx); err == nil {
			dbStatus = "healthy"
		} else {
			dbStatus = "degraded"
		}
		_ = s.db.Pool.QueryRow(ctx, `select current_setting('server_version', true)`).Scan(&dbVersion)
	}
	services = append(services, adminService{
		ID:      "postgres",
		Name:    "Postgres",
		Kind:    "data",
		Status:  dbStatus,
		Version: dbVersion,
		Host:    hostFromDatabaseURL(s.cfg.Database.URL),
		Port:    intPtr(portFromDatabaseURL(s.cfg.Database.URL, 5432)),
		Purpose: "primary database",
		Checked: checked,
	})

	if s.adapterRegistry != nil {
		for _, slot := range s.adapterRegistry.List(ctx).Slots {
			if svc, ok := serviceFromSlot(slot, checked); ok {
				services = append(services, svc)
			}
		}
	}

	services = appendEnvService(services, "loki", "Loki", "observability/logs", "AF_STACK_LOGS_LOKI_URL", "", "log store", checked)
	services = appendEnvService(services, "tempo", "Tempo", "observability/traces", "AF_STACK_TRACES_TEMPO_URL", "", "trace store", checked)
	services = appendEnvService(services, "prometheus", "Prometheus", "observability/metrics", "AF_STACK_METRICS_PROMETHEUS_URL", "AF_STACK_PROMETHEUS_UI_URL", "metrics store", checked)
	services = appendEnvService(services, "glitchtip", "GlitchTip", "observability/errors", "AF_STACK_ERRORS_GLITCHTIP_URL", "", "error tracker", checked)
	services = appendEnvService(services, "grafana", "Grafana", "observability/charts", "AF_STACK_GRAFANA_URL", "AF_STACK_GRAFANA_URL", "charts and dashboards", checked)

	writeJSON(w, http.StatusOK, adminServicesResponse{Services: services})
}

func serviceFromSlot(slot adapterregistry.SlotView, checked string) (adminService, bool) {
	purposeBySlot := map[string]string{
		"llm-chat":      "LLM provider routing",
		"storage":       "object storage",
		"reasoning":     "agent execution and reasoner graph",
		"webhooks":      "outbound webhook delivery",
		"notifications": "notification delivery",
		"billing":       "billing adapter",
		"job-queue":     "background jobs",
	}
	nameBySlot := map[string]string{
		"llm-chat":      "LiteLLM",
		"storage":       "Object storage",
		"reasoning":     "AgentField",
		"webhooks":      "Svix",
		"notifications": "Notifications",
		"billing":       "Billing",
		"job-queue":     "River",
	}
	kindBySlot := map[string]string{
		"llm-chat":      "llm-gateway",
		"storage":       "storage",
		"reasoning":     "reasoning",
		"webhooks":      "webhooks",
		"notifications": "notifications",
		"billing":       "billing",
		"job-queue":     "queue",
	}
	purpose, ok := purposeBySlot[slot.Slot]
	if !ok {
		return adminService{}, false
	}
	name := nameBySlot[slot.Slot]
	adminURL := nullableURL(slot.AdminUI)
	host, port := hostPortFromURL(slot.AdminUI)
	return adminService{
		ID:       slot.Slot,
		Name:     name,
		Kind:     kindBySlot[slot.Slot],
		Status:   serviceStatus(slot.Active.Status),
		Version:  slot.Active.Version,
		Host:     host,
		Port:     nullablePort(port),
		AdminURL: adminURL,
		Purpose:  purpose,
		Checked:  checked,
	}, true
}

func appendEnvService(in []adminService, id, name, kind, urlEnv, adminEnv, purpose, checked string) []adminService {
	raw := strings.TrimSpace(os.Getenv(urlEnv))
	if raw == "" {
		return in
	}
	adminRaw := strings.TrimSpace(os.Getenv(adminEnv))
	if adminRaw == "" {
		adminRaw = raw
	}
	host, port := hostPortFromURL(raw)
	return append(in, adminService{
		ID:       id,
		Name:     name,
		Kind:     kind,
		Status:   "configured",
		Host:     host,
		Port:     nullablePort(port),
		AdminURL: nullableURL(adminRaw),
		Purpose:  purpose,
		Checked:  checked,
	})
}

func serviceStatus(st adapterregistry.Status) string {
	switch st {
	case adapterregistry.StatusHealthy:
		return "healthy"
	case adapterregistry.StatusDegraded, adapterregistry.StatusUnknown:
		return "degraded"
	default:
		return "offline"
	}
}

func hostPortFromURL(raw string) (string, int) {
	if raw == "" {
		return "", 0
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", 0
	}
	host := u.Hostname()
	port := 0
	if p := u.Port(); p != "" {
		port, _ = strconv.Atoi(p)
	}
	return host, port
}

func hostFromDatabaseURL(raw string) string {
	host, _, err := net.SplitHostPort(strings.TrimPrefix(strings.TrimPrefix(raw, "postgres://"), "postgresql://"))
	if err == nil {
		return host
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func portFromDatabaseURL(raw string, fallback int) int {
	u, err := url.Parse(raw)
	if err != nil {
		return fallback
	}
	if p := u.Port(); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil {
			return parsed
		}
	}
	return fallback
}

func portFromAddr(addr string, fallback int) int {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		port = strings.TrimPrefix(addr, ":")
	}
	parsed, err := strconv.Atoi(port)
	if err != nil {
		return fallback
	}
	return parsed
}

func intPtr(v int) *int { return &v }

func nullablePort(v int) *int {
	if v <= 0 {
		return nil
	}
	return &v
}

func nullableURL(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}
