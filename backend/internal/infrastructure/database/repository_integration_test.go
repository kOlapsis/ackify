// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build integration

package database

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kolapsis/ackify/backend/pkg/models"
)

func TestRepository_Create_Integration(t *testing.T) {
	testDB := SetupTestDB(t)
	repo := NewSignatureRepository(testDB.DB, testDB.TenantProvider)
	factory := NewSignatureFactory()
	ctx := context.Background()

	tests := []struct {
		name      string
		signature *models.Signature
		wantError bool
	}{
		{
			name:      "create valid signature",
			signature: factory.CreateValidSignature(),
			wantError: false,
		},
		{
			name:      "create minimal signature",
			signature: factory.CreateMinimalSignature(),
			wantError: false,
		},
		{
			name:      "create signature with previous hash",
			signature: factory.CreateChainedSignature("cHJldmlvdXMtaGFzaA=="),
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDB.ClearTable(t)

			err := repo.Create(ctx, tt.signature)

			if tt.wantError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if tt.signature.ID <= 0 {
				t.Error("Expected ID to be set after create")
			}

			if tt.signature.CreatedAt.IsZero() {
				t.Error("Expected CreatedAt to be set after create")
			}

			count := testDB.GetTableCount(t)
			if count != 1 {
				t.Errorf("Expected 1 signature in DB, got %d", count)
			}
		})
	}
}

func TestRepository_Create_UniqueConstraint_Integration(t *testing.T) {
	testDB := SetupTestDB(t)
	repo := NewSignatureRepository(testDB.DB, testDB.TenantProvider)
	factory := NewSignatureFactory()
	ctx := context.Background()

	sig1 := factory.CreateSignatureWithDocAndUser("doc1", "user1", "user1@example.com")
	err := repo.Create(ctx, sig1)
	if err != nil {
		t.Fatalf("Failed to create first signature: %v", err)
	}

	sig2 := factory.CreateSignatureWithDocAndUser("doc1", "user1", "user1@example.com")
	err = repo.Create(ctx, sig2)

	if err == nil {
		t.Error("Expected error for duplicate signature but got none")
	}

	count := testDB.GetTableCount(t)
	if count != 1 {
		t.Errorf("Expected 1 signature in DB after constraint violation, got %d", count)
	}
}

func TestRepository_GetByDocAndUser_Integration(t *testing.T) {
	testDB := SetupTestDB(t)
	repo := NewSignatureRepository(testDB.DB, testDB.TenantProvider)
	factory := NewSignatureFactory()
	ctx := context.Background()

	tests := []struct {
		name      string
		setup     func() *models.Signature
		docID     string
		userSub   string
		wantError bool
		wantNil   bool
	}{
		{
			name: "get existing signature",
			setup: func() *models.Signature {
				sig := factory.CreateSignatureWithDocAndUser("doc1", "user1", "user1@example.com")
				_ = repo.Create(ctx, sig)
				return sig
			},
			docID:     "doc1",
			userSub:   "user1",
			wantError: false,
			wantNil:   false,
		},
		{
			name: "get non-existent signature",
			setup: func() *models.Signature {
				return nil
			},
			docID:     "non-existent",
			userSub:   "non-existent",
			wantError: true,
			wantNil:   true,
		},
		{
			name: "get signature wrong user",
			setup: func() *models.Signature {
				sig := factory.CreateSignatureWithDocAndUser("doc1", "user1", "user1@example.com")
				_ = repo.Create(ctx, sig)
				return sig
			},
			docID:     "doc1",
			userSub:   "wrong-user",
			wantError: true,
			wantNil:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testDB.ClearTable(t)

			var expected *models.Signature
			if tt.setup != nil {
				expected = tt.setup()
			}

			result, err := repo.GetByDocAndUser(ctx, tt.docID, tt.userSub)

			if tt.wantError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if !errors.Is(err, models.ErrSignatureNotFound) && tt.wantNil {
					t.Errorf("Expected ErrSignatureNotFound, got: %v", err)
				}
				if result != nil {
					t.Error("Expected nil result with error")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result == nil {
				t.Fatal("Expected signature but got nil")
			}

			AssertSignatureEqual(t, expected, result)
		})
	}
}

