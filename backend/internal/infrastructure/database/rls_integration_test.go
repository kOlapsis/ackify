//go:build integration

// SPDX-License-Identifier: AGPL-3.0-or-later
package database

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kolapsis/ackify/backend/internal/infrastructure/tenant"
	"github.com/kolapsis/ackify/backend/pkg/models"
)

// isSuperuser checks if the current database connection is a superuser.
// Superusers bypass RLS policies, so isolation tests will fail when connected as superuser.
func isSuperuser(db *sql.DB) bool {
	var isSuperuser bool
	err := db.QueryRow("SELECT usesuper FROM pg_user WHERE usename = current_user").Scan(&isSuperuser)
	if err != nil {
		return true // Assume superuser on error
	}
	return isSuperuser
}

// skipIfSuperuser skips the test if connected as a superuser.
// RLS policies are only enforced for non-superuser roles.
func skipIfSuperuser(t *testing.T, db *sql.DB) {
	t.Helper()
	if isSuperuser(db) {
		t.Skip("Skipping RLS isolation test: connected as superuser (RLS is bypassed). Use ackify_app role to test RLS enforcement.")
	}
}

// TestRLS_TenantIsolation verifies that data inserted by one tenant
// cannot be accessed by another tenant when using RLS.
// NOTE: This test requires a non-superuser connection to verify RLS enforcement.
func TestRLS_TenantIsolation(t *testing.T) {
	testDB := SetupTestDB(t)
	skipIfSuperuser(t, testDB.DB)
	ctx := context.Background()

	// Get the default tenant ID from the test setup
	tenantA, err := testDB.TenantProvider.CurrentTenant(ctx)
	if err != nil {
		t.Fatalf("Failed to get tenant A ID: %v", err)
	}

	// Create a different tenant ID for isolation testing
	tenantB := uuid.New()

	docRepo := NewDocumentRepository(testDB.DB, testDB.TenantProvider)
	sigRepo := NewSignatureRepository(testDB.DB, testDB.TenantProvider)

	// Create a document with tenant A
	docID := "doc-tenant-a-" + uuid.New().String()[:8]
	docInput := models.DocumentInput{
		Title:             "Document A",
		URL:               "https://example.com/doc-a",
		Checksum:          "checksum-a",
		ChecksumAlgorithm: "SHA-256",
		Description:       "Test document for tenant A",
	}

	var docA *models.Document
	err = tenant.WithTenantContext(ctx, testDB.DB, tenantA, func(txCtx context.Context) error {
		var createErr error
		docA, createErr = docRepo.Create(txCtx, docID, docInput, "user-a@example.com")
		return createErr
	})
	if err != nil {
		t.Fatalf("Failed to create document with tenant A: %v", err)
	}

	// Create a signature with tenant A
	sigA := &models.Signature{
		DocID:       docA.DocID,
		UserSub:     "user-sub-a",
		UserEmail:   "user-a@example.com",
		UserName:    "User A",
		SignedAtUTC: time.Now().UTC(),
		PayloadHash: "cGF5bG9hZC1oYXNoLWE=",
		Signature:   "c2lnbmF0dXJlLWE=",
		Nonce:       "nonce-a-" + uuid.New().String()[:8],
	}

	err = tenant.WithTenantContext(ctx, testDB.DB, tenantA, func(txCtx context.Context) error {
		return sigRepo.Create(txCtx, sigA)
	})
	if err != nil {
		t.Fatalf("Failed to create signature with tenant A: %v", err)
	}

	// Verify tenant A can access its own data
	t.Run("tenant_A_can_access_own_data", func(t *testing.T) {
		var doc *models.Document
		var signatures []*models.Signature

		err := tenant.WithTenantContext(ctx, testDB.DB, tenantA, func(txCtx context.Context) error {
			var fetchErr error
			doc, fetchErr = docRepo.GetByDocID(txCtx, docA.DocID)
			if fetchErr != nil {
				return fetchErr
			}

			signatures, fetchErr = sigRepo.GetByDoc(txCtx, docA.DocID)
			return fetchErr
		})

		if err != nil {
			t.Errorf("Tenant A should be able to access its own data: %v", err)
		}

		if doc == nil {
			t.Error("Tenant A should see its document")
		}

		if len(signatures) != 1 {
			t.Errorf("Tenant A should see 1 signature, got %d", len(signatures))
		}
	})

	// Verify tenant B cannot access tenant A's data
	t.Run("tenant_B_cannot_access_tenant_A_data", func(t *testing.T) {
		var doc *models.Document
		var signatures []*models.Signature

		err := tenant.WithTenantContext(ctx, testDB.DB, tenantB, func(txCtx context.Context) error {
			var fetchErr error
			doc, fetchErr = docRepo.GetByDocID(txCtx, docA.DocID)
			if fetchErr != nil && fetchErr != models.ErrDocumentNotFound {
				return fetchErr
			}

			signatures, fetchErr = sigRepo.GetByDoc(txCtx, docA.DocID)
			return fetchErr
		})

		// Should not return an error, just no data
		if err != nil && err != models.ErrDocumentNotFound {
			t.Errorf("Unexpected error for tenant B: %v", err)
		}

		if doc != nil {
			t.Error("Tenant B should NOT see tenant A's document")
		}

		if len(signatures) != 0 {
			t.Errorf("Tenant B should see 0 signatures from tenant A, got %d", len(signatures))
		}
	})
}

