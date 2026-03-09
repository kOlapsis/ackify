// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"time"

	"github.com/kolapsis/ackify/backend/pkg/types"
)

// User is an alias for the unified user type.
// This allows web package to use web.User while sharing the same underlying type.
type User = types.User

// AuthResult represents the result of an authentication operation.
type AuthResult struct {
	User        *User
	RedirectURL string
}

// QuotaAction represents an action that can be quota-limited.
type QuotaAction string

const (
	QuotaActionCreateDocument    QuotaAction = "document.create"
	QuotaActionDeleteDocument    QuotaAction = "document.delete"
	QuotaActionCreateSignature   QuotaAction = "signature.create"
	QuotaActionSendReminder      QuotaAction = "reminder.send"
	QuotaActionCreateWebhook     QuotaAction = "webhook.create"
	QuotaActionAddExpectedSigner QuotaAction = "signer.add"
	QuotaActionWebhookDelivery   QuotaAction = "webhook.delivery"
)

// QuotaUsage represents current usage metrics for a tenant.
type QuotaUsage struct {
	TenantID   string
	Period     string // e.g., "2024-01" for monthly quotas
	Documents  UsageMetric
	Signatures UsageMetric
	Reminders  UsageMetric
	Webhooks   UsageMetric
}

// UsageMetric represents usage for a single resource type.
type UsageMetric struct {
	Used  int64
	Limit int64 // -1 means unlimited
}

// IsUnlimited returns true if the metric has no limit.
func (m UsageMetric) IsUnlimited() bool {
	return m.Limit < 0
}

// IsExceeded returns true if usage has exceeded the limit.
func (m UsageMetric) IsExceeded() bool {
	if m.IsUnlimited() {
		return false
	}
	return m.Used >= m.Limit
}

// Remaining returns the remaining quota, or -1 if unlimited.
func (m UsageMetric) Remaining() int64 {
	if m.IsUnlimited() {
		return -1
	}
	remaining := m.Limit - m.Used
	if remaining < 0 {
		return 0
	}
	return remaining
}

// AuditEvent represents an auditable action in the system.
type AuditEvent struct {
	Timestamp  time.Time
	TenantID   string
	UserEmail  string
	UserSub    string
	Action     string
	Resource   string
	ResourceID string
	Details    map[string]any
	IPAddress  string
	UserAgent  string
}

// AuditAction constants for common audit events.
const (
	AuditActionLogin           = "auth.login"
	AuditActionLogout          = "auth.logout"
	AuditActionDocumentCreate  = "document.create"
	AuditActionDocumentUpdate  = "document.update"
	AuditActionDocumentDelete  = "document.delete"
	AuditActionSignatureCreate = "signature.create"
	AuditActionReminderSend    = "reminder.send"
	AuditActionWebhookCreate   = "webhook.create"
	AuditActionWebhookUpdate   = "webhook.update"
	AuditActionWebhookDelete   = "webhook.delete"
	AuditActionSignerAdd       = "signer.add"
	AuditActionSignerRemove    = "signer.remove"
	AuditActionAdminAccess     = "admin.access"
)
