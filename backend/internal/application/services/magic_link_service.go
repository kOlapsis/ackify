// SPDX-License-Identifier: AGPL-3.0-or-later
package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/kolapsis/ackify/backend/internal/infrastructure/email"
	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/models"
)

// MagicLinkRepository définit les opérations sur les tokens Magic Link
type MagicLinkRepository interface {
	CreateToken(ctx context.Context, token *models.MagicLinkToken) error
	GetByToken(ctx context.Context, token string) (*models.MagicLinkToken, error)
	MarkAsUsed(ctx context.Context, token string, ip string, userAgent string) error
	DeleteExpired(ctx context.Context) (int64, error)

	LogAttempt(ctx context.Context, attempt *models.MagicLinkAuthAttempt) error
	CountRecentAttempts(ctx context.Context, email string, since time.Time) (int, error)
	CountRecentAttemptsByIP(ctx context.Context, ip string, since time.Time) (int, error)
}

// emailSender defines email sending operations
type emailSender interface {
	Send(ctx context.Context, msg email.Message) error
}

// i18nTranslator defines translation operations
type i18nTranslator interface {
	T(locale, key string) string
}

// MagicLinkService gère l'authentification par Magic Link
type MagicLinkService struct {
	repo              MagicLinkRepository
	emailSender       emailSender
	i18n              i18nTranslator
	baseURL           string
	appName           string
	allowedDomains    []string // Domaines email autorisés (vide = tous)
	tokenValidity     time.Duration
	rateLimitPerEmail int           // Nombre max de requêtes par email par fenêtre (défaut: 3)
	rateLimitPerIP    int           // Nombre max de requêtes par IP par fenêtre (défaut: 10)
	rateLimitWindow   time.Duration // Fenêtre de rate limit (défaut: 1h)
}

// MagicLinkServiceConfig pour le service Magic Link
type MagicLinkServiceConfig struct {
	Repository        MagicLinkRepository
	EmailSender       emailSender
	I18n              i18nTranslator
	BaseURL           string
	AppName           string
	AllowedDomains    []string
	TokenValidity     time.Duration // Défaut: 15 minutes
	RateLimitPerEmail int           // Défaut: 3
	RateLimitPerIP    int           // Défaut: 10
	RateLimitWindow   time.Duration // Défaut: 1 heure
}

func NewMagicLinkService(cfg MagicLinkServiceConfig) *MagicLinkService {
	if cfg.TokenValidity == 0 {
		cfg.TokenValidity = 15 * time.Minute
	}

	if cfg.AppName == "" {
		cfg.AppName = "Ackify"
	}

	if cfg.RateLimitPerEmail == 0 {
		cfg.RateLimitPerEmail = 3
	}

	if cfg.RateLimitPerIP == 0 {
		cfg.RateLimitPerIP = 10
	}

	if cfg.RateLimitWindow == 0 {
		cfg.RateLimitWindow = 1 * time.Hour
	}

	return &MagicLinkService{
		repo:              cfg.Repository,
		emailSender:       cfg.EmailSender,
		i18n:              cfg.I18n,
		baseURL:           cfg.BaseURL,
		appName:           cfg.AppName,
		allowedDomains:    cfg.AllowedDomains,
		tokenValidity:     cfg.TokenValidity,
		rateLimitPerEmail: cfg.RateLimitPerEmail,
		rateLimitPerIP:    cfg.RateLimitPerIP,
		rateLimitWindow:   cfg.RateLimitWindow,
	}
}