// TestRLS_DocumentIsolation tests document isolation specifically.
// NOTE: This test requires a non-superuser connection to verify RLS enforcement.
func TestRLS_DocumentIsolation(t *testing.T) {
	testDB := SetupTestDB(t)
	skipIfSuperuser(t, testDB.DB)
	ctx := context.Background()

	tenantA, _ := testDB.TenantProvider.CurrentTenant(ctx)
	tenantB := uuid.New()

	docRepo := NewDocumentRepository(testDB.DB, testDB.TenantProvider)

	// Create documents for tenant A
	docsToCreate := []struct {
		docID string
		input models.DocumentInput
	}{
		{
			docID: "rls-doc-1-" + uuid.New().String()[:8],
			input: models.DocumentInput{
				Title:             "Doc 1",
				URL:               "https://example.com/1",
				Checksum:          "checksum1",
				ChecksumAlgorithm: "SHA-256",
			},
		},
		{
			docID: "rls-doc-2-" + uuid.New().String()[:8],
			input: models.DocumentInput{
				Title:             "Doc 2",
				URL:               "https://example.com/2",
				Checksum:          "checksum2",
				ChecksumAlgorithm: "SHA-256",
			},
		},
	}

	for _, doc := range docsToCreate {
		err := tenant.WithTenantContext(ctx, testDB.DB, tenantA, func(txCtx context.Context) error {
			_, createErr := docRepo.Create(txCtx, doc.docID, doc.input, "admin@a.com")
			return createErr
		})
		if err != nil {
			t.Fatalf("Failed to create document: %v", err)
		}
	}

	// Verify tenant A sees both documents
	t.Run("tenant_A_sees_all_its_documents", func(t *testing.T) {
		var docs []*models.Document

		err := tenant.WithTenantContext(ctx, testDB.DB, tenantA, func(txCtx context.Context) error {
			var fetchErr error
			docs, fetchErr = docRepo.List(txCtx, 100, 0)
			return fetchErr
		})

		if err != nil {
			t.Fatalf("Failed to list documents for tenant A: %v", err)
		}

		if len(docs) < 2 {
			t.Errorf("Tenant A should see at least 2 documents, got %d", len(docs))
		}
	})

	// Verify tenant B sees no documents from tenant A
	t.Run("tenant_B_sees_no_documents", func(t *testing.T) {
		var docs []*models.Document

		err := tenant.WithTenantContext(ctx, testDB.DB, tenantB, func(txCtx context.Context) error {
			var fetchErr error
			docs, fetchErr = docRepo.List(txCtx, 100, 0)
			return fetchErr
		})

		if err != nil {
			t.Fatalf("Failed to list documents for tenant B: %v", err)
		}

		if len(docs) != 0 {
			t.Errorf("Tenant B should see 0 documents, got %d", len(docs))
		}
	})
}

