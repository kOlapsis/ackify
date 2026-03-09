// SPDX-License-Identifier: AGPL-3.0-or-later
package services

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	mail "github.com/go-mail/mail/v2"
	"github.com/kolapsis/ackify/backend/pkg/config"
	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/models"
)

var (
	ErrNoAuthMethod       = errors.New("at least one authentication method must be enabled")
	ErrMagicLinkNeedsSMTP = errors.New("MagicLink requires SMTP to be configured")
	ErrOIDCNeedsURLs      = errors.New("custom OIDC provider requires auth, token, and userinfo URLs")
	ErrInvalidCategory    = errors.New("invalid configuration category")
)

type configRepository interface {
	GetByCategory(ctx context.Context, category models.ConfigCategory) (*models.TenantConfig, error)
	GetAll(ctx context.Context) ([]*models.TenantConfig, error)
	Upsert(ctx context.Context, category models.ConfigCategory, config json.RawMessage, secrets []byte, updatedBy string) error
	IsSeeded(ctx context.Context) (bool, error)
	MarkSeeded(ctx context.Context) error
	DeleteAll(ctx context.Context) error
	GetLatestUpdatedAt(ctx context.Context) (time.Time, error)
}

// ConfigService manages application configuration with hot-reload support
type ConfigService struct {
	repo          configRepository
	encryptionKey []byte
	envConfig     *config.Config

	currentConfig atomic.Value // *models.MutableConfig

	subscribersMu sync.RWMutex
	subscribers   []chan<- models.MutableConfig
}

// NewConfigService creates a new configuration service
func NewConfigService(repo configRepository, envConfig *config.Config, encryptionKey []byte) *ConfigService {
	svc := &ConfigService{
		repo:          repo,
		envConfig:     envConfig,
		encryptionKey: encryptionKey,
		subscribers:   make([]chan<- models.MutableConfig, 0),
	}
	svc.currentConfig.Store(&models.MutableConfig{})
	return svc
}

// Initialize loads config from DB or seeds from ENV on first start
func (s *ConfigService) Initialize(ctx context.Context) error {
	seeded, err := s.repo.IsSeeded(ctx)
	if err != nil {
		return fmt.Errorf("failed to check if seeded: %w", err)
	}

	if !seeded {
		logger.Logger.Info("First startup: seeding configuration from environment")
		if err := s.seedFromENV(ctx); err != nil {
			return fmt.Errorf("failed to seed config: %w", err)
		}
	}

	return s.Reload(ctx)
}

// GetConfig returns the current config (lock-free read)
func (s *ConfigService) GetConfig() *models.MutableConfig {
	return s.currentConfig.Load().(*models.MutableConfig)
}

// UpdateSection updates a specific config section
func (s *ConfigService) UpdateSection(ctx context.Context, category models.ConfigCategory, input json.RawMessage, updatedBy string) error {
	if !category.IsValid() {
		return ErrInvalidCategory
	}

	// Parse the input to validate structure
	if err := s.validateSection(category, input); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Get current config to check cross-category validation
	currentConfig := s.GetConfig()

	// Apply the update temporarily to check cross-category validation
	tempConfig := *currentConfig
	if err := s.applyUpdateToConfig(&tempConfig, category, input); err != nil {
		return fmt.Errorf("failed to apply update: %w", err)
	}

	// Validate cross-category rules
	if err := s.validateCrossCategory(&tempConfig); err != nil {
		return err
	}

	// Extract and encrypt secrets
	configWithoutSecrets, encryptedSecrets, err := s.processSecrets(category, input)
	if err != nil {
		return fmt.Errorf("failed to process secrets: %w", err)
	}

	// Store in DB
	if err := s.repo.Upsert(ctx, category, configWithoutSecrets, encryptedSecrets, updatedBy); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Hot-reload
	return s.Reload(ctx)
}

// ResetFromENV resets config to current ENV values
func (s *ConfigService) ResetFromENV(ctx context.Context, updatedBy string) error {
	// Delete all existing config
	if err := s.repo.DeleteAll(ctx); err != nil {
		return fmt.Errorf("failed to delete existing config: %w", err)
	}

	// Seed from ENV
	if err := s.seedFromENV(ctx); err != nil {
		return fmt.Errorf("failed to seed from ENV: %w", err)
	}

	// Reload
	return s.Reload(ctx)
}

