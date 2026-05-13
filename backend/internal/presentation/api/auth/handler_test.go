// SPDX-License-Identifier: AGPL-3.0-or-later
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kolapsis/ackify/backend/internal/presentation/api/shared"
	"github.com/kolapsis/ackify/backend/pkg/models"
	"github.com/kolapsis/ackify/backend/pkg/providers"
	"github.com/kolapsis/ackify/backend/pkg/types"
)

// ============================================================================
// TEST FIXTURES
// ============================================================================

const (
	testBaseURL   = "https://example.com"
	testAuthURL   = "https://oauth.example.com/authorize"
	testLogoutURL = "https://oauth.example.com/logout"
)

var testUser = &models.User{
	Sub:   "oauth2|123456789",
	Email: "user@example.com",
	Name:  "Test User",
}

// ============================================================================
// MOCK IMPLEMENTATIONS
// ============================================================================

// mockAuthorizer is a test implementation of providers.Authorizer
type mockAuthorizer struct {
	adminEmails        map[string]bool
	onlyAdminCanCreate bool
}

func newMockAuthorizer(adminEmails []string, onlyAdminCanCreate bool) *mockAuthorizer {
	emails := make(map[string]bool)
	for _, email := range adminEmails {
		emails[strings.ToLower(email)] = true
	}
	return &mockAuthorizer{
		adminEmails:        emails,
		onlyAdminCanCreate: onlyAdminCanCreate,
	}
}

func (m *mockAuthorizer) IsAdmin(_ context.Context, userEmail string) bool {
	return m.adminEmails[strings.ToLower(userEmail)]
}

func (m *mockAuthorizer) CanCreateDocument(_ context.Context, userEmail string) bool {
	if !m.onlyAdminCanCreate {
		return true
	}
	return m.adminEmails[strings.ToLower(userEmail)]
}

func (m *mockAuthorizer) CanManageDocument(ctx context.Context, userEmail, docCreatedBy string) bool {
	if m.IsAdmin(ctx, userEmail) {
		return true
	}
	return strings.ToLower(userEmail) == strings.ToLower(docCreatedBy)
}

// mockAuthProvider is a unified test implementation of providers.AuthProvider
type mockAuthProvider struct {
	mu               sync.RWMutex
	currentUser      *types.User
	oidcEnabled      bool
	magicLinkEnabled bool
	logoutURL        string
}

func newMockAuthProvider() *mockAuthProvider {
	return &mockAuthProvider{
		oidcEnabled:      true,
		magicLinkEnabled: false,
		logoutURL:        testLogoutURL,
	}
}

// Session methods
func (m *mockAuthProvider) GetCurrentUser(_ *http.Request) (*types.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.currentUser == nil {
		return nil, http.ErrNoCookie
	}
	return m.currentUser, nil
}

func (m *mockAuthProvider) SetCurrentUser(_ http.ResponseWriter, _ *http.Request, user *types.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentUser = user
	return nil
}

func (m *mockAuthProvider) Logout(_ http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentUser = nil
}

func (m *mockAuthProvider) IsConfigured() bool {
	return m.oidcEnabled || m.magicLinkEnabled
}

// OIDC methods
func (m *mockAuthProvider) IsOIDCEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.oidcEnabled
}

func (m *mockAuthProvider) StartOIDC(_ http.ResponseWriter, _ *http.Request, nextURL string) string {
	return testAuthURL + "?redirect_uri=" + testBaseURL + "/api/v1/auth/callback&state=test-state&next=" + nextURL
}

func (m *mockAuthProvider) VerifyOIDCState(_ http.ResponseWriter, _ *http.Request, _ string) bool {
	return true
}

func (m *mockAuthProvider) HandleOIDCCallback(_ context.Context, _ http.ResponseWriter, _ *http.Request, _, _ string) (*types.User, string, error) {
	return &types.User{
		Sub:   testUser.Sub,
		Email: testUser.Email,
		Name:  testUser.Name,
	}, "/", nil
}

func (m *mockAuthProvider) GetOIDCLogoutURL() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.logoutURL
}

func (m *mockAuthProvider) IsAllowedDomain(_ string) bool {
	return true
}

// MagicLink methods
func (m *mockAuthProvider) IsMagicLinkEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.magicLinkEnabled
}

func (m *mockAuthProvider) RequestMagicLink(_ context.Context, _, _, _, _, _ string) error {
	return nil
}

func (m *mockAuthProvider) VerifyMagicLink(_ context.Context, _, _, _ string) (*providers.MagicLinkResult, error) {
	return &providers.MagicLinkResult{
		Email:      "test@example.com",
		RedirectTo: "/",
	}, nil
}

