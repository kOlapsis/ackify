// SPDX-License-Identifier: AGPL-3.0-or-later
package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/kolapsis/ackify/backend/pkg/models"
)

// mockDocumentGetter implements DocumentGetter for testing
type mockDocumentGetter struct {
	docs map[string]*models.Document
	err  error
}

func (m *mockDocumentGetter) GetByDocID(ctx context.Context, docID string) (*models.Document, error) {
	if m.err != nil {
		return nil, m.err
	}
	doc, ok := m.docs[docID]
	if !ok {
		return nil, nil
	}
	return doc, nil
}

func TestHandler_MissingParameters(t *testing.T) {
	mock := &mockDocumentGetter{docs: make(map[string]*models.Document)}
	h := NewHandler(mock)
	defer h.Stop()

	tests := []struct {
		name       string
		query      string
		wantStatus int
	}{
		{
			name:       "missing doc parameter",
			query:      "?url=https://example.com/file.pdf",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing url parameter",
			query:      "?doc=test-doc",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty parameters",
			query:      "",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/proxy"+tt.query, nil)
			rec := httptest.NewRecorder()

			h.HandleProxy(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, rec.Code)
			}
		})
	}
}

func TestHandler_InvalidURL(t *testing.T) {
	mock := &mockDocumentGetter{docs: make(map[string]*models.Document)}
	h := NewHandler(mock)
	defer h.Stop()

	tests := []struct {
		name string
		url  string
	}{
		{"invalid scheme", "ftp://example.com/file.pdf"},
		{"localhost", "http://localhost/file.pdf"},
		{"127.0.0.1", "http://127.0.0.1/file.pdf"},
		{"private IP", "http://192.168.1.1/file.pdf"},
		{"no host", "http:///file.pdf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/proxy?doc=test&url="+url.QueryEscape(tt.url), nil)
			rec := httptest.NewRecorder()

			h.HandleProxy(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("Expected status 400, got %d for URL %s", rec.Code, tt.url)
			}
		})
	}
}

func TestHandler_DocumentNotFound(t *testing.T) {
	mock := &mockDocumentGetter{docs: make(map[string]*models.Document)}
	h := NewHandler(mock)
	defer h.Stop()

	req := httptest.NewRequest("GET", "/proxy?doc=nonexistent&url="+url.QueryEscape("https://example.com/file.pdf"), nil)
	rec := httptest.NewRecorder()

	h.HandleProxy(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", rec.Code)
	}
}

func TestHandler_DocumentGetterError(t *testing.T) {
	mock := &mockDocumentGetter{
		docs: make(map[string]*models.Document),
		err:  errors.New("database error"),
	}
	h := NewHandler(mock)
	defer h.Stop()

	req := httptest.NewRequest("GET", "/proxy?doc=test&url="+url.QueryEscape("https://example.com/file.pdf"), nil)
	rec := httptest.NewRecorder()

	h.HandleProxy(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", rec.Code)
	}
}

func TestHandler_URLMismatch(t *testing.T) {
	mock := &mockDocumentGetter{
		docs: map[string]*models.Document{
			"test-doc": {
				DocID: "test-doc",
				URL:   "https://example.com/correct.pdf",
			},
		},
	}
	h := NewHandler(mock)
	defer h.Stop()

	req := httptest.NewRequest("GET", "/proxy?doc=test-doc&url="+url.QueryEscape("https://example.com/wrong.pdf"), nil)
	rec := httptest.NewRecorder()

	h.HandleProxy(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", rec.Code)
	}
}