// Subscribe registers a channel to receive config updates
func (s *ConfigService) Subscribe() <-chan models.MutableConfig {
	ch := make(chan models.MutableConfig, 1)
	s.subscribersMu.Lock()
	s.subscribers = append(s.subscribers, ch)
	s.subscribersMu.Unlock()
	return ch
}

// CloseAllSubscribers closes all subscriber channels (used during shutdown)
func (s *ConfigService) CloseAllSubscribers() {
	s.subscribersMu.Lock()
	defer s.subscribersMu.Unlock()

	for _, ch := range s.subscribers {
		close(ch)
	}
	s.subscribers = nil
}

// --- Test Connection Methods ---

// TestSMTP tests SMTP connection
func (s *ConfigService) TestSMTP(ctx context.Context, cfg models.SMTPConfig) error {
	if cfg.Host == "" {
		return errors.New("SMTP host is required")
	}

	// Handle masked password - use current config's password
	if models.IsSecretMasked(cfg.Password) {
		current := s.GetConfig()
		cfg.Password = current.SMTP.Password
	}

	d := mail.NewDialer(cfg.Host, cfg.Port, cfg.Username, cfg.Password)

	if cfg.TLS {
		d.SSL = true
		d.TLSConfig = &tls.Config{
			ServerName:         cfg.Host,
			InsecureSkipVerify: cfg.InsecureSkipVerify,
		}
	} else if cfg.StartTLS {
		d.TLSConfig = &tls.Config{
			ServerName:         cfg.Host,
			InsecureSkipVerify: cfg.InsecureSkipVerify,
		}
		d.StartTLSPolicy = mail.MandatoryStartTLS
	}

	timeout, err := time.ParseDuration(cfg.Timeout)
	if err != nil {
		timeout = 10 * time.Second
	}
	d.Timeout = timeout

	// Try to connect
	closer, err := d.Dial()
	if err != nil {
		return fmt.Errorf("SMTP connection failed: %w", err)
	}
	defer closer.Close()

	return nil
}

// TestS3 tests S3 connection
func (s *ConfigService) TestS3(ctx context.Context, cfg models.StorageConfig) error {
	if cfg.Type != "s3" {
		return errors.New("storage type must be 's3' for S3 test")
	}
	if cfg.S3Bucket == "" {
		return errors.New("S3 bucket is required")
	}

	// Handle masked secret key - use current config's key
	if models.IsSecretMasked(cfg.S3SecretKey) {
		current := s.GetConfig()
		cfg.S3SecretKey = current.Storage.S3SecretKey
	}

	// Build AWS config
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.S3Region),
	}

	if cfg.S3AccessKey != "" && cfg.S3SecretKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.S3AccessKey, cfg.S3SecretKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client
	s3Opts := []func(*s3.Options){}
	if cfg.S3Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.S3Endpoint)
			o.UsePathStyle = true
		})
	}

	client := s3.NewFromConfig(awsCfg, s3Opts...)

	// Test bucket access
	_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(cfg.S3Bucket),
	})
	if err != nil {
		return fmt.Errorf("S3 bucket access failed: %w", err)
	}

	return nil
}

