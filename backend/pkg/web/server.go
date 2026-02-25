// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/btouchard/ackify-ce/backend/pkg/config"
	"github.com/btouchard/ackify-ce/backend/pkg/providers"
	"github.com/go-chi/chi/v5"

	"github.com/btouchard/ackify-ce/backend/internal/application/services"
	"github.com/btouchard/ackify-ce/backend/internal/infrastructure/auth"
	"github.com/btouchard/ackify-ce/backend/internal/infrastructure/database"
	"github.com/btouchard/ackify-ce/backend/internal/infrastructure/email"
	"github.com/btouchard/ackify-ce/backend/internal/infrastructure/i18n"
	"github.com/btouchard/ackify-ce/backend/internal/infrastructure/tenant"
	"github.com/btouchard/ackify-ce/backend/internal/infrastructure/webhook"
	"github.com/btouchard/ackify-ce/backend/internal/infrastructure/workers"
	"github.com/btouchard/ackify-ce/backend/internal/presentation/api"
	"github.com/btouchard/ackify-ce/backend/internal/presentation/handlers"
	"github.com/btouchard/ackify-ce/backend/pkg/crypto"
	"github.com/btouchard/ackify-ce/backend/pkg/logger"
	"github.com/btouchard/ackify-ce/backend/pkg/storage"
	webauth "github.com/btouchard/ackify-ce/backend/pkg/web/auth"

	sdk "github.com/btouchard/shm/sdk/golang"
)

type Server struct {
	httpServer      *http.Server
	db              *sql.DB
	router          *chi.Mux
	emailSender     email.Sender
	emailWorker     *email.Worker
	webhookWorker   *webhook.Worker
	sessionWorker   *auth.SessionWorker
	magicLinkWorker *workers.MagicLinkCleanupWorker
	baseURL         string

	// Capability providers
	authProvider  AuthProvider
	authorizer    Authorizer
	quotaEnforcer QuotaEnforcer
	auditLogger   AuditLogger
}

// ServerBuilder allows dependency injection for extensibility.
// DB and TenantProvider are REQUIRED.
// AuthProvider and Authorizer have sensible CE defaults (AuthProvider, SimpleAuthorizer).
// QuotaEnforcer and AuditLogger have sensible CE defaults (NoLimit, LogOnly).
// All technical services (I18n, Email, MagicLink, Reminder, Config) are created internally.
type ServerBuilder struct {
	cfg      *config.Config
	frontend embed.FS
	version  string

	// Core infrastructure (required)
	db             *sql.DB
	tenantProvider providers.TenantProvider

	// Capability providers (all have CE defaults)
	authProvider  AuthProvider
	authorizer    Authorizer
	quotaEnforcer QuotaEnforcer
	auditLogger   AuditLogger

	// Internal infrastructure (created by Build)
	signer          *crypto.Ed25519Signer
	i18nService     *i18n.I18n
	emailSender     email.Sender
	emailRenderer   *email.Renderer
	storageProvider storage.Provider
	sessionService  *auth.SessionService

	// Internal services (created by Build)
	magicLinkService *services.MagicLinkService
	signatureService *services.SignatureService
	documentService  *services.DocumentService
	adminService     *services.AdminService
	webhookService   *services.WebhookService
	reminderService  *services.ReminderAsyncService
	configService    *services.ConfigService
}

func NewServerBuilder(cfg *config.Config, frontend embed.FS, version string) *ServerBuilder {
	return &ServerBuilder{
		cfg:      cfg,
		frontend: frontend,
		version:  version,
	}
}

// WithDB injects a database connection (REQUIRED).
func (b *ServerBuilder) WithDB(db *sql.DB) *ServerBuilder {
	b.db = db
	return b
}

// WithTenantProvider injects a tenant provider (REQUIRED).
func (b *ServerBuilder) WithTenantProvider(tp providers.TenantProvider) *ServerBuilder {
	b.tenantProvider = tp
	return b
}

// WithAuthProvider injects an authentication provider (REQUIRED).
func (b *ServerBuilder) WithAuthProvider(provider AuthProvider) *ServerBuilder {
	b.authProvider = provider
	return b
}

// WithAuthorizer injects an authorizer (REQUIRED).
func (b *ServerBuilder) WithAuthorizer(authorizer Authorizer) *ServerBuilder {
	b.authorizer = authorizer
	return b
}