// TestRLS_SignatureIsolation tests signature isolation specifically.
// NOTE: This test requires a non-superuser connection to verify RLS enforcement.
func TestRLS_SignatureIsolation(t *testing.T) {
	testDB := SetupTestDB(t)
	skipIfSuperuser(t, testDB.DB)
	ctx := context.Background()

	tenantA, _ := testDB.TenantProvider.CurrentTenant(ctx)
	tenantB := uuid.New()

	docRepo := NewDocumentRepository(testDB.DB, testDB.TenantProvider)
	sigRepo := NewSignatureRepository(testDB.DB, testDB.TenantProvider)

	// Create document for tenant A
	docID := "rls-sig-test-" + uuid.New().String()[:8]
	docInput := models.DocumentInput{
		Title:             "Signature Test Doc",
		URL:               "https://example.com/sig-test",
		Checksum:          "checksum-sig",
		ChecksumAlgorithm: "SHA-256",
	}

	err := tenant.WithTenantContext(ctx, testDB.DB, tenantA, func(txCtx context.Context) error {
		_, createErr := docRepo.Create(txCtx, docID, docInput, "admin@a.com")
		return createErr
	})
	if err != nil {
		t.Fatalf("Failed to create document: %v", err)
	}

	// Create signatures for tenant A
	userSub := "rls-user-" + uuid.New().String()[:8]
	sig := &models.Signature{
		DocID:       docID,
		UserSub:     userSub,
		UserEmail:   "signer@a.com",
		UserName:    "Signer A",
		SignedAtUTC: time.Now().UTC(),
		PayloadHash: "cGF5bG9hZC1oYXNo",
		Signature:   "c2lnbmF0dXJl",
		Nonce:       "nonce-" + uuid.New().String()[:8],
	}

	err = tenant.WithTenantContext(ctx, testDB.DB, tenantA, func(txCtx context.Context) error {
		return sigRepo.Create(txCtx, sig)
	})
	if err != nil {
		t.Fatalf("Failed to create signature: %v", err)
	}

	// Tenant A can get signature by doc and user
	t.Run("tenant_A_can_get_signature", func(t *testing.T) {
		var fetchedSig *models.Signature

		err := tenant.WithTenantContext(ctx, testDB.DB, tenantA, func(txCtx context.Context) error {
			var fetchErr error
			fetchedSig, fetchErr = sigRepo.GetByDocAndUser(txCtx, docID, userSub)
			return fetchErr
		})

		if err != nil {
			t.Errorf("Tenant A should be able to get its signature: %v", err)
		}

		if fetchedSig == nil {
			t.Error("Tenant A should see its signature")
		}
	})

	// Tenant B cannot get tenant A's signature
	t.Run("tenant_B_cannot_get_signature", func(t *testing.T) {
		var fetchedSig *models.Signature

		err := tenant.WithTenantContext(ctx, testDB.DB, tenantB, func(txCtx context.Context) error {
			var fetchErr error
			fetchedSig, fetchErr = sigRepo.GetByDocAndUser(txCtx, docID, userSub)
			if fetchErr == models.ErrSignatureNotFound {
				return nil // Expected
			}
			return fetchErr
		})

		if err != nil {
			t.Errorf("Unexpected error for tenant B: %v", err)
		}

		if fetchedSig != nil {
			t.Error("Tenant B should NOT see tenant A's signature")
		}
	})

	// Tenant A can check signature status
	t.Run("tenant_A_can_check_signature_status", func(t *testing.T) {
		var hasSigned bool

		err := tenant.WithTenantContext(ctx, testDB.DB, tenantA, func(txCtx context.Context) error {
			var checkErr error
			hasSigned, checkErr = sigRepo.CheckUserSignatureStatus(txCtx, docID, userSub)
			return checkErr
		})

		if err != nil {
			t.Errorf("Tenant A should be able to check signature status: %v", err)
		}

		if !hasSigned {
			t.Error("Tenant A should see that user has signed")
		}
	})

	// Tenant B gets false for signature status check
	t.Run("tenant_B_signature_status_is_false", func(t *testing.T) {
		var hasSigned bool

		err := tenant.WithTenantContext(ctx, testDB.DB, tenantB, func(txCtx context.Context) error {
			var checkErr error
			hasSigned, checkErr = sigRepo.CheckUserSignatureStatus(txCtx, docID, userSub)
			return checkErr
		})

		if err != nil {
			t.Errorf("Unexpected error for tenant B: %v", err)
		}

		if hasSigned {
			t.Error("Tenant B should NOT see tenant A's signature status")
		}
	})
}

