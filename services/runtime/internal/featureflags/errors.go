// SPDX-License-Identifier: Apache-2.0

package featureflags

import "errors"

var (
	ErrValidation     = errors.New("featureflags: validation failed")
	ErrTenantRequired = errors.New("featureflags: tenant required")
)
