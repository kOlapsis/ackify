// SPDX-License-Identifier: AGPL-3.0-or-later
package documents

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/kolapsis/ackify/backend/internal/application/services"
	"github.com/kolapsis/ackify/backend/internal/presentation/api/shared"
	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/models"
	"github.com/kolapsis/ackify/backend/pkg/providers"
)

// QuotaRecorder tracks document and signer quota usage.
// Satisfied by the adapter in the router package to avoid import cycles with pkg/web.
type QuotaRecorder interface {
	CheckDocumentCreation(ctx context.Context, tenantID string) error
	RecordDocumentCreation(ctx context.Context, tenantID string) error
	RecordDocumentDeletion(ctx context.Context, tenantID string) error
	CheckSignerAdd(ctx context.Context, tenantID string) error
	RecordSignerAdd(ctx context.Context, tenantID string) error
}

// documentService defines the interface for document operations
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
	FindPendingDocumentsForEmail(ctx context.Context, email string) ([]*models.PendingDocument, error)
}

// webhookPublisher defines minimal publish capability
type webhookPublisher interface {
	Publish(ctx context.Context, eventType string, payload map[string]interface{}) error
}

// signatureService defines the interface for signature operations
type signatureService interface {
	GetDocumentSignatures(ctx context.Context, docID string) ([]*models.Signature, error)
}

// adminService defines admin-level operations needed for document management
type adminService interface {
	GetDocument(ctx context.Context, docID string) (*models.Document, error)
	UpdateDocumentMetadata(ctx context.Context, docID string, input models.DocumentInput, updatedBy string) (*models.Document, error)
	DeleteDocument(ctx context.Context, docID string) error
	ListExpectedSignersWithStatus(ctx context.Context, docID string) ([]*models.ExpectedSignerWithStatus, error)
	GetSignerStats(ctx context.Context, docID string) (*models.DocCompletionStats, error)
	AddExpectedSigners(ctx context.Context, docID string, contacts []models.ContactInfo, addedBy string) error
	RemoveExpectedSigner(ctx context.Context, docID, email string) error
}

// Handler handles document API requests
type Handler struct {
	signatureService signatureService
	documentService  documentService
	adminService     adminService
	webhookPublisher webhookPublisher
	authorizer       providers.Authorizer
	quotaRecorder    QuotaRecorder
	tenantProvider   providers.TenantProvider
	auditLogger      shared.AuditLogger
	baseURL          string
}

// NewHandler creates a handler with all dependencies for full functionality
func NewHandler(
	signatureService signatureService,
	documentService documentService,
	publisher webhookPublisher,
	authorizer providers.Authorizer,
) *Handler {
	return &Handler{
		signatureService: signatureService,
		documentService:  documentService,
		webhookPublisher: publisher,
		authorizer:       authorizer,
	}
}

// WithQuotaRecorder sets the quota recorder for document creation tracking.
func (h *Handler) WithQuotaRecorder(recorder QuotaRecorder, tp providers.TenantProvider) *Handler {
	h.quotaRecorder = recorder
	h.tenantProvider = tp
	return h
}

// WithAuditLogger sets the audit logger for document events.
func (h *Handler) WithAuditLogger(logger shared.AuditLogger) *Handler {
	h.auditLogger = logger
	return h
}

// WithAdminService sets the admin service for owner-based document management.
func (h *Handler) WithAdminService(adminService adminService, baseURL string) *Handler {
	h.adminService = adminService
	h.baseURL = baseURL
	return h
}

// DocumentDTO represents a document data transfer object
type DocumentDTO struct {
	ID                  string                 `json:"id"`
	Title               string                 `json:"title"`
	Description         string                 `json:"description"`
	CreatedAt           string                 `json:"createdAt,omitempty"`
	UpdatedAt           string                 `json:"updatedAt,omitempty"`
	SignatureCount      int                    `json:"signatureCount"`
	ExpectedSignerCount int                    `json:"expectedSignerCount"`
	Metadata            map[string]interface{} `json:"metadata,omitempty"`
}

// SignatureDTO represents a signature data transfer object
type SignatureDTO struct {
	ID          string `json:"id"`
	DocID       string `json:"docId"`
	UserEmail   string `json:"userEmail"`
	UserName    string `json:"userName,omitempty"`
	SignedAt    string `json:"signedAt"`
	Signature   string `json:"signature"`
	PayloadHash string `json:"payloadHash"`
	Nonce       string `json:"nonce"`
	PrevHash    string `json:"prevHash,omitempty"`
}

// CreateDocumentRequest represents the request body for creating a document
type CreateDocumentRequest struct {
	Reference string `json:"reference"`
	Title     string `json:"title,omitempty"`
}

// getTenantID returns the current tenant ID as a string, or empty if unavailable.
func (h *Handler) getTenantID(ctx context.Context) string {
	if h.tenantProvider == nil {
		return ""
	}
	tid, err := h.tenantProvider.CurrentTenant(ctx)
	if err != nil {
		return ""
	}
	return tid.String()
}

// CreateDocumentResponse represents the response for creating a document
type CreateDocumentResponse struct {
	DocID     string `json:"docId"`
	URL       string `json:"url,omitempty"`
	Title     string `json:"title"`
	CreatedAt string `json:"createdAt"`
}

