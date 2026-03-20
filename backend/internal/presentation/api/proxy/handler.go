// SPDX-License-Identifier: AGPL-3.0-or-later
package proxy

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kolapsis/ackify/backend/internal/presentation/api/shared"
	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/models"
)

// ErrorCode constants for proxy-specific errors
const (
	ErrCodeMIMENotAllowed    shared.ErrorCode = "MIME_NOT_ALLOWED"
	ErrCodeFileTooLarge      shared.ErrorCode = "FILE_TOO_LARGE"
	ErrCodeTimeout           shared.ErrorCode = "TIMEOUT"
	ErrCodeBadGateway        shared.ErrorCode = "BAD_GATEWAY"
	ErrCodeURLMismatch       shared.ErrorCode = "URL_MISMATCH"
	ErrCodeInvalidURL        shared.ErrorCode = "INVALID_URL"
	ErrCodeUpstreamError     shared.ErrorCode = "UPSTREAM_ERROR"
	ErrCodeContentLenMissing shared.ErrorCode = "CONTENT_LENGTH_MISSING"
)

// DocumentGetter defines the interface for getting documents
type DocumentGetter interface {
	GetByDocID(ctx context.Context, docID string) (*models.Document, error)
}

// Handler handles proxy requests for streaming external documents
type Handler struct {
	documentGetter DocumentGetter
	rateLimiter    *RateLimiter
	httpClient     *http.Client
	allowLocalURLs bool // For testing only - allows localhost/private IPs
}

// NewHandler creates a new proxy handler
func NewHandler(documentGetter DocumentGetter) *Handler {
	// Create custom transport with timeouts
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   DialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: HeaderTimeout,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		DisableCompression:    false,
	}

	return &Handler{
		documentGetter: documentGetter,
		rateLimiter: NewRateLimiter(
			RateLimitPerIP,
			RateLimitPerIPDoc,
			RateLimitPerDoc,
			RateLimitWindow,
		),
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   TotalTimeout,
			// Don't follow redirects automatically - we want to control this
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return fmt.Errorf("stopped after 3 redirects")
				}
				return nil
			},
		},
	}
}

