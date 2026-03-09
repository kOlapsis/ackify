// SPDX-License-Identifier: AGPL-3.0-or-later
package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/kolapsis/ackify/backend/pkg/config"
	"github.com/kolapsis/ackify/backend/pkg/models"
)

// fakeConfigRepository is a mock implementation of configRepository
type fakeConfigRepository struct {
	configs          map[models.ConfigCategory]*models.TenantConfig
	seeded           bool
	shouldFailGet    bool
	shouldFailGetAll bool
	shouldFailUpsert bool
	shouldFailSeeded bool
	shouldFailMark   bool
	shouldFailDelete bool
}

func newFakeConfigRepository() *fakeConfigRepository {
	return &fakeConfigRepository{
		configs: make(map[models.ConfigCategory]*models.TenantConfig),
	}
}

func (f *fakeConfigRepository) GetByCategory(_ context.Context, category models.ConfigCategory) (*models.TenantConfig, error) {
	if f.shouldFailGet {
		return nil, errors.New("repository get failed")
	}
	tc, ok := f.configs[category]
	if !ok {
		return nil, errors.New("config not found")
	}
	return tc, nil
}

func (f *fakeConfigRepository) GetAll(_ context.Context) ([]*models.TenantConfig, error) {
	if f.shouldFailGetAll {
		return nil, errors.New("repository get all failed")
	}
	result := make([]*models.TenantConfig, 0, len(f.configs))
	for _, tc := range f.configs {
		result = append(result, tc)
	}
	return result, nil
}

func (f *fakeConfigRepository) Upsert(_ context.Context, category models.ConfigCategory, cfg json.RawMessage, secrets []byte, updatedBy string) error {
	if f.shouldFailUpsert {
		return errors.New("repository upsert failed")
	}
	f.configs[category] = &models.TenantConfig{
		Category:         category,
		Config:           cfg,
		SecretsEncrypted: secrets,
		UpdatedAt:        time.Now(),
	}
	return nil
}

func (f *fakeConfigRepository) IsSeeded(_ context.Context) (bool, error) {
	if f.shouldFailSeeded {
		return false, errors.New("repository is seeded failed")
	}
	return f.seeded, nil
}

func (f *fakeConfigRepository) MarkSeeded(_ context.Context) error {
	if f.shouldFailMark {
		return errors.New("repository mark seeded failed")
	}
	f.seeded = true
	return nil
}

func (f *fakeConfigRepository) DeleteAll(_ context.Context) error {
	if f.shouldFailDelete {
		return errors.New("repository delete all failed")
	}
	f.configs = make(map[models.ConfigCategory]*models.TenantConfig)
	return nil
}

func (f *fakeConfigRepository) GetLatestUpdatedAt(_ context.Context) (time.Time, error) {
	var latest time.Time
	for _, tc := range f.configs {
		if tc.UpdatedAt.After(latest) {
			latest = tc.UpdatedAt
		}
	}
	return latest, nil
}

// createTestConfigService creates a ConfigService with a fake repository for testing
func createTestConfigService() (*ConfigService, *fakeConfigRepository) {
	repo := newFakeConfigRepository()
	envConfig := &config.Config{
		App: config.AppConfig{
			Organisation:       "Test Org",
			OnlyAdminCanCreate: false,
		},
		Auth: config.AuthConfig{
			OAuthEnabled:     true,
			MagicLinkEnabled: false,
		},
		OAuth: config.OAuthConfig{
			ClientID:      "test-client-id",
			ClientSecret:  "test-client-secret",
			AuthURL:       "https://accounts.google.com/o/oauth2/auth",
			TokenURL:      "https://oauth2.googleapis.com/token",
			UserInfoURL:   "https://openidconnect.googleapis.com/v1/userinfo",
			Scopes:        []string{"openid", "email", "profile"},
			AllowedDomain: "@example.com",
		},
		Mail: config.MailConfig{
			Host:     "smtp.example.com",
			Port:     587,
			Username: "test@example.com",
			Password: "smtp-password",
			TLS:      false,
			StartTLS: true,
			From:     "noreply@example.com",
			FromName: "Test App",
			Timeout:  "10s",
		},
		Storage: config.StorageConfig{
			Type:      "local",
			MaxSizeMB: 50,
			LocalPath: "/data/documents",
		},
	}
	encryptionKey := make([]byte, 32)
	for i := range encryptionKey {
		encryptionKey[i] = byte(i)
	}

	svc := NewConfigService(repo, envConfig, encryptionKey)
	return svc, repo
}

