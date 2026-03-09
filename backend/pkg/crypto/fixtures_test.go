// SPDX-License-Identifier: AGPL-3.0-or-later
package crypto

import "github.com/kolapsis/ackify/backend/pkg/models"

var (
	testUserAlice = &models.User{
		Sub:   "user-123-alice",
		Email: "alice@example.com",
		Name:  "Alice Smith",
	}

	testUserBob = &models.User{
		Sub:   "user-456-bob",
		Email: "bob@example.com",
		Name:  "Bob Johnson",
	}

	testUserCharlie = &models.User{
		Sub:   "user-789-charlie",
		Email: "charlie@example.com",
		Name:  "Charlie Brown",
	}
)
