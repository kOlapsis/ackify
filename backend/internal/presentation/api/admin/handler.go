// SPDX-License-Identifier: AGPL-3.0-or-later
package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/kolapsis/ackify/backend/internal/application/services"
	"github.com/kolapsis/ackify/backend/internal/infrastructure/i18n"
	"github.com/kolapsis/ackify/backend/internal/presentation/api/shared"
	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/models"
	"github.com/kolapsis/ackify/backend/pkg/providers"
)

// QuotaRecorder tracks quota usage for documents, reminders and signers.
type QuotaRecorder interface {
	RecordDocumentDeletion(ctx context.Context, tenantID string) error
	CheckReminderSend(ctx context.Context, tenantID string) error
	RecordReminderSend(ctx context.Context, tenantID string) error
	CheckSignerAdd(ctx context.Context, tenantID string) error
	RecordSignerAdd(ctx context.Context, tenantID string) error
}

// adminService defines admin-level operations on documents and signers
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

// reminderService defines the interface for reminder operations
type reminderService interface {
	SendReminders(ctx context.Context, docID, sentBy string, specificEmails []string, docURL string, locale string) (*models.ReminderSendResult, error)
	GetReminderHistory(ctx context.Context, docID string) ([]*models.ReminderLog, error)
	GetReminderStats(ctx context.Context, docID string) (*models.ReminderStats, error)
}

// signatureService defines the interface for signature operations
type signatureService interface {
	GetDocumentSignatures(ctx context.Context, docID string) ([]*models.Signature, error)
}

// Handler handles admin API requests
type Handler struct {
	adminService     adminService
	reminderService  reminderService
	signatureService signatureService
	quotaRecorder    QuotaRecorder
	tenantProvider   providers.TenantProvider
	auditLogger      shared.AuditLogger
	baseURL          string
	importMaxSigners int
}

// NewHandler creates a new admin handler
func NewHandler(adminService adminService, reminderService reminderService, signatureService signatureService, baseURL string, importMaxSigners int) *Handler {
	return &Handler{
		adminService:     adminService,
		reminderService:  reminderService,
		signatureService: signatureService,
		baseURL:          baseURL,
		importMaxSigners: importMaxSigners,
	}
}

// WithQuotaRecorder sets the quota recorder for document deletion tracking.
func (h *Handler) WithQuotaRecorder(recorder QuotaRecorder, tp providers.TenantProvider) *Handler {
	h.quotaRecorder = recorder
	h.tenantProvider = tp
	return h
}

// WithAuditLogger sets the audit logger for admin events.
func (h *Handler) WithAuditLogger(logger shared.AuditLogger) *Handler {
	h.auditLogger = logger
	return h
}

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

// DocumentResponse represents a document in API responses
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

// ExpectedSignerResponse represents an expected signer in API responses
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

// DocumentStatsResponse represents document statistics
type DocumentStatsResponse struct {
	DocID          string  `json:"docId"`
	ExpectedCount  int     `json:"expectedCount"`
	SignedCount    int     `json:"signedCount"`
	PendingCount   int     `json:"pendingCount"`
	CompletionRate float64 `json:"completionRate"`
}

// UnexpectedSignatureResponse represents an unexpected signature
type UnexpectedSignatureResponse struct {
	UserEmail   string  `json:"userEmail"`
	UserName    *string `json:"userName,omitempty"`
	SignedAtUTC string  `json:"signedAtUTC"`
}