// RequestMagicLink génère et envoie un Magic Link par email
func (s *MagicLinkService) RequestMagicLink(
	ctx context.Context,
	emailAddr string,
	redirectTo string,
	ip string,
	userAgent string,
	locale string,
) error {
	// Normaliser l'email
	emailAddr = strings.ToLower(strings.TrimSpace(emailAddr))

	// Valider le format email
	if _, err := mail.ParseAddress(emailAddr); err != nil {
		s.logAttempt(ctx, emailAddr, false, "invalid_email_format", ip, userAgent)
		return fmt.Errorf("invalid email format")
	}

	// Vérifier le domaine autorisé si configuré
	if len(s.allowedDomains) > 0 {
		allowed := false
		for _, domain := range s.allowedDomains {
			if strings.HasSuffix(emailAddr, "@"+domain) {
				allowed = true
				break
			}
		}
		if !allowed {
			s.logAttempt(ctx, emailAddr, false, "domain_not_allowed", ip, userAgent)
			return fmt.Errorf("email domain not allowed")
		}
	}

	// Rate limiting par email
	since := time.Now().Add(-1 * s.rateLimitWindow)
	count, err := s.repo.CountRecentAttempts(ctx, emailAddr, since)
	if err != nil {
		logger.Logger.Error("Failed to check rate limit for email", "email", emailAddr, "error", err)
		return fmt.Errorf("rate limit check failed")
	}
	if count >= s.rateLimitPerEmail {
		s.logAttempt(ctx, emailAddr, false, "rate_limit_exceeded_email", ip, userAgent)
		// Ne pas révéler le rate limiting pour éviter l'énumération
		logger.Logger.Warn("Magic Link rate limit exceeded", "email", emailAddr, "count", count)
		// On retourne success pour ne pas révéler qu'on a bloqué
		return nil
	}

	// Rate limiting par IP
	countIP, err := s.repo.CountRecentAttemptsByIP(ctx, ip, since)
	if err != nil {
		logger.Logger.Error("Failed to check rate limit for IP", "ip", ip, "error", err)
		return fmt.Errorf("rate limit check failed")
	}
	if countIP >= s.rateLimitPerIP {
		s.logAttempt(ctx, emailAddr, false, "rate_limit_exceeded_ip", ip, userAgent)
		logger.Logger.Warn("Magic Link IP rate limit exceeded", "ip", ip, "count", countIP)
		return nil
	}

	// Générer un token cryptographiquement sécurisé
	token, err := s.generateSecureToken()
	if err != nil {
		s.logAttempt(ctx, emailAddr, false, "token_generation_failed", ip, userAgent)
		return fmt.Errorf("failed to generate token: %w", err)
	}

	// Créer le token en DB
	magicToken := &models.MagicLinkToken{
		Token:              token,
		Email:              emailAddr,
		ExpiresAt:          time.Now().Add(s.tokenValidity),
		RedirectTo:         redirectTo,
		CreatedByIP:        ip,
		CreatedByUserAgent: userAgent,
	}

	if err := s.repo.CreateToken(ctx, magicToken); err != nil {
		s.logAttempt(ctx, emailAddr, false, "database_error", ip, userAgent)
		return fmt.Errorf("failed to create token: %w", err)
	}

	// Construire le lien magique avec URL encoding du redirect
	redirectEncoded := url.QueryEscape(redirectTo)
	magicLink := fmt.Sprintf("%s/api/v1/auth/magic-link/verify?token=%s&redirect=%s", s.baseURL, token, redirectEncoded)

	// Utiliser la locale fournie, défaut "en" si vide
	if locale == "" {
		locale = "en"
	}

	// Traduire le sujet de l'email
	subject := "Your login link" // Fallback par défaut
	if s.i18n != nil {
		subject = s.i18n.T(locale, "email.magic_link.subject")
	}

	// Envoyer l'email
	msg := email.Message{
		To:       []string{emailAddr},
		Subject:  subject,
		Template: "magic_link",
		Locale:   locale,
		Data: map[string]interface{}{
			"AppName":   s.appName,
			"Email":     emailAddr,
			"MagicLink": magicLink,
			"ExpiresIn": int(s.tokenValidity.Minutes()),
			"BaseURL":   s.baseURL,
		},
	}

	if err := s.emailSender.Send(ctx, msg); err != nil {
		s.logAttempt(ctx, emailAddr, false, "email_send_failed", ip, userAgent)
		return fmt.Errorf("failed to send email: %w", err)
	}

	// Log succès
	s.logAttempt(ctx, emailAddr, true, "", ip, userAgent)

	logger.Logger.Info("Magic Link sent successfully",
		"email", emailAddr,
		"expires_in", s.tokenValidity,
		"ip", ip)

	return nil
}

// CreateReminderAuthToken génère un token d'authentification pour un email de reminder
// Ce token a une durée de validité de 24 heures (vs 15 min pour magic link classique)
// Il ne valide pas les domaines autorisés et n'envoie pas d'email (géré par ReminderService)
func (s *MagicLinkService) CreateReminderAuthToken(
	ctx context.Context,
	emailAddr string,
	docID string,
) (string, error) {
	// Normaliser l'email
	emailAddr = strings.ToLower(strings.TrimSpace(emailAddr))

	// Valider le format email
	if _, err := mail.ParseAddress(emailAddr); err != nil {
		return "", fmt.Errorf("invalid email format")
	}

	// Générer un token cryptographiquement sécurisé
	token, err := s.generateSecureToken()
	if err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}

	// Créer le token en DB avec purpose='reminder_auth' et durée 24h
	magicToken := &models.MagicLinkToken{
		Token:              token,
		Email:              emailAddr,
		ExpiresAt:          time.Now().Add(24 * time.Hour), // 24 heures pour reminder
		RedirectTo:         "/?doc=" + docID,               // Redirection vers la page de signature
		CreatedByIP:        "127.0.0.1",                    // Localhost = système (reminder)
		CreatedByUserAgent: "reminder-service",
		Purpose:            "reminder_auth",
		DocID:              &docID,
	}

	if err := s.repo.CreateToken(ctx, magicToken); err != nil {
		return "", fmt.Errorf("failed to create reminder auth token: %w", err)
	}

	logger.Logger.Info("Reminder auth token created",
		"email", emailAddr,
		"doc_id", docID,
		"expires_in", "24h")

	return token, nil
}

