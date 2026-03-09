# Ackify Community Edition - Build & Deployment Guide

## Overview

Ackify Community Edition (CE) is the open-source version of Ackify, a document signature validation platform with a modern API-first architecture. This guide covers building and deploying the Community Edition.

## Architecture

Ackify CE consists of:
- **Go Backend**: Vue 3 SPA frontend served by Go backend with REST API v1, OAuth2 authentication, and PostgreSQL database
- **Vue 3 SPA Frontend**: Modern TypeScript-based single-page application with Vite, Pinia state management, and Tailwind CSS
- **Docker Multi-Stage Build**: Optimized containerized deployment

The built Vue 3 SPA is embedded directly into the Go binary via the `//go:embed all:web/dist` directive, allowing single-binary deployment.

## Prerequisites

- Go 1.24.5 or later
- Node.js 22+ and npm (for Vue SPA development)
- Docker and Docker Compose (for containerized deployment)
- PostgreSQL 16+ (for database)

## Building from Source

### 1. Clone the Repository

```bash
git clone https://github.com/kolapsis/ackify.git
cd ackify-ce
```

### 2. Build the Vue SPA

```bash
cd webapp
npm install
npm run build
cd ..
```

This creates an optimized production build in `webapp/dist/`.

### 3. Build the Go Application

Run from project root:

```bash
# Build Community Edition
go build ./backend/cmd/community

# Or build with specific output name
go build -o ackify-ce ./backend/cmd/community
```

The Go application will serve both the API endpoints and the Vue SPA.

### 4. Run Tests

```bash
# Run Go tests
go test ./...

# Run Go tests with verbose output
go test -v ./backend/internal/...

# Run integration tests (requires PostgreSQL)
INTEGRATION_TESTS=1 go test -tags=integration -v ./internal/infrastructure/database/
```

## Configuration

### Environment Variables

Copy the example environment file and modify it:

```bash
cp .env.example .env
```

Required environment variables:

- `ACKIFY_BASE_URL`: Public URL of your application
- `ACKIFY_OAUTH_CLIENT_ID`: OAuth2 client ID
- `ACKIFY_OAUTH_CLIENT_SECRET`: OAuth2 client secret
- `ACKIFY_DB_DSN`: PostgreSQL connection string
- `ACKIFY_OAUTH_COOKIE_SECRET`: Base64-encoded secret for session cookies (32+ bytes)
- `ACKIFY_ORGANISATION`: Organization name displayed in the application

Optional configuration:
- `ACKIFY_TEMPLATES_DIR`: Custom path to emails templates directory (defaults to relative path for development, `/app/templates` in Docker)
- `ACKIFY_LOCALES_DIR`: Custom path to locales directory (default: `locales`)
- `ACKIFY_SPA_DIR`: Custom path to Vue SPA build directory (default: `dist`)
- `ACKIFY_LISTEN_ADDR`: Server listen address (default: `:8080`)
- `ACKIFY_ED25519_PRIVATE_KEY`: Base64-encoded Ed25519 private key for signatures
- `ACKIFY_OAUTH_PROVIDER`: OAuth provider (`google`, `github`, `gitlab` or empty for custom)
- `ACKIFY_OAUTH_ALLOWED_DOMAIN`: Domain restriction for OAuth users
- `ACKIFY_OAUTH_AUTO_LOGIN`: Enable automatic OAuth login when session exists (default: `false`)
- `ACKIFY_LOG_LEVEL`: Logging level - `debug`, `info`, `warn`, `error` (default: `info`)
- `ACKIFY_ADMIN_EMAILS`: Comma-separated list of admin email addresses
- `ACKIFY_MAIL_HOST`: SMTP server host (required to enable email features)
- `ACKIFY_MAIL_PORT`: SMTP server port (default: `587`)
- `ACKIFY_MAIL_USERNAME`: SMTP username for authentication
- `ACKIFY_MAIL_PASSWORD`: SMTP password for authentication
- `ACKIFY_MAIL_TLS`: Enable TLS connection (default: `true`)
- `ACKIFY_MAIL_STARTTLS`: Enable STARTTLS (default: `true`)
- `ACKIFY_MAIL_TIMEOUT`: SMTP connection timeout (default: `10s`)
- `ACKIFY_MAIL_FROM`: Email sender address
- `ACKIFY_MAIL_FROM_NAME`: Email sender name (defaults to `ACKIFY_ORGANISATION`)
- `ACKIFY_MAIL_SUBJECT_PREFIX`: Prefix for email subjects
- `ACKIFY_MAIL_TEMPLATE_DIR`: Custom path to email templates (default: `templates/emails`)
- `ACKIFY_MAIL_DEFAULT_LOCALE`: Default locale for emails (default: `en`)

### Logging Configuration

Ackify uses structured JSON logging with the following levels:

- **debug**: Detailed diagnostic information (request/response details, authentication attempts)
- **info**: General informational messages (successful operations, API requests)
- **warn**: Warning messages (failed authentication, rate limiting)
- **error**: Error messages (server errors, database failures)

Example:
```bash
# Development - verbose logging
ACKIFY_LOG_LEVEL=debug

# Production - standard logging
ACKIFY_LOG_LEVEL=info
```

Logs include structured fields for easy parsing:
- `request_id`: Unique identifier for each request
- `user_email`: Authenticated user email
- `method`, `path`, `status`: HTTP request details
- `duration_ms`: Request processing time

### OAuth2 Providers

