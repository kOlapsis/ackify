// SPDX-License-Identifier: AGPL-3.0-or-later
package signatures

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kolapsis/ackify/backend/internal/presentation/api/shared"
	"github.com/kolapsis/ackify/backend/pkg/models"
)

// ============================================================================
// TEST FIXTURES & MOCKS
// ============================================================================

var (
	testUser = &models.User{
		Sub:   "oauth2|123",
		Email: "user@example.com",
		Name:  "Test User",
	}

	testDoc = &models.Document{
		DocID: "test-doc-123",
		Title: "Test Document",
		URL:   "https://example.com/doc.pdf",
	}

	testSignature = &models.Signature{
		ID:          1,
		DocID:       "test-doc-123",
		UserSub:     "oauth2|123",
		UserEmail:   "user@example.com",
		UserName:    "Test User",
		SignedAtUTC: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		DocChecksum: "checksum-123",
		PayloadHash: "hash-123",
		Signature:   "sig-123",
		Nonce:       "nonce-123",
		CreatedAt:   time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		Referer:     stringPtr("https://github.com/owner/repo"),
		PrevHash:    stringPtr("prev-hash-123"),
		HashVersion: 2,
		DocTitle:    "Test Document",
		DocURL:      "https://example.com/doc.pdf",
	}

	testSignatureStatus = &models.SignatureStatus{
		DocID:     "test-doc-123",
		UserEmail: "user@example.com",
		IsSigned:  true,
		SignedAt:  timePtr(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)),
	}
)

func stringPtr(s string) *string {
	return &s
}

func timePtr(t time.Time) *time.Time {
	return &t
}

// Mock signature service
type mockSignatureService struct {
	createSignatureFunc          func(ctx context.Context, request *models.SignatureRequest) error
	getSignatureStatusFunc       func(ctx context.Context, docID string, user *models.User) (*models.SignatureStatus, error)
	getSignatureByDocAndUserFunc func(ctx context.Context, docID string, user *models.User) (*models.Signature, error)
	getDocumentSignaturesFunc    func(ctx context.Context, docID string) ([]*models.Signature, error)
	getUserSignaturesFunc        func(ctx context.Context, user *models.User) ([]*models.Signature, error)
}

func (m *mockSignatureService) CreateSignature(ctx context.Context, request *models.SignatureRequest) error {
	if m.createSignatureFunc != nil {
		return m.createSignatureFunc(ctx, request)
	}
	return nil
}

func (m *mockSignatureService) GetSignatureStatus(ctx context.Context, docID string, user *models.User) (*models.SignatureStatus, error) {
	if m.getSignatureStatusFunc != nil {
		return m.getSignatureStatusFunc(ctx, docID, user)
	}
	return testSignatureStatus, nil
}

func (m *mockSignatureService) GetSignatureByDocAndUser(ctx context.Context, docID string, user *models.User) (*models.Signature, error) {
	if m.getSignatureByDocAndUserFunc != nil {
		return m.getSignatureByDocAndUserFunc(ctx, docID, user)
	}
	return testSignature, nil
}

func (m *mockSignatureService) GetDocumentSignatures(ctx context.Context, docID string) ([]*models.Signature, error) {
	if m.getDocumentSignaturesFunc != nil {
		return m.getDocumentSignaturesFunc(ctx, docID)
	}
	return []*models.Signature{testSignature}, nil
}

func (m *mockSignatureService) GetUserSignatures(ctx context.Context, user *models.User) ([]*models.Signature, error) {
	if m.getUserSignaturesFunc != nil {
		return m.getUserSignaturesFunc(ctx, user)
	}
	return []*models.Signature{testSignature}, nil
}

func createTestHandler() *Handler {
	return &Handler{
		signatureService: &mockSignatureService{},
	}
}

func addUserToContext(ctx context.Context, user *models.User) context.Context {
	return context.WithValue(ctx, shared.ContextKeyUser, user)
}

// ============================================================================
// TESTS - Constructor
// ============================================================================