func TestRepository_GetByDoc_Integration(t *testing.T) {
	testDB := SetupTestDB(t)
	repo := NewSignatureRepository(testDB.DB, testDB.TenantProvider)
	factory := NewSignatureFactory()
	ctx := context.Background()

	sig1 := factory.CreateSignatureWithDocAndUser("doc1", "user1", "user1@example.com")
	sig2 := factory.CreateSignatureWithDocAndUser("doc1", "user2", "user2@example.com")
	sig3 := factory.CreateSignatureWithDocAndUser("doc2", "user1", "user1@example.com")

	_ = repo.Create(ctx, sig1)
	time.Sleep(10 * time.Millisecond) // Ensure different created_at
	_ = repo.Create(ctx, sig2)
	time.Sleep(10 * time.Millisecond)
	_ = repo.Create(ctx, sig3)

	tests := []struct {
		name          string
		docID         string
		expectedCount int
		expectedUsers []string
	}{
		{
			name:          "get signatures for doc with 2 users",
			docID:         "doc1",
			expectedCount: 2,
			expectedUsers: []string{"user2", "user1"}, // Should be ordered by created_at DESC
		},
		{
			name:          "get signatures for doc with 1 user",
			docID:         "doc2",
			expectedCount: 1,
			expectedUsers: []string{"user1"},
		},
		{
			name:          "get signatures for non-existent doc",
			docID:         "non-existent",
			expectedCount: 0,
			expectedUsers: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.GetByDoc(ctx, tt.docID)

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if len(result) != tt.expectedCount {
				t.Errorf("Expected %d signatures, got %d", tt.expectedCount, len(result))
			}

			for i, sig := range result {
				if i < len(tt.expectedUsers) && sig.UserSub != tt.expectedUsers[i] {
					t.Errorf("Expected user %s at position %d, got %s", tt.expectedUsers[i], i, sig.UserSub)
				}

				if sig.DocID != tt.docID {
					t.Errorf("Expected DocID %s, got %s", tt.docID, sig.DocID)
				}
			}
		})
	}
}

func TestRepository_GetByUserEmail_Integration(t *testing.T) {
	testDB := SetupTestDB(t)
	repo := NewSignatureRepository(testDB.DB, testDB.TenantProvider)
	factory := NewSignatureFactory()
	ctx := context.Background()

	sig1 := factory.CreateSignatureWithDocAndUser("doc1", "user1", "user1@example.com")
	sig2 := factory.CreateSignatureWithDocAndUser("doc2", "user1", "user1@example.com")
	sig3 := factory.CreateSignatureWithDocAndUser("doc1", "user2", "user2@example.com")

	_ = repo.Create(ctx, sig1)
	time.Sleep(10 * time.Millisecond)
	_ = repo.Create(ctx, sig2)
	time.Sleep(10 * time.Millisecond)
	_ = repo.Create(ctx, sig3)

	tests := []struct {
		name           string
		userEmail      string
		expectedCount  int
		expectedDocIDs []string
	}{
		{
			name:           "get signatures for user with 2 docs",
			userEmail:      "user1@example.com",
			expectedCount:  2,
			expectedDocIDs: []string{"doc2", "doc1"}, // Should be ordered by created_at DESC
		},
		{
			name:           "get signatures for user with 1 doc",
			userEmail:      "user2@example.com",
			expectedCount:  1,
			expectedDocIDs: []string{"doc1"},
		},
		{
			name:           "get signatures for non-existent user",
			userEmail:      "non-existent@example.com",
			expectedCount:  0,
			expectedDocIDs: []string{},
		},
		{
			name:           "get signatures case insensitive",
			userEmail:      "USER1@EXAMPLE.COM",
			expectedCount:  2,
			expectedDocIDs: []string{"doc2", "doc1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.GetByUserEmail(ctx, tt.userEmail)

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if len(result) != tt.expectedCount {
				t.Errorf("Expected %d signatures, got %d", tt.expectedCount, len(result))
			}

			for i, sig := range result {
				if i < len(tt.expectedDocIDs) && sig.DocID != tt.expectedDocIDs[i] {
					t.Errorf("Expected DocID %s at position %d, got %s", tt.expectedDocIDs[i], i, sig.DocID)
				}
			}
		})
	}
}

