# Guide Administrateur

Guide complet pour les administrateurs utilisant Ackify pour gérer les documents, les signataires attendus et les rappels email.

## Table des Matières

- [Obtenir l'accès Admin](#obtenir-laccès-admin)
- [Dashboard Admin](#dashboard-admin)
- [Gestion des Documents](#gestion-des-documents)
- [Signataires Attendus](#signataires-attendus)
- [Rappels Email](#rappels-email)
- [Monitoring & Statistiques](#monitoring--statistiques)
- [Bonnes Pratiques](#bonnes-pratiques)
- [Dépannage](#dépannage)

---

## Obtenir l'accès Admin

### Prérequis

Pour accéder aux fonctionnalités admin, votre email doit être configuré dans la variable d'environnement `ACKIFY_ADMIN_EMAILS`.

```bash
# Dans le fichier .env
ACKIFY_ADMIN_EMAILS=admin@company.com,manager@company.com
```

**Après ajout de votre email:**
1. Redémarrer Ackify: `docker compose restart ackify-ce`
2. Se déconnecter et se reconnecter
3. Vous devriez maintenant voir le lien "Admin" dans la navigation

### Vérifier l'accès Admin

Visitez `/admin` - si vous voyez le dashboard admin, vous avez l'accès admin.

---

## Dashboard Admin

**URL**: `/admin`

Le dashboard admin fournit:
- **Total Documents**: Nombre de documents dans le système
- **Lecteurs Attendus**: Nombre total de signataires attendus sur tous les documents
- **Documents Actifs**: Documents non supprimés
- **Liste Documents**: Liste paginée (20 par page) avec recherche

### Fonctionnalités du Dashboard

#### Statistiques Rapides
Trois cartes KPI en haut:
- Nombre total de documents
- Total des lecteurs/signataires attendus
- Documents actifs (non supprimés)

#### Recherche de Documents
- Recherche par titre, ID document ou URL
- Filtrage en temps réel

#### Liste des Documents
**Vue desktop** - Tableau avec colonnes:
- ID Document
- Titre
- URL
- Date de création
- Créateur
- Actions (Voir détails)

**Vue mobile** - Mise en page carte avec:
- ID document et titre
- Info création
- Tap pour voir détails

#### Pagination
- 20 documents par page
- Boutons Précédent/Suivant
- Indicateur de page actuelle

---

## Gestion des Documents

### Créer un Document

**Depuis le Dashboard Admin:**
1. Cliquer sur le bouton "Créer Nouveau Document"
2. Remplir le formulaire:
   - **Référence** (requis): URL, chemin fichier ou ID personnalisé
   - **Titre** (optionnel): Auto-généré depuis l'URL si vide
   - **Description** (optionnel): Contexte additionnel
3. Cliquer sur "Créer Document"

**Fonctionnalités Automatiques:**
- **Génération ID Unique**: doc_id base36 résistant aux collisions
- **Extraction Titre**: Auto-extrait de l'URL si non fourni
- **Calcul Checksum**: Pour URLs distantes (si admin et fichier < 10MB)

**Exemple:**
```
Référence: https://docs.company.com/politique-2025.pdf
Titre: Politique de Sécurité 2025 (auto-extrait ou manuel)
Description: Politique annuelle de conformité sécurité
```

**Résultat:**
- doc_id: `k7m2n4p8` (auto-généré)
- Checksum: SHA-256 auto-calculé (si URL accessible)

### Voir les Détails d'un Document

**URL**: `/admin/docs/{docId}`

Fournit des informations complètes sur le document:

#### 1. **Section Métadonnées**
Éditer les informations du document:
- Titre
- URL
- Description
- Checksum (SHA-256, SHA-512 ou MD5)
- Algorithme Checksum

**Pour éditer:**
1. Cliquer sur "Éditer Métadonnées"
2. Modifier les champs
3. Cliquer sur "Sauvegarder"
4. Modal de confirmation pour changements critiques (checksum, algorithme)

#### 2. **Panneau Statistiques**
Suivi des signatures en temps réel:
- **Attendus**: Nombre de signataires attendus
- **Signés**: Nombre ayant signé
- **En Attente**: Pas encore signé
- **Complétion**: Pourcentage complété

#### 3. **Section Signataires Attendus**
Liste tous les signataires attendus avec statut:
- **Email**: Adresse email du signataire
- **Statut**: ✅ Signé ou ⏳ En attente
- **Ajouté**: Date d'ajout à la liste attendue
- **Jours Depuis Ajout**: Suivi du temps
- **Dernier Rappel**: Quand le dernier rappel a été envoyé
- **Nb Rappels**: Total rappels envoyés
- **Actions**: Bouton retirer signataire

**Code couleur:**
- Fond vert: Signataire a signé
- Fond par défaut: Signature en attente

#### 4. **Signatures Inattendues**
Affiche les utilisateurs ayant signé mais pas sur la liste attendue:
- Email utilisateur
- Date de signature
- Indique participation organique/inattendue

#### 5. **Actions**
- **Envoyer Rappels**: Emailer les signataires en attente
- **Partager Lien**: Générer et copier lien de signature
- **Supprimer Document**: Suppression douce (préserve historique signatures)

### Mettre à Jour les Métadonnées du Document

**Champs importants:**

**Titre & Description:**
- Peuvent être changés librement
- Pas de confirmation requise

**URL:**
- Met à jour où le document est localisé
- Modal de confirmation affiché

**Checksum & Algorithme:**
- Critique pour vérification d'intégrité
- Modal de confirmation avertit de l'impact
- Changer uniquement si version du document a changé

**Workflow:**
1. Cliquer sur "Éditer Métadonnées"
2. Modifier les champs désirés
3. Cliquer sur "Sauvegarder"
4. Si checksum/algorithme changé, confirmer dans modal
5. Notification de succès affichée

### Supprimer un Document

**Comportement Suppression Douce:**
- Document marqué comme supprimé (timestamp `deleted_at` défini)
- Historique signatures préservé
- Document n'apparaît plus dans listes publiques
- Admin peut toujours voir via URL directe
- Signatures CASCADE update (marquées avec `doc_deleted_at`)

**Pour supprimer:**
1. Aller sur page détail document (`/admin/docs/{docId}`)
2. Cliquer sur bouton "Supprimer Document"
3. Confirmer suppression dans modal
4. Document déplacé en état supprimé

**Note**: Il n'y a pas de "restauration" - c'est une suppression douce permanente.

---

## Signataires Attendus

Les signataires attendus sont les utilisateurs que vous souhaitez suivre pour la complétion du document.

### Ajouter des Signataires Attendus

**Depuis la page détail document:**
1. Défiler jusqu'à la section "Signataires Attendus"
2. Cliquer sur bouton "Ajouter Signataire Attendu"
3. Entrer adresse(s) email:
   - Simple: `alice@company.com`
   - Multiple: Séparées par virgules `alice@company.com,bob@company.com`
4. Optionnellement ajouter des notes
5. Cliquer sur "Ajouter"

**Endpoint API:**
```http
POST /api/v1/admin/documents/{docId}/signers
Content-Type: application/json
X-CSRF-Token: {token}

{
  "emails": ["alice@company.com", "bob@company.com"],
  "notes": "Membres du conseil - Q1 2025"
}
```

**Contraintes:**
- Email doit avoir format valide
- Contrainte UNIQUE: Impossible d'ajouter même email deux fois au même document
- Ajouté par admin utilisateur actuel (suivi dans `added_by`)

### Retirer des Signataires Attendus

**Depuis la page détail document:**
1. Trouver signataire dans liste Signataires Attendus
2. Cliquer sur bouton "Retirer" à côté de leur email
3. Confirmer le retrait

**Endpoint API:**
```http
DELETE /api/v1/admin/documents/{docId}/signers/{email}
X-CSRF-Token: {token}
```

**Effet:**
- Signataire retiré de la liste attendue
- NE supprime PAS leur signature s'ils ont déjà signé
- Historique des rappels préservé dans `reminder_logs`

### Suivre le Statut de Complétion

**API Statut Document:**
```http
GET /api/v1/admin/documents/{docId}/status
```

**Réponse:**
```json
{
  "docId": "abc123",
  "expectedCount": 10,
  "signedCount": 7,
  "pendingCount": 3,
  "completionPercentage": 70.0
}
```

**Indicateurs visuels:**
- Barre de progression montrant pourcentage complétion
- Statut code couleur: Vert (signé), Orange (en attente)
- Jours depuis ajout (aide identifier signataires lents)

---

## Rappels Email

Les rappels email sont envoyés de manière asynchrone via le système `email_queue`.

### Envoyer des Rappels

**Depuis la page détail document:**
1. Cliquer sur bouton "Envoyer Rappels"
2. Modal s'ouvre avec options:
   - **Envoyer à**: Tous en attente OU emails spécifiques
   - **URL Document**: Pré-rempli, peut personnaliser
   - **Langue**: en, fr, es, de, it
3. Cliquer sur "Envoyer Rappels"
4. Confirmation: "X rappels mis en file pour envoi"

**Endpoint API:**
```http
POST /api/v1/admin/documents/{docId}/reminders
Content-Type: application/json
X-CSRF-Token: {token}

{
  "emails": ["alice@company.com"],  // Optionnel: emails spécifiques
  "docURL": "https://docs.company.com/politique.pdf",
  "locale": "fr"
}
```

**Comportement:**
- Envoie à TOUS les signataires en attente si `emails` non spécifié
- Envoie aux `emails` spécifiques si fournis (même si déjà signé)
- Emails mis en file dans table `email_queue`
- Worker background traite la file
- Retry en cas d'échec (3 tentatives, exponential backoff)

### Templates Email

**Emplacement**: `backend/templates/emails/`

**Templates disponibles:**
- `reminder.html` - Version HTML
- `reminder.txt` - Version texte simple

**Variables disponibles dans les templates:**
- `{{.DocTitle}}` - Titre document
- `{{.DocURL}}` - URL document
- `{{.RecipientEmail}}` - Email destinataire
- `{{.SenderName}}` - Admin ayant envoyé rappel
- `{{.OrganisationName}}` - Depuis ACKIFY_ORGANISATION

**Locales**: en, fr, es, de, it
- Répertoire template: `templates/emails/{locale}/`
- Fallback vers locale par défaut si traduction manquante

### Historique des Rappels

**Voir log des rappels:**
```http
GET /api/v1/admin/documents/{docId}/reminders
```

**Réponse:**
```json
{
  "reminders": [
    {
      "id": 123,
      "docId": "abc123",
      "recipientEmail": "alice@company.com",
      "sentAt": "2025-01-15T10:30:00Z",
      "sentBy": "admin@company.com",
      "templateUsed": "reminder",
      "status": "sent",
      "errorMessage": null
    }
  ]
}
```

**Valeurs de statut:**
- `queued` - Dans email_queue, pas encore traité
- `sent` - Livré avec succès
- `failed` - Échec de livraison (voir errorMessage)
- `bounced` - Email retourné

**Suivi:**
- Date dernier rappel envoyé affiché par signataire
- Nombre rappels affiché par signataire
- Aide éviter sur-envoi

### Monitoring de la File Email

**Vérifier statut de la file (PostgreSQL):**
```sql
-- Emails en attente
SELECT id, to_addresses, subject, status, scheduled_for
FROM email_queue
WHERE status IN ('pending', 'processing')
ORDER BY priority DESC, scheduled_for ASC;

-- Emails échoués
SELECT id, to_addresses, last_error, retry_count
FROM email_queue
WHERE status = 'failed';
```

**Configuration du worker:**
- Taille lot: 10 emails
- Intervalle polling: 5 secondes
- Max retries: 3
- Cleanup: Rétention 7 jours

---

## Monitoring & Statistiques

### Statistiques Niveau Document

**Suivi complétion:**
- Nombre Attendus vs Signés
- Liste signataires en attente
- Pourcentage complétion
- Temps moyen pour signer

**Efficacité des rappels:**
- Nombre rappels envoyés
- Taux succès/échec
- Temps entre rappel et signature

### Métriques Système Global

**Requêtes PostgreSQL:**

```sql
-- Total documents
SELECT COUNT(*) FROM documents WHERE deleted_at IS NULL;

-- Total signatures
SELECT COUNT(*) FROM signatures;

-- Documents par statut complétion
SELECT
  CASE
    WHEN signed_count = expected_count THEN '100%'
    WHEN signed_count >= expected_count * 0.75 THEN '75-99%'
    WHEN signed_count >= expected_count * 0.50 THEN '50-74%'
    ELSE '<50%'
  END as completion_bracket,
  COUNT(*) as doc_count
FROM (
  SELECT
    d.doc_id,
    COUNT(DISTINCT es.email) as expected_count,
    COUNT(DISTINCT s.user_email) as signed_count
  FROM documents d
  LEFT JOIN expected_signers es ON d.doc_id = es.doc_id
  LEFT JOIN signatures s ON d.doc_id = s.doc_id AND s.user_email = es.email
  WHERE d.deleted_at IS NULL
  GROUP BY d.doc_id
) stats
GROUP BY completion_bracket;

-- Statistiques file email
SELECT status, COUNT(*), MIN(created_at), MAX(created_at)
FROM email_queue
GROUP BY status;
```

### Exporter les Données

**Signatures pour un document:**
```sql
COPY (
  SELECT s.user_email, s.user_name, s.signed_at, s.payload_hash
  FROM signatures s
  WHERE s.doc_id = 'votre_doc_id'
  ORDER BY s.signed_at
) TO '/tmp/signatures_export.csv' WITH CSV HEADER;
```

**Statut signataires attendus:**
```sql
COPY (
  SELECT
    es.email,
    CASE WHEN s.id IS NOT NULL THEN 'Signé' ELSE 'En attente' END as status,
    es.added_at,
    s.signed_at
  FROM expected_signers es
  LEFT JOIN signatures s ON es.doc_id = s.doc_id AND es.email = s.user_email
  WHERE es.doc_id = 'votre_doc_id'
) TO '/tmp/expected_signers_export.csv' WITH CSV HEADER;
```

---

## Bonnes Pratiques

### 1. Création de Documents

✅ **À Faire:**
- Utiliser titres descriptifs
- Ajouter descriptions claires
- Inclure URL document pour accès facile
- Stocker checksum pour vérification intégrité
- Créer liste signataires attendus avant partage

❌ **À Éviter:**
- Utiliser titres génériques comme "Document 1"
- Laisser URL vide si document accessible en ligne
- Changer checksums sauf si document réellement changé

### 2. Gestion Signataires Attendus

✅ **À Faire:**
- Ajouter signataires attendus avant d'envoyer lien document
- Utiliser notes claires pour expliquer pourquoi signataires attendus
- Réviser signataires en attente régulièrement
- Retirer signataires qui ne sont plus pertinents

❌ **À Éviter:**
- Ajouter centaines de signataires d'un coup (utiliser lots)
- Envoyer rappels trop fréquemment (max une fois par semaine)
- Retirer signataires ayant déjà signé (préserver historique)

### 3. Rappels Email

✅ **À Faire:**
- Attendre 3-5 jours avant premier rappel
- Envoyer dans langue préférée du destinataire
- Inclure titre et URL document clairs
- Suivre historique rappels pour éviter spam
- Envoyer rappels pendant heures bureau

❌ **À Éviter:**
- Envoyer rappels quotidiens (cause fatigue)
- Envoyer sans vérifier si déjà signé
- Utiliser sujets génériques (personnaliser avec titre doc)
- Envoyer hors heures bureau

### 4. Intégrité des Données

✅ **À Faire:**
- Sauvegarder régulièrement base PostgreSQL
- Vérifier checksums correspondent documents réels
- Monitorer file email pour échecs
- Réviser signatures inattendues (peut indiquer intérêt plus large)
- Exporter données signatures importantes

❌ **À Éviter:**
- Supprimer documents avec signatures actives
- Modifier timestamps manuellement en base
- Ignorer échecs livraison email
- Changer checksums sans mettre à jour document

### 5. Sécurité

✅ **À Faire:**
- Limiter accès admin aux utilisateurs de confiance uniquement
- Utiliser HTTPS en production (`ACKIFY_BASE_URL=https://...`)
- Tourner `ACKIFY_OAUTH_COOKIE_SECRET` périodiquement
- Monitorer actions admin via logs application
- Utiliser restrictions domaine OAuth autorisé

❌ **À Éviter:**
- Partager identifiants admin
- Fonctionner sans HTTPS en production
- Désactiver protection CSRF
- Ignorer échecs authentification dans logs

---

## Dépannage

### Problèmes Courants

#### 1. Lien Admin Non Visible

**Problème**: Ne peut pas voir lien "Admin" dans navigation

**Solutions:**
- Vérifier email dans variable `ACKIFY_ADMIN_EMAILS`
- Redémarrer Ackify: `docker compose restart ackify-ce`
- Se déconnecter et se reconnecter
- Vérifier logs: `docker compose logs ackify-ce | grep admin`

#### 2. Emails Non Envoyés

**Problème**: Rappels mis en file mais pas livrés

**Diagnostic:**
```sql
SELECT * FROM email_queue WHERE status = 'failed' ORDER BY created_at DESC LIMIT 10;
```

**Solutions:**
- Vérifier configuration SMTP (`ACKIFY_MAIL_HOST`, `ACKIFY_MAIL_USERNAME`, etc.)
- Vérifier identifiants SMTP corrects
- Vérifier logs worker email: `docker compose logs ackify-ce | grep email`
- S'assurer `ACKIFY_MAIL_FROM` est adresse expéditeur valide
- Tester connexion SMTP manuellement

#### 3. Erreur Signataire Dupliqué

**Problème**: "Email existe déjà comme signataire attendu"

**Cause**: Contrainte UNIQUE sur (doc_id, email)

**Solution**: Comportement attendu - chaque email ne peut être ajouté qu'une fois par document

#### 4. Checksum Non Correspondant

**Problème**: Utilisateurs rapportent checksum ne correspond pas

**Solutions:**
- Vérifier checksum stocké correspond document réel
- Vérifier algorithme utilisé (SHA-256, SHA-512, MD5)
- Recalculer checksum et mettre à jour via Éditer Métadonnées
- S'assurer utilisateurs téléchargent version correcte

#### 5. Document N'apparaît Pas

**Problème**: Document créé n'apparaît pas dans liste

**Solutions:**
- Vérifier si document supprimé en douce (`deleted_at IS NOT NULL`)
- Vérifier création réussie (vérifier réponse/logs)
- Vider cache navigateur
- Vérifier base: `SELECT * FROM documents WHERE doc_id = 'votre_id';`

#### 6. Signature Déjà Existe

**Problème**: Utilisateur ne peut pas signer document à nouveau

**Cause**: Contrainte UNIQUE (doc_id, user_sub) - une signature par utilisateur par document

**Solution**: Comportement attendu - utilisateurs ne peuvent pas signer même document deux fois

### Obtenir de l'Aide

**Logs:**
```bash
# Logs application
docker compose logs -f ackify-ce

# Logs base de données
docker compose logs -f ackify-db

# Logs worker email (grep email)
docker compose logs ackify-ce | grep -i email
```

**Inspection base de données:**
```bash
# Se connecter à PostgreSQL
docker compose exec ackify-db psql -U ackifyr ackify

# Requêtes utiles
SELECT * FROM documents ORDER BY created_at DESC LIMIT 10;
SELECT * FROM expected_signers WHERE doc_id = 'votre_doc_id';
SELECT * FROM email_queue WHERE status != 'sent' ORDER BY created_at DESC;
```

**Rapporter problèmes:**
- GitHub: https://github.com/kolapsis/ackify/issues
- Inclure logs et messages erreur
- Décrire comportement attendu vs réel

---

## Référence Rapide

### Variables Environnement
```bash
ACKIFY_ADMIN_EMAILS=admin@company.com
ACKIFY_MAIL_HOST=smtp.gmail.com
ACKIFY_MAIL_FROM=noreply@company.com
```

### Endpoints Clés
```
GET  /admin                              # Dashboard
GET  /admin/docs/{docId}                 # Détail document
POST /admin/documents/{docId}/signers    # Ajouter signataire
POST /admin/documents/{docId}/reminders  # Envoyer rappels
PUT  /admin/documents/{docId}/metadata   # Mettre à jour métadonnées
```

### Tables Importantes
- `documents` - Métadonnées documents
- `signatures` - Signatures utilisateurs
- `expected_signers` - Qui doit signer
- `reminder_logs` - Historique emails
- `email_queue` - File email async

### Raccourcis Clavier (Frontend)
- Barre recherche auto-focus sur dashboard
- Entrée pour soumettre formulaires
- Échap pour fermer modales

---

**Dernière Mise à Jour**: 2025-10-26
**Version**: 1.0.0
