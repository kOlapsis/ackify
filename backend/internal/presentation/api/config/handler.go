// SPDX-License-Identifier: AGPL-3.0-or-later
package config

import (
	"net/http"

	"github.com/kolapsis/ackify/backend/internal/presentation/api/shared"
	"github.com/kolapsis/ackify/backend/pkg/models"
)

// configProvider defines the interface for fetching configuration
type configProvider interface {
	GetConfig() *models.MutableConfig
}

// Handler handles public configuration API requests
type Handler struct {
	configProvider configProvider
}

// NewHandler creates a new config handler
func NewHandler(configProvider configProvider) *Handler {
	return &Handler{
		configProvider: configProvider,
	}
}

// Response represents the public configuration exposed to the frontend
type Response struct {
	SMTPEnabled        bool `json:"smtpEnabled"`
	StorageEnabled     bool `json:"storageEnabled"`
	OnlyAdminCanCreate bool `json:"onlyAdminCanCreate"`
	OAuthEnabled       bool `json:"oauthEnabled"`
	MagicLinkEnabled   bool `json:"magicLinkEnabled"`
}

// HandleGetConfig handles GET /api/v1/config
func (h *Handler) HandleGetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := h.configProvider.GetConfig()

	response := Response{
		SMTPEnabled:        cfg.SMTP.IsConfigured(),
		StorageEnabled:     cfg.Storage.IsEnabled(),
		OnlyAdminCanCreate: cfg.General.OnlyAdminCanCreate,
		OAuthEnabled:       cfg.OIDC.Enabled,
		MagicLinkEnabled:   cfg.MagicLink.Enabled,
	}

	shared.WriteJSON(w, http.StatusOK, response)
}
