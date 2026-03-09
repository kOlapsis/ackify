// SPDX-License-Identifier: AGPL-3.0-or-later
package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/kolapsis/ackify/backend/internal/presentation/api/shared"
	"github.com/kolapsis/ackify/backend/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// MOCKS
// ============================================================================

type mockWebhookService struct {
	createFunc    func(ctx context.Context, input models.WebhookInput) (*models.Webhook, error)
	updateFunc    func(ctx context.Context, id int64, input models.WebhookInput) (*models.Webhook, error)
	setActiveFunc func(ctx context.Context, id int64, active bool) error
	deleteFunc    func(ctx context.Context, id int64) error
	getByIDFunc   func(ctx context.Context, id int64) (*models.Webhook, error)
	listFunc      func(ctx context.Context, limit, offset int) ([]*models.Webhook, error)
	listDelFunc   func(ctx context.Context, webhookID int64, limit, offset int) ([]*models.WebhookDelivery, error)
}

func (m *mockWebhookService) CreateWebhook(ctx context.Context, input models.WebhookInput) (*models.Webhook, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, input)
	}
	return &models.Webhook{ID: 1, Title: input.Title}, nil
}

func (m *mockWebhookService) UpdateWebhook(ctx context.Context, id int64, input models.WebhookInput) (*models.Webhook, error) {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, id, input)
	}
	return &models.Webhook{ID: id, Title: input.Title}, nil
}

func (m *mockWebhookService) SetWebhookActive(ctx context.Context, id int64, active bool) error {
	if m.setActiveFunc != nil {
		return m.setActiveFunc(ctx, id, active)
	}
	return nil
}

func (m *mockWebhookService) DeleteWebhook(ctx context.Context, id int64) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

func (m *mockWebhookService) GetWebhookByID(ctx context.Context, id int64) (*models.Webhook, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	return &models.Webhook{ID: id, Title: "Test"}, nil
}

func (m *mockWebhookService) ListWebhooks(ctx context.Context, limit, offset int) ([]*models.Webhook, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, limit, offset)
	}
	return []*models.Webhook{}, nil
}

func (m *mockWebhookService) ListDeliveries(ctx context.Context, webhookID int64, limit, offset int) ([]*models.WebhookDelivery, error) {
	if m.listDelFunc != nil {
		return m.listDelFunc(ctx, webhookID, limit, offset)
	}
	return []*models.WebhookDelivery{}, nil
}

type mockWebhookQuotaRecorder struct {
	checkCalled bool
	checkErr    error
}

func (m *mockWebhookQuotaRecorder) CheckWebhookCreate(_ context.Context, _ string) error {
	m.checkCalled = true
	return m.checkErr
}

type mockWebhookTenantProvider struct {
	tenantID uuid.UUID
	err      error
}

func (m *mockWebhookTenantProvider) CurrentTenant(_ context.Context) (uuid.UUID, error) {
	return m.tenantID, m.err
}

type mockWebhookAuditLogger struct {
	events []shared.AuditEvent
}

func (m *mockWebhookAuditLogger) Log(_ context.Context, event shared.AuditEvent) error {
	m.events = append(m.events, event)
	return nil
}

// ============================================================================
// HELPERS
// ============================================================================

func validCreateWebhookBody(t *testing.T) *bytes.Reader {
	t.Helper()
	body, err := json.Marshal(CreateWebhookRequest{
		Title:     "Test Webhook",
		TargetURL: "https://example.com/hook",
		Secret:    "secret123",
		Active:    true,
		Events:    []string{"signature.created"},
	})
	require.NoError(t, err)
	return bytes.NewReader(body)
}

func createWebhooksHandler(svc webhookService) *WebhooksHandler {
	return NewWebhooksHandler(svc)
}

// ============================================================================
// TESTS — Webhook Quota Enforcement
// ============================================================================

func TestWebhooksHandler_HandleCreateWebhook_QuotaAllowed(t *testing.T) {
	t.Parallel()

	quota := &mockWebhookQuotaRecorder{}
	tp := &mockWebhookTenantProvider{tenantID: uuid.New()}

	handler := createWebhooksHandler(&mockWebhookService{}).
		WithQuotaRecorder(quota, tp)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/webhooks", validCreateWebhookBody(t))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), shared.ContextKeyUser, &models.User{Email: "admin@test.com"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.HandleCreateWebhook(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.True(t, quota.checkCalled, "quota check should be called")
}

func TestWebhooksHandler_HandleCreateWebhook_QuotaExceeded(t *testing.T) {
	t.Parallel()

	quota := &mockWebhookQuotaRecorder{checkErr: errors.New("quota exceeded")}
	tp := &mockWebhookTenantProvider{tenantID: uuid.New()}

	handler := createWebhooksHandler(&mockWebhookService{}).
		WithQuotaRecorder(quota, tp)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/webhooks", validCreateWebhookBody(t))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), shared.ContextKeyUser, &models.User{Email: "admin@test.com"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.HandleCreateWebhook(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.True(t, quota.checkCalled, "quota check should be called")
}

func TestWebhooksHandler_HandleCreateWebhook_NoQuotaRecorder(t *testing.T) {
	t.Parallel()

	// Without quota recorder, creation should succeed
	handler := createWebhooksHandler(&mockWebhookService{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/webhooks", validCreateWebhookBody(t))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.HandleCreateWebhook(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
}

// ============================================================================
// TESTS — Webhook Audit Logging
// ============================================================================

func TestWebhooksHandler_HandleCreateWebhook_EmitsAuditEvent(t *testing.T) {
	t.Parallel()

	auditLog := &mockWebhookAuditLogger{}
	tp := &mockWebhookTenantProvider{tenantID: uuid.New()}

	handler := createWebhooksHandler(&mockWebhookService{}).
		WithQuotaRecorder(&mockWebhookQuotaRecorder{}, tp).
		WithAuditLogger(auditLog)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/webhooks", validCreateWebhookBody(t))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), shared.ContextKeyUser, &models.User{Email: "admin@test.com"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.HandleCreateWebhook(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Len(t, auditLog.events, 1)
	assert.Equal(t, "webhook.create", auditLog.events[0].Action)
	assert.Equal(t, "webhook", auditLog.events[0].Resource)
	assert.Equal(t, "admin@test.com", auditLog.events[0].UserEmail)
}

func TestWebhooksHandler_HandleDeleteWebhook_EmitsAuditEvent(t *testing.T) {
	t.Parallel()

	auditLog := &mockWebhookAuditLogger{}
	tp := &mockWebhookTenantProvider{tenantID: uuid.New()}

	handler := createWebhooksHandler(&mockWebhookService{}).
		WithQuotaRecorder(&mockWebhookQuotaRecorder{}, tp).
		WithAuditLogger(auditLog)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/webhooks/42", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "42")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	ctx = context.WithValue(ctx, shared.ContextKeyUser, &models.User{Email: "admin@test.com"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.HandleDeleteWebhook(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, auditLog.events, 1)
	assert.Equal(t, "webhook.delete", auditLog.events[0].Action)
	assert.Equal(t, "webhook", auditLog.events[0].Resource)
	assert.Equal(t, "42", auditLog.events[0].ResourceID)
}

func TestWebhooksHandler_HandleCreateWebhook_NoAuditLoggerStillSucceeds(t *testing.T) {
	t.Parallel()

	// Without audit logger, creation should still succeed
	handler := createWebhooksHandler(&mockWebhookService{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/webhooks", validCreateWebhookBody(t))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.HandleCreateWebhook(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
}