func TestHandler_RateLimiting(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", "4")
		w.Write([]byte("test"))
	}))
	defer server.Close()

	mock := &mockDocumentGetter{
		docs: map[string]*models.Document{
			"test-doc": {
				DocID: "test-doc",
				URL:   server.URL + "/file.pdf",
			},
		},
	}

	// Create handler with very low rate limit for testing
	h := &Handler{
		documentGetter: mock,
		rateLimiter:    NewRateLimiter(2, 1, 10, time.Minute), // 1 request per IP+doc
		httpClient:     http.DefaultClient,
		allowLocalURLs: true, // Allow local URLs for testing
	}
	defer h.Stop()

	// First request should succeed
	req := httptest.NewRequest("GET", "/proxy?doc=test-doc&url="+url.QueryEscape(server.URL+"/file.pdf"), nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()
	h.HandleProxy(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("First request: expected status 200, got %d", rec.Code)
	}

	// Second request from same IP to same doc should be rate limited
	req = httptest.NewRequest("GET", "/proxy?doc=test-doc&url="+url.QueryEscape(server.URL+"/file.pdf"), nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rec = httptest.NewRecorder()
	h.HandleProxy(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("Second request: expected status 429, got %d", rec.Code)
	}

	// Check Retry-After header
	if rec.Header().Get("Retry-After") == "" {
		t.Error("Expected Retry-After header")
	}
}

func TestHandler_MIMETypeValidation(t *testing.T) {
	tests := []struct {
		contentType string
		allowed     bool
	}{
		{"application/pdf", true},
		{"image/png", true},
		{"image/jpeg", true},
		{"image/gif", true},
		{"image/webp", true},
		{"image/svg+xml", true},
		{"text/html", true},
		{"text/markdown", true},
		{"text/plain", true},
		{"application/javascript", false},
		{"application/octet-stream", false},
		{"application/x-executable", false},
	}

	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				w.Header().Set("Content-Length", "4")
				w.Write([]byte("test"))
			}))
			defer server.Close()

			mock := &mockDocumentGetter{
				docs: map[string]*models.Document{
					"test-doc": {
						DocID: "test-doc",
						URL:   server.URL + "/file",
					},
				},
			}
			h := NewHandler(mock)
			h.SetAllowLocalURLs(true)
			defer h.Stop()

			req := httptest.NewRequest("GET", "/proxy?doc=test-doc&url="+url.QueryEscape(server.URL+"/file"), nil)
			rec := httptest.NewRecorder()

			h.HandleProxy(rec, req)

			if tt.allowed && rec.Code != http.StatusOK {
				t.Errorf("Expected status 200 for allowed MIME %s, got %d", tt.contentType, rec.Code)
			}
			if !tt.allowed && rec.Code != http.StatusForbidden {
				t.Errorf("Expected status 403 for disallowed MIME %s, got %d", tt.contentType, rec.Code)
			}
		})
	}
}

func TestHandler_FileSizeLimit(t *testing.T) {
	// Create a server that reports a large content length
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", "60000000") // 60 MB
		w.Write([]byte("test"))
	}))
	defer server.Close()

	mock := &mockDocumentGetter{
		docs: map[string]*models.Document{
			"test-doc": {
				DocID: "test-doc",
				URL:   server.URL + "/large.pdf",
			},
		},
	}
	h := NewHandler(mock)
	h.SetAllowLocalURLs(true)
	defer h.Stop()

	req := httptest.NewRequest("GET", "/proxy?doc=test-doc&url="+url.QueryEscape(server.URL+"/large.pdf"), nil)
	rec := httptest.NewRecorder()

	h.HandleProxy(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected status 413, got %d", rec.Code)
	}
}

func TestHandler_SecurityHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", "4")
		w.Write([]byte("test"))
	}))
	defer server.Close()

	mock := &mockDocumentGetter{
		docs: map[string]*models.Document{
			"test-doc": {
				DocID: "test-doc",
				URL:   server.URL + "/file.pdf",
			},
		},
	}
	h := NewHandler(mock)
	h.SetAllowLocalURLs(true)
	defer h.Stop()

	req := httptest.NewRequest("GET", "/proxy?doc=test-doc&url="+url.QueryEscape(server.URL+"/file.pdf"), nil)
	rec := httptest.NewRecorder()

	h.HandleProxy(rec, req)

	expectedHeaders := map[string]string{
		"Content-Security-Policy": "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "SAMEORIGIN",
		"Referrer-Policy":         "no-referrer",
		"Cache-Control":           "no-store, no-cache, must-revalidate",
	}

	for header, expected := range expectedHeaders {
		if got := rec.Header().Get(header); got != expected {
			t.Errorf("Header %s: expected %q, got %q", header, expected, got)
		}
	}
}

