// SPDX-License-Identifier: AGPL-3.0-or-later
package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kolapsis/ackify/backend/pkg/config"
	"github.com/kolapsis/ackify/backend/pkg/models"
)

// mockDocExpectedSignerRepo is a minimal mock for docExpectedSignerRepository
type mockDocExpectedSignerRepo struct{}

func (m *mockDocExpectedSignerRepo) ListByDocID(_ context.Context, _ string) ([]*models.ExpectedSigner, error) {
	return []*models.ExpectedSigner{}, nil
}

func (m *mockDocExpectedSignerRepo) GetStats(_ context.Context, _ string) (*models.DocCompletionStats, error) {
	return &models.DocCompletionStats{}, nil
}

func (m *mockDocExpectedSignerRepo) FindPendingForEmail(_ context.Context, _ string) ([]*models.PendingDocument, error) {
	return nil, nil
}

// Test automatic checksum computation with valid PDF
func TestDocumentService_CreateDocument_WithAutomaticChecksum(t *testing.T) {
	content := "Sample PDF content"
	expectedChecksum := "b3b4e8714358cc79990c5c83391172e01c3e79a1b456d7e0c570cbf59da30e23" // SHA-256

	// Create test HTTP server
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		if r.Method == "GET" {
			w.Write([]byte(content))
		}
	}))
	defer server.Close()

	mockRepo := &mockDocumentRepository{}
	checksumConfig := &config.ChecksumConfig{
		MaxBytes:     10 * 1024 * 1024, // 10 MB
		TimeoutMs:    5000,
		MaxRedirects: 3,
		AllowedContentType: []string{
			"application/pdf",
			"image/*",
		},
		SkipSSRFCheck:      true, // For testing with httptest
		InsecureSkipVerify: true, // Accept self-signed certs in tests
	}
	service := NewDocumentService(mockRepo, &mockDocExpectedSignerRepo{}, checksumConfig)

	req := CreateDocumentRequest{
		Reference: server.URL,
		Title:     "Test Document",
	}

	ctx := context.Background()
	doc, err := service.CreateDocument(ctx, req)

	if err != nil {
		t.Fatalf("CreateDocument failed: %v", err)
	}

	if doc == nil {
		t.Fatal("Expected document to be created, got nil")
	}

	// Verify checksum was computed
	if doc.Checksum != expectedChecksum {
		t.Errorf("Expected checksum %q, got %q", expectedChecksum, doc.Checksum)
	}

	if doc.ChecksumAlgorithm != "SHA-256" {
		t.Errorf("Expected algorithm SHA-256, got %q", doc.ChecksumAlgorithm)
	}
}

// Test automatic checksum computation with HTTP (should be rejected with ErrHTTPSRequired)
func TestDocumentService_CreateDocument_RejectsHTTP(t *testing.T) {
	mockRepo := &mockDocumentRepository{}
	checksumConfig := &config.ChecksumConfig{
		MaxBytes:     10 * 1024 * 1024,
		TimeoutMs:    5000,
		MaxRedirects: 3,
		AllowedContentType: []string{
			"application/pdf",
		},
		SkipSSRFCheck:      true,
		InsecureSkipVerify: true,
	}
	service := NewDocumentService(mockRepo, &mockDocExpectedSignerRepo{}, checksumConfig)

	// HTTP URL (not HTTPS)
	req := CreateDocumentRequest{
		Reference: "http://example.com/document.pdf",
		Title:     "Test Document",
	}

	ctx := context.Background()
	_, err := service.CreateDocument(ctx, req)

	if err == nil {
		t.Fatal("Expected error for HTTP URL, got nil")
	}
	if !errors.Is(err, ErrHTTPSRequired) {
		t.Errorf("Expected ErrHTTPSRequired, got %v", err)
	}
}

// Test automatic checksum computation with too large file
func TestDocumentService_CreateDocument_TooLargeFile(t *testing.T) {
	// Create test HTTP server that returns large Content-Length
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", "20971520") // 20 MB
		if r.Method == "GET" {
			w.Write([]byte("should not reach here"))
		}
	}))
	defer server.Close()

	mockRepo := &mockDocumentRepository{}
	checksumConfig := &config.ChecksumConfig{
		MaxBytes:     10 * 1024 * 1024, // 10 MB limit
		TimeoutMs:    5000,
		MaxRedirects: 3,
		AllowedContentType: []string{
			"application/pdf",
		},
	}
	service := NewDocumentService(mockRepo, &mockDocExpectedSignerRepo{}, checksumConfig)

	req := CreateDocumentRequest{
		Reference: server.URL,
		Title:     "Large Document",
	}

	ctx := context.Background()
	doc, err := service.CreateDocument(ctx, req)

	if err != nil {
		t.Fatalf("CreateDocument failed: %v", err)
	}

	// Document should be created, but without checksum (file too large)
	if doc.Checksum != "" {
		t.Error("Expected checksum to be empty for too large file, got", doc.Checksum)
	}
}