func TestNewConfigService(t *testing.T) {
	svc, _ := createTestConfigService()
	if svc == nil {
		t.Fatal("expected non-nil ConfigService")
	}

	cfg := svc.GetConfig()
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
}

func TestConfigService_Initialize_FirstStartup(t *testing.T) {
	svc, repo := createTestConfigService()
	ctx := context.Background()

	err := svc.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Verify config was seeded
	if !repo.seeded {
		t.Error("expected config to be marked as seeded")
	}

	// Verify configs were created
	if len(repo.configs) == 0 {
		t.Error("expected configs to be created")
	}

	// Verify we can get the config
	cfg := svc.GetConfig()
	if cfg.General.Organisation != "Test Org" {
		t.Errorf("expected organisation 'Test Org', got '%s'", cfg.General.Organisation)
	}
	if !cfg.OIDC.Enabled {
		t.Error("expected OIDC to be enabled")
	}
}

func TestConfigService_Initialize_AlreadySeeded(t *testing.T) {
	svc, repo := createTestConfigService()
	ctx := context.Background()

	// Pre-seed with existing config
	repo.seeded = true
	generalCfg, _ := json.Marshal(models.GeneralConfig{
		Organisation:       "Existing Org",
		OnlyAdminCanCreate: true,
	})
	repo.configs[models.ConfigCategoryGeneral] = &models.TenantConfig{
		Category:  models.ConfigCategoryGeneral,
		Config:    generalCfg,
		UpdatedAt: time.Now(),
	}

	err := svc.Initialize(ctx)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Verify it loaded from DB, not ENV
	cfg := svc.GetConfig()
	if cfg.General.Organisation != "Existing Org" {
		t.Errorf("expected organisation 'Existing Org', got '%s'", cfg.General.Organisation)
	}
}

func TestConfigService_UpdateSection_General(t *testing.T) {
	svc, repo := createTestConfigService()
	ctx := context.Background()

	// First initialize
	_ = svc.Initialize(ctx)

	// Update general config
	input := json.RawMessage(`{"organisation": "Updated Org", "only_admin_can_create": true}`)
	err := svc.UpdateSection(ctx, models.ConfigCategoryGeneral, input, "admin@test.com")
	if err != nil {
		t.Fatalf("UpdateSection failed: %v", err)
	}

	// Verify update
	cfg := svc.GetConfig()
	if cfg.General.Organisation != "Updated Org" {
		t.Errorf("expected organisation 'Updated Org', got '%s'", cfg.General.Organisation)
	}
	if !cfg.General.OnlyAdminCanCreate {
		t.Error("expected OnlyAdminCanCreate to be true")
	}

	// Verify it was saved to repo
	tc, err := repo.GetByCategory(ctx, models.ConfigCategoryGeneral)
	if err != nil {
		t.Fatalf("GetByCategory failed: %v", err)
	}
	var savedCfg models.GeneralConfig
	_ = json.Unmarshal(tc.Config, &savedCfg)
	if savedCfg.Organisation != "Updated Org" {
		t.Errorf("expected saved organisation 'Updated Org', got '%s'", savedCfg.Organisation)
	}
}

func TestConfigService_UpdateSection_InvalidCategory(t *testing.T) {
	svc, _ := createTestConfigService()
	ctx := context.Background()

	_ = svc.Initialize(ctx)

	input := json.RawMessage(`{"foo": "bar"}`)
	err := svc.UpdateSection(ctx, "invalid", input, "admin@test.com")
	if err == nil {
		t.Error("expected error for invalid category")
	}
}

