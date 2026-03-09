// SPDX-License-Identifier: AGPL-3.0-or-later
package workers

import (
	"context"
	"database/sql"
	"time"

	"github.com/kolapsis/ackify/backend/internal/application/services"
	"github.com/kolapsis/ackify/backend/internal/infrastructure/tenant"
	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/providers"
)

// MagicLinkCleanupWorker nettoie périodiquement les tokens expirés
type MagicLinkCleanupWorker struct {
	service  *services.MagicLinkService
	interval time.Duration
	stopChan chan struct{}

	// RLS support
	db      *sql.DB
	tenants providers.TenantProvider
}

func NewMagicLinkCleanupWorker(service *services.MagicLinkService, interval time.Duration, db *sql.DB, tenants providers.TenantProvider) *MagicLinkCleanupWorker {
	if interval == 0 {
		interval = 1 * time.Hour // Défaut: toutes les heures
	}

	return &MagicLinkCleanupWorker{
		service:  service,
		interval: interval,
		stopChan: make(chan struct{}),
		db:       db,
		tenants:  tenants,
	}
}

func (w *MagicLinkCleanupWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	logger.Logger.Info("Magic Link cleanup worker started", "interval", w.interval)

	for {
		select {
		case <-ticker.C:
			w.cleanup(ctx)
		case <-w.stopChan:
			logger.Logger.Info("Magic Link cleanup worker stopped")
			return
		case <-ctx.Done():
			logger.Logger.Info("Magic Link cleanup worker context cancelled")
			return
		}
	}
}

func (w *MagicLinkCleanupWorker) Stop() {
	close(w.stopChan)
}

func (w *MagicLinkCleanupWorker) cleanup(ctx context.Context) {
	// Get tenant ID for RLS context
	tenantID, err := w.tenants.CurrentTenant(ctx)
	if err != nil {
		logger.Logger.Error("Failed to get tenant for magic link cleanup", "error", err)
		return
	}

	var deleted int64
	err = tenant.WithTenantContext(ctx, w.db, tenantID, func(txCtx context.Context) error {
		var cleanupErr error
		deleted, cleanupErr = w.service.CleanupExpiredTokens(txCtx)
		return cleanupErr
	})
	if err != nil {
		logger.Logger.Error("Failed to cleanup expired magic link tokens", "error", err)
		return
	}

	if deleted > 0 {
		logger.Logger.Info("Cleaned up expired magic link tokens", "count", deleted)
	}
}
