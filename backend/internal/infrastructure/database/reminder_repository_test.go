//go:build integration

package database

import (
	"context"
	"testing"

	"github.com/kolapsis/ackify/backend/pkg/models"
)

func TestReminderRepository_Basic_Integration(t *testing.T) {
	testDB := SetupTestDB(t)

	// We need documents and expected_signers tables
	_, err := testDB.DB.Exec(`
		CREATE TABLE IF NOT EXISTS documents (
			doc_id TEXT PRIMARY KEY,
			title TEXT NOT NULL DEFAULT '',
			url TEXT NOT NULL DEFAULT '',
			checksum TEXT NOT NULL DEFAULT '',
			checksum_algorithm TEXT NOT NULL DEFAULT 'SHA-256',
			description TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			created_by TEXT NOT NULL DEFAULT ''
		);
		
		CREATE TABLE IF NOT EXISTS expected_signers (
			id BIGSERIAL PRIMARY KEY,
			doc_id TEXT NOT NULL,
			email TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			added_by TEXT NOT NULL,
			notes TEXT,
			UNIQUE (doc_id, email),
			FOREIGN KEY (doc_id) REFERENCES documents(doc_id) ON DELETE CASCADE
		);
		
		CREATE TABLE IF NOT EXISTS reminder_logs (
			id BIGSERIAL PRIMARY KEY,
			doc_id TEXT NOT NULL,
			recipient_email TEXT NOT NULL,
			sent_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			sent_by TEXT NOT NULL,
			template_used TEXT NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('sent', 'failed', 'bounced')),
			error_message TEXT,
			FOREIGN KEY (doc_id, recipient_email) REFERENCES expected_signers(doc_id, email) ON DELETE CASCADE
		);
	`)
	if err != nil {
		t.Fatalf("Failed to create tables: %v", err)
	}

	ctx := context.Background()
	repo := NewReminderRepository(testDB.DB, testDB.TenantProvider)

	// Create a document and expected signer
	_, err = testDB.DB.Exec(`
		INSERT INTO documents (doc_id, title, created_by) VALUES ('doc1', 'Test', 'admin@test.com');
		INSERT INTO expected_signers (doc_id, email, added_by) VALUES ('doc1', 'user@test.com', 'admin@test.com');
	`)
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	// Test logging a reminder
	log := &models.ReminderLog{
		DocID:          "doc1",
		RecipientEmail: "user@test.com",
		SentBy:         "admin@test.com",
		TemplateUsed:   "test_template",
		Status:         "sent",
	}

	err = repo.LogReminder(ctx, log)
	if err != nil {
		t.Fatalf("LogReminder failed: %v", err)
	}

	// Test getting reminder history
	history, err := repo.GetReminderHistory(ctx, "doc1")
	if err != nil {
		t.Fatalf("GetReminderHistory failed: %v", err)
	}

	if len(history) != 1 {
		t.Errorf("Expected 1 reminder in history, got %d", len(history))
	}
}