func TestConfigService_UpdateSection_ValidationError(t *testing.T) {
	svc, _ := createTestConfigService()
	ctx := context.Background()

	_ = svc.Initialize(ctx)

	// Disable both auth methods - should fail validation
	oidcInput := json.RawMessage(`{"enabled": false, "provider": ""}`)
	err := svc.UpdateSection(ctx, models.ConfigCategoryOIDC, oidcInput, "admin@test.com")
	if err == nil {
		t.Error("expected error when disabling all auth methods")
	}
	if !errors.Is(err, ErrNoAuthMethod) {
		t.Errorf("expected ErrNoAuthMethod, got %v", err)
	}
}

func TestConfigService_UpdateSection_MagicLinkRequiresSMTP(t *testing.T) {
	svc, repo := createTestConfigService()
	ctx := context.Background()

	_ = svc.Initialize(ctx)

	// Clear SMTP config
	emptySMTP, _ := json.Marshal(models.SMTPConfig{})
	repo.configs[models.ConfigCategorySMTP] = &models.TenantConfig{
		Category:  models.ConfigCategorySMTP,
		Config:    emptySMTP,
		UpdatedAt: time.Now(),
	}
	_ = svc.Reload(ctx)

	// Try to enable MagicLink without SMTP
	input := json.RawMessage(`{"enabled": true}`)
	err := svc.UpdateSection(ctx, models.ConfigCategoryMagicLink, input, "admin@test.com")
	if err == nil {
		t.Error("expected error when enabling MagicLink without SMTP")
	}
	if !errors.Is(err, ErrMagicLinkNeedsSMTP) {
		t.Errorf("expected ErrMagicLinkNeedsSMTP, got %v", err)
	}
}

func TestConfigService_UpdateSection_OIDCCustomRequiresURLs(t *testing.T) {
	svc, _ := createTestConfigService()
	ctx := context.Background()

	_ = svc.Initialize(ctx)

	// Enable custom OIDC without URLs
	input := json.RawMessage(`{"enabled": true, "provider": "custom", "client_id": "test"}`)
	err := svc.UpdateSection(ctx, models.ConfigCategoryOIDC, input, "admin@test.com")
	if err == nil {
		t.Error("expected error when enabling custom OIDC without URLs")
	}
}

func TestConfigService_ResetFromENV(t *testing.T) {
	svc, repo := createTestConfigService()
	ctx := context.Background()

	_ = svc.Initialize(ctx)

	// Modify config
	input := json.RawMessage(`{"organisation": "Modified Org", "only_admin_can_create": true}`)
	_ = svc.UpdateSection(ctx, models.ConfigCategoryGeneral, input, "admin@test.com")

	cfg := svc.GetConfig()
	if cfg.General.Organisation != "Modified Org" {
		t.Fatalf("expected 'Modified Org', got '%s'", cfg.General.Organisation)
	}

	// Reset from ENV
	err := svc.ResetFromENV(ctx, "admin@test.com")
	if err != nil {
		t.Fatalf("ResetFromENV failed: %v", err)
	}

	// Verify it was reset to ENV values
	cfg = svc.GetConfig()
	if cfg.General.Organisation != "Test Org" {
		t.Errorf("expected organisation 'Test Org' after reset, got '%s'", cfg.General.Organisation)
	}

	// Verify repo was cleared and reseeded
	if len(repo.configs) == 0 {
		t.Error("expected configs to be present after reset")
	}
}

func TestConfigService_Subscribe(t *testing.T) {
	svc, _ := createTestConfigService()
	ctx := context.Background()

	_ = svc.Initialize(ctx)

	// Subscribe
	ch := svc.Subscribe()
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}

	// Update config - should trigger notification
	go func() {
		input := json.RawMessage(`{"organisation": "Notified Org", "only_admin_can_create": false}`)
		_ = svc.UpdateSection(ctx, models.ConfigCategoryGeneral, input, "admin@test.com")
	}()

	// Wait for notification
	select {
	case cfg := <-ch:
		if cfg.General.Organisation != "Notified Org" {
			t.Errorf("expected 'Notified Org', got '%s'", cfg.General.Organisation)
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for config notification")
	}
}