// HandleCreateDocument handles POST /api/v1/documents
func (h *Handler) HandleCreateDocument(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user, authenticated := shared.GetUserFromContext(ctx)
	userEmail := ""
	if authenticated && user != nil {
		userEmail = user.Email
	}

	if !h.authorizer.CanCreateDocument(ctx, userEmail) {
		if !authenticated {
			logger.Logger.Warn("Unauthenticated user attempted to create document",
				"remote_addr", r.RemoteAddr)
			shared.WriteError(w, http.StatusUnauthorized, shared.ErrCodeUnauthorized, "Authentication required to create document", nil)
			return
		}
		logger.Logger.Warn("Non-admin user attempted to create document",
			"user_email", user.Email,
			"remote_addr", r.RemoteAddr)
		shared.WriteError(w, http.StatusForbidden, shared.ErrCodeForbidden, "Only administrators can create documents", nil)
		return
	}

	var req CreateDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Logger.Warn("Invalid document creation request body",
			"error", err.Error(),
			"remote_addr", r.RemoteAddr)
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	if req.Reference == "" {
		logger.Logger.Warn("Document creation request missing reference field",
			"remote_addr", r.RemoteAddr)
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Reference is required", nil)
		return
	}

	logger.Logger.Info("Document creation request received",
		"reference", req.Reference,
		"has_title", req.Title != "",
		"remote_addr", r.RemoteAddr)

	// Quota check before creation
	if h.quotaRecorder != nil {
		tenantID := h.getTenantID(ctx)
		if tenantID != "" {
			if err := h.quotaRecorder.CheckDocumentCreation(ctx, tenantID); err != nil {
				shared.WriteError(w, http.StatusForbidden, shared.ErrCodeForbidden, "Quota exceeded: "+err.Error(), nil)
				return
			}
		}
	}

	docRequest := services.CreateDocumentRequest{
		Reference: req.Reference,
		Title:     req.Title,
	}

	doc, err := h.documentService.CreateDocument(ctx, docRequest)
	if err != nil {
		if errors.Is(err, services.ErrDocumentAlreadyExists) {
			logger.Logger.Warn("Document already exists (possibly soft-deleted)",
				"reference", req.Reference)
			shared.WriteConflict(w, "A document with this reference already exists. It may have been deleted. Please use a different reference.")
			return
		}
		if errors.Is(err, services.ErrHTTPSRequired) {
			shared.WriteValidationError(w, "HTTPS is required for document URLs", nil)
			return
		}
		if errors.Is(err, services.ErrDomainNotAllowed) {
			shared.WriteValidationError(w, err.Error(), nil)
			return
		}
		logger.Logger.Error("Document creation failed in handler",
			"reference", req.Reference,
			"error", err.Error())
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to create document", map[string]interface{}{"error": err.Error()})
		return
	}

	logger.Logger.Info("Document creation succeeded",
		"doc_id", doc.DocID,
		"title", doc.Title,
		"has_url", doc.URL != "")

	// Record quota usage after successful creation
	if h.quotaRecorder != nil {
		tenantID := h.getTenantID(ctx)
		if tenantID != "" {
			if err := h.quotaRecorder.RecordDocumentCreation(ctx, tenantID); err != nil {
				logger.Logger.Error("Failed to record document creation quota",
					"tenant_id", tenantID,
					"error", err.Error())
			}
		}
	}

	// Publish webhook event
	if h.webhookPublisher != nil {
		_ = h.webhookPublisher.Publish(ctx, "document.created", map[string]interface{}{
			"doc_id":             doc.DocID,
			"title":              doc.Title,
			"url":                doc.URL,
			"checksum":           doc.Checksum,
			"checksum_algorithm": doc.ChecksumAlgorithm,
		})
	}

	// Audit log
	shared.EmitAudit(ctx, h.auditLogger, r, h.getTenantID(ctx), "document.create", "document", doc.DocID, map[string]any{"title": doc.Title})

	// Return the created document
	response := CreateDocumentResponse{
		DocID:     doc.DocID,
		URL:       doc.URL,
		Title:     doc.Title,
		CreatedAt: doc.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	shared.WriteJSON(w, http.StatusCreated, response)
}

// HandleListDocuments handles GET /api/v1/documents
func (h *Handler) HandleListDocuments(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	pagination := shared.ParsePaginationParams(r, 20, 100)
	searchQuery := r.URL.Query().Get("search")

	var docs []*models.Document
	var err error

	if searchQuery != "" {
		// Use search if query is provided
		docs, err = h.documentService.Search(ctx, searchQuery, pagination.PageSize, pagination.Offset)
		logger.Logger.Debug("Public document search request",
			"query", searchQuery,
			"limit", pagination.PageSize,
			"offset", pagination.Offset)
	} else {
		// Otherwise, list all documents
		docs, err = h.documentService.List(ctx, pagination.PageSize, pagination.Offset)
		logger.Logger.Debug("Public document list request",
			"limit", pagination.PageSize,
			"offset", pagination.Offset)
	}

	if err != nil {
		logger.Logger.Error("Failed to fetch documents",
			"search", searchQuery,
			"error", err.Error())
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to fetch documents", nil)
		return
	}

	totalCount, err := h.documentService.Count(ctx, searchQuery)
	if err != nil {
		logger.Logger.Warn("Failed to count documents, using result count",
			"error", err.Error(),
			"search", searchQuery)
		totalCount = len(docs)
	}

	documents := make([]DocumentDTO, 0, len(docs))
	for _, doc := range docs {
		dto := DocumentDTO{
			ID:          doc.DocID,
			Title:       doc.Title,
			Description: doc.Description,
			CreatedAt:   doc.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt:   doc.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}

		if sigs, err := h.signatureService.GetDocumentSignatures(ctx, doc.DocID); err == nil {
			dto.SignatureCount = len(sigs)
		}

		if stats, err := h.documentService.GetExpectedSignerStats(ctx, doc.DocID); err == nil {
			dto.ExpectedSignerCount = stats.ExpectedCount
		}

		documents = append(documents, dto)
	}

	shared.WritePaginatedJSON(w, documents, pagination.Page, pagination.PageSize, totalCount)
}

// HandleGetDocument handles GET /api/v1/documents/{docId}
func (h *Handler) HandleGetDocument(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	docID := chi.URLParam(r, "docId")

	if docID == "" {
		shared.WriteValidationError(w, "Document ID is required", nil)
		return
	}

	doc, err := h.documentService.GetByDocID(ctx, docID)
	if err != nil {
		logger.Logger.Error("Failed to get document", "doc_id", docID, "error", err.Error())
		shared.WriteInternalError(w)
		return
	}
	if doc == nil {
		shared.WriteError(w, http.StatusNotFound, shared.ErrCodeNotFound, "Document not found", nil)
		return
	}

	signatures, err := h.signatureService.GetDocumentSignatures(ctx, docID)
	if err != nil {
		logger.Logger.Error("Failed to get signatures", "doc_id", docID, "error", err.Error())
		shared.WriteInternalError(w)
		return
	}

	// Build response
	response := DocumentDTO{
		ID:             docID,
		Title:          doc.Title,
		Description:    doc.Description,
		CreatedAt:      doc.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:      doc.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		SignatureCount: len(signatures),
	}

	// Get expected signer count
	if stats, err := h.documentService.GetExpectedSignerStats(ctx, docID); err == nil {
		response.ExpectedSignerCount = stats.ExpectedCount
	}

	shared.WriteJSON(w, http.StatusOK, response)
}

// HandleGetDocumentSignatures handles GET /api/v1/documents/{docId}/signatures
// Returns the detailed signature list only for document owner or admin.
// For authenticated users who are not owner/admin, returns only their own signature (if they signed).
// Non-authenticated users receive an empty list (the count remains available via DocumentDTO).
func (h *Handler) HandleGetDocumentSignatures(w http.ResponseWriter, r *http.Request) {
	docID := chi.URLParam(r, "docId")
	if docID == "" {
		shared.WriteValidationError(w, "Document ID is required", nil)
		return
	}

	ctx := r.Context()

	// Check if user can view the detailed list
	user, authenticated := shared.GetUserFromContext(ctx)

	// Retrieve document to get CreatedBy
	doc, err := h.documentService.GetByDocID(ctx, docID)
	if err != nil {
		logger.Logger.Error("Failed to get document", "doc_id", docID, "error", err.Error())
		shared.WriteInternalError(w)
		return
	}
	if doc == nil {
		shared.WriteError(w, http.StatusNotFound, shared.ErrCodeNotFound, "Document not found", nil)
		return
	}

	// Owner/Admin can see all signatures
	canViewAll := authenticated && user != nil && h.authorizer.CanManageDocument(ctx, user.Email, doc.CreatedBy)

	signatures, err := h.signatureService.GetDocumentSignatures(ctx, docID)
	if err != nil {
		logger.Logger.Error("Failed to get signatures",
			"doc_id", docID,
			"error", err.Error())
		shared.WriteInternalError(w)
		return
	}

	// If owner/admin, return all signatures
	if canViewAll {
		dtos := make([]SignatureDTO, len(signatures))
		for i := range signatures {
			dtos[i] = signatureToDTO(signatures[i])
		}
		shared.WriteJSON(w, http.StatusOK, dtos)
		return
	}

	// For authenticated users (not owner/admin), return only their own signature if they signed
	if authenticated && user != nil {
		for _, sig := range signatures {
			if sig.UserEmail == user.Email {
				shared.WriteJSON(w, http.StatusOK, []SignatureDTO{signatureToDTO(sig)})
				return
			}
		}
	}

	// Non-authenticated or user hasn't signed → return empty list
	shared.WriteJSON(w, http.StatusOK, []SignatureDTO{})
}

// PublicExpectedSigner represents an expected signer in public API (minimal info)
type PublicExpectedSigner struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

// HandleGetExpectedSigners handles GET /api/v1/documents/{docId}/expected-signers
// Returns the expected signers list only for document owner or admin.
// Non-authorized users receive an empty list.
func (h *Handler) HandleGetExpectedSigners(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	docID := chi.URLParam(r, "docId")

	if docID == "" {
		shared.WriteValidationError(w, "Document ID is required", nil)
		return
	}

	// Check if user can view the detailed list
	user, authenticated := shared.GetUserFromContext(ctx)

	// Retrieve document to get CreatedBy
	doc, err := h.documentService.GetByDocID(ctx, docID)
	if err != nil {
		logger.Logger.Error("Failed to get document", "doc_id", docID, "error", err.Error())
		shared.WriteInternalError(w)
		return
	}
	if doc == nil {
		shared.WriteError(w, http.StatusNotFound, shared.ErrCodeNotFound, "Document not found", nil)
		return
	}

	// If not authenticated or not authorized → return empty list
	canViewDetails := authenticated && user != nil && h.authorizer.CanManageDocument(ctx, user.Email, doc.CreatedBy)

	if !canViewDetails {
		shared.WriteJSON(w, http.StatusOK, []PublicExpectedSigner{})
		return
	}

	// Get expected signers (public version - without internal notes/metadata)
	signers, err := h.documentService.ListExpectedSigners(ctx, docID)
	if err != nil {
		logger.Logger.Error("Failed to get expected signers", "doc_id", docID, "error", err.Error())
		shared.WriteInternalError(w)
		return
	}

	// Convert to public DTO (minimal info - no notes, no internal metadata)
	response := make([]PublicExpectedSigner, 0, len(signers))
	for _, signer := range signers {
		response = append(response, PublicExpectedSigner{
			Email: signer.Email,
			Name:  signer.Name,
		})
	}

	shared.WriteJSON(w, http.StatusOK, response)
}

// Helper function to convert signature model to DTO
func signatureToDTO(sig *models.Signature) SignatureDTO {
	dto := SignatureDTO{
		ID:          strconv.FormatInt(sig.ID, 10),
		DocID:       sig.DocID,
		UserEmail:   sig.UserEmail,
		UserName:    sig.UserName,
		SignedAt:    sig.SignedAtUTC.Format("2006-01-02T15:04:05Z07:00"),
		Signature:   sig.Signature,
		PayloadHash: sig.PayloadHash,
		Nonce:       sig.Nonce,
	}

	if sig.PrevHash != nil && *sig.PrevHash != "" {
		dto.PrevHash = *sig.PrevHash
	}

	return dto
}

// FindOrCreateDocumentResponse represents the response for finding or creating a document
type FindOrCreateDocumentResponse struct {
	DocID             string `json:"docId"`
	URL               string `json:"url,omitempty"`
	Title             string `json:"title"`
	Checksum          string `json:"checksum,omitempty"`
	ChecksumAlgorithm string `json:"checksumAlgorithm,omitempty"`
	Description       string `json:"description,omitempty"`
	ReadMode          string `json:"readMode"`
	AllowDownload     bool   `json:"allowDownload"`
	RequireFullRead   bool   `json:"requireFullRead"`
	VerifyChecksum    bool   `json:"verifyChecksum"`
	CreatedAt         string `json:"createdAt"`
	IsNew             bool   `json:"isNew"`
	SignatureCount    int    `json:"signatureCount"`
	// Storage fields for uploaded documents
	StorageKey string `json:"storageKey,omitempty"`
	MimeType   string `json:"mimeType,omitempty"`
}

// HandleFindOrCreateDocument handles GET /api/v1/documents/find-or-create?doc={reference}
func (h *Handler) HandleFindOrCreateDocument(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// "doc" is the primary parameter; "ref" is accepted as fallback for backward compatibility
	ref := r.URL.Query().Get("doc")
	if ref == "" {
		ref = r.URL.Query().Get("ref")
	}
	if ref == "" {
		logger.Logger.Warn("Find or create request missing doc parameter",
			"remote_addr", r.RemoteAddr)
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "doc parameter is required", nil)
		return
	}

	logger.Logger.Info("Find or create document request",
		"reference", ref,
		"remote_addr", r.RemoteAddr)

	// Check if user is authenticated
	user, isAuthenticated := shared.GetUserFromContext(ctx)

	// First, try to find the document (without creating)
	refType := detectReferenceType(ref)
	existingDoc, err := h.documentService.FindByReference(ctx, ref, string(refType))
	if err != nil {
		logger.Logger.Error("Failed to search for document",
			"reference", ref,
			"error", err.Error())
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to search for document", map[string]interface{}{"error": err.Error()})
		return
	}

	// If document exists, return it
	if existingDoc != nil {
		logger.Logger.Info("Document found",
			"doc_id", existingDoc.DocID,
			"reference", ref)

		// Get signature count
		signatureCount := 0
		if sigs, err := h.signatureService.GetDocumentSignatures(ctx, existingDoc.DocID); err == nil {
			signatureCount = len(sigs)
		}

		response := FindOrCreateDocumentResponse{
			DocID:             existingDoc.DocID,
			URL:               existingDoc.URL,
			Title:             existingDoc.Title,
			Checksum:          existingDoc.Checksum,
			ChecksumAlgorithm: existingDoc.ChecksumAlgorithm,
			Description:       existingDoc.Description,
			ReadMode:          existingDoc.ReadMode,
			AllowDownload:     existingDoc.AllowDownload,
			RequireFullRead:   existingDoc.RequireFullRead,
			VerifyChecksum:    existingDoc.VerifyChecksum,
			CreatedAt:         existingDoc.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			IsNew:             false,
			SignatureCount:    signatureCount,
			StorageKey:        existingDoc.StorageKey,
			MimeType:          existingDoc.MimeType,
		}

		shared.WriteJSON(w, http.StatusOK, response)
		return
	}

	// Document doesn't exist - check authentication before creating
	if !isAuthenticated {
		logger.Logger.Warn("Unauthenticated user attempted to create document",
			"reference", ref,
			"remote_addr", r.RemoteAddr)
		shared.WriteError(w, http.StatusUnauthorized, shared.ErrCodeUnauthorized, "Authentication required to create document", nil)
		return
	}

	// Check if user can create documents
	if !h.authorizer.CanCreateDocument(ctx, user.Email) {
		logger.Logger.Warn("Non-admin user attempted to create document via find-or-create",
			"user_email", user.Email,
			"reference", ref,
			"remote_addr", r.RemoteAddr)
		shared.WriteError(w, http.StatusForbidden, shared.ErrCodeForbidden, "Only administrators can create documents", nil)
		return
	}

	// Quota check before creation
	if h.quotaRecorder != nil {
		tenantID := h.getTenantID(ctx)
		if tenantID != "" {
			if err := h.quotaRecorder.CheckDocumentCreation(ctx, tenantID); err != nil {
				shared.WriteError(w, http.StatusForbidden, shared.ErrCodeForbidden, "Quota exceeded: "+err.Error(), nil)
				return
			}
		}
	}

	// User is authenticated, create the document
	doc, isNew, err := h.documentService.FindOrCreateDocument(ctx, ref, user.Email)
	if err != nil {
		if errors.Is(err, services.ErrDocumentAlreadyExists) {
			logger.Logger.Warn("Document already exists (possibly soft-deleted)",
				"reference", ref,
				"user_email", user.Email)
			shared.WriteConflict(w, "A document with this reference already exists. It may have been deleted. Please use a different reference.")
			return
		}
		if errors.Is(err, services.ErrHTTPSRequired) {
			shared.WriteValidationError(w, "HTTPS is required for document URLs", nil)
			return
		}
		if errors.Is(err, services.ErrDomainNotAllowed) {
			shared.WriteValidationError(w, err.Error(), nil)
			return
		}
		logger.Logger.Error("Failed to create document",
			"reference", ref,
			"error", err.Error())
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to create document", map[string]interface{}{"error": err.Error()})
		return
	}

	logger.Logger.Info("Document created",
		"doc_id", doc.DocID,
		"reference", ref,
		"user_email", user.Email)

	// Record quota usage if a new document was created
	if isNew && h.quotaRecorder != nil {
		tenantID := h.getTenantID(ctx)
		if tenantID != "" {
			if err := h.quotaRecorder.RecordDocumentCreation(ctx, tenantID); err != nil {
				logger.Logger.Error("Failed to record document creation quota",
					"tenant_id", tenantID,
					"error", err.Error())
			}
		}
	}

	// Build response (new document has 0 signatures)
	response := FindOrCreateDocumentResponse{
		DocID:             doc.DocID,
		URL:               doc.URL,
		Title:             doc.Title,
		Checksum:          doc.Checksum,
		ChecksumAlgorithm: doc.ChecksumAlgorithm,
		Description:       doc.Description,
		ReadMode:          doc.ReadMode,
		AllowDownload:     doc.AllowDownload,
		RequireFullRead:   doc.RequireFullRead,
		VerifyChecksum:    doc.VerifyChecksum,
		CreatedAt:         doc.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		IsNew:             isNew,
		SignatureCount:    0,
		StorageKey:        doc.StorageKey,
		MimeType:          doc.MimeType,
	}

	shared.WriteJSON(w, http.StatusOK, response)
}