// VerifyMagicLink vérifie et consomme un token Magic Link
func (s *MagicLinkService) VerifyMagicLink(
	ctx context.Context,
	token string,
	ip string,
	userAgent string,
) (*models.MagicLinkToken, error) {
	// Récupérer le token
	magicToken, err := s.repo.GetByToken(ctx, token)
	if err != nil {
		logger.Logger.Warn("Magic Link token not found", "token_prefix", token[:min(8, len(token))])
		return nil, fmt.Errorf("invalid token")
	}

	// Vérifier la validité
	if !magicToken.IsValid() {
		if magicToken.UsedAt != nil {
			logger.Logger.Warn("Magic Link token already used",
				"email", magicToken.Email,
				"used_at", magicToken.UsedAt)
			return nil, fmt.Errorf("token already used")
		}
		logger.Logger.Warn("Magic Link token expired",
			"email", magicToken.Email,
			"expires_at", magicToken.ExpiresAt)
		return nil, fmt.Errorf("token expired")
	}

	// Marquer comme utilisé
	if err := s.repo.MarkAsUsed(ctx, token, ip, userAgent); err != nil {
		logger.Logger.Error("Failed to mark token as used", "error", err)
		return nil, fmt.Errorf("failed to mark token as used: %w", err)
	}

	logger.Logger.Info("Magic Link verified successfully",
		"email", magicToken.Email,
		"ip", ip)

	return magicToken, nil
}

// VerifyReminderAuthToken vérifie et consomme un token de reminder auth
func (s *MagicLinkService) VerifyReminderAuthToken(
	ctx context.Context,
	token string,
	ip string,
	userAgent string,
) (*models.MagicLinkToken, error) {
	// Récupérer le token
	magicToken, err := s.repo.GetByToken(ctx, token)
	if err != nil {
		logger.Logger.Warn("Reminder auth token not found", "token_prefix", token[:min(8, len(token))])
		return nil, fmt.Errorf("invalid token")
	}

	// Vérifier que c'est bien un token de type reminder_auth
	if magicToken.Purpose != "reminder_auth" {
		logger.Logger.Warn("Token is not a reminder_auth token",
			"purpose", magicToken.Purpose,
			"email", magicToken.Email)
		return nil, fmt.Errorf("invalid token type")
	}

	// Vérifier la validité
	if !magicToken.IsValid() {
		if magicToken.UsedAt != nil {
			logger.Logger.Warn("Reminder auth token already used",
				"email", magicToken.Email,
				"doc_id", magicToken.DocID,
				"used_at", magicToken.UsedAt)
			return nil, fmt.Errorf("token already used")
		}
		logger.Logger.Warn("Reminder auth token expired",
			"email", magicToken.Email,
			"doc_id", magicToken.DocID,
			"expires_at", magicToken.ExpiresAt)
		return nil, fmt.Errorf("token expired")
	}

	// Marquer comme utilisé
	if err := s.repo.MarkAsUsed(ctx, token, ip, userAgent); err != nil {
		logger.Logger.Error("Failed to mark reminder auth token as used", "error", err)
		return nil, fmt.Errorf("failed to mark token as used: %w", err)
	}

	logger.Logger.Info("Reminder auth token verified successfully",
		"email", magicToken.Email,
		"doc_id", magicToken.DocID,
		"ip", ip)

	return magicToken, nil
}

// generateSecureToken génère un token cryptographiquement sécurisé
func (s *MagicLinkService) generateSecureToken() (string, error) {
	bytes := make([]byte, 32) // 256 bits
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	// Base64 URL-safe encoding (sans padding)
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// logAttempt enregistre une tentative d'authentification
func (s *MagicLinkService) logAttempt(
	ctx context.Context,
	email string,
	success bool,
	failureReason string,
	ip string,
	userAgent string,
) {
	attempt := &models.MagicLinkAuthAttempt{
		Email:         email,
		Success:       success,
		FailureReason: failureReason,
		IPAddress:     ip,
		UserAgent:     userAgent,
	}

	if err := s.repo.LogAttempt(ctx, attempt); err != nil {
		logger.Logger.Error("Failed to log Magic Link attempt", "error", err)
	}
}

// CleanupExpiredTokens supprime les tokens expirés (à appeler périodiquement)
func (s *MagicLinkService) CleanupExpiredTokens(ctx context.Context) (int64, error) {
	return s.repo.DeleteExpired(ctx)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
