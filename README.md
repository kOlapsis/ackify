# 🔐 Ackify

> **Proof of Read. Compliance made simple.**

Secure document reading validation with cryptographic signatures and irrefutable proof.

[![Build](https://github.com/kolapsis/ackify/actions/workflows/ci.yml/badge.svg)](https://github.com/kolapsis/ackify/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/kolapsis/ackify/branch/main/graph/badge.svg)](https://codecov.io/gh/kolapsis/ackify)
[![Security](https://img.shields.io/badge/crypto-Ed25519-blue.svg)](https://en.wikipedia.org/wiki/EdDSA)
[![Go](https://img.shields.io/badge/go-1.24.5-blue.svg)](https://golang.org/)
[![License](https://img.shields.io/badge/License-AGPL_v3-blue.svg)](LICENSE)

> 🇫🇷 [Version française disponible ici](README_FR.md)

**Website**: https://www.ackify.eu

---

## 🎯 Why Ackify?

Prove that collaborators have read and acknowledged important documents with **Ed25519 cryptographic signatures**.

**Use Cases**:
- Security policy validation
- Training attestations
- GDPR acknowledgment
- Contractual agreements
- Compliance procedures

**Key Features**:
- ✅ Ed25519 cryptographic signatures
- ✅ **Flexible authentication**: OAuth2 (Google, GitHub, GitLab, custom) or MagicLink (passwordless email)
- ✅ One signature per user/document (database enforced)
- ✅ Immutable audit trail
- ✅ Expected signers tracking with email reminders
- ✅ Document checksum verification (SHA-256/512, MD5)
- ✅ Optional document storage (local filesystem or S3-compatible)
- ✅ Public embeddable widgets (Notion, Outline, etc.)
- ✅ Admin dashboard (Vue.js 3 + dark mode)
- ✅ Multilingual (fr, en, es, de, it)

---

## ⚡ Quick Start

### Prerequisites

- Docker & Docker Compose
- **At least ONE authentication method**:
  - OAuth2 credentials (Google, GitHub, or GitLab), OR
  - SMTP server for MagicLink (passwordless email authentication)

### Installation

#### Option 1: Automated Installation Script (Recommended)

```bash
# Download and run installation script
bash <(curl -fsSL https://raw.githubusercontent.com/kolapsis/ackify/main/install/install.sh)

# Navigate to installation directory
cd ackify-ce

# Edit configuration with your OAuth2 credentials
nano .env

# Start services
docker compose up -d

# Access web interface
open http://localhost:8080
```

**What the script does:**
- Downloads `compose.yml` and `.env.example`
- Generates secure secrets automatically:
  - `ACKIFY_OAUTH_COOKIE_SECRET` (AES-256 session encryption)
  - `POSTGRES_PASSWORD` (database password)
- Creates ready-to-use `.env` file
- You only need to add your OAuth2 credentials

#### Option 2: Manual Installation

```bash
# Clone repository
git clone https://github.com/kolapsis/ackify.git
cd ackify-ce

# Configure
cp .env.example .env
nano .env  # Edit with your OAuth2 credentials and generate secrets

# Start services
docker compose up -d

# Verify
curl http://localhost:8080/api/v1/health
# Expected: {"status":"healthy","database":"connected"}

# Access web interface
open http://localhost:8080
```

### Required Environment Variables

```bash
# Application
APP_DNS=sign.your-domain.com
ACKIFY_BASE_URL=https://sign.your-domain.com
ACKIFY_ORGANISATION="Your Organization"

# Database
POSTGRES_USER=ackifyr
POSTGRES_PASSWORD=your_secure_password
POSTGRES_DB=ackify

# Security (generate with: openssl rand -base64 32)
ACKIFY_OAUTH_COOKIE_SECRET=your_base64_secret

# ============================================================================
# Authentication (choose AT LEAST ONE method)
# ============================================================================

# Option 1: OAuth2 (Google, GitHub, GitLab, custom)
ACKIFY_OAUTH_PROVIDER=google
ACKIFY_OAUTH_CLIENT_ID=your_client_id
ACKIFY_OAUTH_CLIENT_SECRET=your_client_secret

# Option 2: MagicLink (passwordless email authentication)
# ACKIFY_MAIL_HOST=smtp.example.com
# ACKIFY_MAIL_PORT=587
# ACKIFY_MAIL_USERNAME=your_smtp_username
# ACKIFY_MAIL_PASSWORD=your_smtp_password
# ACKIFY_MAIL_FROM=noreply@example.com
```

**Auto-detection**:
- OAuth is enabled automatically if `ACKIFY_OAUTH_CLIENT_ID` and `ACKIFY_OAUTH_CLIENT_SECRET` are set
- MagicLink requires explicit activation: `ACKIFY_AUTH_MAGICLINK_ENABLED=true` + SMTP configuration
- SMTP/Email service (for reminders) is enabled automatically when `ACKIFY_MAIL_HOST` is configured
- You can use **both methods simultaneously** for maximum flexibility

See [docs/en/configuration.md](docs/en/configuration.md) for all options.

---

## 📸 Screenshots

<table>
<tr>
<td align="center"><strong>Home</strong><br><a href="screenshots/1-home.png"><img src="screenshots/1-home.png" width="200"></a></td>
<td align="center"><strong>Document Upload</strong><br><a href="screenshots/2-new-doc-upload.png"><img src="screenshots/2-new-doc-upload.png" width="200"></a></td>
<td align="center"><strong>Add Readers</strong><br><a href="screenshots/3-add-confirm-request.png"><img src="screenshots/3-add-confirm-request.png" width="200"></a></td>
<td align="center"><strong>Integrated Reader</strong><br><a href="screenshots/4-internal-reader.png"><img src="screenshots/4-internal-reader.png" width="200"></a></td>
</tr>
<tr>
<td align="center"><strong>Reading Confirmed</strong><br><a href="screenshots/5-read-confirmed.png"><img src="screenshots/5-read-confirmed.png" width="200"></a></td>
<td align="center"><strong>My Confirmations</strong><br><a href="screenshots/6-all-my-confirmations.png"><img src="screenshots/6-all-my-confirmations.png" width="200"></a></td>
<td align="center"><strong>My Documents</strong><br><a href="screenshots/7-manage-my-docs.png"><img src="screenshots/7-manage-my-docs.png" width="200"></a></td>
<td align="center"><strong>Admin Settings</strong><br><a href="screenshots/8-admin-settings.png"><img src="screenshots/8-admin-settings.png" width="200"></a></td>
</tr>
</table>

---

## 📚 Documentation

### Getting Started
- **[Getting Started](docs/en/getting-started.md)** - Installation & first steps
- **[Configuration](docs/en/configuration.md)** - Environment variables
- **[OAuth Providers](docs/en/configuration/oauth-providers.md)** - Google, GitHub, GitLab, Custom
- **[Email Setup](docs/en/configuration/email-setup.md)** - SMTP configuration

### Features
- **[Signatures](docs/en/features/signatures.md)** - Ed25519 cryptographic flow
- **[Expected Signers](docs/en/features/expected-signers.md)** - Tracking & reminders
- **[Checksums](docs/en/features/checksums.md)** - Document integrity verification
- **[Document Storage](docs/en/features/storage.md)** - Optional upload (local or S3)
- **[Embedding](docs/en/features/embedding.md)** - oEmbed, iframes, integrations
- **[i18n](docs/en/features/i18n.md)** - Internationalization (5 languages)

### Technical
- **[Architecture](docs/en/architecture.md)** - Tech stack & design principles
- **[Database](docs/en/database.md)** - PostgreSQL schema & migrations
- **[API Reference](docs/en/api.md)** - REST endpoints & examples
- **[Deployment](docs/en/deployment.md)** - Production with Docker Compose
- **[Development](docs/en/development.md)** - Contributing & setup

---

## 🚀 Usage

### Request a Signature

```
https://your-domain.com/?doc=security_policy_2025
```

User authenticates (OAuth2 or MagicLink) and signs with one click.

### Embed in Your Tools

**iFrame**:
```html
<iframe src="https://your-domain.com/embed?doc=policy_2025"
        width="600" height="200" frameborder="0"></iframe>
```

**oEmbed** (Notion, Outline, Confluence):
```
Paste the embed URL: https://your-domain.com/embed?doc=policy_2025
Automatic embed via oEmbed discovery
```

**Open Graph** (Slack, Teams):
```
Paste direct URL: https://your-domain.com/?doc=policy_2025
URL unfurls automatically with signature count
```

> **Important**: Use `/embed?doc=...` for iframe integrations (Notion, Outline) and `/?doc=...` for direct links (emails, Slack).

See [docs/en/features/embedding.md](docs/en/features/embedding.md) for details.

---

## 🛡️ Security

- **Ed25519** - State-of-the-art elliptic curve signatures
- **SHA-256** - Payload hashing for tampering detection
- **PKCE** - OAuth2 security (automatic)
- **AES-256-GCM** - Encrypted refresh tokens
- **Immutable timestamps** - PostgreSQL triggers
- **Rate limiting** - 5 auth/min, 100 req/min
- **HTTPS enforced** - Secure cookies only

See [docs/en/architecture.md](docs/en/architecture.md) for full security details.

---

## 🔧 Tech Stack

**Backend**: Go 1.24.5, PostgreSQL 16, Chi Router, OAuth2, Ed25519

**Frontend**: Vue 3, TypeScript, Vite, Pinia, Tailwind CSS, shadcn/vue

**DevOps**: Docker, Multi-stage builds, Distroless image (< 30MB)

**i18n**: 5 languages (fr, en, es, de, it)

**Coverage**: 72.6% (180+ unit tests, 33 integration tests)

---

## 🤝 Support

- **Issues**: [GitHub Issues](https://github.com/kolapsis/ackify/issues)
- **Discussions**: [GitHub Discussions](https://github.com/kolapsis/ackify/discussions)
- **Email**: contact@ackify.eu

---

## 📄 License

AGPLv3 - See [LICENSE](LICENSE) for details.

---

**Developed with ❤️ by [Benjamin TOUCHARD](https://www.kolapsis.com)**