// WithQuotaEnforcer injects a quota enforcer (optional, defaults to NoLimit).
func (b *ServerBuilder) WithQuotaEnforcer(enforcer QuotaEnforcer) *ServerBuilder {
	b.quotaEnforcer = enforcer
	return b
}

// WithAuditLogger injects an audit logger (optional, defaults to LogOnly).
func (b *ServerBuilder) WithAuditLogger(logger AuditLogger) *ServerBuilder {
	b.auditLogger = logger
	return b
}

// Build constructs the server with all dependencies.
func (b *ServerBuilder) Build(ctx context.Context) (*Server, error) {
	if err := b.validateProviders(); err != nil {
		return nil, err
	}

	if err := b.initializeInfrastructure(); err != nil {
		return nil, err
	}

	repos := b.createRepositories()

	// Initialize services that depend on repos
	if err := b.initializeConfigService(ctx, repos); err != nil {
		return nil, err
	}
	b.initializeMagicLinkService(repos)
	b.initializeSessionService(repos)

	// Now we can set default providers (they depend on services above)
	b.setDefaultProviders()

	b.initializeCoreServices(repos)
	b.initializeReminderService(repos)

	if err := b.initializeTelemetry(ctx); err != nil {
		return nil, err
	}

	whPublisher, whWorker, err := b.initializeWebhookSystem(ctx, repos)
	if err != nil {
		return nil, err
	}

	emailWorker, err := b.initializeEmailWorker(ctx, repos, whPublisher)
	if err != nil {
		return nil, err
	}

	magicLinkWorker := b.initializeMagicLinkCleanupWorker(ctx)

	sessionWorker, err := b.initializeSessionWorker(ctx, repos)
	if err != nil {
		return nil, err
	}

	router := b.buildRouter(repos, whPublisher)

	httpServer := &http.Server{
		Addr:    b.cfg.Server.ListenAddr,
		Handler: handlers.RequestLogger(handlers.SecureHeaders(router)),
	}

	return &Server{
		httpServer:      httpServer,
		db:              b.db,
		router:          router,
		emailSender:     b.emailSender,
		emailWorker:     emailWorker,
		webhookWorker:   whWorker,
		sessionWorker:   sessionWorker,
		magicLinkWorker: magicLinkWorker,
		baseURL:         b.cfg.App.BaseURL,
		authProvider:    b.authProvider,
		authorizer:      b.authorizer,
		quotaEnforcer:   b.quotaEnforcer,
		auditLogger:     b.auditLogger,
	}, nil
}

func (b *ServerBuilder) validateProviders() error {
	if b.db == nil {
		return errors.New("database is required: use WithDB()")
	}
	if b.tenantProvider == nil {
		return errors.New("tenantProvider is required: use WithTenantProvider()")
	}
	return nil
}

// setDefaultProviders sets default implementations for optional providers.
// Must be called AFTER initializeConfigService, initializeMagicLinkService, and initializeSessionService.
func (b *ServerBuilder) setDefaultProviders() {
	if b.authProvider == nil {
		b.authProvider = webauth.NewAuthProvider(webauth.ProviderConfig{
			ConfigProvider:   b.configService,
			SessionService:   b.sessionService,
			MagicLinkService: b.magicLinkService,
			BaseURL:          b.cfg.App.BaseURL,
		})
	}
	if b.authorizer == nil {
		b.authorizer = webauth.NewSimpleAuthorizer(b.cfg.App.AdminEmails, b.configService)
	}
	if b.quotaEnforcer == nil {
		b.quotaEnforcer = NewNoLimitQuotaEnforcer()
	}
	if b.auditLogger == nil {
		b.auditLogger = NewLogOnlyAuditLogger()
	}
}

func (b *ServerBuilder) initializeInfrastructure() error {
	var err error

	// Signer
	b.signer, err = crypto.NewEd25519Signer()
	if err != nil {
		return fmt.Errorf("failed to initialize signer: %w", err)
	}

	// I18n
	b.i18nService, err = i18n.NewI18n(getLocalesDir())
	if err != nil {
		return fmt.Errorf("failed to initialize i18n: %w", err)
	}

	// Email (only if SMTP is configured)
	if b.cfg.Mail.Host != "" {
		b.emailRenderer = email.NewRenderer(
			getTemplatesDir(),
			b.cfg.App.BaseURL,
			b.cfg.App.Organisation,
			b.cfg.Mail.FromName,
			b.cfg.Mail.From,
			b.cfg.Mail.DefaultLocale,
			b.i18nService,
		)
		b.emailSender = email.NewSMTPSender(b.cfg.Mail, b.emailRenderer)
	}

	// Storage
	if b.cfg.Storage.IsEnabled() {
		provider, err := storage.NewProvider(b.cfg.Storage)
		if err != nil {
			return fmt.Errorf("failed to initialize storage provider: %w", err)
		}
		b.storageProvider = provider
		if provider != nil {
			logger.Logger.Info("Storage provider initialized", "type", provider.Type())
		}
	}

	return nil
}

