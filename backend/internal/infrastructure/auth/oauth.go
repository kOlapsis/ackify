// SPDX-License-Identifier: AGPL-3.0-or-later
package auth

import (
	"context"
	"time"

	"github.com/kolapsis/ackify/backend/pkg/models"
)

const sessionName = "ackapp_session"

// SessionRepository defines the interface for OAuth session storage
type SessionRepository interface {
	Create(ctx context.Context, session *models.OAuthSession) error
	GetBySessionID(ctx context.Context, sessionID string) (*models.OAuthSession, error)
	UpdateRefreshToken(ctx context.Context, sessionID string, encryptedToken []byte, expiresAt time.Time) error
	DeleteBySessionID(ctx context.Context, sessionID string) error
	DeleteExpired(ctx context.Context, olderThan time.Duration) (int64, error)
}
