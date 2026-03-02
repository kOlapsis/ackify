// SPDX-License-Identifier: AGPL-3.0-or-later
// Package providers defines capability interfaces for dependency injection.
// These interfaces are in a separate package to avoid import cycles.
package providers

import (
	"context"
	"net/http"
	"time"

	"github.com/btouchard/ackify-ce/backend/pkg/types"
	"github.com/google/uuid"
)

// Common errors for capability providers.
// Defined as strings to avoid import cycles - implementations can wrap these.
const (
	ErrNotAuthenticatedMsg = "user not authenticated"
	ErrNotAuthorizedMsg    = "user not authorized"
	ErrQuotaExceededMsg    = "quota exceeded"
	ErrProviderDisabledMsg = "provider is disabled"
)

// AuthProvider defines the unified interface for all authentication methods.
// This single interface handles sessions, OIDC, MagicLink, and future auth methods.
// Configuration is read dynamically from ConfigService to support hot-reload.
type AuthProvider interface {
	// === Session Management (always available) ===

	// GetCurrentUser returns the authenticated user from the session.
	GetCurrentUser(r *http.Request) (*types.User, error)

	// SetCurrentUser stores the authenticated user in the session.
	SetCurrentUser(w http.ResponseWriter, r *http.Request, user *types.User) error

	// Logout clears the user session.
	Logout(w http.ResponseWriter, r *http.Request)

	// IsConfigured returns true if at least one auth method is enabled.
	IsConfigured() bool

	// === OIDC Authentication (dynamically enabled via config) ===

	// IsOIDCEnabled returns true if OIDC is enabled in current config.
	IsOIDCEnabled() bool

	// StartOIDC generates the OAuth2/OIDC authorization URL.
	StartOIDC(w http.ResponseWriter, r *http.Request, nextURL string) string

	// VerifyOIDCState verifies the OAuth2 state token to prevent CSRF.
	VerifyOIDCState(w http.ResponseWriter, r *http.Request, stateToken string) bool

	// HandleOIDCCallback processes the OAuth2/OIDC callback.
	HandleOIDCCallback(ctx context.Context, w http.ResponseWriter, r *http.Request, code, state string) (*types.User, string, error)

	// GetOIDCLogoutURL returns the OIDC provider's logout URL if configured.
	GetOIDCLogoutURL() string

	// IsAllowedDomain checks if the email domain is allowed for OIDC.
	IsAllowedDomain(email string) bool

	// === MagicLink Authentication (dynamically enabled via config) ===

	// IsMagicLinkEnabled returns true if MagicLink is enabled in current config.
	IsMagicLinkEnabled() bool

	// RequestMagicLink sends a magic link email.
	RequestMagicLink(ctx context.Context, email, redirectTo, ip, userAgent, locale string) error

	// VerifyMagicLink verifies a magic link token and returns user info.
	VerifyMagicLink(ctx context.Context, token, ip, userAgent string) (*MagicLinkResult, error)

	// VerifyReminderAuthToken verifies a reminder auth token.
	VerifyReminderAuthToken(ctx context.Context, token, ip, userAgent string) (*MagicLinkResult, error)

	// CreateReminderAuthToken creates an auth token for reminder emails.
	CreateReminderAuthToken(ctx context.Context, email, docID string) (string, error)

	// === Document Share with OTP ===

	// CreateDocumentShareLink creates a magic link protected by OTP for sharing a document.
	// Returns the link URL and the cleartext OTP (shown once to the admin).
	CreateDocumentShareLink(ctx context.Context, email, docID string, validityDays int, sharedBy, locale string) (linkURL string, otp string, err error)

	// ValidateDocumentShareToken checks if a token is a valid, active document_share token.
	// This is a read-only check that does NOT verify OTP or modify attempt counters.
	ValidateDocumentShareToken(ctx context.Context, token string) error

	// VerifyDocumentShareOTP verifies the OTP for a document share token.
	VerifyDocumentShareOTP(ctx context.Context, token, otp, ip, userAgent string) (*MagicLinkResult, error)

	// RevokeDocumentShare revokes a document share by token ID.
	RevokeDocumentShare(ctx context.Context, tokenID int64) error

	// ListDocumentShares returns all document shares for a given document.
	ListDocumentShares(ctx context.Context, docID string) ([]*DocumentShareInfo, error)
}

// MagicLinkResult represents the result of verifying a magic link.
type MagicLinkResult struct {
	Email      string
	RedirectTo string
	DocID      *string // Non-nil for reminder auth tokens
}

// DocumentShareInfo represents info about a document share for the admin listing.
type DocumentShareInfo struct {
	ID             int64      `json:"id"`
	Email          string     `json:"email"`
	CreatedAt      time.Time  `json:"created_at"`
	ExpiresAt      time.Time  `json:"expires_at"`
	OTPAttempts    int        `json:"otp_attempts"`
	OTPMaxAttempts int        `json:"otp_max_attempts"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
	SharedBy       *string    `json:"shared_by,omitempty"`
	IsActive       bool       `json:"is_active"`
}

// Authorizer defines the interface for authorization decisions.
// CE: SimpleAuthorizer based on admin email list.
// SaaS: RBACAuthorizer with roles and permissions.
type Authorizer interface {
	// IsAdmin returns true if the user is an administrator.
	IsAdmin(ctx context.Context, userEmail string) bool

	// CanCreateDocument returns true if the user can create documents.
	CanCreateDocument(ctx context.Context, userEmail string) bool

	// CanManageDocument returns true if the user can manage (edit/delete) a document.
	// Returns true if user is admin or is the document creator.
	CanManageDocument(ctx context.Context, userEmail, docCreatedBy string) bool
}

// === Legacy interfaces for backward compatibility ===
// These will be removed in a future version.

// OAuthAuthProvider is deprecated. Use AuthProvider instead.
type OAuthAuthProvider interface {
	AuthProvider
}

// TenantProvider defines the interface for obtaining the current tenant ID.
type TenantProvider interface {
	CurrentTenant(ctx context.Context) (uuid.UUID, error)
}
