// SPDX-License-Identifier: AGPL-3.0-or-later
package signatures

import (
	"context"
	"encoding/json"
	"net/http"

	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kolapsis/ackify/backend/internal/presentation/api/shared"
	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/models"
	"github.com/kolapsis/ackify/backend/pkg/providers"
)

// signatureService defines the interface for signature operations
type signatureService interface {
	CreateSignature(ctx context.Context, request *models.SignatureRequest) error
	GetSignatureStatus(ctx context.Context, docID string, user *models.User) (*models.SignatureStatus, error)
	GetSignatureByDocAndUser(ctx context.Context, docID string, user *models.User) (*models.Signature, error)
	GetDocumentSignatures(ctx context.Context, docID string) ([]*models.Signature, error)
	GetUserSignatures(ctx context.Context, user *models.User) ([]*models.Signature, error)
}

// adminService defines minimal admin operations needed for stats
type adminService interface {
	GetSignerStats(ctx context.Context, docID string) (*models.DocCompletionStats, error)
}

// webhookPublisher defines minimal publish capability
type webhookPublisher interface {
	Publish(ctx context.Context, eventType string, payload map[string]interface{}) error
}

// QuotaRecorder tracks signature quota usage.
type QuotaRecorder interface {
	CheckSignatureCreation(ctx context.Context, tenantID string) error
	RecordSignatureCreation(ctx context.Context, tenantID string) error
}

// Handler handles signature-related requests
type Handler struct {
	signatureService signatureService
	adminService     adminService
	webhookPublisher webhookPublisher
	quotaRecorder    QuotaRecorder
	tenantProvider   providers.TenantProvider
	auditLogger      shared.AuditLogger
}

// NewHandler constructor to inject admin service and webhook publisher
func NewHandler(signatureService signatureService, adminSvc adminService, publisher webhookPublisher) *Handler {
	return &Handler{signatureService: signatureService, adminService: adminSvc, webhookPublisher: publisher}
}

// WithQuotaRecorder sets the quota recorder for signature tracking.
func (h *Handler) WithQuotaRecorder(recorder QuotaRecorder, tp providers.TenantProvider) *Handler {
	h.quotaRecorder = recorder
	h.tenantProvider = tp
	return h
}

// WithAuditLogger sets the audit logger for signature events.
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

// CreateSignatureRequest represents the request body for creating a signature
type CreateSignatureRequest struct {
	DocID   string  `json:"docId"`
	Referer *string `json:"referer,omitempty"`
}

// SignatureResponse represents a signature in API responses
type SignatureResponse struct {
	ID           int64              `json:"id"`
	DocID        string             `json:"docId"`
	UserSub      string             `json:"userSub"`
	UserEmail    string             `json:"userEmail"`
	UserName     string             `json:"userName,omitempty"`
	SignedAt     string             `json:"signedAt"`
	PayloadHash  string             `json:"payloadHash"`
	Signature    string             `json:"signature"`
	Nonce        string             `json:"nonce"`
	CreatedAt    string             `json:"createdAt"`
	Referer      *string            `json:"referer,omitempty"`
	PrevHash     *string            `json:"prevHash,omitempty"`
	ServiceInfo  *ServiceInfoResult `json:"serviceInfo,omitempty"`
	DocDeletedAt *string            `json:"docDeletedAt,omitempty"`
	// Document metadata
	DocTitle *string `json:"docTitle,omitempty"`
	DocUrl   *string `json:"docUrl,omitempty"`
}

// ServiceInfoResult represents service detection information
type ServiceInfoResult struct {
	Name     string `json:"name"`
	Icon     string `json:"icon"`
	Type     string `json:"type"`
	Referrer string `json:"referrer"`
}

// SignatureStatusResponse represents the signature status for a document
type SignatureStatusResponse struct {
	DocID     string  `json:"docId"`
	UserEmail string  `json:"userEmail"`
	IsSigned  bool    `json:"isSigned"`
	SignedAt  *string `json:"signedAt,omitempty"`
}

