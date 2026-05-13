// SPDX-License-Identifier: AGPL-3.0-or-later
package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"gopkg.in/yaml.v3"

	"github.com/kolapsis/ackify/backend/internal/application/services"
	apiAdmin "github.com/kolapsis/ackify/backend/internal/presentation/api/admin"
	apiAuth "github.com/kolapsis/ackify/backend/internal/presentation/api/auth"
	apiConfig "github.com/kolapsis/ackify/backend/internal/presentation/api/config"
	"github.com/kolapsis/ackify/backend/internal/presentation/api/documents"
	"github.com/kolapsis/ackify/backend/internal/presentation/api/health"
	"github.com/kolapsis/ackify/backend/internal/presentation/api/proxy"
	"github.com/kolapsis/ackify/backend/internal/presentation/api/shared"
	"github.com/kolapsis/ackify/backend/internal/presentation/api/signatures"
	apiStorage "github.com/kolapsis/ackify/backend/internal/presentation/api/storage"
	"github.com/kolapsis/ackify/backend/internal/presentation/api/users"
	"github.com/kolapsis/ackify/backend/pkg/models"
	"github.com/kolapsis/ackify/backend/pkg/providers"
	"github.com/kolapsis/ackify/backend/pkg/storage"
)

// magicLinkService defines magic link authentication operations
type magicLinkService interface {
	RequestMagicLink(ctx context.Context, email, redirectTo, ip, userAgent, locale string) error
	VerifyMagicLink(ctx context.Context, token, ip, userAgent string) (*models.MagicLinkToken, error)
	VerifyReminderAuthToken(ctx context.Context, token, ip, userAgent string) (*models.MagicLinkToken, error)
}

// signatureService defines signature operations
type signatureService interface {
	CreateSignature(ctx context.Context, request *models.SignatureRequest) error
	GetDocumentSignatures(ctx context.Context, docID string) ([]*models.Signature, error)
	GetSignatureStatus(ctx context.Context, docID string, user *models.User) (*models.SignatureStatus, error)
	GetSignatureByDocAndUser(ctx context.Context, docID string, user *models.User) (*models.Signature, error)
	GetUserSignatures(ctx context.Context, user *models.User) ([]*models.Signature, error)
}

// documentService defines document operations
type documentService interface {
	CreateDocument(ctx context.Context, req services.CreateDocumentRequest) (*models.Document, error)
	FindOrCreateDocument(ctx context.Context, ref string, createdBy string) (*models.Document, bool, error)
	FindByReference(ctx context.Context, ref string, refType string) (*models.Document, error)
	List(ctx context.Context, limit, offset int) ([]*models.Document, error)
	Search(ctx context.Context, query string, limit, offset int) ([]*models.Document, error)
	Count(ctx context.Context, searchQuery string) (int, error)
	GetByDocID(ctx context.Context, docID string) (*models.Document, error)
	GetExpectedSignerStats(ctx context.Context, docID string) (*models.DocCompletionStats, error)
	ListExpectedSigners(ctx context.Context, docID string) ([]*models.ExpectedSigner, error)
	ListByCreatedBy(ctx context.Context, createdBy string, limit, offset int) ([]*models.Document, error)
	SearchByCreatedBy(ctx context.Context, createdBy, query string, limit, offset int) ([]*models.Document, error)
	CountByCreatedBy(ctx context.Context, createdBy, searchQuery string) (int, error)
	GetAggregateDocumentStats(ctx context.Context, createdBy string) (pending, completed int, err error)
	FindPendingDocumentsForEmail(ctx context.Context, email string) ([]*models.PendingDocument, error)
}

// reminderService defines reminder operations
type reminderService interface {
	SendReminders(ctx context.Context, docID, sentBy string, specificEmails []string, docURL, locale string) (*models.ReminderSendResult, error)
	GetReminderHistory(ctx context.Context, docID string) ([]*models.ReminderLog, error)
	GetReminderStats(ctx context.Context, docID string) (*models.ReminderStats, error)
}

