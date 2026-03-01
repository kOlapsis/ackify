// SPDX-License-Identifier: AGPL-3.0-or-later
package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/btouchard/ackify-ce/backend/internal/application/services"
	"github.com/btouchard/ackify-ce/backend/internal/presentation/api/shared"
	"github.com/btouchard/ackify-ce/backend/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// MOCKS
// ============================================================================

type mockStorageProvider struct {
	uploadFunc   func(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	downloadFunc func(ctx context.Context, key string) (io.ReadCloser, int64, string, error)
	deleteFunc   func(ctx context.Context, key string) error
	providerType string
}

func (m *mockStorageProvider) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	if m.uploadFunc != nil {
		return m.uploadFunc(ctx, key, reader, size, contentType)
	}
	return nil
}

func (m *mockStorageProvider) Download(ctx context.Context, key string) (io.ReadCloser, int64, string, error) {
	if m.downloadFunc != nil {
		return m.downloadFunc(ctx, key)
	}
	return io.NopCloser(bytes.NewReader([]byte("content"))), 7, "application/pdf", nil
}

func (m *mockStorageProvider) Delete(ctx context.Context, key string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, key)
	}
	return nil
}

func (m *mockStorageProvider) Exists(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (m *mockStorageProvider) Type() string {
	if m.providerType != "" {
		return m.providerType
	}
	return "managed_s3"
}

type mockDocService struct {
	createFunc func(ctx context.Context, req services.CreateDocumentRequest) (*models.Document, error)
	getFunc    func(ctx context.Context, docID string) (*models.Document, error)
}

func (m *mockDocService) CreateDocument(ctx context.Context, req services.CreateDocumentRequest) (*models.Document, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, req)
	}
	return &models.Document{
		DocID:             "test-doc-123",
		Title:             req.Title,
		StorageKey:        req.StorageKey,
		StorageProvider:   req.StorageProvider,
		FileSize:          req.FileSize,
		MimeType:          req.MimeType,
		Checksum:          req.Checksum,
		ChecksumAlgorithm: req.ChecksumAlgorithm,
	}, nil
}

func (m *mockDocService) GetByDocID(ctx context.Context, docID string) (*models.Document, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, docID)
	}
	return nil, errors.New("not found")
}

type mockStorageQuotaChecker struct {
	checkCalled bool
	checkErr    error
	lastSize    int64
}

func (m *mockStorageQuotaChecker) CheckStorageQuota(_ context.Context, _ string, fileSize int64) error {
	m.checkCalled = true
	m.lastSize = fileSize
	return m.checkErr
}

// ============================================================================
// HELPERS
// ============================================================================

func createMultipartRequest(t *testing.T, filename string, content []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)

	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/storage/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func createHandlerWithQuota(checker StorageQuotaChecker) *Handler {
	h := NewHandler(&mockStorageProvider{}, &mockDocService{}, 10)
	h.WithStorageQuotaChecker(checker, func(_ context.Context) string {
		return "tenant-123"
	})
	return h
}

// ============================================================================
// TESTS — Storage Quota Enforcement
// ============================================================================

func TestHandler_HandleUpload_QuotaAllowed(t *testing.T) {
	t.Parallel()

	quota := &mockStorageQuotaChecker{}
	handler := createHandlerWithQuota(quota)

	// Create a valid PDF-like file (starts with %PDF)
	content := []byte("%PDF-1.4 test content for upload quota check")
	req := createMultipartRequest(t, "test.pdf", content)
	ctx := context.WithValue(req.Context(), shared.ContextKeyUser, &models.User{Email: "user@test.com"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.HandleUpload(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.True(t, quota.checkCalled, "quota check should be called before upload")
}

func TestHandler_HandleUpload_QuotaExceeded(t *testing.T) {
	t.Parallel()

	quota := &mockStorageQuotaChecker{checkErr: errors.New("storage quota exceeded")}
	handler := createHandlerWithQuota(quota)

	content := []byte("%PDF-1.4 test content for upload quota check")
	req := createMultipartRequest(t, "test.pdf", content)
	ctx := context.WithValue(req.Context(), shared.ContextKeyUser, &models.User{Email: "user@test.com"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.HandleUpload(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.True(t, quota.checkCalled, "quota check should be called")
}

func TestHandler_HandleUpload_NoQuotaChecker(t *testing.T) {
	t.Parallel()

	// Without quota checker, upload should succeed
	handler := NewHandler(&mockStorageProvider{}, &mockDocService{}, 10)

	content := []byte("%PDF-1.4 test content for upload without quota")
	req := createMultipartRequest(t, "test.pdf", content)
	ctx := context.WithValue(req.Context(), shared.ContextKeyUser, &models.User{Email: "user@test.com"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.HandleUpload(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestHandler_HandleUpload_QuotaNotCheckedWithoutTenantID(t *testing.T) {
	t.Parallel()

	quota := &mockStorageQuotaChecker{}
	handler := NewHandler(&mockStorageProvider{}, &mockDocService{}, 10)
	// Set quota checker but with empty tenant ID
	handler.WithStorageQuotaChecker(quota, func(_ context.Context) string {
		return "" // empty tenant = no quota check
	})

	content := []byte("%PDF-1.4 test content for upload without tenant")
	req := createMultipartRequest(t, "test.pdf", content)
	ctx := context.WithValue(req.Context(), shared.ContextKeyUser, &models.User{Email: "user@test.com"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.HandleUpload(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.False(t, quota.checkCalled, "quota check should NOT be called when tenant ID is empty")
}
