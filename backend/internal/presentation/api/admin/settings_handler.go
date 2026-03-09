// SPDX-License-Identifier: AGPL-3.0-or-later
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/kolapsis/ackify/backend/internal/presentation/api/shared"
	"github.com/kolapsis/ackify/backend/pkg/models"
)

// configService defines the interface for configuration management
type configService interface {
	GetConfig() *models.MutableConfig
	UpdateSection(ctx context.Context, category models.ConfigCategory, input json.RawMessage, updatedBy string) error
	TestSMTP(ctx context.Context, cfg models.SMTPConfig) error
	TestS3(ctx context.Context, cfg models.StorageConfig) error
	TestOIDC(ctx context.Context, cfg models.OIDCConfig) error
	ResetFromENV(ctx context.Context, updatedBy string) error
}

// SettingsHandler handles admin settings endpoints
type SettingsHandler struct {
	configService configService
}

// NewSettingsHandler creates a new settings handler
func NewSettingsHandler(configService configService) *SettingsHandler {
	return &SettingsHandler{configService: configService}
}

// SettingsResponse represents the full settings response
type SettingsResponse struct {
	General   models.GeneralConfig   `json:"general"`
	OIDC      OIDCResponse           `json:"oidc"`
	MagicLink models.MagicLinkConfig `json:"magiclink"`
	SMTP      SMTPResponse           `json:"smtp"`
	Storage   StorageResponse        `json:"storage"`
	UpdatedAt string                 `json:"updated_at"`
}

// OIDCResponse is OIDCConfig with masked secrets
type OIDCResponse struct {
	Enabled       bool     `json:"enabled"`
	Provider      string   `json:"provider"`
	ClientID      string   `json:"client_id"`
	ClientSecret  string   `json:"client_secret"`
	AuthURL       string   `json:"auth_url,omitempty"`
	TokenURL      string   `json:"token_url,omitempty"`
	UserInfoURL   string   `json:"userinfo_url,omitempty"`
	LogoutURL     string   `json:"logout_url,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`
	AllowedDomain string   `json:"allowed_domain,omitempty"`
	AutoLogin     bool     `json:"auto_login"`
}

// SMTPResponse is SMTPConfig with masked secrets
type SMTPResponse struct {
	Host               string `json:"host"`
	Port               int    `json:"port"`
	Username           string `json:"username"`
	Password           string `json:"password"`
	TLS                bool   `json:"tls"`
	StartTLS           bool   `json:"starttls"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify"`
	Timeout            string `json:"timeout"`
	From               string `json:"from"`
	FromName           string `json:"from_name"`
	SubjectPrefix      string `json:"subject_prefix,omitempty"`
}

// StorageResponse is StorageConfig with masked secrets
type StorageResponse struct {
	Type        string `json:"type"`
	MaxSizeMB   int64  `json:"max_size_mb"`
	LocalPath   string `json:"local_path,omitempty"`
	S3Endpoint  string `json:"s3_endpoint,omitempty"`
	S3Bucket    string `json:"s3_bucket,omitempty"`
	S3AccessKey string `json:"s3_access_key,omitempty"`
	S3SecretKey string `json:"s3_secret_key,omitempty"`
	S3Region    string `json:"s3_region,omitempty"`
	S3UseSSL    bool   `json:"s3_use_ssl"`
}

