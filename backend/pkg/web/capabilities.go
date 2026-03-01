// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"context"
	"errors"

	"github.com/btouchard/ackify-ce/backend/pkg/models"
	"github.com/btouchard/ackify-ce/backend/pkg/providers"
)

// Common errors for capability providers.
var (
	ErrNotAuthenticated = errors.New("user not authenticated")
	ErrNotAuthorized    = errors.New("user not authorized")
	ErrQuotaExceeded    = errors.New("quota exceeded")
	ErrProviderDisabled = errors.New("provider is disabled")
)

// Re-export types from pkg/providers for convenience.
type AuthProvider = providers.AuthProvider
type MagicLinkProvider = providers.MagicLinkProvider
type Authorizer = providers.Authorizer
type MagicLinkResult = providers.MagicLinkResult

// ConfigProvider provides dynamic configuration values.
type ConfigProvider interface {
	GetConfig() *models.MutableConfig
}

// QuotaEnforcer defines the interface for quota management.
// CE: NoLimitQuotaEnforcer (no limits).
// SaaS: PlanBasedQuotaEnforcer (limits based on subscription plan).
type QuotaEnforcer interface {
	// Check verifies if the action is allowed under current quotas.
	Check(ctx context.Context, tenantID string, action QuotaAction) error

	// Record records that an action was performed.
	Record(ctx context.Context, tenantID string, action QuotaAction) error

	// GetUsage returns the current usage metrics for a tenant.
	GetUsage(ctx context.Context, tenantID string) (*QuotaUsage, error)
}

// AuditLogger defines the interface for audit logging.
// CE: LogOnlyAuditLogger (logs to standard logger).
// SaaS: DatabaseAuditLogger (stores in database with search/export).
type AuditLogger interface {
	// Log records an audit event.
	Log(ctx context.Context, event AuditEvent) error
}

// StorageQuotaChecker verifies storage quota before file upload.
// CE: nil (no storage quota).
// SaaS: PlanBasedStorageQuotaChecker (limits based on subscription plan).
type StorageQuotaChecker interface {
	// CheckStorageQuota returns an error if the upload would exceed the storage quota.
	CheckStorageQuota(ctx context.Context, tenantID string, fileSize int64) error
}
