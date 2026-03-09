# Getting Started

Guide d'installation et de configuration d'Ackify avec Docker Compose.

## Prérequis

- Docker et Docker Compose installés
- Un domaine (ou localhost pour les tests)
- Credentials OAuth2 (Google, GitHub, GitLab, ou custom)

## Installation Rapide

### 1. Cloner le dépôt

```bash
git clone https://github.com/kolapsis/ackify.git
cd ackify-ce
```

### 2. Configuration

Copier le fichier d'exemple et l'éditer :

```bash
cp .env.example .env
nano .env
```

**Variables obligatoires minimales** :

```bash
# Domaine public de votre instance
APP_DNS=sign.your-domain.com
ACKIFY_BASE_URL=https://sign.your-domain.com
ACKIFY_ORGANISATION="Your Organization Name"

# Base de données PostgreSQL
POSTGRES_USER=ackifyr
POSTGRES_PASSWORD=your_secure_password_here
POSTGRES_DB=ackify

# OAuth2 (exemple avec Google)
ACKIFY_OAUTH_PROVIDER=google
ACKIFY_OAUTH_CLIENT_ID=your_google_client_id
ACKIFY_OAUTH_CLIENT_SECRET=your_google_client_secret

# Sécurité - générer avec: openssl rand -base64 32
ACKIFY_OAUTH_COOKIE_SECRET=your_base64_encoded_secret_key
```

### 3. Démarrage

```bash
docker compose up -d
```

Cette commande va :
- Télécharger les images Docker nécessaires
- Démarrer PostgreSQL avec healthcheck
- Appliquer les migrations de base de données
- Lancer l'application Ackify

### 4. Vérification

```bash
# Voir les logs
docker compose logs -f ackify-ce

# Vérifier le health check
curl http://localhost:8080/api/v1/health
# Attendu: {"status":"healthy","database":"connected"}
```

### 5. Accès à l'interface

Ouvrir votre navigateur :
- **Interface publique** : http://localhost:8080
- **Admin dashboard** : http://localhost:8080/admin (nécessite email dans ACKIFY_ADMIN_EMAILS)

## Configuration OAuth2

Avant de pouvoir utiliser Ackify, configurez votre provider OAuth2.

### Google OAuth2

1. Aller sur [Google Cloud Console](https://console.cloud.google.com/)
2. Créer un nouveau projet ou sélectionner un projet existant
3. Activer l'API "Google+ API"
4. Créer des credentials OAuth 2.0 :
   - Type : Web application
   - Authorized redirect URIs : `https://sign.your-domain.com/api/v1/auth/callback`
5. Copier le Client ID et Client Secret dans `.env`

```bash
ACKIFY_OAUTH_PROVIDER=google
ACKIFY_OAUTH_CLIENT_ID=123456789-abc.apps.googleusercontent.com
ACKIFY_OAUTH_CLIENT_SECRET=GOCSPX-xyz...
```

### GitHub OAuth2

1. Aller sur [GitHub Developer Settings](https://github.com/settings/developers)
2. Créer une nouvelle OAuth App
3. Configuration :
   - Homepage URL : `https://sign.your-domain.com`
   - Callback URL : `https://sign.your-domain.com/api/v1/auth/callback`
4. Générer un client secret

```bash
ACKIFY_OAUTH_PROVIDER=github
ACKIFY_OAUTH_CLIENT_ID=Iv1.abc123
ACKIFY_OAUTH_CLIENT_SECRET=ghp_xyz...
```

Voir [OAuth Providers](configuration/oauth-providers.md) pour GitLab et custom providers.

## Génération des Secrets

```bash
# Cookie secret (obligatoire)
openssl rand -base64 32

# Ed25519 private key (optionnel, auto-généré si absent)
openssl rand -base64 64
```

## Premiers Pas

### Créer votre première signature

1. Accéder à `http://localhost:8080/?doc=test_document`
2. Cliquer sur "Sign this document"
3. Se connecter via OAuth2
4. Valider la signature

### Accéder au dashboard admin

1. Ajouter votre email dans `.env` :
   ```bash
   ACKIFY_ADMIN_EMAILS=admin@company.com
   ```
2. Redémarrer :
   ```bash
   docker compose restart ackify-ce
   ```
3. Se connecter puis accéder à `/admin`

### Intégrer dans une page

```html
<!-- Widget embeddable -->
<iframe src="https://sign.your-domain.com/?doc=test_document"
        width="600" height="200"
        frameborder="0"
        style="border: 1px solid #ddd; border-radius: 6px;"></iframe>
```

## Commandes Utiles

```bash
# Voir les logs
docker compose logs -f ackify-ce

# Redémarrer
docker compose restart ackify-ce

# Arrêter
docker compose down

# Reconstruire après modifications
docker compose up -d --force-recreate ackify-ce --build

# Accéder à la base de données
docker compose exec ackify-db psql -U ackifyr -d ackify
```

## Troubleshooting

### L'application ne démarre pas

```bash
# Vérifier les logs
docker compose logs ackify-ce

# Vérifier la santé de PostgreSQL
docker compose ps ackify-db
```

### Erreur de migration

```bash
# Relancer les migrations manuellement
docker compose up ackify-migrate
```

### OAuth callback error

Vérifier que :
- `ACKIFY_BASE_URL` correspond exactement à votre domaine
- La callback URL dans le provider OAuth2 est correcte
- Le cookie secret est bien configuré

## Next Steps

- [Configuration complète](configuration.md)
- [Déploiement en production](deployment.md)
- [Configuration des fonctionnalités](features/)
- [API Reference](api.md)
