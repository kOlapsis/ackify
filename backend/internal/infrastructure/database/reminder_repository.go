// SPDX-License-Identifier: AGPL-3.0-or-later
package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kolapsis/ackify/backend/internal/infrastructure/dbctx"
	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/models"
	"github.com/kolapsis/ackify/backend/pkg/providers"
)

// ReminderRepository handles database operations for reminder logs
type ReminderRepository struct {
	db      *sql.DB
	tenants providers.TenantProvider
}

// NewReminderRepository creates a new reminder repository
func NewReminderRepository(db *sql.DB, tenants providers.TenantProvider) *ReminderRepository {
	return &ReminderRepository{db: db, tenants: tenants}
}

// LogReminder records an email reminder event with delivery status for audit tracking
func (r *ReminderRepository) LogReminder(ctx context.Context, log *models.ReminderLog) error {
	tenantID, err := r.tenants.CurrentTenant(ctx)
	if err != nil {
		return fmt.Errorf("failed to get tenant: %w", err)
	}

	query := `
		INSERT INTO reminder_logs
		(tenant_id, doc_id, recipient_email, sent_at, sent_by, template_used, status, error_message)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`

	err = dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, query,
		tenantID,
		log.DocID,
		log.RecipientEmail,
		log.SentAt,
		log.SentBy,
		log.TemplateUsed,
		log.Status,
		log.ErrorMessage,
	).Scan(&log.ID)

	if err != nil {
		return fmt.Errorf("failed to log reminder: %w", err)
	}

	log.TenantID = tenantID
	return nil
}

// GetReminderHistory retrieves complete reminder audit trail for a document, ordered by send time descending
// RLS policy automatically filters by tenant_id
func (r *ReminderRepository) GetReminderHistory(ctx context.Context, docID string) ([]*models.ReminderLog, error) {
	query := `
		SELECT id, tenant_id, doc_id, recipient_email, sent_at, sent_by, template_used, status, error_message
		FROM reminder_logs
		WHERE doc_id = $1
		ORDER BY sent_at DESC
	`

	rows, err := dbctx.GetQuerier(ctx, r.db).QueryContext(ctx, query, docID)
	if err != nil {
		return nil, fmt.Errorf("failed to query reminder history: %w", err)
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			logger.Logger.Error("failed to close rows", "error", err)
		}
	}(rows)

	var logs []*models.ReminderLog
	for rows.Next() {
		log := &models.ReminderLog{}
		err := rows.Scan(
			&log.ID,
			&log.TenantID,
			&log.DocID,
			&log.RecipientEmail,
			&log.SentAt,
			&log.SentBy,
			&log.TemplateUsed,
			&log.Status,
			&log.ErrorMessage,
		)
		if err != nil {
			continue
		}
		logs = append(logs, log)
	}

	return logs, nil
}

// GetLastReminderByEmail retrieves the most recent reminder sent to a specific recipient for throttling logic
// RLS policy automatically filters by tenant_id
func (r *ReminderRepository) GetLastReminderByEmail(ctx context.Context, docID, email string) (*models.ReminderLog, error) {
	query := `
		SELECT id, tenant_id, doc_id, recipient_email, sent_at, sent_by, template_used, status, error_message
		FROM reminder_logs
		WHERE doc_id = $1 AND LOWER(recipient_email) = LOWER($2)
		ORDER BY sent_at DESC
		LIMIT 1
	`

	log := &models.ReminderLog{}
	err := dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, query, docID, email).Scan(
		&log.ID,
		&log.TenantID,
		&log.DocID,
		&log.RecipientEmail,
		&log.SentAt,
		&log.SentBy,
		&log.TemplateUsed,
		&log.Status,
		&log.ErrorMessage,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get last reminder: %w", err)
	}

	return log, nil
}

// GetReminderCount tallies successfully delivered reminders to a recipient for rate limiting
// RLS policy automatically filters by tenant_id
func (r *ReminderRepository) GetReminderCount(ctx context.Context, docID, email string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM reminder_logs
		WHERE doc_id = $1 AND LOWER(recipient_email) = LOWER($2) AND status = 'sent'
	`

	var count int
	err := dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, query, docID, email).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get reminder count: %w", err)
	}

	return count, nil
}

// GetReminderStats aggregates reminder metrics including pending signers and last send timestamp
// RLS policy automatically filters by tenant_id
func (r *ReminderRepository) GetReminderStats(ctx context.Context, docID string) (*models.ReminderStats, error) {
	query := `
		SELECT
			COUNT(*) as total_sent,
			MAX(sent_at) as last_sent_at
		FROM reminder_logs
		WHERE doc_id = $1 AND status = 'sent'
	`

	stats := &models.ReminderStats{}
	var lastSent sql.NullTime

	err := dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, query, docID).Scan(&stats.TotalSent, &lastSent)
	if err != nil {
		return nil, fmt.Errorf("failed to get reminder stats: %w", err)
	}

	if lastSent.Valid {
		stats.LastSentAt = &lastSent.Time
	}

	pendingQuery := `
		SELECT COUNT(*)
		FROM expected_signers es
		LEFT JOIN signatures s ON es.tenant_id = s.tenant_id AND es.doc_id = s.doc_id AND LOWER(es.email) = LOWER(s.user_email)
		WHERE es.doc_id = $1 AND s.id IS NULL
	`

	err = dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, pendingQuery, docID).Scan(&stats.PendingCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending count: %w", err)
	}

	return stats, nil
}

// Count returns the number of sent reminders in the database
func (r *ReminderRepository) Count(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM reminder_logs WHERE status = 'sent'`
	var count int
	err := dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count sent reminders count: %w", err)
	}
	return count, nil
}
