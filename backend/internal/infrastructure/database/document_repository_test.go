// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build integration

package database

import (
	"context"
	"testing"

	"github.com/kolapsis/ackify/backend/pkg/models"
)

// setupDocumentsTable is no longer needed - migrations handle schema creation
// Removed to use real migrations from testutils.go

func clearDocumentsTable(t *testing.T, testDB *TestDB) {
	t.Helper()
	_, err := testDB.DB.Exec("TRUNCATE TABLE documents RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("failed to clear documents table: %v", err)
	}
}

func TestDocumentRepository_Create(t *testing.T) {
	testDB := SetupTestDB(t)

	ctx := context.Background()
	repo := NewDocumentRepository(testDB.DB, testDB.TenantProvider)

	input := models.DocumentInput{
		Title:             "Test Document",
		URL:               "https://example.com/doc.pdf",
		Checksum:          "abc123def456",
		ChecksumAlgorithm: "SHA-256",
		Description:       "A test document for integration testing",
	}

	doc, err := repo.Create(ctx, "test-doc-001", input, "admin@example.com")
	if err != nil {
		t.Fatalf("Failed to create document: %v", err)
	}

	if doc.DocID != "test-doc-001" {
		t.Errorf("Expected DocID test-doc-001, got %s", doc.DocID)
	}

	if doc.Title != input.Title {
		t.Errorf("Expected Title %s, got %s", input.Title, doc.Title)
	}

	if doc.URL != input.URL {
		t.Errorf("Expected URL %s, got %s", input.URL, doc.URL)
	}

	if doc.Checksum != input.Checksum {
		t.Errorf("Expected Checksum %s, got %s", input.Checksum, doc.Checksum)
	}

	if doc.ChecksumAlgorithm != input.ChecksumAlgorithm {
		t.Errorf("Expected ChecksumAlgorithm %s, got %s", input.ChecksumAlgorithm, doc.ChecksumAlgorithm)
	}

	if doc.Description != input.Description {
		t.Errorf("Expected Description %s, got %s", input.Description, doc.Description)
	}

	if doc.CreatedBy != "admin@example.com" {
		t.Errorf("Expected CreatedBy admin@example.com, got %s", doc.CreatedBy)
	}

	if doc.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}

	if doc.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
}

func TestDocumentRepository_GetByDocID(t *testing.T) {
	testDB := SetupTestDB(t)

	ctx := context.Background()
	repo := NewDocumentRepository(testDB.DB, testDB.TenantProvider)

	input := models.DocumentInput{
		Title:             "Get Test Document",
		URL:               "https://example.com/get-doc.pdf",
		Checksum:          "get123abc456",
		ChecksumAlgorithm: "SHA-512",
		Description:       "Document for get testing",
	}

	created, err := repo.Create(ctx, "get-doc-001", input, "user@example.com")
	if err != nil {
		t.Fatalf("Failed to create document: %v", err)
	}

	retrieved, err := repo.GetByDocID(ctx, "get-doc-001")
	if err != nil {
		t.Fatalf("Failed to get document: %v", err)
	}

	if retrieved == nil {
		t.Fatal("Retrieved document is nil")
	}

	if retrieved.DocID != created.DocID {
		t.Errorf("Expected DocID %s, got %s", created.DocID, retrieved.DocID)
	}

	if retrieved.Title != created.Title {
		t.Errorf("Expected Title %s, got %s", created.Title, retrieved.Title)
	}

	// Test non-existent document
	nonExistent, err := repo.GetByDocID(ctx, "non-existent-doc")
	if err != nil {
		t.Errorf("Expected no error for non-existent document, got %v", err)
	}

	if nonExistent != nil {
		t.Error("Expected nil for non-existent document")
	}
}

