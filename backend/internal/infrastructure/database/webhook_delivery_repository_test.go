//go:build integration
// +build integration

// SPDX-License-Identifier: AGPL-3.0-or-later
package database

import (
	"context"
	"testing"

	"github.com/kolapsis/ackify/backend/pkg/models"
)

func TestWebhookDeliveryRepository_Enqueue_And_GetNext(t *testing.T) {
	tdb := SetupTestDB(t)
	wrepo := NewWebhookRepository(tdb.DB, tdb.TenantProvider)
	drepo := NewWebhookDeliveryRepository(tdb.DB, tdb.TenantProvider)

	ctx := context.Background()
	wh, err := wrepo.Create(ctx, models.WebhookInput{TargetURL: "https://example.com/hook", Secret: "secret", Active: true, Events: []string{"document.created"}})
	if err != nil {
		t.Fatalf("create webhook err: %v", err)
	}

	in := models.WebhookDeliveryInput{
		WebhookID: wh.ID,
		EventType: "document.created",
		EventID:   "00000000-0000-0000-0000-000000000000",
		Payload:   map[string]interface{}{"doc_id": "ABC"},
	}
	if _, err := drepo.Enqueue(ctx, in); err != nil {
		t.Fatalf("enqueue err: %v", err)
	}

	items, err := drepo.GetNextToProcess(ctx, 10)
	if err != nil {
		t.Fatalf("get next err: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].WebhookID != wh.ID {
		t.Fatal("webhook id mismatch")
	}
}
