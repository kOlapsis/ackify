// SPDX-License-Identifier: AGPL-3.0-or-later
package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/btouchard/ackify-ce/backend/internal/infrastructure/email"
	"github.com/btouchard/ackify-ce/backend/pkg/logger"
	"github.com/btouchard/ackify-ce/backend/pkg/models"
)

// MagicLinkRepository définit les opérations sur les tokens Magic Link
type MagicLinkRepository interface {
	CreateToken(ctx context.Context, token *models.MagicLinkToken) error
	GetByToken(ctx context.Context, token string) (*models.MagicLinkToken, error)
	MarkAsUsed(ctx context.Context, token string, ip string, userAgent string) error
	DeleteExpired(ctx context.Context) (int64, error)
	IncrementOTPAttempts(ctx context.Context, token string) error
	RevokeToken(ctx context.Context, tokenID int64) error
	ListByDocAndPurpose(ctx context.Context, docID string, purpose string) ([]*models.MagicLinkToken, error)

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

// CreateDocumentShareLink creates a magic link protected by OTP for sharing a document.
// Returns the link URL and the cleartext OTP (shown once to the admin).
func (s *MagicLinkService) CreateDocumentShareLink(
	ctx context.Context,
	emailAddr string,
	docID string,
	validityDays int,
	sharedBy string,
	locale string,
) (linkURL string, otp string, err error) {
	// Normaliser l'email
	emailAddr = strings.ToLower(strings.TrimSpace(emailAddr))

	// Valider le format email
	if _, err := mail.ParseAddress(emailAddr); err != nil {
		return "", "", fmt.Errorf("invalid email format")
	}

	if docID == "" {
		return "", "", fmt.Errorf("document ID is required")
	}

	// Validate and cap validity
	if validityDays <= 0 {
		validityDays = 7 // Default: 7 days
	}
	if validityDays > 90 {
		validityDays = 90 // Max: 90 days
	}

	// Generate secure token for the URL
	token, err := s.generateSecureToken()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate token: %w", err)
	}

	// Generate 6-digit OTP
	otp, err = s.generateOTP()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate OTP: %w", err)
	}

	// Hash OTP with bcrypt
	otpHashBytes, err := bcrypt.GenerateFromPassword([]byte(otp), bcrypt.DefaultCost)
	if err != nil {
		return "", "", fmt.Errorf("failed to hash OTP: %w", err)
	}
	otpHash := string(otpHashBytes)

	// Create token in DB
	magicToken := &models.MagicLinkToken{
		Token:              token,
		Email:              emailAddr,
		ExpiresAt:          time.Now().Add(time.Duration(validityDays) * 24 * time.Hour),
		RedirectTo:         "/?doc=" + docID,
		CreatedByIP:        "127.0.0.1", // System-initiated
		CreatedByUserAgent: "admin-share",
		Purpose:            "document_share",
		DocID:              &docID,
		OTPHash:            &otpHash,
		OTPAttempts:        0,
		OTPMaxAttempts:     5,
		SharedBy:           &sharedBy,
	}

	if err := s.repo.CreateToken(ctx, magicToken); err != nil {
		return "", "", fmt.Errorf("failed to create document share token: %w", err)
	}

	// Build the magic link URL
	link := fmt.Sprintf("%s/api/v1/auth/document-share/verify?token=%s", s.baseURL, token)

	// Send email to recipient (link only, no OTP)
	if locale == "" {
		locale = "en"
	}

	subject := "A document has been shared with you"
	if s.i18n != nil {
		subject = s.i18n.T(locale, "email.document_share.subject")
	}

	msg := email.Message{
		To:       []string{emailAddr},
		Subject:  subject,
		Template: "document_share",
		Locale:   locale,
		Data: map[string]interface{}{
			"AppName":      s.appName,
			"Email":        emailAddr,
			"ShareLink":    link,
			"ValidityDays": validityDays,
			"BaseURL":      s.baseURL,
		},
	}

	if err := s.emailSender.Send(ctx, msg); err != nil {
		logger.Logger.Error("Failed to send document share email", "error", err, "email", emailAddr)
		// Don't fail the share creation if email fails — admin has the link
	}

	logger.Logger.Info("Document share link created",
		"email", emailAddr,
		"doc_id", docID,
		"shared_by", sharedBy,
		"validity_days", validityDays)

	return link, otp, nil
}

