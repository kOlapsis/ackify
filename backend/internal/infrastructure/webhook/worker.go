// SPDX-License-Identifier: AGPL-3.0-or-later
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kolapsis/ackify/backend/internal/infrastructure/database"
	"github.com/kolapsis/ackify/backend/internal/infrastructure/tenant"
	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/providers"
)

// DeliveryRepository is the minimal interface used by the worker
type DeliveryRepository interface {
	GetNextToProcess(ctx context.Context, limit int) ([]*database.WebhookDeliveryItem, error)
	GetRetryable(ctx context.Context, limit int) ([]*database.WebhookDeliveryItem, error)
	MarkDelivered(ctx context.Context, id int64, responseStatus int, responseHeaders map[string]string, responseBody string) error
	MarkFailed(ctx context.Context, id int64, err error, shouldRetry bool) error
	CleanupOld(ctx context.Context, olderThan time.Duration) (int64, error)
}

// HTTPDoer abstracts http.Client for testing
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// WorkerConfig controls batch, concurrency and timings
type WorkerConfig struct {
	BatchSize       int
	PollInterval    time.Duration
	CleanupInterval time.Duration
	CleanupAge      time.Duration
	MaxConcurrent   int
	RequestTimeout  time.Duration
}

func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{BatchSize: 10, PollInterval: 5 * time.Second, CleanupInterval: 1 * time.Hour, CleanupAge: 30 * 24 * time.Hour, MaxConcurrent: 5, RequestTimeout: 10 * time.Second}
}

// Worker sends webhook deliveries asynchronously
type Worker struct {
	repo DeliveryRepository
	http HTTPDoer
	cfg  WorkerConfig

	// RLS support
	db      *sql.DB
	tenants providers.TenantProvider

	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	stopChan chan struct{}
	mu       sync.Mutex
	started  bool
}

func NewWorker(repo DeliveryRepository, httpClient HTTPDoer, cfg WorkerConfig, parentCtx context.Context, db *sql.DB, tenants providers.TenantProvider) *Worker {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 10
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Second
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = 1 * time.Hour
	}
	if cfg.CleanupAge <= 0 {
		cfg.CleanupAge = 30 * 24 * time.Hour
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 5
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 10 * time.Second
	}
	ctx, cancel := context.WithCancel(parentCtx)
	return &Worker{repo: repo, http: httpClient, cfg: cfg, db: db, tenants: tenants, ctx: ctx, cancel: cancel, stopChan: make(chan struct{})}
}

func (w *Worker) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started {
		return nil
	}
	w.started = true
	logger.Logger.Info("Starting webhook worker", "batch_size", w.cfg.BatchSize, "poll_interval", w.cfg.PollInterval)
	w.wg.Add(1)
	go w.processLoop()
	w.wg.Add(1)
	go w.cleanupLoop()
	return nil
}

func (w *Worker) Stop() error {
	w.mu.Lock()
	if !w.started {
		w.mu.Unlock()
		return nil
	}
	w.mu.Unlock()
	w.cancel()
	close(w.stopChan)
	done := make(chan struct{})
	go func() { w.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		logger.Logger.Warn("Webhook worker stop timeout")
	}
	w.mu.Lock()
	w.started = false
	w.mu.Unlock()
	return nil
}

func (w *Worker) processLoop() {
	defer w.wg.Done()
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()
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

func (w *Worker) cleanupLoop() {
	defer w.wg.Done()
	t := time.NewTicker(w.cfg.CleanupInterval)
	defer t.Stop()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-w.stopChan:
			return
		case <-t.C:
			w.performCleanup()
		}
	}
}

func (w *Worker) performCleanup() {
	ctx, cancel := context.WithTimeout(w.ctx, 5*time.Minute)
	defer cancel()

	// Cleanup is a cross-tenant background job — no RLS needed
	deleted, err := w.repo.CleanupOld(ctx, w.cfg.CleanupAge)
	if err != nil {
		logger.Logger.Error("Failed to cleanup webhook deliveries", "error", err.Error())
	} else if deleted > 0 {
		logger.Logger.Info("Cleaned webhook deliveries", "count", deleted)
	}
}

