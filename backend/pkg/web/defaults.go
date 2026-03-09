// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"context"

	"github.com/kolapsis/ackify/backend/pkg/logger"
)

// NoLimitQuotaEnforcer is a quota enforcer that imposes no limits.
// This is the default for Community Edition.
type NoLimitQuotaEnforcer struct{}

func NewNoLimitQuotaEnforcer() *NoLimitQuotaEnforcer {
	return &NoLimitQuotaEnforcer{}
}

func (e *NoLimitQuotaEnforcer) Check(_ context.Context, _ string, _ QuotaAction) error {
	return nil
}

func (e *NoLimitQuotaEnforcer) Record(_ context.Context, _ string, _ QuotaAction) error {
	return nil
}

func (e *NoLimitQuotaEnforcer) GetUsage(_ context.Context, tenantID string) (*QuotaUsage, error) {
	unlimited := UsageMetric{Used: 0, Limit: -1}
	return &QuotaUsage{
		TenantID:   tenantID,
		Period:     "unlimited",
		Documents:  unlimited,
		Signatures: unlimited,
		Reminders:  unlimited,
		Webhooks:   unlimited,
	}, nil
}

// Compile-time interface check.
var _ QuotaEnforcer = (*NoLimitQuotaEnforcer)(nil)

// LogOnlyAuditLogger logs audit events to the standard logger.
// This is the default for Community Edition.
type LogOnlyAuditLogger struct{}

func NewLogOnlyAuditLogger() *LogOnlyAuditLogger {
	return &LogOnlyAuditLogger{}
}

func (l *LogOnlyAuditLogger) Log(_ context.Context, event AuditEvent) error {
	logger.Logger.Info("audit",
		"action", event.Action,
		"resource", event.Resource,
		"resource_id", event.ResourceID,
		"user_email", event.UserEmail,
		"tenant_id", event.TenantID,
		"ip", event.IPAddress,
	)
	return nil
}

// Compile-time interface check.
var _ AuditLogger = (*LogOnlyAuditLogger)(nil)
