// SPDX-License-Identifier: AGPL-3.0-or-later
package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/btouchard/ackify-ce/backend/internal/infrastructure/dbctx"
	"github.com/btouchard/ackify-ce/backend/pkg/models"
	"github.com/btouchard/ackify-ce/backend/pkg/providers"
	"github.com/google/uuid"
)

// Joined view of a delivery with webhook send data
type WebhookDeliveryItem struct {
	ID            int64
	TenantID      uuid.UUID
	WebhookID     int64
	EventType     string
	EventID       string
	Payload       []byte
	Status        string
	RetryCount    int
	MaxRetries    int
	Priority      int
	ScheduledFor  time.Time
	TargetURL     string
	Secret        string
	CustomHeaders map[string]string
}

type WebhookDeliveryRepository struct {
	db      *sql.DB
	tenants providers.TenantProvider
}

func NewWebhookDeliveryRepository(db *sql.DB, tenants providers.TenantProvider) *WebhookDeliveryRepository {
	return &WebhookDeliveryRepository{db: db, tenants: tenants}
}

func (r *WebhookDeliveryRepository) Enqueue(ctx context.Context, input models.WebhookDeliveryInput) (*models.WebhookDelivery, error) {
	tenantID, err := r.tenants.CurrentTenant(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}

	payloadJSON, err := json.Marshal(input.Payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}
	maxRetries := input.MaxRetries
	if maxRetries == 0 {
		maxRetries = 6
	}
	scheduled := time.Now()
	if input.ScheduledFor != nil {
		scheduled = *input.ScheduledFor
	}

	q := `
        INSERT INTO webhook_deliveries (tenant_id, webhook_id, event_type, event_id, payload, priority, max_retries, scheduled_for)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
        RETURNING id, tenant_id, status, retry_count, created_at, processed_at, next_retry_at
    `
	item := &models.WebhookDelivery{
		TenantID:     tenantID,
		WebhookID:    input.WebhookID,
		EventType:    input.EventType,
		EventID:      input.EventID,
		Payload:      payloadJSON,
		Priority:     input.Priority,
		MaxRetries:   maxRetries,
		ScheduledFor: scheduled,
	}
	err = dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, q,
		tenantID, input.WebhookID, input.EventType, input.EventID, payloadJSON, input.Priority, maxRetries, scheduled,
	).Scan(&item.ID, &item.TenantID, &item.Status, &item.RetryCount, &item.CreatedAt, &item.ProcessedAt, &item.NextRetryAt)
	if err != nil {
		return nil, fmt.Errorf("failed to enqueue webhook delivery: %w", err)
	}
	return item, nil
}

// GetNextToProcess fetches deliveries and moves them to processing; joins webhooks data
func (r *WebhookDeliveryRepository) GetNextToProcess(ctx context.Context, limit int) ([]*WebhookDeliveryItem, error) {
	// Use CTE to select and lock rows, then join
	q := `
        WITH picked AS (
            SELECT id FROM webhook_deliveries
            WHERE status = 'pending' AND scheduled_for <= $1
            ORDER BY priority DESC, scheduled_for ASC
            LIMIT $2
            FOR UPDATE SKIP LOCKED
        ), upd AS (
            UPDATE webhook_deliveries wd
            SET status = 'processing'
            WHERE wd.id IN (SELECT id FROM picked)
            RETURNING wd.*
        )
        SELECT u.id, u.tenant_id, u.webhook_id, u.event_type, u.event_id, u.payload, u.status, u.retry_count, u.max_retries, u.priority, u.scheduled_for,
               w.target_url, w.secret, w.headers
        FROM upd u
        JOIN webhooks w ON w.id = u.webhook_id
    `
	rows, err := dbctx.GetQuerier(ctx, r.db).QueryContext(ctx, q, time.Now(), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get next webhook deliveries: %w", err)
	}
	defer rows.Close()
	var out []*WebhookDeliveryItem
	for rows.Next() {
		var headersJSON models.NullRawMessage
		item := &WebhookDeliveryItem{}
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.WebhookID, &item.EventType, &item.EventID, &item.Payload, &item.Status, &item.RetryCount, &item.MaxRetries, &item.Priority, &item.ScheduledFor,
			&item.TargetURL, &item.Secret, &headersJSON,
		); err != nil {
			return nil, err
		}
		if headersJSON.Valid && len(headersJSON.RawMessage) > 0 {
			_ = json.Unmarshal(headersJSON.RawMessage, &item.CustomHeaders)
		}
		out = append(out, item)
	}
	return out, nil
}

