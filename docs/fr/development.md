# Development

Guide pour contribuer et développer sur Ackify.

## Setup Développement

### Prérequis

- **Go 1.24.5+**
- **Node.js 22+** et npm
- **PostgreSQL 16+**
- **Docker & Docker Compose**
- Git

### Clone & Setup

```bash
# Clone
git clone https://github.com/kolapsis/ackify.git
cd ackify-ce

# Copier .env
cp .env.example .env

# Éditer .env avec vos credentials OAuth2
nano .env
```

## Développement Backend

### Build

```bash
cd backend
go mod download
go build ./cmd/community
```

### Run

```bash
# Démarrer PostgreSQL avec Docker
docker compose up -d ackify-db

# Appliquer les migrations
go run ./cmd/migrate up

# Lancer l'app
./community
```

L'API est accessible sur `http://localhost:8080`.

### Tests

```bash
# Tests unitaires
go test -v -short ./...

# Tests avec coverage
go test -coverprofile=coverage.out ./internal/... ./pkg/...

# Voir le coverage
go tool cover -html=coverage.out

# Tests d'intégration (PostgreSQL requis)
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

# Staticcheck (optionnel)
go install honnef.co/go/tools/cmd/staticcheck@latest
staticcheck ./...
```

## Développement Frontend

### Setup

```bash
cd webapp
npm install
```

### Dev Server

```bash
npm run dev
```

Frontend accessible sur `http://localhost:5173` avec Hot Module Replacement.

### Build Production

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

Vérifie que toutes les traductions sont complètes.

## Docker Development

### Build Local

```bash
# Build l'image complète (frontend + backend)
docker compose -f compose.local.yml up -d --build

# Logs
docker compose -f compose.local.yml logs -f ackify-ce

# Rebuild après modifications
docker compose -f compose.local.yml up -d --force-recreate ackify-ce --build
```

### Debug

```bash
# Shell dans le container
docker compose exec ackify-ce sh

# PostgreSQL shell
docker compose exec ackify-db psql -U ackifyr -d ackify
```

## Structure du Code

### Backend

```
backend/
├── cmd/
│   ├── community/        # main.go + injection dépendances
│   └── migrate/          # Outil migrations
├── internal/
│   ├── domain/models/    # Entités (User, Signature, Document)
│   ├── application/services/  # Logique métier
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
├── components/           # Composants Vue
├── pages/               # Pages (router)
├── services/            # API client
├── stores/              # Pinia stores
├── router/              # Vue Router
└── locales/             # Traductions
```

## Conventions de Code

### Go

**Naming** :
- Packages : lowercase, singular (`user`, `signature`)
- Interfaces : suffixe `er` ou descriptif (`SignatureRepository`, `EmailSender`)
- Constructeurs : `New...()` ou `...From...()`

**Exemple** :
```go
// Service
type SignatureService struct {
    repo SignatureRepository
    crypto CryptoService
}

func NewSignatureService(repo SignatureRepository, crypto CryptoService) *SignatureService {
    return &SignatureService{repo: repo, crypto: crypto}
}

// Méthode
func (s *SignatureService) CreateSignature(ctx context.Context, docID, userSub string) (*models.Signature, error) {
    // ...
}
```

**Erreurs** :
```go
// Wrapping
return nil, fmt.Errorf("failed to create signature: %w", err)

// Custom errors
var ErrAlreadySigned = errors.New("user has already signed this document")
```

### TypeScript

**Naming** :
- Components : PascalCase (`DocumentCard.vue`)
- Composables : camelCase avec `use` prefix (`useAuth.ts`)
- Stores : camelCase avec `Store` suffix (`userStore.ts`)

**Exemple** :
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

## Ajouter une Feature

### 1. Planifier

- Définir les endpoints API requis
- Schéma SQL si nécessaire
- Interface utilisateur

### 2. Backend

