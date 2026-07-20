// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// errNoRows signals a single-row query returned nothing, mapped to 404 by
// the handlers.
var errNoRows = errors.New("modules: no rows")

// scanRows drains a result set into a slice of column-keyed maps. Because
// a resource's columns are known only at runtime (declared in the
// manifest), we scan generically via pgx.Rows.Values rather than into a
// fixed struct.
func scanRows(rows pgx.Rows, cols []string, byName map[string]Field) ([]map[string]any, error) {
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		out = append(out, rowToMap(cols, vals, byName))
	}
	return out, rows.Err()
}

// scanOne reads exactly one row (RETURNING / by-id lookups). Returns
// errNoRows when the set is empty.
func scanOne(rows pgx.Rows, cols []string, byName map[string]Field) (map[string]any, error) {
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, errNoRows
	}
	vals, err := rows.Values()
	if err != nil {
		return nil, err
	}
	return rowToMap(cols, vals, byName), rows.Err()
}

func rowToMap(cols []string, vals []any, byName map[string]Field) map[string]any {
	m := make(map[string]any, len(cols))
	for i, c := range cols {
		if i >= len(vals) {
			break
		}
		ft := ""
		if f, ok := byName[c]; ok {
			ft = f.Type
		}
		m[c] = normalizeOut(ft, vals[i])
	}
	return m
}

// normalizeOut converts driver-native values into JSON-friendly shapes:
// timestamps to RFC3339, uuid byte arrays to canonical strings, and jsonb
// bytes to their decoded value.
func normalizeOut(fieldType string, v any) any {
	switch val := v.(type) {
	case nil:
		return nil
	case time.Time:
		return val.UTC().Format(time.RFC3339Nano)
	case [16]byte:
		return formatUUID(val)
	case []byte:
		if fieldType == FieldTypeJSON {
			var decoded any
			if err := json.Unmarshal(val, &decoded); err == nil {
				return decoded
			}
		}
		return string(val)
	default:
		return v
	}
}

func formatUUID(b [16]byte) string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
