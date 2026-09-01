// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"encoding/json"
	"fmt"
	"time"
)

// SQLColumnType maps a manifest field type to the Postgres column type a
// module author declares in their migration. Documented so the
// resource->table contract is unambiguous; the runtime never emits DDL.
func SQLColumnType(fieldType string) string {
	switch fieldType {
	case FieldTypeString:
		return "text"
	case FieldTypeInt:
		return "bigint"
	case FieldTypeBool:
		return "boolean"
	case FieldTypeTimestamp:
		return "timestamptz"
	case FieldTypeJSON:
		return "jsonb"
	default:
		return "text"
	}
}

// coerceValue validates and normalises a JSON-decoded value against the
// declared field type, returning the value to bind as a query argument.
// It accepts the range of shapes encoding/json and yaml.v3 produce for a
// given type so both manifest defaults and request bodies flow through
// the same gate.
func coerceValue(fieldType string, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch fieldType {
	case FieldTypeString:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("expected string, got %T", v)
		}
		return s, nil
	case FieldTypeInt:
		switch n := v.(type) {
		case int:
			return int64(n), nil
		case int64:
			return n, nil
		case float64:
			if n != float64(int64(n)) {
				return nil, fmt.Errorf("expected integer, got fractional %v", n)
			}
			return int64(n), nil
		case json.Number:
			i, err := n.Int64()
			if err != nil {
				return nil, fmt.Errorf("expected integer: %w", err)
			}
			return i, nil
		default:
			return nil, fmt.Errorf("expected int, got %T", v)
		}
	case FieldTypeBool:
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("expected bool, got %T", v)
		}
		return b, nil
	case FieldTypeTimestamp:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("expected RFC3339 timestamp string, got %T", v)
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return nil, fmt.Errorf("expected RFC3339 timestamp: %w", err)
		}
		return t.UTC(), nil
	case FieldTypeJSON:
		// Round-trip through JSON so the driver receives a jsonb-encodable
		// []byte regardless of whether the value came from a map, slice, or
		// scalar.
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("expected JSON-serialisable value: %w", err)
		}
		return raw, nil
	default:
		return nil, fmt.Errorf("unknown field type %q", fieldType)
	}
}
