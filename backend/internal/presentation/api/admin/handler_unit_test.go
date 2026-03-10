// SPDX-License-Identifier: AGPL-3.0-or-later
package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

type mockAdminService struct {
	getDocumentFunc                   func(ctx context.Context, docID string) (*models.Document, error)
	listDocumentsFunc                 func(ctx context.Context, limit, offset int) ([]*models.Document, error)
	searchDocumentsFunc               func(ctx context.Context, query string, limit, offset int) ([]*models.Document, error)
	countDocumentsFunc                func(ctx context.Context, searchQuery string) (int, error)
	updateDocumentMetadataFunc        func(ctx context.Context, docID string, input models.DocumentInput, updatedBy string) (*models.Document, error)
	deleteDocumentFunc                func(ctx context.Context, docID string) error
	listExpectedSignersFunc           func(ctx context.Context, docID string) ([]*models.ExpectedSigner, error)
	listExpectedSignersWithStatusFunc func(ctx context.Context, docID string) ([]*models.ExpectedSignerWithStatus, error)
	addExpectedSignersFunc            func(ctx context.Context, docID string, contacts []models.ContactInfo, addedBy string) error
	removeExpectedSignerFunc          func(ctx context.Context, docID, email string) error
	getSignerStatsFunc                func(ctx context.Context, docID string) (*models.DocCompletionStats, error)
}

func (m *mockAdminService) GetDocument(ctx context.Context, docID string) (*models.Document, error) {
	if m.getDocumentFunc != nil {
		return m.getDocumentFunc(ctx, docID)
	}
	return nil, errors.New("not implemented")
}

func (m *mockAdminService) ListDocuments(ctx context.Context, limit, offset int) ([]*models.Document, error) {
	if m.listDocumentsFunc != nil {
		return m.listDocumentsFunc(ctx, limit, offset)
	}
	return nil, errors.New("not implemented")
}

func (m *mockAdminService) SearchDocuments(ctx context.Context, query string, limit, offset int) ([]*models.Document, error) {
	if m.searchDocumentsFunc != nil {
		return m.searchDocumentsFunc(ctx, query, limit, offset)
	}
	return nil, errors.New("not implemented")
}

func (m *mockAdminService) CountDocuments(ctx context.Context, searchQuery string) (int, error) {
	if m.countDocumentsFunc != nil {
		return m.countDocumentsFunc(ctx, searchQuery)
	}
	return 0, errors.New("not implemented")
}

func (m *mockAdminService) UpdateDocumentMetadata(ctx context.Context, docID string, input models.DocumentInput, updatedBy string) (*models.Document, error) {
	if m.updateDocumentMetadataFunc != nil {
		return m.updateDocumentMetadataFunc(ctx, docID, input, updatedBy)
	}
	return nil, errors.New("not implemented")
}

func (m *mockAdminService) DeleteDocument(ctx context.Context, docID string) error {
	if m.deleteDocumentFunc != nil {
		return m.deleteDocumentFunc(ctx, docID)
	}
	return errors.New("not implemented")
}

func (m *mockAdminService) ListExpectedSigners(ctx context.Context, docID string) ([]*models.ExpectedSigner, error) {
	if m.listExpectedSignersFunc != nil {
		return m.listExpectedSignersFunc(ctx, docID)
	}
	return nil, errors.New("not implemented")
}

func (m *mockAdminService) ListExpectedSignersWithStatus(ctx context.Context, docID string) ([]*models.ExpectedSignerWithStatus, error) {
	if m.listExpectedSignersWithStatusFunc != nil {
		return m.listExpectedSignersWithStatusFunc(ctx, docID)
	}
	return nil, errors.New("not implemented")
}

func (m *mockAdminService) AddExpectedSigners(ctx context.Context, docID string, contacts []models.ContactInfo, addedBy string) error {
	if m.addExpectedSignersFunc != nil {
		return m.addExpectedSignersFunc(ctx, docID, contacts, addedBy)
	}
	return errors.New("not implemented")
}

func (m *mockAdminService) RemoveExpectedSigner(ctx context.Context, docID, email string) error {
	if m.removeExpectedSignerFunc != nil {
		return m.removeExpectedSignerFunc(ctx, docID, email)
	}
	return errors.New("not implemented")
}

func (m *mockAdminService) GetSignerStats(ctx context.Context, docID string) (*models.DocCompletionStats, error) {
	if m.getSignerStatsFunc != nil {
		return m.getSignerStatsFunc(ctx, docID)
	}
	return nil, errors.New("not implemented")
}

func (m *mockAdminService) GetAggregateDocumentStats(_ context.Context) (int, int, error) {
	return 0, 0, nil
}

type mockReminderService struct {
	sendRemindersFunc      func(ctx context.Context, docID, sentBy string, specificEmails []string, docURL string, locale string) (*models.ReminderSendResult, error)
	getReminderHistoryFunc func(ctx context.Context, docID string) ([]*models.ReminderLog, error)
	getReminderStatsFunc   func(ctx context.Context, docID string) (*models.ReminderStats, error)
}

func (m *mockReminderService) SendReminders(ctx context.Context, docID, sentBy string, specificEmails []string, docURL string, locale string) (*models.ReminderSendResult, error) {
	if m.sendRemindersFunc != nil {
		return m.sendRemindersFunc(ctx, docID, sentBy, specificEmails, docURL, locale)
	}
	return nil, errors.New("not implemented")
}

func (m *mockReminderService) GetReminderHistory(ctx context.Context, docID string) ([]*models.ReminderLog, error) {
	if m.getReminderHistoryFunc != nil {
		return m.getReminderHistoryFunc(ctx, docID)
	}
	return nil, errors.New("not implemented")
}

func (m *mockReminderService) GetReminderStats(ctx context.Context, docID string) (*models.ReminderStats, error) {
	if m.getReminderStatsFunc != nil {
		return m.getReminderStatsFunc(ctx, docID)
	}
	return nil, errors.New("not implemented")
}

type mockSignatureService struct {
	getDocumentSignaturesFunc func(ctx context.Context, docID string) ([]*models.Signature, error)
}

func (m *mockSignatureService) GetDocumentSignatures(ctx context.Context, docID string) ([]*models.Signature, error) {
	if m.getDocumentSignaturesFunc != nil {
		return m.getDocumentSignaturesFunc(ctx, docID)
	}
	return nil, errors.New("not implemented")
}

// ============================================================================
// HELPERS
// ============================================================================

func createTestHandler(adminSvc adminService, reminderSvc reminderService, sigService signatureService) *Handler {
	return NewHandler(adminSvc, reminderSvc, sigService, "https://test.example.com", 500)
}