// ValidateDocumentShareToken checks if a document share token exists and is valid.
// This is a read-only check: it does NOT verify OTP or modify attempt counters.
func (s *MagicLinkService) ValidateDocumentShareToken(ctx context.Context, token string) error {
	magicToken, err := s.repo.GetByToken(ctx, token)
	if err != nil {
		return fmt.Errorf("invalid token")
	}

	if magicToken.Purpose != "document_share" {
		return fmt.Errorf("invalid token type")
	}

	if !magicToken.IsValid() {
		if magicToken.RevokedAt != nil {
			return fmt.Errorf("share has been revoked")
		}
		if magicToken.IsOTPLocked() {
			return fmt.Errorf("share is locked")
		}
		return fmt.Errorf("share has expired")
	}

	return nil
}

// VerifyDocumentShareOTP verifies the OTP for a document share token.
// Unlike standard magic links, document_share tokens are multi-use.
func (s *MagicLinkService) VerifyDocumentShareOTP(
	ctx context.Context,
	token string,
	otpInput string,
	ip string,
	userAgent string,
) (*models.MagicLinkToken, error) {
	// Get the token
	magicToken, err := s.repo.GetByToken(ctx, token)
	if err != nil {
		if len(token) > 8 {
			logger.Logger.Warn("Document share token not found", "token_prefix", token[:8])
		} else {
			logger.Logger.Warn("Document share token not found", "token_prefix", token)
		}
		return nil, fmt.Errorf("invalid token")
	}

	// Check purpose
	if magicToken.Purpose != "document_share" {
		logger.Logger.Warn("Token is not a document_share token", "purpose", magicToken.Purpose)
		return nil, fmt.Errorf("invalid token type")
	}

	// Check validity (expiry, revocation, OTP lockout)
	if !magicToken.IsValid() {
		if magicToken.RevokedAt != nil {
			return nil, fmt.Errorf("share has been revoked")
		}
		if magicToken.IsOTPLocked() {
			return nil, fmt.Errorf("too many failed attempts, share is locked")
		}
		return nil, fmt.Errorf("share has expired")
	}

	// Verify OTP
	if magicToken.OTPHash == nil {
		return nil, fmt.Errorf("token has no OTP configured")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(*magicToken.OTPHash), []byte(otpInput)); err != nil {
		// Wrong OTP — increment attempts
		if incErr := s.repo.IncrementOTPAttempts(ctx, token); incErr != nil {
			logger.Logger.Error("Failed to increment OTP attempts", "error", incErr)
		}
		remaining := magicToken.OTPMaxAttempts - magicToken.OTPAttempts - 1
		logger.Logger.Warn("Document share OTP verification failed",
			"email", magicToken.Email,
			"attempts_remaining", remaining,
			"ip", ip)
		return nil, fmt.Errorf("invalid access code")
	}

	// OTP is correct — do NOT mark as used (multi-use token)
	logger.Logger.Info("Document share OTP verified successfully",
		"email", magicToken.Email,
		"doc_id", magicToken.DocID,
		"ip", ip)

	return magicToken, nil
}

// RevokeDocumentShare revokes a document share by token ID
func (s *MagicLinkService) RevokeDocumentShare(ctx context.Context, tokenID int64) error {
	if err := s.repo.RevokeToken(ctx, tokenID); err != nil {
		return fmt.Errorf("failed to revoke share: %w", err)
	}

	logger.Logger.Info("Document share revoked", "token_id", tokenID)
	return nil
}

// ListDocumentShares returns all document_share tokens for a given document
func (s *MagicLinkService) ListDocumentShares(ctx context.Context, docID string) ([]*models.MagicLinkToken, error) {
	return s.repo.ListByDocAndPurpose(ctx, docID, "document_share")
}

// generateOTP generates a cryptographically secure 6-digit OTP
func (s *MagicLinkService) generateOTP() (string, error) {
	max := big.NewInt(1000000) // 0-999999
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
