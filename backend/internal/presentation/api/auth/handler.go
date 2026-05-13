// SPDX-License-Identifier: AGPL-3.0-or-later
package auth

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/kolapsis/ackify/backend/internal/infrastructure/i18n"
	"github.com/kolapsis/ackify/backend/internal/presentation/api/shared"
	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/providers"
	"github.com/kolapsis/ackify/backend/pkg/types"
)

// middleware defines CSRF middleware operations
type middleware interface {
	GenerateCSRFToken() (string, error)
}

// Handler handles authentication API requests using unified AuthProvider
type Handler struct {
	authProvider         providers.AuthProvider
	middleware           middleware
	baseURL              string
	allowedRedirectHosts []string
}

// NewHandler creates a new auth handler with unified AuthProvider
func NewHandler(authProvider providers.AuthProvider, middleware middleware, baseURL string, allowedRedirectHosts []string) *Handler {
	return &Handler{
		authProvider:         authProvider,
		middleware:           middleware,
		baseURL:              baseURL,
		allowedRedirectHosts: allowedRedirectHosts,
	}
}

// HandleGetCSRFToken handles GET /api/v1/csrf
func (h *Handler) HandleGetCSRFToken(w http.ResponseWriter, r *http.Request) {
	token, err := h.middleware.GenerateCSRFToken()
	if err != nil {
		shared.WriteInternalError(w)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     shared.CSRFTokenCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: false,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})

	shared.WriteJSON(w, http.StatusOK, map[string]string{
		"token": token,
	})
}

// HandleStartOIDC handles POST /api/v1/auth/start
func (h *Handler) HandleStartOIDC(w http.ResponseWriter, r *http.Request) {
	if !h.authProvider.IsOIDCEnabled() {
		shared.WriteError(w, http.StatusServiceUnavailable, shared.ErrCodeServiceUnavailable, "OIDC not enabled", nil)
		return
	}

	var req struct {
		RedirectTo string `json:"redirectTo"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.RedirectTo = "/"
	}

	req.RedirectTo = shared.SafeRedirectURL(req.RedirectTo, h.allowedRedirectHosts)

	authURL := h.authProvider.StartOIDC(w, r, req.RedirectTo)
	if authURL == "" {
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to generate auth URL", nil)
		return
	}

	shared.WriteJSON(w, http.StatusOK, map[string]string{
		"redirectUrl": authURL,
	})
}

// HandleOIDCCallback handles GET /api/v1/auth/callback
func (h *Handler) HandleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if !h.authProvider.IsOIDCEnabled() {
		shared.WriteError(w, http.StatusServiceUnavailable, shared.ErrCodeServiceUnavailable, "OIDC not enabled", nil)
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	oauthError := r.URL.Query().Get("error")
	errorDescription := r.URL.Query().Get("error_description")

	// Handle OAuth errors (e.g., prompt=none without active session)
	if oauthError != "" {
		logger.Logger.Debug("OIDC error received", "error", oauthError, "description", errorDescription)

		if oauthError == "login_required" || oauthError == "interaction_required" || oauthError == "consent_required" {
			parts := strings.SplitN(state, ":", 2)
			nextURL := "/"
			if len(parts) == 2 {
				if nb, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
					nextURL = shared.SafeRedirectURL(string(nb), h.allowedRedirectHosts)
				}
			}
			http.Redirect(w, r, nextURL, http.StatusFound)
			return
		}

		http.Error(w, "OAuth error: "+oauthError, http.StatusBadRequest)
		return
	}

	if code == "" {
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}

	// Validate state
	parts := strings.SplitN(state, ":", 2)
	token := ""
	if len(parts) > 0 {
		token = parts[0]
	}

	if token == "" || !h.authProvider.VerifyOIDCState(w, r, token) {
		http.Error(w, "Invalid OAuth state", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	user, nextURL, err := h.authProvider.HandleOIDCCallback(ctx, w, r, code, state)
	if err != nil {
		logger.Logger.Error("OIDC callback failed", "error", err.Error())
		http.Error(w, "Authentication failed: "+err.Error(), http.StatusUnauthorized)
		return
	}

	if err := h.authProvider.SetCurrentUser(w, r, user); err != nil {
		logger.Logger.Error("Failed to set user session", "error", err.Error())
		http.Error(w, "Failed to set user session", http.StatusInternalServerError)
		return
	}

	nextURL = shared.SafeRedirectURL(nextURL, h.allowedRedirectHosts)

	http.Redirect(w, r, nextURL, http.StatusFound)
}

// HandleLogout handles GET /api/v1/auth/logout
func (h *Handler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	h.authProvider.Logout(w, r)

	logoutURL := h.authProvider.GetOIDCLogoutURL()
	if logoutURL != "" {
		returnURL := h.baseURL + "/"
		fullLogoutURL := logoutURL + "?post_logout_redirect_uri=" + url.QueryEscape(returnURL)

		shared.WriteJSON(w, http.StatusOK, map[string]string{
			"message":     "Successfully logged out",
			"redirectUrl": fullLogoutURL,
		})
	} else {
		shared.WriteJSON(w, http.StatusOK, map[string]string{
			"message": "Successfully logged out",
		})
	}
}

// HandleAuthCheck handles GET /api/v1/auth/check
func (h *Handler) HandleAuthCheck(w http.ResponseWriter, r *http.Request) {
	user, err := h.authProvider.GetCurrentUser(r)
	if err != nil || user == nil {
		shared.WriteJSON(w, http.StatusOK, map[string]interface{}{
			"authenticated": false,
		})
		return
	}

	shared.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"authenticated": true,
		"user": map[string]interface{}{
			"id":    user.Sub,
			"email": user.Email,
			"name":  user.Name,
		},
	})
}

// === MagicLink Handlers ===

// HandleRequestMagicLink handles POST /api/v1/auth/magic-link/request
func (h *Handler) HandleRequestMagicLink(w http.ResponseWriter, r *http.Request) {
	if !h.authProvider.IsMagicLinkEnabled() {
		shared.WriteError(w, http.StatusServiceUnavailable, shared.ErrCodeServiceUnavailable, "Magic Link not enabled", nil)
		return
	}

	var req struct {
		Email      string `json:"email"`
		RedirectTo string `json:"redirectTo"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeValidation, "Invalid request body", nil)
		return
	}

	if req.Email == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeValidation, "Email is required", nil)
		return
	}

	req.RedirectTo = shared.SafeRedirectURL(req.RedirectTo, h.allowedRedirectHosts)

	ip := extractIP(r.RemoteAddr)
	userAgent := r.UserAgent()
	ctx := r.Context()
	locale := i18n.GetLang(ctx)
	if err := h.authProvider.RequestMagicLink(ctx, req.Email, req.RedirectTo, ip, userAgent, locale); err != nil {
		logger.Logger.Error("Failed to request magic link", "error", err.Error())
		// Don't reveal if email exists or not
		shared.WriteJSON(w, http.StatusOK, map[string]string{
			"message": "If the email exists, a magic link has been sent",
		})
		return
	}

	shared.WriteJSON(w, http.StatusOK, map[string]string{
		"message": "If the email exists, a magic link has been sent",
	})
}

