// SPDX-License-Identifier: AGPL-3.0-or-later
package shared

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/btouchard/ackify-ce/backend/pkg/logger"
	"github.com/btouchard/ackify-ce/backend/pkg/providers"
	"github.com/btouchard/ackify-ce/backend/pkg/types"
)

// ContextKey represents a context key type
type ContextKey string

const (
	// ContextKeyUser is the context key for the authenticated user
	ContextKeyUser ContextKey = "user"
	// ContextKeyRequestID is the context key for the request ID
	ContextKeyRequestID ContextKey = "request_id"
	// CSRFTokenHeader is the header name for CSRF token
	CSRFTokenHeader = "X-CSRF-Token"
	// CSRFTokenCookie is the cookie name for CSRF token
	CSRFTokenCookie = "csrf_token"
)

// Middleware represents API middleware
type Middleware struct {
	authProvider providers.AuthProvider
	csrfTokens   *sync.Map
	baseURL      string
	authorizer   providers.Authorizer
}

// NewMiddleware creates a new middleware instance
func NewMiddleware(authProvider providers.AuthProvider, baseURL string, authorizer providers.Authorizer) *Middleware {
	return &Middleware{
		authProvider: authProvider,
		csrfTokens:   &sync.Map{},
		baseURL:      baseURL,
		authorizer:   authorizer,
	}
}

// CORS middleware for handling cross-origin requests
func (m *Middleware) CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// In development, allow localhost:5173 (Vite dev server)
		if origin == "http://localhost:5173" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, Authorization, X-CSRF-Token")
			w.Header().Set("Access-Control-Expose-Headers", "X-CSRF-Token")
		}

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RequireAuth middleware ensures user is authenticated
func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := getRequestID(r.Context())

		user, err := m.authProvider.GetCurrentUser(r)
		if err != nil || user == nil {
			logger.Logger.Debug("authentication_required",
				"request_id", requestID,
				"path", r.URL.Path,
				"method", r.Method,
				"error", errToString(err))
			WriteUnauthorized(w, "Authentication required")
			return
		}

		logger.Logger.Debug("authentication_success",
			"request_id", requestID,
			"user_email", user.Email,
			"path", r.URL.Path)

		// Add user to context
		ctx := context.WithValue(r.Context(), ContextKeyUser, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// OptionalAuth middleware adds user to context if authenticated, but doesn't block if not
func (m *Middleware) OptionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := getRequestID(r.Context())

		user, err := m.authProvider.GetCurrentUser(r)
		if err == nil && user != nil {
			// User is authenticated, add to context
			logger.Logger.Debug("optional_auth_success",
				"request_id", requestID,
				"user_email", user.Email,
				"path", r.URL.Path)
			ctx := context.WithValue(r.Context(), ContextKeyUser, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		} else {
			// User not authenticated, continue without user in context
			logger.Logger.Debug("optional_auth_none",
				"request_id", requestID,
				"path", r.URL.Path)
			next.ServeHTTP(w, r)
		}
	})
}

// RequireAdmin middleware ensures user is an admin
func (m *Middleware) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := getRequestID(r.Context())

		user, err := m.authProvider.GetCurrentUser(r)
		if err != nil || user == nil {
			logger.Logger.Debug("admin_authentication_required",
				"request_id", requestID,
				"path", r.URL.Path,
				"error", errToString(err))
			WriteUnauthorized(w, "Authentication required")
			return
		}

		// Check if user is admin via authorizer
		if !m.authorizer.IsAdmin(r.Context(), user.Email) {
			logger.Logger.Warn("admin_access_denied",
				"request_id", requestID,
				"user_email", user.Email,
				"path", r.URL.Path)
			WriteForbidden(w, "Admin access required")
			return
		}

		logger.Logger.Debug("admin_access_granted",
			"request_id", requestID,
			"user_email", user.Email,
			"path", r.URL.Path)

		// Add user to context
		ctx := context.WithValue(r.Context(), ContextKeyUser, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GenerateCSRFToken generates a new CSRF token
func (m *Middleware) GenerateCSRFToken() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}

	token := base64.URLEncoding.EncodeToString(b)

	// Store token with expiration
	m.csrfTokens.Store(token, time.Now().Add(24*time.Hour))

	// Clean up expired tokens periodically
	go m.cleanExpiredTokens()

	return token, nil
}