// HandleCreateSignature handles POST /api/v1/signatures
func (h *Handler) HandleCreateSignature(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user, ok := shared.GetUserFromContext(ctx)
	if !ok || user == nil {
		shared.WriteUnauthorized(w, "Authentication required")
		return
	}

	var req CreateSignatureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Invalid request body", map[string]interface{}{"error": err.Error()})
		return
	}

	if req.DocID == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Document ID is required", nil)
		return
	}

	// Check signature quota
	if h.quotaRecorder != nil {
		tenantID := h.getTenantID(ctx)
		if tenantID != "" {
			if err := h.quotaRecorder.CheckSignatureCreation(ctx, tenantID); err != nil {
				shared.WriteError(w, http.StatusForbidden, shared.ErrCodeForbidden, "Quota exceeded", nil)
				return
			}
		}
	}

	sigRequest := &models.SignatureRequest{
		DocID:   req.DocID,
		User:    user,
		Referer: req.Referer,
	}

	err := h.signatureService.CreateSignature(ctx, sigRequest)
	if err != nil {
		if err == models.ErrSignatureAlreadyExists {
			shared.WriteConflict(w, "You have already signed this document")
			return
		}

		if err == models.ErrInvalidDocument {
			shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Invalid document", nil)
			return
		}

		if err == models.ErrDocumentModified {
			shared.WriteError(w, http.StatusConflict, "DOCUMENT_MODIFIED", "The document has been modified since it was created. Please verify the current version before signing.", map[string]interface{}{
				"docId": req.DocID,
			})
			return
		}

		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to create signature", map[string]interface{}{"error": err.Error()})
		return
	}

	// Record signature quota usage
	if h.quotaRecorder != nil {
		tenantID := h.getTenantID(ctx)
		if tenantID != "" {
			if err := h.quotaRecorder.RecordSignatureCreation(ctx, tenantID); err != nil {
				logger.Logger.Warn("Failed to record signature creation quota", "tenant_id", tenantID, "error", err.Error())
			}
		}
	}

	// Publish signature.created webhook
	if h.webhookPublisher != nil {
		_ = h.webhookPublisher.Publish(ctx, "signature.created", map[string]interface{}{
			"doc_id":     req.DocID,
			"user_email": user.Email,
			"user_name":  user.Name,
		})
	}

	// If expected signers completed -> publish document.completed
	if h.adminService != nil && h.webhookPublisher != nil {
		if stats, err := h.adminService.GetSignerStats(ctx, req.DocID); err == nil {
			if stats.ExpectedCount > 0 && stats.PendingCount == 0 {
				_ = h.webhookPublisher.Publish(ctx, "document.completed", map[string]interface{}{
					"doc_id":         req.DocID,
					"completed_at":   time.Now().UTC().Format("2006-01-02T15:04:05Z07:00"),
					"expected_count": stats.ExpectedCount,
					"signed_count":   stats.SignedCount,
				})
			}
		}
	}

	// Audit log
	shared.EmitAudit(ctx, h.auditLogger, r, h.getTenantID(ctx), "signature.create", "document", req.DocID, map[string]any{"signer_email": user.Email})

	signature, err := h.signatureService.GetSignatureByDocAndUser(ctx, req.DocID, user)
	if err != nil {
		shared.WriteJSON(w, http.StatusCreated, map[string]interface{}{
			"message": "Signature created successfully",
			"docId":   req.DocID,
		})
		return
	}

	shared.WriteJSON(w, http.StatusCreated, h.toSignatureResponse(ctx, signature))
}

// HandleGetUserSignatures handles GET /api/v1/signatures
func (h *Handler) HandleGetUserSignatures(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user, ok := shared.GetUserFromContext(ctx)
	if !ok || user == nil {
		shared.WriteUnauthorized(w, "Authentication required")
		return
	}

	signatures, err := h.signatureService.GetUserSignatures(ctx, user)
	if err != nil {
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to fetch signatures", map[string]interface{}{"error": err.Error()})
		return
	}

	response := make([]*SignatureResponse, 0, len(signatures))
	for _, sig := range signatures {
		response = append(response, h.toSignatureResponse(ctx, sig))
	}

	shared.WriteJSON(w, http.StatusOK, response)
}

// HandleGetDocumentSignatures handles GET /api/v1/documents/{docId}/signatures
func (h *Handler) HandleGetDocumentSignatures(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	docID := chi.URLParam(r, "docId")
	if docID == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Document ID is required", nil)
		return
	}

	signatures, err := h.signatureService.GetDocumentSignatures(ctx, docID)
	if err != nil {
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to fetch signatures", map[string]interface{}{"error": err.Error()})
		return
	}

	response := make([]*SignatureResponse, 0, len(signatures))
	for _, sig := range signatures {
		response = append(response, h.toSignatureResponse(ctx, sig))
	}

	shared.WriteJSON(w, http.StatusOK, response)
}

// HandleGetSignatureStatus handles GET /api/v1/documents/{docId}/signatures/status
func (h *Handler) HandleGetSignatureStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user, ok := shared.GetUserFromContext(ctx)
	if !ok || user == nil {
		shared.WriteUnauthorized(w, "Authentication required")
		return
	}

	docID := chi.URLParam(r, "docId")
	if docID == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Document ID is required", nil)
		return
	}

	status, err := h.signatureService.GetSignatureStatus(ctx, docID, user)
	if err != nil {
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to fetch signature status", map[string]interface{}{"error": err.Error()})
		return
	}

	response := SignatureStatusResponse{
		DocID:     status.DocID,
		UserEmail: status.UserEmail,
		IsSigned:  status.IsSigned,
	}

	if status.SignedAt != nil {
		signedAt := status.SignedAt.Format("2006-01-02T15:04:05Z07:00")
		response.SignedAt = &signedAt
	}

	shared.WriteJSON(w, http.StatusOK, response)
}

// toSignatureResponse converts a domain signature to API response format
func (h *Handler) toSignatureResponse(ctx context.Context, sig *models.Signature) *SignatureResponse {
	response := &SignatureResponse{
		ID:          sig.ID,
		DocID:       sig.DocID,
		UserSub:     sig.UserSub,
		UserEmail:   sig.UserEmail,
		UserName:    sig.UserName,
		SignedAt:    sig.SignedAtUTC.Format("2006-01-02T15:04:05Z07:00"),
		PayloadHash: sig.PayloadHash,
		Signature:   sig.Signature,
		Nonce:       sig.Nonce,
		CreatedAt:   sig.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		Referer:     sig.Referer,
		PrevHash:    sig.PrevHash,
	}

	// Add doc_deleted_at if document was deleted
	if sig.DocDeletedAt != nil {
		deletedAt := sig.DocDeletedAt.Format("2006-01-02T15:04:05Z07:00")
		response.DocDeletedAt = &deletedAt
	}

	// Add service info if available
	if serviceInfo := sig.GetServiceInfo(); serviceInfo != nil {
		response.ServiceInfo = &ServiceInfoResult{
			Name:     serviceInfo.Name,
			Icon:     serviceInfo.Icon,
			Type:     serviceInfo.Type,
			Referrer: serviceInfo.Referrer,
		}
	}

	// Document metadata is enriched from LEFT JOIN in repository
	if sig.DocTitle != "" {
		response.DocTitle = &sig.DocTitle
	}
	if sig.DocURL != "" {
		response.DocUrl = &sig.DocURL
	}

	return response
}