// HandleListDocuments handles GET /api/v1/admin/documents
func (h *Handler) HandleListDocuments(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse pagination and search parameters
	pagination := shared.ParsePaginationParams(r, 100, 200)
	searchQuery := r.URL.Query().Get("search")

	// Fetch documents with or without search
	var documents []*models.Document
	var err error

	if searchQuery != "" {
		documents, err = h.adminService.SearchDocuments(ctx, searchQuery, pagination.PageSize, pagination.Offset)
		logger.Logger.Debug("Admin document search",
			"query", searchQuery,
			"limit", pagination.PageSize,
			"offset", pagination.Offset)
	} else {
		documents, err = h.adminService.ListDocuments(ctx, pagination.PageSize, pagination.Offset)
		logger.Logger.Debug("Admin document list",
			"limit", pagination.PageSize,
			"offset", pagination.Offset)
	}

	if err != nil {
		logger.Logger.Error("Failed to fetch documents", "error", err.Error(), "search", searchQuery)
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to list documents", nil)
		return
	}

	// Get total count of documents (with or without search filter)
	totalCount, err := h.adminService.CountDocuments(ctx, searchQuery)
	if err != nil {
		logger.Logger.Warn("Failed to count documents, using result count",
			"error", err.Error(),
			"search", searchQuery)
		totalCount = len(documents)
	}

	response := make([]*DocumentResponse, 0, len(documents))
	for _, doc := range documents {
		response = append(response, toDocumentResponse(doc))
	}

	meta := map[string]interface{}{
		"total":  totalCount,     // Total matching documents in DB
		"count":  len(documents), // Count in this page
		"limit":  pagination.PageSize,
		"offset": pagination.Offset,
		"page":   pagination.Page,
	}

	if searchQuery != "" {
		meta["search"] = searchQuery
	}

	if pendingDocs, completedDocs, err := h.adminService.GetAggregateDocumentStats(ctx); err == nil {
		meta["pendingDocuments"] = pendingDocs
		meta["completedDocuments"] = completedDocs
	}

	shared.WriteJSONWithMeta(w, http.StatusOK, response, meta)
}

// HandleGetDocument handles GET /api/v1/admin/documents/{docId}
func (h *Handler) HandleGetDocument(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	docID := chi.URLParam(r, "docId")

	if docID == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Document ID is required", nil)
		return
	}

	document, err := h.adminService.GetDocument(ctx, docID)
	if err != nil {
		shared.WriteError(w, http.StatusNotFound, shared.ErrCodeNotFound, "Document not found", nil)
		return
	}

	shared.WriteJSON(w, http.StatusOK, toDocumentResponse(document))
}

// HandleGetDocumentWithSigners handles GET /api/v1/admin/documents/{docId}/signers
func (h *Handler) HandleGetDocumentWithSigners(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	docID := chi.URLParam(r, "docId")

	if docID == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Document ID is required", nil)
		return
	}

	// Get document
	document, err := h.adminService.GetDocument(ctx, docID)
	if err != nil {
		shared.WriteError(w, http.StatusNotFound, shared.ErrCodeNotFound, "Document not found", nil)
		return
	}

	// Get expected signers with status
	signers, err := h.adminService.ListExpectedSignersWithStatus(ctx, docID)
	if err != nil {
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to get signers", nil)
		return
	}

	// Get completion stats
	stats, err := h.adminService.GetSignerStats(ctx, docID)
	if err != nil {
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to get stats", nil)
		return
	}

	signersResponse := make([]*ExpectedSignerResponse, 0, len(signers))
	for _, signer := range signers {
		signersResponse = append(signersResponse, toExpectedSignerResponse(signer))
	}

	response := map[string]interface{}{
		"document": toDocumentResponse(document),
		"signers":  signersResponse,
		"stats":    toStatsResponse(stats),
	}

	shared.WriteJSON(w, http.StatusOK, response)
}

// AddExpectedSignerRequest represents the request body for adding an expected signer
type AddExpectedSignerRequest struct {
	Email string  `json:"email"`
	Name  string  `json:"name"`
	Notes *string `json:"notes,omitempty"`
}

