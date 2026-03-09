// SPDX-License-Identifier: AGPL-3.0-or-later
package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kolapsis/ackify/backend/pkg/providers"
	"github.com/lib/pq"

	"github.com/kolapsis/ackify/backend/internal/infrastructure/dbctx"
	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/models"
)

// EmailQueueRepository handles database operations for the email queue
type EmailQueueRepository struct {
	db      *sql.DB
	tenants providers.TenantProvider
}

// NewEmailQueueRepository creates a new email queue repository
func NewEmailQueueRepository(db *sql.DB, tenants providers.TenantProvider) *EmailQueueRepository {
	return &EmailQueueRepository{db: db, tenants: tenants}
}

// Enqueue adds a new email to the queue
func (r *EmailQueueRepository) Enqueue(ctx context.Context, input models.EmailQueueInput) (*models.EmailQueueItem, error) {
	tenantID, err := r.tenants.CurrentTenant(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}

	// Prepare data as JSON
	dataJSON, err := json.Marshal(input.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal email data: %w", err)
	}

	var headersJSON []byte
	if input.Headers != nil {
		headersJSON, err = json.Marshal(input.Headers)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal email headers: %w", err)
		}
	} else {
		// Use empty JSON object instead of nil for PostgreSQL JSONB compatibility
		headersJSON = []byte("{}")
	}

	// Default values
	maxRetries := input.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}

	scheduledFor := time.Now()
	if input.ScheduledFor != nil {
		scheduledFor = *input.ScheduledFor
	}

	query := `
		INSERT INTO email_queue (
			tenant_id, to_addresses, cc_addresses, bcc_addresses,
			subject, template, locale, data, headers,
			priority, scheduled_for, max_retries,
			reference_type, reference_id, created_by
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
		) RETURNING
			id, tenant_id, status, retry_count, created_at, processed_at,
			next_retry_at, last_error, error_details
	`

	item := &models.EmailQueueItem{
		TenantID:      tenantID,
		ToAddresses:   input.ToAddresses,
		CcAddresses:   input.CcAddresses,
		BccAddresses:  input.BccAddresses,
		Subject:       input.Subject,
		Template:      input.Template,
		Locale:        input.Locale,
		Data:          dataJSON,
		Headers:       models.NullRawMessage{RawMessage: headersJSON, Valid: input.Headers != nil},
		Priority:      input.Priority,
		ScheduledFor:  scheduledFor,
		MaxRetries:    maxRetries,
		ReferenceType: input.ReferenceType,
		ReferenceID:   input.ReferenceID,
		CreatedBy:     input.CreatedBy,
	}

	err = dbctx.GetQuerier(ctx, r.db).QueryRowContext(
		ctx,
		query,
		tenantID,
		pq.Array(input.ToAddresses),
		pq.Array(input.CcAddresses),
		pq.Array(input.BccAddresses),
		input.Subject,
		input.Template,
		input.Locale,
		dataJSON,
		headersJSON,
		input.Priority,
		scheduledFor,
		maxRetries,
		input.ReferenceType,
		input.ReferenceID,
		input.CreatedBy,
	).Scan(
		&item.ID,
		&item.TenantID,
		&item.Status,
		&item.RetryCount,
		&item.CreatedAt,
		&item.ProcessedAt,
		&item.NextRetryAt,
		&item.LastError,
		&item.ErrorDetails,
	)

	if err != nil {
		logger.Logger.Error("Failed to enqueue email",
			"error", err.Error(),
			"template", input.Template)
		return nil, fmt.Errorf("failed to enqueue email: %w", err)
	}

	logger.Logger.Info("Email enqueued successfully",
		"id", item.ID,
		"template", input.Template,
		"priority", input.Priority)

	return item, nil
}

// GetNextToProcess fetches the next email(s) to process from the queue
func (r *EmailQueueRepository) GetNextToProcess(ctx context.Context, limit int) ([]*models.EmailQueueItem, error) {
	query := `
		UPDATE email_queue
		SET status = 'processing'
		WHERE id IN (
			SELECT id FROM email_queue
			WHERE status = 'pending'
			  AND scheduled_for <= $1
			ORDER BY priority DESC, scheduled_for ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		RETURNING
			id, tenant_id, to_addresses, cc_addresses, bcc_addresses,
			subject, template, locale, data, headers,
			status, priority, retry_count, max_retries,
			created_at, scheduled_for, processed_at, next_retry_at,
			last_error, error_details, reference_type, reference_id, created_by
	`

	rows, err := dbctx.GetQuerier(ctx, r.db).QueryContext(ctx, query, time.Now(), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get next emails to process: %w", err)
	}
	defer rows.Close()

	var items []*models.EmailQueueItem
	for rows.Next() {
		item := &models.EmailQueueItem{}
		err := rows.Scan(
			&item.ID,
			&item.TenantID,
			pq.Array(&item.ToAddresses),
			pq.Array(&item.CcAddresses),
			pq.Array(&item.BccAddresses),
			&item.Subject,
			&item.Template,
			&item.Locale,
			&item.Data,
			&item.Headers,
			&item.Status,
			&item.Priority,
			&item.RetryCount,
			&item.MaxRetries,
			&item.CreatedAt,
			&item.ScheduledFor,
			&item.ProcessedAt,
			&item.NextRetryAt,
			&item.LastError,
			&item.ErrorDetails,
			&item.ReferenceType,
			&item.ReferenceID,
			&item.CreatedBy,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan email queue item: %w", err)
		}
		items = append(items, item)
	}

	return items, nil
}