// repositories holds all repository instances.
type repositories struct {
	signature       *database.SignatureRepository
	document        *database.DocumentRepository
	expectedSigner  *database.ExpectedSignerRepository
	reminder        *database.ReminderRepository
	emailQueue      *database.EmailQueueRepository
	webhook         *database.WebhookRepository
	webhookDelivery *database.WebhookDeliveryRepository
	oauthSession    *database.OAuthSessionRepository
	config          *database.ConfigRepository
	magicLink       services.MagicLinkRepository
}

func (b *ServerBuilder) createRepositories() *repositories {
	return &repositories{
		signature:       database.NewSignatureRepository(b.db, b.tenantProvider),
		document:        database.NewDocumentRepository(b.db, b.tenantProvider),
		expectedSigner:  database.NewExpectedSignerRepository(b.db, b.tenantProvider),
		reminder:        database.NewReminderRepository(b.db, b.tenantProvider),
		emailQueue:      database.NewEmailQueueRepository(b.db, b.tenantProvider),
		webhook:         database.NewWebhookRepository(b.db, b.tenantProvider),
		webhookDelivery: database.NewWebhookDeliveryRepository(b.db, b.tenantProvider),
		oauthSession:    database.NewOAuthSessionRepository(b.db, b.tenantProvider),
		config:          database.NewConfigRepository(b.db, b.tenantProvider),
		magicLink:       database.NewMagicLinkRepository(b.db),
	}
}

func (b *ServerBuilder) initializeTelemetry(ctx context.Context) error {
	if !b.cfg.Telemetry.Enabled {
		return nil
	}

	telemetry, err := sdk.New(sdk.Config{
		ServerURL:   "https://metrics.kolapsis.com",
		AppName:     "Ackify",
		AppVersion:  b.version,
		Environment: "production",
		Enabled:     true,
		DataDir:     b.cfg.Telemetry.DataDir,
	})
	if err != nil {
		return err
	}
	telemetry.SetProvider(func() map[string]interface{} {
		return map[string]interface{}{
			"documents":     b.documentService.CountDocs(ctx),
			"confirmations": b.signatureService.CountSigns(ctx),
			"webhooks":      b.webhookService.CountWebhooks(ctx),
			"reminds_sent":  b.reminderService.CountSent(ctx),
		}
	})
	go telemetry.Start(context.Background())
	return nil
}

func (b *ServerBuilder) initializeWebhookSystem(ctx context.Context, repos *repositories) (*services.WebhookPublisher, *webhook.Worker, error) {
	whPublisher := services.NewWebhookPublisher(repos.webhook, repos.webhookDelivery)
	whCfg := webhook.DefaultWorkerConfig()
	whWorker := webhook.NewWorker(repos.webhookDelivery, &http.Client{}, whCfg, ctx, b.db, b.tenantProvider)

	if err := whWorker.Start(); err != nil {
		return nil, nil, fmt.Errorf("failed to start webhook worker: %w", err)
	}

	return whPublisher, whWorker, nil
}

// emailRenderer is expected to be injected from main.go via WithEmailRenderer().
func (b *ServerBuilder) initializeEmailWorker(ctx context.Context, repos *repositories, whPublisher *services.WebhookPublisher) (*email.Worker, error) {
	if b.emailSender == nil || b.cfg.Mail.Host == "" || b.emailRenderer == nil {
		return nil, nil
	}

	workerConfig := email.DefaultWorkerConfig()
	emailWorker := email.NewWorker(repos.emailQueue, b.emailSender, b.emailRenderer, workerConfig, ctx, b.db, b.tenantProvider)

	if whPublisher != nil {
		emailWorker.SetPublisher(whPublisher)
	}

	if err := emailWorker.Start(); err != nil {
		return nil, fmt.Errorf("failed to start email worker: %w", err)
	}

	return emailWorker, nil
}

