// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var runIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,255}$`)

type runEventWSMessage struct {
	Type  string `json:"type"`
	Event string `json:"event,omitempty"`
	Data  any    `json:"data,omitempty"`
	Raw   string `json:"raw,omitempty"`
	Path  string `json:"path,omitempty"`
}

func (s *Server) handleRunEvents(w http.ResponseWriter, r *http.Request) {
	if s.af == nil {
		writeJSON(w, http.StatusServiceUnavailable,
			errEnvelope("AGENTFIELD_NOT_CONFIGURED", "agentfield client not configured"))
		return
	}
	runID := r.PathValue("id")
	if !validRunID(runID) {
		writeJSON(w, http.StatusBadRequest,
			errEnvelope("VALIDATION_FAILED", "invalid run id"))
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	afStream, err := s.af.StreamRunEvents(ctx, runID)
	if err != nil {
		writeJSON(w, http.StatusBadGateway,
			errEnvelope("AGENTFIELD_UNREACHABLE", err.Error()))
		return
	}
	defer afStream.Body.Close()

	if afStream.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(afStream.Body, 1<<20))
		code := "AGENTFIELD_RUN_EVENTS_UNAVAILABLE"
		status := http.StatusBadGateway
		if afStream.StatusCode == http.StatusNotFound {
			code = "AGENTFIELD_RUN_EVENTS_NOT_FOUND"
			status = http.StatusNotFound
		}
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = "agentfield run event stream is not available"
		}
		writeJSON(w, status, errEnvelope(code, msg))
		return
	}

	conn, err := realtimeUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				cancel()
				return
			}
		}
	}()

	if err := relaySSEToWebSocket(ctx, afStream.Body, afStream.Path, conn); err != nil &&
		!errors.Is(ctx.Err(), context.Canceled) {
		s.log.Warn("run event relay stopped", "run_id", runID, "error", err)
	}
}

func validRunID(id string) bool {
	return id != "" && runIDPattern.MatchString(id)
}

func relaySSEToWebSocket(ctx context.Context, body io.Reader, path string, conn *websocket.Conn) error {
	scanner := bufio.NewScanner(body)
	buf := make([]byte, 0, 1<<16)
	scanner.Buffer(buf, 1<<20)

	eventName := ""
	var dataLines []string
	dispatch := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		raw := strings.Join(dataLines, "\n")
		msg := runEventWSMessage{
			Type:  "agentfield.run_event",
			Event: eventName,
			Raw:   raw,
			Path:  path,
		}
		var data any
		if err := json.Unmarshal([]byte(raw), &data); err == nil {
			msg.Data = data
			msg.Raw = ""
			if msg.Event == "" {
				if m, ok := data.(map[string]any); ok {
					if t, ok := m["type"].(string); ok && t != "" {
						msg.Event = t
					} else if t, ok := m["event_type"].(string); ok && t != "" {
						msg.Event = t
					}
				}
			}
		}
		if msg.Event == "" {
			msg.Event = "message"
		}
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteJSON(msg); err != nil {
			return err
		}
		eventName = ""
		dataLines = dataLines[:0]
		return nil
	}

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Text()
		if line == "" {
			if err := dispatch(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteJSON(runEventWSMessage{
				Type: "heartbeat",
				Raw:  line,
				Path: path,
			}); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return dispatch()
}