// MarkAsSent marks an email as successfully sent
func (r *EmailQueueRepository) MarkAsSent(ctx context.Context, id int64) error {
	query := `
		UPDATE email_queue
		SET status = 'sent',
		    processed_at = $1
		WHERE id = $2
	`

	result, err := dbctx.GetQuerier(ctx, r.db).ExecContext(ctx, query, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to mark email as sent: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("email not found: %d", id)
	}

	logger.Logger.Debug("Email marked as sent", "id", id)
	return nil
}

// MarkAsFailed marks an email as failed with error details
// This method uses the default PostgreSQL exponential backoff calculation
func (r *EmailQueueRepository) MarkAsFailed(ctx context.Context, id int64, err error, shouldRetry bool) error {
	return r.MarkAsFailedWithDelay(ctx, id, err, shouldRetry, 0)
}

// MarkAsFailedWithDelay marks an email as failed with error details and custom retry delay
func (r *EmailQueueRepository) MarkAsFailedWithDelay(ctx context.Context, id int64, err error, shouldRetry bool, retryDelay time.Duration) error {
	errorMsg := err.Error()

	errorDetails := map[string]interface{}{
		"error":        errorMsg,
		"timestamp":    time.Now().Format(time.RFC3339),
		"should_retry": shouldRetry,
	}

	errorDetailsJSON, _ := json.Marshal(errorDetails)

	var query string
	var args []interface{}

	if shouldRetry {
		// If retrying, increment retry count and set next retry time
		// If retryDelay is 0, use PostgreSQL function for default exponential backoff
		// Otherwise, use the custom delay provided by the caller
		if retryDelay > 0 {
			nextRetry := time.Now().Add(retryDelay)
			query = `
				UPDATE email_queue
				SET status = 'pending',
				    retry_count = retry_count + 1,
				    last_error = $1,
				    error_details = $2,
				    scheduled_for = $3
				WHERE id = $4 AND retry_count < max_retries
			`
			args = []interface{}{errorMsg, errorDetailsJSON, nextRetry, id}
		} else {
			// Use default PostgreSQL function
			query = `
				UPDATE email_queue
				SET status = 'pending',
				    retry_count = retry_count + 1,
				    last_error = $1,
				    error_details = $2,
				    scheduled_for = calculate_next_retry_time(retry_count + 1)
				WHERE id = $3 AND retry_count < max_retries
			`
			args = []interface{}{errorMsg, errorDetailsJSON, id}
		}
	} else {
		// If not retrying, mark as failed
		query = `
			UPDATE email_queue
			SET status = 'failed',
			    processed_at = $1,
			    last_error = $2,
			    error_details = $3
			WHERE id = $4
		`
		args = []interface{}{time.Now(), errorMsg, errorDetailsJSON, id}
	}

	result, err := dbctx.GetQuerier(ctx, r.db).ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to mark email as failed: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 && shouldRetry {
		// Max retries reached, mark as permanently failed
		query = `
			UPDATE email_queue
			SET status = 'failed',
			    processed_at = $1,
			    last_error = $2,
			    error_details = $3
			WHERE id = $4
		`
		_, err = dbctx.GetQuerier(ctx, r.db).ExecContext(ctx, query, time.Now(), errorMsg, errorDetailsJSON, id)
		if err != nil {
			return fmt.Errorf("failed to mark email as permanently failed: %w", err)
		}
		logger.Logger.Warn("Email max retries reached, marked as failed", "id", id)
	}

	logger.Logger.Debug("Email marked as failed",
		"id", id,
		"should_retry", shouldRetry,
		"retry_delay", retryDelay)
	return nil
}

