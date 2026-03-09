// SPDX-License-Identifier: AGPL-3.0-or-later
package users

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kolapsis/ackify/backend/internal/presentation/api/shared"
	"github.com/kolapsis/ackify/backend/pkg/models"
)

// ============================================================================
// TEST FIXTURES
// ============================================================================

var (
	testUserRegular = &models.User{
		Sub:   "google-oauth2|123456789",
		Email: "user@example.com",
		Name:  "Regular User",
	}

	testUserAdmin = &models.User{
		Sub:   "google-oauth2|987654321",
		Email: "admin@example.com",
		Name:  "Admin User",
	}

	testUserAdminUpperCase = &models.User{
		Sub:   "google-oauth2|111111111",
		Email: "ADMIN@example.com", // Uppercase to test case-insensitive matching
		Name:  "Admin Uppercase",
	}

	testAdminEmails = []string{"admin@example.com", "admin2@example.com"}
)

// mockAuthorizer is a test implementation of authorizer interface
type mockAuthorizer struct {
	adminEmails map[string]bool
}

func newMockAuthorizer(adminEmails []string) *mockAuthorizer {
	emails := make(map[string]bool)
	for _, email := range adminEmails {
		emails[strings.ToLower(email)] = true
	}
	return &mockAuthorizer{adminEmails: emails}
}

func (m *mockAuthorizer) IsAdmin(_ context.Context, email string) bool {
	return m.adminEmails[strings.ToLower(email)]
}

func (m *mockAuthorizer) CanCreateDocument(_ context.Context, email string) bool {
	return m.adminEmails[strings.ToLower(email)]
}

func (m *mockAuthorizer) CanManageDocument(ctx context.Context, userEmail, docCreatedBy string) bool {
	if m.IsAdmin(ctx, userEmail) {
		return true
	}
	return strings.ToLower(userEmail) == strings.ToLower(docCreatedBy)
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

func addUserToContext(ctx context.Context, user *models.User) context.Context {
	return context.WithValue(ctx, shared.ContextKeyUser, user)
}

// ============================================================================
// TESTS
// ============================================================================

func TestNewHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		adminEmails []string
	}{
		{
			name:        "with admin emails",
			adminEmails: []string{"admin@example.com"},
		},
		{
			name:        "with multiple admin emails",
			adminEmails: []string{"admin1@example.com", "admin2@example.com", "admin3@example.com"},
		},
		{
			name:        "with empty admin emails",
			adminEmails: []string{},
		},
		{
			name:        "with nil admin emails",
			adminEmails: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			authorizer := newMockAuthorizer(tt.adminEmails)
			handler := NewHandler(authorizer)

			assert.NotNil(t, handler)
			assert.NotNil(t, handler.authorizer)
		})
	}
}

func TestHandler_HandleGetCurrentUser_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		user            *models.User
		adminEmails     []string
		expectedIsAdmin bool
		expectedID      string
		expectedEmail   string
		expectedName    string
	}{
		{
			name:            "regular user - not admin",
			user:            testUserRegular,
			adminEmails:     testAdminEmails,
			expectedIsAdmin: false,
			expectedID:      "google-oauth2|123456789",
			expectedEmail:   "user@example.com",
			expectedName:    "Regular User",
		},
		{
			name:            "admin user - is admin",
			user:            testUserAdmin,
			adminEmails:     testAdminEmails,
			expectedIsAdmin: true,
			expectedID:      "google-oauth2|987654321",
			expectedEmail:   "admin@example.com",
			expectedName:    "Admin User",
		},
		{
			name:            "admin with uppercase email - case insensitive match",
			user:            testUserAdminUpperCase,
			adminEmails:     testAdminEmails,
			expectedIsAdmin: true,
			expectedID:      "google-oauth2|111111111",
			expectedEmail:   "ADMIN@example.com",
			expectedName:    "Admin Uppercase",
		},
		{
			name:            "user with no admin emails configured",
			user:            testUserRegular,
			adminEmails:     []string{},
			expectedIsAdmin: false,
			expectedID:      "google-oauth2|123456789",
			expectedEmail:   "user@example.com",
			expectedName:    "Regular User",
		},
		{
			name:            "user with different admin email",
			user:            testUserRegular,
			adminEmails:     []string{"different@example.com"},
			expectedIsAdmin: false,
			expectedID:      "google-oauth2|123456789",
			expectedEmail:   "user@example.com",
			expectedName:    "Regular User",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup
			authorizer := newMockAuthorizer(tt.adminEmails)
			handler := NewHandler(authorizer)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
			ctx := addUserToContext(req.Context(), tt.user)
			req = req.WithContext(ctx)

			rec := httptest.NewRecorder()

			// Execute
			handler.HandleGetCurrentUser(rec, req)

			// Assert
			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

			// Parse response
			var wrapper struct {
				Data UserDTO `json:"data"`
			}
			err := json.Unmarshal(rec.Body.Bytes(), &wrapper)
			require.NoError(t, err, "Response should be valid JSON")

			// Validate fields
			assert.Equal(t, tt.expectedID, wrapper.Data.ID)
			assert.Equal(t, tt.expectedEmail, wrapper.Data.Email)
			assert.Equal(t, tt.expectedName, wrapper.Data.Name)
			assert.Equal(t, tt.expectedIsAdmin, wrapper.Data.IsAdmin)
		})
	}
}