func TestNewHandler(t *testing.T) {
	t.Parallel()

	sigService := &mockSignatureService{}

	handler := NewHandler(sigService, nil, nil)

	assert.NotNil(t, handler)
	assert.Equal(t, sigService, handler.signatureService)
}

// ============================================================================
// TESTS - HandleCreateSignature
// ============================================================================

func TestHandler_HandleCreateSignature_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		docID    string
		referer  *string
		checkReq func(t *testing.T, req *models.SignatureRequest)
	}{
		{
			name:    "with referer",
			docID:   "test-doc-123",
			referer: stringPtr("https://github.com/owner/repo"),
			checkReq: func(t *testing.T, req *models.SignatureRequest) {
				assert.Equal(t, "test-doc-123", req.DocID)
				assert.NotNil(t, req.Referer)
				assert.Equal(t, "https://github.com/owner/repo", *req.Referer)
				assert.Equal(t, testUser.Email, req.User.Email)
			},
		},
		{
			name:    "without referer",
			docID:   "test-doc-456",
			referer: nil,
			checkReq: func(t *testing.T, req *models.SignatureRequest) {
				assert.Equal(t, "test-doc-456", req.DocID)
				assert.Nil(t, req.Referer)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockSigService := &mockSignatureService{
				createSignatureFunc: func(ctx context.Context, request *models.SignatureRequest) error {
					tt.checkReq(t, request)
					return nil
				},
			}

			handler := &Handler{
				signatureService: mockSigService,
			}

			reqBody := CreateSignatureRequest{
				DocID:   tt.docID,
				Referer: tt.referer,
			}
			body, err := json.Marshal(reqBody)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/signatures", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			ctx := addUserToContext(req.Context(), testUser)
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()

			handler.HandleCreateSignature(rec, req)

			assert.Equal(t, http.StatusCreated, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

			var wrapper struct {
				Data SignatureResponse `json:"data"`
			}
			err = json.Unmarshal(rec.Body.Bytes(), &wrapper)
			require.NoError(t, err)

			assert.Equal(t, testSignature.ID, wrapper.Data.ID)
			assert.Equal(t, testSignature.DocID, wrapper.Data.DocID)
			assert.Equal(t, testSignature.UserEmail, wrapper.Data.UserEmail)
		})
	}
}

func TestHandler_HandleCreateSignature_Unauthorized(t *testing.T) {
	t.Parallel()

	handler := createTestHandler()

	reqBody := CreateSignatureRequest{
		DocID: "test-doc-123",
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/signatures", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No user in context
	rec := httptest.NewRecorder()

	handler.HandleCreateSignature(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandler_HandleCreateSignature_ValidationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		requestBody    interface{}
		expectedStatus int
	}{
		{
			name:           "empty docID",
			requestBody:    CreateSignatureRequest{DocID: ""},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid JSON",
			requestBody:    "invalid json",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := createTestHandler()

			var body []byte
			var err error
			if str, ok := tt.requestBody.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/signatures", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			ctx := addUserToContext(req.Context(), testUser)
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()

			handler.HandleCreateSignature(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)
		})
	}
}

func TestHandler_HandleCreateSignature_ServiceErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		serviceError   error
		expectedStatus int
		expectedMsg    string
	}{
		{
			name:           "signature already exists",
			serviceError:   models.ErrSignatureAlreadyExists,
			expectedStatus: http.StatusConflict,
			expectedMsg:    "You have already signed this document",
		},
		{
			name:           "invalid document",
			serviceError:   models.ErrInvalidDocument,
			expectedStatus: http.StatusBadRequest,
			expectedMsg:    "Invalid document",
		},
		{
			name:           "document modified",
			serviceError:   models.ErrDocumentModified,
			expectedStatus: http.StatusConflict,
			expectedMsg:    "The document has been modified since it was created",
		},
		{
			name:           "generic error",
			serviceError:   fmt.Errorf("database error"),
			expectedStatus: http.StatusInternalServerError,
			expectedMsg:    "Failed to create signature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockSigService := &mockSignatureService{
				createSignatureFunc: func(ctx context.Context, request *models.SignatureRequest) error {
					return tt.serviceError
				},
			}

			handler := &Handler{
				signatureService: mockSigService,
			}

			reqBody := CreateSignatureRequest{
				DocID: "test-doc-123",
			}
			body, err := json.Marshal(reqBody)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/signatures", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			ctx := addUserToContext(req.Context(), testUser)
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()

			handler.HandleCreateSignature(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)

			var response map[string]interface{}
			err = json.Unmarshal(rec.Body.Bytes(), &response)
			require.NoError(t, err)

			assert.Contains(t, response, "error")
		})
	}
}

