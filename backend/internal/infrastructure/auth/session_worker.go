// SPDX-License-Identifier: AGPL-3.0-or-later
package auth

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/kolapsis/ackify/backend/internal/infrastructure/tenant"
	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/providers"
)

// SessionWorker handles background cleanup of expired OAuth sessions
type SessionWorker struct {
	sessionRepo     SessionRepository
	cleanupInterval time.Duration
	cleanupAge      time.Duration

	// RLS support
	db      *sql.DB
	tenants providers.TenantProvider

	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	stopChan chan struct{}
	started  bool
	mu       sync.Mutex
}

// SessionWorkerConfig contains configuration for the session worker
type SessionWorkerConfig struct {
	CleanupInterval time.Duration // How often to run cleanup (default: 24 hours)
	CleanupAge      time.Duration // Age of sessions to delete (default: 37 days = 30 + 7 grace period)
}

// DefaultSessionWorkerConfig returns default session worker configuration
func DefaultSessionWorkerConfig() SessionWorkerConfig {
	return SessionWorkerConfig{
		CleanupInterval: 24 * time.Hour,            // Run cleanup once per day
		CleanupAge:      (30 + 7) * 24 * time.Hour, // Delete sessions older than 37 days
	}
}

// NewSessionWorker creates a new OAuth session cleanup worker
func NewSessionWorker(sessionRepo SessionRepository, config SessionWorkerConfig, parentCtx context.Context, db *sql.DB, tenants providers.TenantProvider) *SessionWorker {
	// Apply defaults
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = 24 * time.Hour
	}
	if config.CleanupAge <= 0 {
		config.CleanupAge = 37 * 24 * time.Hour
	}

	ctx, cancel := context.WithCancel(parentCtx)

	return &SessionWorker{
		sessionRepo:     sessionRepo,
		cleanupInterval: config.CleanupInterval,
		cleanupAge:      config.CleanupAge,
		db:              db,
		tenants:         tenants,
		ctx:             ctx,
		cancel:          cancel,
		stopChan:        make(chan struct{}),
	}
}

// Start begins the cleanup worker
func (w *SessionWorker) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.started {
		return fmt.Errorf("session worker already started")
	}

	logger.Logger.Info("Starting OAuth session cleanup worker",
		"cleanup_interval", w.cleanupInterval,
		"cleanup_age", w.cleanupAge)

	w.started = true

	// Start the cleanup loop
	w.wg.Add(1)
	go w.cleanupLoop()

	return nil
}

// Stop gracefully stops the worker
func (w *SessionWorker) Stop() error {
	w.mu.Lock()
	if !w.started {
		w.mu.Unlock()
		return fmt.Errorf("session worker not started")
	}
	w.mu.Unlock()

	logger.Logger.Info("Stopping OAuth session cleanup worker...")

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
		logger.Logger.Info("OAuth session cleanup worker stopped gracefully")
	case <-time.After(30 * time.Second):
		logger.Logger.Warn("OAuth session cleanup worker stop timeout")
	}

	w.mu.Lock()
	w.started = false
	w.mu.Unlock()

	return nil
}

// cleanupLoop periodically cleans up expired sessions
func (w *SessionWorker) cleanupLoop() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.cleanupInterval)
	defer ticker.Stop()

	// Run cleanup immediately on start
	w.performCleanup()

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

// performCleanup removes expired OAuth sessions
func (w *SessionWorker) performCleanup() {
	ctx, cancel := context.WithTimeout(w.ctx, 5*time.Minute)
	defer cancel()

	logger.Logger.Debug("Starting OAuth session cleanup",
		"older_than", w.cleanupAge)

	var deleted int64
	var err error

	// Use RLS context if db and tenants are available
	if w.db != nil && w.tenants != nil {
		tenantID, tenantErr := w.tenants.CurrentTenant(ctx)
		if tenantErr != nil {
			logger.Logger.Error("Failed to get tenant for session cleanup",
				"error", tenantErr.Error())
			return
		}

		err = tenant.WithTenantContext(ctx, w.db, tenantID, func(txCtx context.Context) error {
			var cleanupErr error
			deleted, cleanupErr = w.sessionRepo.DeleteExpired(txCtx, w.cleanupAge)
			return cleanupErr
		})
	} else {
		// No RLS - direct repository access (for tests)
		deleted, err = w.sessionRepo.DeleteExpired(ctx, w.cleanupAge)
	}

	if err != nil {
		logger.Logger.Error("Failed to cleanup expired OAuth sessions",
			"error", err.Error())
		return
	}

	if deleted > 0 {
		logger.Logger.Info("Cleaned up expired OAuth sessions",
			"count", deleted,
			"older_than", w.cleanupAge)
	} else {
		logger.Logger.Debug("No expired OAuth sessions to clean up")
	}
}
