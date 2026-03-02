// SPDX-License-Identifier: AGPL-3.0-or-later
package models

import (
	"time"

	"github.com/google/uuid"
)

// MagicLinkToken représente un token de connexion Magic Link
type MagicLinkToken struct {
	ID                 int64      `json:"id" db:"id"`
	TenantID           *uuid.UUID `json:"tenant_id,omitempty" db:"tenant_id"` // NULL for login requests, set for admin reminders
	Token              string     `json:"token" db:"token"`
	Email              string     `json:"email" db:"email"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
	ExpiresAt          time.Time  `json:"expires_at" db:"expires_at"`
	UsedAt             *time.Time `json:"used_at,omitempty" db:"used_at"`
	UsedByIP           *string    `json:"used_by_ip,omitempty" db:"used_by_ip"`
	UsedByUserAgent    *string    `json:"used_by_user_agent,omitempty" db:"used_by_user_agent"`
	RedirectTo         string     `json:"redirect_to" db:"redirect_to"` // URL destination après auth (ex: /?doc=xxx)
	CreatedByIP        string     `json:"created_by_ip" db:"created_by_ip"`
	CreatedByUserAgent string     `json:"created_by_user_agent" db:"created_by_user_agent"`
	Purpose            string     `json:"purpose" db:"purpose"`         // 'login', 'reminder_auth', or 'document_share'
	DocID              *string    `json:"doc_id,omitempty" db:"doc_id"` // Document ID for reminder_auth/document_share (NULL for login)

	// OTP fields (used for document_share purpose)
	OTPHash        *string    `json:"-" db:"otp_hash"`                        // Bcrypt hash of the 6-digit OTP (never exposed in JSON)
	OTPAttempts    int        `json:"otp_attempts" db:"otp_attempts"`         // Failed OTP verification attempts
	OTPMaxAttempts int        `json:"otp_max_attempts" db:"otp_max_attempts"` // Max allowed failed attempts (default: 5)
	RevokedAt      *time.Time `json:"revoked_at,omitempty" db:"revoked_at"`   // When the share was revoked (NULL = active)
	SharedBy       *string    `json:"shared_by,omitempty" db:"shared_by"`     // Email of the admin who created the share
}

// IsValid vérifie si le token est valide (non expiré, non utilisé, non révoqué, non verrouillé OTP)
func (t *MagicLinkToken) IsValid() bool {
	if t.RevokedAt != nil {
		return false // Révoqué
	}
	// For document_share tokens, we don't check UsedAt (they are multi-use)
	if t.Purpose != "document_share" && t.UsedAt != nil {
		return false // Déjà utilisé (single-use tokens only)
	}
	if time.Now().After(t.ExpiresAt) {
		return false // Expiré
	}
	if t.IsOTPLocked() {
		return false // Trop de tentatives OTP
	}
	return true
}

// IsOTPLocked returns true if the OTP attempt limit has been reached
func (t *MagicLinkToken) IsOTPLocked() bool {
	return t.OTPMaxAttempts > 0 && t.OTPAttempts >= t.OTPMaxAttempts
}

// MagicLinkAuthAttempt représente une tentative d'authentification
type MagicLinkAuthAttempt struct {
	ID            int64      `json:"id" db:"id"`
	TenantID      *uuid.UUID `json:"tenant_id,omitempty" db:"tenant_id"` // May be NULL before authentication
	Email         string     `json:"email" db:"email"`
	Success       bool       `json:"success" db:"success"`
	FailureReason string     `json:"failure_reason,omitempty" db:"failure_reason"`
	IPAddress     string     `json:"ip_address" db:"ip_address"`
	UserAgent     string     `json:"user_agent,omitempty" db:"user_agent"`
	AttemptedAt   time.Time  `json:"attempted_at" db:"attempted_at"`
}
