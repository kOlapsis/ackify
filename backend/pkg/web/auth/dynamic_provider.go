// SPDX-License-Identifier: AGPL-3.0-or-later
package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/securecookie"
	"golang.org/x/oauth2"

	infraAuth "github.com/kolapsis/ackify/backend/internal/infrastructure/auth"
	"github.com/kolapsis/ackify/backend/pkg/crypto"
	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/models"
	"github.com/kolapsis/ackify/backend/pkg/providers"
	"github.com/kolapsis/ackify/backend/pkg/types"
)

type configProvider interface {
	GetConfig() *models.MutableConfig
}

type magicLinkService interface {
	RequestMagicLink(ctx context.Context, email, redirectTo, ip, userAgent, locale string) error
	VerifyMagicLink(ctx context.Context, token, ip, userAgent string) (*models.MagicLinkToken, error)
	VerifyReminderAuthToken(ctx context.Context, token, ip, userAgent string) (*models.MagicLinkToken, error)
	CreateReminderAuthToken(ctx context.Context, email, docID string) (string, error)
}

// ProviderConfig holds a configuration for creating a Provider.
type ProviderConfig struct {
	ConfigProvider   configProvider
	SessionService   *infraAuth.SessionService
	MagicLinkService magicLinkService
	BaseURL          string
}

// Provider implements providers.AuthProvider with dynamic config.
// It reads OIDC/MagicLink configuration from configProvider on each call,
// supporting hot-reload of authentication settings.
type Provider struct {
	configProvider   configProvider
	sessionService   *infraAuth.SessionService
	magicLinkService magicLinkService
	baseURL          string

	// Cache for oauth2.Config to avoid recreating on every request
	// Invalidated when config changes
	mu             sync.RWMutex
	cachedOAuthCfg *oauth2.Config
	cachedOIDCCfg  models.OIDCConfig
}

// NewAuthProvider creates a new dynamic auth provider.
func NewAuthProvider(cfg ProviderConfig) *Provider {
	return &Provider{
		configProvider:   cfg.ConfigProvider,
		sessionService:   cfg.SessionService,
		magicLinkService: cfg.MagicLinkService,
		baseURL:          cfg.BaseURL,
	}
}

// === Session Management ===

func (p *Provider) GetCurrentUser(r *http.Request) (*types.User, error) {
	return p.sessionService.GetUser(r)
}

func (p *Provider) SetCurrentUser(w http.ResponseWriter, r *http.Request, user *types.User) error {
	return p.sessionService.SetUser(w, r, user)
}

func (p *Provider) Logout(w http.ResponseWriter, r *http.Request) {
	p.sessionService.Logout(w, r)
}

func (p *Provider) IsConfigured() bool {
	return p.IsOIDCEnabled() || p.IsMagicLinkEnabled()
}

// === OIDC Authentication ===

func (p *Provider) IsOIDCEnabled() bool {
	cfg := p.configProvider.GetConfig()
	return cfg.OIDC.Enabled && cfg.OIDC.ClientID != "" && cfg.OIDC.ClientSecret != ""
}