// webhookPublisher defines webhook publish operations
type webhookPublisher interface {
	Publish(ctx context.Context, eventType string, payload map[string]interface{}) error
}

// adminService defines admin-level document and signer operations
type adminService interface {
	GetDocument(ctx context.Context, docID string) (*models.Document, error)
	ListDocuments(ctx context.Context, limit, offset int) ([]*models.Document, error)
	SearchDocuments(ctx context.Context, query string, limit, offset int) ([]*models.Document, error)
	CountDocuments(ctx context.Context, searchQuery string) (int, error)
	UpdateDocumentMetadata(ctx context.Context, docID string, input models.DocumentInput, updatedBy string) (*models.Document, error)
	DeleteDocument(ctx context.Context, docID string) error
	ListExpectedSigners(ctx context.Context, docID string) ([]*models.ExpectedSigner, error)
	ListExpectedSignersWithStatus(ctx context.Context, docID string) ([]*models.ExpectedSignerWithStatus, error)
	AddExpectedSigners(ctx context.Context, docID string, contacts []models.ContactInfo, addedBy string) error
	RemoveExpectedSigner(ctx context.Context, docID, email string) error
	GetSignerStats(ctx context.Context, docID string) (*models.DocCompletionStats, error)
	GetAggregateDocumentStats(ctx context.Context) (pending, completed int, err error)
}

// webhookService defines webhook management operations
type webhookService interface {
	CreateWebhook(ctx context.Context, input models.WebhookInput) (*models.Webhook, error)
	UpdateWebhook(ctx context.Context, id int64, input models.WebhookInput) (*models.Webhook, error)
	SetWebhookActive(ctx context.Context, id int64, active bool) error
	DeleteWebhook(ctx context.Context, id int64) error
	GetWebhookByID(ctx context.Context, id int64) (*models.Webhook, error)
	ListWebhooks(ctx context.Context, limit, offset int) ([]*models.Webhook, error)
	ListDeliveries(ctx context.Context, webhookID int64, limit, offset int) ([]*models.WebhookDelivery, error)
}

// configService defines configuration management operations
type configService interface {
	Reload(ctx context.Context) error
	GetConfig() *models.MutableConfig
	UpdateSection(ctx context.Context, category models.ConfigCategory, input json.RawMessage, updatedBy string) error
	TestSMTP(ctx context.Context, cfg models.SMTPConfig) error
	TestS3(ctx context.Context, cfg models.StorageConfig) error
	TestOIDC(ctx context.Context, cfg models.OIDCConfig) error
	ResetFromENV(ctx context.Context, updatedBy string) error
}

// RouterConfig holds configuration for the API router
// QuotaEnforcer checks and records quota usage using generic action-based methods.
type QuotaEnforcer interface {
	Check(ctx context.Context, tenantID string, action string) error
	Record(ctx context.Context, tenantID string, action string) error
}

type RouterConfig struct {
	// Database for RLS middleware
	DB             *sql.DB                  // Required for RLS transaction management
	TenantProvider providers.TenantProvider // Required for tenant context

	// Capability providers
	// AuthProvider handles all auth methods (sessions, OIDC, MagicLink) dynamically
	AuthProvider  providers.AuthProvider // Required - unified auth provider
	Authorizer    providers.Authorizer   // Required for authorization decisions
	QuotaEnforcer QuotaEnforcer          // Optional - quota enforcement (SaaS)
	AuditLogger   shared.AuditLogger     // Optional - audit logging (SaaS)

	// Services
	SignatureService signatureService
	DocumentService  documentService
	AdminService     adminService
	ReminderService  reminderService
	WebhookService   webhookService
	WebhookPublisher webhookPublisher
	ConfigService    configService

	// Storage
	StorageProvider     storage.Provider               // Optional, for document file storage
	StorageMaxSizeMB    int64                          // Maximum upload size in MB
	StorageQuotaChecker apiStorage.StorageQuotaChecker // Optional, for storage quota enforcement (SaaS)

	// Configuration
	BaseURL              string
	AllowedRedirectHosts []string // Optional allowlist of external hosts accepted as post-auth redirect targets
	AuthRateLimit        int      // Global auth rate limit (requests per minute), default: 5
	DocumentRateLimit    int      // Document creation rate limit (requests per minute), default: 10
	GeneralRateLimit     int      // General API rate limit (requests per minute), default: 100
	ImportMaxSigners     int      // Maximum signers per CSV import, default: 500
}

