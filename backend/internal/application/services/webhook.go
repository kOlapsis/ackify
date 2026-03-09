// SPDX-License-Identifier: AGPL-3.0-or-later
package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/models"
)

// Interfaces kept local to application layer
type webhookRepo interface {
	ListActiveByEvent(ctx context.Context, event string) ([]*models.Webhook, error)
}

type webhookDeliveryRepo interface {
	Enqueue(ctx context.Context, input models.WebhookDeliveryInput) (*models.WebhookDelivery, error)
}

// WebhookPublisher publishes events to active webhooks via delivery queue
type WebhookPublisher struct {
	repo       webhookRepo
	deliveries webhookDeliveryRepo
}

func NewWebhookPublisher(repo webhookRepo, deliveries webhookDeliveryRepo) *WebhookPublisher {
	return &WebhookPublisher{repo: repo, deliveries: deliveries}
}

// Publish enqueues deliveries for all webhooks subscribed to the event
func (p *WebhookPublisher) Publish(ctx context.Context, eventType string, payload map[string]interface{}) error {
	logger.Logger.Debug("Publishing event", "event", eventType)
	hooks, err := p.repo.ListActiveByEvent(ctx, eventType)
	if err != nil {
		return fmt.Errorf("failed to list webhooks: %w", err)
	}
	if len(hooks) == 0 {
		return nil
	}

	eventID := newEventID()
	for _, h := range hooks {
		input := models.WebhookDeliveryInput{
			WebhookID:  h.ID,
			EventType:  eventType,
			EventID:    eventID,
			Payload:    payload,
			Priority:   0,
			MaxRetries: 6,
		}
		if _, err := p.deliveries.Enqueue(ctx, input); err != nil {
			logger.Logger.Warn("Failed to enqueue webhook delivery", "webhook_id", h.ID, "error", err.Error())
		}
	}
	return nil
}

func newEventID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	// Format hex with dashes like UUID v4 (not asserting version bits here to avoid extra ops)
	hexStr := hex.EncodeToString(b)
	return hexStr[0:8] + "-" + hexStr[8:12] + "-" + hexStr[12:16] + "-" + hexStr[16:20] + "-" + hexStr[20:32]
}
