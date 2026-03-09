// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build integration

package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kolapsis/ackify/backend/pkg/models"
)

func TestRepository_DatabaseConstraints_Integration(t *testing.T) {
	testDB := SetupTestDB(t)
	repo := NewSignatureRepository(testDB.DB, testDB.TenantProvider)
	factory := NewSignatureFactory()
	ctx := context.Background()

	t.Run("unique constraint violation", func(t *testing.T) {
		testDB.ClearTable(t)

		// Create first signature
		sig1 := factory.CreateSignatureWithDocAndUser("doc1", "user1", "user1@example.com")
		err := repo.Create(ctx, sig1)
		if err != nil {
			t.Fatalf("Failed to create first signature: %v", err)
		}

		// Try to create duplicate
		sig2 := factory.CreateSignatureWithDocAndUser("doc1", "user1", "user1@example.com")
		err = repo.Create(ctx, sig2)

		if err == nil {
			t.Fatal("Expected error for unique constraint violation")
		}

		// Verify it's a constraint violation (PostgreSQL specific)
		if !strings.Contains(err.Error(), "duplicate key") &&
			!strings.Contains(err.Error(), "unique constraint") {
			t.Errorf("Expected constraint violation error, got: %v", err)
		}

		// Verify only one record exists
		count := testDB.GetTableCount(t)
		if count != 1 {
			t.Errorf("Expected 1 record after constraint violation, got %d", count)
		}
	})

	t.Run("null constraints", func(t *testing.T) {
		testDB.ClearTable(t)

		tests := []struct {
			name     string
			modifyFn func(*models.Signature)
			wantErr  bool
		}{
			{
				name: "valid signature with nulls",
				modifyFn: func(s *models.Signature) {
					s.UserName = ""
					s.Referer = nil
					s.PrevHash = nil
				},
				wantErr: false,
			},
			{
				name:     "empty doc_id is allowed by DB",
				modifyFn: func(s *models.Signature) { s.DocID = "" },
				wantErr:  false, // Empty string != NULL in PostgreSQL
			},
			{
				name:     "empty user_sub is allowed by DB",
				modifyFn: func(s *models.Signature) { s.UserSub = "" },
				wantErr:  false, // Empty string != NULL in PostgreSQL
			},
			{
				name:     "empty user_email is allowed by DB",
				modifyFn: func(s *models.Signature) { s.UserEmail = "" },
				wantErr:  false, // Empty string != NULL in PostgreSQL
			},
			{
				name:     "empty payload_hash is allowed by DB",
				modifyFn: func(s *models.Signature) { s.PayloadHash = "" },
				wantErr:  false, // Empty string != NULL in PostgreSQL
			},
			{
				name:     "empty signature is allowed by DB",
				modifyFn: func(s *models.Signature) { s.Signature = "" },
				wantErr:  false, // Empty string != NULL in PostgreSQL
			},
			{
				name:     "empty nonce is allowed by DB",
				modifyFn: func(s *models.Signature) { s.Nonce = "" },
				wantErr:  false, // Empty string != NULL in PostgreSQL
			},
		}

		for i, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				sig := factory.CreateSignatureWithDocAndUser(
					fmt.Sprintf("test-doc-%d", i),
					fmt.Sprintf("test-user-%d", i),
					fmt.Sprintf("test%d@example.com", i),
				)
				tt.modifyFn(sig)

				err := repo.Create(ctx, sig)

				if tt.wantErr {
					if err == nil {
						t.Error("Expected error for null constraint violation")
					}
				} else {
					if err != nil {
						t.Errorf("Unexpected error: %v", err)
					}
				}
			})
		}
	})

	t.Run("index performance validation", func(t *testing.T) {
		testDB.ClearTable(t)

		// Create multiple signatures for performance testing
		const numSignatures = 1000
		for i := 0; i < numSignatures; i++ {
			sig := factory.CreateSignatureWithDocAndUser(
				"perf-doc",
				fmt.Sprintf("user-%d", i%100), // Reuse some users
				fmt.Sprintf("user%d@example.com", i),
			)
			_ = repo.Create(ctx, sig)
		}

		// Test indexed queries performance
		start := time.Now()
		_, err := repo.GetByDoc(ctx, "perf-doc")
		duration := time.Since(start)

		if err != nil {
			t.Fatalf("GetByDoc failed: %v", err)
		}

		// Should be fast with index
		if duration > 100*time.Millisecond {
			t.Errorf("GetByDoc too slow: %v (expected < 100ms)", duration)
		}

		t.Logf("GetByDoc for %d signatures took: %v", numSignatures, duration)
	})
}

