// SPDX-License-Identifier: AGPL-3.0-or-later
package email

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/btouchard/ackify-ce/backend/internal/infrastructure/tenant"
	"github.com/btouchard/ackify-ce/backend/pkg/logger"
	"github.com/btouchard/ackify-ce/backend/pkg/models"
	"github.com/btouchard/ackify-ce/backend/pkg/providers"
	"github.com/google/uuid"
)

// EmailErrorType represents the category of an email sending error
type EmailErrorType int

const (
	// ErrorTypeRetryable represents temporary errors that should be retried
	// Examples: 4xx SMTP codes, network timeouts, connection refused
	ErrorTypeRetryable EmailErrorType = iota

	// ErrorTypePermanent represents permanent errors that should not be retried
	// Examples: 5xx SMTP codes, invalid email format, mailbox not found
	ErrorTypePermanent

	// ErrorTypeRateLimited represents rate limiting errors
	// Examples: 429 HTTP, SMTP rate limit exceeded
	ErrorTypeRateLimited
)

// QueueRepository defines the interface for email queue operations
type QueueRepository interface {
	Enqueue(ctx context.Context, input models.EmailQueueInput) (*models.EmailQueueItem, error)
	GetNextToProcess(ctx context.Context, limit int) ([]*models.EmailQueueItem, error)
	MarkAsSent(ctx context.Context, id int64) error
	MarkAsFailed(ctx context.Context, id int64, err error, shouldRetry bool) error
	MarkAsFailedWithDelay(ctx context.Context, id int64, err error, shouldRetry bool, retryDelay time.Duration) error
	GetRetryableEmails(ctx context.Context, limit int) ([]*models.EmailQueueItem, error)
	CleanupOldEmails(ctx context.Context, olderThan time.Duration) (int64, error)
}

// Worker processes emails from the queue asynchronously
type Worker struct {
	queueRepo QueueRepository
	sender    Sender
	renderer  *Renderer
	publisher EventPublisher

	// RLS support
	db      *sql.DB
	tenants providers.TenantProvider

	// Worker configuration
	batchSize       int
	pollInterval    time.Duration
	cleanupInterval time.Duration
	cleanupAge      time.Duration
	maxConcurrent   int

	// Control
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	stopChan chan struct{}
	started  bool
	mu       sync.Mutex
}

// WorkerConfig contains configuration for the email worker
type WorkerConfig struct {
	BatchSize       int           // Number of emails to process in each batch (default: 10)
	PollInterval    time.Duration // How often to check for new emails (default: 5s)
	CleanupInterval time.Duration // How often to cleanup old emails (default: 1 hour)
	CleanupAge      time.Duration // Age of emails to cleanup (default: 7 days)
	MaxConcurrent   int           // Maximum concurrent email sends (default: 5)
}

// DefaultWorkerConfig returns default worker configuration
func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		BatchSize:       10,
		PollInterval:    5 * time.Second,
		CleanupInterval: 1 * time.Hour,
		CleanupAge:      7 * 24 * time.Hour, // 7 days
		MaxConcurrent:   5,
	}
}

// NewWorker creates a new email worker
func NewWorker(queueRepo QueueRepository, sender Sender, renderer *Renderer, config WorkerConfig, parentCtx context.Context, db *sql.DB, tenants providers.TenantProvider) *Worker {
	// Apply defaults
	if config.BatchSize <= 0 {
		config.BatchSize = 10
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 5 * time.Second
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = 1 * time.Hour
	}
	if config.CleanupAge <= 0 {
		config.CleanupAge = 7 * 24 * time.Hour
	}
	if config.MaxConcurrent <= 0 {
		config.MaxConcurrent = 5
	}

	ctx, cancel := context.WithCancel(parentCtx)

	return &Worker{
		queueRepo:       queueRepo,
		sender:          sender,
		renderer:        renderer,
		db:              db,
		tenants:         tenants,
		batchSize:       config.BatchSize,
		pollInterval:    config.PollInterval,
		cleanupInterval: config.CleanupInterval,
		cleanupAge:      config.CleanupAge,
		maxConcurrent:   config.MaxConcurrent,
		ctx:             ctx,
		cancel:          cancel,
		stopChan:        make(chan struct{}),
	}
}

