// SPDX-License-Identifier: AGPL-3.0-or-later
package services

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kolapsis/ackify/backend/pkg/config"
	"github.com/kolapsis/ackify/backend/pkg/models"
)

// mockSignatureRepository for testing
type mockSignatureRepository struct {
	createFunc             func(ctx context.Context, signature *models.Signature) error
	existsByDocAndUserFunc func(ctx context.Context, docID, userSub string) (bool, error)
	getLastSignatureFunc   func(ctx context.Context, docID string) (*models.Signature, error)
	getByDocAndUserFunc    func(ctx context.Context, docID, userSub string) (*models.Signature, error)
}

func (m *mockSignatureRepository) Create(ctx context.Context, signature *models.Signature) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, signature)
	}
	signature.ID = 1
	signature.CreatedAt = time.Now()
	return nil
}

func (m *mockSignatureRepository) ExistsByDocAndUser(ctx context.Context, docID, userSub string) (bool, error) {
	if m.existsByDocAndUserFunc != nil {
		return m.existsByDocAndUserFunc(ctx, docID, userSub)
	}
	return false, nil
}

func (m *mockSignatureRepository) GetLastSignature(ctx context.Context, docID string) (*models.Signature, error) {
	if m.getLastSignatureFunc != nil {
		return m.getLastSignatureFunc(ctx, docID)
	}
	return nil, nil
}

func (m *mockSignatureRepository) GetByDocAndUser(ctx context.Context, docID, userSub string) (*models.Signature, error) {
	if m.getByDocAndUserFunc != nil {
		return m.getByDocAndUserFunc(ctx, docID, userSub)
	}
	return nil, models.ErrSignatureNotFound
}

func (m *mockSignatureRepository) GetByDoc(ctx context.Context, docID string) ([]*models.Signature, error) {
	return nil, nil
}

func (m *mockSignatureRepository) GetByUserEmail(ctx context.Context, userEmail string) ([]*models.Signature, error) {
	return nil, nil
}

func (m *mockSignatureRepository) CheckUserSignatureStatus(ctx context.Context, docID, userIdentifier string) (bool, error) {
	return false, nil
}

func (m *mockSignatureRepository) GetAllSignaturesOrdered(ctx context.Context) ([]*models.Signature, error) {
	return nil, nil
}

func (m *mockSignatureRepository) UpdatePrevHash(ctx context.Context, id int64, prevHash *string) error {
	return nil
}

func (m *mockSignatureRepository) Count(ctx context.Context) (int, error) {
	return 0, nil
}

// mockCryptoSigner for testing
type mockCryptoSigner struct{}

func (m *mockCryptoSigner) CreateSignature(ctx context.Context, docID string, user *models.User, timestamp time.Time, nonce string, docChecksum string) (string, string, error) {
	return "payload_hash", "signature_base64", nil
}

// Test document integrity verification with matching checksum
func TestSignatureService_DocumentIntegrity_Success(t *testing.T) {
	content := "Sample PDF content"
	expectedChecksum := "b3b4e8714358cc79990c5c83391172e01c3e79a1b456d7e0c570cbf59da30e23"

	// Create test server with consistent content
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		if r.Method == "GET" {
			w.Write([]byte(content))
		}
	}))
	defer server.Close()

	// Create mock repositories
	docRepo := &mockDocumentRepository{
		getByDocIDFunc: func(ctx context.Context, docID string) (*models.Document, error) {
			return &models.Document{
				DocID:             "test-doc",
				URL:               server.URL,
				Checksum:          expectedChecksum,
				ChecksumAlgorithm: "SHA-256",
			}, nil
		},
	}

	sigRepo := &mockSignatureRepository{}
	signer := &mockCryptoSigner{}

	// Create service with checksum config
	service := NewSignatureService(sigRepo, docRepo, signer)
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
	service.SetChecksumConfig(checksumConfig)

	// Create signature request
	user := &models.User{
		Sub:   "test-user",
		Email: "test@example.com",
		Name:  "Test User",
	}

	request := &models.SignatureRequest{
		DocID: "test-doc",
		User:  user,
	}

	// Should succeed because checksum matches
	err := service.CreateSignature(context.Background(), request)
	if err != nil {
		t.Fatalf("Expected signature creation to succeed, got error: %v", err)
	}
}