// TestOIDC tests OIDC configuration by fetching the well-known endpoint
func (s *ConfigService) TestOIDC(ctx context.Context, cfg models.OIDCConfig) error {
	if !cfg.Enabled {
		return errors.New("OIDC is not enabled")
	}

	// Handle masked client secret - use current config's secret
	if models.IsSecretMasked(cfg.ClientSecret) {
		current := s.GetConfig()
		cfg.ClientSecret = current.OIDC.ClientSecret
	}

	// Determine the well-known URL based on provider
	var wellKnownURL string
	switch cfg.Provider {
	case "google":
		wellKnownURL = "https://accounts.google.com/.well-known/openid-configuration"
	case "github":
		// GitHub doesn't have a standard OIDC discovery endpoint, just validate URLs exist
		if cfg.ClientID == "" || cfg.ClientSecret == "" {
			return errors.New("GitHub OAuth requires client_id and client_secret")
		}
		return nil
	case "gitlab":
		baseURL := "https://gitlab.com"
		if cfg.AuthURL != "" && strings.Contains(cfg.AuthURL, "gitlab") {
			// Extract base URL from auth URL
			parts := strings.Split(cfg.AuthURL, "/oauth")
			if len(parts) > 0 {
				baseURL = parts[0]
			}
		}
		wellKnownURL = baseURL + "/.well-known/openid-configuration"
	case "custom":
		// For custom providers, validate that required URLs are present
		if cfg.AuthURL == "" || cfg.TokenURL == "" || cfg.UserInfoURL == "" {
			return ErrOIDCNeedsURLs
		}
		// Try to derive issuer from auth URL
		parts := strings.Split(cfg.AuthURL, "/")
		if len(parts) >= 3 {
			issuer := strings.Join(parts[:3], "/")
			wellKnownURL = issuer + "/.well-known/openid-configuration"
		}
	default:
		return fmt.Errorf("unknown OIDC provider: %s", cfg.Provider)
	}

	if wellKnownURL == "" {
		return nil // No well-known to check
	}

	// Fetch well-known endpoint
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wellKnownURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch OIDC discovery endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("OIDC discovery endpoint returned status %d", resp.StatusCode)
	}

	// Parse response to validate it's a valid OIDC configuration
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	var discovery struct {
		Issuer                string `json:"issuer"`
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
		UserinfoEndpoint      string `json:"userinfo_endpoint"`
	}

	if err := json.Unmarshal(body, &discovery); err != nil {
		return fmt.Errorf("invalid OIDC discovery response: %w", err)
	}

	if discovery.AuthorizationEndpoint == "" || discovery.TokenEndpoint == "" {
		return errors.New("OIDC discovery missing required endpoints")
	}

	return nil
}

// --- Internal Methods ---

// seedFromENV seeds configuration from environment variables
func (s *ConfigService) seedFromENV(ctx context.Context) error {
	// General config
	general := models.GeneralConfig{
		Organisation:       s.envConfig.App.Organisation,
		OnlyAdminCanCreate: s.envConfig.App.OnlyAdminCanCreate,
		AllowedDomains:     s.envConfig.App.AllowedDomains,
	}
	if err := s.upsertSection(ctx, models.ConfigCategoryGeneral, general, nil, "system"); err != nil {
		return fmt.Errorf("failed to seed general config: %w", err)
	}

	// OIDC config
	oidc := models.OIDCConfig{
		Enabled:       s.envConfig.Auth.OAuthEnabled,
		Provider:      s.detectOAuthProvider(),
		ClientID:      s.envConfig.OAuth.ClientID,
		ClientSecret:  s.envConfig.OAuth.ClientSecret,
		AuthURL:       s.envConfig.OAuth.AuthURL,
		TokenURL:      s.envConfig.OAuth.TokenURL,
		UserInfoURL:   s.envConfig.OAuth.UserInfoURL,
		LogoutURL:     s.envConfig.OAuth.LogoutURL,
		Scopes:        s.envConfig.OAuth.Scopes,
		AllowedDomain: s.envConfig.OAuth.AllowedDomain,
		AutoLogin:     s.envConfig.OAuth.AutoLogin,
	}
	oidcSecrets := models.OIDCSecrets{ClientSecret: s.envConfig.OAuth.ClientSecret}
	if err := s.upsertSection(ctx, models.ConfigCategoryOIDC, oidc, oidcSecrets, "system"); err != nil {
		return fmt.Errorf("failed to seed OIDC config: %w", err)
	}

	// MagicLink config
	magicLink := models.MagicLinkConfig{
		Enabled: s.envConfig.Auth.MagicLinkEnabled,
	}
	if err := s.upsertSection(ctx, models.ConfigCategoryMagicLink, magicLink, nil, "system"); err != nil {
		return fmt.Errorf("failed to seed MagicLink config: %w", err)
	}

	// SMTP config
	smtp := models.SMTPConfig{
		Host:               s.envConfig.Mail.Host,
		Port:               s.envConfig.Mail.Port,
		Username:           s.envConfig.Mail.Username,
		Password:           s.envConfig.Mail.Password,
		TLS:                s.envConfig.Mail.TLS,
		StartTLS:           s.envConfig.Mail.StartTLS,
		InsecureSkipVerify: s.envConfig.Mail.InsecureSkipVerify,
		Timeout:            s.envConfig.Mail.Timeout,
		From:               s.envConfig.Mail.From,
		FromName:           s.envConfig.Mail.FromName,
		SubjectPrefix:      s.envConfig.Mail.SubjectPrefix,
	}
	smtpSecrets := models.SMTPSecrets{Password: s.envConfig.Mail.Password}
	if err := s.upsertSection(ctx, models.ConfigCategorySMTP, smtp, smtpSecrets, "system"); err != nil {
		return fmt.Errorf("failed to seed SMTP config: %w", err)
	}

	// Storage config
	storage := models.StorageConfig{
		Type:        s.envConfig.Storage.Type,
		MaxSizeMB:   s.envConfig.Storage.MaxSizeMB,
		LocalPath:   s.envConfig.Storage.LocalPath,
		S3Endpoint:  s.envConfig.Storage.S3Endpoint,
		S3Bucket:    s.envConfig.Storage.S3Bucket,
		S3AccessKey: s.envConfig.Storage.S3AccessKey,
		S3SecretKey: s.envConfig.Storage.S3SecretKey,
		S3Region:    s.envConfig.Storage.S3Region,
		S3UseSSL:    s.envConfig.Storage.S3UseSSL,
	}
	storageSecrets := models.StorageSecrets{S3SecretKey: s.envConfig.Storage.S3SecretKey}
	if err := s.upsertSection(ctx, models.ConfigCategoryStorage, storage, storageSecrets, "system"); err != nil {
		return fmt.Errorf("failed to seed Storage config: %w", err)
	}

	// Mark as seeded
	if err := s.repo.MarkSeeded(ctx); err != nil {
		return fmt.Errorf("failed to mark config as seeded: %w", err)
	}

	return nil
}