func createContextWithUser(email string, isAdmin bool) context.Context {
	user := &models.User{
		Sub:   "test-sub-123",
		Email: email,
		Name:  "Test User",
	}
	return context.WithValue(context.Background(), shared.ContextKeyUser, user)
}

func createTestDocument(docID string) *models.Document {
	now := time.Now()
	return &models.Document{
		DocID:             docID,
		Title:             "Test Document",
		URL:               "https://example.com/doc.pdf",
		Checksum:          "abc123",
		ChecksumAlgorithm: "SHA-256",
		Description:       "Test description",
		ReadMode:          "integrated",
		AllowDownload:     true,
		RequireFullRead:   false,
		VerifyChecksum:    true,
		CreatedAt:         now,
		UpdatedAt:         now,
		CreatedBy:         "admin@example.com",
	}
}

func createTestExpectedSignerWithStatus(docID, email string, hasSigned bool) *models.ExpectedSignerWithStatus {
	now := time.Now()
	status := &models.ExpectedSignerWithStatus{
		ExpectedSigner: models.ExpectedSigner{
			ID:      1,
			DocID:   docID,
			Email:   email,
			Name:    "Test Signer",
			AddedAt: now,
			AddedBy: "admin@example.com",
		},
		HasSigned:             hasSigned,
		ReminderCount:         0,
		DaysSinceAdded:        5,
		DaysSinceLastReminder: nil,
	}
	if hasSigned {
		signedAt := now.Add(-2 * time.Hour)
		status.SignedAt = &signedAt
		userName := "Test Signer"
		status.UserName = &userName
	}
	return status
}

func createTestReminderLog(docID, email string) *models.ReminderLog {
	return &models.ReminderLog{
		ID:             1,
		DocID:          docID,
		RecipientEmail: email,
		SentAt:         time.Now(),
		SentBy:         "admin@example.com",
		TemplateUsed:   "reminder",
		Status:         "sent",
	}
}

// ============================================================================
// TESTS - HandleListDocuments
// ============================================================================

