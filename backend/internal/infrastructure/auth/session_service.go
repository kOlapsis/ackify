// SPDX-License-Identifier: AGPL-3.0-or-later
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/sessions"
	"golang.org/x/oauth2"

	"github.com/kolapsis/ackify/backend/pkg/crypto"
	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/models"
)

// SessionService manages user sessions independently of authentication method
// This service is always required, regardless of whether OAuth or MagicLink is used
type SessionService struct {
	sessionStore  *sessions.CookieStore
	sessionRepo   SessionRepository
	encryptionKey []byte
	secureCookies bool
}

// SessionServiceConfig holds configuration for the session service
type SessionServiceConfig struct {
	CookieSecret  []byte
	SecureCookies bool
	SessionRepo   SessionRepository
}

// NewSessionService creates a new session service
func NewSessionService(config SessionServiceConfig) *SessionService {
	sessionStore := sessions.NewCookieStore(config.CookieSecret)

	// Configure session options globally on the store
	sessionStore.Options = &sessions.Options{
		Path:     "/",
		HttpOnly: true,
		Secure:   config.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400 * 30, // 30 days
	}

	logger.Logger.Info("Session store configured",
		"secure_cookies", config.SecureCookies,
		"max_age_days", 30)

	// Use CookieSecret as encryption key (must be 32 bytes for AES-256)
	encryptionKey := config.CookieSecret
	if len(encryptionKey) < 32 {
		logger.Logger.Warn("Encryption key too short, padding to 32 bytes",
			"original_length", len(encryptionKey))
		// Pad with zeros (not ideal, but prevents crashes)
		padded := make([]byte, 32)
		copy(padded, encryptionKey)
		encryptionKey = padded
	} else if len(encryptionKey) > 32 {
		// Truncate to 32 bytes for AES-256
		encryptionKey = encryptionKey[:32]
	}

	return &SessionService{
		sessionStore:  sessionStore,
		sessionRepo:   config.SessionRepo,
		encryptionKey: encryptionKey,
		secureCookies: config.SecureCookies,
	}
}

// GetUser retrieves the authenticated user from the session
func (s *SessionService) GetUser(r *http.Request) (*models.User, error) {
	session, err := s.sessionStore.Get(r, sessionName)
	if err != nil {
		logger.Logger.Debug("GetUser: failed to get session", "error", err.Error())
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	userJSON, ok := session.Values["user"].(string)
	if !ok || userJSON == "" {
		logger.Logger.Debug("GetUser: no user in session",
			"user_key_exists", ok,
			"user_json_empty", userJSON == "")
		return nil, models.ErrUnauthorized
	}

	var user models.User
	if err := json.Unmarshal([]byte(userJSON), &user); err != nil {
		logger.Logger.Error("GetUser: failed to unmarshal user", "error", err.Error())
		return nil, fmt.Errorf("failed to unmarshal user: %w", err)
	}

	logger.Logger.Debug("GetUser: user found", "email", user.Email)
	return &user, nil
}

// SetUser stores a user in the session (works for both OAuth and MagicLink)
func (s *SessionService) SetUser(w http.ResponseWriter, r *http.Request, user *models.User) error {
	// Always create a fresh new session to ensure session ID is generated
	// This fixes an issue where reusing an existing invalid session results in empty session.ID
	session, err := s.sessionStore.New(r, sessionName)
	if err != nil {
		logger.Logger.Error("SetUser: failed to create new session", "error", err.Error())
		return fmt.Errorf("failed to create new session: %w", err)
	}

	userJSON, err := json.Marshal(user)
	if err != nil {
		logger.Logger.Error("SetUser: failed to marshal user", "error", err.Error())
		return fmt.Errorf("failed to marshal user: %w", err)
	}

	logger.Logger.Debug("SetUser: saving user to new session",
		"email", user.Email,
		"secure_cookies", s.secureCookies,
		"session_is_new", session.IsNew)

	session.Values["user"] = string(userJSON)

	// Session options are already configured globally on the store
	// No need to set them again here

	if err := session.Save(r, w); err != nil {
		logger.Logger.Error("SetUser: failed to save session",
			"error", err.Error(),
			"session_is_new", session.IsNew,
			"session_id_length", len(session.ID))
		return fmt.Errorf("failed to save session: %w", err)
	}

	logger.Logger.Info("SetUser: session saved successfully",
		"email", user.Email,
		"session_id_length", len(session.ID))
	return nil
}

// Logout clears the user session
func (s *SessionService) Logout(w http.ResponseWriter, r *http.Request) {
	session, _ := s.sessionStore.Get(r, sessionName)

	// Clear all session values first (important for cookie-based sessions)
	for key := range session.Values {
		delete(session.Values, key)
	}

	// Set MaxAge to -1 to expire the cookie
	session.Options.MaxAge = -1

	// Save the cleared session
	_ = session.Save(r, w)

	logger.Logger.Debug("Logout: session cleared")
}

// GetSession returns the raw session (useful for storing additional data like OAuth state)
func (s *SessionService) GetSession(r *http.Request) (*sessions.Session, error) {
	return s.sessionStore.Get(r, sessionName)
}

// GetNewSession creates a new session
func (s *SessionService) GetNewSession(r *http.Request) (*sessions.Session, error) {
	return s.sessionStore.New(r, sessionName)
}

// StoreRefreshToken encrypts and stores the OAuth refresh token
// This is called by OAuthProvider after successful authentication
func (s *SessionService) StoreRefreshToken(ctx context.Context, w http.ResponseWriter, r *http.Request, token *oauth2.Token, user *models.User) error {
	if s.sessionRepo == nil {
		return fmt.Errorf("session repository not configured")
	}

	if s.encryptionKey == nil {
		return fmt.Errorf("encryption key not configured")
	}

	// Encrypt refresh token
	encryptedToken, err := crypto.EncryptToken(token.RefreshToken, s.encryptionKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt refresh token: %w", err)
	}

	// Generate unique session ID for OAuth session tracking
	sessionID := generateSessionID()

	// Get client IP and user agent for security tracking
	ipAddress := getClientIP(r)
	userAgent := r.UserAgent()

	// Create OAuth session
	oauthSession := &models.OAuthSession{
		SessionID:             sessionID,
		UserSub:               user.Sub,
		RefreshTokenEncrypted: encryptedToken,
		AccessTokenExpiresAt:  token.Expiry,
		UserAgent:             userAgent,
		IPAddress:             ipAddress,
	}

	// Save to database
	if err := s.sessionRepo.Create(ctx, oauthSession); err != nil {
		return fmt.Errorf("failed to create OAuth session: %w", err)
	}

	// Link OAuth session ID to user session
	userSession, _ := s.sessionStore.Get(r, sessionName)
	userSession.Values["oauth_session_id"] = sessionID
	if err := userSession.Save(r, w); err != nil {
		logger.Logger.Error("Failed to link OAuth session to user session",
			"session_id", sessionID,
			"error", err.Error())
		// Don't return error, session is already created in DB
	}

	logger.Logger.Info("Stored encrypted refresh token",
		"user_sub", user.Sub,
		"session_id", sessionID,
		"expires_at", token.Expiry)

	return nil
}

// generateSessionID generates a unique session ID for OAuth sessions
func generateSessionID() string {
	nonce, _ := crypto.GenerateNonce()
	return nonce
}

// getClientIP extracts the client IP address from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (if behind proxy)
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		// Take the first IP in the list
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	// Check X-Real-IP header
	realIP := r.Header.Get("X-Real-IP")
	if realIP != "" {
		return realIP
	}

	// Fallback to RemoteAddr
	return r.RemoteAddr
}
