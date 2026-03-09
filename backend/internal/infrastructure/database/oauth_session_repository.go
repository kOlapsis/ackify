// SPDX-License-Identifier: AGPL-3.0-or-later
package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/kolapsis/ackify/backend/internal/infrastructure/dbctx"
	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/models"
	"github.com/kolapsis/ackify/backend/pkg/providers"
)

// OAuthSessionRepository implements the OAuth session repository
type OAuthSessionRepository struct {
	db      *sql.DB
	tenants providers.TenantProvider
}

// NewOAuthSessionRepository creates a new OAuth session repository
func NewOAuthSessionRepository(db *sql.DB, tenants providers.TenantProvider) *OAuthSessionRepository {
	return &OAuthSessionRepository{db: db, tenants: tenants}
}

// Create creates a new OAuth session
func (r *OAuthSessionRepository) Create(ctx context.Context, session *models.OAuthSession) error {
	tenantID, err := r.tenants.CurrentTenant(ctx)
	if err != nil {
		return fmt.Errorf("failed to get tenant: %w", err)
	}

	query := `
		INSERT INTO oauth_sessions (
			tenant_id,
			session_id,
			user_sub,
			refresh_token_encrypted,
			access_token_expires_at,
			user_agent,
			ip_address
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at
	`

	err = dbctx.GetQuerier(ctx, r.db).QueryRowContext(
		ctx,
		query,
		tenantID,
		session.SessionID,
		session.UserSub,
		session.RefreshTokenEncrypted,
		session.AccessTokenExpiresAt,
		session.UserAgent,
		session.IPAddress,
	).Scan(&session.ID, &session.CreatedAt, &session.UpdatedAt)

	if err != nil {
		logger.Logger.Error("Failed to create OAuth session",
			"session_id", session.SessionID,
			"user_sub", session.UserSub,
			"error", err.Error())
		return fmt.Errorf("failed to create OAuth session: %w", err)
	}

	session.TenantID = tenantID

	logger.Logger.Info("Created OAuth session",
		"session_id", session.SessionID,
		"user_sub", session.UserSub)

	return nil
}

// GetBySessionID retrieves an OAuth session by session ID
// RLS policy automatically filters by tenant_id
func (r *OAuthSessionRepository) GetBySessionID(ctx context.Context, sessionID string) (*models.OAuthSession, error) {
	query := `
		SELECT
			id,
			tenant_id,
			session_id,
			user_sub,
			refresh_token_encrypted,
			access_token_expires_at,
			created_at,
			updated_at,
			last_refreshed_at,
			user_agent,
			ip_address
		FROM oauth_sessions
		WHERE session_id = $1
	`

	session := &models.OAuthSession{}
	var lastRefreshedAt sql.NullTime

	err := dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, query, sessionID).Scan(
		&session.ID,
		&session.TenantID,
		&session.SessionID,
		&session.UserSub,
		&session.RefreshTokenEncrypted,
		&session.AccessTokenExpiresAt,
		&session.CreatedAt,
		&session.UpdatedAt,
		&lastRefreshedAt,
		&session.UserAgent,
		&session.IPAddress,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("OAuth session not found")
	}

	if err != nil {
		logger.Logger.Error("Failed to get OAuth session",
			"session_id", sessionID,
			"error", err.Error())
		return nil, fmt.Errorf("failed to get OAuth session: %w", err)
	}

	if lastRefreshedAt.Valid {
		session.LastRefreshedAt = &lastRefreshedAt.Time
	}

	return session, nil
}

// UpdateRefreshToken updates the refresh token and expiration time
// RLS policy automatically filters by tenant_id
func (r *OAuthSessionRepository) UpdateRefreshToken(ctx context.Context, sessionID string, encryptedToken []byte, expiresAt time.Time) error {
	query := `
		UPDATE oauth_sessions
		SET
			refresh_token_encrypted = $1,
			access_token_expires_at = $2,
			last_refreshed_at = now(),
			updated_at = now()
		WHERE session_id = $3
	`

	result, err := dbctx.GetQuerier(ctx, r.db).ExecContext(ctx, query, encryptedToken, expiresAt, sessionID)
	if err != nil {
		logger.Logger.Error("Failed to update OAuth session refresh token",
			"session_id", sessionID,
			"error", err.Error())
		return fmt.Errorf("failed to update refresh token: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("OAuth session not found")
	}

	logger.Logger.Info("Updated OAuth session refresh token",
		"session_id", sessionID)

	return nil
}

// DeleteBySessionID deletes an OAuth session by session ID
// RLS policy automatically filters by tenant_id
func (r *OAuthSessionRepository) DeleteBySessionID(ctx context.Context, sessionID string) error {
	query := `DELETE FROM oauth_sessions WHERE session_id = $1`

	result, err := dbctx.GetQuerier(ctx, r.db).ExecContext(ctx, query, sessionID)
	if err != nil {
		logger.Logger.Error("Failed to delete OAuth session",
			"session_id", sessionID,
			"error", err.Error())
		return fmt.Errorf("failed to delete OAuth session: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected > 0 {
		logger.Logger.Info("Deleted OAuth session", "session_id", sessionID)
	}

	return nil
}

// DeleteExpired deletes OAuth sessions older than the specified duration
func (r *OAuthSessionRepository) DeleteExpired(ctx context.Context, olderThan time.Duration) (int64, error) {
	query := `
		DELETE FROM oauth_sessions
		WHERE updated_at < $1
	`

	cutoffTime := time.Now().Add(-olderThan)
	result, err := dbctx.GetQuerier(ctx, r.db).ExecContext(ctx, query, cutoffTime)
	if err != nil {
		logger.Logger.Error("Failed to delete expired OAuth sessions",
			"cutoff_time", cutoffTime,
			"error", err.Error())
		return 0, fmt.Errorf("failed to delete expired sessions: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected > 0 {
		logger.Logger.Info("Deleted expired OAuth sessions",
			"count", rowsAffected,
			"older_than", olderThan)
	}

	return rowsAffected, nil
}