func TestHandler_HandleGetCurrentUser_Unauthorized(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setupCtx    func(context.Context) context.Context
		expectedMsg string
	}{
		{
			name: "no user in context",
			setupCtx: func(ctx context.Context) context.Context {
				return ctx // No user added
			},
			expectedMsg: "", // Empty unauthorized message
		},
		{
			name: "nil user in context",
			setupCtx: func(ctx context.Context) context.Context {
				return context.WithValue(ctx, shared.ContextKeyUser, nil)
			},
			expectedMsg: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup
			authorizer := newMockAuthorizer(testAdminEmails)
			handler := NewHandler(authorizer)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
			ctx := tt.setupCtx(req.Context())
			req = req.WithContext(ctx)

			rec := httptest.NewRecorder()

			// Execute
			handler.HandleGetCurrentUser(rec, req)

			// Assert
			assert.Equal(t, http.StatusUnauthorized, rec.Code)

			// Parse error response
			var response map[string]interface{}
			err := json.Unmarshal(rec.Body.Bytes(), &response)
			require.NoError(t, err)

			// Should have error structure
			assert.Contains(t, response, "error")
		})
	}
}

func TestHandler_HandleGetCurrentUser_ResponseFormat(t *testing.T) {
	t.Parallel()

	authorizer := newMockAuthorizer(testAdminEmails)
	handler := NewHandler(authorizer)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	ctx := addUserToContext(req.Context(), testUserRegular)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()

	handler.HandleGetCurrentUser(rec, req)

	// Check Content-Type
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Equal(t, http.StatusOK, rec.Code)

	// Validate JSON structure
	var response map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	// Check wrapper structure
	assert.Contains(t, response, "data")

	// Get data object
	data, ok := response["data"].(map[string]interface{})
	require.True(t, ok, "data should be an object")

	// Check required fields
	assert.Contains(t, data, "id")
	assert.Contains(t, data, "email")
	assert.Contains(t, data, "name")
	assert.Contains(t, data, "isAdmin")

	// Validate field types
	_, ok = data["id"].(string)
	assert.True(t, ok, "id should be a string")

	_, ok = data["email"].(string)
	assert.True(t, ok, "email should be a string")

	_, ok = data["name"].(string)
	assert.True(t, ok, "name should be a string")

	_, ok = data["isAdmin"].(bool)
	assert.True(t, ok, "isAdmin should be a boolean")
}