// HandleAddExpectedSigner handles POST /api/v1/admin/documents/{docId}/signers
func (h *Handler) HandleAddExpectedSigner(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	docID := chi.URLParam(r, "docId")

	if docID == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Document ID is required", nil)
		return
	}

	// Get user from context
	user, ok := shared.GetUserFromContext(ctx)
	if !ok {
		shared.WriteUnauthorized(w, "")
		return
	}

	// Parse request body
	var req AddExpectedSignerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Invalid request body", nil)
		return
	}

	// Validate
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

	// Add expected signer
	contacts := []models.ContactInfo{{Email: req.Email, Name: req.Name}}
	err := h.adminService.AddExpectedSigners(ctx, docID, contacts, user.Email)
	if err != nil {
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
	shared.EmitAudit(ctx, h.auditLogger, r, h.getTenantID(ctx), "signer.add", "document", docID, map[string]any{"signer_email": req.Email})

	shared.WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"message": "Expected signer added successfully",
		"email":   req.Email,
	})
}

// HandleRemoveExpectedSigner handles DELETE /api/v1/admin/documents/{docId}/signers/{email}
func (h *Handler) HandleRemoveExpectedSigner(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	docID := chi.URLParam(r, "docId")
	emailEncoded := chi.URLParam(r, "email")

	// Decode URL-encoded email (e.g., al%40bundy.com -> al@bundy.com)
	email, err := url.QueryUnescape(emailEncoded)
	if err != nil {
		logger.Logger.Error("failed to decode email from URL", "error", err, "email_encoded", emailEncoded)
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Invalid email format", nil)
		return
	}

	if docID == "" || email == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Document ID and email are required", nil)
		return
	}

	// Remove expected signer
	err = h.adminService.RemoveExpectedSigner(ctx, docID, email)
	if err != nil {
		logger.Logger.Error("failed to remove expected signer", "error", err, "doc_id", docID, "email", email)
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to remove expected signer", nil)
		return
	}

	// Audit log
	shared.EmitAudit(ctx, h.auditLogger, r, h.getTenantID(ctx), "signer.remove", "document", docID, map[string]any{"signer_email": email})

	shared.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Expected signer removed successfully",
	})
}

// Helper functions to convert models to API responses
func toDocumentResponse(doc *models.Document) *DocumentResponse {
	return &DocumentResponse{
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
	}
}

func toExpectedSignerResponse(signer *models.ExpectedSignerWithStatus) *ExpectedSignerResponse {
	response := &ExpectedSignerResponse{
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
		response.SignedAt = &signedAt
	}

	if signer.LastReminderSent != nil {
		lastReminder := signer.LastReminderSent.Format("2006-01-02T15:04:05Z07:00")
		response.LastReminderSent = &lastReminder
	}

	return response
}

func toStatsResponse(stats *models.DocCompletionStats) *DocumentStatsResponse {
	return &DocumentStatsResponse{
		DocID:          stats.DocID,
		ExpectedCount:  stats.ExpectedCount,
		SignedCount:    stats.SignedCount,
		PendingCount:   stats.PendingCount,
		CompletionRate: stats.CompletionRate,
	}
}

// SendRemindersRequest represents the request body for sending reminders
type SendRemindersRequest struct {
	Emails []string `json:"emails,omitempty"` // If empty, send to all pending signers
}