func (r *WebhookDeliveryRepository) GetRetryable(ctx context.Context, limit int) ([]*WebhookDeliveryItem, error) {
	q := `
        WITH picked AS (
            SELECT id FROM webhook_deliveries
            WHERE status = 'pending' AND retry_count > 0 AND retry_count < max_retries AND scheduled_for <= $1
            ORDER BY priority DESC, scheduled_for ASC
            LIMIT $2
            FOR UPDATE SKIP LOCKED
        ), upd AS (
            UPDATE webhook_deliveries wd
            SET status = 'processing'
            WHERE wd.id IN (SELECT id FROM picked)
            RETURNING wd.*
        )
        SELECT u.id, u.tenant_id, u.webhook_id, u.event_type, u.event_id, u.payload, u.status, u.retry_count, u.max_retries, u.priority, u.scheduled_for,
               w.target_url, w.secret, w.headers
        FROM upd u
        JOIN webhooks w ON w.id = u.webhook_id
    `
	rows, err := dbctx.GetQuerier(ctx, r.db).QueryContext(ctx, q, time.Now(), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get retryable webhook deliveries: %w", err)
	}
	defer rows.Close()
	var out []*WebhookDeliveryItem
	for rows.Next() {
		var headersJSON models.NullRawMessage
		item := &WebhookDeliveryItem{}
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.WebhookID, &item.EventType, &item.EventID, &item.Payload, &item.Status, &item.RetryCount, &item.MaxRetries, &item.Priority, &item.ScheduledFor,
			&item.TargetURL, &item.Secret, &headersJSON,
		); err != nil {
			return nil, err
		}
		if headersJSON.Valid && len(headersJSON.RawMessage) > 0 {
			_ = json.Unmarshal(headersJSON.RawMessage, &item.CustomHeaders)
		}
		out = append(out, item)
	}
	return out, nil
}

func (r *WebhookDeliveryRepository) MarkDelivered(ctx context.Context, id int64, responseStatus int, responseHeaders map[string]string, responseBody string) error {
	headersJSON, _ := json.Marshal(responseHeaders)
	// Truncate response body to 4096 chars for storage safety
	if len(responseBody) > 4096 {
		responseBody = responseBody[:4096]
	}
	q := `
        UPDATE webhook_deliveries
        SET status='delivered', processed_at=now(), response_status=$1, response_headers=$2, response_body=$3
        WHERE id=$4
    `
	_, err := dbctx.GetQuerier(ctx, r.db).ExecContext(ctx, q, responseStatus, headersJSON, responseBody, id)
	return err
}

func (r *WebhookDeliveryRepository) MarkFailed(ctx context.Context, id int64, err error, shouldRetry bool) error {
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	if shouldRetry {
		q := `
            UPDATE webhook_deliveries
            SET status='pending', retry_count=retry_count+1, last_error=$1, scheduled_for=calculate_next_retry_time(retry_count+1)
            WHERE id=$2 AND retry_count < max_retries
        `
		res, e := dbctx.GetQuerier(ctx, r.db).ExecContext(ctx, q, errMsg, id)
		if e != nil {
			return e
		}
		if n, _ := res.RowsAffected(); n == 0 {
			// mark as permanently failed
			q := `UPDATE webhook_deliveries SET status='failed', processed_at=now(), last_error=$1 WHERE id=$2`
			_, e = dbctx.GetQuerier(ctx, r.db).ExecContext(ctx, q, errMsg, id)
			return e
		}
		return nil
	}
	q := `UPDATE webhook_deliveries SET status='failed', processed_at=now(), last_error=$1 WHERE id=$2`
	_, e := dbctx.GetQuerier(ctx, r.db).ExecContext(ctx, q, errMsg, id)
	return e
}

// ListByWebhook retrieves paginated webhook deliveries for a specific webhook
// RLS policy automatically filters by tenant_id
func (r *WebhookDeliveryRepository) ListByWebhook(ctx context.Context, webhookID int64, limit, offset int) ([]*models.WebhookDelivery, error) {
	q := `
        SELECT id, tenant_id, webhook_id, event_type, event_id, payload, status, retry_count, max_retries, priority,
               created_at, scheduled_for, processed_at, next_retry_at, request_headers, response_status, response_headers, response_body, last_error
        FROM webhook_deliveries
        WHERE webhook_id=$1
        ORDER BY id DESC
        LIMIT $2 OFFSET $3
    `
	rows, err := dbctx.GetQuerier(ctx, r.db).QueryContext(ctx, q, webhookID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list deliveries: %w", err)
	}
	defer rows.Close()
	var out []*models.WebhookDelivery
	for rows.Next() {
		d := &models.WebhookDelivery{}
		if err := rows.Scan(
			&d.ID, &d.TenantID, &d.WebhookID, &d.EventType, &d.EventID, &d.Payload, &d.Status, &d.RetryCount, &d.MaxRetries, &d.Priority,
			&d.CreatedAt, &d.ScheduledFor, &d.ProcessedAt, &d.NextRetryAt, &d.RequestHeaders, &d.ResponseStatus, &d.ResponseHeaders, &d.ResponseBody, &d.LastError,
		); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func (r *WebhookDeliveryRepository) CleanupOld(ctx context.Context, olderThan time.Duration) (int64, error) {
	q := `DELETE FROM webhook_deliveries WHERE status IN ('delivered','failed','cancelled') AND processed_at < $1`
	cutoff := time.Now().Add(-olderThan)
	res, err := dbctx.GetQuerier(ctx, r.db).ExecContext(ctx, q, cutoff)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup old deliveries: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