func (b *ServerBuilder) initializeCoreServices(repos *repositories) {
	b.signatureService = services.NewSignatureService(repos.signature, repos.document, b.signer)
	b.signatureService.SetChecksumConfig(&b.cfg.Checksum)
	b.documentService = services.NewDocumentService(repos.document, repos.expectedSigner, &b.cfg.Checksum)
	if b.configService != nil {
		b.documentService.WithConfigProvider(b.configService)
	}
	b.adminService = services.NewAdminService(repos.document, repos.expectedSigner)
	b.webhookService = services.NewWebhookService(repos.webhook, repos.webhookDelivery)
}

func (b *ServerBuilder) initializeConfigService(ctx context.Context, repos *repositories) error {
	encryptionKey := b.cfg.OAuth.CookieSecret
	b.configService = services.NewConfigService(repos.config, b.cfg, encryptionKey)

	// Initialize config from DB or ENV
	err := tenant.WithTenantContextFromProvider(ctx, b.db, b.tenantProvider, func(txCtx context.Context) error {
		return b.configService.Initialize(txCtx)
	})
	if err != nil {
		logger.Logger.Warn("Failed to initialize config service, using ENV config", "error", err)
	}
	return nil
}

// initializeMagicLinkService creates the magic link service.
func (b *ServerBuilder) initializeMagicLinkService(repos *repositories) {
	b.magicLinkService = services.NewMagicLinkService(services.MagicLinkServiceConfig{
		Repository:        repos.magicLink,
		EmailSender:       b.emailSender,
		I18n:              b.i18nService,
		BaseURL:           b.cfg.App.BaseURL,
		AppName:           b.cfg.App.Organisation,
		RateLimitPerEmail: b.cfg.Auth.MagicLinkRateLimitEmail,
		RateLimitPerIP:    b.cfg.Auth.MagicLinkRateLimitIP,
	})
}

// initializeSessionService creates the session service for auth.
func (b *ServerBuilder) initializeSessionService(repos *repositories) {
	b.sessionService = auth.NewSessionService(auth.SessionServiceConfig{
		CookieSecret:  b.cfg.OAuth.CookieSecret,
		SecureCookies: b.cfg.App.SecureCookies,
		SessionRepo:   repos.oauthSession,
	})
}

// initializeMagicLinkCleanupWorker starts the cleanup worker for expired magic link tokens.
func (b *ServerBuilder) initializeMagicLinkCleanupWorker(ctx context.Context) *workers.MagicLinkCleanupWorker {
	magicLinkWorker := workers.NewMagicLinkCleanupWorker(b.magicLinkService, 1*time.Hour, b.db, b.tenantProvider)
	go magicLinkWorker.Start(ctx)
	return magicLinkWorker
}

func (b *ServerBuilder) initializeReminderService(repos *repositories) {
	b.reminderService = services.NewReminderAsyncService(
		repos.expectedSigner,
		repos.reminder,
		repos.emailQueue,
		b.magicLinkService,
		b.i18nService,
		services.StaticBaseURL(b.cfg.App.BaseURL),
	)
}

func (b *ServerBuilder) initializeSessionWorker(ctx context.Context, repos *repositories) (*auth.SessionWorker, error) {
	if repos.oauthSession == nil {
		return nil, nil
	}

	workerConfig := auth.DefaultSessionWorkerConfig()
	sessionWorker := auth.NewSessionWorker(repos.oauthSession, workerConfig, ctx, b.db, b.tenantProvider)
	if err := sessionWorker.Start(); err != nil {
		return nil, fmt.Errorf("failed to start OAuth session worker: %w", err)
	}

	return sessionWorker, nil
}

// apiQuotaAdapter adapts web.QuotaEnforcer to api.QuotaEnforcer (generic Check/Record with string action).
type apiQuotaAdapter struct {
	enforcer QuotaEnforcer
}

func (a *apiQuotaAdapter) Check(ctx context.Context, tenantID string, action string) error {
	return a.enforcer.Check(ctx, tenantID, QuotaAction(action))
}

func (a *apiQuotaAdapter) Record(ctx context.Context, tenantID string, action string) error {
	return a.enforcer.Record(ctx, tenantID, QuotaAction(action))
}