func (m *mockAuthProvider) VerifyReminderAuthToken(_ context.Context, _, _, _ string) (*providers.MagicLinkResult, error) {
	return &providers.MagicLinkResult{
		Email:      "test@example.com",
		RedirectTo: "/",
	}, nil
}

func (m *mockAuthProvider) CreateReminderAuthToken(_ context.Context, _, _ string) (string, error) {
	return "test-token", nil
}

// Helper for tests
func (m *mockAuthProvider) setOIDCEnabled(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.oidcEnabled = enabled
}

func (m *mockAuthProvider) setMagicLinkEnabled(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.magicLinkEnabled = enabled
}

func (m *mockAuthProvider) setLogoutURL(url string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logoutURL = url
}

func createTestMiddleware() *shared.Middleware {
	authProvider := newMockAuthProvider()
	authorizer := newMockAuthorizer([]string{}, false)
	return shared.NewMiddleware(authProvider, testBaseURL, authorizer)
}

// ============================================================================
// TESTS - Constructor
// ============================================================================

func TestNewHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
	}{
		{
			name:    "with valid dependencies",
			baseURL: testBaseURL,
		},
		{
			name:    "with empty baseURL",
			baseURL: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			authProvider := newMockAuthProvider()
			middleware := createTestMiddleware()
			handler := NewHandler(authProvider, middleware, tt.baseURL, nil)

			assert.NotNil(t, handler)
			assert.NotNil(t, handler.authProvider)
			assert.NotNil(t, handler.middleware)
			assert.Equal(t, tt.baseURL, handler.baseURL)
		})
	}
}

// ============================================================================
// TESTS - HandleAuthCheck
// ============================================================================

func TestHandler_HandleAuthCheck_Authenticated(t *testing.T) {
	t.Parallel()

	authProvider := newMockAuthProvider()
	authProvider.currentUser = &types.User{
		Sub:   testUser.Sub,
		Email: testUser.Email,
		Name:  testUser.Name,
	}
	handler := NewHandler(authProvider, createTestMiddleware(), testBaseURL, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/check", nil)
	rec := httptest.NewRecorder()

	handler.HandleAuthCheck(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var wrapper struct {
		Data struct {
			Authenticated bool                   `json:"authenticated"`
			User          map[string]interface{} `json:"user"`
		} `json:"data"`
	}
	err := json.Unmarshal(rec.Body.Bytes(), &wrapper)
	require.NoError(t, err, "Response should be valid JSON")

	assert.True(t, wrapper.Data.Authenticated)
	assert.NotNil(t, wrapper.Data.User)
	assert.Equal(t, testUser.Sub, wrapper.Data.User["id"])
	assert.Equal(t, testUser.Email, wrapper.Data.User["email"])
	assert.Equal(t, testUser.Name, wrapper.Data.User["name"])
}

func TestHandler_HandleAuthCheck_NotAuthenticated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setupFunc func(*http.Request) *http.Request
	}{
		{
			name: "no session cookie",
			setupFunc: func(req *http.Request) *http.Request {
				return req
			},
		},
		{
			name: "invalid session cookie",
			setupFunc: func(req *http.Request) *http.Request {
				req.AddCookie(&http.Cookie{
					Name:  "ackapp_session",
					Value: "invalid-cookie-value",
				})
				return req
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := NewHandler(newMockAuthProvider(), createTestMiddleware(), testBaseURL, nil)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/check", nil)
			req = tt.setupFunc(req)
			rec := httptest.NewRecorder()

			handler.HandleAuthCheck(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)

			var wrapper struct {
				Data struct {
					Authenticated bool `json:"authenticated"`
				} `json:"data"`
			}
			err := json.Unmarshal(rec.Body.Bytes(), &wrapper)
			require.NoError(t, err)

			assert.False(t, wrapper.Data.Authenticated)
		})
	}
}