func TestHandleListDocuments_Success(t *testing.T) {
	t.Parallel()

	docs := []*models.Document{
		createTestDocument("doc1"),
		createTestDocument("doc2"),
	}

	adminSvc := &mockAdminService{
		listDocumentsFunc: func(ctx context.Context, limit, offset int) ([]*models.Document, error) {
			assert.Equal(t, 100, limit)
			assert.Equal(t, 0, offset)
			return docs, nil
		},
	}

	handler := createTestHandler(adminSvc, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/documents", nil)
	rec := httptest.NewRecorder()

	handler.HandleListDocuments(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response struct {
		Data []DocumentResponse     `json:"data"`
		Meta map[string]interface{} `json:"meta"`
	}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Len(t, response.Data, 2)
	assert.Equal(t, 2, int(response.Meta["total"].(float64)))
}

func TestHandleListDocuments_EmptyList(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{
		listDocumentsFunc: func(ctx context.Context, limit, offset int) ([]*models.Document, error) {
			return []*models.Document{}, nil
		},
	}

	handler := createTestHandler(adminSvc, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/documents", nil)
	rec := httptest.NewRecorder()

	handler.HandleListDocuments(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response struct {
		Data []DocumentResponse     `json:"data"`
		Meta map[string]interface{} `json:"meta"`
	}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Len(t, response.Data, 0)
}

func TestHandleListDocuments_RepositoryError(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{
		listDocumentsFunc: func(ctx context.Context, limit, offset int) ([]*models.Document, error) {
			return nil, errors.New("database error")
		},
	}

	handler := createTestHandler(adminSvc, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/documents", nil)
	rec := httptest.NewRecorder()

	handler.HandleListDocuments(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// ============================================================================
// TESTS - HandleGetDocument
// ============================================================================

func TestHandleGetDocument_Success(t *testing.T) {
	t.Parallel()

	doc := createTestDocument("doc1")
	adminSvc := &mockAdminService{
		getDocumentFunc: func(ctx context.Context, docID string) (*models.Document, error) {
			assert.Equal(t, "doc1", docID)
			return doc, nil
		},
	}

	handler := createTestHandler(adminSvc, nil, nil)

	router := chi.NewRouter()
	router.Get("/api/v1/admin/documents/{docId}", handler.HandleGetDocument)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/documents/doc1", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response struct {
		Data DocumentResponse `json:"data"`
	}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "doc1", response.Data.DocID)
	assert.Equal(t, "Test Document", response.Data.Title)
}

func TestHandleGetDocument_NotFound(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{
		getDocumentFunc: func(ctx context.Context, docID string) (*models.Document, error) {
			return nil, errors.New("not found")
		},
	}

	handler := createTestHandler(adminSvc, nil, nil)

	router := chi.NewRouter()
	router.Get("/api/v1/admin/documents/{docId}", handler.HandleGetDocument)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/documents/nonexistent", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleGetDocument_EmptyDocID(t *testing.T) {
	t.Parallel()

	handler := createTestHandler(nil, nil, nil)

	// Without chi routing context, docId will be empty
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/documents/", nil)
	rec := httptest.NewRecorder()

	handler.HandleGetDocument(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ============================================================================
// TESTS - HandleGetDocumentWithSigners
// ============================================================================

func TestHandleGetDocumentWithSigners_Success(t *testing.T) {
	t.Parallel()

	doc := createTestDocument("doc1")
	signers := []*models.ExpectedSignerWithStatus{
		createTestExpectedSignerWithStatus("doc1", "signer1@example.com", true),
		createTestExpectedSignerWithStatus("doc1", "signer2@example.com", false),
	}
	stats := &models.DocCompletionStats{
		DocID:          "doc1",
		ExpectedCount:  2,
		SignedCount:    1,
		PendingCount:   1,
		CompletionRate: 50.0,
	}

	adminSvc := &mockAdminService{
		getDocumentFunc: func(ctx context.Context, docID string) (*models.Document, error) {
			return doc, nil
		},
		listExpectedSignersWithStatusFunc: func(ctx context.Context, docID string) ([]*models.ExpectedSignerWithStatus, error) {
			return signers, nil
		},
		getSignerStatsFunc: func(ctx context.Context, docID string) (*models.DocCompletionStats, error) {
			return stats, nil
		},
	}

	handler := createTestHandler(adminSvc, nil, nil)

	router := chi.NewRouter()
	router.Get("/api/v1/admin/documents/{docId}/signers", handler.HandleGetDocumentWithSigners)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/documents/doc1/signers", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response struct {
		Data map[string]interface{} `json:"data"`
	}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.NotNil(t, response.Data["document"])
	assert.NotNil(t, response.Data["signers"])
	assert.NotNil(t, response.Data["stats"])
}

func TestHandleGetDocumentWithSigners_DocumentNotFound(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{
		getDocumentFunc: func(ctx context.Context, docID string) (*models.Document, error) {
			return nil, errors.New("not found")
		},
	}

	handler := createTestHandler(adminSvc, nil, nil)

	router := chi.NewRouter()
	router.Get("/api/v1/admin/documents/{docId}/signers", handler.HandleGetDocumentWithSigners)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/documents/nonexistent/signers", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHandleGetDocumentWithSigners_SignersError(t *testing.T) {
	t.Parallel()

	doc := createTestDocument("doc1")
	adminSvc := &mockAdminService{
		getDocumentFunc: func(ctx context.Context, docID string) (*models.Document, error) {
			return doc, nil
		},
		listExpectedSignersWithStatusFunc: func(ctx context.Context, docID string) ([]*models.ExpectedSignerWithStatus, error) {
			return nil, errors.New("database error")
		},
	}

	handler := createTestHandler(adminSvc, nil, nil)

	router := chi.NewRouter()
	router.Get("/api/v1/admin/documents/{docId}/signers", handler.HandleGetDocumentWithSigners)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/documents/doc1/signers", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// ============================================================================
// TESTS - HandleAddExpectedSigner
// ============================================================================

func TestHandleAddExpectedSigner_Success(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{
		addExpectedSignersFunc: func(ctx context.Context, docID string, contacts []models.ContactInfo, addedBy string) error {
			assert.Equal(t, "doc1", docID)
			assert.Len(t, contacts, 1)
			assert.Equal(t, "new@example.com", contacts[0].Email)
			assert.Equal(t, "admin@example.com", addedBy)
			return nil
		},
	}

	handler := createTestHandler(adminSvc, nil, nil)

	router := chi.NewRouter()
	router.Post("/api/v1/admin/documents/{docId}/signers", handler.HandleAddExpectedSigner)

	reqBody := AddExpectedSignerRequest{
		Email: "new@example.com",
		Name:  "New Signer",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/documents/doc1/signers", bytes.NewReader(body))
	req = req.WithContext(createContextWithUser("admin@example.com", true))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var response struct {
		Data map[string]interface{} `json:"data"`
	}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "new@example.com", response.Data["email"])
}

func TestHandleAddExpectedSigner_MissingEmail(t *testing.T) {
	t.Parallel()

	handler := createTestHandler(nil, nil, nil)

	router := chi.NewRouter()
	router.Post("/api/v1/admin/documents/{docId}/signers", handler.HandleAddExpectedSigner)

	reqBody := AddExpectedSignerRequest{
		Email: "",
		Name:  "New Signer",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/documents/doc1/signers", bytes.NewReader(body))
	req = req.WithContext(createContextWithUser("admin@example.com", true))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleAddExpectedSigner_NoUser(t *testing.T) {
	t.Parallel()

	handler := createTestHandler(nil, nil, nil)

	router := chi.NewRouter()
	router.Post("/api/v1/admin/documents/{docId}/signers", handler.HandleAddExpectedSigner)

	reqBody := AddExpectedSignerRequest{
		Email: "new@example.com",
		Name:  "New Signer",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/documents/doc1/signers", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandleAddExpectedSigner_InvalidJSON(t *testing.T) {
	t.Parallel()

	handler := createTestHandler(nil, nil, nil)

	router := chi.NewRouter()
	router.Post("/api/v1/admin/documents/{docId}/signers", handler.HandleAddExpectedSigner)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/documents/doc1/signers", strings.NewReader("invalid json"))
	req = req.WithContext(createContextWithUser("admin@example.com", true))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ============================================================================
// TESTS - HandleRemoveExpectedSigner
// ============================================================================

func TestHandleRemoveExpectedSigner_Success(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{
		removeExpectedSignerFunc: func(ctx context.Context, docID, email string) error {
			assert.Equal(t, "doc1", docID)
			assert.Equal(t, "remove@example.com", email)
			return nil
		},
	}

	handler := createTestHandler(adminSvc, nil, nil)

	router := chi.NewRouter()
	router.Delete("/api/v1/admin/documents/{docId}/signers/{email}", handler.HandleRemoveExpectedSigner)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/documents/doc1/signers/remove@example.com", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleRemoveExpectedSigner_RepositoryError(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{
		removeExpectedSignerFunc: func(ctx context.Context, docID, email string) error {
			return errors.New("database error")
		},
	}

	handler := createTestHandler(adminSvc, nil, nil)

	router := chi.NewRouter()
	router.Delete("/api/v1/admin/documents/{docId}/signers/{email}", handler.HandleRemoveExpectedSigner)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/documents/doc1/signers/remove@example.com", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestHandleRemoveExpectedSigner_EmptyParams(t *testing.T) {
	t.Parallel()

	handler := createTestHandler(nil, nil, nil)

	// Without chi routing context, params will be empty
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/documents//signers/", nil)
	rec := httptest.NewRecorder()

	handler.HandleRemoveExpectedSigner(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ============================================================================
// TESTS - HandleSendReminders
// ============================================================================

func TestHandleSendReminders_Success(t *testing.T) {
	t.Parallel()

	doc := createTestDocument("doc1")
	adminSvc := &mockAdminService{
		getDocumentFunc: func(ctx context.Context, docID string) (*models.Document, error) {
			return doc, nil
		},
	}

	reminderSvc := &mockReminderService{
		sendRemindersFunc: func(ctx context.Context, docID, sentBy string, specificEmails []string, docURL string, locale string) (*models.ReminderSendResult, error) {
			assert.Equal(t, "doc1", docID)
			assert.Equal(t, "admin@example.com", sentBy)
			assert.Equal(t, "en", locale) // Default locale when no language preference is set
			return &models.ReminderSendResult{
				TotalAttempted:   2,
				SuccessfullySent: 2,
				Failed:           0,
			}, nil
		},
	}

	handler := createTestHandler(adminSvc, reminderSvc, nil)

	router := chi.NewRouter()
	router.Post("/api/v1/admin/documents/{docId}/reminders", handler.HandleSendReminders)

	reqBody := SendRemindersRequest{}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/documents/doc1/reminders", bytes.NewReader(body))
	req = req.WithContext(createContextWithUser("admin@example.com", true))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleSendReminders_ServiceNotAvailable(t *testing.T) {
	t.Parallel()

	handler := createTestHandler(nil, nil, nil)

	router := chi.NewRouter()
	router.Post("/api/v1/admin/documents/{docId}/reminders", handler.HandleSendReminders)

	reqBody := SendRemindersRequest{}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/documents/doc1/reminders", bytes.NewReader(body))
	req = req.WithContext(createContextWithUser("admin@example.com", true))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHandleSendReminders_WithLocale(t *testing.T) {
	t.Parallel()

	doc := createTestDocument("doc1")
	adminSvc := &mockAdminService{
		getDocumentFunc: func(ctx context.Context, docID string) (*models.Document, error) {
			return doc, nil
		},
	}

	reminderSvc := &mockReminderService{
		sendRemindersFunc: func(ctx context.Context, docID, sentBy string, specificEmails []string, docURL string, locale string) (*models.ReminderSendResult, error) {
			assert.Equal(t, "en", locale)
			return &models.ReminderSendResult{
				TotalAttempted:   1,
				SuccessfullySent: 1,
			}, nil
		},
	}

	handler := createTestHandler(adminSvc, reminderSvc, nil)

	router := chi.NewRouter()
	router.Post("/api/v1/admin/documents/{docId}/reminders", handler.HandleSendReminders)

	reqBody := SendRemindersRequest{}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/documents/doc1/reminders", bytes.NewReader(body))
	req.Header.Set("Accept-Language", "en")
	req = req.WithContext(createContextWithUser("admin@example.com", true))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleSendReminders_SpecificEmails(t *testing.T) {
	t.Parallel()

	doc := createTestDocument("doc1")
	adminSvc := &mockAdminService{
		getDocumentFunc: func(ctx context.Context, docID string) (*models.Document, error) {
			return doc, nil
		},
	}

	reminderSvc := &mockReminderService{
		sendRemindersFunc: func(ctx context.Context, docID, sentBy string, specificEmails []string, docURL string, locale string) (*models.ReminderSendResult, error) {
			assert.Len(t, specificEmails, 2)
			assert.Contains(t, specificEmails, "user1@example.com")
			assert.Contains(t, specificEmails, "user2@example.com")
			return &models.ReminderSendResult{
				TotalAttempted:   2,
				SuccessfullySent: 2,
			}, nil
		},
	}

	handler := createTestHandler(adminSvc, reminderSvc, nil)

	router := chi.NewRouter()
	router.Post("/api/v1/admin/documents/{docId}/reminders", handler.HandleSendReminders)

	reqBody := SendRemindersRequest{
		Emails: []string{"user1@example.com", "user2@example.com"},
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/documents/doc1/reminders", bytes.NewReader(body))
	req = req.WithContext(createContextWithUser("admin@example.com", true))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// ============================================================================
// TESTS - HandleGetReminderHistory
// ============================================================================

func TestHandleGetReminderHistory_Success(t *testing.T) {
	t.Parallel()

	logs := []*models.ReminderLog{
		createTestReminderLog("doc1", "user1@example.com"),
		createTestReminderLog("doc1", "user2@example.com"),
	}

	adminSvc := &mockAdminService{}
	reminderSvc := &mockReminderService{
		getReminderHistoryFunc: func(ctx context.Context, docID string) ([]*models.ReminderLog, error) {
			assert.Equal(t, "doc1", docID)
			return logs, nil
		},
	}

	handler := createTestHandler(adminSvc, reminderSvc, nil)

	router := chi.NewRouter()
	router.Get("/api/v1/admin/documents/{docId}/reminders", handler.HandleGetReminderHistory)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/documents/doc1/reminders", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response struct {
		Data []ReminderLogResponse `json:"data"`
	}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Len(t, response.Data, 2)
}

func TestHandleGetReminderHistory_ServiceNotAvailable(t *testing.T) {
	t.Parallel()

	handler := createTestHandler(nil, nil, nil)

	router := chi.NewRouter()
	router.Get("/api/v1/admin/documents/{docId}/reminders", handler.HandleGetReminderHistory)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/documents/doc1/reminders", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHandleGetReminderHistory_EmptyHistory(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{}
	reminderSvc := &mockReminderService{
		getReminderHistoryFunc: func(ctx context.Context, docID string) ([]*models.ReminderLog, error) {
			return []*models.ReminderLog{}, nil
		},
	}

	handler := createTestHandler(adminSvc, reminderSvc, nil)

	router := chi.NewRouter()
	router.Get("/api/v1/admin/documents/{docId}/reminders", handler.HandleGetReminderHistory)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/documents/doc1/reminders", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response struct {
		Data []ReminderLogResponse `json:"data"`
	}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Len(t, response.Data, 0)
}

// ============================================================================
// TESTS - HandleUpdateDocumentMetadata
// ============================================================================

func TestHandleUpdateDocumentMetadata_CreateNew(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{
		getDocumentFunc: func(ctx context.Context, docID string) (*models.Document, error) {
			return nil, errors.New("not found")
		},
		updateDocumentMetadataFunc: func(ctx context.Context, docID string, input models.DocumentInput, createdBy string) (*models.Document, error) {
			assert.Equal(t, "new-doc", docID)
			assert.Equal(t, "New Document", input.Title)
			assert.Equal(t, "admin@example.com", createdBy)
			return createTestDocument(docID), nil
		},
	}

	handler := createTestHandler(adminSvc, nil, nil)

	router := chi.NewRouter()
	router.Put("/api/v1/admin/documents/{docId}/metadata", handler.HandleUpdateDocumentMetadata)

	title := "New Document"
	reqBody := UpdateDocumentMetadataRequest{
		Title: &title,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/documents/new-doc/metadata", bytes.NewReader(body))
	req = req.WithContext(createContextWithUser("admin@example.com", true))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleUpdateDocumentMetadata_UpdateExisting(t *testing.T) {
	t.Parallel()

	doc := createTestDocument("doc1")
	adminSvc := &mockAdminService{
		getDocumentFunc: func(ctx context.Context, docID string) (*models.Document, error) {
			return doc, nil
		},
		updateDocumentMetadataFunc: func(ctx context.Context, docID string, input models.DocumentInput, createdBy string) (*models.Document, error) {
			assert.Equal(t, "Updated Title", input.Title)
			doc.Title = input.Title
			return doc, nil
		},
	}

	handler := createTestHandler(adminSvc, nil, nil)

	router := chi.NewRouter()
	router.Put("/api/v1/admin/documents/{docId}/metadata", handler.HandleUpdateDocumentMetadata)

	title := "Updated Title"
	reqBody := UpdateDocumentMetadataRequest{
		Title: &title,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/documents/doc1/metadata", bytes.NewReader(body))
	req = req.WithContext(createContextWithUser("admin@example.com", true))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleUpdateDocumentMetadata_AllFields(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{
		getDocumentFunc: func(ctx context.Context, docID string) (*models.Document, error) {
			return createTestDocument(docID), nil
		},
		updateDocumentMetadataFunc: func(ctx context.Context, docID string, input models.DocumentInput, createdBy string) (*models.Document, error) {
			assert.Equal(t, "New Title", input.Title)
			assert.Equal(t, "https://new.example.com/doc.pdf", input.URL)
			assert.Equal(t, "xyz789", input.Checksum)
			assert.Equal(t, "SHA-512", input.ChecksumAlgorithm)
			assert.Equal(t, "New description", input.Description)
			return createTestDocument(docID), nil
		},
	}

	handler := createTestHandler(adminSvc, nil, nil)

	router := chi.NewRouter()
	router.Put("/api/v1/admin/documents/{docId}/metadata", handler.HandleUpdateDocumentMetadata)

	title := "New Title"
	url := "https://new.example.com/doc.pdf"
	checksum := "xyz789"
	algorithm := "SHA-512"
	description := "New description"
	reqBody := UpdateDocumentMetadataRequest{
		Title:             &title,
		URL:               &url,
		Checksum:          &checksum,
		ChecksumAlgorithm: &algorithm,
		Description:       &description,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/documents/doc1/metadata", bytes.NewReader(body))
	req = req.WithContext(createContextWithUser("admin@example.com", true))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestHandleUpdateDocumentMetadata_NoUser(t *testing.T) {
	t.Parallel()

	handler := createTestHandler(nil, nil, nil)

	router := chi.NewRouter()
	router.Put("/api/v1/admin/documents/{docId}/metadata", handler.HandleUpdateDocumentMetadata)

	title := "New Title"
	reqBody := UpdateDocumentMetadataRequest{
		Title: &title,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/documents/doc1/metadata", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// ============================================================================
// TESTS - HandleGetDocumentStatus
// ============================================================================

func TestHandleGetDocumentStatus_Complete(t *testing.T) {
	t.Parallel()

	doc := createTestDocument("doc1")
	signers := []*models.ExpectedSignerWithStatus{
		createTestExpectedSignerWithStatus("doc1", "expected@example.com", true),
	}
	stats := &models.DocCompletionStats{
		DocID:          "doc1",
		ExpectedCount:  1,
		SignedCount:    1,
		PendingCount:   0,
		CompletionRate: 100.0,
	}
	signatures := []*models.Signature{
		{
			ID:          1,
			DocID:       "doc1",
			UserSub:     "unexpected-sub",
			UserEmail:   "unexpected@example.com",
			UserName:    "Unexpected User",
			SignedAtUTC: time.Now(),
		},
	}
	lastSent := time.Now()
	reminderStats := &models.ReminderStats{
		TotalSent:    5,
		PendingCount: 0,
		LastSentAt:   &lastSent,
	}

	adminSvc := &mockAdminService{
		getDocumentFunc: func(ctx context.Context, docID string) (*models.Document, error) {
			return doc, nil
		},
		listExpectedSignersWithStatusFunc: func(ctx context.Context, docID string) ([]*models.ExpectedSignerWithStatus, error) {
			return signers, nil
		},
		getSignerStatsFunc: func(ctx context.Context, docID string) (*models.DocCompletionStats, error) {
			return stats, nil
		},
	}
	sigService := &mockSignatureService{
		getDocumentSignaturesFunc: func(ctx context.Context, docID string) ([]*models.Signature, error) {
			return signatures, nil
		},
	}
	reminderSvc := &mockReminderService{
		getReminderStatsFunc: func(ctx context.Context, docID string) (*models.ReminderStats, error) {
			return reminderStats, nil
		},
	}

	handler := createTestHandler(adminSvc, reminderSvc, sigService)

	router := chi.NewRouter()
	router.Get("/api/v1/admin/documents/{docId}/status", handler.HandleGetDocumentStatus)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/documents/doc1/status", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response struct {
		Data DocumentStatusResponse `json:"data"`
	}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "doc1", response.Data.DocID)
	assert.NotNil(t, response.Data.Document)
	assert.Len(t, response.Data.ExpectedSigners, 1)
	assert.Len(t, response.Data.UnexpectedSignatures, 1)
	assert.Equal(t, "unexpected@example.com", response.Data.UnexpectedSignatures[0].UserEmail)
	assert.NotNil(t, response.Data.Stats)
	assert.NotNil(t, response.Data.ReminderStats)
	assert.Contains(t, response.Data.ShareLink, "doc1")
}

func TestHandleGetDocumentStatus_MinimalData(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{
		getDocumentFunc: func(ctx context.Context, docID string) (*models.Document, error) {
			return nil, errors.New("not found")
		},
		listExpectedSignersWithStatusFunc: func(ctx context.Context, docID string) ([]*models.ExpectedSignerWithStatus, error) {
			return []*models.ExpectedSignerWithStatus{}, nil
		},
		getSignerStatsFunc: func(ctx context.Context, docID string) (*models.DocCompletionStats, error) {
			return nil, errors.New("no stats")
		},
	}

	handler := createTestHandler(adminSvc, nil, nil)

	router := chi.NewRouter()
	router.Get("/api/v1/admin/documents/{docId}/status", handler.HandleGetDocumentStatus)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/documents/doc1/status", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response struct {
		Data DocumentStatusResponse `json:"data"`
	}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "doc1", response.Data.DocID)
	assert.Nil(t, response.Data.Document)
	assert.Empty(t, response.Data.ExpectedSigners)
	assert.Empty(t, response.Data.UnexpectedSignatures)
	assert.NotNil(t, response.Data.Stats)
	assert.Equal(t, 0.0, response.Data.Stats.CompletionRate)
}

// ============================================================================
// TESTS - HandleDeleteDocument
// ============================================================================

func TestHandleDeleteDocument_Success(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{
		deleteDocumentFunc: func(ctx context.Context, docID string) error {
			assert.Equal(t, "doc1", docID)
			return nil
		},
	}

	handler := createTestHandler(adminSvc, nil, nil)

	router := chi.NewRouter()
	router.Delete("/api/v1/admin/documents/{docId}", handler.HandleDeleteDocument)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/documents/doc1", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response struct {
		Data map[string]interface{} `json:"data"`
	}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Contains(t, response.Data["message"], "deleted successfully")
}

func TestHandleDeleteDocument_RepositoryError(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{
		deleteDocumentFunc: func(ctx context.Context, docID string) error {
			return errors.New("database error")
		},
	}

	handler := createTestHandler(adminSvc, nil, nil)

	router := chi.NewRouter()
	router.Delete("/api/v1/admin/documents/{docId}", handler.HandleDeleteDocument)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/documents/doc1", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// ============================================================================
// TESTS - Helper Functions
// ============================================================================

func TestToDocumentResponse(t *testing.T) {
	t.Parallel()

	doc := createTestDocument("doc1")
	response := toDocumentResponse(doc)

	assert.Equal(t, "doc1", response.DocID)
	assert.Equal(t, "Test Document", response.Title)
	assert.Equal(t, "https://example.com/doc.pdf", response.URL)
	assert.Equal(t, "abc123", response.Checksum)
	assert.Equal(t, "SHA-256", response.ChecksumAlgorithm)
	assert.Equal(t, "Test description", response.Description)
	assert.NotEmpty(t, response.CreatedAt)
	assert.NotEmpty(t, response.UpdatedAt)
	assert.Equal(t, "admin@example.com", response.CreatedBy)
}

func TestToExpectedSignerResponse_WithSignature(t *testing.T) {
	t.Parallel()

	signer := createTestExpectedSignerWithStatus("doc1", "test@example.com", true)
	response := toExpectedSignerResponse(signer)

	assert.Equal(t, "test@example.com", response.Email)
	assert.True(t, response.HasSigned)
	assert.NotNil(t, response.SignedAt)
	assert.NotNil(t, response.UserName)
}

func TestToExpectedSignerResponse_NoSignature(t *testing.T) {
	t.Parallel()

	signer := createTestExpectedSignerWithStatus("doc1", "test@example.com", false)
	response := toExpectedSignerResponse(signer)

	assert.Equal(t, "test@example.com", response.Email)
	assert.False(t, response.HasSigned)
	assert.Nil(t, response.SignedAt)
}

func TestToStatsResponse(t *testing.T) {
	t.Parallel()

	stats := &models.DocCompletionStats{
		DocID:          "doc1",
		ExpectedCount:  10,
		SignedCount:    7,
		PendingCount:   3,
		CompletionRate: 70.0,
	}

	response := toStatsResponse(stats)

	assert.Equal(t, "doc1", response.DocID)
	assert.Equal(t, 10, response.ExpectedCount)
	assert.Equal(t, 7, response.SignedCount)
	assert.Equal(t, 3, response.PendingCount)
	assert.Equal(t, 70.0, response.CompletionRate)
}

// ============================================================================
// BENCHMARKS
// ============================================================================

func BenchmarkHandleListDocuments(b *testing.B) {
	docs := []*models.Document{
		createTestDocument("doc1"),
		createTestDocument("doc2"),
		createTestDocument("doc3"),
	}

	adminSvc := &mockAdminService{
		listDocumentsFunc: func(ctx context.Context, limit, offset int) ([]*models.Document, error) {
			return docs, nil
		},
	}

	handler := createTestHandler(adminSvc, nil, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/documents", nil)
		rec := httptest.NewRecorder()
		handler.HandleListDocuments(rec, req)
	}
}

func BenchmarkHandleGetDocumentStatus(b *testing.B) {
	doc := createTestDocument("doc1")
	signers := []*models.ExpectedSignerWithStatus{
		createTestExpectedSignerWithStatus("doc1", "signer1@example.com", true),
		createTestExpectedSignerWithStatus("doc1", "signer2@example.com", false),
	}
	stats := &models.DocCompletionStats{
		DocID:          "doc1",
		ExpectedCount:  2,
		SignedCount:    1,
		PendingCount:   1,
		CompletionRate: 50.0,
	}

	adminSvc := &mockAdminService{
		getDocumentFunc: func(ctx context.Context, docID string) (*models.Document, error) {
			return doc, nil
		},
		listExpectedSignersWithStatusFunc: func(ctx context.Context, docID string) ([]*models.ExpectedSignerWithStatus, error) {
			return signers, nil
		},
		getSignerStatsFunc: func(ctx context.Context, docID string) (*models.DocCompletionStats, error) {
			return stats, nil
		},
	}

	handler := createTestHandler(adminSvc, nil, nil)

	router := chi.NewRouter()
	router.Get("/api/v1/admin/documents/{docId}/status", handler.HandleGetDocumentStatus)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/documents/doc1/status", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
	}
}

func BenchmarkToDocumentResponse(b *testing.B) {
	doc := createTestDocument("doc1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = toDocumentResponse(doc)
	}
}

// ============================================================================
// MOCKS - Quota
// ============================================================================

type mockQuotaRecorder struct {
	deleteCalled      bool
	deleteErr         error
	checkReminderErr  error
	recordReminderErr error
	reminderRecorded  int
	checkSignerErr    error
	recordSignerErr   error
	signerRecorded    int
}

func (m *mockQuotaRecorder) RecordDocumentDeletion(_ context.Context, _ string) error {
	m.deleteCalled = true
	return m.deleteErr
}

func (m *mockQuotaRecorder) CheckReminderSend(_ context.Context, _ string) error {
	return m.checkReminderErr
}

func (m *mockQuotaRecorder) RecordReminderSend(_ context.Context, _ string) error {
	m.reminderRecorded++
	return m.recordReminderErr
}

func (m *mockQuotaRecorder) CheckSignerAdd(_ context.Context, _ string) error {
	return m.checkSignerErr
}

func (m *mockQuotaRecorder) RecordSignerAdd(_ context.Context, _ string) error {
	m.signerRecorded++
	return m.recordSignerErr
}

type mockTenantProvider struct {
	tenantID uuid.UUID
}

func (m *mockTenantProvider) CurrentTenant(_ context.Context) (uuid.UUID, error) {
	return m.tenantID, nil
}

// ============================================================================
// TESTS - HandleDeleteDocument with Quota
// ============================================================================

func TestHandleDeleteDocument_QuotaDecrementCalled(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{
		deleteDocumentFunc: func(_ context.Context, docID string) error {
			assert.Equal(t, "doc1", docID)
			return nil
		},
	}

	recorder := &mockQuotaRecorder{}
	tp := &mockTenantProvider{tenantID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")}

	handler := createTestHandler(adminSvc, nil, nil)
	handler.WithQuotaRecorder(recorder, tp)

	router := chi.NewRouter()
	router.Delete("/api/v1/admin/documents/{docId}", handler.HandleDeleteDocument)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/documents/doc1", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, recorder.deleteCalled, "quota deletion should be recorded after successful delete")
}

func TestHandleDeleteDocument_QuotaNotCalledOnError(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{
		deleteDocumentFunc: func(_ context.Context, _ string) error {
			return errors.New("database error")
		},
	}

	recorder := &mockQuotaRecorder{}
	tp := &mockTenantProvider{tenantID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")}

	handler := createTestHandler(adminSvc, nil, nil)
	handler.WithQuotaRecorder(recorder, tp)

	router := chi.NewRouter()
	router.Delete("/api/v1/admin/documents/{docId}", handler.HandleDeleteDocument)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/documents/doc1", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.False(t, recorder.deleteCalled, "quota deletion should NOT be recorded when delete fails")
}

func TestHandleDeleteDocument_NoQuotaRecorder(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{
		deleteDocumentFunc: func(_ context.Context, _ string) error {
			return nil
		},
	}

	handler := createTestHandler(adminSvc, nil, nil)
	// No quotaRecorder set → CE mode

	router := chi.NewRouter()
	router.Delete("/api/v1/admin/documents/{docId}", handler.HandleDeleteDocument)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/documents/doc1", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// ============================================================================
// TESTS - HandleAddExpectedSigner with Quota
// ============================================================================

func TestHandleAddExpectedSigner_QuotaCheckAndRecord(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{
		addExpectedSignersFunc: func(_ context.Context, docID string, contacts []models.ContactInfo, addedBy string) error {
			assert.Equal(t, "doc1", docID)
			assert.Len(t, contacts, 1)
			assert.Equal(t, "new@example.com", contacts[0].Email)
			return nil
		},
	}

	recorder := &mockQuotaRecorder{}
	tp := &mockTenantProvider{tenantID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")}

	handler := createTestHandler(adminSvc, nil, nil)
	handler.WithQuotaRecorder(recorder, tp)

	body, _ := json.Marshal(AddExpectedSignerRequest{Email: "new@example.com", Name: "New User"})
	router := chi.NewRouter()
	router.Post("/api/v1/admin/documents/{docId}/signers", handler.HandleAddExpectedSigner)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/documents/doc1/signers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(createContextWithUser("admin@test.com", true))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, 1, recorder.signerRecorded, "signer quota should be recorded after successful add")
}

func TestHandleAddExpectedSigner_QuotaExceeded(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{}

	recorder := &mockQuotaRecorder{checkSignerErr: errors.New("signer quota exceeded")}
	tp := &mockTenantProvider{tenantID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")}

	handler := createTestHandler(adminSvc, nil, nil)
	handler.WithQuotaRecorder(recorder, tp)

	body, _ := json.Marshal(AddExpectedSignerRequest{Email: "new@example.com", Name: "New User"})
	router := chi.NewRouter()
	router.Post("/api/v1/admin/documents/{docId}/signers", handler.HandleAddExpectedSigner)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/documents/doc1/signers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(createContextWithUser("admin@test.com", true))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, 0, recorder.signerRecorded, "signer quota should NOT be recorded when check fails")
}

func TestHandleAddExpectedSigner_QuotaNotRecordedOnError(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{
		addExpectedSignersFunc: func(_ context.Context, _ string, _ []models.ContactInfo, _ string) error {
			return errors.New("database error")
		},
	}

	recorder := &mockQuotaRecorder{}
	tp := &mockTenantProvider{tenantID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")}

	handler := createTestHandler(adminSvc, nil, nil)
	handler.WithQuotaRecorder(recorder, tp)

	body, _ := json.Marshal(AddExpectedSignerRequest{Email: "new@example.com", Name: "New User"})
	router := chi.NewRouter()
	router.Post("/api/v1/admin/documents/{docId}/signers", handler.HandleAddExpectedSigner)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/documents/doc1/signers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(createContextWithUser("admin@test.com", true))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, 0, recorder.signerRecorded, "signer quota should NOT be recorded when add fails")
}

// ============================================================================
// TESTS - HandleImportSigners with Quota
// ============================================================================

func TestHandleImportSigners_QuotaCheckAndRecord(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{
		listExpectedSignersFunc: func(_ context.Context, _ string) ([]*models.ExpectedSigner, error) {
			return []*models.ExpectedSigner{}, nil // no existing signers
		},
		addExpectedSignersFunc: func(_ context.Context, _ string, contacts []models.ContactInfo, _ string) error {
			return nil
		},
	}

	recorder := &mockQuotaRecorder{}
	tp := &mockTenantProvider{tenantID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")}

	handler := createTestHandler(adminSvc, nil, nil)
	handler.WithQuotaRecorder(recorder, tp)

	reqBody := ImportSignersRequest{
		Signers: []ImportSignerEntry{
			{Email: "a@test.com", Name: "A"},
			{Email: "b@test.com", Name: "B"},
			{Email: "c@test.com", Name: "C"},
		},
	}
	body, _ := json.Marshal(reqBody)

	router := chi.NewRouter()
	router.Post("/api/v1/admin/documents/{docId}/signers/import", handler.HandleImportSigners)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/documents/doc1/signers/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(createContextWithUser("admin@test.com", true))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 3, recorder.signerRecorded, "signer quota should be recorded for each imported signer")
}

func TestHandleImportSigners_QuotaExceeded(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{
		listExpectedSignersFunc: func(_ context.Context, _ string) ([]*models.ExpectedSigner, error) {
			return []*models.ExpectedSigner{}, nil
		},
	}

	recorder := &mockQuotaRecorder{checkSignerErr: errors.New("signer quota exceeded")}
	tp := &mockTenantProvider{tenantID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")}

	handler := createTestHandler(adminSvc, nil, nil)
	handler.WithQuotaRecorder(recorder, tp)

	reqBody := ImportSignersRequest{
		Signers: []ImportSignerEntry{
			{Email: "a@test.com", Name: "A"},
		},
	}
	body, _ := json.Marshal(reqBody)

	router := chi.NewRouter()
	router.Post("/api/v1/admin/documents/{docId}/signers/import", handler.HandleImportSigners)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/documents/doc1/signers/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(createContextWithUser("admin@test.com", true))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, 0, recorder.signerRecorded, "signer quota should NOT be recorded when check fails")
}

func TestHandleImportSigners_SkippedSignersNotCounted(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{
		listExpectedSignersFunc: func(_ context.Context, _ string) ([]*models.ExpectedSigner, error) {
			return []*models.ExpectedSigner{
				{Email: "existing@test.com"},
			}, nil
		},
		addExpectedSignersFunc: func(_ context.Context, _ string, _ []models.ContactInfo, _ string) error {
			return nil
		},
	}

	recorder := &mockQuotaRecorder{}
	tp := &mockTenantProvider{tenantID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")}

	handler := createTestHandler(adminSvc, nil, nil)
	handler.WithQuotaRecorder(recorder, tp)

	reqBody := ImportSignersRequest{
		Signers: []ImportSignerEntry{
			{Email: "existing@test.com", Name: "Existing"},
			{Email: "new@test.com", Name: "New"},
		},
	}
	body, _ := json.Marshal(reqBody)

	router := chi.NewRouter()
	router.Post("/api/v1/admin/documents/{docId}/signers/import", handler.HandleImportSigners)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/documents/doc1/signers/import", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(createContextWithUser("admin@test.com", true))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 1, recorder.signerRecorded, "only new signers should be quota-recorded, not skipped ones")
}

// ============================================================================
// TESTS - HandleSendReminders with Quota
// ============================================================================

func TestHandleSendReminders_QuotaCheckAndRecord(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{
		getDocumentFunc: func(_ context.Context, docID string) (*models.Document, error) {
			return &models.Document{DocID: docID, Title: "Test Doc", URL: "https://test.com/doc"}, nil
		},
	}

	reminderSvc := &mockReminderService{
		sendRemindersFunc: func(_ context.Context, _ string, _ string, _ []string, _ string, _ string) (*models.ReminderSendResult, error) {
			return &models.ReminderSendResult{
				TotalAttempted:   2,
				SuccessfullySent: 2,
				Failed:           0,
			}, nil
		},
	}

	recorder := &mockQuotaRecorder{}
	tp := &mockTenantProvider{tenantID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")}

	handler := createTestHandler(adminSvc, reminderSvc, nil)
	handler.WithQuotaRecorder(recorder, tp)

	router := chi.NewRouter()
	router.Post("/api/v1/admin/documents/{docId}/reminders", handler.HandleSendReminders)

	body, _ := json.Marshal(map[string]interface{}{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/documents/doc1/reminders", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(createContextWithUser("admin@test.com", true))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 2, recorder.reminderRecorded, "reminder quota should be recorded for each attempted reminder")
}

func TestHandleSendReminders_QuotaExceeded(t *testing.T) {
	t.Parallel()

	adminSvc := &mockAdminService{
		getDocumentFunc: func(_ context.Context, docID string) (*models.Document, error) {
			return &models.Document{DocID: docID, Title: "Test Doc", URL: "https://test.com/doc"}, nil
		},
	}

	reminderSvc := &mockReminderService{}

	recorder := &mockQuotaRecorder{checkReminderErr: errors.New("reminder quota exceeded")}
	tp := &mockTenantProvider{tenantID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")}

	handler := createTestHandler(adminSvc, reminderSvc, nil)
	handler.WithQuotaRecorder(recorder, tp)

	router := chi.NewRouter()
	router.Post("/api/v1/admin/documents/{docId}/reminders", handler.HandleSendReminders)

	body, _ := json.Marshal(map[string]interface{}{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/documents/doc1/reminders", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(createContextWithUser("admin@test.com", true))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, 0, recorder.reminderRecorded, "reminder quota should NOT be recorded when check fails")
}

// ============================================================================
// TESTS — Audit Logging
// ============================================================================

type mockAuditLogger struct {
	events []shared.AuditEvent
}

func (m *mockAuditLogger) Log(_ context.Context, event shared.AuditEvent) error {
	m.events = append(m.events, event)
	return nil
}

func TestHandler_HandleDeleteDocument_EmitsAuditEvent(t *testing.T) {
	t.Parallel()

	auditLog := &mockAuditLogger{}
	tp := &mockTenantProvider{tenantID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")}
	svc := &mockAdminService{
		deleteDocumentFunc: func(_ context.Context, _ string) error { return nil },
		getDocumentFunc: func(_ context.Context, _ string) (*models.Document, error) {
			return createTestDocument("doc-123"), nil
		},
	}

	handler := createTestHandler(svc, &mockReminderService{}, &mockSignatureService{}).
		WithQuotaRecorder(&mockQuotaRecorder{}, tp).
		WithAuditLogger(auditLog)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/documents/doc-123", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("docId", "doc-123")
	ctx := createContextWithUser("admin@example.com", true)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.HandleDeleteDocument(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, auditLog.events, 1)
	assert.Equal(t, "document.delete", auditLog.events[0].Action)
	assert.Equal(t, "document", auditLog.events[0].Resource)
	assert.Equal(t, "doc-123", auditLog.events[0].ResourceID)
}

func TestHandler_HandleAddExpectedSigner_EmitsAuditEvent(t *testing.T) {
	t.Parallel()

	auditLog := &mockAuditLogger{}
	tp := &mockTenantProvider{tenantID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")}
	svc := &mockAdminService{
		addExpectedSignersFunc: func(_ context.Context, _ string, _ []models.ContactInfo, _ string) error {
			return nil
		},
	}

	handler := createTestHandler(svc, &mockReminderService{}, &mockSignatureService{}).
		WithQuotaRecorder(&mockQuotaRecorder{}, tp).
		WithAuditLogger(auditLog)

	body, _ := json.Marshal(map[string]string{"email": "signer@test.com", "name": "Test Signer"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/documents/doc-123/signers", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("docId", "doc-123")
	ctx := createContextWithUser("admin@example.com", true)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.HandleAddExpectedSigner(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Len(t, auditLog.events, 1)
	assert.Equal(t, "signer.add", auditLog.events[0].Action)
	assert.Equal(t, "document", auditLog.events[0].Resource)
	assert.Equal(t, "doc-123", auditLog.events[0].ResourceID)
	assert.Equal(t, "signer@test.com", auditLog.events[0].Details["signer_email"])
}

func TestHandler_HandleRemoveExpectedSigner_EmitsAuditEvent(t *testing.T) {
	t.Parallel()

	auditLog := &mockAuditLogger{}
	tp := &mockTenantProvider{tenantID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")}
	svc := &mockAdminService{
		removeExpectedSignerFunc: func(_ context.Context, _, _ string) error { return nil },
	}

	handler := createTestHandler(svc, &mockReminderService{}, &mockSignatureService{}).
		WithQuotaRecorder(&mockQuotaRecorder{}, tp).
		WithAuditLogger(auditLog)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/documents/doc-123/signers/signer@test.com", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("docId", "doc-123")
	rctx.URLParams.Add("email", "signer@test.com")
	ctx := createContextWithUser("admin@example.com", true)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.HandleRemoveExpectedSigner(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, auditLog.events, 1)
	assert.Equal(t, "signer.remove", auditLog.events[0].Action)
	assert.Equal(t, "signer@test.com", auditLog.events[0].Details["signer_email"])
}

func TestHandler_HandleSendReminders_EmitsAuditEvent(t *testing.T) {
	t.Parallel()

	auditLog := &mockAuditLogger{}
	tp := &mockTenantProvider{tenantID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")}
	reminderSvc := &mockReminderService{
		sendRemindersFunc: func(_ context.Context, _, _ string, _ []string, _ string, _ string) (*models.ReminderSendResult, error) {
			return &models.ReminderSendResult{TotalAttempted: 3, SuccessfullySent: 3}, nil
		},
	}

	handler := createTestHandler(&mockAdminService{}, reminderSvc, &mockSignatureService{}).
		WithQuotaRecorder(&mockQuotaRecorder{}, tp).
		WithAuditLogger(auditLog)

	body, _ := json.Marshal(map[string]any{"locale": "fr"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/documents/doc-123/reminders", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("docId", "doc-123")
	ctx := createContextWithUser("admin@example.com", true)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.HandleSendReminders(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, auditLog.events, 1)
	assert.Equal(t, "reminder.send", auditLog.events[0].Action)
	assert.Equal(t, "document", auditLog.events[0].Resource)
	assert.Equal(t, "doc-123", auditLog.events[0].ResourceID)
}

func BenchmarkToExpectedSignerResponse(b *testing.B) {
	signer := createTestExpectedSignerWithStatus("doc1", "test@example.com", true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = toExpectedSignerResponse(signer)
	}
}