// EventPublisher publishes webhook-like events ( decoupled interface )
type EventPublisher interface {
	Publish(ctx context.Context, eventType string, payload map[string]interface{}) error
}

// SetPublisher injects an optional event publisher (e.g., webhooks)
func (w *Worker) SetPublisher(p EventPublisher) { w.publisher = p }

// Start begins processing emails from the queue
func (w *Worker) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.started {
		return fmt.Errorf("worker already started")
	}

	logger.Logger.Info("Starting email worker",
		"batch_size", w.batchSize,
		"poll_interval", w.pollInterval,
		"max_concurrent", w.maxConcurrent)

	w.started = true

	// Start the main processing loop
	w.wg.Add(1)
	go w.processLoop()

	// Start the cleanup loop
	w.wg.Add(1)
	go w.cleanupLoop()

	return nil
}

// Stop gracefully stops the worker
func (w *Worker) Stop() error {
	w.mu.Lock()
	if !w.started {
		w.mu.Unlock()
		return fmt.Errorf("worker not started")
	}
	w.mu.Unlock()

	logger.Logger.Info("Stopping email worker...")

	// Signal shutdown
	w.cancel()
	close(w.stopChan)

	// Wait for goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Logger.Info("Email worker stopped gracefully")
	case <-time.After(30 * time.Second):
		logger.Logger.Warn("Email worker stop timeout, some operations may not have completed")
	}

	w.mu.Lock()
	w.started = false
	w.mu.Unlock()

	return nil
}

// processLoop is the main processing loop
func (w *Worker) processLoop() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	// Immediate first check
	w.processBatch()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-w.stopChan:
			return
		case <-ticker.C:
			w.processBatch()
		}
	}
}

// processBatch processes a batch of emails
func (w *Worker) processBatch() {
	ctx, cancel := context.WithTimeout(w.ctx, 5*time.Minute)
	defer cancel()

	// Fetch pending emails across all tenants (no RLS for background SELECT)
	emails, err := w.queueRepo.GetNextToProcess(ctx, w.batchSize)
	if err == nil && len(emails) == 0 {
		emails, err = w.queueRepo.GetRetryableEmails(ctx, w.batchSize)
	}
	if err != nil {
		logger.Logger.Error("Failed to get emails to process", "error", err.Error())
		return
	}

	if len(emails) == 0 {
		return
	}

	logger.Logger.Debug("Processing email batch", "count", len(emails))

	sem := make(chan struct{}, w.maxConcurrent)
	var wg sync.WaitGroup

	for _, email := range emails {
		wg.Add(1)
		sem <- struct{}{}

		go func(item *models.EmailQueueItem) {
			defer wg.Done()
			defer func() { <-sem }()

			// Use per-item tenant RLS context for processing/updating
			if w.db != nil && item.TenantID != uuid.Nil {
				err := tenant.WithTenantContext(ctx, w.db, item.TenantID, func(txCtx context.Context) error {
					w.processEmail(txCtx, item)
					return nil
				})
				if err != nil {
					logger.Logger.Error("Failed to process email with tenant context",
						"id", item.ID,
						"error", err.Error())
				}
			} else {
				w.processEmail(ctx, item)
			}
		}(email)
	}

	wg.Wait()
}