func TestHandler_HandleGetCurrentUser_AdminEmailCaseInsensitive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		adminEmails   []string
		userEmail     string
		expectedAdmin bool
	}{
		{
			name:          "exact match lowercase",
			adminEmails:   []string{"admin@example.com"},
			userEmail:     "admin@example.com",
			expectedAdmin: true,
		},
		{
			name:          "user uppercase, admin lowercase",
			adminEmails:   []string{"admin@example.com"},
			userEmail:     "ADMIN@EXAMPLE.COM",
			expectedAdmin: true,
		},
		{
			name:          "user lowercase, admin uppercase",
			adminEmails:   []string{"ADMIN@EXAMPLE.COM"},
			userEmail:     "admin@example.com",
			expectedAdmin: true,
		},
		{
			name:          "mixed case both",
			adminEmails:   []string{"Admin@Example.COM"},
			userEmail:     "aDmIn@eXaMpLe.CoM",
			expectedAdmin: true,
		},
		{
			name:          "different email",
			adminEmails:   []string{"admin@example.com"},
			userEmail:     "user@example.com",
			expectedAdmin: false,
		},
		{
			name:          "multiple admins, user matches second",
			adminEmails:   []string{"admin1@example.com", "admin2@example.com"},
			userEmail:     "ADMIN2@EXAMPLE.COM",
			expectedAdmin: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			authorizer := newMockAuthorizer(tt.adminEmails)
			handler := NewHandler(authorizer)

			user := &models.User{
				Sub:   "test-sub",
				Email: tt.userEmail,
				Name:  "Test User",
			}

			req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
			ctx := addUserToContext(req.Context(), user)
			req = req.WithContext(ctx)

			rec := httptest.NewRecorder()

			handler.HandleGetCurrentUser(rec, req)

			var wrapper struct {
				Data UserDTO `json:"data"`
			}
			err := json.Unmarshal(rec.Body.Bytes(), &wrapper)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedAdmin, wrapper.Data.IsAdmin, "Admin status mismatch")
		})
	}
}

func TestHandler_HandleGetCurrentUser_Concurrent(t *testing.T) {
	t.Parallel()

	authorizer := newMockAuthorizer(testAdminEmails)
	handler := NewHandler(authorizer)

	const numRequests = 100
	done := make(chan bool, numRequests)
	errors := make(chan error, numRequests)

	// Spawn concurrent requests
	for i := 0; i < numRequests; i++ {
		go func(id int) {
			defer func() { done <- true }()

			var user *models.User
			if id%2 == 0 {
				user = testUserRegular
			} else {
				user = testUserAdmin
			}

			req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
			ctx := addUserToContext(req.Context(), user)
			req = req.WithContext(ctx)

			rec := httptest.NewRecorder()

			handler.HandleGetCurrentUser(rec, req)

			if rec.Code != http.StatusOK {
				errors <- assert.AnError
			}

			var wrapper struct {
				Data UserDTO `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &wrapper); err != nil {
				errors <- err
			}

			// Validate admin status
			if id%2 == 0 && wrapper.Data.IsAdmin {
				errors <- assert.AnError
			}
			if id%2 != 0 && !wrapper.Data.IsAdmin {
				errors <- assert.AnError
			}
		}(i)
	}

	// Wait for all requests
	for i := 0; i < numRequests; i++ {
		<-done
	}
	close(errors)

	// Check for errors
	var errCount int
	for err := range errors {
		t.Logf("Concurrent request error: %v", err)
		errCount++
	}

	assert.Equal(t, 0, errCount, "All concurrent requests should succeed")
}

func TestHandler_HandleGetCurrentUser_DifferentHTTPMethods(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		method         string
		expectedStatus int
	}{
		{
			name:           "GET method (correct)",
			method:         http.MethodGet,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST method (works but not RESTful)",
			method:         http.MethodPost,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "PUT method",
			method:         http.MethodPut,
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			authorizer := newMockAuthorizer(testAdminEmails)
			handler := NewHandler(authorizer)

			req := httptest.NewRequest(tt.method, "/api/v1/users/me", nil)
			ctx := addUserToContext(req.Context(), testUserRegular)
			req = req.WithContext(ctx)

			rec := httptest.NewRecorder()

			handler.HandleGetCurrentUser(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)
		})
	}
}

func BenchmarkHandler_HandleGetCurrentUser(b *testing.B) {
	authorizer := newMockAuthorizer(testAdminEmails)
	handler := NewHandler(authorizer)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
		ctx := addUserToContext(req.Context(), testUserRegular)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()

		handler.HandleGetCurrentUser(rec, req)
	}
}

func BenchmarkHandler_HandleGetCurrentUser_Parallel(b *testing.B) {
	authorizer := newMockAuthorizer(testAdminEmails)
	handler := NewHandler(authorizer)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
			ctx := addUserToContext(req.Context(), testUserRegular)
			req = req.WithContext(ctx)

			rec := httptest.NewRecorder()

			handler.HandleGetCurrentUser(rec, req)
		}
	})
}