func TestHandler_HandleAuthCheck_ResponseFormat(t *testing.T) {
	t.Parallel()

	handler := NewHandler(newMockAuthProvider(), createTestMiddleware(), testBaseURL, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/check", nil)
	rec := httptest.NewRecorder()

	handler.HandleAuthCheck(rec, req)

	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Contains(t, response, "data")

	data, ok := response["data"].(map[string]interface{})
	require.True(t, ok, "data should be an object")

	assert.Contains(t, data, "authenticated")

	_, ok = data["authenticated"].(bool)
	assert.True(t, ok, "authenticated should be a boolean")
}

// ============================================================================
// TESTS - HandleLogout
// ============================================================================

func TestHandler_HandleLogout_WithSSO(t *testing.T) {
	t.Parallel()

	authProvider := newMockAuthProvider()
	handler := NewHandler(authProvider, createTestMiddleware(), testBaseURL, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/logout", nil)
	rec := httptest.NewRecorder()

	handler.HandleLogout(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var wrapper struct {
		Data struct {
			Message     string `json:"message"`
			RedirectURL string `json:"redirectUrl"`
		} `json:"data"`
	}
	err := json.Unmarshal(rec.Body.Bytes(), &wrapper)
	require.NoError(t, err)

	assert.Equal(t, "Successfully logged out", wrapper.Data.Message)
	assert.Contains(t, wrapper.Data.RedirectURL, testLogoutURL)
	assert.Contains(t, wrapper.Data.RedirectURL, "post_logout_redirect_uri")
	assert.Contains(t, wrapper.Data.RedirectURL, url.QueryEscape(testBaseURL))
}

func TestHandler_HandleLogout_WithoutSSO(t *testing.T) {
	t.Parallel()

	authProvider := newMockAuthProvider()
	authProvider.setLogoutURL("")
	handler := NewHandler(authProvider, createTestMiddleware(), testBaseURL, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/logout", nil)
	rec := httptest.NewRecorder()

	handler.HandleLogout(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var wrapper struct {
		Data struct {
			Message     string `json:"message"`
			RedirectURL string `json:"redirectUrl,omitempty"`
		} `json:"data"`
	}
	err := json.Unmarshal(rec.Body.Bytes(), &wrapper)
	require.NoError(t, err)

	assert.Equal(t, "Successfully logged out", wrapper.Data.Message)
	assert.Empty(t, wrapper.Data.RedirectURL)
}

func TestHandler_HandleLogout_ClearsSession(t *testing.T) {
	t.Parallel()

	authProvider := newMockAuthProvider()
	authProvider.currentUser = &types.User{Sub: "test"}
	handler := NewHandler(authProvider, createTestMiddleware(), testBaseURL, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/logout", nil)
	rec := httptest.NewRecorder()

	handler.HandleLogout(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var wrapper struct {
		Data struct {
			Message     string `json:"message"`
			RedirectURL string `json:"redirectUrl"`
		} `json:"data"`
	}
	err := json.Unmarshal(rec.Body.Bytes(), &wrapper)
	require.NoError(t, err)
	assert.Equal(t, "Successfully logged out", wrapper.Data.Message)

	// Verify session was cleared
	assert.Nil(t, authProvider.currentUser)
}

// ============================================================================
// TESTS - HandleStartOIDC
// ============================================================================

func TestHandler_HandleStartOIDC_WithRedirect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		requestBody map[string]string
		expectedURL string
	}{
		{
			name:        "with custom redirect path",
			requestBody: map[string]string{"redirectTo": "/dashboard"},
			expectedURL: "/dashboard",
		},
		{
			name:        "with root redirect",
			requestBody: map[string]string{"redirectTo": "/"},
			expectedURL: "/",
		},
		{
			name:        "with empty redirect",
			requestBody: map[string]string{"redirectTo": ""},
			expectedURL: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			authProvider := newMockAuthProvider()
			handler := NewHandler(authProvider, createTestMiddleware(), testBaseURL, nil)

			body, err := json.Marshal(tt.requestBody)
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/start", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.HandleStartOIDC(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

			var wrapper struct {
				Data struct {
					RedirectURL string `json:"redirectUrl"`
				} `json:"data"`
			}
			err = json.Unmarshal(rec.Body.Bytes(), &wrapper)
			require.NoError(t, err)

			assert.NotEmpty(t, wrapper.Data.RedirectURL)
			assert.Contains(t, wrapper.Data.RedirectURL, testAuthURL)
		})
	}
}

func TestHandler_HandleStartOIDC_NoBody(t *testing.T) {
	t.Parallel()

	authProvider := newMockAuthProvider()
	handler := NewHandler(authProvider, createTestMiddleware(), testBaseURL, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/start", nil)
	rec := httptest.NewRecorder()

	handler.HandleStartOIDC(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var wrapper struct {
		Data struct {
			RedirectURL string `json:"redirectUrl"`
		} `json:"data"`
	}
	err := json.Unmarshal(rec.Body.Bytes(), &wrapper)
	require.NoError(t, err)

	assert.NotEmpty(t, wrapper.Data.RedirectURL)
	assert.Contains(t, wrapper.Data.RedirectURL, testAuthURL)
}

func TestHandler_HandleStartOIDC_InvalidJSON(t *testing.T) {
	t.Parallel()

	authProvider := newMockAuthProvider()
	handler := NewHandler(authProvider, createTestMiddleware(), testBaseURL, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/start", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.HandleStartOIDC(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var wrapper struct {
		Data struct {
			RedirectURL string `json:"redirectUrl"`
		} `json:"data"`
	}
	err := json.Unmarshal(rec.Body.Bytes(), &wrapper)
	require.NoError(t, err)

	assert.NotEmpty(t, wrapper.Data.RedirectURL)
}

func TestHandler_HandleStartOIDC_Disabled(t *testing.T) {
	t.Parallel()

	authProvider := newMockAuthProvider()
	authProvider.setOIDCEnabled(false)
	handler := NewHandler(authProvider, createTestMiddleware(), testBaseURL, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/start", nil)
	rec := httptest.NewRecorder()

	handler.HandleStartOIDC(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestHandler_HandleStartOIDC_ResponseFormat(t *testing.T) {
	t.Parallel()

	authProvider := newMockAuthProvider()
	handler := NewHandler(authProvider, createTestMiddleware(), testBaseURL, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/start", nil)
	rec := httptest.NewRecorder()

	handler.HandleStartOIDC(rec, req)

	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Contains(t, response, "data")

	data, ok := response["data"].(map[string]interface{})
	require.True(t, ok, "data should be an object")

	assert.Contains(t, data, "redirectUrl")

	redirectURL, ok := data["redirectUrl"].(string)
	assert.True(t, ok, "redirectUrl should be a string")
	assert.NotEmpty(t, redirectURL)
}

// ============================================================================
// TESTS - HandleGetCSRFToken
// ============================================================================

func TestHandler_HandleGetCSRFToken_Success(t *testing.T) {
	t.Parallel()

	handler := NewHandler(newMockAuthProvider(), createTestMiddleware(), testBaseURL, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/csrf", nil)
	rec := httptest.NewRecorder()

	handler.HandleGetCSRFToken(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var wrapper struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	err := json.Unmarshal(rec.Body.Bytes(), &wrapper)
	require.NoError(t, err)

	assert.NotEmpty(t, wrapper.Data.Token)
	assert.Greater(t, len(wrapper.Data.Token), 20, "CSRF token should be sufficiently long")

	cookies := rec.Result().Cookies()
	var csrfCookie *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == shared.CSRFTokenCookie {
			csrfCookie = cookie
			break
		}
	}

	require.NotNil(t, csrfCookie, "CSRF cookie should be set")
	assert.Equal(t, wrapper.Data.Token, csrfCookie.Value)
	assert.Equal(t, "/", csrfCookie.Path)
	assert.False(t, csrfCookie.HttpOnly, "CSRF cookie should be readable by JS")
	assert.Equal(t, http.SameSiteLaxMode, csrfCookie.SameSite)
	assert.Equal(t, 86400, csrfCookie.MaxAge, "CSRF token should have 24h lifetime")
}

func TestHandler_HandleGetCSRFToken_ResponseFormat(t *testing.T) {
	t.Parallel()

	handler := NewHandler(newMockAuthProvider(), createTestMiddleware(), testBaseURL, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/csrf", nil)
	rec := httptest.NewRecorder()

	handler.HandleGetCSRFToken(rec, req)

	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Equal(t, http.StatusOK, rec.Code)

	var response map[string]interface{}
	err := json.Unmarshal(rec.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Contains(t, response, "data")

	data, ok := response["data"].(map[string]interface{})
	require.True(t, ok, "data should be an object")

	assert.Contains(t, data, "token")

	token, ok := data["token"].(string)
	assert.True(t, ok, "token should be a string")
	assert.NotEmpty(t, token)
}

// ============================================================================
// TESTS - Concurrency
// ============================================================================

func TestHandler_HandleAuthCheck_Concurrent(t *testing.T) {
	t.Parallel()

	handler := NewHandler(newMockAuthProvider(), createTestMiddleware(), testBaseURL, nil)

	const numRequests = 100
	done := make(chan bool, numRequests)
	errors := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			defer func() { done <- true }()

			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/check", nil)
			rec := httptest.NewRecorder()

			handler.HandleAuthCheck(rec, req)

			if rec.Code != http.StatusOK {
				errors <- assert.AnError
			}
		}()
	}

	for i := 0; i < numRequests; i++ {
		<-done
	}
	close(errors)

	var errCount int
	for err := range errors {
		t.Logf("Concurrent request error: %v", err)
		errCount++
	}

	assert.Equal(t, 0, errCount, "All concurrent requests should succeed")
}

func TestHandler_HandleLogout_Concurrent(t *testing.T) {
	t.Parallel()

	handler := NewHandler(newMockAuthProvider(), createTestMiddleware(), testBaseURL, nil)

	const numRequests = 100
	done := make(chan bool, numRequests)
	errors := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			defer func() { done <- true }()

			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/logout", nil)
			rec := httptest.NewRecorder()

			handler.HandleLogout(rec, req)

			if rec.Code != http.StatusOK {
				errors <- assert.AnError
			}

			var wrapper struct {
				Data struct {
					Message string `json:"message"`
				} `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &wrapper); err != nil {
				errors <- err
			}
		}()
	}

	for i := 0; i < numRequests; i++ {
		<-done
	}
	close(errors)

	var errCount int
	for err := range errors {
		t.Logf("Concurrent request error: %v", err)
		errCount++
	}

	assert.Equal(t, 0, errCount, "All concurrent requests should succeed")
}

func TestHandler_HandleStartOIDC_Concurrent(t *testing.T) {
	t.Parallel()

	authProvider := newMockAuthProvider()
	handler := NewHandler(authProvider, createTestMiddleware(), testBaseURL, nil)

	const numRequests = 100
	done := make(chan bool, numRequests)
	errors := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			defer func() { done <- true }()

			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/start", nil)
			rec := httptest.NewRecorder()

			handler.HandleStartOIDC(rec, req)

			if rec.Code != http.StatusOK {
				errors <- assert.AnError
			}

			var wrapper struct {
				Data struct {
					RedirectURL string `json:"redirectUrl"`
				} `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &wrapper); err != nil {
				errors <- err
				return
			}

			if wrapper.Data.RedirectURL == "" {
				errors <- assert.AnError
			}
		}()
	}

	for i := 0; i < numRequests; i++ {
		<-done
	}
	close(errors)

	var errCount int
	for err := range errors {
		t.Logf("Concurrent request error: %v", err)
		errCount++
	}

	assert.Equal(t, 0, errCount, "All concurrent requests should succeed")
}

// ============================================================================
// BENCHMARKS
// ============================================================================

func BenchmarkHandler_HandleAuthCheck(b *testing.B) {
	handler := NewHandler(newMockAuthProvider(), createTestMiddleware(), testBaseURL, nil)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/check", nil)
		rec := httptest.NewRecorder()

		handler.HandleAuthCheck(rec, req)
	}
}

func BenchmarkHandler_HandleAuthCheck_Parallel(b *testing.B) {
	handler := NewHandler(newMockAuthProvider(), createTestMiddleware(), testBaseURL, nil)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/check", nil)
			rec := httptest.NewRecorder()

			handler.HandleAuthCheck(rec, req)
		}
	})
}

func BenchmarkHandler_HandleLogout(b *testing.B) {
	handler := NewHandler(newMockAuthProvider(), createTestMiddleware(), testBaseURL, nil)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/logout", nil)
		rec := httptest.NewRecorder()

		handler.HandleLogout(rec, req)
	}
}

func BenchmarkHandler_HandleLogout_Parallel(b *testing.B) {
	handler := NewHandler(newMockAuthProvider(), createTestMiddleware(), testBaseURL, nil)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/logout", nil)
			rec := httptest.NewRecorder()

			handler.HandleLogout(rec, req)
		}
	})
}

func BenchmarkHandler_HandleStartOIDC(b *testing.B) {
	handler := NewHandler(newMockAuthProvider(), createTestMiddleware(), testBaseURL, nil)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/start", nil)
		rec := httptest.NewRecorder()

		handler.HandleStartOIDC(rec, req)
	}
}

func BenchmarkHandler_HandleStartOIDC_Parallel(b *testing.B) {
	handler := NewHandler(newMockAuthProvider(), createTestMiddleware(), testBaseURL, nil)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/start", nil)
			rec := httptest.NewRecorder()

			handler.HandleStartOIDC(rec, req)
		}
	})
}

func BenchmarkHandler_HandleGetCSRFToken(b *testing.B) {
	handler := NewHandler(newMockAuthProvider(), createTestMiddleware(), testBaseURL, nil)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/csrf", nil)
		rec := httptest.NewRecorder()

		handler.HandleGetCSRFToken(rec, req)
	}
}

func BenchmarkHandler_HandleGetCSRFToken_Parallel(b *testing.B) {
	handler := NewHandler(newMockAuthProvider(), createTestMiddleware(), testBaseURL, nil)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/csrf", nil)
			rec := httptest.NewRecorder()

			handler.HandleGetCSRFToken(rec, req)
		}
	})
}