// TestRLS_TransactionCommitRollback tests that RLS transactions properly commit and rollback
func TestRLS_TransactionCommitRollback(t *testing.T) {
	testDB := SetupTestDB(t)
	ctx := context.Background()

	tenantID, _ := testDB.TenantProvider.CurrentTenant(ctx)
	docRepo := NewDocumentRepository(testDB.DB, testDB.TenantProvider)

	t.Run("successful_transaction_commits", func(t *testing.T) {
		docID := "rls-commit-test-" + uuid.New().String()[:8]
		docInput := models.DocumentInput{
			Title:             "Commit Test",
			URL:               "https://example.com/commit",
			Checksum:          "checksum",
			ChecksumAlgorithm: "SHA-256",
		}

		// Create document in transaction
		err := tenant.WithTenantContext(ctx, testDB.DB, tenantID, func(txCtx context.Context) error {
			_, createErr := docRepo.Create(txCtx, docID, docInput, "admin@test.com")
			return createErr
		})
		if err != nil {
			t.Fatalf("Failed to create document: %v", err)
		}

		// Verify document exists after commit
		var doc *models.Document
		err = tenant.WithTenantContext(ctx, testDB.DB, tenantID, func(txCtx context.Context) error {
			var fetchErr error
			doc, fetchErr = docRepo.GetByDocID(txCtx, docID)
			return fetchErr
		})

		if err != nil {
			t.Errorf("Should be able to fetch document after commit: %v", err)
		}

		if doc == nil {
			t.Error("Document should exist after commit")
		}
	})

	t.Run("failed_transaction_rollbacks", func(t *testing.T) {
		docID := "rls-rollback-test-" + uuid.New().String()[:8]
		docInput := models.DocumentInput{
			Title:             "Rollback Test",
			URL:               "https://example.com/rollback",
			Checksum:          "checksum",
			ChecksumAlgorithm: "SHA-256",
		}

		// Attempt to create document but return an error to trigger rollback
		err := tenant.WithTenantContext(ctx, testDB.DB, tenantID, func(txCtx context.Context) error {
			_, createErr := docRepo.Create(txCtx, docID, docInput, "admin@test.com")
			if createErr != nil {
				return createErr
			}
			// Return error to trigger rollback
			return models.ErrDocumentNotFound
		})

		if err == nil {
			t.Fatal("Expected error to be returned")
		}

		// Verify document does NOT exist after rollback
		var doc *models.Document
		err = tenant.WithTenantContext(ctx, testDB.DB, tenantID, func(txCtx context.Context) error {
			var fetchErr error
			doc, fetchErr = docRepo.GetByDocID(txCtx, docID)
			if fetchErr == models.ErrDocumentNotFound {
				return nil // Expected
			}
			return fetchErr
		})

		if err != nil {
			t.Errorf("Unexpected error when checking for rolled back document: %v", err)
		}

		if doc != nil {
			t.Error("Document should NOT exist after rollback")
		}
	})
}

// TestRLS_ExpectedSignersIsolation tests expected signers isolation.
// NOTE: This test requires a non-superuser connection to verify RLS enforcement.
func TestRLS_ExpectedSignersIsolation(t *testing.T) {
	testDB := SetupTestDB(t)
	skipIfSuperuser(t, testDB.DB)
	ctx := context.Background()

	tenantA, _ := testDB.TenantProvider.CurrentTenant(ctx)
	tenantB := uuid.New()

	docRepo := NewDocumentRepository(testDB.DB, testDB.TenantProvider)
	signerRepo := NewExpectedSignerRepository(testDB.DB, testDB.TenantProvider)

	// Create document for tenant A
	docID := "rls-signer-test-" + uuid.New().String()[:8]
	docInput := models.DocumentInput{
		Title:             "Expected Signers Test",
		URL:               "https://example.com/signers",
		Checksum:          "checksum",
		ChecksumAlgorithm: "SHA-256",
	}

	err := tenant.WithTenantContext(ctx, testDB.DB, tenantA, func(txCtx context.Context) error {
		_, createErr := docRepo.Create(txCtx, docID, docInput, "admin@a.com")
		return createErr
	})
	if err != nil {
		t.Fatalf("Failed to create document: %v", err)
	}

	// Add expected signers for tenant A using AddExpected
	contacts := []models.ContactInfo{
		{Name: "Signer One", Email: "signer1@a.com"},
		{Name: "Signer Two", Email: "signer2@a.com"},
	}

	err = tenant.WithTenantContext(ctx, testDB.DB, tenantA, func(txCtx context.Context) error {
		return signerRepo.AddExpected(txCtx, docID, contacts, "admin@a.com")
	})
	if err != nil {
		t.Fatalf("Failed to add expected signers: %v", err)
	}

	// Tenant A can see expected signers
	t.Run("tenant_A_sees_expected_signers", func(t *testing.T) {
		var fetchedSigners []*models.ExpectedSigner

		err := tenant.WithTenantContext(ctx, testDB.DB, tenantA, func(txCtx context.Context) error {
			var fetchErr error
			fetchedSigners, fetchErr = signerRepo.ListByDocID(txCtx, docID)
			return fetchErr
		})

		if err != nil {
			t.Errorf("Tenant A should be able to get expected signers: %v", err)
		}

		if len(fetchedSigners) != 2 {
			t.Errorf("Tenant A should see 2 expected signers, got %d", len(fetchedSigners))
		}
	})

	// Tenant B cannot see tenant A's expected signers
	t.Run("tenant_B_cannot_see_expected_signers", func(t *testing.T) {
		var fetchedSigners []*models.ExpectedSigner

		err := tenant.WithTenantContext(ctx, testDB.DB, tenantB, func(txCtx context.Context) error {
			var fetchErr error
			fetchedSigners, fetchErr = signerRepo.ListByDocID(txCtx, docID)
			return fetchErr
		})

		if err != nil {
			t.Errorf("Unexpected error for tenant B: %v", err)
		}

		if len(fetchedSigners) != 0 {
			t.Errorf("Tenant B should see 0 expected signers, got %d", len(fetchedSigners))
		}
	})
}