// HandleGetSettings handles GET /api/v1/admin/settings
func (h *SettingsHandler) HandleGetSettings(w http.ResponseWriter, r *http.Request) {
	cfg := h.configService.GetConfig()

	// Build response with masked secrets
	response := SettingsResponse{
		General: cfg.General,
		OIDC: OIDCResponse{
			Enabled:       cfg.OIDC.Enabled,
			Provider:      cfg.OIDC.Provider,
			ClientID:      cfg.OIDC.ClientID,
			ClientSecret:  maskSecret(cfg.OIDC.ClientSecret),
			AuthURL:       cfg.OIDC.AuthURL,
			TokenURL:      cfg.OIDC.TokenURL,
			UserInfoURL:   cfg.OIDC.UserInfoURL,
			LogoutURL:     cfg.OIDC.LogoutURL,
			Scopes:        cfg.OIDC.Scopes,
			AllowedDomain: cfg.OIDC.AllowedDomain,
			AutoLogin:     cfg.OIDC.AutoLogin,
		},
		MagicLink: cfg.MagicLink,
		SMTP: SMTPResponse{
			Host:               cfg.SMTP.Host,
			Port:               cfg.SMTP.Port,
			Username:           cfg.SMTP.Username,
			Password:           maskSecret(cfg.SMTP.Password),
			TLS:                cfg.SMTP.TLS,
			StartTLS:           cfg.SMTP.StartTLS,
			InsecureSkipVerify: cfg.SMTP.InsecureSkipVerify,
			Timeout:            cfg.SMTP.Timeout,
			From:               cfg.SMTP.From,
			FromName:           cfg.SMTP.FromName,
			SubjectPrefix:      cfg.SMTP.SubjectPrefix,
		},
		Storage: StorageResponse{
			Type:        cfg.Storage.Type,
			MaxSizeMB:   cfg.Storage.MaxSizeMB,
			LocalPath:   cfg.Storage.LocalPath,
			S3Endpoint:  cfg.Storage.S3Endpoint,
			S3Bucket:    cfg.Storage.S3Bucket,
			S3AccessKey: cfg.Storage.S3AccessKey,
			S3SecretKey: maskSecret(cfg.Storage.S3SecretKey),
			S3Region:    cfg.Storage.S3Region,
			S3UseSSL:    cfg.Storage.S3UseSSL,
		},
		UpdatedAt: cfg.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	shared.WriteJSON(w, http.StatusOK, response)
}

// HandleUpdateSection handles PUT /api/v1/admin/settings/{section}
func (h *SettingsHandler) HandleUpdateSection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	section := chi.URLParam(r, "section")

	category, err := parseCategory(section)
	if err != nil {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Invalid section: "+section, nil)
		return
	}

	user, ok := shared.GetUserFromContext(ctx)
	if !ok || user == nil {
		shared.WriteUnauthorized(w, "Authentication required")
		return
	}

	var input json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Invalid JSON: "+err.Error(), nil)
		return
	}

	if err := h.configService.UpdateSection(ctx, category, input, user.Email); err != nil {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, err.Error(), nil)
		return
	}

	shared.WriteJSON(w, http.StatusOK, map[string]string{"message": "Configuration updated"})
}

// HandleTestConnection handles POST /api/v1/admin/settings/test/{type}
func (h *SettingsHandler) HandleTestConnection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	testType := chi.URLParam(r, "type")

	var err error
	switch testType {
	case "smtp":
		var cfg models.SMTPConfig
		if decErr := json.NewDecoder(r.Body).Decode(&cfg); decErr != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Invalid SMTP config: "+decErr.Error(), nil)
			return
		}
		err = h.configService.TestSMTP(ctx, cfg)

	case "s3":
		var cfg models.StorageConfig
		if decErr := json.NewDecoder(r.Body).Decode(&cfg); decErr != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Invalid storage config: "+decErr.Error(), nil)
			return
		}
		err = h.configService.TestS3(ctx, cfg)

	case "oidc":
		var cfg models.OIDCConfig
		if decErr := json.NewDecoder(r.Body).Decode(&cfg); decErr != nil {
			shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Invalid OIDC config: "+decErr.Error(), nil)
			return
		}
		err = h.configService.TestOIDC(ctx, cfg)

	default:
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Unknown test type: "+testType, nil)
		return
	}

	if err != nil {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, err.Error(), nil)
		return
	}

	shared.WriteJSON(w, http.StatusOK, map[string]string{"message": "Connection successful"})
}

// HandleResetFromENV handles POST /api/v1/admin/settings/reset
func (h *SettingsHandler) HandleResetFromENV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user, ok := shared.GetUserFromContext(ctx)
	if !ok || user == nil {
		shared.WriteUnauthorized(w, "Authentication required")
		return
	}

	if err := h.configService.ResetFromENV(ctx, user.Email); err != nil {
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Reset failed: "+err.Error(), nil)
		return
	}

	shared.WriteJSON(w, http.StatusOK, map[string]string{"message": "Configuration reset from environment"})
}

// parseCategory converts a string to a ConfigCategory
func parseCategory(s string) (models.ConfigCategory, error) {
	category := models.ConfigCategory(s)
	if !category.IsValid() {
		return "", errors.New("invalid category")
	}
	return category, nil
}

// maskSecret returns the mask if the secret is set, empty string otherwise
func maskSecret(secret string) string {
	if secret == "" {
		return ""
	}
	return models.SecretMask
}