// Test automatic checksum computation with wrong content type
func TestDocumentService_CreateDocument_WrongContentType(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html") // Not allowed
		w.Header().Set("Content-Length", "100")
		if r.Method == "GET" {
			w.Write([]byte("<html>test</html>"))
		}
	}))
	defer server.Close()

	mockRepo := &mockDocumentRepository{}
	checksumConfig := &config.ChecksumConfig{
		MaxBytes:     10 * 1024 * 1024,
		TimeoutMs:    5000,
		MaxRedirects: 3,
		AllowedContentType: []string{
			"application/pdf",
		},
		SkipSSRFCheck:      true,
		InsecureSkipVerify: true,
	}
	service := NewDocumentService(mockRepo, &mockDocExpectedSignerRepo{}, checksumConfig)

	req := CreateDocumentRequest{
		Reference: server.URL,
		Title:     "HTML Document",
	}

	ctx := context.Background()
	doc, err := service.CreateDocument(ctx, req)

	if err != nil {
		t.Fatalf("CreateDocument failed: %v", err)
	}

	// Document should be created, but without checksum (wrong content type)
	if doc.Checksum != "" {
		t.Error("Expected checksum to be empty for wrong content type, got", doc.Checksum)
	}
}

// Test automatic checksum computation with image wildcard
func TestDocumentService_CreateDocument_ImageWildcard(t *testing.T) {
	content := []byte{0x89, 0x50, 0x4E, 0x47} // PNG header
	expectedChecksum := "0f4636c78f65d3639ece5a064b5ae753e3408614a14fb18ab4d7540d2c248543"

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		if r.Method == "GET" {
			w.Write(content)
		}
	}))
	defer server.Close()

	mockRepo := &mockDocumentRepository{}
	checksumConfig := &config.ChecksumConfig{
		MaxBytes:     10 * 1024 * 1024,
		TimeoutMs:    5000,
		MaxRedirects: 3,
		AllowedContentType: []string{
			"image/*", // Wildcard for all images
		},
		SkipSSRFCheck:      true,
		InsecureSkipVerify: true,
	}
	service := NewDocumentService(mockRepo, &mockDocExpectedSignerRepo{}, checksumConfig)

	req := CreateDocumentRequest{
		Reference: server.URL,
		Title:     "Test Image",
	}

	ctx := context.Background()
	doc, err := service.CreateDocument(ctx, req)

	if err != nil {
		t.Fatalf("CreateDocument failed: %v", err)
	}

	// Verify checksum was computed for image
	if doc.Checksum != expectedChecksum {
		t.Errorf("Expected checksum %q, got %q", expectedChecksum, doc.Checksum)
	}
}

// Test automatic checksum computation disabled (nil config)
func TestDocumentService_CreateDocument_NoChecksumConfig(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("content"))
	}))
	defer server.Close()

	mockRepo := &mockDocumentRepository{}
	service := NewDocumentService(mockRepo, &mockDocExpectedSignerRepo{}, nil) // No checksum config

	req := CreateDocumentRequest{
		Reference: server.URL,
		Title:     "Test Document",
	}

	ctx := context.Background()
	doc, err := service.CreateDocument(ctx, req)

	if err != nil {
		t.Fatalf("CreateDocument failed: %v", err)
	}

	// Document should be created without checksum (feature disabled)
	if doc.Checksum != "" {
		t.Error("Expected checksum to be empty when config is nil, got", doc.Checksum)
	}
}

// Test automatic checksum computation with network error
func TestDocumentService_CreateDocument_NetworkError(t *testing.T) {
	mockRepo := &mockDocumentRepository{}
	checksumConfig := &config.ChecksumConfig{
		MaxBytes:     10 * 1024 * 1024,
		TimeoutMs:    100, // Very short timeout
		MaxRedirects: 3,
		AllowedContentType: []string{
			"application/pdf",
		},
	}
	service := NewDocumentService(mockRepo, &mockDocExpectedSignerRepo{}, checksumConfig)

	// Non-existent server
	req := CreateDocumentRequest{
		Reference: "https://non-existent-server-12345.example.com/doc.pdf",
		Title:     "Test Document",
	}

	ctx := context.Background()
	doc, err := service.CreateDocument(ctx, req)

	if err != nil {
		t.Fatalf("CreateDocument failed: %v", err)
	}

	// Document should be created without checksum (network error)
	if doc.Checksum != "" {
		t.Error("Expected checksum to be empty for network error, got", doc.Checksum)
	}
}

// Test CreateDocument without URL (plain reference)
func TestDocumentService_CreateDocument_PlainReferenceNoChecksum(t *testing.T) {
	mockRepo := &mockDocumentRepository{}
	checksumConfig := &config.ChecksumConfig{
		MaxBytes:     10 * 1024 * 1024,
		TimeoutMs:    5000,
		MaxRedirects: 3,
		AllowedContentType: []string{
			"application/pdf",
		},
		SkipSSRFCheck:      true,
		InsecureSkipVerify: true,
	}
	service := NewDocumentService(mockRepo, &mockDocExpectedSignerRepo{}, checksumConfig)

	req := CreateDocumentRequest{
		Reference: "company-policy-2024",
		Title:     "",
	}

	ctx := context.Background()
	doc, err := service.CreateDocument(ctx, req)

	if err != nil {
		t.Fatalf("CreateDocument failed: %v", err)
	}

	// Document should be created without checksum (no URL)
	if doc.Checksum != "" {
		t.Error("Expected checksum to be empty for plain reference, got", doc.Checksum)
	}

	// Verify it's not treated as URL
	if doc.URL != "" {
		t.Errorf("Expected URL to be empty, got %q", doc.URL)
	}
}