func (p *Provider) StartOIDC(w http.ResponseWriter, r *http.Request, nextURL string) string {
	if !p.IsOIDCEnabled() {
		logger.Logger.Error("StartOIDC called but OIDC is not enabled")
		return ""
	}

	oauthConfig := p.getOAuthConfig()
	if oauthConfig == nil {
		return ""
	}

	// Generate PKCE code verifier and challenge
	codeVerifier, err := crypto.GenerateCodeVerifier()
	if err != nil {
		logger.Logger.Error("Failed to generate PKCE code verifier", "error", err.Error())
		return p.startOIDCWithoutPKCE(w, r, nextURL, oauthConfig)
	}

	codeChallenge := crypto.GenerateCodeChallenge(codeVerifier)

	// Generate state token
	randPart := securecookie.GenerateRandomKey(20)
	token := base64.RawURLEncoding.EncodeToString(randPart)
	state := token + ":" + base64.RawURLEncoding.EncodeToString([]byte(nextURL))

	promptParam := "select_account"
	isSilent := r.URL.Query().Get("silent") == "true"
	if isSilent {
		promptParam = "none"
	}

	logger.Logger.Info("Starting OIDC flow with PKCE",
		"next_url", nextURL,
		"silent", isSilent)

	session, err := p.sessionService.GetSession(r)
	if err != nil {
		session, _ = p.sessionService.GetNewSession(r)
	}

	session.Values["oauth_state"] = token
	session.Values["code_verifier"] = codeVerifier
	_ = session.Save(r, w)

	return oauthConfig.AuthCodeURL(state,
		oauth2.SetAuthURLParam("prompt", promptParam),
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"))
}

func (p *Provider) startOIDCWithoutPKCE(w http.ResponseWriter, r *http.Request, nextURL string, oauthConfig *oauth2.Config) string {
	randPart := securecookie.GenerateRandomKey(20)
	token := base64.RawURLEncoding.EncodeToString(randPart)
	state := token + ":" + base64.RawURLEncoding.EncodeToString([]byte(nextURL))

	promptParam := "select_account"
	if r.URL.Query().Get("silent") == "true" {
		promptParam = "none"
	}

	session, err := p.sessionService.GetSession(r)
	if err != nil {
		session, _ = p.sessionService.GetNewSession(r)
	}

	session.Values["oauth_state"] = token
	_ = session.Save(r, w)

	return oauthConfig.AuthCodeURL(state, oauth2.SetAuthURLParam("prompt", promptParam))
}

func (p *Provider) VerifyOIDCState(w http.ResponseWriter, r *http.Request, stateToken string) bool {
	session, _ := p.sessionService.GetSession(r)
	stored, _ := session.Values["oauth_state"].(string)

	if stored == "" || stateToken == "" {
		return false
	}

	if subtleConstantTimeCompare(stored, stateToken) {
		delete(session.Values, "oauth_state")
		_ = session.Save(r, w)
		return true
	}

	return false
}

func (p *Provider) HandleOIDCCallback(ctx context.Context, w http.ResponseWriter, r *http.Request, code, state string) (*types.User, string, error) {
	if !p.IsOIDCEnabled() {
		return nil, "/", fmt.Errorf("OIDC is not enabled")
	}

	oauthConfig := p.getOAuthConfig()
	if oauthConfig == nil {
		return nil, "/", fmt.Errorf("failed to get OAuth config")
	}

	parts := strings.SplitN(state, ":", 2)
	nextURL := "/"
	if len(parts) == 2 {
		if nb, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
			nextURL = string(nb)
		}
	}

	// Retrieve code_verifier from session for PKCE
	session, _ := p.sessionService.GetSession(r)
	codeVerifier, hasPKCE := session.Values["code_verifier"].(string)

	if hasPKCE {
		delete(session.Values, "code_verifier")
		_ = session.Save(r, w)
	}

	// Exchange authorization code for token
	var token *oauth2.Token
	var err error

	if hasPKCE && codeVerifier != "" {
		token, err = oauthConfig.Exchange(ctx, code,
			oauth2.SetAuthURLParam("code_verifier", codeVerifier))
	} else {
		token, err = oauthConfig.Exchange(ctx, code)
	}

	if err != nil {
		return nil, nextURL, fmt.Errorf("oauth exchange failed: %w", err)
	}

	// Fetch user info
	cfg := p.configProvider.GetConfig()
	client := oauthConfig.Client(ctx, token)
	resp, err := client.Get(cfg.OIDC.UserInfoURL)
	if err != nil || resp.StatusCode != 200 {
		return nil, nextURL, fmt.Errorf("userinfo request failed: %w", err)
	}
	defer resp.Body.Close()

	user, err := p.parseUserInfo(resp)
	if err != nil {
		return nil, nextURL, fmt.Errorf("failed to parse user info: %w", err)
	}

	if !p.IsAllowedDomain(user.Email) {
		return nil, nextURL, models.ErrDomainNotAllowed
	}

	// Store refresh token if available
	if token.RefreshToken != "" && p.sessionService != nil {
		if err := p.sessionService.StoreRefreshToken(ctx, w, r, token, user); err != nil {
			logger.Logger.Error("Failed to store refresh token (non-fatal)", "error", err.Error())
		}
	}

	return user, nextURL, nil
}

func (p *Provider) GetOIDCLogoutURL() string {
	cfg := p.configProvider.GetConfig()
	if cfg.OIDC.LogoutURL == "" {
		return ""
	}
	return cfg.OIDC.LogoutURL + "?continue=" + p.baseURL
}

func (p *Provider) IsAllowedDomain(email string) bool {
	cfg := p.configProvider.GetConfig()
	if cfg.OIDC.AllowedDomain == "" {
		return true
	}

	domain := strings.ToLower(cfg.OIDC.AllowedDomain)
	if !strings.HasPrefix(domain, "@") {
		domain = "@" + domain
	}

	return strings.HasSuffix(strings.ToLower(email), domain)
}

// === MagicLink Authentication ===

func (p *Provider) IsMagicLinkEnabled() bool {
	cfg := p.configProvider.GetConfig()
	return cfg.MagicLink.Enabled && cfg.SMTP.Host != ""
}

func (p *Provider) RequestMagicLink(ctx context.Context, email, redirectTo, ip, userAgent, locale string) error {
	if !p.IsMagicLinkEnabled() {
		return fmt.Errorf("MagicLink is not enabled")
	}
	if p.magicLinkService == nil {
		return fmt.Errorf("MagicLink service not configured")
	}
	return p.magicLinkService.RequestMagicLink(ctx, email, redirectTo, ip, userAgent, locale)
}