// HandleSendReminders handles POST /api/v1/admin/documents/{docId}/reminders
func (h *Handler) HandleSendReminders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	docID := chi.URLParam(r, "docId")

	if docID == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Document ID is required", nil)
		return
	}

	// Check if reminder service is available
	if h.reminderService == nil {
		shared.WriteError(w, http.StatusServiceUnavailable, shared.ErrCodeInternal, "Reminder service not configured", nil)
		return
	}

	// Get user from context
	user, ok := shared.GetUserFromContext(ctx)
	if !ok {
		shared.WriteUnauthorized(w, "")
		return
	}

	// Parse request body
	var req SendRemindersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Invalid request body", nil)
		return
	}

	// Check reminder quota before sending
	if h.quotaRecorder != nil {
		tenantID := h.getTenantID(ctx)
		if tenantID != "" {
			if err := h.quotaRecorder.CheckReminderSend(ctx, tenantID); err != nil {
				shared.WriteError(w, http.StatusForbidden, shared.ErrCodeForbidden, "Quota exceeded", nil)
				return
			}
		}
	}

	// Get document URL from metadata
	var docURL string
	if doc, err := h.adminService.GetDocument(ctx, docID); err == nil && doc != nil && doc.URL != "" {
		docURL = doc.URL
	}

	// Get locale from request using i18n helper
	locale := i18n.GetLangFromRequest(r)

	// Send reminders
	result, err := h.reminderService.SendReminders(ctx, docID, user.Email, req.Emails, docURL, locale)
	if err != nil {
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to send reminders", nil)
		return
	}

	// Record reminder usage (one record per reminder sent)
	if h.quotaRecorder != nil && result.TotalAttempted > 0 {
		tenantID := h.getTenantID(ctx)
		if tenantID != "" {
			for i := 0; i < result.TotalAttempted; i++ {
				if err := h.quotaRecorder.RecordReminderSend(ctx, tenantID); err != nil {
					logger.Logger.Warn("Failed to record reminder quota usage",
						"tenant_id", tenantID,
						"error", err.Error())
					break
				}
			}
		}
	}

	// Audit log
	shared.EmitAudit(ctx, h.auditLogger, r, h.getTenantID(ctx), "reminder.send", "document", docID, map[string]any{"count": result.TotalAttempted})

	shared.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Reminders sent",
		"result":  result,
	})
}

// ReminderLogResponse represents a reminder log entry in API responses
type ReminderLogResponse struct {
	ID             int64   `json:"id"`
	DocID          string  `json:"docId"`
	RecipientEmail string  `json:"recipientEmail"`
	SentAt         string  `json:"sentAt"`
	SentBy         string  `json:"sentBy"`
	TemplateUsed   string  `json:"templateUsed"`
	Status         string  `json:"status"`
	ErrorMessage   *string `json:"errorMessage,omitempty"`
}

// HandleGetReminderHistory handles GET /api/v1/admin/documents/{docId}/reminders
func (h *Handler) HandleGetReminderHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	docID := chi.URLParam(r, "docId")

	if docID == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Document ID is required", nil)
		return
	}

	// Check if reminder service is available
	if h.reminderService == nil {
		shared.WriteError(w, http.StatusServiceUnavailable, shared.ErrCodeInternal, "Reminder service not configured", nil)
		return
	}

	history, err := h.reminderService.GetReminderHistory(ctx, docID)
	if err != nil {
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to get reminder history", nil)
		return
	}

	response := make([]*ReminderLogResponse, 0, len(history))
	for _, log := range history {
		response = append(response, &ReminderLogResponse{
			ID:             log.ID,
			DocID:          log.DocID,
			RecipientEmail: log.RecipientEmail,
			SentAt:         log.SentAt.Format("2006-01-02T15:04:05Z07:00"),
			SentBy:         log.SentBy,
			TemplateUsed:   log.TemplateUsed,
			Status:         log.Status,
			ErrorMessage:   log.ErrorMessage,
		})
	}

	shared.WriteJSON(w, http.StatusOK, response)
}

