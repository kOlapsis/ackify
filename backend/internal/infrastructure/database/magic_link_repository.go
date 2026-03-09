// SPDX-License-Identifier: AGPL-3.0-or-later
package database

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/kolapsis/ackify/backend/internal/application/services"
	"github.com/kolapsis/ackify/backend/internal/infrastructure/dbctx"
	"github.com/kolapsis/ackify/backend/pkg/models"
)

type magicLinkRepo struct {
	db *sql.DB
}

func NewMagicLinkRepository(db *sql.DB) services.MagicLinkRepository {
	return &magicLinkRepo{db: db}
}

func (r *magicLinkRepo) CreateToken(ctx context.Context, token *models.MagicLinkToken) error {
	query := `
		INSERT INTO magic_link_tokens
		(tenant_id, token, email, expires_at, redirect_to, created_by_ip, created_by_user_agent, purpose, doc_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at
	`

	// Set default purpose if empty
	purpose := token.Purpose
	if purpose == "" {
		purpose = "login"
	}

	return dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, query,
		token.TenantID, // Can be NULL for login requests
		token.Token,
		token.Email,
		token.ExpiresAt,
		token.RedirectTo,
		token.CreatedByIP,
		token.CreatedByUserAgent,
		purpose,
		token.DocID,
	).Scan(&token.ID, &token.CreatedAt)
}

func (r *magicLinkRepo) GetByToken(ctx context.Context, token string) (*models.MagicLinkToken, error) {
	query := `
		SELECT id, tenant_id, token, email, created_at, expires_at, used_at, used_by_ip,
		       used_by_user_agent, redirect_to, created_by_ip, created_by_user_agent,
		       purpose, doc_id
		FROM magic_link_tokens
		WHERE token = $1
	`

	var t models.MagicLinkToken
	var usedAt sql.NullTime
	var usedByIP, usedByUserAgent, docID sql.NullString
	var tenantID sql.NullString

	err := dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, query, token).Scan(
		&t.ID,
		&tenantID,
		&t.Token,
		&t.Email,
		&t.CreatedAt,
		&t.ExpiresAt,
		&usedAt,
		&usedByIP,
		&usedByUserAgent,
		&t.RedirectTo,
		&t.CreatedByIP,
		&t.CreatedByUserAgent,
		&t.Purpose,
		&docID,
	)

	if err == sql.ErrNoRows {
		return nil, err
	}
	if err != nil {
		return nil, err
	}

	if tenantID.Valid {
		parsed, _ := uuid.Parse(tenantID.String)
		t.TenantID = &parsed
	}
	if usedAt.Valid {
		t.UsedAt = &usedAt.Time
	}
	if usedByIP.Valid {
		t.UsedByIP = &usedByIP.String
	}
	if usedByUserAgent.Valid {
		t.UsedByUserAgent = &usedByUserAgent.String
	}
	if docID.Valid {
		t.DocID = &docID.String
	}

	return &t, nil
}

func (r *magicLinkRepo) MarkAsUsed(ctx context.Context, token string, ip string, userAgent string) error {
	query := `
		UPDATE magic_link_tokens
		SET used_at = now(),
		    used_by_ip = $2,
		    used_by_user_agent = $3
		WHERE token = $1 AND used_at IS NULL
	`

	result, err := dbctx.GetQuerier(ctx, r.db).ExecContext(ctx, query, token, ip, userAgent)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func (r *magicLinkRepo) DeleteExpired(ctx context.Context) (int64, error) {
	query := `
		DELETE FROM magic_link_tokens
		WHERE expires_at < now() OR (created_at < now() - INTERVAL '7 days' AND used_at IS NULL)
	`

	result, err := dbctx.GetQuerier(ctx, r.db).ExecContext(ctx, query)
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

func (r *magicLinkRepo) LogAttempt(ctx context.Context, attempt *models.MagicLinkAuthAttempt) error {
	query := `
		INSERT INTO magic_link_auth_attempts
		(tenant_id, email, success, failure_reason, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, attempted_at
	`

	return dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, query,
		attempt.TenantID, // Can be NULL before authentication
		attempt.Email,
		attempt.Success,
		attempt.FailureReason,
		attempt.IPAddress,
		attempt.UserAgent,
	).Scan(&attempt.ID, &attempt.AttemptedAt)
}

func (r *magicLinkRepo) CountRecentAttempts(ctx context.Context, email string, since time.Time) (int, error) {
	var count int
	query := `
		SELECT COUNT(*)
		FROM magic_link_auth_attempts
		WHERE email = $1 AND attempted_at > $2
	`

	err := dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, query, email, since).Scan(&count)
	return count, err
}

func (r *magicLinkRepo) CountRecentAttemptsByIP(ctx context.Context, ip string, since time.Time) (int, error) {
	var count int
	query := `
		SELECT COUNT(*)
		FROM magic_link_auth_attempts
		WHERE ip_address = $1 AND attempted_at > $2
	`

	err := dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, query, ip, since).Scan(&count)
	return count, err
}