// processEmail processes a single email
func (w *Worker) processEmail(ctx context.Context, item *models.EmailQueueItem) {
	logger.Logger.Debug("Processing email",
		"id", item.ID,
		"template", item.Template,
		"retry_count", item.RetryCount)

	// Convert data from JSON to map
	var data map[string]interface{}
	if len(item.Data) > 0 {
		if err := json.Unmarshal(item.Data, &data); err != nil {
			logger.Logger.Error("Failed to unmarshal email data",
				"id", item.ID,
				"error", err.Error())
			// Mark as failed without retry (data corruption)
			w.queueRepo.MarkAsFailed(ctx, item.ID, err, false)
			return
		}
	}

	// Convert headers from JSON to map
	var headers map[string]string
	if item.Headers.Valid && len(item.Headers.RawMessage) > 0 {
		if err := json.Unmarshal(item.Headers.RawMessage, &headers); err != nil {
			logger.Logger.Error("Failed to unmarshal email headers",
				"id", item.ID,
				"error", err.Error())
			// Continue without headers
			headers = nil
		}
	}

	// Create message
	msg := Message{
		To:       item.ToAddresses,
		Cc:       item.CcAddresses,
		Bcc:      item.BccAddresses,
		Subject:  item.Subject,
		Template: item.Template,
		Locale:   item.Locale,
		Data:     data,
		Headers:  headers,
	}

	// Send email
	err := w.sender.Send(ctx, msg)
	if err != nil {
		logger.Logger.Warn("Failed to send email",
			"id", item.ID,
			"template", item.Template,
			"error", err.Error(),
			"retry_count", item.RetryCount)

		// Categorize error and determine retry strategy
		errorType := w.categorizeError(err)
		shouldRetry := item.RetryCount < item.MaxRetries && w.isRetryableError(err)
		retryDelay := w.calculateRetryDelay(errorType, item.RetryCount)

		// Log error type for debugging
		logger.Logger.Debug("Email error categorized",
			"id", item.ID,
			"error_type", errorType,
			"should_retry", shouldRetry,
			"retry_delay", retryDelay)

		// Mark as failed with appropriate retry delay
		if markErr := w.queueRepo.MarkAsFailedWithDelay(ctx, item.ID, err, shouldRetry, retryDelay); markErr != nil {
			logger.Logger.Error("Failed to mark email as failed",
				"id", item.ID,
				"error", markErr.Error())
		}

		// Publish reminder.failed event
		if w.publisher != nil {
			payload := map[string]interface{}{
				"template": item.Template,
				"to":       item.ToAddresses,
			}
			if item.ReferenceType != nil && item.ReferenceID != nil && *item.ReferenceType == "signature_reminder" {
				payload["doc_id"] = *item.ReferenceID
			}
			_ = w.publisher.Publish(ctx, "reminder.failed", payload)
		}
		return
	}

	// Mark as sent
	if err := w.queueRepo.MarkAsSent(ctx, item.ID); err != nil {
		logger.Logger.Error("Failed to mark email as sent",
			"id", item.ID,
			"error", err.Error())
		// Email was sent but we failed to update the database
		// This is not critical, the email won't be resent
	}

	logger.Logger.Info("Email sent successfully",
		"id", item.ID,
		"template", item.Template,
		"to", item.ToAddresses)

	// Publish reminder.sent event
	if w.publisher != nil {
		payload := map[string]interface{}{
			"template": item.Template,
			"to":       item.ToAddresses,
		}
		if item.ReferenceType != nil && item.ReferenceID != nil && *item.ReferenceType == "signature_reminder" {
			payload["doc_id"] = *item.ReferenceID
		}
		_ = w.publisher.Publish(ctx, "reminder.sent", payload)
	}
}

// cleanupLoop periodically cleans up old emails
func (w *Worker) cleanupLoop() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-w.stopChan:
			return
		case <-ticker.C:
			w.performCleanup()
		}
	}
}

// performCleanup removes old processed emails
func (w *Worker) performCleanup() {
	ctx, cancel := context.WithTimeout(w.ctx, 5*time.Minute)
	defer cancel()

	// Cleanup is a cross-tenant background job — no RLS needed
	deleted, err := w.queueRepo.CleanupOldEmails(ctx, w.cleanupAge)
	if err != nil {
		logger.Logger.Error("Failed to cleanup old emails", "error", err.Error())
		return
	}

	if deleted > 0 {
		logger.Logger.Info("Cleaned up old emails", "count", deleted)
	}
}

