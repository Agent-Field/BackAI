// SPDX-License-Identifier: Apache-2.0

package featureflags

type Flag struct {
	Key         string         `json:"key"`
	Label       string         `json:"label"`
	Description string         `json:"description"`
	Enabled     bool           `json:"enabled"`
	Source      string         `json:"source"`
	Metadata    map[string]any `json:"metadata"`
	UpdatedAt   *string        `json:"updated_at"`
}

type List struct {
	Flags []Flag `json:"flags"`
}

type SetInput struct {
	Enabled     bool           `json:"enabled"`
	Label       string         `json:"label,omitempty"`
	Description string         `json:"description,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}