func detectReferenceType(ref string) ReferenceType {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return "url"
	}

	if strings.Contains(ref, "/") || strings.Contains(ref, "\\") {
		return "path"
	}

	return "reference"
}

type ReferenceType string

// MyDocumentDTO represents a document with stats for the current user's documents list
type MyDocumentDTO struct {
	ID                  string `json:"id"`
	Title               string `json:"title"`
	URL                 string `json:"url,omitempty"`
	Description         string `json:"description"`
	CreatedAt           string `json:"createdAt"`
	UpdatedAt           string `json:"updatedAt"`
	SignatureCount      int    `json:"signatureCount"`
	ExpectedSignerCount int    `json:"expectedSignerCount"`
}

// HandleListMyDocuments handles GET /api/v1/users/me/documents
// Returns documents created by the current authenticated user
func (h *Handler) HandleListMyDocuments(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user, authenticated := shared.GetUserFromContext(ctx)
	if !authenticated || user == nil {
		shared.WriteError(w, http.StatusUnauthorized, shared.ErrCodeUnauthorized, "Authentication required", nil)
		return
	}

	if !h.authorizer.CanCreateDocument(ctx, user.Email) {
		shared.WriteForbidden(w, "You don't have permission to manage documents")
		return
	}

	pagination := shared.ParsePaginationParams(r, 20, 100)
	searchQuery := r.URL.Query().Get("search")

	var docs []*models.Document
	var err error

	if searchQuery != "" {
		docs, err = h.documentService.SearchByCreatedBy(ctx, user.Email, searchQuery, pagination.PageSize, pagination.Offset)
		logger.Logger.Debug("User document search request",
			"user_email", user.Email,
			"query", searchQuery,
			"limit", pagination.PageSize,
			"offset", pagination.Offset)
	} else {
		docs, err = h.documentService.ListByCreatedBy(ctx, user.Email, pagination.PageSize, pagination.Offset)
		logger.Logger.Debug("User document list request",
			"user_email", user.Email,
			"limit", pagination.PageSize,
			"offset", pagination.Offset)
	}

	if err != nil {
		logger.Logger.Error("Failed to fetch user documents",
			"user_email", user.Email,
			"search", searchQuery,
			"error", err.Error())
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to fetch documents", nil)
		return
	}

	totalCount, err := h.documentService.CountByCreatedBy(ctx, user.Email, searchQuery)
	if err != nil {
		logger.Logger.Warn("Failed to count user documents, using result count",
			"error", err.Error(),
			"user_email", user.Email,
			"search", searchQuery)
		totalCount = len(docs)
	}

	documents := make([]MyDocumentDTO, 0, len(docs))
	for _, doc := range docs {
		dto := MyDocumentDTO{
			ID:          doc.DocID,
			Title:       doc.Title,
			URL:         doc.URL,
			Description: doc.Description,
			CreatedAt:   doc.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt:   doc.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}

		// Get stats which correctly calculates SignedCount as expected signers who signed
		if stats, err := h.documentService.GetExpectedSignerStats(ctx, doc.DocID); err == nil {
			dto.SignatureCount = stats.SignedCount
			dto.ExpectedSignerCount = stats.ExpectedCount
		}

		documents = append(documents, dto)
	}

	shared.WritePaginatedJSON(w, documents, pagination.Page, pagination.PageSize, totalCount)
}

