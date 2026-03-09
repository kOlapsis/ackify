# Ackify CE - Installation Guide

This directory contains the installation scripts and configuration files for Ackify Community Edition.

## Quick Start

### Interactive Installation (Recommended)

The interactive installation script will guide you through the entire configuration process:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/kolapsis/ackify/main/install/install.sh)
```

The script will prompt you for:

1. **Basic Configuration**
   - Application Base URL (e.g., `https://ackify.example.com`)
   - Organization Name

2. **OAuth2 Authentication** (Optional)
   - Enable/disable OAuth
   - OAuth Provider (Google, GitHub, GitLab, or custom)
   - Client ID and Client Secret
   - Email domain restriction (optional)
   - Auto-login configuration (optional)

3. **SMTP Configuration** (Optional)
   - Enable/disable SMTP for email notifications
   - SMTP server settings (host, port, credentials)
   - Email sender configuration
   - TLS/STARTTLS settings

4. **MagicLink Authentication** (Optional)
   - Auto-enabled when SMTP is configured
   - Option to disable if needed

5. **Admin Users** (Required)
   - Configure at least one admin email address
   - Admins have access to document management and reminder features

6. **Document Storage** (Optional)
   - None (URL-only mode)
   - Local filesystem storage
   - S3-compatible storage (AWS, MinIO, Wasabi, etc.)

The script will automatically:
- Download the necessary configuration files (docker compose & .env.example)
- Generate secure secrets (cookie secret, Ed25519 key, database password)
- Create a ready-to-use `.env` file
- Validate that at least one authentication method is enabled

### After Installation

1. **Review the configuration:**
   ```bash
   cd ackify-ce
   cat .env
   ```

2. **Start Ackify:**
   ```bash
   docker compose up -d
   ```

3. **Check logs:**
   ```bash
   docker compose logs -f ackify-ce
   ```

4. **Access the application:**
   Open your browser and navigate to the configured base URL

5. **Verify health:**
   ```bash
   curl http://localhost:8080/health
   ```

## Manual Installation

If you prefer to configure manually:

1. **Download configuration files:**
   ```bash
   mkdir ackify-ce && cd ackify-ce
   curl -fsSL https://raw.githubusercontent.com/kolapsis/ackify/main/install/compose.yml -o compose.yml
   curl -fsSL https://raw.githubusercontent.com/kolapsis/ackify/main/install/.env.example -o .env
   ```

2. **Generate secrets:**
   ```bash
   # Cookie secret (for session encryption)
   openssl rand -base64 32

   # Ed25519 private key (for signatures)
   openssl rand 64 | base64 -w 0

   # Database password
   openssl rand -base64 24
   ```

3. **Edit `.env` file:**
   - Configure your application URL and organization name
   - Set up at least one authentication method (OAuth or MagicLink)
   - Add the generated secrets
   - Configure at least one admin user (required)
   - Configure optional features (SMTP for email reminders and MagicLink)

4. **Start the application:**
   ```bash
   docker compose up -d
   ```

## Authentication Methods

Ackify CE supports two authentication methods. **At least one must be enabled.**

### OAuth2 Authentication

OAuth allows users to sign in using existing accounts from popular providers.

**Supported Providers:**
- Google
- GitHub
- GitLab (including self-hosted)
- Custom OAuth2 provider

**Required Variables:**
```env
OAUTH_PROVIDER=google
OAUTH_CLIENT_ID=your_client_id
OAUTH_CLIENT_SECRET=your_client_secret
```

