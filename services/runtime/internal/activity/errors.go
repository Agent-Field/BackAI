// SPDX-License-Identifier: Apache-2.0

package activity

import "errors"

var (
	ErrValidation     = errors.New("activity: validation failed")
	ErrTenantRequired = errors.New("activity: tenant required")
)