// ============================================================================
// TESTS - HandleGetUserSignatures
// ============================================================================

func TestHandler_HandleGetUserSignatures_Success(t *testing.T) {
	t.Parallel()

	mockSigService := &mockSignatureService{
		getUserSignaturesFunc: func(ctx context.Context, user *models.User) ([]*models.Signature, error) {
			assert.Equal(t, testUser.Email, user.Email)
			return []*models.Signature{testSignature}, nil
		},
	}

	handler := &Handler{
		signatureService: mockSigService,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/signatures", nil)
	ctx := addUserToContext(req.Context(), testUser)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.HandleGetUserSignatures(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var wrapper struct {
		Data []*SignatureResponse `json:"data"`
	}
	err := json.Unmarshal(rec.Body.Bytes(), &wrapper)
	require.NoError(t, err)

	assert.Len(t, wrapper.Data, 1)
	assert.Equal(t, testSignature.ID, wrapper.Data[0].ID)
	assert.Equal(t, testSignature.DocID, wrapper.Data[0].DocID)
}

func TestHandler_HandleGetUserSignatures_Unauthorized(t *testing.T) {
	t.Parallel()

	handler := createTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/signatures", nil)
	// No user in context
	rec := httptest.NewRecorder()

	handler.HandleGetUserSignatures(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandler_HandleGetUserSignatures_ServiceError(t *testing.T) {
	t.Parallel()

	mockSigService := &mockSignatureService{
		getUserSignaturesFunc: func(ctx context.Context, user *models.User) ([]*models.Signature, error) {
			return nil, fmt.Errorf("database error")
		},
	}

	handler := &Handler{
		signatureService: mockSigService,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/signatures", nil)
	ctx := addUserToContext(req.Context(), testUser)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.HandleGetUserSignatures(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// ============================================================================
// TESTS - HandleGetDocumentSignatures
// ============================================================================

func TestHandler_HandleGetDocumentSignatures_Success(t *testing.T) {
	t.Parallel()

	mockSigService := &mockSignatureService{
		getDocumentSignaturesFunc: func(ctx context.Context, docID string) ([]*models.Signature, error) {
			assert.Equal(t, "test-doc-123", docID)
			return []*models.Signature{testSignature}, nil
		},
	}

	handler := &Handler{
		signatureService: mockSigService,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/documents/test-doc-123/signatures", nil)

	// Add chi context with URL param
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("docId", "test-doc-123")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()

	handler.HandleGetDocumentSignatures(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var wrapper struct {
		Data []*SignatureResponse `json:"data"`
	}
	err := json.Unmarshal(rec.Body.Bytes(), &wrapper)
	require.NoError(t, err)

	assert.Len(t, wrapper.Data, 1)
	assert.Equal(t, testSignature.DocID, wrapper.Data[0].DocID)
}

func TestHandler_HandleGetDocumentSignatures_MissingDocID(t *testing.T) {
	t.Parallel()

	handler := createTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/documents//signatures", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("docId", "")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()

	handler.HandleGetDocumentSignatures(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandler_HandleGetDocumentSignatures_ServiceError(t *testing.T) {
	t.Parallel()

	mockSigService := &mockSignatureService{
		getDocumentSignaturesFunc: func(ctx context.Context, docID string) ([]*models.Signature, error) {
			return nil, fmt.Errorf("database error")
		},
	}

	handler := &Handler{
		signatureService: mockSigService,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/documents/test-doc-123/signatures", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("docId", "test-doc-123")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()

	handler.HandleGetDocumentSignatures(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// ============================================================================
// TESTS - HandleGetSignatureStatus
// ============================================================================

func TestHandler_HandleGetSignatureStatus_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		status       *models.SignatureStatus
		expectSigned bool
	}{
		{
			name: "signed document",
			status: &models.SignatureStatus{
				DocID:     "test-doc-123",
				UserEmail: "user@example.com",
				IsSigned:  true,
				SignedAt:  timePtr(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)),
			},
			expectSigned: true,
		},
		{
			name: "unsigned document",
			status: &models.SignatureStatus{
				DocID:     "test-doc-456",
				UserEmail: "user@example.com",
				IsSigned:  false,
				SignedAt:  nil,
			},
			expectSigned: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockSigService := &mockSignatureService{
				getSignatureStatusFunc: func(ctx context.Context, docID string, user *models.User) (*models.SignatureStatus, error) {
					return tt.status, nil
				},
			}

			handler := &Handler{
				signatureService: mockSigService,
			}

			req := httptest.NewRequest(http.MethodGet, "/api/v1/documents/"+tt.status.DocID+"/signatures/status", nil)

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("docId", tt.status.DocID)
			ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
			ctx = addUserToContext(ctx, testUser)
			req = req.WithContext(ctx)

			rec := httptest.NewRecorder()

			handler.HandleGetSignatureStatus(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)

			var wrapper struct {
				Data SignatureStatusResponse `json:"data"`
			}
			err := json.Unmarshal(rec.Body.Bytes(), &wrapper)
			require.NoError(t, err)

			assert.Equal(t, tt.status.DocID, wrapper.Data.DocID)
			assert.Equal(t, tt.status.UserEmail, wrapper.Data.UserEmail)
			assert.Equal(t, tt.expectSigned, wrapper.Data.IsSigned)

			if tt.expectSigned {
				assert.NotNil(t, wrapper.Data.SignedAt)
			} else {
				assert.Nil(t, wrapper.Data.SignedAt)
			}
		})
	}
}

func TestHandler_HandleGetSignatureStatus_Unauthorized(t *testing.T) {
	t.Parallel()

	handler := createTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/documents/test-doc-123/signatures/status", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("docId", "test-doc-123")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	// No user in context

	rec := httptest.NewRecorder()

	handler.HandleGetSignatureStatus(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestHandler_HandleGetSignatureStatus_MissingDocID(t *testing.T) {
	t.Parallel()

	handler := createTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/documents//signatures/status", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("docId", "")
	ctx := addUserToContext(req.Context(), testUser)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()

	handler.HandleGetSignatureStatus(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ============================================================================
// TESTS - toSignatureResponse
// ============================================================================

func Test_toSignatureResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sig      *models.Signature
		checkDTO func(t *testing.T, resp *SignatureResponse)
	}{
		{
			name: "with all fields",
			sig:  testSignature,
			checkDTO: func(t *testing.T, resp *SignatureResponse) {
				assert.Equal(t, testSignature.ID, resp.ID)
				assert.Equal(t, testSignature.DocID, resp.DocID)
				assert.Equal(t, testSignature.UserEmail, resp.UserEmail)
				assert.NotNil(t, resp.Referer)
				assert.NotNil(t, resp.PrevHash)
				assert.NotNil(t, resp.DocTitle)
				assert.NotNil(t, resp.DocUrl)
				// Service info may be populated depending on referer URL detection
				// We just verify the field structure exists
			},
		},
		{
			name: "without referer",
			sig: &models.Signature{
				ID:          2,
				DocID:       "doc-456",
				UserSub:     "oauth2|456",
				UserEmail:   "user2@example.com",
				UserName:    "User 2",
				SignedAtUTC: time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC),
				PayloadHash: "hash-456",
				Signature:   "sig-456",
				Nonce:       "nonce-456",
				CreatedAt:   time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC),
				Referer:     nil,
				PrevHash:    nil,
			},
			checkDTO: func(t *testing.T, resp *SignatureResponse) {
				assert.Equal(t, int64(2), resp.ID)
				assert.Nil(t, resp.Referer)
				assert.Nil(t, resp.PrevHash)
				assert.Nil(t, resp.ServiceInfo)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := createTestHandler()

			resp := handler.toSignatureResponse(context.Background(), tt.sig)
			tt.checkDTO(t, resp)
		})
	}
}

func Test_toSignatureResponse_ServiceInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		referer string
	}{
		{
			name:    "GitHub URL",
			referer: "https://github.com/owner/repo",
		},
		{
			name:    "GitLab URL",
			referer: "https://gitlab.com/owner/repo",
		},
		{
			name:    "Generic URL",
			referer: "https://example.com/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sig := &models.Signature{
				ID:          1,
				DocID:       "test-doc",
				UserSub:     "oauth2|123",
				UserEmail:   "user@example.com",
				SignedAtUTC: time.Now(),
				PayloadHash: "hash",
				Signature:   "sig",
				Nonce:       "nonce",
				CreatedAt:   time.Now(),
				Referer:     &tt.referer,
			}

			handler := createTestHandler()
			resp := handler.toSignatureResponse(context.Background(), sig)

			// Just verify the response is created correctly
			// Service info detection is tested in the services package
			assert.Equal(t, sig.ID, resp.ID)
			assert.Equal(t, sig.UserEmail, resp.UserEmail)
			assert.NotNil(t, resp.Referer)
		})
	}
}

// ============================================================================
// TESTS - Concurrency
// ============================================================================

func TestHandler_HandleCreateSignature_Concurrent(t *testing.T) {
	t.Parallel()

	handler := createTestHandler()

	const numRequests = 50
	done := make(chan bool, numRequests)
	errors := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(id int) {
			defer func() { done <- true }()

			reqBody := CreateSignatureRequest{
				DocID: fmt.Sprintf("doc-%d", id),
			}
			body, err := json.Marshal(reqBody)
			if err != nil {
				errors <- err
				return
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/signatures", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			ctx := addUserToContext(req.Context(), testUser)
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()

			handler.HandleCreateSignature(rec, req)

			if rec.Code != http.StatusCreated {
				errors <- fmt.Errorf("unexpected status: %d", rec.Code)
			}
		}(i)
	}

	for i := 0; i < numRequests; i++ {
		<-done
	}
	close(errors)

	var errCount int
	for err := range errors {
		t.Logf("Concurrent request error: %v", err)
		errCount++
	}

	assert.Equal(t, 0, errCount, "All concurrent requests should succeed")
}

// ============================================================================
// BENCHMARKS
// ============================================================================

func BenchmarkHandler_HandleCreateSignature(b *testing.B) {
	handler := createTestHandler()

	reqBody := CreateSignatureRequest{
		DocID: "test-doc-123",
	}
	body, _ := json.Marshal(reqBody)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/signatures", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		ctx := addUserToContext(req.Context(), testUser)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		handler.HandleCreateSignature(rec, req)
	}
}

func BenchmarkHandler_HandleCreateSignature_Parallel(b *testing.B) {
	handler := createTestHandler()

	reqBody := CreateSignatureRequest{
		DocID: "test-doc-123",
	}
	body, _ := json.Marshal(reqBody)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/signatures", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			ctx := addUserToContext(req.Context(), testUser)
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()

			handler.HandleCreateSignature(rec, req)
		}
	})
}

func BenchmarkHandler_HandleGetUserSignatures(b *testing.B) {
	handler := createTestHandler()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/signatures", nil)
		ctx := addUserToContext(req.Context(), testUser)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		handler.HandleGetUserSignatures(rec, req)
	}
}

func BenchmarkHandler_HandleGetUserSignatures_Parallel(b *testing.B) {
	handler := createTestHandler()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/signatures", nil)
			ctx := addUserToContext(req.Context(), testUser)
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()

			handler.HandleGetUserSignatures(rec, req)
		}
	})
}

