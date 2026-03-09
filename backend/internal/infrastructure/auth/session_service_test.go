// SPDX-License-Identifier: AGPL-3.0-or-later
package auth

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/kolapsis/ackify/backend/pkg/models"
)

var errSessionNotFound = errors.New("session not found")

// mockSessionRepository implements SessionRepository for testing
type mockSessionRepository struct {
	sessions map[string]*models.OAuthSession
}

func newMockSessionRepository() *mockSessionRepository {
	return &mockSessionRepository{
		sessions: make(map[string]*models.OAuthSession),
	}
}

func (m *mockSessionRepository) Create(ctx context.Context, session *models.OAuthSession) error {
	m.sessions[session.SessionID] = session
	return nil
}

func (m *mockSessionRepository) GetBySessionID(ctx context.Context, sessionID string) (*models.OAuthSession, error) {
	session, ok := m.sessions[sessionID]
	if !ok {
		return nil, errSessionNotFound
	}
	return session, nil
}

func (m *mockSessionRepository) UpdateRefreshToken(ctx context.Context, sessionID string, encryptedToken []byte, expiresAt time.Time) error {
	if session, ok := m.sessions[sessionID]; ok {
		session.RefreshTokenEncrypted = encryptedToken
		session.AccessTokenExpiresAt = expiresAt
		return nil
	}
	return errSessionNotFound
}

func (m *mockSessionRepository) DeleteBySessionID(ctx context.Context, sessionID string) error {
	delete(m.sessions, sessionID)
	return nil
}

func (m *mockSessionRepository) DeleteExpired(ctx context.Context, olderThan time.Duration) (int64, error) {
	return 0, nil
}

func TestSessionService_SetUser_GetUser(t *testing.T) {
	config := SessionServiceConfig{
		CookieSecret:  []byte("32-byte-secret-for-secure-cookies"),
		SecureCookies: false, // Use false for testing (no HTTPS)
	}
	service := NewSessionService(config)

	testUser := &models.User{
		Sub:   "test-user-123",
		Email: "test@example.com",
		Name:  "Test User",
	}

	// Test SetUser
	t.Run("SetUser", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)

		err := service.SetUser(rec, req, testUser)
		if err != nil {
			t.Fatalf("SetUser() failed: %v", err)
		}

		// Check that cookie was set
		cookies := rec.Result().Cookies()
		if len(cookies) == 0 {
			t.Fatal("No cookies were set")
		}

		foundSessionCookie := false
		for _, cookie := range cookies {
			if cookie.Name == sessionName {
				foundSessionCookie = true
				if cookie.Path != "/" {
					t.Errorf("Cookie path = %v, expected /", cookie.Path)
				}
				if cookie.HttpOnly != true {
					t.Error("Cookie should be HttpOnly")
				}
			}
		}

		if !foundSessionCookie {
			t.Errorf("Session cookie %s not found", sessionName)
		}
	})

	// Test GetUser with valid session
	t.Run("GetUser with valid session", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)

		// First set the user
		err := service.SetUser(rec, req, testUser)
		if err != nil {
			t.Fatalf("SetUser() failed: %v", err)
		}

		// Create a new request with the session cookie
		req2 := httptest.NewRequest("GET", "/", nil)
		for _, cookie := range rec.Result().Cookies() {
			req2.AddCookie(cookie)
		}

		// Now get the user
		user, err := service.GetUser(req2)
		if err != nil {
			t.Fatalf("GetUser() failed: %v", err)
		}

		if user == nil {
			t.Fatal("GetUser() returned nil user")
		}

		if user.Sub != testUser.Sub {
			t.Errorf("user.Sub = %v, expected %v", user.Sub, testUser.Sub)
		}
		if user.Email != testUser.Email {
			t.Errorf("user.Email = %v, expected %v", user.Email, testUser.Email)
		}
		if user.Name != testUser.Name {
			t.Errorf("user.Name = %v, expected %v", user.Name, testUser.Name)
		}
	})

	// Test GetUser without session
	t.Run("GetUser without session", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)

		user, err := service.GetUser(req)
		if err == nil {
			t.Error("GetUser() should return error without session")
		}
		if user != nil {
			t.Error("GetUser() should return nil user without session")
		}
	})
}

func TestSessionService_Logout(t *testing.T) {
	config := SessionServiceConfig{
		CookieSecret:  []byte("32-byte-secret-for-secure-cookies"),
		SecureCookies: false,
	}
	service := NewSessionService(config)

	testUser := &models.User{
		Sub:   "test-user-123",
		Email: "test@example.com",
		Name:  "Test User",
	}

	// Create session
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)

	err := service.SetUser(rec, req, testUser)
	if err != nil {
		t.Fatalf("SetUser() failed: %v", err)
	}

	// Create request with session cookie
	req2 := httptest.NewRequest("GET", "/", nil)
	for _, cookie := range rec.Result().Cookies() {
		req2.AddCookie(cookie)
	}

	// Verify user exists before logout
	user, err := service.GetUser(req2)
	if err != nil {
		t.Fatalf("GetUser() before logout failed: %v", err)
	}
	if user == nil {
		t.Fatal("User should exist before logout")
	}

	// Logout
	rec2 := httptest.NewRecorder()
	service.Logout(rec2, req2)

	// Create request with expired session cookie
	req3 := httptest.NewRequest("GET", "/", nil)
	for _, cookie := range rec2.Result().Cookies() {
		req3.AddCookie(cookie)
	}

	// Verify user is gone after logout
	user, err = service.GetUser(req3)
	if err == nil {
		t.Error("GetUser() after logout should return error")
	}
	if user != nil {
		t.Error("User should be nil after logout")
	}
}