// GetRetryableEmails fetches emails that should be retried
func (r *EmailQueueRepository) GetRetryableEmails(ctx context.Context, limit int) ([]*models.EmailQueueItem, error) {
	query := `
		UPDATE email_queue
		SET status = 'processing'
		WHERE id IN (
			SELECT id FROM email_queue
			WHERE status = 'pending'
			  AND retry_count > 0
			  AND retry_count < max_retries
			  AND scheduled_for <= $1
			ORDER BY priority DESC, scheduled_for ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		RETURNING
			id, tenant_id, to_addresses, cc_addresses, bcc_addresses,
			subject, template, locale, data, headers,
			status, priority, retry_count, max_retries,
			created_at, scheduled_for, processed_at, next_retry_at,
			last_error, error_details, reference_type, reference_id, created_by
	`

	rows, err := dbctx.GetQuerier(ctx, r.db).QueryContext(ctx, query, time.Now(), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get retryable emails: %w", err)
	}
	defer rows.Close()

	var items []*models.EmailQueueItem
	for rows.Next() {
		item := &models.EmailQueueItem{}
		err := rows.Scan(
			&item.ID,
			&item.TenantID,
			pq.Array(&item.ToAddresses),
			pq.Array(&item.CcAddresses),
			pq.Array(&item.BccAddresses),
			&item.Subject,
			&item.Template,
			&item.Locale,
			&item.Data,
			&item.Headers,
			&item.Status,
			&item.Priority,
			&item.RetryCount,
			&item.MaxRetries,
			&item.CreatedAt,
			&item.ScheduledFor,
			&item.ProcessedAt,
			&item.NextRetryAt,
			&item.LastError,
			&item.ErrorDetails,
			&item.ReferenceType,
			&item.ReferenceID,
			&item.CreatedBy,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan email queue item: %w", err)
		}
		items = append(items, item)
	}

	return items, nil
}

// GetQueueStats returns statistics about the email queue
func (r *EmailQueueRepository) GetQueueStats(ctx context.Context) (*models.EmailQueueStats, error) {
	stats := &models.EmailQueueStats{
		ByStatus:   make(map[string]int),
		ByPriority: make(map[string]int),
	}

	// Get counts by status
	statusQuery := `
		SELECT status, COUNT(*)
		FROM email_queue
		GROUP BY status
	`
	rows, err := dbctx.GetQuerier(ctx, r.db).QueryContext(ctx, statusQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to get status counts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, fmt.Errorf("failed to scan status count: %w", err)
		}
		stats.ByStatus[status] = count

		switch models.EmailQueueStatus(status) {
		case models.EmailStatusPending:
			stats.TotalPending = count
		case models.EmailStatusProcessing:
			stats.TotalProcessing = count
		case models.EmailStatusSent:
			stats.TotalSent = count
		case models.EmailStatusFailed:
			stats.TotalFailed = count
		}
	}

	// Get oldest pending email
	var oldestPending sql.NullTime
	err = dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, `
		SELECT MIN(created_at)
		FROM email_queue
		WHERE status = 'pending'
	`).Scan(&oldestPending)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get oldest pending: %w", err)
	}
	if oldestPending.Valid {
		stats.OldestPending = &oldestPending.Time
	}

	// Get average retry count
	err = dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, `
		SELECT AVG(retry_count)::float
		FROM email_queue
		WHERE status IN ('sent', 'failed')
	`).Scan(&stats.AverageRetries)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get average retries: %w", err)
	}

	// Get last 24 hours stats
	err = dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status = 'sent' AND processed_at >= NOW() - INTERVAL '24 hours') as sent,
			COUNT(*) FILTER (WHERE status = 'failed' AND processed_at >= NOW() - INTERVAL '24 hours') as failed,
			COUNT(*) FILTER (WHERE created_at >= NOW() - INTERVAL '24 hours') as queued
		FROM email_queue
	`).Scan(&stats.Last24Hours.Sent, &stats.Last24Hours.Failed, &stats.Last24Hours.Queued)
	if err != nil {
		return nil, fmt.Errorf("failed to get 24h stats: %w", err)
	}

	return stats, nil
}

// CancelEmail cancels a pending email
func (r *EmailQueueRepository) CancelEmail(ctx context.Context, id int64) error {
	query := `
		UPDATE email_queue
		SET status = 'cancelled',
		    processed_at = $1
		WHERE id = $2 AND status IN ('pending', 'processing')
	`

	result, err := dbctx.GetQuerier(ctx, r.db).ExecContext(ctx, query, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to cancel email: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("email not found or already processed: %d", id)
	}

	logger.Logger.Info("Email cancelled", "id", id)
	return nil
}

// CleanupOldEmails removes old processed emails from the queue
func (r *EmailQueueRepository) CleanupOldEmails(ctx context.Context, olderThan time.Duration) (int64, error) {
	query := `
		DELETE FROM email_queue
		WHERE status IN ('sent', 'failed', 'cancelled')
		  AND processed_at < $1
	`

	cutoff := time.Now().Add(-olderThan)
	result, err := dbctx.GetQuerier(ctx, r.db).ExecContext(ctx, query, cutoff)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup old emails: %w", err)
	}

	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get deleted count: %w", err)
	}

	if deleted > 0 {
		logger.Logger.Info("Old emails cleaned up", "count", deleted, "older_than", olderThan)
	}

	return deleted, nil
}