Supported providers:
- `google` (default)
- `github`
- `gitlab`
- Custom (specify `ACKIFY_OAUTH_AUTH_URL`, `ACKIFY_OAUTH_TOKEN_URL`, `ACKIFY_OAUTH_USERINFO_URL`)

## Deployment Options

### Option 1: Direct Binary

1. Build the application
2. Set environment variables
3. Run the binary:

```bash
./ackify
```

### Option 2: Docker Compose (Recommended)

1. Configure environment variables in `.env` file
2. Start services:

```bash
docker compose up -d
```

3. Check logs:

```bash
docker compose logs ackify-ce
```

4. Stop services:

```bash
docker compose down
```

### Option 3: Docker Build

```bash
# Build Docker image
docker build -t ackify-ce:latest .

# Run with environment file
docker run --env-file .env -p 8080:8080 ackify-ce:latest
```

## Database Setup

The application requires PostgreSQL. When using Docker Compose, the database is automatically created and configured.

For manual setup:

1. Create a PostgreSQL database
2. The application will automatically create required tables on first run
3. Set the `ACKIFY_DB_DSN` environment variable to your database connection string

## Health Checks

The application provides a health endpoint:

```bash
curl http://localhost:8080/health
```

## Production Considerations

1. **HTTPS**: Always use HTTPS in production (set `ACKIFY_BASE_URL` with https://)
2. **Secrets**: Use strong, randomly generated secrets for `ACKIFY_OAUTH_COOKIE_SECRET`
3. **Database**: Use a dedicated PostgreSQL instance with proper backups
4. **Monitoring**: Monitor the `/health` endpoint for application status
5. **Logs**: Configure proper log aggregation and monitoring

## API Endpoints

### API v1 (RESTful)

All API v1 endpoints are prefixed with `/api/v1`.

#### Public Endpoints
- `GET /api/v1/health` - Health check
- `GET /api/v1/csrf` - Get CSRF token for authenticated requests
- `GET /api/v1/documents` - List all documents
- `GET /api/v1/documents/{docId}` - Get document details
- `GET /api/v1/documents/{docId}/signatures` - Get document signatures
- `GET /api/v1/documents/{docId}/expected-signers` - Get expected signers list

#### Authentication Endpoints
- `POST /api/v1/auth/start` - Start OAuth flow
- `GET /api/v1/auth/logout` - Logout
- `GET /api/v1/auth/check` - Check authentication status (if `ACKIFY_OAUTH_AUTO_LOGIN=true`)

#### Authenticated Endpoints (require valid session)
- `GET /api/v1/users/me` - Get current user profile
- `GET /api/v1/signatures` - Get current user's signatures
- `POST /api/v1/signatures` - Create new signature
- `GET /api/v1/documents/{docId}/signatures/status` - Get user's signature status for document

#### Admin Endpoints (require admin privileges)
- `GET /api/v1/admin/documents` - List all documents with stats
- `GET /api/v1/admin/documents/{docId}` - Get document details (admin view)
- `GET /api/v1/admin/documents/{docId}/signers` - Get document with signers and stats
- `POST /api/v1/admin/documents/{docId}/signers` - Add expected signer
- `DELETE /api/v1/admin/documents/{docId}/signers/{email}` - Remove expected signer
- `POST /api/v1/admin/documents/{docId}/reminders` - Send email reminders
- `GET /api/v1/admin/documents/{docId}/reminders` - Get reminder history

### Public Endpoints

- `GET /` - Vue SPA (serves index.html for all routes)
- `GET /health` - Health check
- `GET /api/v1/auth/callback` - OAuth2 callback handler

**Note:** Link unfurling for messaging apps (Slack, Discord, etc.) is handled automatically via dynamic Open Graph meta tags in the Vue SPA. There are no separate `/embed` or `/oembed` endpoints.

## Development

### Vue SPA Development

For Vue SPA development with hot-reload:

```bash
cd webapp
npm install
npm run dev
```

This starts a Vite development server on `http://localhost:5173` with:
- Hot module replacement (HMR)
- TypeScript type checking
- API proxy to backend (configured in `vite.config.ts`)

The development server proxies API requests to your Go backend (default: `http://localhost:8080`).

### Backend Development

Run the Go backend separately:

```bash
# In project root
go build ./backend/cmd/community
./ackify
```

Or use Docker Compose for complete stack:

```bash
docker compose up -d
```

## Troubleshooting

### Common Issues

1. **Port already in use**: Change `ACKIFY_LISTEN_ADDR` in environment variables
2. **Database connection failed**: Check `ACKIFY_DB_DSN` and ensure PostgreSQL is running
3. **OAuth2 errors**: Verify `ACKIFY_OAUTH_CLIENT_ID` and `ACKIFY_OAUTH_CLIENT_SECRET`
4. **SPA not loading**: Ensure Vue app is built (`npm run build` in webapp/) before running Go binary
5. **CORS errors in development**: Check that Vite dev server proxy is correctly configured

### Logs

Enable debug logging to see detailed request/response information:

```bash
ACKIFY_LOG_LEVEL=debug ./ackify
```

Debug logs include:
- HTTP request details (method, path, headers)
- Authentication attempts and results
- Database queries and performance
- OAuth flow progression
- Signature creation and validation steps

## Contributing

This is the Community Edition. Contributions are welcome! Please see the main repository for contribution guidelines.

## License

Community Edition is released under the GNU Affero General Public License v3.0 (AGPLv3).