func TestHandler_StreamContent(t *testing.T) {
	content := "This is test PDF content for streaming verification"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Length", "51")
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", "51")
		w.Write([]byte(content))
	}))
	defer server.Close()

	mock := &mockDocumentGetter{
		docs: map[string]*models.Document{
			"test-doc": {
				DocID: "test-doc",
				URL:   server.URL + "/file.pdf",
			},
		},
	}
	h := NewHandler(mock)
	h.SetAllowLocalURLs(true)
	defer h.Stop()

	req := httptest.NewRequest("GET", "/proxy?doc=test-doc&url="+url.QueryEscape(server.URL+"/file.pdf"), nil)
	rec := httptest.NewRecorder()

	h.HandleProxy(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if string(body) != content {
		t.Errorf("Expected body %q, got %q", content, string(body))
	}
}

func TestHandler_UpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	mock := &mockDocumentGetter{
		docs: map[string]*models.Document{
			"test-doc": {
				DocID: "test-doc",
				URL:   server.URL + "/error",
			},
		},
	}
	h := NewHandler(mock)
	h.SetAllowLocalURLs(true)
	defer h.Stop()

	req := httptest.NewRequest("GET", "/proxy?doc=test-doc&url="+url.QueryEscape(server.URL+"/error"), nil)
	rec := httptest.NewRecorder()

	h.HandleProxy(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("Expected status 502, got %d", rec.Code)
	}
}

func TestIsAllowedMIMEType(t *testing.T) {
	tests := []struct {
		mimeType string
		expected bool
	}{
		{"application/pdf", true},
		{"image/png", true},
		{"image/jpeg", true},
		{"image/gif", true},
		{"image/webp", true},
		{"image/svg+xml", true},
		{"text/html", true},
		{"text/markdown", true},
		{"text/plain", true},
		{"application/json", false},
		{"application/javascript", false},
		{"text/css", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			if got := IsAllowedMIMEType(tt.mimeType); got != tt.expected {
				t.Errorf("IsAllowedMIMEType(%q) = %v, want %v", tt.mimeType, got, tt.expected)
			}
		})
	}
}

func TestIsValidURL(t *testing.T) {
	mock := &mockDocumentGetter{docs: make(map[string]*models.Document)}
	h := NewHandler(mock)
	defer h.Stop()

	tests := []struct {
		rawURL   string
		expected bool
	}{
		{"https://example.com/file.pdf", true},
		{"http://example.com/file.pdf", true},
		{"ftp://example.com/file.pdf", false},
		{"http://localhost/file.pdf", false},
		{"http://127.0.0.1/file.pdf", false},
		{"http://[::1]/file.pdf", false},
		{"http://192.168.1.1/file.pdf", false},
		{"http://10.0.0.1/file.pdf", false},
		{"http://172.16.0.1/file.pdf", false},
		{"http:///file.pdf", false},
	}

	for _, tt := range tests {
		t.Run(tt.rawURL, func(t *testing.T) {
			parsed, err := url.Parse(tt.rawURL)
			if err != nil {
				t.Fatalf("Failed to parse URL: %v", err)
			}
			if got := h.isValidURL(parsed); got != tt.expected {
				t.Errorf("isValidURL(%q) = %v, want %v", tt.rawURL, got, tt.expected)
			}
		})
	}
}

func TestUrlsMatch(t *testing.T) {
	tests := []struct {
		docURL      string
		providedURL string
		expected    bool
	}{
		{"https://example.com/file.pdf", "https://example.com/file.pdf", true},
		{"https://example.com/file.pdf", "https://example.com/other.pdf", false},
		{"https://example.com/file.pdf", "http://example.com/file.pdf", false},
	}

	for _, tt := range tests {
		t.Run(tt.docURL+"_vs_"+tt.providedURL, func(t *testing.T) {
			if got := urlsMatch(tt.docURL, tt.providedURL); got != tt.expected {
				t.Errorf("urlsMatch(%q, %q) = %v, want %v", tt.docURL, tt.providedURL, got, tt.expected)
			}
		})
	}
}