**Setup Links:**
- [Google OAuth Setup](https://console.cloud.google.com/)
- [GitHub OAuth Setup](https://github.com/settings/developers)
- [GitLab OAuth Setup](https://gitlab.com/-/profile/applications)

### MagicLink Authentication

MagicLink provides passwordless authentication via email. Users receive a secure link to sign in.

**Requirements:**
- SMTP server configuration (MAIL_HOST must be set)

**When to use:**
- Simplified user experience (no password management)
- Internal applications where email domain is trusted
- Combination with OAuth for flexible authentication

## Document Storage

Ackify CE can optionally store uploaded documents, allowing users to upload files directly instead of providing URLs.

### Storage Options

**None (Default):**
- Users must provide document URLs
- No local storage required
- Simplest configuration

**Local Storage:**
- Documents stored on the server filesystem
- Uses Docker volume for persistence
- Best for single-server deployments

```env
ACKIFY_STORAGE_TYPE=local
ACKIFY_STORAGE_LOCAL_PATH=/data/documents
ACKIFY_STORAGE_MAX_SIZE_MB=50
```

**S3-Compatible Storage:**
- Works with AWS S3, MinIO, Wasabi, DigitalOcean Spaces, etc.
- Best for scalable/distributed deployments
- Supports any S3-compatible provider

```env
ACKIFY_STORAGE_TYPE=s3
ACKIFY_STORAGE_MAX_SIZE_MB=50
ACKIFY_STORAGE_S3_ENDPOINT=https://s3.amazonaws.com
ACKIFY_STORAGE_S3_BUCKET=ackify-documents
ACKIFY_STORAGE_S3_ACCESS_KEY=your_access_key
ACKIFY_STORAGE_S3_SECRET_KEY=your_secret_key
ACKIFY_STORAGE_S3_REGION=us-east-1
ACKIFY_STORAGE_S3_USE_SSL=true
```

### Using MinIO (Self-hosted S3)

MinIO is a popular open-source S3-compatible storage solution:

```env
ACKIFY_STORAGE_TYPE=s3
ACKIFY_STORAGE_S3_ENDPOINT=http://minio:9000
ACKIFY_STORAGE_S3_BUCKET=ackify-documents
ACKIFY_STORAGE_S3_ACCESS_KEY=minioadmin
ACKIFY_STORAGE_S3_SECRET_KEY=minioadmin
ACKIFY_STORAGE_S3_REGION=us-east-1
ACKIFY_STORAGE_S3_USE_SSL=false
```

### Supported File Types

- PDF documents (.pdf)
- Images (.png, .jpg, .jpeg, .gif, .webp)
- Office documents (.doc, .docx)
- Text files (.txt)
- HTML files (.html, .htm)

## Anonymous Telemetry

Ackify can collect anonymous usage metrics to help improve the project.

### What is collected

**Business metrics only:**
- Number of documents created
- Number of signatures/confirmations
- Number of webhooks configured
- Number of email reminders sent

### What is NOT collected

- No personal data
- No user information (names, emails, IPs)
- No document content
- No authentication details

### Privacy

- **GDPR compliant** - No personal data is ever collected
- **Non-intrusive** - Runs in background, no impact on performance
- **Opt-in** - Disabled by default, you choose to enable it

### Configuration

```env
# Enable anonymous telemetry (default: false)
ACKIFY_TELEMETRY=true
```

We encourage you to enable telemetry to help us improve Ackify for everyone!

## SMTP Configuration

SMTP is used for:
- Email reminders for document signatures
- MagicLink authentication

**Popular SMTP Providers:**

**Gmail:**
1. Enable 2FA on your Google account
2. Create an App Password at https://myaccount.google.com/apppasswords
3. Use settings:
   ```env
   MAIL_HOST=smtp.gmail.com
   MAIL_PORT=587
   MAIL_USERNAME=your-email@gmail.com
   MAIL_PASSWORD=your-app-password
   ```

**SMTP2GO:** https://www.smtp2go.com/
**SendGrid:** https://sendgrid.com/
**Mailgun:** https://www.mailgun.com/

## Configuration Variables Reference

See `.env.example` for a complete list of configuration variables with detailed comments.

### Required Variables

```env
APP_BASE_URL=https://your-domain.com
APP_ORGANISATION="Your Organization"
POSTGRES_USER=ackifyr
POSTGRES_PASSWORD=generated_password
POSTGRES_DB=ackify
OAUTH_COOKIE_SECRET=generated_secret
ED25519_PRIVATE_KEY_B64=generated_key
ADMIN_EMAILS=admin@your-domain.com
```

**Note:** At least one authentication method (OAuth or MagicLink) must also be configured.

### Optional Variables

- `OAUTH_ALLOWED_DOMAIN` - Restrict sign-ins to specific email domain
- `OAUTH_AUTO_LOGIN` - Automatically log in if OAuth session exists
- `MAIL_*` - SMTP configuration for email features
- `AUTH_MAGICLINK_ENABLED` - Force enable/disable MagicLink
- `ONLY_ADMIN_CAN_CREATE` - Restrict document creation to admins only (default: false)
- `ACKIFY_TELEMETRY` - Enable anonymous usage metrics (default: false)
- `ACKIFY_STORAGE_TYPE` - Document storage type: `local` or `s3` (default: disabled)
- `ACKIFY_STORAGE_MAX_SIZE_MB` - Maximum upload file size in MB (default: 50)
- `ACKIFY_STORAGE_S3_*` - S3-compatible storage configuration

## Troubleshooting

### No authentication method enabled

**Error:** "At least ONE authentication method must be enabled!"

**Solution:** Configure either:
- OAuth (set `OAUTH_CLIENT_ID` and `OAUTH_CLIENT_SECRET`)
- MagicLink (set `MAIL_HOST` and SMTP credentials)

### OAuth not working

1. Verify redirect URI in OAuth provider settings:
   ```
   https://your-domain.com/auth/callback
   ```

2. Check OAuth credentials are correct in `.env`

3. Verify `APP_BASE_URL` matches your domain

### MagicLink emails not sending

1. Verify SMTP credentials are correct
2. Check SMTP host and port settings
3. Review logs: `docker compose logs -f ackify-ce`
4. Test SMTP connection with your provider's tools

### Permission denied errors

Make sure Docker has necessary permissions:
```bash
sudo usermod -aG docker $USER
newgrp docker
```

## Updating Ackify

To update to the latest version:

```bash
cd ackify-ce
docker compose pull
docker compose up -d
```

## Support

- Documentation: https://github.com/kolapsis/ackify
- Issues: https://github.com/kolapsis/ackify/issues
