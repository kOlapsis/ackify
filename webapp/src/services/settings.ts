// SPDX-License-Identifier: AGPL-3.0-or-later
import http, { type ApiResponse } from './http'

// ============================================================================
// TYPES
// ============================================================================

export interface GeneralConfig {
  organisation: string
  organisation_domain: string
  only_admin_can_create: boolean
  allowed_domains: string[]
}

export interface OIDCConfig {
  enabled: boolean
  provider: 'google' | 'github' | 'gitlab' | 'custom' | ''
  client_id: string
  client_secret: string // "********" if set, empty if not
  auth_url?: string
  token_url?: string
  userinfo_url?: string
  logout_url?: string
  scopes?: string[]
  allowed_domain?: string
  auto_login: boolean
}

export interface MagicLinkConfig {
  enabled: boolean
}

export interface SMTPConfig {
  host: string
  port: number
  username: string
  password: string // "********" if set, empty if not
  tls: boolean
  starttls: boolean
  insecure_skip_verify: boolean
  timeout: string
  from: string
  from_name: string
  subject_prefix?: string
}

export interface StorageConfig {
  type: '' | 'local' | 's3'
  max_size_mb: number
  local_path?: string
  s3_endpoint?: string
  s3_bucket?: string
  s3_access_key?: string
  s3_secret_key?: string // "********" if set, empty if not
  s3_region?: string
  s3_use_ssl: boolean
}

export interface SettingsResponse {
  general: GeneralConfig
  oidc: OIDCConfig
  magiclink: MagicLinkConfig
  smtp: SMTPConfig
  storage: StorageConfig
  updated_at: string
}

export type ConfigSection =
  | 'general'
  | 'oidc'
  | 'magiclink'
  | 'smtp'
  | 'storage'

// ============================================================================
// API FUNCTIONS
// ============================================================================

/**
 * Get all settings (secrets are masked with "********")
 */
export async function getSettings(): Promise<ApiResponse<SettingsResponse>> {
  const response = await http.get('/admin/settings')
  return response.data
}

/**
 * Update a specific settings section
 * @param section - The section to update (general, oidc, magiclink, smtp, storage)
 * @param config - The new configuration for the section
 */
export async function updateSection<T>(
  section: ConfigSection,
  config: T
): Promise<ApiResponse<{ message: string }>> {
  const response = await http.put(`/admin/settings/${section}`, config)
  return response.data
}

/**
 * Test a connection (SMTP, S3, or OIDC)
 * @param type - The type of connection to test (smtp, s3, oidc)
 * @param config - The configuration to test
 */
export async function testConnection(
  type: 'smtp' | 's3' | 'oidc',
  config: SMTPConfig | StorageConfig | OIDCConfig
): Promise<ApiResponse<{ message: string }>> {
  const response = await http.post(`/admin/settings/test/${type}`, config)
  return response.data
}

/**
 * Reset all settings from environment variables
 */
export async function resetFromENV(): Promise<ApiResponse<{ message: string }>> {
  const response = await http.post('/admin/settings/reset', {})
  return response.data
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

/**
 * Check if a value is a masked secret
 */
export function isSecretMasked(value: string): boolean {
  return value === '********'
}

/**
 * Check if SMTP is configured (has host and from)
 */
export function isSMTPConfigured(config: SMTPConfig): boolean {
  return config.host !== '' && config.from !== ''
}

/**
 * Check if storage is enabled
 */
export function isStorageEnabled(config: StorageConfig): boolean {
  return config.type === 'local' || config.type === 's3'
}

/**
 * Get default values for a new SMTP config
 */
export function getDefaultSMTPConfig(): SMTPConfig {
  return {
    host: '',
    port: 587,
    username: '',
    password: '',
    tls: false,
    starttls: true,
    insecure_skip_verify: false,
    timeout: '10s',
    from: '',
    from_name: '',
    subject_prefix: ''
  }
}

/**
 * Get default values for a new storage config
 */
export function getDefaultStorageConfig(): StorageConfig {
  return {
    type: '',
    max_size_mb: 50,
    local_path: '/data/documents',
    s3_endpoint: '',
    s3_bucket: '',
    s3_access_key: '',
    s3_secret_key: '',
    s3_region: 'us-east-1',
    s3_use_ssl: true
  }
}

/**
 * Get default values for a new OIDC config
 */
export function getDefaultOIDCConfig(): OIDCConfig {
  return {
    enabled: false,
    provider: '',
    client_id: '',
    client_secret: '',
    auth_url: '',
    token_url: '',
    userinfo_url: '',
    logout_url: '',
    scopes: ['openid', 'email', 'profile'],
    allowed_domain: '',
    auto_login: false
  }
}

/**
 * Get OIDC provider URLs for well-known providers
 */
export function getOIDCProviderURLs(provider: string): Partial<OIDCConfig> {
  switch (provider) {
    case 'google':
      return {
        auth_url: 'https://accounts.google.com/o/oauth2/auth',
        token_url: 'https://oauth2.googleapis.com/token',
        userinfo_url: 'https://openidconnect.googleapis.com/v1/userinfo',
        logout_url: 'https://accounts.google.com/Logout',
        scopes: ['openid', 'email', 'profile']
      }
    case 'github':
      return {
        auth_url: 'https://github.com/login/oauth/authorize',
        token_url: 'https://github.com/login/oauth/access_token',
        userinfo_url: 'https://api.github.com/user',
        logout_url: 'https://github.com/logout',
        scopes: ['user:email', 'read:user']
      }
    case 'gitlab':
      return {
        auth_url: 'https://gitlab.com/oauth/authorize',
        token_url: 'https://gitlab.com/oauth/token',
        userinfo_url: 'https://gitlab.com/api/v4/user',
        logout_url: 'https://gitlab.com/users/sign_out',
        scopes: ['read_user', 'profile']
      }
    default:
      return {}
  }
}
