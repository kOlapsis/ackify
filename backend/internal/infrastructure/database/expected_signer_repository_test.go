//go:build integration

// SPDX-License-Identifier: AGPL-3.0-or-later
package database

import (
	"context"
	"testing"

	"github.com/kolapsis/ackify/backend/pkg/models"
)

func TestExpectedSignerRepository_AddExpected(t *testing.T) {
	testDB := SetupTestDB(t)
	repo := NewExpectedSignerRepository(testDB.DB, testDB.TenantProvider)
	ctx := context.Background()

	tests := []struct {
		name      string
		docID     string
		contacts  []models.ContactInfo
		addedBy   string
		wantError bool
	}{
		{
			name:  "add single expected signer",
			docID: "doc-001",
			contacts: []models.ContactInfo{
				{Email: "user1@example.com", Name: ""},
			},
			addedBy:   "admin@example.com",
			wantError: false,
		},
		{
			name:  "add multiple expected signers",
			docID: "doc-002",
			contacts: []models.ContactInfo{
				{Email: "user1@example.com", Name: "User One"},
				{Email: "user2@example.com", Name: "User Two"},
				{Email: "user3@example.com", Name: ""},
			},
			addedBy:   "admin@example.com",
			wantError: false,
		},
		{
			name:  "add duplicate emails (should not error)",
			docID: "doc-003",
			contacts: []models.ContactInfo{
				{Email: "duplicate@example.com", Name: ""},
				{Email: "duplicate@example.com", Name: ""},
			},
			addedBy:   "admin@example.com",
			wantError: false,
		},
		{
			name:      "add empty list",
			docID:     "doc-004",
			contacts:  []models.ContactInfo{},
			addedBy:   "admin@example.com",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearExpectedSignersTable(t, testDB)

			err := repo.AddExpected(ctx, tt.docID, tt.contacts, tt.addedBy)

			if tt.wantError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			// Verify records were added
			if !tt.wantError && len(tt.contacts) > 0 {
				signers, err := repo.ListByDocID(ctx, tt.docID)
				if err != nil {
					t.Fatalf("failed to list signers: %v", err)
				}

				// Extract emails from contacts for unique check
				emails := make([]string, len(tt.contacts))
				for i, c := range tt.contacts {
					emails[i] = c.Email
				}
				expectedCount := len(uniqueStrings(emails))
				if len(signers) != expectedCount {
					t.Errorf("expected %d signers, got %d", expectedCount, len(signers))
				}
			}
		})
	}
}

func TestExpectedSignerRepository_ListWithStatusByDocID(t *testing.T) {
	testDB := SetupTestDB(t)
	sigRepo := NewSignatureRepository(testDB.DB, testDB.TenantProvider)
	expectedRepo := NewExpectedSignerRepository(testDB.DB, testDB.TenantProvider)
	factory := NewSignatureFactory()
	ctx := context.Background()

	// Setup test data
	clearExpectedSignersTable(t, testDB)
	testDB.ClearTable(t)

	docID := "doc-status-test"
	emails := []string{"signed@example.com", "pending@example.com"}

	// Add expected signers
	err := expectedRepo.AddExpected(ctx, docID, emailsToContacts(emails), "admin@example.com")
	if err != nil {
		t.Fatalf("failed to add expected signers: %v", err)
	}

	// Add a signature for one of them
	sig := factory.CreateSignatureWithDocAndUser(docID, "user-signed", "signed@example.com")
	err = sigRepo.Create(ctx, sig)
	if err != nil {
		t.Fatalf("failed to create signature: %v", err)
	}

	// Test ListWithStatusByDocID
	signers, err := expectedRepo.ListWithStatusByDocID(ctx, docID)
	if err != nil {
		t.Fatalf("failed to list signers with status: %v", err)
	}

	if len(signers) != 2 {
		t.Fatalf("expected 2 signers, got %d", len(signers))
	}

	// Check that one has signed and one hasn't
	signedCount := 0
	pendingCount := 0
	for _, s := range signers {
		if s.HasSigned {
			signedCount++
			if s.SignedAt == nil {
				t.Error("signed signer should have signed_at timestamp")
			}
		} else {
			pendingCount++
			if s.SignedAt != nil {
				t.Error("pending signer should not have signed_at timestamp")
			}
		}
	}

	if signedCount != 1 {
		t.Errorf("expected 1 signed, got %d", signedCount)
	}
	if pendingCount != 1 {
		t.Errorf("expected 1 pending, got %d", pendingCount)
	}
}

