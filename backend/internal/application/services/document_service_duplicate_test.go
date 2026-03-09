// SPDX-License-Identifier: AGPL-3.0-or-later
package services

import (
	"context"
	"testing"

	"github.com/kolapsis/ackify/backend/pkg/models"
)

// mockDocRepo is a simple in-memory mock for testing document duplication scenarios
type mockDocRepo struct {
	documents map[string]*models.Document
	callCount int
}

func newMockDocRepo() *mockDocRepo {
	return &mockDocRepo{
		documents: make(map[string]*models.Document),
	}
}

func (m *mockDocRepo) Create(ctx context.Context, docID string, input models.DocumentInput, createdBy string) (*models.Document, error) {
	m.callCount++
	doc := &models.Document{
		DocID:             docID,
		Title:             input.Title,
		URL:               input.URL,
		Checksum:          input.Checksum,
		ChecksumAlgorithm: input.ChecksumAlgorithm,
		Description:       input.Description,
		CreatedBy:         createdBy,
	}
	m.documents[docID] = doc
	return doc, nil
}

func (m *mockDocRepo) GetByDocID(ctx context.Context, docID string) (*models.Document, error) {
	doc, exists := m.documents[docID]
	if !exists {
		return nil, nil
	}
	return doc, nil
}

func (m *mockDocRepo) FindByReference(ctx context.Context, ref string, refType string) (*models.Document, error) {
	// Search by reference logic
	switch refType {
	case "reference":
		// Search by doc_id
		return m.GetByDocID(ctx, ref)
	case "url":
		// Search by URL
		for _, doc := range m.documents {
			if doc.URL == ref {
				return doc, nil
			}
		}
	case "path":
		// Search by URL (paths stored in URL field)
		for _, doc := range m.documents {
			if doc.URL == ref {
				return doc, nil
			}
		}
	}
	return nil, nil
}

func (m *mockDocRepo) List(_ context.Context, _, _ int) ([]*models.Document, error) {
	result := make([]*models.Document, 0, len(m.documents))
	for _, doc := range m.documents {
		result = append(result, doc)
	}
	return result, nil
}

func (m *mockDocRepo) Search(_ context.Context, _ string, _, _ int) ([]*models.Document, error) {
	return []*models.Document{}, nil
}

func (m *mockDocRepo) Count(_ context.Context, _ string) (int, error) {
	return len(m.documents), nil
}

func (m *mockDocRepo) ListByCreatedBy(_ context.Context, _ string, _, _ int) ([]*models.Document, error) {
	return []*models.Document{}, nil
}

func (m *mockDocRepo) SearchByCreatedBy(_ context.Context, _, _ string, _, _ int) ([]*models.Document, error) {
	return []*models.Document{}, nil
}

func (m *mockDocRepo) CountByCreatedBy(_ context.Context, _, _ string) (int, error) {
	return 0, nil
}

// TestFindOrCreateDocument_SameReferenceTwice tests that calling FindOrCreateDocument
// with the same reference twice does NOT create duplicate documents
func TestFindOrCreateDocument_SameReferenceTwice(t *testing.T) {
	ctx := context.Background()
	repo := newMockDocRepo()
	service := NewDocumentService(repo, &mockDocExpectedSignerRepoTest{}, nil)

	reference := "doc-123"

	// First call - should create document
	doc1, isNew1, err := service.FindOrCreateDocument(ctx, reference, "")
	if err != nil {
		t.Fatalf("First FindOrCreateDocument failed: %v", err)
	}

	if !isNew1 {
		t.Error("First call should return isNew=true")
	}

	if doc1.DocID != reference {
		t.Errorf("Expected doc_id=%s, got %s", reference, doc1.DocID)
	}

	// Second call with SAME reference - should find existing document
	doc2, isNew2, err := service.FindOrCreateDocument(ctx, reference, "")
	if err != nil {
		t.Fatalf("Second FindOrCreateDocument failed: %v", err)
	}

	if isNew2 {
		t.Error("Second call should return isNew=false (document already exists)")
	}

	if doc2.DocID != doc1.DocID {
		t.Errorf("Expected same doc_id=%s, got different doc_id=%s", doc1.DocID, doc2.DocID)
	}

	// Verify only ONE document was created
	if len(repo.documents) != 1 {
		t.Errorf("Expected 1 document in repository, got %d", len(repo.documents))
	}

	// Verify Create was called only ONCE
	if repo.callCount != 1 {
		t.Errorf("Expected Create to be called 1 time, got %d calls", repo.callCount)
	}
}

// TestFindOrCreateDocument_URLReference tests that URL references are properly deduplicated
func TestFindOrCreateDocument_URLReference(t *testing.T) {
	ctx := context.Background()
	repo := newMockDocRepo()
	service := NewDocumentService(repo, &mockDocExpectedSignerRepoTest{}, nil)

	urlRef := "https://example.com/policy.pdf"

	// First call - should create document
	doc1, isNew1, err := service.FindOrCreateDocument(ctx, urlRef, "")
	if err != nil {
		t.Fatalf("First FindOrCreateDocument failed: %v", err)
	}

	if !isNew1 {
		t.Error("First call should return isNew=true")
	}

	firstDocID := doc1.DocID

	// Second call with SAME URL - should find existing document
	doc2, isNew2, err := service.FindOrCreateDocument(ctx, urlRef, "")
	if err != nil {
		t.Fatalf("Second FindOrCreateDocument failed: %v", err)
	}

	if isNew2 {
		t.Error("Second call should return isNew=false (document with this URL already exists)")
	}

	if doc2.DocID != firstDocID {
		t.Errorf("Expected same doc_id=%s, got different doc_id=%s", firstDocID, doc2.DocID)
	}

	// Verify only ONE document was created
	if len(repo.documents) != 1 {
		t.Errorf("Expected 1 document in repository, got %d", len(repo.documents))
	}
}

// TestCreateDocument_AlwaysCreatesNew demonstrates the problematic behavior
// of CreateDocument (always creates new documents without checking)
func TestCreateDocument_AlwaysCreatesNew(t *testing.T) {
	ctx := context.Background()
	repo := newMockDocRepo()
	service := NewDocumentService(repo, &mockDocExpectedSignerRepoTest{}, nil)

	reference := "doc-456"

	// First call
	doc1, err := service.CreateDocument(ctx, CreateDocumentRequest{Reference: reference})
	if err != nil {
		t.Fatalf("First CreateDocument failed: %v", err)
	}

	firstDocID := doc1.DocID

	// Second call with SAME reference
	doc2, err := service.CreateDocument(ctx, CreateDocumentRequest{Reference: reference})
	if err != nil {
		t.Fatalf("Second CreateDocument failed: %v", err)
	}

	secondDocID := doc2.DocID

	// This is the PROBLEM: CreateDocument creates different doc_ids for the same reference
	if firstDocID == secondDocID {
		t.Error("CreateDocument generated the same doc_id twice (unlikely but possible)")
	}

	// This demonstrates the bug: we now have 2+ documents for the same reference
	if len(repo.documents) < 2 {
		t.Logf("WARNING: CreateDocument was called twice but created %d documents", len(repo.documents))
	}

	t.Logf("CreateDocument behavior: Reference '%s' created doc_id '%s' and '%s' (DUPLICATION)",
		reference, firstDocID, secondDocID)
}