// detectOAuthProvider detects the OAuth provider from the configuration
func (s *ConfigService) detectOAuthProvider() string {
	authURL := s.envConfig.OAuth.AuthURL
	if strings.Contains(authURL, "accounts.google.com") {
		return "google"
	}
	if strings.Contains(authURL, "github.com") {
		return "github"
	}
	if strings.Contains(authURL, "gitlab") {
		return "gitlab"
	}
	if authURL != "" {
		return "custom"
	}
	return ""
}

// upsertSection marshals and stores a config section
func (s *ConfigService) upsertSection(ctx context.Context, category models.ConfigCategory, cfg any, secrets any, updatedBy string) error {
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Remove secrets from config JSON
	configWithoutSecrets := s.removeSecretsFromJSON(category, configJSON)

	var encryptedSecrets []byte
	if secrets != nil {
		secretsJSON, err := json.Marshal(secrets)
		if err != nil {
			return fmt.Errorf("failed to marshal secrets: %w", err)
		}
		encryptedSecrets, err = s.encryptSecrets(secretsJSON)
		if err != nil {
			return fmt.Errorf("failed to encrypt secrets: %w", err)
		}
	}

	return s.repo.Upsert(ctx, category, configWithoutSecrets, encryptedSecrets, updatedBy)
}

// removeSecretsFromJSON removes secret fields from config JSON
func (s *ConfigService) removeSecretsFromJSON(category models.ConfigCategory, configJSON json.RawMessage) json.RawMessage {
	var data map[string]any
	if err := json.Unmarshal(configJSON, &data); err != nil {
		return configJSON
	}

	// Remove secret fields based on category
	switch category {
	case models.ConfigCategoryOIDC:
		delete(data, "client_secret")
	case models.ConfigCategorySMTP:
		delete(data, "password")
	case models.ConfigCategoryStorage:
		delete(data, "s3_secret_key")
	}

	result, err := json.Marshal(data)
	if err != nil {
		return configJSON
	}
	return result
}

// Reload fetches all config from DB and notifies subscribers.
// In multi-tenant mode, the caller should ensure the context has the correct
// tenant RLS transaction so the right config is loaded.
func (s *ConfigService) Reload(ctx context.Context) error {
	configs, err := s.repo.GetAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to load configs: %w", err)
	}

	mutable := &models.MutableConfig{}
	for _, cfg := range configs {
		if err := s.populateCategory(mutable, cfg); err != nil {
			logger.Logger.Warn("Failed to parse config category", "category", cfg.Category, "error", err)
		}
	}

	// Get latest updated_at
	updatedAt, err := s.repo.GetLatestUpdatedAt(ctx)
	if err != nil {
		logger.Logger.Warn("Failed to get latest updated_at", "error", err)
	} else {
		mutable.UpdatedAt = updatedAt
	}

	// Atomic swap
	s.currentConfig.Store(mutable)

	// Notify subscribers
	s.notifySubscribers(*mutable)

	logger.Logger.Info("Configuration reloaded", "updated_at", mutable.UpdatedAt)
	return nil
}

