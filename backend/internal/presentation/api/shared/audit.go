// SPDX-License-Identifier: AGPL-3.0-or-later
package shared

import (
	"context"
	"net/http"
	"time"
)

// AuditLogger records auditable actions in the system.
// Defined here to avoid circular imports between internal handlers and pkg/web.
type AuditLogger interface {
	Log(ctx context.Context, event AuditEvent) error
}

// AuditEvent represents an auditable action.
type AuditEvent struct {
	Timestamp  time.Time
	TenantID   string
	UserEmail  string
	Action     string
	Resource   string
	ResourceID string
	Details    map[string]any
	IPAddress  string
	UserAgent  string
}

// EmitAudit is a convenience helper to emit an audit event from an HTTP handler.
// It silently ignores errors (audit logging should not break the request).
func EmitAudit(ctx context.Context, logger AuditLogger, r *http.Request, tenantID, action, resource, resourceID string, details map[string]any) {
	if logger == nil {
		return
	}
	userEmail := ""
	if user, ok := GetUserFromContext(ctx); ok && user != nil {
		userEmail = user.Email
	}
	_ = logger.Log(ctx, AuditEvent{
		Timestamp:  time.Now(),
		TenantID:   tenantID,
		UserEmail:  userEmail,
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Details:    details,
		IPAddress:  r.RemoteAddr,
		UserAgent:  r.UserAgent(),
	})
}