func (p *Provider) VerifyMagicLink(ctx context.Context, token, ip, userAgent string) (*providers.MagicLinkResult, error) {
	if p.magicLinkService == nil {
		return nil, fmt.Errorf("MagicLink service not configured")
	}

	result, err := p.magicLinkService.VerifyMagicLink(ctx, token, ip, userAgent)
	if err != nil {
		return nil, err
	}

	return &providers.MagicLinkResult{
		Email:      result.Email,
		RedirectTo: result.RedirectTo,
		DocID:      result.DocID,
	}, nil
}

func (p *Provider) VerifyReminderAuthToken(ctx context.Context, token, ip, userAgent string) (*providers.MagicLinkResult, error) {
	if p.magicLinkService == nil {
		return nil, fmt.Errorf("MagicLink service not configured")
	}

	result, err := p.magicLinkService.VerifyReminderAuthToken(ctx, token, ip, userAgent)
	if err != nil {
		return nil, err
	}

	return &providers.MagicLinkResult{
		Email:      result.Email,
		RedirectTo: result.RedirectTo,
		DocID:      result.DocID,
	}, nil
}

func (p *Provider) CreateReminderAuthToken(ctx context.Context, email, docID string) (string, error) {
	if p.magicLinkService == nil {
		return "", fmt.Errorf("MagicLink service not configured")
	}
	return p.magicLinkService.CreateReminderAuthToken(ctx, email, docID)
}

// === Internal helpers ===

func (p *Provider) getOAuthConfig() *oauth2.Config {
	cfg := p.configProvider.GetConfig()

	p.mu.RLock()
	if p.cachedOAuthCfg != nil && p.configMatches(cfg.OIDC) {
		defer p.mu.RUnlock()
		return p.cachedOAuthCfg
	}
	p.mu.RUnlock()

	// Config changed, rebuild
	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock
	if p.cachedOAuthCfg != nil && p.configMatches(cfg.OIDC) {
		return p.cachedOAuthCfg
	}

	p.cachedOAuthCfg = &oauth2.Config{
		ClientID:     cfg.OIDC.ClientID,
		ClientSecret: cfg.OIDC.ClientSecret,
		RedirectURL:  p.baseURL + "/api/v1/auth/callback",
		Scopes:       cfg.OIDC.Scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  cfg.OIDC.AuthURL,
			TokenURL: cfg.OIDC.TokenURL,
		},
	}
	p.cachedOIDCCfg = cfg.OIDC

	logger.Logger.Info("OAuth config rebuilt",
		"client_id_set", cfg.OIDC.ClientID != "",
		"auth_url", cfg.OIDC.AuthURL)

	return p.cachedOAuthCfg
}

func (p *Provider) configMatches(cfg models.OIDCConfig) bool {
	return p.cachedOIDCCfg.ClientID == cfg.ClientID &&
		p.cachedOIDCCfg.ClientSecret == cfg.ClientSecret &&
		p.cachedOIDCCfg.AuthURL == cfg.AuthURL &&
		p.cachedOIDCCfg.TokenURL == cfg.TokenURL
}

func (p *Provider) parseUserInfo(resp *http.Response) (*types.User, error) {
	var rawUser map[string]interface{}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if err := json.Unmarshal(body, &rawUser); err != nil {
		return nil, fmt.Errorf("failed to decode user info: %w", err)
	}

	user := &types.User{}

	// Extract user ID
	if sub, ok := rawUser["sub"].(string); ok {
		user.Sub = sub
	} else if id, ok := rawUser["id"]; ok {
		user.Sub = fmt.Sprintf("%v", id)
	} else {
		return nil, fmt.Errorf("missing user ID in response")
	}

	// Extract email
	if email, ok := rawUser["email"].(string); ok && email != "" {
		user.Email = email
	} else if mail, ok := rawUser["mail"].(string); ok && mail != "" {
		user.Email = mail
	} else if upn, ok := rawUser["userPrincipalName"].(string); ok && upn != "" {
		user.Email = upn
	} else {
		return nil, fmt.Errorf("missing email in user info response")
	}

	// Extract name
	if name, ok := rawUser["name"].(string); ok && name != "" {
		user.Name = name
	} else if displayName, ok := rawUser["displayName"].(string); ok && displayName != "" {
		user.Name = displayName
	} else if preferredName, ok := rawUser["preferred_username"].(string); ok && preferredName != "" {
		user.Name = preferredName
	}

	// Extract avatar URL (picture for Google/OIDC, avatar_url for GitHub/GitLab)
	if picture, ok := rawUser["picture"].(string); ok && picture != "" {
		user.Picture = picture
	} else if avatarURL, ok := rawUser["avatar_url"].(string); ok && avatarURL != "" {
		user.Picture = avatarURL
	} else if photo, ok := rawUser["photo"].(string); ok && photo != "" {
		user.Picture = photo
	}

	return user, nil
}

func subtleConstantTimeCompare(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// Compile-time interface check
var _ providers.AuthProvider = (*Provider)(nil)
