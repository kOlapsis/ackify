// SPDX-License-Identifier: AGPL-3.0-or-later
package health

import (
	"net/http"
	"time"

	"github.com/kolapsis/ackify/backend/internal/presentation/api/shared"
)

// Handler handles health check requests
type Handler struct{}

// NewHandler creates a new health handler
func NewHandler() *Handler {
	return &Handler{}
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

// HandleHealth handles GET /api/v1/health
func (h *Handler) HandleHealth(w http.ResponseWriter, r *http.Request) {
	response := HealthResponse{
		Status:    "ok",
		Timestamp: time.Now(),
	}

	shared.WriteJSON(w, http.StatusOK, response)
}
