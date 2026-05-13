// SPDX-License-Identifier: AGPL-3.0-or-later
package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/gorilla/securecookie"
	"github.com/kolapsis/ackify/backend/pkg/logger"
)

type Config struct {
	App       AppConfig
	Server    ServerConfig
	Database  DatabaseConfig
	Checksum  ChecksumConfig
	Auth      AuthConfig
	OAuth     OAuthConfig
	Mail      MailConfig
	Storage   StorageConfig
	Logger    LoggerConfig
	Telemetry TelemetryConfig
}

type TelemetryConfig struct {
	Enabled bool
	DataDir string // Directory to store identity file (must be persisted on host)
}

type StorageConfig struct {
	Type      string // "local", "s3", or "" (disabled)
	MaxSizeMB int64  // Max file size in MB (default: 50)

	// Local storage
	LocalPath string // Path for local storage (default: /data/documents)

	// S3-compatible storage
	S3Endpoint  string
	S3Bucket    string
	S3AccessKey string
	S3SecretKey string
	S3Region    string
	S3UseSSL    bool
}

func (s *StorageConfig) IsEnabled() bool {
	return s.Type == "local" || s.Type == "s3"
}

type AuthConfig struct {
	OAuthEnabled            bool
	MagicLinkEnabled        bool
	MagicLinkRateLimitEmail int // Max requests per email per window (default: 3)
	MagicLinkRateLimitIP    int // Max requests per IP per window (default: 10)

	// DisableAuthMethodCheck disables cross-category auth method validation in the
	// config service. Set by extension modes (e.g. SaaS) that manage auth at a
	// higher layer and do not store auth method state in the CE config tables.
	DisableAuthMethodCheck bool
}

type AppConfig struct {
	BaseURL              string
	Organisation         string
	SecureCookies        bool
	AdminEmails          []string
	OnlyAdminCanCreate   bool
	OrganisationDomain   string   // If set, only users with this email domain can create documents
	AllowedDomains       []string // Whitelist of allowed domains for document URLs (supports wildcards like *.company.com)
	AllowedRedirectHosts []string // Optional allowlist of external hosts accepted as post-auth redirect targets
	SMTPEnabled          bool     // True if SMTP is configured (for email reminders)
	AuthRateLimit        int      // Global auth rate limit (requests per minute), default: 5
	DocumentRateLimit    int      // Document creation rate limit (requests per minute), default: 10
	GeneralRateLimit     int      // General API rate limit (requests per minute), default: 100
	ImportMaxSigners     int      // Maximum signers per CSV import, default: 500
}

type DatabaseConfig struct {
	DSN string
}

type OAuthConfig struct {
	ClientID      string
	ClientSecret  string
	AuthURL       string
	TokenURL      string
	UserInfoURL   string
	LogoutURL     string
	Scopes        []string
	AllowedDomain string
	CookieSecret  []byte
	AutoLogin     bool
}

type ServerConfig struct {
	ListenAddr string
}

type MailConfig struct {
	Host               string
	Port               int
	Username           string
	Password           string
	TLS                bool
	StartTLS           bool
	InsecureSkipVerify bool
	Timeout            string
	From               string
	FromName           string
	SubjectPrefix      string
	TemplateDir        string
	DefaultLocale      string
}

type ChecksumConfig struct {
	MaxBytes           int64
	TimeoutMs          int
	MaxRedirects       int
	AllowedContentType []string
	SkipSSRFCheck      bool // For testing only - DO NOT use in production
	InsecureSkipVerify bool // For testing only - DO NOT use in production
}