// ValidateCSRFToken validates a CSRF token
func (m *Middleware) ValidateCSRFToken(token string) bool {
	if token == "" {
		return false
	}

	if val, ok := m.csrfTokens.Load(token); ok {
		expiry := val.(time.Time)
		if time.Now().Before(expiry) {
			return true
		}
		// Token expired, remove it
		m.csrfTokens.Delete(token)
	}

	return false
}

// CSRFProtect middleware for CSRF protection
func (m *Middleware) CSRFProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip CSRF check for safe methods
		if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" {
			next.ServeHTTP(w, r)
			return
		}

		// Get token from header
		token := r.Header.Get(CSRFTokenHeader)
		if token == "" {
			// Try cookie as fallback
			if cookie, err := r.Cookie(CSRFTokenCookie); err == nil {
				token = cookie.Value
			}
		}

		if !m.ValidateCSRFToken(token) {
			WriteError(w, http.StatusForbidden, ErrCodeCSRFInvalid, "Invalid or missing CSRF token", nil)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// cleanExpiredTokens removes expired CSRF tokens
func (m *Middleware) cleanExpiredTokens() {
	m.csrfTokens.Range(func(key, value interface{}) bool {
		expiry := value.(time.Time)
		if time.Now().After(expiry) {
			m.csrfTokens.Delete(key)
		}
		return true
	})
}

// GetUserFromContext retrieves the user from the request context
func GetUserFromContext(ctx context.Context) (*types.User, bool) {
	user, ok := ctx.Value(ContextKeyUser).(*types.User)
	return user, ok
}

// SecurityHeaders middleware adds security headers
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if this is a content endpoint that needs to be embeddable
		path := r.URL.Path
		isContentEndpoint := strings.Contains(path, "/content") || strings.Contains(path, "/proxy") || strings.Contains(path, "/document-share/verify")

		// Security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

		if isContentEndpoint {
			// Allow same-origin framing for content endpoints (PDF viewer, etc.)
			w.Header().Set("X-Frame-Options", "SAMEORIGIN")
			// No restrictive CSP for content - let handlers define their own if needed
		} else {
			// Strict framing policy for other endpoints
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none';")
		}

		next.ServeHTTP(w, r)
	})
}

// RateLimit represents a simple rate limiter
type RateLimit struct {
	attempts *sync.Map
	limit    int
	window   time.Duration
}

// NewRateLimit creates a new rate limiter
func NewRateLimit(limit int, window time.Duration) *RateLimit {
	return &RateLimit{
		attempts: &sync.Map{},
		limit:    limit,
		window:   window,
	}
}

// RateLimitMiddleware creates a rate limiting middleware
func (rl *RateLimit) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Use IP address as identifier
		ip := r.RemoteAddr
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			ip = strings.Split(forwarded, ",")[0]
		}

		now := time.Now()

		// Check current attempts
		if val, ok := rl.attempts.Load(ip); ok {
			attempts := val.([]time.Time)

			// Filter out old attempts
			var valid []time.Time
			for _, t := range attempts {
				if now.Sub(t) < rl.window {
					valid = append(valid, t)
				}
			}

			if len(valid) >= rl.limit {
				WriteError(w, http.StatusTooManyRequests, ErrCodeRateLimited, "Rate limit exceeded", map[string]interface{}{
					"retryAfter": rl.window.Seconds(),
				})
				return
			}

			valid = append(valid, now)
			rl.attempts.Store(ip, valid)
		} else {
			rl.attempts.Store(ip, []time.Time{now})
		}

		next.ServeHTTP(w, r)
	})
}
