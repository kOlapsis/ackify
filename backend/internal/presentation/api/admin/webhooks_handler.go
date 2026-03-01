// SPDX-License-Identifier: AGPL-3.0-or-later
package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/btouchard/ackify-ce/backend/internal/presentation/api/shared"
	"github.com/btouchard/ackify-ce/backend/pkg/models"
	"github.com/btouchard/ackify-ce/backend/pkg/providers"
	"github.com/go-chi/chi/v5"
)

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

// WebhookQuotaRecorder tracks webhook quota usage.
type WebhookQuotaRecorder interface {
	CheckWebhookCreate(ctx context.Context, tenantID string) error
}

// WebhooksHandler groups operations on webhooks
type WebhooksHandler struct {
	service        webhookService
	quotaRecorder  WebhookQuotaRecorder
	tenantProvider providers.TenantProvider
	auditLogger    shared.AuditLogger
}

func NewWebhooksHandler(service webhookService) *WebhooksHandler {
	return &WebhooksHandler{service: service}
}

// WithQuotaRecorder sets the quota recorder for webhook creation tracking.
func (h *WebhooksHandler) WithQuotaRecorder(recorder WebhookQuotaRecorder, tp providers.TenantProvider) *WebhooksHandler {
	h.quotaRecorder = recorder
	h.tenantProvider = tp
	return h
}

// WithAuditLogger sets the audit logger for webhook events.
func (h *WebhooksHandler) WithAuditLogger(logger shared.AuditLogger) *WebhooksHandler {
	h.auditLogger = logger
	return h
}

func (h *WebhooksHandler) getTenantID(ctx context.Context) string {
	if h.tenantProvider == nil {
		return ""
	}
	tid, err := h.tenantProvider.CurrentTenant(ctx)
	if err != nil {
		return ""
	}
	return tid.String()
}

type CreateWebhookRequest struct {
	Title       string            `json:"title"`
	TargetURL   string            `json:"targetUrl"`
	Secret      string            `json:"secret"`
	Active      bool              `json:"active"`
	Events      []string          `json:"events"`
	Headers     map[string]string `json:"headers,omitempty"`
	Description string            `json:"description,omitempty"`
}

func (h *WebhooksHandler) HandleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req CreateWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Invalid request body", nil)
		return
	}
	if req.Title == "" || req.TargetURL == "" || req.Secret == "" || len(req.Events) == 0 {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "title, targetUrl, secret and events are required", nil)
		return
	}

	// Check webhook quota before creation
	if h.quotaRecorder != nil {
		tenantID := h.getTenantID(ctx)
		if tenantID != "" {
			if err := h.quotaRecorder.CheckWebhookCreate(ctx, tenantID); err != nil {
				shared.WriteError(w, http.StatusForbidden, shared.ErrCodeForbidden, "Quota exceeded", nil)
				return
			}
		}
	}

	user, _ := shared.GetUserFromContext(ctx)
	input := models.WebhookInput{Title: req.Title, TargetURL: req.TargetURL, Secret: req.Secret, Active: req.Active, Events: req.Events, Headers: req.Headers, Description: req.Description}
	if user != nil {
		input.CreatedBy = user.Email
	}
	wh, err := h.service.CreateWebhook(ctx, input)
	if err != nil {
		shared.WriteInternalError(w)
		return
	}

	// Audit log
	shared.EmitAudit(ctx, h.auditLogger, r, h.getTenantID(ctx), "webhook.create", "webhook", strconv.FormatInt(wh.ID, 10), map[string]any{"title": req.Title, "target_url": req.TargetURL})

	shared.WriteJSON(w, http.StatusCreated, wh)
}

func (h *WebhooksHandler) HandleListWebhooks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	limit := 100
	offset := 0
	list, err := h.service.ListWebhooks(ctx, limit, offset)
	if err != nil {
		shared.WriteInternalError(w)
		return
	}
	meta := map[string]interface{}{"total": len(list), "limit": limit, "offset": offset}
	shared.WriteJSONWithMeta(w, http.StatusOK, list, meta)
}

func (h *WebhooksHandler) HandleGetWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	wh, err := h.service.GetWebhookByID(ctx, id)
	if err != nil {
		shared.WriteError(w, http.StatusNotFound, shared.ErrCodeNotFound, "Webhook not found", nil)
		return
	}
	shared.WriteJSON(w, http.StatusOK, wh)
}

func (h *WebhooksHandler) HandleUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req CreateWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Invalid request body", nil)
		return
	}
	// For updates, allow empty secret to keep current value
	if req.Title == "" || req.TargetURL == "" || len(req.Events) == 0 {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "title, targetUrl and events are required", nil)
		return
	}
	input := models.WebhookInput{Title: req.Title, TargetURL: req.TargetURL, Secret: req.Secret, Active: req.Active, Events: req.Events, Headers: req.Headers, Description: req.Description}
	wh, err := h.service.UpdateWebhook(ctx, id, input)
	if err != nil {
		shared.WriteInternalError(w)
		return
	}
	shared.WriteJSON(w, http.StatusOK, wh)
}

func (h *WebhooksHandler) HandleToggleWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	enable := chi.URLParam(r, "action") == "enable"
	if err := h.service.SetWebhookActive(ctx, id, enable); err != nil {
		shared.WriteInternalError(w)
		return
	}
	status := "disabled"
	if enable {
		status = "enabled"
	}
	shared.WriteJSON(w, http.StatusOK, map[string]string{"message": "Webhook " + status})
}

func (h *WebhooksHandler) HandleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err := h.service.DeleteWebhook(ctx, id); err != nil {
		shared.WriteInternalError(w)
		return
	}

	// Audit log
	shared.EmitAudit(ctx, h.auditLogger, r, h.getTenantID(ctx), "webhook.delete", "webhook", strconv.FormatInt(id, 10), nil)

	shared.WriteJSON(w, http.StatusOK, map[string]string{"message": "Webhook deleted"})
}

func (h *WebhooksHandler) HandleListDeliveries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	deliveries, err := h.service.ListDeliveries(ctx, id, 100, 0)
	if err != nil {
		shared.WriteInternalError(w)
		return
	}
	shared.WriteJSON(w, http.StatusOK, deliveries)
}
