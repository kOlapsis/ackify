// SPDX-License-Identifier: AGPL-3.0-or-later
package users

import (
	"net/http"

	"github.com/kolapsis/ackify/backend/internal/presentation/api/shared"
	"github.com/kolapsis/ackify/backend/pkg/providers"
)

// Handler handles user API requests
type Handler struct {
	authorizer providers.Authorizer
}

// NewHandler creates a new users handler
func NewHandler(authorizer providers.Authorizer) *Handler {
	return &Handler{
		authorizer: authorizer,
	}
}

// UserDTO represents a user data transfer object
type UserDTO struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture,omitempty"`
	IsAdmin bool   `json:"isAdmin"`
}

// HandleGetCurrentUser handles GET /api/v1/users/me
func (h *Handler) HandleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	user, ok := shared.GetUserFromContext(r.Context())
	if !ok {
		shared.WriteUnauthorized(w, "")
		return
	}

	userDTO := UserDTO{
		ID:      user.Sub,
		Email:   user.Email,
		Name:    user.Name,
		Picture: user.Picture,
		IsAdmin: h.authorizer.IsAdmin(r.Context(), user.Email),
	}

	shared.WriteJSON(w, http.StatusOK, userDTO)
}
