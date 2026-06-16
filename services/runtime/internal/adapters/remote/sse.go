// SPDX-License-Identifier: Apache-2.0

package remote

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// Event is a single SSE message parsed from an adapter stream. Data
// carries the raw JSON body of the event; per-slot shims unmarshal it
// into their own type. Comment-only events (lines starting with ":")
// are skipped by the reader.
type Event struct {
	// ID is the optional event id (lines like "id: 42"). Empty when
	// the adapter doesn't set one.
	ID string

	// Type is the SSE event type ("message" by default; lines like
	// "event: log_line"). Empty when the adapter doesn't set one.
	Type string

	// Data is the raw bytes from one or more "data:" lines, joined by
	// newline. For our protocol every event is a single JSON object
	// fitting one line, so Data is typically <4 KiB.
	Data []byte
}

// DecodeData unmarshals the event data into v. Helper for per-slot
// shims. Renamed from UnmarshalJSON so we don't accidentally satisfy
// json.Unmarshaler (which has a different signature).
func (e *Event) DecodeData(v any) error {
	if len(e.Data) == 0 {
		return errors.New("remote: empty SSE event data")
	}
	return json.Unmarshal(e.Data, v)
}

// readEvents emits events parsed from r as they arrive on the channel.
// The channel is closed when r is exhausted or ctx is done. The goroutine
// stops if r is closed by the caller — typical pattern is: caller
// captures the response Body, defers Body.Close, ranges over the
// channel, and breaks/returns to abort the stream.
//
// We deliberately don't buffer multiple events; the consumer applies
// back-pressure naturally by being slow to receive.
func readEvents(ctx context.Context, r io.Reader) <-chan Event {
	out := make(chan Event)
	go func() {
		defer close(out)

		sc := bufio.NewScanner(r)
		// SSE events can be up to a few hundred KiB in theory; in
		// practice our protocol stays well under 64 KiB per line.
		// Allow up to 1 MiB so a misbehaving adapter just stalls
		// rather than crashing us.
		sc.Buffer(make([]byte, 0, 16*1024), 1024*1024)

		var (
			id, typ string
			dataBuf strings.Builder
			hasData bool
		)

		flush := func() bool {
			if !hasData {
				return true
			}
			ev := Event{ID: id, Type: typ, Data: []byte(dataBuf.String())}
			select {
			case <-ctx.Done():
				return false
			case out <- ev:
			}
			id, typ = "", ""
			dataBuf.Reset()
			hasData = false
			return true
		}

		for sc.Scan() {
			if ctx.Err() != nil {
				return
			}
			line := sc.Text()
			if line == "" {
				if !flush() {
					return
				}
				continue
			}
			if strings.HasPrefix(line, ":") {
				// Comment / keep-alive. Ignore.
				continue
			}
			field, value, ok := splitField(line)
			if !ok {
				// Malformed line; skip rather than break.
				continue
			}
			switch field {
			case "id":
				id = value
			case "event":
				typ = value
			case "data":
				if hasData {
					dataBuf.WriteByte('\n')
				}
				dataBuf.WriteString(value)
				hasData = true
			case "retry":
				// Per SSE spec; we don't honour it currently.
			}
		}
		// Final flush on EOF.
		if sc.Err() == nil {
			flush()
		}
	}()
	return out
}

// splitField parses one SSE line into (field, value). Per spec, the
// separator is the first ":". A space after the colon is optional and
// stripped if present.
func splitField(line string) (string, string, bool) {
	i := strings.IndexByte(line, ':')
	if i < 0 {
		// Per spec, a bare field name with no colon is treated as
		// field with empty value. We're more strict — return false
		// and let the caller skip.
		return "", "", false
	}
	field := line[:i]
	value := line[i+1:]
	if len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}
	return field, value, true
}