func TestDocumentRepository_Update(t *testing.T) {
	testDB := SetupTestDB(t)

	ctx := context.Background()
	repo := NewDocumentRepository(testDB.DB, testDB.TenantProvider)

	input := models.DocumentInput{
		Title:             "Original Title",
		URL:               "https://example.com/original.pdf",
		Checksum:          "original123",
		ChecksumAlgorithm: "MD5",
		Description:       "Original description",
	}

	created, err := repo.Create(ctx, "update-doc-001", input, "creator@example.com")
	if err != nil {
		t.Fatalf("Failed to create document: %v", err)
	}

	updateInput := models.DocumentInput{
		Title:             "Updated Title",
		URL:               "https://example.com/updated.pdf",
		Checksum:          "updated456",
		ChecksumAlgorithm: "SHA-256",
		Description:       "Updated description",
	}

	updated, err := repo.Update(ctx, "update-doc-001", updateInput)
	if err != nil {
		t.Fatalf("Failed to update document: %v", err)
	}

	if updated.Title != updateInput.Title {
		t.Errorf("Expected Title %s, got %s", updateInput.Title, updated.Title)
	}

	if updated.URL != updateInput.URL {
		t.Errorf("Expected URL %s, got %s", updateInput.URL, updated.URL)
	}

	if updated.Checksum != updateInput.Checksum {
		t.Errorf("Expected Checksum %s, got %s", updateInput.Checksum, updated.Checksum)
	}

	if updated.ChecksumAlgorithm != updateInput.ChecksumAlgorithm {
		t.Errorf("Expected ChecksumAlgorithm %s, got %s", updateInput.ChecksumAlgorithm, updated.ChecksumAlgorithm)
	}

	if updated.Description != updateInput.Description {
		t.Errorf("Expected Description %s, got %s", updateInput.Description, updated.Description)
	}

	// CreatedBy should remain unchanged
	if updated.CreatedBy != created.CreatedBy {
		t.Errorf("Expected CreatedBy to remain %s, got %s", created.CreatedBy, updated.CreatedBy)
	}

	// UpdatedAt should be later than CreatedAt
	if !updated.UpdatedAt.After(created.CreatedAt) && !updated.UpdatedAt.Equal(created.CreatedAt) {
		t.Error("UpdatedAt should be after or equal to CreatedAt")
	}
}

func TestDocumentRepository_CreateOrUpdate(t *testing.T) {
	testDB := SetupTestDB(t)

	ctx := context.Background()
	repo := NewDocumentRepository(testDB.DB, testDB.TenantProvider)

	input := models.DocumentInput{
		Title:             "CreateOrUpdate Test",
		URL:               "https://example.com/test.pdf",
		Checksum:          "test123",
		ChecksumAlgorithm: "SHA-256",
		Description:       "Test description",
	}

	// First call should create
	doc1, err := repo.CreateOrUpdate(ctx, "upsert-doc-001", input, "creator@example.com")
	if err != nil {
		t.Fatalf("Failed to create document: %v", err)
	}

	if doc1.Title != input.Title {
		t.Errorf("Expected Title %s, got %s", input.Title, doc1.Title)
	}

	// Second call with same doc_id should update
	updateInput := models.DocumentInput{
		Title:             "Updated via Upsert",
		URL:               "https://example.com/updated.pdf",
		Checksum:          "updated789",
		ChecksumAlgorithm: "SHA-512",
		Description:       "Updated description",
	}

	doc2, err := repo.CreateOrUpdate(ctx, "upsert-doc-001", updateInput, "updater@example.com")
	if err != nil {
		t.Fatalf("Failed to update document: %v", err)
	}

	if doc2.Title != updateInput.Title {
		t.Errorf("Expected Title %s, got %s", updateInput.Title, doc2.Title)
	}

	if doc2.URL != updateInput.URL {
		t.Errorf("Expected URL %s, got %s", updateInput.URL, doc2.URL)
	}

	// Verify only one record exists
	retrieved, err := repo.GetByDocID(ctx, "upsert-doc-001")
	if err != nil {
		t.Fatalf("Failed to get document: %v", err)
	}

	if retrieved.Title != updateInput.Title {
		t.Errorf("Expected final Title %s, got %s", updateInput.Title, retrieved.Title)
	}
}

