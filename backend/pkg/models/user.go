// SPDX-License-Identifier: AGPL-3.0-or-later
package models

import "github.com/kolapsis/ackify/backend/pkg/types"

// User is an alias for the unified user type.
// This allows domain code to use models.User while sharing the same underlying type.
type User = types.User