// populateCategory populates the MutableConfig with data from a TenantConfig
func (s *ConfigService) populateCategory(mutable *models.MutableConfig, tc *models.TenantConfig) error {
	// Decrypt secrets if present
	var secrets map[string]string
	if len(tc.SecretsEncrypted) > 0 {
		decrypted, err := s.decryptSecrets(tc.SecretsEncrypted)
		if err != nil {
			logger.Logger.Warn("Failed to decrypt secrets", "category", tc.Category, "error", err)
		} else {
			_ = json.Unmarshal(decrypted, &secrets)
		}
	}

	switch tc.Category {
	case models.ConfigCategoryGeneral:
		var cfg models.GeneralConfig
		if err := json.Unmarshal(tc.Config, &cfg); err != nil {
			return err
		}
		mutable.General = cfg

	case models.ConfigCategoryOIDC:
		var cfg models.OIDCConfig
		if err := json.Unmarshal(tc.Config, &cfg); err != nil {
			return err
		}
		if secrets != nil {
			if v, ok := secrets["client_secret"]; ok {
				cfg.ClientSecret = v
			}
		}
		mutable.OIDC = cfg

	case models.ConfigCategoryMagicLink:
		var cfg models.MagicLinkConfig
		if err := json.Unmarshal(tc.Config, &cfg); err != nil {
			return err
		}
		mutable.MagicLink = cfg

	case models.ConfigCategorySMTP:
		var cfg models.SMTPConfig
		if err := json.Unmarshal(tc.Config, &cfg); err != nil {
			return err
		}
		if secrets != nil {
			if v, ok := secrets["password"]; ok {
				cfg.Password = v
			}
		}
		mutable.SMTP = cfg

	case models.ConfigCategoryStorage:
		var cfg models.StorageConfig
		if err := json.Unmarshal(tc.Config, &cfg); err != nil {
			return err
		}
		if secrets != nil {
			if v, ok := secrets["s3_secret_key"]; ok {
				cfg.S3SecretKey = v
			}
		}
		mutable.Storage = cfg
	}

	return nil
}

// notifySubscribers sends config updates to all subscribers
func (s *ConfigService) notifySubscribers(cfg models.MutableConfig) {
	s.subscribersMu.RLock()
	defer s.subscribersMu.RUnlock()

	for _, ch := range s.subscribers {
		select {
		case ch <- cfg:
		default:
			// Channel full, skip this update
		}
	}
}

// validateSection validates a single config section
func (s *ConfigService) validateSection(category models.ConfigCategory, input json.RawMessage) error {
	switch category {
	case models.ConfigCategoryGeneral:
		var cfg models.GeneralConfig
		return json.Unmarshal(input, &cfg)

	case models.ConfigCategoryOIDC:
		var cfg models.OIDCConfig
		if err := json.Unmarshal(input, &cfg); err != nil {
			return err
		}
		if cfg.Enabled && cfg.Provider == "custom" {
			if cfg.AuthURL == "" || cfg.TokenURL == "" || cfg.UserInfoURL == "" {
				return ErrOIDCNeedsURLs
			}
		}
		return nil

	case models.ConfigCategoryMagicLink:
		var cfg models.MagicLinkConfig
		return json.Unmarshal(input, &cfg)

	case models.ConfigCategorySMTP:
		var cfg models.SMTPConfig
		return json.Unmarshal(input, &cfg)

	case models.ConfigCategoryStorage:
		var cfg models.StorageConfig
		if err := json.Unmarshal(input, &cfg); err != nil {
			return err
		}
		if cfg.Type == "s3" && cfg.S3Bucket == "" {
			return errors.New("S3 bucket is required when storage type is 's3'")
		}
		return nil
	}

	return ErrInvalidCategory
}