// HandleProxy handles GET /api/proxy?url={encoded_url}&doc={doc_id}
func (h *Handler) HandleProxy(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	requestID := getRequestID(r.Context())

	// "doc" is the primary parameter; "ref" is accepted as fallback for backward compatibility
	docID := r.URL.Query().Get("doc")
	if docID == "" {
		docID = r.URL.Query().Get("ref")
	}
	rawURL := r.URL.Query().Get("url")

	// Validate required parameters
	if docID == "" {
		logger.Logger.Warn("proxy_missing_doc_id", "request_id", requestID)
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Missing doc parameter", nil)
		return
	}

	if rawURL == "" {
		logger.Logger.Warn("proxy_missing_url", "request_id", requestID, "doc_id", docID)
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Missing url parameter", nil)
		return
	}

	// Validate and parse URL
	parsedURL, err := url.Parse(rawURL)
	if err != nil || !h.isValidURL(parsedURL) {
		logger.Logger.Warn("proxy_invalid_url", "request_id", requestID, "doc_id", docID, "url", rawURL)
		shared.WriteError(w, http.StatusBadRequest, ErrCodeInvalidURL, "Invalid URL", nil)
		return
	}

	// Get client IP
	clientIP := getClientIP(r)

	// Check rate limit
	rateLimitResult := h.rateLimiter.Check(clientIP, docID)
	if !rateLimitResult.Allowed {
		logger.Logger.Warn("proxy_rate_limited",
			"request_id", requestID,
			"doc_id", docID,
			"client_ip", clientIP,
			"limit_type", rateLimitResult.LimitType)

		w.Header().Set("Retry-After", strconv.Itoa(int(rateLimitResult.RetryAfter.Seconds())))
		shared.WriteError(w, http.StatusTooManyRequests, shared.ErrCodeRateLimited, "Rate limit exceeded", map[string]interface{}{
			"retryAfter": rateLimitResult.RetryAfter.Seconds(),
			"limitType":  rateLimitResult.LimitType,
		})
		return
	}

	// Validate document exists and URL matches
	doc, err := h.documentGetter.GetByDocID(r.Context(), docID)
	if err != nil || doc == nil {
		logger.Logger.Warn("proxy_document_not_found", "request_id", requestID, "doc_id", docID)
		shared.WriteNotFound(w, "Document")
		return
	}

	// Verify URL matches document's URL
	if !urlsMatch(doc.URL, rawURL) {
		logger.Logger.Warn("proxy_url_mismatch",
			"request_id", requestID,
			"doc_id", docID,
			"expected_url", doc.URL,
			"provided_url", rawURL)
		shared.WriteError(w, http.StatusNotFound, ErrCodeURLMismatch, "URL does not match document", nil)
		return
	}

	// Perform HEAD request to get metadata
	headCtx, headCancel := context.WithTimeout(r.Context(), HeaderTimeout)
	defer headCancel()

	headReq, err := http.NewRequestWithContext(headCtx, "HEAD", rawURL, nil)
	if err != nil {
		logger.Logger.Error("proxy_head_request_create_failed", "request_id", requestID, "doc_id", docID, "error", err)
		shared.WriteError(w, http.StatusBadGateway, ErrCodeBadGateway, "Failed to create request", nil)
		return
	}

	// Add user agent
	headReq.Header.Set("User-Agent", "Ackify-Proxy/1.0")

	headResp, err := h.httpClient.Do(headReq)
	if err != nil {
		if isTimeout(err) {
			logger.Logger.Warn("proxy_head_timeout", "request_id", requestID, "doc_id", docID, "error", err)
			shared.WriteError(w, http.StatusRequestTimeout, ErrCodeTimeout, "Request timeout", nil)
			return
		}
		logger.Logger.Error("proxy_head_request_failed", "request_id", requestID, "doc_id", docID, "error", err)
		shared.WriteError(w, http.StatusBadGateway, ErrCodeBadGateway, "Failed to fetch document metadata", nil)
		return
	}
	headResp.Body.Close()

	// Check response status
	if headResp.StatusCode >= 400 {
		logger.Logger.Warn("proxy_upstream_error",
			"request_id", requestID,
			"doc_id", docID,
			"upstream_status", headResp.StatusCode)
		shared.WriteError(w, http.StatusBadGateway, ErrCodeUpstreamError, "Upstream server error", map[string]interface{}{
			"upstreamStatus": headResp.StatusCode,
		})
		return
	}

	// Validate Content-Type
	contentType := headResp.Header.Get("Content-Type")
	mimeType, _, _ := mime.ParseMediaType(contentType)
	if !IsAllowedMIMEType(mimeType) {
		logger.Logger.Warn("proxy_mime_not_allowed",
			"request_id", requestID,
			"doc_id", docID,
			"content_type", contentType,
			"mime_type", mimeType)
		shared.WriteError(w, http.StatusForbidden, ErrCodeMIMENotAllowed, "MIME type not allowed", map[string]interface{}{
			"mimeType": mimeType,
		})
		return
	}

	// Validate Content-Length
	contentLengthStr := headResp.Header.Get("Content-Length")
	var contentLength int64
	if contentLengthStr != "" {
		contentLength, err = strconv.ParseInt(contentLengthStr, 10, 64)
		if err != nil {
			logger.Logger.Warn("proxy_invalid_content_length", "request_id", requestID, "doc_id", docID, "content_length", contentLengthStr)
			shared.WriteError(w, http.StatusBadGateway, ErrCodeBadGateway, "Invalid content length from upstream", nil)
			return
		}

		if contentLength > MaxDocumentSize {
			logger.Logger.Warn("proxy_file_too_large",
				"request_id", requestID,
				"doc_id", docID,
				"content_length", contentLength,
				"max_size", MaxDocumentSize)
			shared.WriteError(w, http.StatusRequestEntityTooLarge, ErrCodeFileTooLarge, "File too large", map[string]interface{}{
				"size":    contentLength,
				"maxSize": MaxDocumentSize,
			})
			return
		}
	}

	// Perform GET request to stream content
	getCtx, getCancel := context.WithTimeout(r.Context(), TotalTimeout)
	defer getCancel()

	getReq, err := http.NewRequestWithContext(getCtx, "GET", rawURL, nil)
	if err != nil {
		logger.Logger.Error("proxy_get_request_create_failed", "request_id", requestID, "doc_id", docID, "error", err)
		shared.WriteError(w, http.StatusBadGateway, ErrCodeBadGateway, "Failed to create request", nil)
		return
	}

	getReq.Header.Set("User-Agent", "Ackify-Proxy/1.0")

	getResp, err := h.httpClient.Do(getReq)
	if err != nil {
		if isTimeout(err) {
			logger.Logger.Warn("proxy_get_timeout", "request_id", requestID, "doc_id", docID, "error", err)
			shared.WriteError(w, http.StatusRequestTimeout, ErrCodeTimeout, "Request timeout", nil)
			return
		}
		logger.Logger.Error("proxy_get_request_failed", "request_id", requestID, "doc_id", docID, "error", err)
		shared.WriteError(w, http.StatusBadGateway, ErrCodeBadGateway, "Failed to fetch document", nil)
		return
	}
	defer getResp.Body.Close()

	// Check response status again
	if getResp.StatusCode >= 400 {
		logger.Logger.Warn("proxy_get_upstream_error",
			"request_id", requestID,
			"doc_id", docID,
			"upstream_status", getResp.StatusCode)
		shared.WriteError(w, http.StatusBadGateway, ErrCodeUpstreamError, "Upstream server error", map[string]interface{}{
			"upstreamStatus": getResp.StatusCode,
		})
		return
	}

	// Set security headers
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")

	// Forward relevant headers
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if contentLengthStr != "" {
		w.Header().Set("Content-Length", contentLengthStr)
	}
	if lastModified := getResp.Header.Get("Last-Modified"); lastModified != "" {
		w.Header().Set("Last-Modified", lastModified)
	}
	if etag := getResp.Header.Get("ETag"); etag != "" {
		w.Header().Set("ETag", etag)
	}

	// Stream content directly to client (pipe, no buffer)
	w.WriteHeader(http.StatusOK)

	// Use a limited reader to enforce size limit even if Content-Length was missing
	limitedReader := io.LimitReader(getResp.Body, MaxDocumentSize+1)
	written, err := io.Copy(w, limitedReader)

	duration := time.Since(startTime)

	if err != nil {
		logger.Logger.Error("proxy_stream_error",
			"request_id", requestID,
			"doc_id", docID,
			"bytes_written", written,
			"duration_ms", duration.Milliseconds(),
			"error", err)
		return
	}

	// Check if we hit the size limit
	if written > MaxDocumentSize {
		logger.Logger.Warn("proxy_size_limit_exceeded_during_stream",
			"request_id", requestID,
			"doc_id", docID,
			"bytes_written", written)
		return
	}

	logger.Logger.Info("proxy_stream_complete",
		"request_id", requestID,
		"doc_id", docID,
		"bytes_written", written,
		"duration_ms", duration.Milliseconds(),
		"content_type", mimeType)
}