// quotaRecorderAdapter adapts generic QuotaEnforcer to handler-specific QuotaRecorder interfaces.
// It implements both documents.QuotaRecorder, admin.QuotaRecorder, and signatures.QuotaRecorder.
type quotaRecorderAdapter struct {
	enforcer QuotaEnforcer
}

// documents.QuotaRecorder

func (a *quotaRecorderAdapter) CheckDocumentCreation(ctx context.Context, tenantID string) error {
	return a.enforcer.Check(ctx, tenantID, "document.create")
}

func (a *quotaRecorderAdapter) RecordDocumentCreation(ctx context.Context, tenantID string) error {
	return a.enforcer.Record(ctx, tenantID, "document.create")
}

func (a *quotaRecorderAdapter) RecordDocumentDeletion(ctx context.Context, tenantID string) error {
	return a.enforcer.Record(ctx, tenantID, "document.delete")
}

func (a *quotaRecorderAdapter) CheckSignerAdd(ctx context.Context, tenantID string) error {
	return a.enforcer.Check(ctx, tenantID, "signer.add")
}

func (a *quotaRecorderAdapter) RecordSignerAdd(ctx context.Context, tenantID string) error {
	return a.enforcer.Record(ctx, tenantID, "signer.add")
}

// admin.QuotaRecorder

func (a *quotaRecorderAdapter) CheckReminderSend(ctx context.Context, tenantID string) error {
	return a.enforcer.Check(ctx, tenantID, "reminder.send")
}

func (a *quotaRecorderAdapter) RecordReminderSend(ctx context.Context, tenantID string) error {
	return a.enforcer.Record(ctx, tenantID, "reminder.send")
}

// signatures.QuotaRecorder

func (a *quotaRecorderAdapter) CheckSignatureCreation(ctx context.Context, tenantID string) error {
	return a.enforcer.Check(ctx, tenantID, "signature.create")
}

func (a *quotaRecorderAdapter) RecordSignatureCreation(ctx context.Context, tenantID string) error {
	return a.enforcer.Record(ctx, tenantID, "signature.create")
}

// admin.WebhookQuotaRecorder

func (a *quotaRecorderAdapter) CheckWebhookCreate(ctx context.Context, tenantID string) error {
	return a.enforcer.Check(ctx, tenantID, "webhook.create")
}