// UpdateDocumentMetadataRequest represents the request body for updating document metadata
type UpdateDocumentMetadataRequest struct {
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

// HandleUpdateDocumentMetadata handles PUT /api/v1/admin/documents/{docId}/metadata
func (h *Handler) HandleUpdateDocumentMetadata(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	docID := chi.URLParam(r, "docId")

	if docID == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Document ID is required", nil)
		return
	}

	// Get user from context
	user, ok := shared.GetUserFromContext(ctx)
	if !ok {
		shared.WriteUnauthorized(w, "")
		return
	}

	// Parse request body
	var req UpdateDocumentMetadataRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Invalid request body", nil)
		return
	}

	// Get existing document or create new one
	doc, err := h.adminService.GetDocument(ctx, docID)
	if err != nil || doc == nil {
		// Document doesn't exist, create a new one
		doc = &models.Document{
			DocID:     docID,
			CreatedBy: user.Email,
		}
	}

	// Update fields if provided
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

	// Save document using CreateOrUpdate (preserve storage fields from existing document)
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
	doc, err = h.adminService.UpdateDocumentMetadata(ctx, docID, input, user.Email)
	if err != nil {
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to update document metadata", nil)
		return
	}

	shared.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"message":  "Document metadata updated successfully",
		"document": toDocumentResponse(doc),
	})
}

// DocumentStatusResponse represents complete document status including everything
type DocumentStatusResponse struct {
	DocID                string                         `json:"docId"`
	Document             *DocumentResponse              `json:"document,omitempty"`
	ExpectedSigners      []*ExpectedSignerResponse      `json:"expectedSigners"`
	UnexpectedSignatures []*UnexpectedSignatureResponse `json:"unexpectedSignatures"`
	Stats                *DocumentStatsResponse         `json:"stats"`
	ReminderStats        *ReminderStatsResponse         `json:"reminderStats,omitempty"`
	ShareLink            string                         `json:"shareLink"`
}

// ReminderStatsResponse represents reminder statistics
type ReminderStatsResponse struct {
	TotalSent    int     `json:"totalSent"`
	PendingCount int     `json:"pendingCount"`
	LastSentAt   *string `json:"lastSentAt,omitempty"`
}

// HandleGetDocumentStatus handles GET /api/v1/admin/documents/{docId}/status
func (h *Handler) HandleGetDocumentStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	docID := chi.URLParam(r, "docId")

	if docID == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Document ID is required", nil)
		return
	}

	response := &DocumentStatusResponse{
		DocID:                docID,
		ExpectedSigners:      []*ExpectedSignerResponse{},
		UnexpectedSignatures: []*UnexpectedSignatureResponse{},
		ShareLink:            h.baseURL + "/?doc=" + docID,
	}

	// Get document (optional)
	if doc, err := h.adminService.GetDocument(ctx, docID); err == nil && doc != nil {
		response.Document = toDocumentResponse(doc)
	}

	// Get expected signers with status
	expectedEmails := make(map[string]bool)
	if signers, err := h.adminService.ListExpectedSignersWithStatus(ctx, docID); err == nil {
		for _, signer := range signers {
			response.ExpectedSigners = append(response.ExpectedSigners, toExpectedSignerResponse(signer))
			expectedEmails[strings.ToLower(signer.Email)] = true
		}
	}

	// Get all signatures for this document and find unexpected ones
	if h.signatureService != nil {
		if signatures, err := h.signatureService.GetDocumentSignatures(ctx, docID); err == nil {
			for _, sig := range signatures {
				// If this signature's email is not in the expected list, it's unexpected
				if !expectedEmails[strings.ToLower(sig.UserEmail)] {
					userName := sig.UserName
					response.UnexpectedSignatures = append(response.UnexpectedSignatures, &UnexpectedSignatureResponse{
						UserEmail:   sig.UserEmail,
						UserName:    &userName,
						SignedAtUTC: sig.SignedAtUTC.Format("2006-01-02T15:04:05Z07:00"),
					})
				}
			}
		}
	}

	// Get completion stats
	if stats, err := h.adminService.GetSignerStats(ctx, docID); err == nil {
		response.Stats = toStatsResponse(stats)
	} else {
		// Default stats if no expected signers
		response.Stats = &DocumentStatsResponse{
			DocID:          docID,
			ExpectedCount:  0,
			SignedCount:    0,
			PendingCount:   0,
			CompletionRate: 0,
		}
	}

	// Get reminder stats if service available
	if h.reminderService != nil {
		reminderStats, err := h.reminderService.GetReminderStats(ctx, docID)
		if err == nil && reminderStats != nil {
			var lastSentAt *string
			if reminderStats.LastSentAt != nil {
				formatted := reminderStats.LastSentAt.Format("2006-01-02T15:04:05Z07:00")
				lastSentAt = &formatted
			}
			response.ReminderStats = &ReminderStatsResponse{
				TotalSent:    reminderStats.TotalSent,
				PendingCount: reminderStats.PendingCount,
				LastSentAt:   lastSentAt,
			}
		} else if err != nil {
			logger.Logger.Debug("Failed to get reminder stats", "doc_id", docID, "error", err.Error())
		}
	}

	shared.WriteJSON(w, http.StatusOK, response)
}