// applyUpdateToConfig applies an update to a MutableConfig for validation
func (s *ConfigService) applyUpdateToConfig(cfg *models.MutableConfig, category models.ConfigCategory, input json.RawMessage) error {
	switch category {
	case models.ConfigCategoryGeneral:
		return json.Unmarshal(input, &cfg.General)
	case models.ConfigCategoryOIDC:
		var oidc models.OIDCConfig
		if err := json.Unmarshal(input, &oidc); err != nil {
			return err
		}
		// Preserve existing secret if masked
		if models.IsSecretMasked(oidc.ClientSecret) {
			oidc.ClientSecret = cfg.OIDC.ClientSecret
		}
		cfg.OIDC = oidc
		return nil
	case models.ConfigCategoryMagicLink:
		return json.Unmarshal(input, &cfg.MagicLink)
	case models.ConfigCategorySMTP:
		var smtp models.SMTPConfig
		if err := json.Unmarshal(input, &smtp); err != nil {
			return err
		}
		// Preserve existing secret if masked
		if models.IsSecretMasked(smtp.Password) {
			smtp.Password = cfg.SMTP.Password
		}
		cfg.SMTP = smtp
		return nil
	case models.ConfigCategoryStorage:
		var storage models.StorageConfig
		if err := json.Unmarshal(input, &storage); err != nil {
			return err
		}
		// Preserve existing secret if masked
		if models.IsSecretMasked(storage.S3SecretKey) {
			storage.S3SecretKey = cfg.Storage.S3SecretKey
		}
		cfg.Storage = storage
		return nil
	}
	return ErrInvalidCategory
}

// validateCrossCategory validates cross-category rules.
// When DisableAuthMethodCheck is set in the env config (e.g. by an extension like
// SaaS that manages auth at a higher layer), auth method checks are skipped entirely.
func (s *ConfigService) validateCrossCategory(cfg *models.MutableConfig) error {
	if s.envConfig.Auth.DisableAuthMethodCheck {
		return nil
	}

	// At least one auth method must be enabled
	if !cfg.HasAtLeastOneAuthMethod() {
		return ErrNoAuthMethod
	}

	// MagicLink requires SMTP
	if !cfg.MagicLinkRequiresSMTP() {
		return ErrMagicLinkNeedsSMTP
	}

	return nil
}

// processSecrets extracts and encrypts secrets from input
func (s *ConfigService) processSecrets(category models.ConfigCategory, input json.RawMessage) (json.RawMessage, []byte, error) {
	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return nil, nil, err
	}

	secrets := make(map[string]string)
	currentConfig := s.GetConfig()

	switch category {
	case models.ConfigCategoryOIDC:
		if secret, ok := data["client_secret"].(string); ok && secret != "" {
			if models.IsSecretMasked(secret) {
				secrets["client_secret"] = currentConfig.OIDC.ClientSecret
			} else {
				secrets["client_secret"] = secret
			}
		}
		delete(data, "client_secret")

	case models.ConfigCategorySMTP:
		if secret, ok := data["password"].(string); ok && secret != "" {
			if models.IsSecretMasked(secret) {
				secrets["password"] = currentConfig.SMTP.Password
			} else {
				secrets["password"] = secret
			}
		}
		delete(data, "password")

	case models.ConfigCategoryStorage:
		if secret, ok := data["s3_secret_key"].(string); ok && secret != "" {
			if models.IsSecretMasked(secret) {
				secrets["s3_secret_key"] = currentConfig.Storage.S3SecretKey
			} else {
				secrets["s3_secret_key"] = secret
			}
		}
		delete(data, "s3_secret_key")
	}

	configWithoutSecrets, err := json.Marshal(data)
	if err != nil {
		return nil, nil, err
	}

	var encryptedSecrets []byte
	if len(secrets) > 0 {
		secretsJSON, err := json.Marshal(secrets)
		if err != nil {
			return nil, nil, err
		}
		encryptedSecrets, err = s.encryptSecrets(secretsJSON)
		if err != nil {
			return nil, nil, err
		}
	}

	return configWithoutSecrets, encryptedSecrets, nil
}

// encryptSecrets encrypts secrets using AES-256-GCM
func (s *ConfigService) encryptSecrets(plaintext []byte) ([]byte, error) {
	if len(s.encryptionKey) < 32 {
		return nil, errors.New("encryption key too short")
	}

	block, err := aes.NewCipher(s.encryptionKey[:32])
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decryptSecrets decrypts secrets using AES-256-GCM
func (s *ConfigService) decryptSecrets(ciphertext []byte) ([]byte, error) {
	if len(s.encryptionKey) < 32 {
		return nil, errors.New("encryption key too short")
	}

	block, err := aes.NewCipher(s.encryptionKey[:32])
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}