func (b *ServerBuilder) buildRouter(repos *repositories, whPublisher *services.WebhookPublisher) *chi.Mux {
	router := chi.NewRouter()
	router.Use(i18n.Middleware(b.i18nService))
	router.Use(EmbedDocumentMiddleware(b.documentService, whPublisher))

	// Build API router config using unified auth provider
	apiConfig := api.RouterConfig{
		// Database for RLS middleware
		DB:             b.db,
		TenantProvider: b.tenantProvider,

		// Capability providers (TenantProvider handles OIDC + MagicLink dynamically)
		AuthProvider:     b.authProvider,
		Authorizer:       b.authorizer,
		QuotaEnforcer:    &apiQuotaAdapter{enforcer: b.quotaEnforcer},
		SignatureService: b.signatureService,
		DocumentService:  b.documentService,
		AdminService:     b.adminService,
		ReminderService:  b.reminderService,
		WebhookService:   b.webhookService,
		WebhookPublisher: whPublisher,
		StorageProvider:  b.storageProvider,
		StorageMaxSizeMB: b.cfg.Storage.MaxSizeMB,
		BaseURL:          b.cfg.App.BaseURL,

		// Rate limiting
		AuthRateLimit:     b.cfg.App.AuthRateLimit,
		DocumentRateLimit: b.cfg.App.DocumentRateLimit,
		GeneralRateLimit:  b.cfg.App.GeneralRateLimit,
		ImportMaxSigners:  b.cfg.App.ImportMaxSigners,

		// Config service for dynamic settings
		ConfigService: b.configService,
	}
	apiRouter := api.NewRouter(apiConfig)
	router.Mount("/api/v1", apiRouter)

	router.Get("/oembed", handlers.HandleOEmbed(b.cfg.App.BaseURL))
	router.NotFound(EmbedFolder(b.frontend, "web/dist", b.cfg.App.BaseURL, b.version, repos.signature))

	return router
}

// === Server Methods ===

func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	// Stop Magic Link cleanup worker if it exists
	if s.magicLinkWorker != nil {
		s.magicLinkWorker.Stop()
	}

	// Stop OAuth session worker if it exists
	if s.sessionWorker != nil {
		if err := s.sessionWorker.Stop(); err != nil {
			logger.Logger.Warn("Failed to stop OAuth session worker", "error", err)
		}
	}

	// Stop email worker if it exists
	if s.emailWorker != nil {
		if err := s.emailWorker.Stop(); err != nil {
			logger.Logger.Warn("Failed to stop email worker", "error", err)
		}
	}

	// Stop webhook worker
	if s.webhookWorker != nil {
		if err := s.webhookWorker.Stop(); err != nil {
			logger.Logger.Warn("Failed to stop webhook worker", "error", err)
		}
	}

	// Shutdown HTTP server
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return err
	}

	// Close database connection
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Server) GetAddr() string {
	return s.httpServer.Addr
}

func (s *Server) Router() *chi.Mux {
	return s.router
}

func (s *Server) RegisterRoutes(fn func(r *chi.Mux)) {
	fn(s.router)
}

func (s *Server) GetDB() *sql.DB {
	return s.db
}

func (s *Server) GetAuthProvider() AuthProvider {
	return s.authProvider
}

func (s *Server) GetAuthorizer() Authorizer {
	return s.authorizer
}

func (s *Server) GetQuotaEnforcer() QuotaEnforcer {
	return s.quotaEnforcer
}

func (s *Server) GetAuditLogger() AuditLogger {
	return s.auditLogger
}

func (s *Server) GetEmailSender() email.Sender {
	return s.emailSender
}

// === Helper Functions ===

func getTemplatesDir() string {
	if envDir := os.Getenv("ACKIFY_TEMPLATES_DIR"); envDir != "" {
		return envDir
	}

	if execPath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(execPath)
		defaultDir := filepath.Join(execDir, "templates")
		if _, err := os.Stat(defaultDir); err == nil {
			return defaultDir
		}
	}

	possiblePaths := []string{
		"templates",
		"./templates",
	}

	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return "templates"
}

func getLocalesDir() string {
	if envDir := os.Getenv("ACKIFY_LOCALES_DIR"); envDir != "" {
		return envDir
	}

	if execPath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(execPath)
		defaultDir := filepath.Join(execDir, "locales")
		if _, err := os.Stat(defaultDir); err == nil {
			return defaultDir
		}
	}

	possiblePaths := []string{
		"locales",
		"./locales",
	}

	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return "locales"
}