// Test document integrity verification with mismatched checksum
func TestSignatureService_DocumentIntegrity_Modified(t *testing.T) {
	content := "Modified PDF content"
	storedChecksum := "b3b4e8714358cc79990c5c83391172e01c3e79a1b456d7e0c570cbf59da30e23" // Original checksum

	// Create test server with different content
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		if r.Method == "GET" {
			w.Write([]byte(content))
		}
	}))
	defer server.Close()

	// Create mock repositories
	docRepo := &mockDocumentRepository{
		getByDocIDFunc: func(ctx context.Context, docID string) (*models.Document, error) {
			return &models.Document{
				DocID:             "test-doc",
				URL:               server.URL,
				Checksum:          storedChecksum,
				ChecksumAlgorithm: "SHA-256",
			}, nil
		},
	}

	sigRepo := &mockSignatureRepository{}
	signer := &mockCryptoSigner{}

	// Create service with checksum config
	service := NewSignatureService(sigRepo, docRepo, signer)
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
	service.SetChecksumConfig(checksumConfig)

	// Create signature request
	user := &models.User{
		Sub:   "test-user",
		Email: "test@example.com",
		Name:  "Test User",
	}

	request := &models.SignatureRequest{
		DocID: "test-doc",
		User:  user,
	}

	// Should fail with ErrDocumentModified
	err := service.CreateSignature(context.Background(), request)
	if err != models.ErrDocumentModified {
		t.Fatalf("Expected ErrDocumentModified, got: %v", err)
	}
}

// Test signature creation without checksum (document has no URL or checksum)
func TestSignatureService_NoChecksum_Success(t *testing.T) {
	// Create mock repositories
	docRepo := &mockDocumentRepository{
		getByDocIDFunc: func(ctx context.Context, docID string) (*models.Document, error) {
			return &models.Document{
				DocID:    "test-doc",
				URL:      "",
				Checksum: "",
			}, nil
		},
	}

	sigRepo := &mockSignatureRepository{}
	signer := &mockCryptoSigner{}

	// Create service with checksum config
	service := NewSignatureService(sigRepo, docRepo, signer)
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
	service.SetChecksumConfig(checksumConfig)

	// Create signature request
	user := &models.User{
		Sub:   "test-user",
		Email: "test@example.com",
		Name:  "Test User",
	}

	request := &models.SignatureRequest{
		DocID: "test-doc",
		User:  user,
	}

	// Should succeed because no checksum to verify
	err := service.CreateSignature(context.Background(), request)
	if err != nil {
		t.Fatalf("Expected signature creation to succeed without checksum, got error: %v", err)
	}
}

// Test signature creation without checksum config
func TestSignatureService_NoChecksumConfig_Success(t *testing.T) {
	content := "Sample PDF content"

	// Create test server
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		if r.Method == "GET" {
			w.Write([]byte(content))
		}
	}))
	defer server.Close()

	// Create mock repositories
	docRepo := &mockDocumentRepository{
		getByDocIDFunc: func(ctx context.Context, docID string) (*models.Document, error) {
			return &models.Document{
				DocID:             "test-doc",
				URL:               server.URL,
				Checksum:          "some_checksum",
				ChecksumAlgorithm: "SHA-256",
			}, nil
		},
	}

	sigRepo := &mockSignatureRepository{}
	signer := &mockCryptoSigner{}

	// Create service WITHOUT checksum config
	service := NewSignatureService(sigRepo, docRepo, signer)
	// Don't call SetChecksumConfig

	// Create signature request
	user := &models.User{
		Sub:   "test-user",
		Email: "test@example.com",
		Name:  "Test User",
	}

	request := &models.SignatureRequest{
		DocID: "test-doc",
		User:  user,
	}

	// Should succeed because no config means no verification
	err := service.CreateSignature(context.Background(), request)
	if err != nil {
		t.Fatalf("Expected signature creation to succeed without config, got error: %v", err)
	}
}

// Test document integrity with network error (should not block signature)
func TestSignatureService_NetworkError_ContinuesAnyway(t *testing.T) {
	// Create mock repositories with unreachable URL
	docRepo := &mockDocumentRepository{
		getByDocIDFunc: func(ctx context.Context, docID string) (*models.Document, error) {
			return &models.Document{
				DocID:             "test-doc",
				URL:               "https://non-existent-server-12345.example.com/doc.pdf",
				Checksum:          "some_checksum",
				ChecksumAlgorithm: "SHA-256",
			}, nil
		},
	}

	sigRepo := &mockSignatureRepository{}
	signer := &mockCryptoSigner{}

	// Create service with checksum config
	service := NewSignatureService(sigRepo, docRepo, signer)
	checksumConfig := &config.ChecksumConfig{
		MaxBytes:     10 * 1024 * 1024,
		TimeoutMs:    100, // Very short timeout
		MaxRedirects: 3,
		AllowedContentType: []string{
			"application/pdf",
		},
		SkipSSRFCheck:      false, // Enable SSRF check
		InsecureSkipVerify: false,
	}
	service.SetChecksumConfig(checksumConfig)

	// Create signature request
	user := &models.User{
		Sub:   "test-user",
		Email: "test@example.com",
		Name:  "Test User",
	}

	request := &models.SignatureRequest{
		DocID: "test-doc",
		User:  user,
	}

	// Should succeed even though we can't verify (network error doesn't block signature)
	err := service.CreateSignature(context.Background(), request)
	if err != nil {
		t.Fatalf("Expected signature creation to succeed despite network error, got: %v", err)
	}
}