func TestExpectedSignerRepository_GetStats(t *testing.T) {
	testDB := SetupTestDB(t)
	sigRepo := NewSignatureRepository(testDB.DB, testDB.TenantProvider)
	expectedRepo := NewExpectedSignerRepository(testDB.DB, testDB.TenantProvider)
	factory := NewSignatureFactory()
	ctx := context.Background()

	// Setup test data
	clearExpectedSignersTable(t, testDB)
	testDB.ClearTable(t)

	docID := "doc-stats-test"
	emails := []string{
		"user1@example.com",
		"user2@example.com",
		"user3@example.com",
		"user4@example.com",
	}

	// Add expected signers
	err := expectedRepo.AddExpected(ctx, docID, emailsToContacts(emails), "admin@example.com")
	if err != nil {
		t.Fatalf("failed to add expected signers: %v", err)
	}

	// Add signatures for 2 out of 4
	sig1 := factory.CreateSignatureWithDocAndUser(docID, "sub1", "user1@example.com")
	sig2 := factory.CreateSignatureWithDocAndUser(docID, "sub2", "user2@example.com")

	if err := sigRepo.Create(ctx, sig1); err != nil {
		t.Fatalf("failed to create sig1: %v", err)
	}
	if err := sigRepo.Create(ctx, sig2); err != nil {
		t.Fatalf("failed to create sig2: %v", err)
	}

	// Get stats
	stats, err := expectedRepo.GetStats(ctx, docID)
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}

	// Verify stats
	if stats.DocID != docID {
		t.Errorf("expected doc_id %s, got %s", docID, stats.DocID)
	}
	if stats.ExpectedCount != 4 {
		t.Errorf("expected ExpectedCount 4, got %d", stats.ExpectedCount)
	}
	if stats.SignedCount != 2 {
		t.Errorf("expected SignedCount 2, got %d", stats.SignedCount)
	}
	if stats.PendingCount != 2 {
		t.Errorf("expected PendingCount 2, got %d", stats.PendingCount)
	}
	expectedRate := 50.0
	if stats.CompletionRate != expectedRate {
		t.Errorf("expected CompletionRate %.2f, got %.2f", expectedRate, stats.CompletionRate)
	}
	if stats.TotalSignatureCount != 2 {
		t.Errorf("expected TotalSignatureCount 2, got %d", stats.TotalSignatureCount)
	}
}

func TestExpectedSignerRepository_GetStats_WithUnexpectedSigners(t *testing.T) {
	testDB := SetupTestDB(t)
	sigRepo := NewSignatureRepository(testDB.DB, testDB.TenantProvider)
	expectedRepo := NewExpectedSignerRepository(testDB.DB, testDB.TenantProvider)
	factory := NewSignatureFactory()
	ctx := context.Background()

	clearExpectedSignersTable(t, testDB)
	testDB.ClearTable(t)

	docID := "doc-stats-unexpected"

	// Add 2 expected signers
	err := expectedRepo.AddExpected(ctx, docID, emailsToContacts([]string{
		"expected1@example.com",
		"expected2@example.com",
	}), "admin@example.com")
	if err != nil {
		t.Fatalf("failed to add expected signers: %v", err)
	}

	// 1 expected signer signs
	sig1 := factory.CreateSignatureWithDocAndUser(docID, "sub1", "expected1@example.com")
	if err := sigRepo.Create(ctx, sig1); err != nil {
		t.Fatalf("failed to create sig1: %v", err)
	}

	// 2 unexpected signers sign
	sig2 := factory.CreateSignatureWithDocAndUser(docID, "sub-extra1", "extra1@example.com")
	sig3 := factory.CreateSignatureWithDocAndUser(docID, "sub-extra2", "extra2@example.com")
	if err := sigRepo.Create(ctx, sig2); err != nil {
		t.Fatalf("failed to create sig2: %v", err)
	}
	if err := sigRepo.Create(ctx, sig3); err != nil {
		t.Fatalf("failed to create sig3: %v", err)
	}

	stats, err := expectedRepo.GetStats(ctx, docID)
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}

	if stats.ExpectedCount != 2 {
		t.Errorf("expected ExpectedCount 2, got %d", stats.ExpectedCount)
	}
	if stats.SignedCount != 1 {
		t.Errorf("expected SignedCount 1, got %d", stats.SignedCount)
	}
	if stats.TotalSignatureCount != 3 {
		t.Errorf("expected TotalSignatureCount 3, got %d", stats.TotalSignatureCount)
	}
	if stats.PendingCount != 1 {
		t.Errorf("expected PendingCount 1, got %d", stats.PendingCount)
	}
}