func TestDocumentRepository_Delete(t *testing.T) {
	testDB := SetupTestDB(t)

	ctx := context.Background()
	repo := NewDocumentRepository(testDB.DB, testDB.TenantProvider)

	input := models.DocumentInput{
		Title:             "Delete Test",
		URL:               "https://example.com/delete.pdf",
		Checksum:          "delete123",
		ChecksumAlgorithm: "SHA-256",
		Description:       "Document to be deleted",
	}

	_, err := repo.Create(ctx, "delete-doc-001", input, "admin@example.com")
	if err != nil {
		t.Fatalf("Failed to create document: %v", err)
	}

	err = repo.Delete(ctx, "delete-doc-001")
	if err != nil {
		t.Fatalf("Failed to delete document: %v", err)
	}

	// Verify deletion
	retrieved, err := repo.GetByDocID(ctx, "delete-doc-001")
	if err != nil {
		t.Errorf("Expected no error when getting deleted document, got %v", err)
	}

	if retrieved != nil {
		t.Error("Expected nil after deletion")
	}

	// Test deleting non-existent document
	err = repo.Delete(ctx, "non-existent-doc")
	if err == nil {
		t.Error("Expected error when deleting non-existent document")
	}
}

func TestDocumentRepository_List(t *testing.T) {
	testDB := SetupTestDB(t)

	ctx := context.Background()
	repo := NewDocumentRepository(testDB.DB, testDB.TenantProvider)

	// Create multiple documents
	for i := 1; i <= 5; i++ {
		input := models.DocumentInput{
			Title:             "Document " + string(rune('A'+i-1)),
			URL:               "https://example.com/doc" + string(rune('0'+i)) + ".pdf",
			Checksum:          "checksum" + string(rune('0'+i)),
			ChecksumAlgorithm: "SHA-256",
			Description:       "Test document " + string(rune('0'+i)),
		}

		_, err := repo.Create(ctx, "list-doc-00"+string(rune('0'+i)), input, "admin@example.com")
		if err != nil {
			t.Fatalf("Failed to create document %d: %v", i, err)
		}
	}

	// Test listing all
	docs, err := repo.List(ctx, 10, 0)
	if err != nil {
		t.Fatalf("Failed to list documents: %v", err)
	}

	if len(docs) != 5 {
		t.Errorf("Expected 5 documents, got %d", len(docs))
	}

	// Test pagination
	page1, err := repo.List(ctx, 2, 0)
	if err != nil {
		t.Fatalf("Failed to get page 1: %v", err)
	}

	if len(page1) != 2 {
		t.Errorf("Expected 2 documents in page 1, got %d", len(page1))
	}

	page2, err := repo.List(ctx, 2, 2)
	if err != nil {
		t.Fatalf("Failed to get page 2: %v", err)
	}

	if len(page2) != 2 {
		t.Errorf("Expected 2 documents in page 2, got %d", len(page2))
	}

	// Verify ordering (newest first)
	if len(docs) >= 2 {
		if docs[0].CreatedAt.Before(docs[1].CreatedAt) {
			t.Error("Documents should be ordered by created_at DESC")
		}
	}
}

func TestDocumentRepository_FindByReference_Integration(t *testing.T) {
	testDB := SetupTestDB(t)

	ctx := context.Background()
	repo := NewDocumentRepository(testDB.DB, testDB.TenantProvider)

	// Create a document first
	input := models.DocumentInput{
		Title: "Test Doc",
		URL:   "https://example.com/doc.pdf",
	}

	created, err := repo.Create(ctx, "test-doc-123", input, "admin@example.com")
	if err != nil {
		t.Fatalf("Failed to create document: %v", err)
	}

	// Test finding by URL reference
	found, err := repo.FindByReference(ctx, created.URL, "url")
	if err != nil {
		t.Fatalf("FindByReference failed: %v", err)
	}

	if found == nil {
		t.Fatal("Expected to find document, got nil")
	}

	if found.DocID != created.DocID {
		t.Errorf("Expected DocID %s, got %s", created.DocID, found.DocID)
	}

	// Test finding by reference type (doc_id)
	foundByRef, err := repo.FindByReference(ctx, "test-doc-123", "reference")
	if err != nil {
		t.Fatalf("FindByReference by ref failed: %v", err)
	}

	if foundByRef == nil {
		t.Fatal("Expected to find document by reference, got nil")
	}

	// Test not found case
	notFound, err := repo.FindByReference(ctx, "non-existent-url", "url")
	if err == nil && notFound == nil {
		// This is expected - not found
	}
}