```bash
# 1. Créer migration si besoin
touch backend/migrations/XXXX_my_feature.up.sql
touch backend/migrations/XXXX_my_feature.down.sql

# 2. Créer le modèle
# backend/internal/domain/models/my_model.go

# 3. Créer le repository interface
# backend/internal/application/services/my_service.go

# 4. Implémenter le repository
# backend/internal/infrastructure/database/my_repository.go

# 5. Créer le handler API
# backend/internal/presentation/api/myfeature/handler.go

# 6. Enregistrer les routes
# backend/internal/presentation/api/router.go
```

### 3. Frontend

```bash
# 1. Créer le service API
# webapp/src/services/myFeatureService.ts

# 2. Créer le store Pinia
# webapp/src/stores/myFeatureStore.ts

# 3. Créer les composants
# webapp/src/components/MyFeature.vue

# 4. Ajouter les traductions
# webapp/src/locales/{fr,en,es,de,it}.json

# 5. Ajouter les routes si nécessaire
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

Mettre à jour :
- `/api/openapi.yaml` - Spécification OpenAPI
- `/docs/api.md` - Documentation API
- `/docs/features/my-feature.md` - Guide utilisateur

## Debugging

### Backend

```go
// Logs structurés
logger.Info("signature created",
    "doc_id", docID,
    "user_sub", userSub,
    "signature_id", sig.ID,
)

// Debug via Delve (optionnel)
dlv debug ./cmd/community
```

### Frontend

```typescript
// Vue DevTools (extension Chrome/Firefox)
// Inspecter: Components, Pinia stores, Router

// Console debug
console.log('[DEBUG] User:', user.value)

// Breakpoints via navigateur
debugger
```

## Migrations SQL

### Créer une Migration

```sql
-- XXXX_add_field.up.sql
ALTER TABLE signatures ADD COLUMN new_field TEXT;

-- XXXX_add_field.down.sql
ALTER TABLE signatures DROP COLUMN new_field;
```

### Appliquer

```bash
go run ./cmd/migrate up
```

### Rollback

```bash
go run ./cmd/migrate down
```

## Tests d'Intégration

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

Le projet utilise `.github/workflows/ci.yml` :

**Jobs** :
1. **Lint** - go fmt, go vet, eslint
2. **Test Backend** - Unit + integration tests
3. **Test Frontend** - Type checking + i18n validation
4. **Coverage** - Upload vers Codecov
5. **Build** - Vérifier que l'image Docker build

### Pre-commit Hooks (optionnel)

```bash
# Installer pre-commit
pip install pre-commit

# Setup hooks
pre-commit install

# Run manuellement
pre-commit run --all-files
```

## Contribution

### Workflow Git

```bash
# 1. Créer une branche
git checkout -b feature/my-feature

# 2. Développer + commit
git add .
git commit -m "feat: add my feature"

# 3. Push
git push origin feature/my-feature

# 4. Créer une Pull Request sur GitHub
```

### Commit Messages

Format : `type: description`

**Types** :
- `feat` - Nouvelle feature
- `fix` - Bug fix
- `docs` - Documentation
- `refactor` - Refactoring
- `test` - Tests
- `chore` - Maintenance

**Exemples** :
```
feat: add checksum verification feature
fix: resolve OAuth callback redirect loop
docs: update API documentation for signatures
refactor: simplify signature service logic
test: add integration tests for expected signers
```

### Code Review

**Checklist** :
- ✅ Tests passent (CI green)
- ✅ Code formaté (`go fmt`, `eslint`)
- ✅ Pas de secrets commitées
- ✅ Documentation à jour
- ✅ Traductions complètes (i18n)

## Troubleshooting

### Backend ne démarre pas

```bash
# Vérifier PostgreSQL
docker compose ps ackify-db
docker compose logs ackify-db

# Vérifier les variables d'env
cat .env

# Logs détaillés
ACKIFY_LOG_LEVEL=debug ./community
```

### Frontend build échoue

```bash
# Nettoyer et réinstaller
rm -rf node_modules package-lock.json
npm install

# Vérifier Node version
node --version  # Doit être 22+
```

### Tests échouent

```bash
# Backend - vérifier PostgreSQL test
docker compose -f compose.test.yml ps

# Frontend - vérifier types
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
