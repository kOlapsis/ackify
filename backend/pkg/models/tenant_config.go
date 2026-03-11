// SPDX-License-Identifier: AGPL-3.0-or-later
package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ConfigCategory represents the category of configuration
type ConfigCategory string

const (
	ConfigCategoryGeneral   ConfigCategory = "general"
	ConfigCategoryOIDC      ConfigCategory = "oidc"
	ConfigCategoryMagicLink ConfigCategory = "magiclink"
	ConfigCategorySMTP      ConfigCategory = "smtp"
	ConfigCategoryStorage   ConfigCategory = "storage"
)

// AllConfigCategories returns all valid configuration categories
func AllConfigCategories() []ConfigCategory {
	return []ConfigCategory{
		ConfigCategoryGeneral,
		ConfigCategoryOIDC,
		ConfigCategoryMagicLink,
		ConfigCategorySMTP,
		ConfigCategoryStorage,
	}
}

// IsValid checks if the category is valid
func (c ConfigCategory) IsValid() bool {
	switch c {
	case ConfigCategoryGeneral, ConfigCategoryOIDC, ConfigCategoryMagicLink,
		ConfigCategorySMTP, ConfigCategoryStorage:
		return true
	}
	return false
}

// TenantConfig represents a configuration section stored in the database
type TenantConfig struct {
	ID               int64           `json:"id" db:"id"`
	TenantID         uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	Category         ConfigCategory  `json:"category" db:"category"`
	Config           json.RawMessage `json:"config" db:"config"`
	SecretsEncrypted []byte          `json:"-" db:"secrets_encrypted"`
	Version          int             `json:"version" db:"version"`
	CreatedAt        time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at" db:"updated_at"`
	UpdatedBy        *string         `json:"updated_by,omitempty" db:"updated_by"`
}

// GeneralConfig holds general application settings
type GeneralConfig struct {
	Organisation       string   `json:"organisation"`
	OnlyAdminCanCreate bool     `json:"only_admin_can_create"`
	OrganisationDomain string   `json:"organisation_domain"`
	AllowedDomains     []string `json:"allowed_domains"`
}

// OIDCConfig holds OIDC/OAuth2 authentication settings
type OIDCConfig struct {
	Enabled       bool     `json:"enabled"`
	Provider      string   `json:"provider"` // google, github, gitlab, custom
	ClientID      string   `json:"client_id"`
	ClientSecret  string   `json:"client_secret,omitempty"`
	AuthURL       string   `json:"auth_url,omitempty"`
	TokenURL      string   `json:"token_url,omitempty"`
	UserInfoURL   string   `json:"userinfo_url,omitempty"`
	LogoutURL     string   `json:"logout_url,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`
	AllowedDomain string   `json:"allowed_domain,omitempty"`
	AutoLogin     bool     `json:"auto_login"`
}

// OIDCSecrets holds the secret fields for OIDC config
type OIDCSecrets struct {
	ClientSecret string `json:"client_secret,omitempty"`
}

// MagicLinkConfig holds MagicLink authentication settings
type MagicLinkConfig struct {
	Enabled bool `json:"enabled"`
}

// SMTPConfig holds SMTP email settings
type SMTPConfig struct {
	Host               string `json:"host"`
	Port               int    `json:"port"`
	Username           string `json:"username"`
	Password           string `json:"password,omitempty"`
	TLS                bool   `json:"tls"`
	StartTLS           bool   `json:"starttls"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify"`
	Timeout            string `json:"timeout"`
	From               string `json:"from"`
	FromName           string `json:"from_name"`
	SubjectPrefix      string `json:"subject_prefix,omitempty"`
}

// SMTPSecrets holds the secret fields for SMTP config
type SMTPSecrets struct {
	Password string `json:"password,omitempty"`
}

// IsConfigured returns true if SMTP is properly configured
func (c *SMTPConfig) IsConfigured() bool {
	return c.Host != "" && c.From != ""
}

// StorageConfig holds document storage settings
type StorageConfig struct {
	Type        string `json:"type"` // "", "local", "s3"
	MaxSizeMB   int64  `json:"max_size_mb"`
	LocalPath   string `json:"local_path,omitempty"`
	S3Endpoint  string `json:"s3_endpoint,omitempty"`
	S3Bucket    string `json:"s3_bucket,omitempty"`
	S3AccessKey string `json:"s3_access_key,omitempty"`
	S3SecretKey string `json:"s3_secret_key,omitempty"`
	S3Region    string `json:"s3_region,omitempty"`
	S3UseSSL    bool   `json:"s3_use_ssl"`
}

// StorageSecrets holds the secret fields for Storage config
type StorageSecrets struct {
	S3SecretKey string `json:"s3_secret_key,omitempty"`
}

// IsEnabled returns true if storage is enabled
func (c *StorageConfig) IsEnabled() bool {
	return c.Type == "local" || c.Type == "s3"
}

// MutableConfig combines all mutable configuration sections
type MutableConfig struct {
	General   GeneralConfig   `json:"general"`
	OIDC      OIDCConfig      `json:"oidc"`
	MagicLink MagicLinkConfig `json:"magiclink"`
	SMTP      SMTPConfig      `json:"smtp"`
	Storage   StorageConfig   `json:"storage"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// ConfigSecrets holds all encrypted secrets
type ConfigSecrets struct {
	OIDCClientSecret string `json:"oidc_client_secret,omitempty"`
	SMTPPassword     string `json:"smtp_password,omitempty"`
	S3SecretKey      string `json:"s3_secret_key,omitempty"`
}

// HasAtLeastOneAuthMethod validates that at least one auth method is enabled
func (c *MutableConfig) HasAtLeastOneAuthMethod() bool {
	return c.OIDC.Enabled || c.MagicLink.Enabled
}

// MagicLinkRequiresSMTP validates that MagicLink has SMTP configured
func (c *MutableConfig) MagicLinkRequiresSMTP() bool {
	if !c.MagicLink.Enabled {
		return true
	}
	return c.SMTP.IsConfigured()
}

// SecretMask is the value returned for masked secrets
const SecretMask = "********"

// MaskSecrets returns a copy of MutableConfig with secrets masked
func (c *MutableConfig) MaskSecrets() *MutableConfig {
	masked := *c

	if masked.OIDC.ClientSecret != "" {
		masked.OIDC.ClientSecret = SecretMask
	}
	if masked.SMTP.Password != "" {
		masked.SMTP.Password = SecretMask
	}
	if masked.Storage.S3SecretKey != "" {
		masked.Storage.S3SecretKey = SecretMask
	}

	return &masked
}

// IsSecretMasked checks if a value is the secret mask
func IsSecretMasked(value string) bool {
	return value == SecretMask
}