// HandleDeleteDocument handles DELETE /api/v1/admin/documents/{docId}
func (h *Handler) HandleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	docID := chi.URLParam(r, "docId")

	if docID == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Document ID is required", nil)
		return
	}

	// Delete document (this will cascade delete signatures and expected signers due to DB constraints)
	err := h.adminService.DeleteDocument(ctx, docID)
	if err != nil {
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
	shared.EmitAudit(ctx, h.auditLogger, r, h.getTenantID(ctx), "document.delete", "document", docID, nil)

	shared.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Document deleted successfully",
	})
}

// CSVPreviewResponse represents the response for CSV preview
type CSVPreviewResponse struct {
	Signers        []services.CSVSignerEntry `json:"signers"`
	Errors         []services.CSVParseError  `json:"errors"`
	TotalLines     int                       `json:"totalLines"`
	ValidCount     int                       `json:"validCount"`
	InvalidCount   int                       `json:"invalidCount"`
	HasHeader      bool                      `json:"hasHeader"`
	ExistingEmails []string                  `json:"existingEmails"`
	MaxSigners     int                       `json:"maxSigners"`
}

// HandlePreviewCSV handles POST /api/v1/admin/documents/{docId}/signers/preview-csv
func (h *Handler) HandlePreviewCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	docID := chi.URLParam(r, "docId")

	if docID == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Document ID is required", nil)
		return
	}

	// Limit file size to 1MB
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB

	// Parse multipart form
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "File too large or invalid form data", nil)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "CSV file is required", nil)
		return
	}
	defer file.Close()

	// Read file content
	content, err := io.ReadAll(file)
	if err != nil {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Failed to read file", nil)
		return
	}

	// Parse CSV
	parser := services.NewCSVParser(h.importMaxSigners)
	result, err := parser.Parse(strings.NewReader(string(content)))
	if err != nil {
		logger.Logger.Error("Failed to parse CSV", "error", err.Error(), "doc_id", docID)
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, fmt.Sprintf("Failed to parse CSV: %s", err.Error()), nil)
		return
	}

	// Get existing signers for this document to identify duplicates
	existingEmails := []string{}
	existingSigners, err := h.adminService.ListExpectedSigners(ctx, docID)
	if err == nil {
		existingEmailsMap := make(map[string]bool)
		for _, signer := range existingSigners {
			existingEmailsMap[strings.ToLower(signer.Email)] = true
		}

		// Check which emails from the CSV already exist
		for _, entry := range result.Signers {
			if existingEmailsMap[strings.ToLower(entry.Email)] {
				existingEmails = append(existingEmails, entry.Email)
			}
		}
	}

	response := CSVPreviewResponse{
		Signers:        result.Signers,
		Errors:         result.Errors,
		TotalLines:     result.TotalLines,
		ValidCount:     result.ValidCount,
		InvalidCount:   result.InvalidCount,
		HasHeader:      result.HasHeader,
		ExistingEmails: existingEmails,
		MaxSigners:     h.importMaxSigners,
	}

	shared.WriteJSON(w, http.StatusOK, response)
}