// categorizeError analyzes the error and determines the error type
func (w *Worker) categorizeError(err error) EmailErrorType {
	if err == nil {
		return ErrorTypeRetryable
	}

	errStr := strings.ToLower(err.Error())

	// Permanent errors - SMTP 5xx codes (permanent failure)
	// 550: Mailbox not found, user unknown
	// 551: User not local; please try forward path
	// 552: Exceeded storage allocation
	// 553: Mailbox name not allowed
	// 554: Transaction failed
	if strings.Contains(errStr, "550") ||
		strings.Contains(errStr, "551") ||
		strings.Contains(errStr, "552") ||
		strings.Contains(errStr, "553") ||
		strings.Contains(errStr, "554") {
		return ErrorTypePermanent
	}

	// Permanent errors - Invalid email format or data corruption
	if strings.Contains(errStr, "invalid email") ||
		strings.Contains(errStr, "invalid recipient") ||
		strings.Contains(errStr, "invalid sender") ||
		strings.Contains(errStr, "unmarshal") ||
		strings.Contains(errStr, "template not found") ||
		strings.Contains(errStr, "invalid template") {
		return ErrorTypePermanent
	}

	// Rate limiting errors
	// 421: Service not available (often rate limiting)
	// 429: Too many requests (HTTP)
	// 450: Mailbox unavailable (temporary, might be rate limit)
	if strings.Contains(errStr, "421") ||
		strings.Contains(errStr, "429") ||
		strings.Contains(errStr, "450") ||
		strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "too many requests") ||
		strings.Contains(errStr, "quota exceeded") {
		return ErrorTypeRateLimited
	}

	// Retryable errors - SMTP 4xx codes (temporary failure)
	// 451: Requested action aborted (temporary error)
	// 452: Insufficient system storage
	if strings.Contains(errStr, "451") ||
		strings.Contains(errStr, "452") {
		return ErrorTypeRetryable
	}

	// Retryable errors - Network and connection issues
	if strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "network") ||
		strings.Contains(errStr, "dial") ||
		strings.Contains(errStr, "eof") ||
		strings.Contains(errStr, "broken pipe") {
		return ErrorTypeRetryable
	}

	// Retryable errors - DNS issues (temporary)
	if strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "dns") {
		return ErrorTypeRetryable
	}

	// Retryable errors - TLS/SSL issues (might be temporary)
	if strings.Contains(errStr, "tls") ||
		strings.Contains(errStr, "certificate") {
		return ErrorTypeRetryable
	}

	// Default to retryable for unknown errors
	// Better to retry and potentially waste resources than to lose emails
	return ErrorTypeRetryable
}

// isRetryableError determines if an error is retryable based on error type
func (w *Worker) isRetryableError(err error) bool {
	errorType := w.categorizeError(err)

	switch errorType {
	case ErrorTypePermanent:
		// Never retry permanent errors
		return false
	case ErrorTypeRateLimited:
		// Retry rate limited errors, but they will have exponential backoff
		return true
	case ErrorTypeRetryable:
		// Retry temporary errors
		return true
	default:
		// Default to retryable
		return true
	}
}

// calculateRetryDelay calculates the appropriate retry delay based on error type
func (w *Worker) calculateRetryDelay(errorType EmailErrorType, retryCount int) time.Duration {
	var baseDelay time.Duration

	switch errorType {
	case ErrorTypePermanent:
		// No retry for permanent errors
		return 0

	case ErrorTypeRateLimited:
		// Aggressive exponential backoff for rate limiting
		// Start at 5 minutes, then 15min, 45min, 2h15min, etc.
		// Formula: 5 * 3^retryCount minutes
		baseDelay = 5 * time.Minute
		multiplier := 1.0
		for i := 0; i < retryCount; i++ {
			multiplier *= 3
		}
		return time.Duration(float64(baseDelay) * multiplier)

	case ErrorTypeRetryable:
		// Standard exponential backoff for temporary errors
		// 1min, 2min, 4min, 8min, 16min, 32min...
		// Formula: 1 * 2^retryCount minutes
		baseDelay = 1 * time.Minute
		return baseDelay * time.Duration(1<<uint(retryCount))

	default:
		// Default: conservative exponential backoff
		// 2min, 4min, 8min, 16min, 32min...
		baseDelay = 2 * time.Minute
		return baseDelay * time.Duration(1<<uint(retryCount))
	}
}
