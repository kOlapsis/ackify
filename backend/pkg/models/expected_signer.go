// SPDX-License-Identifier: AGPL-3.0-or-later
package models

import (
	"time"

	"github.com/google/uuid"
)

// ExpectedSigner represents an expected signer for a document
type ExpectedSigner struct {
	ID       int64     `json:"id" db:"id"`
	TenantID uuid.UUID `json:"tenant_id" db:"tenant_id"`
	DocID    string    `json:"doc_id" db:"doc_id"`
	Email    string    `json:"email" db:"email"`
	Name     string    `json:"name" db:"name"`
	AddedAt  time.Time `json:"added_at" db:"added_at"`
	AddedBy  string    `json:"added_by" db:"added_by"`
	Notes    *string   `json:"notes,omitempty" db:"notes"`
}

// ExpectedSignerWithStatus combines expected signer info with signature status
type ExpectedSignerWithStatus struct {
	ExpectedSigner
	HasSigned             bool       `json:"has_signed"`
	SignedAt              *time.Time `json:"signed_at,omitempty"`
	UserName              *string    `json:"user_name,omitempty"`
	LastReminderSent      *time.Time `json:"last_reminder_sent,omitempty"`
	ReminderCount         int        `json:"reminder_count"`
	DaysSinceAdded        int        `json:"days_since_added"`
	DaysSinceLastReminder *int       `json:"days_since_last_reminder,omitempty"`
}

// DocCompletionStats provides completion statistics for a document
type DocCompletionStats struct {
	DocID          string  `json:"doc_id"`
	ExpectedCount  int     `json:"expected_count"`
	SignedCount    int     `json:"signed_count"`
	PendingCount   int     `json:"pending_count"`
	CompletionRate float64 `json:"completion_rate"` // Percentage 0-100
}

// PendingDocument represents a document awaiting confirmation by a specific user
type PendingDocument struct {
	DocID   string    `json:"doc_id"`
	Title   string    `json:"title"`
	AddedAt time.Time `json:"added_at"`
}

// ContactInfo represents a contact with optional name and email
type ContactInfo struct {
	Name  string
	Email string
}