// ImportSignersRequest represents the request body for importing signers
type ImportSignersRequest struct {
	Signers []ImportSignerEntry `json:"signers"`
}

// ImportSignerEntry represents a single signer to import
type ImportSignerEntry struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

// ImportSignersResponse represents the response for signer import
type ImportSignersResponse struct {
	Message  string `json:"message"`
	Imported int    `json:"imported"`
	Skipped  int    `json:"skipped"`
	Total    int    `json:"total"`
}

// HandleImportSigners handles POST /api/v1/admin/documents/{docId}/signers/import
func (h *Handler) HandleImportSigners(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	docID := chi.URLParam(r, "docId")

	if docID == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Document ID is required", nil)
		return
	}

	// Get user from context
	user, ok := shared.GetUserFromContext(ctx)
	if !ok {
		shared.WriteUnauthorized(w, "")
		return
	}

	// Parse request body
	var req ImportSignersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Invalid request body", nil)
		return
	}

	// Validate signers count
	if len(req.Signers) == 0 {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "At least one signer is required", nil)
		return
	}

	if len(req.Signers) > h.importMaxSigners {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest,
			fmt.Sprintf("Maximum %d signers per import (received %d)", h.importMaxSigners, len(req.Signers)), nil)
		return
	}

	// Get existing signers to calculate skipped count
	existingEmailsMap := make(map[string]bool)
	existingSigners, err := h.adminService.ListExpectedSigners(ctx, docID)
	if err == nil {
		for _, signer := range existingSigners {
			existingEmailsMap[strings.ToLower(signer.Email)] = true
		}
	}

	// Count how many will be skipped (already exist)
	skippedCount := 0
	for _, signer := range req.Signers {
		if existingEmailsMap[strings.ToLower(signer.Email)] {
			skippedCount++
		}
	}

	// Convert to ContactInfo slice
	contacts := make([]models.ContactInfo, 0, len(req.Signers))
	for _, signer := range req.Signers {
		contacts = append(contacts, models.ContactInfo{
			Email: strings.ToLower(strings.TrimSpace(signer.Email)),
			Name:  strings.TrimSpace(signer.Name),
		})
	}

	importedCount := len(req.Signers) - skippedCount

	// Check signer quota for new signers only
	if h.quotaRecorder != nil && importedCount > 0 {
		tenantID := h.getTenantID(ctx)
		if tenantID != "" {
			for range importedCount {
				if err := h.quotaRecorder.CheckSignerAdd(ctx, tenantID); err != nil {
					shared.WriteError(w, http.StatusForbidden, shared.ErrCodeForbidden, "Quota exceeded", nil)
					return
				}
			}
		}
	}

	// Add all signers (repository handles duplicates with ON CONFLICT DO NOTHING)
	if err := h.adminService.AddExpectedSigners(ctx, docID, contacts, user.Email); err != nil {
		logger.Logger.Error("Failed to import signers", "error", err.Error(), "doc_id", docID, "count", len(contacts))
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to import signers", nil)
		return
	}

	// Record signer quota for each imported signer
	if h.quotaRecorder != nil && importedCount > 0 {
		tenantID := h.getTenantID(ctx)
		if tenantID != "" {
			for range importedCount {
				if err := h.quotaRecorder.RecordSignerAdd(ctx, tenantID); err != nil {
					logger.Logger.Warn("Failed to record signer add quota", "tenant_id", tenantID, "error", err.Error())
				}
			}
		}
	}

	logger.Logger.Info("Signers imported successfully",
		"doc_id", docID,
		"imported", importedCount,
		"skipped", skippedCount,
		"total", len(req.Signers),
		"imported_by", user.Email)

	shared.WriteJSON(w, http.StatusOK, ImportSignersResponse{
		Message:  "Import completed",
		Imported: importedCount,
		Skipped:  skippedCount,
		Total:    len(req.Signers),
	})
}
