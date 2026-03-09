// SPDX-License-Identifier: AGPL-3.0-or-later
package services

import (
	"context"
	"testing"

	"github.com/kolapsis/ackify/backend/pkg/models"
)

type fakeWebhookRepo struct {
	hooks []*models.Webhook
	err   error
}

func (f *fakeWebhookRepo) ListActiveByEvent(_ context.Context, _ string) ([]*models.Webhook, error) {
	return f.hooks, f.err
}

type fakeDeliveryRepo struct{ inputs []models.WebhookDeliveryInput }

func (f *fakeDeliveryRepo) Enqueue(_ context.Context, in models.WebhookDeliveryInput) (*models.WebhookDelivery, error) {
	f.inputs = append(f.inputs, in)
	return &models.WebhookDelivery{ID: int64(len(f.inputs)), WebhookID: in.WebhookID, EventType: in.EventType, EventID: in.EventID}, nil
}

func TestWebhookPublisher_Publish(t *testing.T) {
	hooks := []*models.Webhook{{ID: 1, Active: true, Events: []string{"document.created"}}, {ID: 2, Active: true, Events: []string{"document.created"}}}
	repo := &fakeWebhookRepo{hooks: hooks}
	drepo := &fakeDeliveryRepo{}
	p := NewWebhookPublisher(repo, drepo)
	payload := map[string]interface{}{"doc_id": "abc123", "title": "Title"}
	if err := p.Publish(context.Background(), "document.created", payload); err != nil {
		t.Fatalf("Publish error: %v", err)
	}
	if len(drepo.inputs) != 2 {
		t.Fatalf("expected 2 enqueues, got %d", len(drepo.inputs))
	}
	// ensure event type propagated
	if drepo.inputs[0].EventType != "document.created" || drepo.inputs[1].EventType != "document.created" {
		t.Errorf("unexpected event types: %#v", drepo.inputs)
	}
	// ensure payload reference equality not required, but keys exist
	for _, in := range drepo.inputs {
		if in.Payload["doc_id"] != "abc123" {
			t.Error("payload doc_id mismatch")
		}
	}
}