func TestSessionService_GetSession(t *testing.T) {
	config := SessionServiceConfig{
		CookieSecret:  []byte("32-byte-secret-for-secure-cookies"),
		SecureCookies: false,
	}
	service := NewSessionService(config)

	req := httptest.NewRequest("GET", "/", nil)

	session, err := service.GetSession(req)
	if err != nil {
		t.Fatalf("GetSession() failed: %v", err)
	}

	if session == nil {
		t.Fatal("GetSession() returned nil session")
	}

	// Test that we can store arbitrary data
	session.Values["test_key"] = "test_value"

	if val, ok := session.Values["test_key"].(string); !ok || val != "test_value" {
		t.Error("Failed to store and retrieve value from session")
	}
}

func TestSessionService_GetNewSession(t *testing.T) {
	config := SessionServiceConfig{
		CookieSecret:  []byte("32-byte-secret-for-secure-cookies"),
		SecureCookies: false,
	}
	service := NewSessionService(config)

	req := httptest.NewRequest("GET", "/", nil)

	session, err := service.GetNewSession(req)
	if err != nil {
		t.Fatalf("GetNewSession() failed: %v", err)
	}

	if session == nil {
		t.Fatal("GetNewSession() returned nil session")
	}

	if !session.IsNew {
		t.Error("GetNewSession() should return a new session")
	}
}

func TestSessionService_StoreRefreshToken(t *testing.T) {
	mockRepo := newMockSessionRepository()
	config := SessionServiceConfig{
		CookieSecret:  []byte("32-byte-secret-for-secure-cookies"),
		SecureCookies: false,
		SessionRepo:   mockRepo,
	}
	service := NewSessionService(config)

	testUser := &models.User{
		Sub:   "test-user-123",
		Email: "test@example.com",
		Name:  "Test User",
	}

	testToken := &oauth2.Token{
		AccessToken:  "access-token-123",
		RefreshToken: "refresh-token-456",
		Expiry:       time.Now().Add(1 * time.Hour),
	}

	t.Run("successful storage", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)

		err := service.StoreRefreshToken(context.Background(), rec, req, testToken, testUser)
		if err != nil {
			t.Fatalf("StoreRefreshToken() failed: %v", err)
		}

		// Verify session was created in repository
		if len(mockRepo.sessions) == 0 {
			t.Error("No sessions were created in repository")
		}

		// Verify session contains encrypted refresh token
		var session *models.OAuthSession
		for _, s := range mockRepo.sessions {
			if s.UserSub == testUser.Sub {
				session = s
				break
			}
		}

		if session == nil {
			t.Fatal("Session not found in repository")
		}

		if len(session.RefreshTokenEncrypted) == 0 {
			t.Error("Refresh token was not encrypted and stored")
		}

		if session.SessionID == "" {
			t.Error("Session ID should not be empty")
		}

		if session.AccessTokenExpiresAt.IsZero() {
			t.Error("Access token expiry should be set")
		}
	})

	t.Run("without repository", func(t *testing.T) {
		configNoRepo := SessionServiceConfig{
			CookieSecret:  []byte("32-byte-secret-for-secure-cookies"),
			SecureCookies: false,
			SessionRepo:   nil,
		}
		serviceNoRepo := NewSessionService(configNoRepo)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/", nil)

		err := serviceNoRepo.StoreRefreshToken(context.Background(), rec, req, testToken, testUser)
		if err == nil {
			t.Error("StoreRefreshToken() should fail without repository")
		}
	})
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name          string
		remoteAddr    string
		xForwardedFor string
		xRealIP       string
		expectedIP    string
	}{
		{
			name:       "from RemoteAddr",
			remoteAddr: "192.168.1.100:12345",
			expectedIP: "192.168.1.100:12345",
		},
		{
			name:       "from X-Real-IP",
			remoteAddr: "192.168.1.100:12345",
			xRealIP:    "203.0.113.45",
			expectedIP: "203.0.113.45",
		},
		{
			name:          "from X-Forwarded-For single",
			remoteAddr:    "192.168.1.100:12345",
			xForwardedFor: "203.0.113.45",
			expectedIP:    "203.0.113.45",
		},
		{
			name:          "from X-Forwarded-For multiple",
			remoteAddr:    "192.168.1.100:12345",
			xForwardedFor: "203.0.113.45, 198.51.100.67, 192.0.2.123",
			expectedIP:    "203.0.113.45",
		},
		{
			name:          "X-Forwarded-For takes precedence over X-Real-IP",
			remoteAddr:    "192.168.1.100:12345",
			xForwardedFor: "203.0.113.45",
			xRealIP:       "198.51.100.67",
			expectedIP:    "203.0.113.45",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}
			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}

			ip := getClientIP(req)
			if ip != tt.expectedIP {
				t.Errorf("getClientIP() = %v, expected %v", ip, tt.expectedIP)
			}
		})
	}
}