func TestConfigService_CloseAllSubscribers(t *testing.T) {
	svc, _ := createTestConfigService()
	ctx := context.Background()

	_ = svc.Initialize(ctx)

	ch := svc.Subscribe()
	svc.CloseAllSubscribers()

	// Channel should be closed
	_, ok := <-ch
	if ok {
		t.Error("expected channel to be closed")
	}
}

func TestConfigService_EncryptDecryptSecrets(t *testing.T) {
	svc, _ := createTestConfigService()

	plaintext := []byte(`{"password":"secret123"}`)

	encrypted, err := svc.encryptSecrets(plaintext)
	if err != nil {
		t.Fatalf("encryptSecrets failed: %v", err)
	}

	if len(encrypted) == 0 {
		t.Error("expected non-empty encrypted data")
	}

	decrypted, err := svc.decryptSecrets(encrypted)
	if err != nil {
		t.Fatalf("decryptSecrets failed: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Errorf("expected '%s', got '%s'", plaintext, decrypted)
	}
}

func TestConfigService_ValidateSection_Storage(t *testing.T) {
	svc, _ := createTestConfigService()
	ctx := context.Background()

	_ = svc.Initialize(ctx)

	// S3 without bucket should fail
	input := json.RawMessage(`{"type": "s3", "max_size_mb": 50, "s3_endpoint": "s3.amazonaws.com"}`)
	err := svc.UpdateSection(ctx, models.ConfigCategoryStorage, input, "admin@test.com")
	if err == nil {
		t.Error("expected error when S3 type without bucket")
	}
}

func TestConfigService_UpdateSection_PreserveMaskedSecrets(t *testing.T) {
	svc, _ := createTestConfigService()
	ctx := context.Background()

	_ = svc.Initialize(ctx)

	// First set a secret
	input := json.RawMessage(`{"enabled": true, "provider": "google", "client_id": "id123", "client_secret": "secret123"}`)
	err := svc.UpdateSection(ctx, models.ConfigCategoryOIDC, input, "admin@test.com")
	if err != nil {
		t.Fatalf("UpdateSection failed: %v", err)
	}

	cfg := svc.GetConfig()
	if cfg.OIDC.ClientSecret != "secret123" {
		t.Fatalf("expected secret to be stored")
	}

	// Update with masked secret - should preserve original
	maskedInput := json.RawMessage(`{"enabled": true, "provider": "google", "client_id": "id123", "client_secret": "********"}`)
	err = svc.UpdateSection(ctx, models.ConfigCategoryOIDC, maskedInput, "admin@test.com")
	if err != nil {
		t.Fatalf("UpdateSection with masked secret failed: %v", err)
	}

	cfg = svc.GetConfig()
	if cfg.OIDC.ClientSecret != "secret123" {
		t.Errorf("expected secret to be preserved, got '%s'", cfg.OIDC.ClientSecret)
	}
}

func TestConfigService_DetectOAuthProvider(t *testing.T) {
	tests := []struct {
		name     string
		authURL  string
		expected string
	}{
		{"Google", "https://accounts.google.com/o/oauth2/auth", "google"},
		{"GitHub", "https://github.com/login/oauth/authorize", "github"},
		{"GitLab", "https://gitlab.com/oauth/authorize", "gitlab"},
		{"Custom", "https://auth.custom.com/authorize", "custom"},
		{"Empty", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeConfigRepository()
			envConfig := &config.Config{
				OAuth: config.OAuthConfig{
					AuthURL: tc.authURL,
				},
			}
			svc := NewConfigService(repo, envConfig, make([]byte, 32))
			result := svc.detectOAuthProvider()
			if result != tc.expected {
				t.Errorf("expected '%s', got '%s'", tc.expected, result)
			}
		})
	}
}

func TestConfigService_Initialize_RepoError(t *testing.T) {
	svc, repo := createTestConfigService()
	repo.shouldFailSeeded = true
	ctx := context.Background()

	err := svc.Initialize(ctx)
	if err == nil {
		t.Error("expected error when repo fails")
	}
}

