// SPDX-License-Identifier: AGPL-3.0-or-later
package admin

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/btouchard/ackify-ce/backend/internal/infrastructure/i18n"
	"github.com/btouchard/ackify-ce/backend/internal/presentation/api/shared"
	"github.com/btouchard/ackify-ce/backend/pkg/logger"
	"github.com/btouchard/ackify-ce/backend/pkg/providers"
	"github.com/go-chi/chi/v5"
)

// ShareHandler handles admin document sharing API requests
type ShareHandler struct {
	authProvider providers.AuthProvider
}

// NewShareHandler creates a new share handler
func NewShareHandler(authProvider providers.AuthProvider) *ShareHandler {
	return &ShareHandler{
		authProvider: authProvider,
	}
}

// HandleCreateDocumentShare handles POST /api/v1/admin/documents/{docId}/share
func (h *ShareHandler) HandleCreateDocumentShare(w http.ResponseWriter, r *http.Request) {
	docID := chi.URLParam(r, "docId")
	if docID == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeValidation, "Document ID is required", nil)
		return
	}

	var req struct {
		Email        string `json:"email"`
		ValidityDays int    `json:"validity_days"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeValidation, "Invalid request body", nil)
		return
	}

	if req.Email == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeValidation, "Email is required", nil)
		return
	}

	// Get the current admin user
	currentUser, err := h.authProvider.GetCurrentUser(r)
	if err != nil || currentUser == nil {
		shared.WriteError(w, http.StatusUnauthorized, shared.ErrCodeUnauthorized, "Not authenticated", nil)
		return
	}

	ctx := r.Context()
	locale := i18n.GetLang(ctx)

	linkURL, otp, err := h.authProvider.CreateDocumentShareLink(ctx, req.Email, docID, req.ValidityDays, currentUser.Email, locale)
	if err != nil {
		logger.Logger.Error("Failed to create document share", "error", err.Error())
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to create document share", nil)
		return
	}

	shared.WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"otp":  otp,
		"link": linkURL,
	})
}

// HandleListDocumentShares handles GET /api/v1/admin/documents/{docId}/shares
func (h *ShareHandler) HandleListDocumentShares(w http.ResponseWriter, r *http.Request) {
	docID := chi.URLParam(r, "docId")
	if docID == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeValidation, "Document ID is required", nil)
		return
	}

	ctx := r.Context()
	shares, err := h.authProvider.ListDocumentShares(ctx, docID)
	if err != nil {
		logger.Logger.Error("Failed to list document shares", "error", err.Error())
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to list shares", nil)
		return
	}

	if shares == nil {
		shares = []*providers.DocumentShareInfo{}
	}

	shared.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"shares": shares,
	})
}

// HandleRevokeDocumentShare handles DELETE /api/v1/admin/documents/{docId}/share/{tokenId}
func (h *ShareHandler) HandleRevokeDocumentShare(w http.ResponseWriter, r *http.Request) {
	docID := chi.URLParam(r, "docId")
	if docID == "" {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeValidation, "Document ID is required", nil)
		return
	}

	tokenIDStr := chi.URLParam(r, "tokenId")
	tokenID, err := strconv.ParseInt(tokenIDStr, 10, 64)
	if err != nil {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeValidation, "Invalid token ID", nil)
		return
	}

	ctx := r.Context()
	if err := h.authProvider.RevokeDocumentShare(ctx, tokenID); err != nil {
		logger.Logger.Error("Failed to revoke document share", "error", err.Error())
		shared.WriteError(w, http.StatusInternalServerError, shared.ErrCodeInternal, "Failed to revoke share", nil)
		return
	}

	shared.WriteJSON(w, http.StatusOK, map[string]string{
		"message": "Share revoked successfully",
	})
}
