# Development

Guide for contributing and developing on Ackify.

## Development Setup

### Prerequisites

- **Go 1.24.5+**
- **Node.js 22+** and npm
- **PostgreSQL 16+**
- **Docker & Docker Compose**
- Git

### Clone & Setup

```bash
# Clone
git clone https://github.com/kolapsis/ackify.git
cd ackify-ce

# Copy .env
cp .env.example .env

# Edit .env with your OAuth2 credentials
nano .env
```

## Backend Development

### Build

```bash
cd backend
go mod download
go build ./cmd/community
```

### Run

```bash
# Start PostgreSQL with Docker
docker compose up -d ackify-db

# Apply migrations
go run ./cmd/migrate up

# Launch app
./community
```

API accessible at `http://localhost:8080`.

### Tests

```bash
# Unit tests
go test -v -short ./...

# Tests with coverage
go test -coverprofile=coverage.out ./internal/... ./pkg/...

# View coverage
go tool cover -html=coverage.out

# Integration tests (PostgreSQL required)
docker compose -f ../compose.test.yml up -d
INTEGRATION_TESTS=1 go test -tags=integration -v ./internal/infrastructure/database/
docker compose -f ../compose.test.yml down
```

### Linting

```bash
# Format
go fmt ./...

# Vet
go vet ./...

# Staticcheck (optional)
go install honnef.co/go/tools/cmd/staticcheck@latest
staticcheck ./...
```

## Frontend Development

### Setup

```bash
cd webapp
npm install
```

### Dev Server

```bash
npm run dev
```

Frontend accessible at `http://localhost:5173` with Hot Module Replacement.

### Production Build

```bash
npm run build
# Output: webapp/dist/
```

### Type Checking

```bash
npm run type-check
```

### i18n Validation

```bash
npm run lint:i18n
```

Verifies all translations are complete.

## Docker Development

### Local Build

```bash
# Build complete image (frontend + backend)
docker compose -f compose.local.yml up -d --build

# Logs
docker compose -f compose.local.yml logs -f ackify-ce

# Rebuild after changes
docker compose -f compose.local.yml up -d --force-recreate ackify-ce --build
```

### Debug

```bash
# Shell in container
docker compose exec ackify-ce sh

# PostgreSQL shell
docker compose exec ackify-db psql -U ackifyr -d ackify
```

## Code Structure

### Backend

```
backend/
├── cmd/
│   ├── community/        # main.go + dependency injection
│   └── migrate/          # Migration tool
├── internal/
│   ├── domain/models/    # Entities (User, Signature, Document)
│   ├── application/services/  # Business logic
│   ├── infrastructure/
│   │   ├── auth/         # OAuth2
│   │   ├── database/     # Repositories
│   │   ├── email/        # SMTP
│   │   └── config/       # Config
│   └── presentation/api/ # HTTP handlers
└── pkg/                  # Utilities
```

### Frontend

```
webapp/src/
├── components/           # Vue components
├── pages/               # Pages (router)
├── services/            # API client
├── stores/              # Pinia stores
├── router/              # Vue Router
└── locales/             # Translations
```

## Code Conventions

### Go

**Naming**:
- Packages: lowercase, singular (`user`, `signature`)
- Interfaces: suffix `er` or descriptive (`SignatureRepository`, `EmailSender`)
- Constructors: `New...()` or `...From...()`

**Example**:
```go
// Service
type SignatureService struct {
    repo SignatureRepository
    crypto CryptoService
}

func NewSignatureService(repo SignatureRepository, crypto CryptoService) *SignatureService {
    return &SignatureService{repo: repo, crypto: crypto}
}

// Method
func (s *SignatureService) CreateSignature(ctx context.Context, docID, userSub string) (*models.Signature, error) {
    // ...
}
```

**Errors**:
```go
// Wrapping
return nil, fmt.Errorf("failed to create signature: %w", err)

// Custom errors
var ErrAlreadySigned = errors.New("user has already signed this document")
```

### TypeScript

**Naming**:
- Components: PascalCase (`DocumentCard.vue`)
- Composables: camelCase with `use` prefix (`useAuth.ts`)
- Stores: camelCase with `Store` suffix (`userStore.ts`)

**Example**:
```typescript
// Composable
export function useAuth() {
  const user = ref<User | null>(null)

  async function login() {
    // ...
  }

  return { user, login }
}

// Store
export const useUserStore = defineStore('user', () => {
  const currentUser = ref<User | null>(null)

  async function fetchMe() {
    const { data } = await api.get('/users/me')
    currentUser.value = data
  }

  return { currentUser, fetchMe }
})
```

## Adding a Feature

### 1. Planning

- Define required API endpoints
- SQL schema if needed
- User interface

### 2. Backend