func TestRepository_Transactions_Integration(t *testing.T) {
	testDB := SetupTestDB(t)
	factory := NewSignatureFactory()
	ctx := context.Background()

	t.Run("transaction rollback on constraint violation", func(t *testing.T) {
		testDB.ClearTable(t)

		// Start transaction
		tx, err := testDB.DB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("Failed to begin transaction: %v", err)
		}
		defer tx.Rollback()

		// Execute operations within transaction context
		// Create first signature
		query := `INSERT INTO signatures (doc_id, user_sub, user_email, signed_at, payload_hash, signature, nonce) 
				 VALUES ($1, $2, $3, $4, $5, $6, $7)`

		_, err = tx.ExecContext(ctx, query, "test-doc", "test-user", "test@example.com",
			time.Now().UTC(), "hash1", "sig1", "nonce1")
		if err != nil {
			t.Fatalf("Failed to create first signature: %v", err)
		}

		// Try to create duplicate - should fail
		_, err = tx.ExecContext(ctx, query, "test-doc", "test-user", "test@example.com",
			time.Now().UTC(), "hash2", "sig2", "nonce2")

		if err == nil {
			t.Error("Expected constraint violation error")
		}

		// Rollback transaction
		err = tx.Rollback()
		if err != nil {
			t.Fatalf("Failed to rollback transaction: %v", err)
		}

		// Verify rollback worked - no signatures should exist
		count := testDB.GetTableCount(t)
		if count != 0 {
			t.Errorf("Expected 0 signatures after rollback, got %d", count)
		}
	})

	t.Run("transaction commit", func(t *testing.T) {
		testDB.ClearTable(t)

		// Get tenant ID for direct SQL insert
		tenantID, err := testDB.TenantProvider.CurrentTenant(ctx)
		if err != nil {
			t.Fatalf("Failed to get tenant ID: %v", err)
		}

		// Start transaction
		tx, err := testDB.DB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("Failed to begin transaction: %v", err)
		}

		// Execute operations within transaction context
		query := `INSERT INTO signatures (tenant_id, doc_id, user_sub, user_email, signed_at, payload_hash, signature, nonce)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

		_, err = tx.ExecContext(ctx, query, tenantID, "test-doc", "test-user", "test@example.com",
			time.Now().UTC(), "hash1", "sig1", "nonce1")
		if err != nil {
			t.Fatalf("Failed to create signature in transaction: %v", err)
		}

		// Commit transaction
		err = tx.Commit()
		if err != nil {
			t.Fatalf("Failed to commit transaction: %v", err)
		}

		// Verify commit worked - signature should exist
		count := testDB.GetTableCount(t)
		if count != 1 {
			t.Errorf("Expected 1 signature after commit, got %d", count)
		}

		// Verify using repository
		repo := NewSignatureRepository(testDB.DB, testDB.TenantProvider)
		result, err := repo.GetByDocAndUser(ctx, "test-doc", "test-user")
		if err != nil {
			t.Fatalf("Failed to get signature after commit: %v", err)
		}
		if result == nil {
			t.Fatal("Expected signature after commit")
		}
	})

	t.Run("isolation levels", func(t *testing.T) {
		testDB.ClearTable(t)

		// Create initial signature
		sig1 := factory.CreateValidSignature()
		mainRepo := NewSignatureRepository(testDB.DB, testDB.TenantProvider)
		_ = mainRepo.Create(ctx, sig1)

		// Start transaction with READ COMMITTED isolation
		tx1, err := testDB.DB.BeginTx(ctx, &sql.TxOptions{
			Isolation: sql.LevelReadCommitted,
		})
		if err != nil {
			t.Fatalf("Failed to begin transaction 1: %v", err)
		}
		defer tx1.Rollback()

		repo1 := NewSignatureRepository(testDB.DB, testDB.TenantProvider)

		// Start another transaction
		tx2, err := testDB.DB.BeginTx(ctx, &sql.TxOptions{
			Isolation: sql.LevelReadCommitted,
		})
		if err != nil {
			t.Fatalf("Failed to begin transaction 2: %v", err)
		}
		defer tx2.Rollback()

		repo2 := NewSignatureRepository(testDB.DB, testDB.TenantProvider)

		// Both transactions should see the initial signature
		result1, err := repo1.GetByDocAndUser(ctx, sig1.DocID, sig1.UserSub)
		if err != nil {
			t.Fatalf("Transaction 1 failed to get signature: %v", err)
		}
		if result1 == nil {
			t.Fatal("Transaction 1 expected signature")
		}

		result2, err := repo2.GetByDocAndUser(ctx, sig1.DocID, sig1.UserSub)
		if err != nil {
			t.Fatalf("Transaction 2 failed to get signature: %v", err)
		}
		if result2 == nil {
			t.Fatal("Transaction 2 expected signature")
		}
	})
}

func TestRepository_DataIntegrity_Integration(t *testing.T) {
	testDB := SetupTestDB(t)
	repo := NewSignatureRepository(testDB.DB, testDB.TenantProvider)
	factory := NewSignatureFactory()
	ctx := context.Background()

	t.Run("timestamp precision", func(t *testing.T) {
		testDB.ClearTable(t)

		// Create signature with specific timestamp
		now := time.Now().UTC()
		sig := factory.CreateValidSignature()
		sig.SignedAtUTC = now

		err := repo.Create(ctx, sig)
		if err != nil {
			t.Fatalf("Failed to create signature: %v", err)
		}

		// Retrieve and verify timestamp precision
		result, err := repo.GetByDocAndUser(ctx, sig.DocID, sig.UserSub)
		if err != nil {
			t.Fatalf("Failed to get signature: %v", err)
		}

		// Check timestamp is preserved (allowing for some precision loss)
		timeDiff := result.SignedAtUTC.Sub(now).Abs()
		if timeDiff > time.Microsecond {
			t.Errorf("Timestamp precision lost: expected %v, got %v (diff: %v)",
				now, result.SignedAtUTC, timeDiff)
		}
	})

	t.Run("string encoding preservation", func(t *testing.T) {
		testDB.ClearTable(t)

		// Test with various string encodings
		sig := factory.CreateValidSignature()
		sig.DocID = "test-éñcode-中文-🎯"
		sig.UserEmail = "tëst@éxample.com"
		sig.PayloadHash = "SGVsbG8gV29ybGQh" // "Hello World!" in base64
		sig.Nonce = "nonce-with-special-chars-αβγ"

		referer := "https://example.com/path/with/émojis🚀?param=value"
		sig.Referer = &referer

		err := repo.Create(ctx, sig)
		if err != nil {
			t.Fatalf("Failed to create signature with special chars: %v", err)
		}

		// Retrieve and verify encoding preservation
		result, err := repo.GetByDocAndUser(ctx, sig.DocID, sig.UserSub)
		if err != nil {
			t.Fatalf("Failed to get signature: %v", err)
		}

		AssertSignatureEqual(t, sig, result)
	})

	t.Run("large data handling", func(t *testing.T) {
		testDB.ClearTable(t)

		// Create signature with large data
		sig := factory.CreateValidSignature()

		// Large base64 strings (simulate large signatures/hashes)
		largeData := strings.Repeat("SGVsbG8gV29ybGQh", 100) // Repeat base64 string
		sig.PayloadHash = largeData
		sig.Signature = largeData

		longReferer := "https://example.com/very/long/path/" + strings.Repeat("segment/", 50)
		sig.Referer = &longReferer

		err := repo.Create(ctx, sig)
		if err != nil {
			t.Fatalf("Failed to create signature with large data: %v", err)
		}

		// Retrieve and verify
		result, err := repo.GetByDocAndUser(ctx, sig.DocID, sig.UserSub)
		if err != nil {
			t.Fatalf("Failed to get signature: %v", err)
		}

		if len(result.PayloadHash) != len(sig.PayloadHash) {
			t.Errorf("PayloadHash length mismatch: expected %d, got %d",
				len(sig.PayloadHash), len(result.PayloadHash))
		}

		if len(*result.Referer) != len(*sig.Referer) {
			t.Errorf("Referer length mismatch: expected %d, got %d",
				len(*sig.Referer), len(*result.Referer))
		}
	})
}

func TestRepository_EdgeCases_Integration(t *testing.T) {
	testDB := SetupTestDB(t)
	repo := NewSignatureRepository(testDB.DB, testDB.TenantProvider)
	factory := NewSignatureFactory()
	ctx := context.Background()

	t.Run("empty string vs null handling", func(t *testing.T) {
		testDB.ClearTable(t)

		// Test with empty strings for nullable fields
		sig := factory.CreateValidSignature()
		emptyString := ""
		sig.UserName = emptyString
		sig.Referer = &emptyString
		sig.PrevHash = &emptyString

		err := repo.Create(ctx, sig)
		if err != nil {
			t.Fatalf("Failed to create signature with empty strings: %v", err)
		}

		result, err := repo.GetByDocAndUser(ctx, sig.DocID, sig.UserSub)
		if err != nil {
			t.Fatalf("Failed to get signature: %v", err)
		}

		// Verify empty strings are preserved (not converted to NULL)
		if result.UserName != "" {
			t.Error("Empty string UserName not preserved")
		}
		if result.Referer == nil || *result.Referer != "" {
			t.Error("Empty string Referer not preserved")
		}
		if result.PrevHash == nil || *result.PrevHash != "" {
			t.Error("Empty string PrevHash not preserved")
		}
	})

	t.Run("boundary values", func(t *testing.T) {
		testDB.ClearTable(t)

		// Test with boundary timestamp values
		sig := factory.CreateValidSignature()

		// Use a very old timestamp
		sig.SignedAtUTC = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)

		err := repo.Create(ctx, sig)
		if err != nil {
			t.Fatalf("Failed to create signature with old timestamp: %v", err)
		}

		result, err := repo.GetByDocAndUser(ctx, sig.DocID, sig.UserSub)
		if err != nil {
			t.Fatalf("Failed to get signature: %v", err)
		}

		if !result.SignedAtUTC.Equal(sig.SignedAtUTC) {
			t.Errorf("Timestamp boundary value not preserved: expected %v, got %v",
				sig.SignedAtUTC, result.SignedAtUTC)
		}
	})

	t.Run("case sensitivity", func(t *testing.T) {
		testDB.ClearTable(t)

		// Create signatures with different case variations
		sig1 := factory.CreateSignatureWithDocAndUser("Doc1", "User1", "USER1@EXAMPLE.COM")
		sig2 := factory.CreateSignatureWithDocAndUser("doc1", "user1", "user1@example.com")

		err1 := repo.Create(ctx, sig1)
		err2 := repo.Create(ctx, sig2)

		if err1 != nil {
			t.Fatalf("Failed to create signature 1: %v", err1)
		}
		if err2 != nil {
			t.Fatalf("Failed to create signature 2: %v", err2)
		}

		// Both should exist as they have different case for doc_id and user_sub
		count := testDB.GetTableCount(t)
		if count != 2 {
			t.Errorf("Expected 2 signatures with different cases, got %d", count)
		}

		// Test CheckUserSignatureStatus with case variations
		exists1, _ := repo.CheckUserSignatureStatus(ctx, "Doc1", "USER1@EXAMPLE.COM")
		exists2, _ := repo.CheckUserSignatureStatus(ctx, "doc1", "user1@example.com")
		exists3, _ := repo.CheckUserSignatureStatus(ctx, "Doc1", "user1@example.com") // Cross-case: different doc case but same email case-insensitive
		exists4, _ := repo.CheckUserSignatureStatus(ctx, "doc1", "USER1@EXAMPLE.COM") // Cross-case: different doc case but same email case-insensitive

		if !exists1 {
			t.Error("Expected to find signature with exact case match")
		}
		if !exists2 {
			t.Error("Expected to find signature with exact case match")
		}
		if !exists3 {
			t.Error("Expected to find signature with case-insensitive email match for Doc1")
		}
		if !exists4 {
			t.Error("Expected to find signature with case-insensitive email match for doc1")
		}

		// Test with non-matching doc_id case
		exists5, _ := repo.CheckUserSignatureStatus(ctx, "DOC1", "user1@example.com") // All caps doc_id should not match
		if exists5 {
			t.Error("Should not find signature with different case for doc_id when no exact match")
		}
	})
}