// ============================================================================
// MOCKS - Quota
// ============================================================================

type mockQuotaRecorder struct {
	checkCalled  bool
	recordCalled bool
	checkErr     error
	recordErr    error
}

func (m *mockQuotaRecorder) CheckSignatureCreation(_ context.Context, _ string) error {
	m.checkCalled = true
	return m.checkErr
}

func (m *mockQuotaRecorder) RecordSignatureCreation(_ context.Context, _ string) error {
	m.recordCalled = true
	return m.recordErr
}

type mockTenantProvider struct {
	tenantID uuid.UUID
}

func (m *mockTenantProvider) CurrentTenant(_ context.Context) (uuid.UUID, error) {
	return m.tenantID, nil
}

// ============================================================================
// TESTS - Quota Integration
// ============================================================================

func TestHandler_HandleCreateSignature_QuotaCheckAndRecord(t *testing.T) {
	t.Parallel()

	recorder := &mockQuotaRecorder{}
	tp := &mockTenantProvider{tenantID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")}

	handler := &Handler{
		signatureService: &mockSignatureService{},
		quotaRecorder:    recorder,
		tenantProvider:   tp,
	}

	body, _ := json.Marshal(CreateSignatureRequest{DocID: "test-doc-123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/signatures", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(addUserToContext(req.Context(), testUser))
	rec := httptest.NewRecorder()

	handler.HandleCreateSignature(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.True(t, recorder.checkCalled, "quota check should be called before creation")
	assert.True(t, recorder.recordCalled, "quota record should be called after creation")
}

func TestHandler_HandleCreateSignature_QuotaExceeded(t *testing.T) {
	t.Parallel()

	recorder := &mockQuotaRecorder{checkErr: fmt.Errorf("signature quota exceeded")}
	tp := &mockTenantProvider{tenantID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")}

	handler := &Handler{
		signatureService: &mockSignatureService{},
		quotaRecorder:    recorder,
		tenantProvider:   tp,
	}

	body, _ := json.Marshal(CreateSignatureRequest{DocID: "test-doc-123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/signatures", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(addUserToContext(req.Context(), testUser))
	rec := httptest.NewRecorder()

	handler.HandleCreateSignature(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.True(t, recorder.checkCalled, "quota check should be called")
	assert.False(t, recorder.recordCalled, "quota record should NOT be called when check fails")
}

func TestHandler_HandleCreateSignature_NoQuotaRecorder(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		signatureService: &mockSignatureService{},
	}

	body, _ := json.Marshal(CreateSignatureRequest{DocID: "test-doc-123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/signatures", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(addUserToContext(req.Context(), testUser))
	rec := httptest.NewRecorder()

	handler.HandleCreateSignature(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestHandler_HandleCreateSignature_QuotaNotRecordedOnError(t *testing.T) {
	t.Parallel()

	recorder := &mockQuotaRecorder{}
	tp := &mockTenantProvider{tenantID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")}

	handler := &Handler{
		signatureService: &mockSignatureService{
			createSignatureFunc: func(_ context.Context, _ *models.SignatureRequest) error {
				return fmt.Errorf("database error")
			},
		},
		quotaRecorder:  recorder,
		tenantProvider: tp,
	}

	body, _ := json.Marshal(CreateSignatureRequest{DocID: "test-doc-123"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/signatures", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(addUserToContext(req.Context(), testUser))
	rec := httptest.NewRecorder()

	handler.HandleCreateSignature(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.True(t, recorder.checkCalled, "quota check should be called before attempt")
	assert.False(t, recorder.recordCalled, "quota record should NOT be called when creation fails")
}

func Benchmark_toSignatureResponse(b *testing.B) {
	handler := createTestHandler()
	ctx := context.Background()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		handler.toSignatureResponse(ctx, testSignature)
	}
}
