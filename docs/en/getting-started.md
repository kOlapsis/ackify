# Getting Started

Installation and configuration guide for Ackify with Docker Compose.

## Prerequisites

- Docker and Docker Compose installed
- A domain (or localhost for testing)
- OAuth2 credentials (Google, GitHub, GitLab, or custom)

## Quick Installation

### 1. Clone the repository

```bash
git clone https://github.com/kolapsis/ackify.git
cd ackify-ce
```

### 2. Configuration

Copy the example file and edit it:

```bash
cp .env.example .env
nano .env
```

**Minimum required variables**:

```bash
# Public domain of your instance
APP_DNS=sign.your-domain.com
ACKIFY_BASE_URL=https://sign.your-domain.com
ACKIFY_ORGANISATION="Your Organization Name"

# PostgreSQL database
POSTGRES_USER=ackifyr
POSTGRES_PASSWORD=your_secure_password_here
POSTGRES_DB=ackify

# OAuth2 (example with Google)
ACKIFY_OAUTH_PROVIDER=google
ACKIFY_OAUTH_CLIENT_ID=your_google_client_id
ACKIFY_OAUTH_CLIENT_SECRET=your_google_client_secret

# Security - generate with: openssl rand -base64 32
ACKIFY_OAUTH_COOKIE_SECRET=your_base64_encoded_secret_key
```

### 3. Start

```bash
docker compose up -d
```

This command will:
- Download necessary Docker images
- Start PostgreSQL with healthcheck
- Apply database migrations
- Launch the Ackify application

### 4. Verification

```bash
# View logs
docker compose logs -f ackify-ce

# Check health endpoint
curl http://localhost:8080/api/v1/health
# Expected: {"status":"healthy","database":"connected"}
```

### 5. Access the interface

Open your browser:
- **Public interface**: http://localhost:8080
- **Admin dashboard**: http://localhost:8080/admin (requires email in ACKIFY_ADMIN_EMAILS)

## OAuth2 Configuration

Before using Ackify, configure your OAuth2 provider.

### Google OAuth2

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project or select an existing one
3. Enable the "Google+ API"
4. Create OAuth 2.0 credentials:
   - Type: Web application
   - Authorized redirect URIs: `https://sign.your-domain.com/api/v1/auth/callback`
5. Copy the Client ID and Client Secret to `.env`

```bash
ACKIFY_OAUTH_PROVIDER=google
ACKIFY_OAUTH_CLIENT_ID=123456789-abc.apps.googleusercontent.com
ACKIFY_OAUTH_CLIENT_SECRET=GOCSPX-xyz...
```

### GitHub OAuth2

1. Go to [GitHub Developer Settings](https://github.com/settings/developers)
2. Create a new OAuth App
3. Configuration:
   - Homepage URL: `https://sign.your-domain.com`
   - Callback URL: `https://sign.your-domain.com/api/v1/auth/callback`
4. Generate a client secret

```bash
ACKIFY_OAUTH_PROVIDER=github
ACKIFY_OAUTH_CLIENT_ID=Iv1.abc123
ACKIFY_OAUTH_CLIENT_SECRET=ghp_xyz...
```

See [OAuth Providers](configuration/oauth-providers.md) for GitLab and custom providers.

## Generating Secrets

```bash
# Cookie secret (required)
openssl rand -base64 32

# Ed25519 private key (optional, auto-generated if missing)
openssl rand -base64 64
```

## First Steps

### Create your first signature

1. Go to `http://localhost:8080/?doc=test_document`
2. Click "Sign this document"
3. Login via OAuth2
4. Validate the signature

### Access the admin dashboard

1. Add your email in `.env`:
   ```bash
   ACKIFY_ADMIN_EMAILS=admin@company.com
   ```
2. Restart:
   ```bash
   docker compose restart ackify-ce
   ```
3. Login and access `/admin`

### Embed in a page

```html
<!-- Embeddable widget -->
<iframe src="https://sign.your-domain.com/?doc=test_document"
        width="600" height="200"
        frameborder="0"
        style="border: 1px solid #ddd; border-radius: 6px;"></iframe>
```

## Useful Commands

```bash
# View logs
docker compose logs -f ackify-ce

# Restart
docker compose restart ackify-ce

# Stop
docker compose down

# Rebuild after changes
docker compose up -d --force-recreate ackify-ce --build

# Access the database
docker compose exec ackify-db psql -U ackifyr -d ackify
```

## Troubleshooting

### Application doesn't start

```bash
# Check logs
docker compose logs ackify-ce

# Check PostgreSQL health
docker compose ps ackify-db
```

### Migration error

```bash
# Manually re-run migrations
docker compose up ackify-migrate
```

### OAuth callback error

Verify that:
- `ACKIFY_BASE_URL` exactly matches your domain
- The callback URL in the OAuth2 provider is correct
- The cookie secret is properly configured

## Next Steps

- [Complete configuration](configuration.md)
- [Production deployment](deployment.md)
- [Features configuration](features/)
- [API Reference](api.md)
