//go:build integration
// +build integration

// SPDX-License-Identifier: AGPL-3.0-or-later
package database

import (
	"context"
	"testing"

	"github.com/kolapsis/ackify/backend/pkg/models"
)

func TestWebhookRepository_CRUD_And_ListActiveByEvent(t *testing.T) {
	tdb := SetupTestDB(t)
	repo := NewWebhookRepository(tdb.DB, tdb.TenantProvider)

	ctx := context.Background()
	input := models.WebhookInput{
		Title:       "My Webhook",
		TargetURL:   "https://example.com/hook",
		Secret:      "s3cr3t",
		Active:      true,
		Events:      []string{"document.created", "signature.created"},
		Headers:     map[string]string{"X-Test": "1"},
		Description: "Test hook",
		CreatedBy:   "admin@example.com",
	}

	wh, err := repo.Create(ctx, input)
	if err != nil {
		t.Fatalf("create err: %v", err)
	}
	if wh.ID == 0 {
		t.Fatal("expected id")
	}
	if wh.Title != "My Webhook" {
		t.Fatalf("expected title, got %q", wh.Title)
	}

	list, err := repo.ListActiveByEvent(ctx, "document.created")
	if err != nil {
		t.Fatalf("list active err: %v", err)
	}
	if len(list) == 0 {
		t.Fatalf("expected at least one active webhook")
	}

	// Update
	input.Active = false
	wh2, err := repo.Update(ctx, wh.ID, input)
	if err != nil {
		t.Fatalf("update err: %v", err)
	}
	if wh2.Active {
		t.Fatal("expected inactive after update")
	}

	// SetActive
	if err := repo.SetActive(ctx, wh.ID, true); err != nil {
		t.Fatalf("set active err: %v", err)
	}
	got, err := repo.GetByID(ctx, wh.ID)
	if err != nil {
		t.Fatalf("get err: %v", err)
	}
	if !got.Active {
		t.Fatal("expected active true")
	}
}
