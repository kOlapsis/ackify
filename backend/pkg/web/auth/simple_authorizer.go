// SPDX-License-Identifier: AGPL-3.0-or-later
package auth

import (
	"context"
	"strings"

	"github.com/kolapsis/ackify/backend/pkg/models"
	"github.com/kolapsis/ackify/backend/pkg/providers"
)

// ConfigProvider provides access to general configuration.
type ConfigProvider interface {
	GetConfig() *models.MutableConfig
}

// SimpleAuthorizer is an authorization implementation based on a list of admin emails.
// This is the default authorizer for Community Edition.
type SimpleAuthorizer struct {
	adminEmails    map[string]bool
	configProvider ConfigProvider
}

// NewSimpleAuthorizer creates a new simple authorizer.
func NewSimpleAuthorizer(adminEmails []string, configProvider ConfigProvider) *SimpleAuthorizer {
	emailMap := make(map[string]bool, len(adminEmails))
	for _, email := range adminEmails {
		normalized := strings.ToLower(strings.TrimSpace(email))
		if normalized != "" {
			emailMap[normalized] = true
		}
	}
	return &SimpleAuthorizer{
		adminEmails:    emailMap,
		configProvider: configProvider,
	}
}

// IsAdmin implements providers.Authorizer.
func (a *SimpleAuthorizer) IsAdmin(_ context.Context, userEmail string) bool {
	normalized := strings.ToLower(strings.TrimSpace(userEmail))
	return a.adminEmails[normalized]
}

// CanCreateDocument implements providers.Authorizer.
func (a *SimpleAuthorizer) CanCreateDocument(ctx context.Context, userEmail string) bool {
	if a.IsAdmin(ctx, userEmail) {
		return true
	}
	cfg := a.configProvider.GetConfig()
	if cfg.General.OnlyAdminCanCreate {
		return false
	}
	if domain := cfg.General.OrganisationDomain; domain != "" {
		normalized := strings.ToLower(strings.TrimSpace(userEmail))
		return strings.HasSuffix(normalized, "@"+strings.ToLower(domain))
	}
	return true
}

// CanManageDocument implements providers.Authorizer.
func (a *SimpleAuthorizer) CanManageDocument(ctx context.Context, userEmail, docCreatedBy string) bool {
	if a.IsAdmin(ctx, userEmail) {
		return true
	}
	normalized := strings.ToLower(strings.TrimSpace(userEmail))
	normalizedCreator := strings.ToLower(strings.TrimSpace(docCreatedBy))
	return normalized != "" && normalized == normalizedCreator
}

// Compile-time interface check.
var _ providers.Authorizer = (*SimpleAuthorizer)(nil)