func TestRepository_CheckUserSignatureStatus_Integration(t *testing.T) {
	testDB := SetupTestDB(t)
	repo := NewSignatureRepository(testDB.DB, testDB.TenantProvider)
	factory := NewSignatureFactory()
	ctx := context.Background()

	sig1 := factory.CreateSignatureWithDocAndUser("doc1", "user-sub-123", "user@EXAMPLE.COM")
	sig2 := factory.CreateSignatureWithDocAndUser("doc2", "another-user", "another@example.com")

	_ = repo.Create(ctx, sig1)
	_ = repo.Create(ctx, sig2)

	tests := []struct {
		name           string
		docID          string
		userIdentifier string
		expected       bool
	}{
		{
			name:           "check by user_sub",
			docID:          "doc1",
			userIdentifier: "user-sub-123",
			expected:       true,
		},
		{
			name:           "check by email (case insensitive)",
			docID:          "doc1",
			userIdentifier: "user@example.com",
			expected:       true,
		},
		{
			name:           "check by email exact case",
			docID:          "doc1",
			userIdentifier: "USER@EXAMPLE.COM",
			expected:       true,
		},
		{
			name:           "non-existent doc",
			docID:          "non-existent",
			userIdentifier: "user-sub-123",
			expected:       false,
		},
		{
			name:           "non-existent user",
			docID:          "doc1",
			userIdentifier: "non-existent",
			expected:       false,
		},
		{
			name:           "wrong doc for user",
			docID:          "doc2",
			userIdentifier: "user-sub-123",
			expected:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := repo.CheckUserSignatureStatus(ctx, tt.docID, tt.userIdentifier)

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestRepository_GetLastSignature_Integration(t *testing.T) {
	testDB := SetupTestDB(t)
	repo := NewSignatureRepository(testDB.DB, testDB.TenantProvider)
	factory := NewSignatureFactory()
	ctx := context.Background()

	t.Run("no signatures", func(t *testing.T) {
		testDB.ClearTable(t)

		result, err := repo.GetLastSignature(ctx, "test-doc-123")

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if result != nil {
			t.Error("Expected nil when no signatures exist")
		}
	})

	t.Run("single signature", func(t *testing.T) {
		testDB.ClearTable(t)

		sig := factory.CreateValidSignature()
		_ = repo.Create(ctx, sig)

		result, err := repo.GetLastSignature(ctx, "test-doc-123")

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if result == nil {
			t.Fatal("Expected signature but got nil")
		}

		AssertSignatureEqual(t, sig, result)
	})

	t.Run("multiple signatures", func(t *testing.T) {
		testDB.ClearTable(t)

		sig1 := factory.CreateSignatureWithUser("user1", "user1@example.com")
		sig2 := factory.CreateSignatureWithUser("user2", "user2@example.com")
		sig3 := factory.CreateSignatureWithUser("user3", "user3@example.com")

		_ = repo.Create(ctx, sig1)
		time.Sleep(10 * time.Millisecond)
		_ = repo.Create(ctx, sig2)
		time.Sleep(10 * time.Millisecond)
		_ = repo.Create(ctx, sig3)

		result, err := repo.GetLastSignature(ctx, "test-doc-123")

		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if result == nil {
			t.Fatal("Expected signature but got nil")
		}

		if result.UserSub != "user3" {
			t.Errorf("Expected last signature to be user3, got %s", result.UserSub)
		}
	})
}