func TestConfigService_UpdateSection_RepoError(t *testing.T) {
	svc, repo := createTestConfigService()
	ctx := context.Background()

	_ = svc.Initialize(ctx)
	repo.shouldFailUpsert = true

	input := json.RawMessage(`{"organisation": "Test", "only_admin_can_create": false}`)
	err := svc.UpdateSection(ctx, models.ConfigCategoryGeneral, input, "admin@test.com")
	if err == nil {
		t.Error("expected error when repo fails")
	}
}

func TestConfigService_ResetFromENV_DeleteError(t *testing.T) {
	svc, repo := createTestConfigService()
	ctx := context.Background()

	_ = svc.Initialize(ctx)
	repo.shouldFailDelete = true

	err := svc.ResetFromENV(ctx, "admin@test.com")
	if err == nil {
		t.Error("expected error when delete fails")
	}
}

func TestConfigCategory_IsValid(t *testing.T) {
	tests := []struct {
		category models.ConfigCategory
		valid    bool
	}{
		{models.ConfigCategoryGeneral, true},
		{models.ConfigCategoryOIDC, true},
		{models.ConfigCategoryMagicLink, true},
		{models.ConfigCategorySMTP, true},
		{models.ConfigCategoryStorage, true},
		{"invalid", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(string(tc.category), func(t *testing.T) {
			result := tc.category.IsValid()
			if result != tc.valid {
				t.Errorf("expected %v, got %v", tc.valid, result)
			}
		})
	}
}

func TestMutableConfig_HasAtLeastOneAuthMethod(t *testing.T) {
	tests := []struct {
		name         string
		oidcEnabled  bool
		magicEnabled bool
		expected     bool
	}{
		{"Both enabled", true, true, true},
		{"Only OIDC", true, false, true},
		{"Only MagicLink", false, true, true},
		{"Neither", false, false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &models.MutableConfig{
				OIDC:      models.OIDCConfig{Enabled: tc.oidcEnabled},
				MagicLink: models.MagicLinkConfig{Enabled: tc.magicEnabled},
			}
			result := cfg.HasAtLeastOneAuthMethod()
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestMutableConfig_MagicLinkRequiresSMTP(t *testing.T) {
	tests := []struct {
		name         string
		magicEnabled bool
		smtpHost     string
		smtpFrom     string
		expected     bool
	}{
		{"MagicLink disabled", false, "", "", true},
		{"MagicLink enabled with SMTP", true, "smtp.test.com", "noreply@test.com", true},
		{"MagicLink enabled without SMTP", true, "", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &models.MutableConfig{
				MagicLink: models.MagicLinkConfig{Enabled: tc.magicEnabled},
				SMTP:      models.SMTPConfig{Host: tc.smtpHost, From: tc.smtpFrom},
			}
			result := cfg.MagicLinkRequiresSMTP()
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestMutableConfig_MaskSecrets(t *testing.T) {
	cfg := &models.MutableConfig{
		OIDC:    models.OIDCConfig{ClientSecret: "secret123"},
		SMTP:    models.SMTPConfig{Password: "smtppass"},
		Storage: models.StorageConfig{S3SecretKey: "s3key"},
	}

	masked := cfg.MaskSecrets()

	if masked.OIDC.ClientSecret != models.SecretMask {
		t.Errorf("expected OIDC secret to be masked")
	}
	if masked.SMTP.Password != models.SecretMask {
		t.Errorf("expected SMTP password to be masked")
	}
	if masked.Storage.S3SecretKey != models.SecretMask {
		t.Errorf("expected S3 secret to be masked")
	}

	// Original should be unchanged
	if cfg.OIDC.ClientSecret != "secret123" {
		t.Errorf("original OIDC secret should be unchanged")
	}
}

func TestIsSecretMasked(t *testing.T) {
	tests := []struct {
		value    string
		expected bool
	}{
		{models.SecretMask, true},
		{"********", true},
		{"secret123", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			result := models.IsSecretMasked(tc.value)
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}