func TestExpectedSignerRepository_GetStats_NoExpectedSigners(t *testing.T) {
	testDB := SetupTestDB(t)
	sigRepo := NewSignatureRepository(testDB.DB, testDB.TenantProvider)
	expectedRepo := NewExpectedSignerRepository(testDB.DB, testDB.TenantProvider)
	factory := NewSignatureFactory()
	ctx := context.Background()

	clearExpectedSignersTable(t, testDB)
	testDB.ClearTable(t)

	docID := "doc-stats-no-expected"

	// No expected signers, but 2 signatures exist
	sig1 := factory.CreateSignatureWithDocAndUser(docID, "sub1", "user1@example.com")
	sig2 := factory.CreateSignatureWithDocAndUser(docID, "sub2", "user2@example.com")
	if err := sigRepo.Create(ctx, sig1); err != nil {
		t.Fatalf("failed to create sig1: %v", err)
	}
	if err := sigRepo.Create(ctx, sig2); err != nil {
		t.Fatalf("failed to create sig2: %v", err)
	}

	stats, err := expectedRepo.GetStats(ctx, docID)
	if err != nil {
		t.Fatalf("failed to get stats: %v", err)
	}

	if stats.ExpectedCount != 0 {
		t.Errorf("expected ExpectedCount 0, got %d", stats.ExpectedCount)
	}
	if stats.SignedCount != 0 {
		t.Errorf("expected SignedCount 0, got %d", stats.SignedCount)
	}
	if stats.TotalSignatureCount != 2 {
		t.Errorf("expected TotalSignatureCount 2, got %d", stats.TotalSignatureCount)
	}
}

func TestExpectedSignerRepository_Remove(t *testing.T) {
	testDB := SetupTestDB(t)
	repo := NewExpectedSignerRepository(testDB.DB, testDB.TenantProvider)
	ctx := context.Background()

	// Setup
	clearExpectedSignersTable(t, testDB)
	docID := "doc-remove-test"
	emails := []string{"user1@example.com", "user2@example.com"}
	err := repo.AddExpected(ctx, docID, emailsToContacts(emails), "admin@example.com")
	if err != nil {
		t.Fatalf("failed to add expected signers: %v", err)
	}

	// Remove one
	err = repo.Remove(ctx, docID, "user1@example.com")
	if err != nil {
		t.Fatalf("failed to remove signer: %v", err)
	}

	// Verify only one remains
	signers, err := repo.ListByDocID(ctx, docID)
	if err != nil {
		t.Fatalf("failed to list signers: %v", err)
	}

	if len(signers) != 1 {
		t.Errorf("expected 1 signer remaining, got %d", len(signers))
	}
	if signers[0].Email != "user2@example.com" {
		t.Errorf("expected user2@example.com to remain, got %s", signers[0].Email)
	}

	// Try removing non-existent should error
	err = repo.Remove(ctx, docID, "nonexistent@example.com")
	if err == nil {
		t.Error("expected error when removing non-existent signer")
	}
}

// Helper functions

func clearExpectedSignersTable(t *testing.T, testDB *TestDB) {
	t.Helper()
	_, err := testDB.DB.Exec("TRUNCATE TABLE reminder_logs, expected_signers RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("failed to clear expected_signers table: %v", err)
	}
}

// Helper function to convert emails to ContactInfo
func emailsToContacts(emails []string) []models.ContactInfo {
	contacts := make([]models.ContactInfo, len(emails))
	for i, email := range emails {
		contacts[i] = models.ContactInfo{Email: email, Name: ""}
	}
	return contacts
}

func uniqueStrings(slice []string) []string {
	seen := make(map[string]bool)
	result := []string{}
	for _, v := range slice {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}