// Stop stops the handler's background goroutines
func (h *Handler) Stop() {
	h.rateLimiter.Stop()
}

// SetAllowLocalURLs enables or disables local URL access (for testing only)
func (h *Handler) SetAllowLocalURLs(allow bool) {
	h.allowLocalURLs = allow
}

// Helper functions

func getRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(shared.ContextKeyRequestID).(string); ok {
		return id
	}
	return ""
}

func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (h *Handler) isValidURL(u *url.URL) bool {
	// Must be HTTP or HTTPS
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}

	// Must have a host
	if u.Host == "" {
		return false
	}

	// Allow local URLs for testing
	if h.allowLocalURLs {
		return true
	}

	// Block localhost and private IPs (basic SSRF protection)
	host := u.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return false
	}

	// Check for private IP ranges
	ip := net.ParseIP(host)
	if ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()) {
		return false
	}

	return true
}

func urlsMatch(docURL, providedURL string) bool {
	// Simple exact match for now
	// Could be enhanced to handle trailing slashes, query params, etc.
	return docURL == providedURL
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}

	// Check for context deadline exceeded
	if err == context.DeadlineExceeded {
		return true
	}

	// Check for net.Error timeout
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return true
	}

	// Check wrapped errors
	if strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline exceeded") {
		return true
	}

	return false
}