// PendingDocumentDTO represents a document awaiting confirmation by the current user
type PendingDocumentDTO struct {
	DocID   string `json:"docId"`
	Title   string `json:"title"`
	AddedAt string `json:"addedAt"`
}

// HandleGetMyPendingDocuments handles GET /api/v1/users/me/pending-documents
func (h *Handler) HandleGetMyPendingDocuments(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user, authenticated := shared.GetUserFromContext(ctx)
	if !authenticated || user == nil {
		shared.WriteError(w, http.StatusUnauthorized, shared.ErrCodeUnauthorized, "Authentication required", nil)
		return
	}

	docs, err := h.documentService.FindPendingDocumentsForEmail(ctx, user.Email)
	if err != nil {
		logger.Logger.Error("Failed to fetch pending documents",
			"user_email", user.Email,
			"error", err.Error())
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to fetch pending documents", nil)
		return
	}

	response := make([]PendingDocumentDTO, 0, len(docs))
	for _, doc := range docs {
		response = append(response, PendingDocumentDTO{
			DocID:   doc.DocID,
			Title:   doc.Title,
			AddedAt: doc.AddedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	shared.WriteJSON(w, http.StatusOK, response)
}

// checkDocumentOwnership verifies the user can manage the document (admin or owner).
// Returns the document and user if access is granted, nil otherwise (error already written to response).
func (h *Handler) checkDocumentOwnership(w http.ResponseWriter, r *http.Request) (*models.Document, *models.User) {
	ctx := r.Context()
	docID := chi.URLParam(r, "docId")

	if docID == "" {
		shared.WriteValidationError(w, "Document ID is required", nil)
		return nil, nil
	}

	user, authenticated := shared.GetUserFromContext(ctx)
	if !authenticated || user == nil {
		shared.WriteError(w, http.StatusUnauthorized, shared.ErrCodeUnauthorized, "Authentication required", nil)
		return nil, nil
	}

	if !h.authorizer.CanCreateDocument(ctx, user.Email) {
		shared.WriteForbidden(w, "You don't have permission to manage documents")
		return nil, nil
	}

	doc, err := h.adminService.GetDocument(ctx, docID)
	if err != nil || doc == nil {
		shared.WriteError(w, http.StatusNotFound, shared.ErrCodeNotFound, "Document not found", nil)
		return nil, nil
	}

	if !h.authorizer.CanManageDocument(ctx, user.Email, doc.CreatedBy) {
		shared.WriteForbidden(w, "You don't have permission to manage this document")
		return nil, nil
	}

	return doc, user
}

// HandleGetMyDocumentStatus handles GET /api/v1/users/me/documents/{docId}/status
func (h *Handler) HandleGetMyDocumentStatus(w http.ResponseWriter, r *http.Request) {
	doc, _ := h.checkDocumentOwnership(w, r)
	if doc == nil {
		return
	}

	ctx := r.Context()
	docID := doc.DocID

	type ExpectedSignerResponse struct {
		ID                    int64   `json:"id"`
		DocID                 string  `json:"docId"`
		Email                 string  `json:"email"`
		Name                  string  `json:"name"`
		AddedAt               string  `json:"addedAt"`
		AddedBy               string  `json:"addedBy"`
		Notes                 *string `json:"notes,omitempty"`
		HasSigned             bool    `json:"hasSigned"`
		SignedAt              *string `json:"signedAt,omitempty"`
		UserName              *string `json:"userName,omitempty"`
		LastReminderSent      *string `json:"lastReminderSent,omitempty"`
		ReminderCount         int     `json:"reminderCount"`
		DaysSinceAdded        int     `json:"daysSinceAdded"`
		DaysSinceLastReminder *int    `json:"daysSinceLastReminder,omitempty"`
	}

	type UnexpectedSignatureResponse struct {
		UserEmail   string  `json:"userEmail"`
		UserName    *string `json:"userName,omitempty"`
		SignedAtUTC string  `json:"signedAtUTC"`
	}

	type DocumentStatsResponse struct {
		DocID          string  `json:"docId"`
		ExpectedCount  int     `json:"expectedCount"`
		SignedCount    int     `json:"signedCount"`
		PendingCount   int     `json:"pendingCount"`
		CompletionRate float64 `json:"completionRate"`
	}

	type DocumentResponse struct {
		DocID             string `json:"docId"`
		Title             string `json:"title"`
		URL               string `json:"url"`
		Checksum          string `json:"checksum,omitempty"`
		ChecksumAlgorithm string `json:"checksumAlgorithm,omitempty"`
		Description       string `json:"description"`
		ReadMode          string `json:"readMode"`
		AllowDownload     bool   `json:"allowDownload"`
		RequireFullRead   bool   `json:"requireFullRead"`
		VerifyChecksum    bool   `json:"verifyChecksum"`
		CreatedAt         string `json:"createdAt"`
		UpdatedAt         string `json:"updatedAt"`
		CreatedBy         string `json:"createdBy"`
		StorageKey        string `json:"storageKey,omitempty"`
		StorageProvider   string `json:"storageProvider,omitempty"`
		FileSize          int64  `json:"fileSize,omitempty"`
		MimeType          string `json:"mimeType,omitempty"`
	}

	type StatusResponse struct {
		DocID                string                         `json:"docId"`
		Document             *DocumentResponse              `json:"document,omitempty"`
		ExpectedSigners      []*ExpectedSignerResponse      `json:"expectedSigners"`
		UnexpectedSignatures []*UnexpectedSignatureResponse `json:"unexpectedSignatures"`
		Stats                *DocumentStatsResponse         `json:"stats"`
		ShareLink            string                         `json:"shareLink"`
	}

	response := &StatusResponse{
		DocID:                docID,
		ExpectedSigners:      []*ExpectedSignerResponse{},
		UnexpectedSignatures: []*UnexpectedSignatureResponse{},
		ShareLink:            h.baseURL + "/?doc=" + docID,
		Document: &DocumentResponse{
			DocID:             doc.DocID,
			Title:             doc.Title,
			URL:               doc.URL,
			Checksum:          doc.Checksum,
			ChecksumAlgorithm: doc.ChecksumAlgorithm,
			Description:       doc.Description,
			ReadMode:          doc.ReadMode,
			AllowDownload:     doc.AllowDownload,
			RequireFullRead:   doc.RequireFullRead,
			VerifyChecksum:    doc.VerifyChecksum,
			CreatedAt:         doc.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt:         doc.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
			CreatedBy:         doc.CreatedBy,
			StorageKey:        doc.StorageKey,
			StorageProvider:   doc.StorageProvider,
			FileSize:          doc.FileSize,
			MimeType:          doc.MimeType,
		},
	}

	// Get expected signers with status
	expectedEmails := make(map[string]bool)
	if signers, err := h.adminService.ListExpectedSignersWithStatus(ctx, docID); err == nil {
		for _, signer := range signers {
			resp := &ExpectedSignerResponse{
				ID:                    signer.ID,
				DocID:                 signer.DocID,
				Email:                 signer.Email,
				Name:                  signer.Name,
				AddedAt:               signer.AddedAt.Format("2006-01-02T15:04:05Z07:00"),
				AddedBy:               signer.AddedBy,
				Notes:                 signer.Notes,
				HasSigned:             signer.HasSigned,
				UserName:              signer.UserName,
				ReminderCount:         signer.ReminderCount,
				DaysSinceAdded:        signer.DaysSinceAdded,
				DaysSinceLastReminder: signer.DaysSinceLastReminder,
			}
			if signer.SignedAt != nil {
				signedAt := signer.SignedAt.Format("2006-01-02T15:04:05Z07:00")
				resp.SignedAt = &signedAt
			}
			if signer.LastReminderSent != nil {
				lastReminder := signer.LastReminderSent.Format("2006-01-02T15:04:05Z07:00")
				resp.LastReminderSent = &lastReminder
			}
			response.ExpectedSigners = append(response.ExpectedSigners, resp)
			expectedEmails[signer.Email] = true
		}
	}

	// Get all signatures and find unexpected ones
	if signatures, err := h.signatureService.GetDocumentSignatures(ctx, docID); err == nil {
		for _, sig := range signatures {
			if !expectedEmails[sig.UserEmail] {
				userName := sig.UserName
				response.UnexpectedSignatures = append(response.UnexpectedSignatures, &UnexpectedSignatureResponse{
					UserEmail:   sig.UserEmail,
					UserName:    &userName,
					SignedAtUTC: sig.SignedAtUTC.Format("2006-01-02T15:04:05Z07:00"),
				})
			}
		}
	}

	// Get completion stats
	if stats, err := h.adminService.GetSignerStats(ctx, docID); err == nil {
		response.Stats = &DocumentStatsResponse{
			DocID:          stats.DocID,
			ExpectedCount:  stats.ExpectedCount,
			SignedCount:    stats.SignedCount,
			PendingCount:   stats.PendingCount,
			CompletionRate: stats.CompletionRate,
		}
	} else {
		response.Stats = &DocumentStatsResponse{
			DocID: docID,
		}
	}

	shared.WriteJSON(w, http.StatusOK, response)
}

// HandleUpdateMyDocumentMetadata handles PUT /api/v1/users/me/documents/{docId}/metadata
func (h *Handler) HandleUpdateMyDocumentMetadata(w http.ResponseWriter, r *http.Request) {
	doc, user := h.checkDocumentOwnership(w, r)
	if doc == nil {
		return
	}

	ctx := r.Context()

	var req struct {
		Title             *string `json:"title,omitempty"`
		URL               *string `json:"url,omitempty"`
		Checksum          *string `json:"checksum,omitempty"`
		ChecksumAlgorithm *string `json:"checksumAlgorithm,omitempty"`
		Description       *string `json:"description,omitempty"`
		ReadMode          *string `json:"readMode,omitempty"`
		AllowDownload     *bool   `json:"allowDownload,omitempty"`
		RequireFullRead   *bool   `json:"requireFullRead,omitempty"`
		VerifyChecksum    *bool   `json:"verifyChecksum,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Invalid request body", nil)
		return
	}

	if req.Title != nil {
		doc.Title = *req.Title
	}
	if req.URL != nil {
		doc.URL = *req.URL
	}
	if req.Checksum != nil {
		doc.Checksum = *req.Checksum
	}
	if req.ChecksumAlgorithm != nil {
		doc.ChecksumAlgorithm = *req.ChecksumAlgorithm
	}
	if req.Description != nil {
		doc.Description = *req.Description
	}
	if req.ReadMode != nil {
		doc.ReadMode = *req.ReadMode
	}
	if req.AllowDownload != nil {
		doc.AllowDownload = *req.AllowDownload
	}
	if req.RequireFullRead != nil {
		doc.RequireFullRead = *req.RequireFullRead
	}
	if req.VerifyChecksum != nil {
		doc.VerifyChecksum = *req.VerifyChecksum
	}

	input := models.DocumentInput{
		Title:             doc.Title,
		URL:               doc.URL,
		Checksum:          doc.Checksum,
		ChecksumAlgorithm: doc.ChecksumAlgorithm,
		Description:       doc.Description,
		ReadMode:          doc.ReadMode,
		AllowDownload:     &doc.AllowDownload,
		RequireFullRead:   &doc.RequireFullRead,
		VerifyChecksum:    &doc.VerifyChecksum,
		StorageKey:        doc.StorageKey,
		StorageProvider:   doc.StorageProvider,
		FileSize:          doc.FileSize,
		MimeType:          doc.MimeType,
		OriginalFilename:  doc.OriginalFilename,
	}

	updated, err := h.adminService.UpdateDocumentMetadata(ctx, doc.DocID, input, user.Email)
	if err != nil {
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to update document metadata", nil)
		return
	}

	shared.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Document metadata updated successfully",
		"document": map[string]interface{}{
			"docId":             updated.DocID,
			"title":             updated.Title,
			"url":               updated.URL,
			"checksum":          updated.Checksum,
			"checksumAlgorithm": updated.ChecksumAlgorithm,
			"description":       updated.Description,
			"readMode":          updated.ReadMode,
			"allowDownload":     updated.AllowDownload,
			"requireFullRead":   updated.RequireFullRead,
			"verifyChecksum":    updated.VerifyChecksum,
			"createdAt":         updated.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			"updatedAt":         updated.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
			"createdBy":         updated.CreatedBy,
		},
	})
}

// HandleDeleteMyDocument handles DELETE /api/v1/users/me/documents/{docId}
func (h *Handler) HandleDeleteMyDocument(w http.ResponseWriter, r *http.Request) {
	doc, _ := h.checkDocumentOwnership(w, r)
	if doc == nil {
		return
	}

	ctx := r.Context()

	if err := h.adminService.DeleteDocument(ctx, doc.DocID); err != nil {
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to delete document", nil)
		return
	}

	// Decrement document quota counter
	if h.quotaRecorder != nil {
		tenantID := h.getTenantID(ctx)
		if tenantID != "" {
			if err := h.quotaRecorder.RecordDocumentDeletion(ctx, tenantID); err != nil {
				logger.Logger.Error("Failed to record document deletion quota",
					"tenant_id", tenantID,
					"error", err.Error())
			}
		}
	}

	// Audit log
	shared.EmitAudit(ctx, h.auditLogger, r, h.getTenantID(ctx), "document.delete", "document", doc.DocID, nil)

	shared.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Document deleted successfully",
	})
}

// HandleAddMyExpectedSigner handles POST /api/v1/users/me/documents/{docId}/signers
func (h *Handler) HandleAddMyExpectedSigner(w http.ResponseWriter, r *http.Request) {
	doc, user := h.checkDocumentOwnership(w, r)
	if doc == nil {
		return
	}

	ctx := r.Context()

	var req struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Invalid request body", nil)
		return
	}

	if req.Email == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Email is required", nil)
		return
	}

	// Check signer quota
	if h.quotaRecorder != nil {
		tenantID := h.getTenantID(ctx)
		if tenantID != "" {
			if err := h.quotaRecorder.CheckSignerAdd(ctx, tenantID); err != nil {
				shared.WriteError(w, http.StatusForbidden, shared.ErrCodeForbidden, "Quota exceeded", nil)
				return
			}
		}
	}

	contacts := []models.ContactInfo{{Email: req.Email, Name: req.Name}}
	if err := h.adminService.AddExpectedSigners(ctx, doc.DocID, contacts, user.Email); err != nil {
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to add expected signer", nil)
		return
	}

	// Record signer quota usage
	if h.quotaRecorder != nil {
		tenantID := h.getTenantID(ctx)
		if tenantID != "" {
			if err := h.quotaRecorder.RecordSignerAdd(ctx, tenantID); err != nil {
				logger.Logger.Warn("Failed to record signer add quota", "tenant_id", tenantID, "error", err.Error())
			}
		}
	}

	// Audit log
	shared.EmitAudit(ctx, h.auditLogger, r, h.getTenantID(ctx), "signer.add", "document", doc.DocID, map[string]any{"signer_email": req.Email})

	shared.WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"message": "Expected signer added successfully",
		"email":   req.Email,
	})
}

// HandleRemoveMyExpectedSigner handles DELETE /api/v1/users/me/documents/{docId}/signers/{email}
func (h *Handler) HandleRemoveMyExpectedSigner(w http.ResponseWriter, r *http.Request) {
	doc, _ := h.checkDocumentOwnership(w, r)
	if doc == nil {
		return
	}

	ctx := r.Context()
	emailEncoded := chi.URLParam(r, "email")

	email, err := url.QueryUnescape(emailEncoded)
	if err != nil {
		logger.Logger.Error("failed to decode email from URL", "error", err, "email_encoded", emailEncoded)
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Invalid email format", nil)
		return
	}

	if email == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Email is required", nil)
		return
	}

	if err := h.adminService.RemoveExpectedSigner(ctx, doc.DocID, email); err != nil {
		logger.Logger.Error("failed to remove expected signer", "error", err, "doc_id", doc.DocID, "email", email)
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to remove expected signer", nil)
		return
	}

	// Audit log
	shared.EmitAudit(ctx, h.auditLogger, r, h.getTenantID(ctx), "signer.remove", "document", doc.DocID, map[string]any{"signer_email": email})

	shared.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Expected signer removed successfully",
	})
}