// TestRLS_WebhookIsolation tests webhook isolation.
// NOTE: This test requires a non-superuser connection to verify RLS enforcement.
func TestRLS_WebhookIsolation(t *testing.T) {
	testDB := SetupTestDB(t)
	skipIfSuperuser(t, testDB.DB)
	ctx := context.Background()

	tenantA, _ := testDB.TenantProvider.CurrentTenant(ctx)
	tenantB := uuid.New()

	webhookRepo := NewWebhookRepository(testDB.DB, testDB.TenantProvider)

	// Create webhook for tenant A
	var webhookID int64
	err := tenant.WithTenantContext(ctx, testDB.DB, tenantA, func(txCtx context.Context) error {
		wh, createErr := webhookRepo.Create(txCtx, models.WebhookInput{
			Title:       "Webhook A",
			TargetURL:   "https://hooks.example.com/a",
			Secret:      "secret-a",
			Description: "Webhook for tenant A",
			Active:      true,
			Events:      []string{"document.signed"},
			CreatedBy:   "admin@a.com",
		})
		if createErr != nil {
			return createErr
		}
		webhookID = wh.ID
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to create webhook: %v", err)
	}

	// Tenant A can see its webhook
	t.Run("tenant_A_sees_webhook", func(t *testing.T) {
		var webhook *models.Webhook

		err := tenant.WithTenantContext(ctx, testDB.DB, tenantA, func(txCtx context.Context) error {
			var fetchErr error
			webhook, fetchErr = webhookRepo.GetByID(txCtx, webhookID)
			return fetchErr
		})

		if err != nil {
			t.Errorf("Tenant A should be able to get its webhook: %v", err)
		}

		if webhook == nil {
			t.Error("Tenant A should see its webhook")
		}
	})

	// Tenant B cannot see tenant A's webhook (will get sql.ErrNoRows)
	t.Run("tenant_B_cannot_see_webhook", func(t *testing.T) {
		var webhook *models.Webhook

		err := tenant.WithTenantContext(ctx, testDB.DB, tenantB, func(txCtx context.Context) error {
			var fetchErr error
			webhook, fetchErr = webhookRepo.GetByID(txCtx, webhookID)
			// sql.ErrNoRows is expected, ignore it
			if fetchErr != nil {
				return nil // Expected: no rows
			}
			return nil
		})

		if err != nil {
			t.Errorf("Unexpected error for tenant B: %v", err)
		}

		if webhook != nil {
			t.Error("Tenant B should NOT see tenant A's webhook")
		}
	})

	// Tenant B sees empty list
	t.Run("tenant_B_sees_empty_webhook_list", func(t *testing.T) {
		var webhooks []*models.Webhook

		err := tenant.WithTenantContext(ctx, testDB.DB, tenantB, func(txCtx context.Context) error {
			var fetchErr error
			webhooks, fetchErr = webhookRepo.List(txCtx, 100, 0)
			return fetchErr
		})

		if err != nil {
			t.Errorf("Unexpected error for tenant B: %v", err)
		}

		if len(webhooks) != 0 {
			t.Errorf("Tenant B should see 0 webhooks, got %d", len(webhooks))
		}
	})
}
