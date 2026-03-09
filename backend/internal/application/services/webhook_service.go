// SPDX-License-Identifier: AGPL-3.0-or-later
package services

import (
	"context"

	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/models"
)

// webhookRepository defines webhook storage operations
type webhookRepository interface {
	Create(ctx context.Context, input models.WebhookInput) (*models.Webhook, error)
	Update(ctx context.Context, id int64, input models.WebhookInput) (*models.Webhook, error)
	SetActive(ctx context.Context, id int64, active bool) error
	Delete(ctx context.Context, id int64) error
	GetByID(ctx context.Context, id int64) (*models.Webhook, error)
	List(ctx context.Context, limit, offset int) ([]*models.Webhook, error)
	ListActiveByEvent(ctx context.Context, event string) ([]*models.Webhook, error)
	Count(ctx context.Context) (int, error)
}

// webhookDeliveryRepository defines webhook delivery operations
type webhookDeliveryRepository interface {
	Enqueue(ctx context.Context, input models.WebhookDeliveryInput) (*models.WebhookDelivery, error)
	ListByWebhook(ctx context.Context, webhookID int64, limit, offset int) ([]*models.WebhookDelivery, error)
}

// WebhookService handles webhook management and delivery operations
type WebhookService struct {
	webhookRepo  webhookRepository
	deliveryRepo webhookDeliveryRepository
}

// NewWebhookService creates a new webhook service
func NewWebhookService(webhookRepo webhookRepository, deliveryRepo webhookDeliveryRepository) *WebhookService {
	return &WebhookService{
		webhookRepo:  webhookRepo,
		deliveryRepo: deliveryRepo,
	}
}

// CreateWebhook creates a new webhook
func (s *WebhookService) CreateWebhook(ctx context.Context, input models.WebhookInput) (*models.Webhook, error) {
	logger.Logger.Info("Creating webhook", "title", input.Title, "target_url", input.TargetURL)
	return s.webhookRepo.Create(ctx, input)
}

// UpdateWebhook updates an existing webhook
func (s *WebhookService) UpdateWebhook(ctx context.Context, id int64, input models.WebhookInput) (*models.Webhook, error) {
	logger.Logger.Info("Updating webhook", "id", id, "title", input.Title)
	return s.webhookRepo.Update(ctx, id, input)
}

// SetWebhookActive enables or disables a webhook
func (s *WebhookService) SetWebhookActive(ctx context.Context, id int64, active bool) error {
	logger.Logger.Info("Setting webhook active status", "id", id, "active", active)
	return s.webhookRepo.SetActive(ctx, id, active)
}

// DeleteWebhook deletes a webhook
func (s *WebhookService) DeleteWebhook(ctx context.Context, id int64) error {
	logger.Logger.Info("Deleting webhook", "id", id)
	return s.webhookRepo.Delete(ctx, id)
}

// GetWebhookByID retrieves a webhook by ID
func (s *WebhookService) GetWebhookByID(ctx context.Context, id int64) (*models.Webhook, error) {
	return s.webhookRepo.GetByID(ctx, id)
}

// ListWebhooks retrieves all webhooks with pagination
func (s *WebhookService) ListWebhooks(ctx context.Context, limit, offset int) ([]*models.Webhook, error) {
	return s.webhookRepo.List(ctx, limit, offset)
}

// ListActiveWebhooksByEvent retrieves active webhooks for a specific event
func (s *WebhookService) ListActiveWebhooksByEvent(ctx context.Context, event string) ([]*models.Webhook, error) {
	return s.webhookRepo.ListActiveByEvent(ctx, event)
}

// ListDeliveries retrieves delivery history for a webhook
func (s *WebhookService) ListDeliveries(ctx context.Context, webhookID int64, limit, offset int) ([]*models.WebhookDelivery, error) {
	return s.deliveryRepo.ListByWebhook(ctx, webhookID, limit, offset)
}

// EnqueueDelivery enqueues a webhook delivery
func (s *WebhookService) EnqueueDelivery(ctx context.Context, input models.WebhookDeliveryInput) (*models.WebhookDelivery, error) {
	return s.deliveryRepo.Enqueue(ctx, input)
}

// CountWebhooks returns the total number of webhooks
func (s *WebhookService) CountWebhooks(ctx context.Context) int {
	c, err := s.webhookRepo.Count(ctx)
	if err != nil {
		c = 0
	}
	return c
}