// NewRouter creates and configures the API v1 router
func NewRouter(cfg RouterConfig) *chi.Mux {
	r := chi.NewRouter()

	// Initialize middleware with providers
	apiMiddleware := shared.NewMiddleware(cfg.AuthProvider, cfg.BaseURL, cfg.Authorizer)

	// Rate limiters with configurable limits
	authLimit := cfg.AuthRateLimit
	if authLimit == 0 {
		authLimit = 5 // Default: 5 attempts per minute for auth
	}
	documentLimit := cfg.DocumentRateLimit
	if documentLimit == 0 {
		documentLimit = 10 // Default: 10 documents per minute
	}
	generalLimit := cfg.GeneralRateLimit
	if generalLimit == 0 {
		generalLimit = 100 // Default: 100 requests per minute general
	}

	authRateLimit := shared.NewRateLimit(authLimit, time.Minute)
	documentRateLimit := shared.NewRateLimit(documentLimit, time.Minute)
	generalRateLimit := shared.NewRateLimit(generalLimit, time.Minute)

	// Global middleware
	r.Use(middleware.RequestID)
	r.Use(shared.AddRequestIDToContext)
	r.Use(middleware.RealIP)
	r.Use(shared.RequestLogger)
	r.Use(middleware.Recoverer)
	r.Use(shared.SecurityHeaders)
	r.Use(apiMiddleware.CORS)
	r.Use(generalRateLimit.Middleware)

	// RLS middleware for database tenant isolation (always active)
	// Must be after Recoverer to handle panics, before handlers that use DB
	if cfg.DB != nil && cfg.TenantProvider != nil {
		rlsMiddleware := shared.NewRLSMiddleware(cfg.DB, cfg.TenantProvider)
		r.Use(rlsMiddleware.Handler)
	}

	// Initialize handlers
	healthHandler := health.NewHandler()
	configHandler := apiConfig.NewHandler(cfg.ConfigService)
	authHandler := apiAuth.NewHandler(cfg.AuthProvider, apiMiddleware, cfg.BaseURL, cfg.AllowedRedirectHosts)
	usersHandler := users.NewHandler(cfg.Authorizer)
	documentsHandler := documents.NewHandler(
		cfg.SignatureService,
		cfg.DocumentService,
		cfg.WebhookPublisher,
		cfg.Authorizer,
	).WithAdminService(cfg.AdminService, cfg.BaseURL)

	if cfg.StorageProvider != nil {
		documentsHandler.WithStorageProvider(cfg.StorageProvider)
	}
	if cfg.QuotaEnforcer != nil && cfg.TenantProvider != nil {
		documentsHandler.WithQuotaRecorder(
			&quotaRecorderAdapter{enforcer: cfg.QuotaEnforcer},
			cfg.TenantProvider,
		)
	}
	if cfg.AuditLogger != nil {
		documentsHandler.WithAuditLogger(cfg.AuditLogger)
	}
	signaturesHandler := signatures.NewHandler(cfg.SignatureService, cfg.AdminService, cfg.WebhookPublisher)
	if cfg.QuotaEnforcer != nil && cfg.TenantProvider != nil {
		signaturesHandler.WithQuotaRecorder(
			&quotaRecorderAdapter{enforcer: cfg.QuotaEnforcer},
			cfg.TenantProvider,
		)
	}
	if cfg.AuditLogger != nil {
		signaturesHandler.WithAuditLogger(cfg.AuditLogger)
	}
	proxyHandler := proxy.NewHandler(cfg.DocumentService)

	// Storage handler (optional - only if storage is configured)
	maxSizeMB := cfg.StorageMaxSizeMB
	if maxSizeMB == 0 {
		maxSizeMB = 50 // Default: 50 MB
	}
	storageHandler := apiStorage.NewHandler(cfg.StorageProvider, cfg.DocumentService, maxSizeMB)
	if cfg.StorageQuotaChecker != nil && cfg.TenantProvider != nil {
		storageHandler.WithStorageQuotaChecker(cfg.StorageQuotaChecker, func(ctx context.Context) string {
			tid, err := cfg.TenantProvider.CurrentTenant(ctx)
			if err != nil {
				return ""
			}
			return tid.String()
		})
	}

	// Public routes
	r.Group(func(r chi.Router) {
		// Health check
		r.Get("/health", healthHandler.HandleHealth)

		// Public configuration (smtpEnabled, storageEnabled, auth methods)
		r.Get("/config", configHandler.HandleGetConfig)

		// CSRF token
		r.Get("/csrf", authHandler.HandleGetCSRFToken)

		// Proxy for streaming external documents (has its own rate limiting)
		r.Get("/proxy", proxyHandler.HandleProxy)

		// Auth endpoints - all routes defined, handlers check if method is enabled
		r.Route("/auth", func(r chi.Router) {
			// Apply rate limiting to auth endpoints
			r.Group(func(r chi.Router) {
				r.Use(authRateLimit.Middleware)

				// OIDC endpoints (handler checks if enabled dynamically)
				r.Post("/start", authHandler.HandleStartOIDC)
				r.Get("/callback", authHandler.HandleOIDCCallback)
				r.Get("/check", authHandler.HandleAuthCheck)

				// Magic Link endpoints (handler checks if enabled dynamically)
				r.Post("/magic-link/request", authHandler.HandleRequestMagicLink)
				r.Get("/magic-link/verify", authHandler.HandleVerifyMagicLink)
				r.Get("/reminder-link/verify", authHandler.HandleVerifyReminderAuthLink)

				// Logout endpoint (always available)
				r.Get("/logout", authHandler.HandleLogout)
			})
		})

		// Public document endpoints
		r.Route("/documents", func(r chi.Router) {
			// Document creation (with CSRF and stricter rate limiting)
			r.Group(func(r chi.Router) {
				r.Use(apiMiddleware.CSRFProtect)
				r.Use(documentRateLimit.Middleware)
				r.Post("/", documentsHandler.HandleCreateDocument)
			})

			// Read-only document endpoints
			r.Get("/", documentsHandler.HandleListDocuments)
			r.Get("/{docId}", documentsHandler.HandleGetDocument)

			// Signatures and expected-signers: detailed list restricted to owner/admin
			r.Group(func(r chi.Router) {
				r.Use(apiMiddleware.OptionalAuth)
				r.Get("/{docId}/signatures", documentsHandler.HandleGetDocumentSignatures)
				r.Get("/{docId}/expected-signers", documentsHandler.HandleGetExpectedSigners)
			})

			// Find or create document by reference (public for embed support, but with optional auth)
			r.Group(func(r chi.Router) {
				r.Use(apiMiddleware.OptionalAuth)
				r.Get("/find-or-create", documentsHandler.HandleFindOrCreateDocument)
			})
		})

		// Storage configuration endpoint (public, tells frontend if storage is enabled)
		r.Get("/storage/config", storageHandler.HandleStorageConfig)
	})

	// Authenticated routes
	r.Group(func(r chi.Router) {
		r.Use(apiMiddleware.RequireAuth)
		r.Use(apiMiddleware.CSRFProtect)

		// User endpoints
		r.Route("/users", func(r chi.Router) {
			r.Get("/me", usersHandler.HandleGetCurrentUser)
			r.Get("/me/documents", documentsHandler.HandleListMyDocuments)
			r.Get("/me/pending-documents", documentsHandler.HandleGetMyPendingDocuments)

			// Owner-based document management (user can manage docs they created)
			r.Get("/me/documents/{docId}/status", documentsHandler.HandleGetMyDocumentStatus)
			r.Put("/me/documents/{docId}/metadata", documentsHandler.HandleUpdateMyDocumentMetadata)
			r.Delete("/me/documents/{docId}", documentsHandler.HandleDeleteMyDocument)

			// Expected signers management (owner can manage signers for their documents)
			r.Post("/me/documents/{docId}/signers", documentsHandler.HandleAddMyExpectedSigner)
			r.Delete("/me/documents/{docId}/signers/{email}", documentsHandler.HandleRemoveMyExpectedSigner)
		})

		// Signature endpoints
		r.Route("/signatures", func(r chi.Router) {
			r.Get("/", signaturesHandler.HandleGetUserSignatures)
			r.Post("/", signaturesHandler.HandleCreateSignature)
		})

		// Document signature status (authenticated)
		r.Get("/documents/{docId}/signatures/status", signaturesHandler.HandleGetSignatureStatus)

		// Document content (authenticated - serves stored files)
		r.Get("/documents/{docId}/content", storageHandler.HandleContent)

		// Document upload (authenticated, with rate limiting)
		if storageHandler.IsEnabled() {
			r.Group(func(r chi.Router) {
				r.Use(documentRateLimit.Middleware)
				r.Post("/documents/upload", storageHandler.HandleUpload)
			})
		}
	})

	// Admin routes
	r.Group(func(r chi.Router) {
		r.Use(apiMiddleware.RequireAdmin)
		r.Use(apiMiddleware.CSRFProtect)

		// Configure import max signers with default
		importMaxSigners := cfg.ImportMaxSigners
		if importMaxSigners == 0 {
			importMaxSigners = 500 // Default: 500 signers per import
		}

		// Initialize admin handler
		adminHandler := apiAdmin.NewHandler(cfg.AdminService, cfg.ReminderService, cfg.SignatureService, cfg.BaseURL, importMaxSigners)
		if cfg.QuotaEnforcer != nil && cfg.TenantProvider != nil {
			adminHandler.WithQuotaRecorder(
				&quotaRecorderAdapter{enforcer: cfg.QuotaEnforcer},
				cfg.TenantProvider,
			)
		}
		if cfg.AuditLogger != nil {
			adminHandler.WithAuditLogger(cfg.AuditLogger)
		}
		webhooksHandler := apiAdmin.NewWebhooksHandler(cfg.WebhookService)
		if cfg.QuotaEnforcer != nil && cfg.TenantProvider != nil {
			webhooksHandler.WithQuotaRecorder(
				&quotaRecorderAdapter{enforcer: cfg.QuotaEnforcer},
				cfg.TenantProvider,
			)
		}
		if cfg.AuditLogger != nil {
			webhooksHandler.WithAuditLogger(cfg.AuditLogger)
		}

		r.Route("/admin", func(r chi.Router) {
			// Document management
			r.Route("/documents", func(r chi.Router) {
				r.Get("/", adminHandler.HandleListDocuments)
				r.Get("/{docId}", adminHandler.HandleGetDocument)
				r.Get("/{docId}/signers", adminHandler.HandleGetDocumentWithSigners)
				r.Get("/{docId}/status", adminHandler.HandleGetDocumentStatus)

				// Document metadata
				r.Put("/{docId}/metadata", adminHandler.HandleUpdateDocumentMetadata)

				// Document deletion
				r.Delete("/{docId}", adminHandler.HandleDeleteDocument)

				// Expected signers management
				r.Post("/{docId}/signers", adminHandler.HandleAddExpectedSigner)
				r.Delete("/{docId}/signers/{email}", adminHandler.HandleRemoveExpectedSigner)

				// CSV import for expected signers
				r.Post("/{docId}/signers/preview-csv", adminHandler.HandlePreviewCSV)
				r.Post("/{docId}/signers/import", adminHandler.HandleImportSigners)

				// Reminder management
				r.Post("/{docId}/reminders", adminHandler.HandleSendReminders)
				r.Get("/{docId}/reminders", adminHandler.HandleGetReminderHistory)
			})

			// Webhooks management
			r.Route("/webhooks", func(r chi.Router) {
				r.Get("/", webhooksHandler.HandleListWebhooks)
				r.Post("/", webhooksHandler.HandleCreateWebhook)
				r.Get("/{id}", webhooksHandler.HandleGetWebhook)
				r.Put("/{id}", webhooksHandler.HandleUpdateWebhook)
				r.Patch("/{id}/{action}", webhooksHandler.HandleToggleWebhook) // action: enable|disable
				r.Delete("/{id}", webhooksHandler.HandleDeleteWebhook)
				r.Get("/{id}/deliveries", webhooksHandler.HandleListDeliveries)
			})

			// Settings management (configuration)
			if cfg.ConfigService != nil {
				settingsHandler := apiAdmin.NewSettingsHandler(cfg.ConfigService)
				r.Route("/settings", func(r chi.Router) {
					r.Get("/", settingsHandler.HandleGetSettings)
					r.Put("/{section}", settingsHandler.HandleUpdateSection)
					r.Post("/test/{type}", settingsHandler.HandleTestConnection)
					r.Post("/reset", settingsHandler.HandleResetFromENV)
				})
			}
		})
	})

	// Serve OpenAPI spec
	r.Get("/openapi.json", serveOpenAPISpec)

	return r
}

// serveOpenAPISpec serves the OpenAPI specification
func serveOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	// Read the OpenAPI YAML file and convert to JSON
	yamlData, err := os.ReadFile("openapi.yaml")
	if err != nil {
		// Fallback to basic response if file not found
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"info":{"title":"Ackify API","version":"1.0.0"},"message":"OpenAPI spec file not found - see /backend/openapi.yaml"}`))
		return
	}

	// Parse YAML and convert to JSON
	var spec map[string]interface{}
	if err := yaml.Unmarshal(yamlData, &spec); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"Failed to parse OpenAPI spec"}`))
		return
	}

	jsonData, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"Failed to convert OpenAPI spec to JSON"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(jsonData)
}