func (w *Worker) processBatch() {
	ctx, cancel := context.WithTimeout(w.ctx, 5*time.Minute)
	defer cancel()

	// Fetch pending items across all tenants (no RLS for background SELECT)
	items, err := w.repo.GetNextToProcess(ctx, w.cfg.BatchSize)
	if err == nil && len(items) == 0 {
		items, err = w.repo.GetRetryable(ctx, w.cfg.BatchSize)
	}
	if err != nil {
		logger.Logger.Error("Failed to get webhook deliveries", "error", err.Error())
		return
	}
	if len(items) == 0 {
		return
	}

	sem := make(chan struct{}, w.cfg.MaxConcurrent)
	var wg sync.WaitGroup
	for _, it := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func(item *database.WebhookDeliveryItem) {
			defer wg.Done()
			defer func() { <-sem }()

			// Use per-item tenant RLS context for processing/updating
			if w.db != nil && item.TenantID != uuid.Nil {
				err := tenant.WithTenantContext(ctx, w.db, item.TenantID, func(txCtx context.Context) error {
					w.processOne(txCtx, item)
					return nil
				})
				if err != nil {
					logger.Logger.Error("Failed to process webhook with tenant context",
						"id", item.ID,
						"error", err.Error())
				}
			} else {
				w.processOne(ctx, item)
			}
		}(it)
	}
	wg.Wait()
}

func (w *Worker) processOne(ctx context.Context, item *database.WebhookDeliveryItem) {
	// Build request
	reqBody := strings.NewReader(string(item.Payload))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, item.TargetURL, reqBody)
	if err != nil {
		_ = w.repo.MarkFailed(ctx, item.ID, err, true)
		return
	}
	// Default headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Ackify-Webhooks/1.0")

	timestamp := time.Now().UTC().Unix()
	signature := ComputeSignature(item.Secret, timestamp, item.EventID, item.EventType, item.Payload)
	req.Header.Set("X-Ackify-Event", item.EventType)
	req.Header.Set("X-Ackify-Event-Id", item.EventID)
	req.Header.Set("X-Ackify-Timestamp", fmtInt64(timestamp))
	req.Header.Set("X-Ackify-Signature", "sha256="+signature)

	// Custom headers
	for k, v := range item.CustomHeaders {
		req.Header.Set(k, v)
	}

	httpClient := w.http
	if client, ok := httpClient.(*http.Client); ok {
		client.Timeout = w.cfg.RequestTimeout
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		logger.Logger.Warn("Webhook delivery failed", "id", item.ID, "error", err.Error(), "retry", item.RetryCount)
		_ = w.repo.MarkFailed(ctx, item.ID, err, item.RetryCount < item.MaxRetries)
		return
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := string(bodyBytes)
	// Collect response headers
	respHeaders := map[string]string{}
	for k, vals := range resp.Header {
		if len(vals) > 0 {
			respHeaders[k] = vals[0]
		}
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_ = w.repo.MarkDelivered(ctx, item.ID, resp.StatusCode, respHeaders, bodyStr)
		logger.Logger.Info("Webhook delivered", "id", item.ID, "status", resp.StatusCode)
	} else {
		_ = w.repo.MarkFailed(ctx, item.ID, fmtError("HTTP %d", resp.StatusCode), item.RetryCount < item.MaxRetries)
		logger.Logger.Warn("Webhook non-2xx", "id", item.ID, "status", resp.StatusCode)
	}
}

func ComputeSignature(secret string, ts int64, eventID, event string, body []byte) string {
	base := strings.Builder{}
	base.WriteString(fmtInt64(ts))
	base.WriteString(".")
	base.WriteString(eventID)
	base.WriteString(".")
	base.WriteString(event)
	base.WriteString(".")
	base.Write(body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(base.String()))
	return hex.EncodeToString(mac.Sum(nil))
}

func fmtInt64(v int64) string { return strconv.FormatInt(v, 10) }

// Small wrappers to keep imports localized
func fmtError(format string, a ...any) error { return fmt.Errorf(format, a...) }