// HandleVerifyMagicLink handles GET /api/v1/auth/magic-link/verify
func (h *Handler) HandleVerifyMagicLink(w http.ResponseWriter, r *http.Request) {
	if !h.authProvider.IsMagicLinkEnabled() {
		shared.WriteError(w, http.StatusServiceUnavailable, shared.ErrCodeServiceUnavailable, "Magic Link not enabled", nil)
		return
	}

	token := r.URL.Query().Get("token")
	if token == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeValidation, "Token is required", nil)
		return
	}

	ip := extractIP(r.RemoteAddr)
	userAgent := r.UserAgent()

	ctx := r.Context()
	result, err := h.authProvider.VerifyMagicLink(ctx, token, ip, userAgent)
	if err != nil {
		logger.Logger.Error("Failed to verify magic link", "error", err.Error())
		http.Redirect(w, r, "/?error=invalid_token", http.StatusFound)
		return
	}

	// Create user from magic link result
	user := &types.User{
		Sub:   "magiclink:" + result.Email,
		Email: result.Email,
		Name:  result.Email,
	}

	if err := h.authProvider.SetCurrentUser(w, r, user); err != nil {
		logger.Logger.Error("Failed to set user session", "error", err.Error())
		http.Redirect(w, r, "/?error=session_error", http.StatusFound)
		return
	}

	redirectTo := shared.SafeRedirectURL(result.RedirectTo, h.allowedRedirectHosts)

	http.Redirect(w, r, redirectTo, http.StatusFound)
}

// HandleVerifyReminderAuthLink handles GET /api/v1/auth/reminder-link/verify
// Reminder auth tokens are issued by admins when sending signature reminders. They are
// independent from the user-facing MagicLink feature: gating only on the underlying token
// service availability lets reminders work even when MagicLink user-auth is disabled.
func (h *Handler) HandleVerifyReminderAuthLink(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeValidation, "Token is required", nil)
		return
	}

	ip := extractIP(r.RemoteAddr)
	userAgent := r.UserAgent()

	ctx := r.Context()
	result, err := h.authProvider.VerifyReminderAuthToken(ctx, token, ip, userAgent)
	if err != nil {
		logger.Logger.Error("Failed to verify reminder auth token", "error", err.Error())
		http.Redirect(w, r, "/?error=invalid_token", http.StatusFound)
		return
	}

	// Create user session from reminder auth result
	user := &types.User{
		Sub:   "reminder:" + result.Email,
		Email: result.Email,
		Name:  result.Email,
	}

	if err := h.authProvider.SetCurrentUser(w, r, user); err != nil {
		logger.Logger.Error("Failed to set user session", "error", err.Error())
		http.Redirect(w, r, "/?error=session_error", http.StatusFound)
		return
	}

	redirectTo := result.RedirectTo
	if redirectTo == "" && result.DocID != nil {
		redirectTo = "/?doc=" + *result.DocID
	}
	redirectTo = shared.SafeRedirectURL(redirectTo, h.allowedRedirectHosts)

	http.Redirect(w, r, redirectTo, http.StatusFound)
}

func extractIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