```bash
# 1. Create migration if needed
touch backend/migrations/XXXX_my_feature.up.sql
touch backend/migrations/XXXX_my_feature.down.sql

# 2. Create model
# backend/internal/domain/models/my_model.go

# 3. Create repository interface
# backend/internal/application/services/my_service.go

# 4. Implement repository
# backend/internal/infrastructure/database/my_repository.go

# 5. Create API handler
# backend/internal/presentation/api/myfeature/handler.go

# 6. Register routes
# backend/internal/presentation/api/router.go
```

### 3. Frontend

```bash
# 1. Create API service
# webapp/src/services/myFeatureService.ts

# 2. Create Pinia store
# webapp/src/stores/myFeatureStore.ts

# 3. Create components
# webapp/src/components/MyFeature.vue

# 4. Add translations
# webapp/src/locales/{fr,en,es,de,it}.json

# 5. Add routes if needed
# webapp/src/router/index.ts
```

### 4. Tests

```bash
# Backend
# backend/internal/presentation/api/myfeature/handler_test.go

# Test
go test -v ./internal/presentation/api/myfeature/
```

### 5. Documentation

Update:
- `/api/openapi.yaml` - OpenAPI specification
- `/docs/api.md` - API documentation
- `/docs/features/my-feature.md` - User guide

## Debugging

### Backend

```go
// Structured logs
logger.Info("signature created",
    "doc_id", docID,
    "user_sub", userSub,
    "signature_id", sig.ID,
)

// Debug via Delve (optional)
dlv debug ./cmd/community
```

### Frontend

```typescript
// Vue DevTools (Chrome/Firefox extension)
// Inspect: Components, Pinia stores, Router

// Console debug
console.log('[DEBUG] User:', user.value)

// Breakpoints via browser
debugger
```

## SQL Migrations

### Create Migration

```sql
-- XXXX_add_field.up.sql
ALTER TABLE signatures ADD COLUMN new_field TEXT;

-- XXXX_add_field.down.sql
ALTER TABLE signatures DROP COLUMN new_field;
```

### Apply

```bash
go run ./cmd/migrate up
```

### Rollback

```bash
go run ./cmd/migrate down
```

## Integration Tests

### Setup PostgreSQL Test

```bash
docker compose -f compose.test.yml up -d
```

### Run Tests

```bash
INTEGRATION_TESTS=1 go test -tags=integration -v ./internal/infrastructure/database/
```

### Cleanup

```bash
docker compose -f compose.test.yml down -v
```

## CI/CD

### GitHub Actions

Project uses `.github/workflows/ci.yml`:

**Jobs**:
1. **Lint** - go fmt, go vet, eslint
2. **Test Backend** - Unit + integration tests
3. **Test Frontend** - Type checking + i18n validation
4. **Coverage** - Upload to Codecov
5. **Build** - Verify Docker image builds

### Pre-commit Hooks (optional)

```bash
# Install pre-commit
pip install pre-commit

# Setup hooks
pre-commit install

# Run manually
pre-commit run --all-files
```

## Contribution

### Git Workflow

```bash
# 1. Create branch
git checkout -b feature/my-feature

# 2. Develop + commit
git add .
git commit -m "feat: add my feature"

# 3. Push
git push origin feature/my-feature

# 4. Create Pull Request on GitHub
```

### Commit Messages

Format: `type: description`

**Types**:
- `feat` - New feature
- `fix` - Bug fix
- `docs` - Documentation
- `refactor` - Refactoring
- `test` - Tests
- `chore` - Maintenance

**Examples**:
```
feat: add checksum verification feature
fix: resolve OAuth callback redirect loop
docs: update API documentation for signatures
refactor: simplify signature service logic
test: add integration tests for expected signers
```

### Code Review

**Checklist**:
- ✅ Tests pass (CI green)
- ✅ Code formatted (`go fmt`, `eslint`)
- ✅ No committed secrets
- ✅ Documentation updated
- ✅ Complete translations (i18n)

## Troubleshooting

### Backend won't start

```bash
# Check PostgreSQL
docker compose ps ackify-db
docker compose logs ackify-db

# Check environment variables
cat .env

# Detailed logs
ACKIFY_LOG_LEVEL=debug ./community
```

### Frontend build fails

```bash
# Clean and reinstall
rm -rf node_modules package-lock.json
npm install

# Check Node version
node --version  # Should be 22+
```

### Tests fail

```bash
# Backend - check PostgreSQL test
docker compose -f compose.test.yml ps

# Frontend - check types
npm run type-check
```

## Resources

- [Go Documentation](https://go.dev/doc/)
- [Vue 3 Guide](https://vuejs.org/guide/)
- [PostgreSQL Docs](https://www.postgresql.org/docs/)
- [Chi Router](https://github.com/go-chi/chi)
- [Pinia](https://pinia.vuejs.org/)

## Support

- [GitHub Issues](https://github.com/kolapsis/ackify/issues)
- [GitHub Discussions](https://github.com/kolapsis/ackify/discussions)
