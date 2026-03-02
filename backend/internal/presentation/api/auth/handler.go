// SPDX-License-Identifier: AGPL-3.0-or-later
package auth

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/btouchard/ackify-ce/backend/internal/infrastructure/i18n"
	"github.com/btouchard/ackify-ce/backend/internal/presentation/api/shared"
	"github.com/btouchard/ackify-ce/backend/pkg/logger"
	"github.com/btouchard/ackify-ce/backend/pkg/providers"
	"github.com/btouchard/ackify-ce/backend/pkg/types"
)

// middleware defines CSRF middleware operations
type middleware interface {
	GenerateCSRFToken() (string, error)
}

// Handler handles authentication API requests using unified AuthProvider
type Handler struct {
	authProvider providers.AuthProvider
	middleware   middleware
	baseURL      string
}

// NewHandler creates a new auth handler with unified AuthProvider
func NewHandler(authProvider providers.AuthProvider, middleware middleware, baseURL string) *Handler {
	return &Handler{
		authProvider: authProvider,
		middleware:   middleware,
		baseURL:      baseURL,
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

	if req.RedirectTo == "" {
		req.RedirectTo = "/"
	}

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
					nextURL = string(nb)
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

	if nextURL == "" {
		nextURL = "/"
	}

	if parsedURL, err := url.Parse(nextURL); err != nil ||
		(parsedURL.Host != "" && parsedURL.Host != r.Host) {
		nextURL = "/"
	}

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

	if req.RedirectTo == "" {
		req.RedirectTo = "/"
	}

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

	redirectTo := result.RedirectTo
	if redirectTo == "" {
		redirectTo = "/"
	}

	http.Redirect(w, r, redirectTo, http.StatusFound)
}

// HandleVerifyReminderAuthLink handles GET /api/v1/auth/reminder-link/verify
func (h *Handler) HandleVerifyReminderAuthLink(w http.ResponseWriter, r *http.Request) {
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
	if redirectTo == "" {
		redirectTo = "/"
	}

	http.Redirect(w, r, redirectTo, http.StatusFound)
}

// === Document Share Handlers ===

// HandleDocumentSharePage handles GET /api/v1/auth/document-share/verify
// Validates the token exists and is valid, then serves an OTP input page
func (h *Handler) HandleDocumentSharePage(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeValidation, "Token is required", nil)
		return
	}

	// Validate token exists and is a valid document_share (read-only, no OTP check)
	ctx := r.Context()
	if err := h.authProvider.ValidateDocumentShareToken(ctx, token); err != nil {
		shared.WriteError(w, http.StatusUnauthorized, shared.ErrCodeUnauthorized, "Invalid or expired link", nil)
		return
	}

	// Serve a self-contained HTML page with OTP input
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline' https://fonts.googleapis.com; font-src https://fonts.gstatic.com; script-src 'unsafe-inline'; img-src data:; connect-src 'self';")
	w.WriteHeader(http.StatusOK)
	page := strings.Replace(otpPageHTML, "{{TOKEN}}", token, 1)
	w.Write([]byte(page))
}

const otpPageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Access Code · Ackify</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:'Inter',system-ui,-apple-system,sans-serif;background:linear-gradient(135deg,#eef2f7 0%%,#e2e8f0 50%%,#dbeafe 100%);min-height:100vh;display:flex;flex-direction:column;align-items:center;justify-content:center;color:#0f172a}

.logo{display:flex;align-items:center;gap:10px;margin-bottom:32px}
.logo svg{width:36px;height:36px}
.logo span{font-size:1.5rem;font-weight:700;color:#0f172a}

.card{background:#fff;border-radius:16px;box-shadow:0 1px 3px rgba(0,0,0,.06),0 8px 30px rgba(0,0,0,.04);padding:40px 36px;width:100%;max-width:420px;text-align:center}

.lock-icon{width:48px;height:48px;margin:0 auto 20px;background:linear-gradient(135deg,#6366f1,#3b82f6);border-radius:12px;display:flex;align-items:center;justify-content:center}
.lock-icon svg{width:24px;height:24px;color:#fff;fill:none;stroke:currentColor;stroke-width:2;stroke-linecap:round;stroke-linejoin:round}

h1{font-size:1.25rem;font-weight:700;color:#0f172a;margin-bottom:6px}
.subtitle{font-size:.875rem;color:#64748b;margin-bottom:28px;line-height:1.5}

.otp-group{display:flex;gap:8px;justify-content:center;margin-bottom:20px}
.otp-box{width:48px;height:56px;text-align:center;font-size:1.5rem;font-weight:600;font-family:'Inter',monospace;border:2px solid #e2e8f0;border-radius:10px;outline:none;transition:all .2s;color:#0f172a;background:#f8fafc}
.otp-box:focus{border-color:#3b82f6;box-shadow:0 0 0 3px rgba(59,130,246,.15);background:#fff}
.otp-box.filled{border-color:#94a3b8;background:#fff}

.btn{width:100%;padding:13px 24px;background:#0f172a;color:#fff;border:none;border-radius:10px;font-size:.935rem;font-weight:600;font-family:'Inter',sans-serif;cursor:pointer;transition:all .2s;display:flex;align-items:center;justify-content:center;gap:8px}
.btn:hover{background:#1e293b;transform:translateY(-1px);box-shadow:0 4px 12px rgba(15,23,42,.15)}
.btn:active{transform:translateY(0)}
.btn:disabled{background:#94a3b8;cursor:not-allowed;transform:none;box-shadow:none}

.error-msg{color:#dc2626;font-size:.8rem;margin-top:14px;display:none;padding:8px 12px;background:#fef2f2;border-radius:8px;border:1px solid #fecaca}

.spinner{display:none;width:18px;height:18px;border:2.5px solid rgba(255,255,255,.3);border-top-color:#fff;border-radius:50%%;animation:spin .5s linear infinite}
@keyframes spin{to{transform:rotate(360deg)}}

.footer{margin-top:24px;font-size:.75rem;color:#94a3b8}
.footer a{color:#64748b;text-decoration:none}
</style>
</head>
<body>

<div class="logo">
<svg viewBox="0 0 36 36" fill="none"><path d="M18 3L4 10v16l14 7 14-7V10L18 3z" fill="url(#g)"/><path d="M18 13l-7 4v7l7 4 7-4v-7l-7-4z" fill="#fff" fill-opacity=".9"/><defs><linearGradient id="g" x1="4" y1="3" x2="32" y2="33"><stop stop-color="#6366f1"/><stop offset="1" stop-color="#06b6d4"/></linearGradient></defs></svg>
<span>Ackify</span>
</div>

<div class="card">
<div class="lock-icon">
<svg viewBox="0 0 24 24"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/><circle cx="12" cy="16" r="1" fill="currentColor"/></svg>
</div>
<h1>Secure Access</h1>
<p class="subtitle">Enter the 6-digit code provided to you to access this document.</p>
<form id="otp-form">
<div class="otp-group">
<input class="otp-box" type="text" maxlength="1" inputmode="numeric" pattern="[0-9]" autofocus>
<input class="otp-box" type="text" maxlength="1" inputmode="numeric" pattern="[0-9]">
<input class="otp-box" type="text" maxlength="1" inputmode="numeric" pattern="[0-9]">
<input class="otp-box" type="text" maxlength="1" inputmode="numeric" pattern="[0-9]">
<input class="otp-box" type="text" maxlength="1" inputmode="numeric" pattern="[0-9]">
<input class="otp-box" type="text" maxlength="1" inputmode="numeric" pattern="[0-9]">
</div>
<button class="btn" type="submit" id="submit-btn">
<span id="btn-text">Verify Access Code</span>
<div class="spinner" id="spinner"></div>
</button>
<div class="error-msg" id="error"></div>
</form>
</div>
<div class="footer">Secured by Ackify · Your authentication is encrypted</div>

<script>
const token = "{{TOKEN}}";
const boxes = document.querySelectorAll(".otp-box");
const form = document.getElementById("otp-form");
const errorEl = document.getElementById("error");
const spinner = document.getElementById("spinner");
const btnText = document.getElementById("btn-text");
const btn = document.getElementById("submit-btn");

boxes.forEach((box, i) => {
  box.addEventListener("input", function(e) {
    this.value = this.value.replace(/\D/g, "");
    if (this.value && i < boxes.length - 1) boxes[i + 1].focus();
    this.classList.toggle("filled", !!this.value);
    errorEl.style.display = "none";
  });
  box.addEventListener("keydown", function(e) {
    if (e.key === "Backspace" && !this.value && i > 0) {
      boxes[i - 1].focus();
      boxes[i - 1].value = "";
      boxes[i - 1].classList.remove("filled");
    }
  });
  box.addEventListener("paste", function(e) {
    e.preventDefault();
    const data = (e.clipboardData || window.clipboardData).getData("text").replace(/\D/g, "").slice(0, 6);
    data.split("").forEach((ch, j) => {
      if (boxes[j]) { boxes[j].value = ch; boxes[j].classList.add("filled"); }
    });
    if (data.length > 0) boxes[Math.min(data.length, 5)].focus();
  });
});

function getOTP() { return Array.from(boxes).map(b => b.value).join(""); }

form.addEventListener("submit", async function(e) {
  e.preventDefault();
  const otp = getOTP();
  if (otp.length !== 6) { boxes[0].focus(); return; }
  btn.disabled = true;
  btnText.textContent = "Verifying...";
  spinner.style.display = "block";
  errorEl.style.display = "none";
  try {
    const resp = await fetch("/api/v1/auth/document-share/verify-otp", {
      method: "POST", credentials: "include",
      headers: {"Content-Type": "application/json"},
      body: JSON.stringify({token: token, otp: otp})
    });
    const data = await resp.json();
    if (resp.ok && data.data && data.data.redirect_to) {
      btnText.textContent = "Redirecting...";
      window.location.href = data.data.redirect_to;
    } else {
      errorEl.textContent = "Invalid code. Please check and try again.";
      errorEl.style.display = "block";
      btn.disabled = false;
      btnText.textContent = "Verify Access Code";
      spinner.style.display = "none";
      boxes.forEach(b => { b.value = ""; b.classList.remove("filled"); });
      boxes[0].focus();
    }
  } catch(err) {
    errorEl.textContent = "Something went wrong. Please try again.";
    errorEl.style.display = "block";
    btn.disabled = false;
    btnText.textContent = "Verify Access Code";
    spinner.style.display = "none";
  }
});
</script>
</body>
</html>`

// HandleVerifyDocumentShareOTP handles POST /api/v1/auth/document-share/verify-otp
func (h *Handler) HandleVerifyDocumentShareOTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
		OTP   string `json:"otp"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeValidation, "Invalid request body", nil)
		return
	}

	if req.Token == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeValidation, "Token is required", nil)
		return
	}

	if req.OTP == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeValidation, "Access code is required", nil)
		return
	}

	ip := extractIP(r.RemoteAddr)
	userAgent := r.UserAgent()
	ctx := r.Context()

	result, err := h.authProvider.VerifyDocumentShareOTP(ctx, req.Token, req.OTP, ip, userAgent)
	if err != nil {
		logger.Logger.Warn("Document share OTP verification failed", "error", err.Error(), "ip", ip)
		// Return a generic error message to avoid leaking internal state
		shared.WriteError(w, http.StatusUnauthorized, shared.ErrCodeUnauthorized, "Invalid or expired link or access code", nil)
		return
	}

	// Create user session from document share result
	user := &types.User{
		Sub:   "docshare:" + result.Email,
		Email: result.Email,
		Name:  result.Email,
	}

	if err := h.authProvider.SetCurrentUser(w, r, user); err != nil {
		logger.Logger.Error("Failed to set user session", "error", err.Error())
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to create session", nil)
		return
	}

	redirectTo := result.RedirectTo
	if redirectTo == "" {
		redirectTo = "/"
	}

	shared.WriteJSON(w, http.StatusOK, map[string]string{
		"redirect_to": redirectTo,
	})
}

func extractIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