type LoggerConfig struct {
	Level  string
	Format string // "classic" or "json"
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	config := &Config{}

	baseURL, err := getRequiredEnv("ACKIFY_BASE_URL")
	if err != nil {
		return nil, err
	}
	config.App.BaseURL = baseURL

	organisation, err := getRequiredEnv("ACKIFY_ORGANISATION")
	if err != nil {
		return nil, err
	}
	config.App.Organisation = organisation
	config.App.SecureCookies = strings.HasPrefix(strings.ToLower(baseURL), "https://")

	dsn, err := getRequiredEnv("ACKIFY_DB_DSN")
	if err != nil {
		return nil, err
	}
	config.Database.DSN = dsn

	// OAuth configuration - now OPTIONAL
	config.OAuth.ClientID = getEnv("ACKIFY_OAUTH_CLIENT_ID", "")
	config.OAuth.ClientSecret = getEnv("ACKIFY_OAUTH_CLIENT_SECRET", "")
	config.OAuth.AllowedDomain = getEnv("ACKIFY_OAUTH_ALLOWED_DOMAIN", "")
	config.OAuth.AutoLogin = getEnvBool("ACKIFY_OAUTH_AUTO_LOGIN", false)

	// Auto-detect OAuth enabled: true if ClientID and ClientSecret are provided
	oauthConfigured := config.OAuth.ClientID != "" && config.OAuth.ClientSecret != ""

	// Allow manual override via environment variable
	if oauthEnabledStr := getEnv("ACKIFY_AUTH_OAUTH_ENABLED", ""); oauthEnabledStr != "" {
		config.Auth.OAuthEnabled = getEnvBool("ACKIFY_AUTH_OAUTH_ENABLED", false)
	} else {
		config.Auth.OAuthEnabled = oauthConfigured
	}

	// Only configure OAuth URLs if OAuth is enabled
	if config.Auth.OAuthEnabled {
		provider := strings.ToLower(getEnv("ACKIFY_OAUTH_PROVIDER", ""))
		switch provider {
		case "google":
			config.OAuth.AuthURL = "https://accounts.google.com/o/oauth2/auth"
			config.OAuth.TokenURL = "https://oauth2.googleapis.com/token"
			config.OAuth.UserInfoURL = "https://openidconnect.googleapis.com/v1/userinfo"
			config.OAuth.LogoutURL = "https://accounts.google.com/Logout"
			config.OAuth.Scopes = []string{"openid", "email", "profile"}
		case "github":
			config.OAuth.AuthURL = "https://github.com/login/oauth/authorize"
			config.OAuth.TokenURL = "https://github.com/login/oauth/access_token"
			config.OAuth.UserInfoURL = "https://api.github.com/user"
			config.OAuth.LogoutURL = "https://github.com/logout"
			config.OAuth.Scopes = []string{"user:email", "read:user"}
		case "gitlab":
			gitlabURL := getEnv("ACKIFY_OAUTH_GITLAB_URL", "https://gitlab.com")
			config.OAuth.AuthURL = fmt.Sprintf("%s/oauth/authorize", gitlabURL)
			config.OAuth.TokenURL = fmt.Sprintf("%s/oauth/token", gitlabURL)
			config.OAuth.UserInfoURL = fmt.Sprintf("%s/api/v4/user", gitlabURL)
			config.OAuth.LogoutURL = fmt.Sprintf("%s/users/sign_out", gitlabURL)
			config.OAuth.Scopes = []string{"read_user", "profile"}
		default:
			// Custom OAuth provider - require URLs
			authURL, err := getRequiredEnv("ACKIFY_OAUTH_AUTH_URL")
			if err != nil {
				return nil, fmt.Errorf("OAuth enabled with custom provider: %w", err)
			}
			tokenURL, err := getRequiredEnv("ACKIFY_OAUTH_TOKEN_URL")
			if err != nil {
				return nil, fmt.Errorf("OAuth enabled with custom provider: %w", err)
			}
			userInfoURL, err := getRequiredEnv("ACKIFY_OAUTH_USERINFO_URL")
			if err != nil {
				return nil, fmt.Errorf("OAuth enabled with custom provider: %w", err)
			}

			config.OAuth.AuthURL = authURL
			config.OAuth.TokenURL = tokenURL
			config.OAuth.UserInfoURL = userInfoURL
			config.OAuth.LogoutURL = getEnv("ACKIFY_OAUTH_LOGOUT_URL", "")
			scopesStr := getEnv("ACKIFY_OAUTH_SCOPES", "openid,email,profile")
			config.OAuth.Scopes = strings.Split(scopesStr, ",")
		}
	}

	cookieSecret, err := parseCookieSecret()
	if err != nil {
		return nil, fmt.Errorf("failed to parse cookie secret: %w", err)
	}
	config.OAuth.CookieSecret = cookieSecret

	config.Server.ListenAddr = getEnv("ACKIFY_LISTEN_ADDR", ":8080")

	config.Logger.Level = getEnv("ACKIFY_LOG_LEVEL", "info")
	config.Logger.Format = getEnv("ACKIFY_LOG_FORMAT", "classic")

	// Parse admin emails
	adminEmailsStr := getEnv("ACKIFY_ADMIN_EMAILS", "")
	if adminEmailsStr != "" {
		emails := strings.Split(strings.ToLower(adminEmailsStr), ",")
		for _, email := range emails {
			trimmed := strings.TrimSpace(email)
			if trimmed != "" {
				config.App.AdminEmails = append(config.App.AdminEmails, trimmed)
			}
		}
	}

	// Parse admin-only document creation flag
	config.App.OnlyAdminCanCreate = getEnvBool("ACKIFY_ONLY_ADMIN_CAN_CREATE", false)

	// Parse organisation domain for document creation restriction
	config.App.OrganisationDomain = strings.TrimSpace(getEnv("ACKIFY_ORGANISATION_DOMAIN", ""))

	// Parse allowed domains whitelist for document URLs
	allowedDomainsStr := getEnv("ACKIFY_ALLOWED_DOMAINS", "")
	if allowedDomainsStr != "" {
		for _, domain := range strings.Split(allowedDomainsStr, ",") {
			trimmed := strings.TrimSpace(domain)
			if trimmed != "" {
				config.App.AllowedDomains = append(config.App.AllowedDomains, trimmed)
			}
		}
	}

	// Parse optional allowlist of external hosts accepted as post-auth redirect targets.
	// Format: comma-separated hostnames (with optional port), e.g. "partner.com,app.example.com:8443"
	allowedRedirectHostsStr := getEnv("ACKIFY_ALLOWED_REDIRECT_HOSTS", "")
	if allowedRedirectHostsStr != "" {
		for _, h := range strings.Split(allowedRedirectHostsStr, ",") {
			trimmed := strings.TrimSpace(h)
			if trimmed != "" {
				config.App.AllowedRedirectHosts = append(config.App.AllowedRedirectHosts, trimmed)
			}
		}
	}

	// Parse mail config (optional, service disabled if MAIL_HOST not set)
	mailHost := getEnv("ACKIFY_MAIL_HOST", "")
	if mailHost != "" {
		config.Mail.Host = mailHost
		config.Mail.Port = getEnvInt("ACKIFY_MAIL_PORT", 587)
		config.Mail.Username = getEnv("ACKIFY_MAIL_USERNAME", "")
		config.Mail.Password = getEnv("ACKIFY_MAIL_PASSWORD", "")
		config.Mail.TLS = getEnvBool("ACKIFY_MAIL_TLS", true)
		config.Mail.StartTLS = getEnvBool("ACKIFY_MAIL_STARTTLS", true)
		config.Mail.InsecureSkipVerify = getEnvBool("ACKIFY_MAIL_INSECURE_SKIP_VERIFY", false)
		config.Mail.Timeout = getEnv("ACKIFY_MAIL_TIMEOUT", "10s")
		config.Mail.From = getEnv("ACKIFY_MAIL_FROM", "")
		config.Mail.FromName = getEnv("ACKIFY_MAIL_FROM_NAME", config.App.Organisation)
		config.Mail.SubjectPrefix = getEnv("ACKIFY_MAIL_SUBJECT_PREFIX", "")
		config.Mail.TemplateDir = getEnv("ACKIFY_MAIL_TEMPLATE_DIR", "templates/emails")
		config.Mail.DefaultLocale = getEnv("ACKIFY_MAIL_DEFAULT_LOCALE", "en")
	}

	// Parse checksum config (automatic checksum computation for remote URLs)
	config.Checksum.MaxBytes = getEnvInt64("ACKIFY_CHECKSUM_MAX_BYTES", 10*1024*1024) // 10 MB default
	config.Checksum.TimeoutMs = getEnvInt("ACKIFY_CHECKSUM_TIMEOUT_MS", 5000)         // 5 seconds default
	config.Checksum.MaxRedirects = getEnvInt("ACKIFY_CHECKSUM_MAX_REDIRECTS", 3)

	// Parse allowed content types
	allowedTypesStr := getEnv("ACKIFY_CHECKSUM_ALLOWED_TYPES", "application/pdf,image/*,application/msword,application/vnd.openxmlformats-officedocument.wordprocessingml.document,application/vnd.ms-excel,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,application/vnd.oasis.opendocument.*")
	if allowedTypesStr != "" {
		types := strings.Split(allowedTypesStr, ",")
		for _, typ := range types {
			trimmed := strings.TrimSpace(typ)
			if trimmed != "" {
				config.Checksum.AllowedContentType = append(config.Checksum.AllowedContentType, trimmed)
			}
		}
	}

	smtpConfigured := mailHost != ""
	config.App.SMTPEnabled = smtpConfigured
	config.Auth.MagicLinkEnabled = getEnvBool("ACKIFY_AUTH_MAGICLINK_ENABLED", false) && smtpConfigured

	// Magic Link rate limiting configuration
	config.Auth.MagicLinkRateLimitEmail = getEnvInt("ACKIFY_AUTH_MAGICLINK_RATE_LIMIT_EMAIL", 3)
	config.Auth.MagicLinkRateLimitIP = getEnvInt("ACKIFY_AUTH_MAGICLINK_RATE_LIMIT_IP", 10)

	// Global API rate limiting configuration (for e2e testing)
	config.App.AuthRateLimit = getEnvInt("ACKIFY_AUTH_RATE_LIMIT", 5)
	config.App.DocumentRateLimit = getEnvInt("ACKIFY_DOCUMENT_RATE_LIMIT", 10)
	config.App.GeneralRateLimit = getEnvInt("ACKIFY_GENERAL_RATE_LIMIT", 100)

	// CSV import configuration
	config.App.ImportMaxSigners = getEnvInt("ACKIFY_IMPORT_MAX_SIGNERS", 500)

	// Storage configuration (optional, disabled if ACKIFY_STORAGE_TYPE not set)
	storageType := strings.ToLower(getEnv("ACKIFY_STORAGE_TYPE", ""))
	if storageType == "local" || storageType == "s3" {
		config.Storage.Type = storageType
		config.Storage.MaxSizeMB = getEnvInt64("ACKIFY_STORAGE_MAX_SIZE_MB", 50)

		if storageType == "local" {
			config.Storage.LocalPath = getEnv("ACKIFY_STORAGE_LOCAL_PATH", "/data/documents")
		} else if storageType == "s3" {
			config.Storage.S3Endpoint = getEnv("ACKIFY_STORAGE_S3_ENDPOINT", "")
			config.Storage.S3Bucket = getEnv("ACKIFY_STORAGE_S3_BUCKET", "")
			config.Storage.S3AccessKey = getEnv("ACKIFY_STORAGE_S3_ACCESS_KEY", "")
			config.Storage.S3SecretKey = getEnv("ACKIFY_STORAGE_S3_SECRET_KEY", "")
			config.Storage.S3Region = getEnv("ACKIFY_STORAGE_S3_REGION", "us-east-1")
			config.Storage.S3UseSSL = getEnvBool("ACKIFY_STORAGE_S3_USE_SSL", true)

			// Validate S3 configuration
			if config.Storage.S3Bucket == "" {
				return nil, fmt.Errorf("S3 storage enabled but ACKIFY_STORAGE_S3_BUCKET not set")
			}
		}
	}

	// Telemetry configuration
	config.Telemetry.Enabled = getEnv("ACKIFY_TELEMETRY", "false") != "false" && getEnv("DO_NOT_TRACK", "") != "1"
	config.Telemetry.DataDir = getEnv("ACKIFY_TELEMETRY_DATA_DIR", "/data/telemetry")

	// Validation: At least one authentication method must be enabled
	if !config.Auth.OAuthEnabled && !config.Auth.MagicLinkEnabled {
		return nil, fmt.Errorf("at least one authentication method must be enabled: set ACKIFY_OAUTH_CLIENT_ID/CLIENT_SECRET for OAuth or ACKIFY_MAIL_HOST for MagicLink")
	}

	return config, nil
}

func getRequiredEnv(key string) (string, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return "", fmt.Errorf("missing required environment variable: %s", key)
	}
	return value, nil
}

func getEnv(key, defaultValue string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	return value
}

func parseCookieSecret() ([]byte, error) {
	raw := os.Getenv("ACKIFY_OAUTH_COOKIE_SECRET")
	if raw == "" {
		secret := securecookie.GenerateRandomKey(32)
		logger.Logger.Warn("OAuth cookie secret not set, sessions will reset on restart")
		return secret, nil
	}

	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil && (len(decoded) == 32 || len(decoded) == 64) {
		return decoded, nil
	}

	return []byte(raw), nil
}

func getEnvInt(key string, defaultValue int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	var result int
	if _, err := fmt.Sscanf(value, "%d", &result); err == nil {
		return result
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	return strings.ToLower(value) == "true" || value == "1"
}

func getEnvInt64(key string, defaultValue int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	var result int64
	if _, err := fmt.Sscanf(value, "%d", &result); err == nil {
		return result
	}
	return defaultValue
}
