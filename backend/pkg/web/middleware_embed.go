// SPDX-License-Identifier: AGPL-3.0-or-later
package web

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/models"
)

type docService interface {
	FindOrCreateDocument(ctx context.Context, ref string, createdBy string) (*models.Document, bool, error)
}

// webhookPublisher defines minimal publish capability
type webhookPublisher interface {
	Publish(ctx context.Context, eventType string, payload map[string]interface{}) error
}

// EmbedDocumentMiddleware creates documents on /embed access with strict rate limiting
// This ensures documents exist before the SPA renders, without requiring authentication
// The docServiceFn should be a function that calls FindOrCreateDocument
func EmbedDocumentMiddleware(
	docService docService,
	publisher webhookPublisher,
) func(http.Handler) http.Handler {
	rateLimiter := newEmbedRateLimiter(2, time.Minute)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only intercept /embed path
			if !strings.HasPrefix(r.URL.Path, "/embed") {
				next.ServeHTTP(w, r)
				return
			}

			// Check rate limit
			ip := getClientIP(r)
			if !rateLimiter.Allow(ip) {
				logger.Logger.Warn("Embed rate limit exceeded",
					"ip", ip,
					"path", r.URL.Path)
				// Let the request continue to SPA - frontend will handle the error display
				// The frontend can check for rate limit errors via API calls
				next.ServeHTTP(w, r)
				return
			}

			// "doc" is the primary parameter; "ref" is accepted as fallback for backward compatibility
			docID := r.URL.Query().Get("doc")
			if docID == "" {
				docID = r.URL.Query().Get("ref")
			}
			if docID == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Try to create document if it doesn't exist
			ctx := r.Context()
			doc, isNew, err := docService.FindOrCreateDocument(ctx, docID, "")
			if err != nil {
				logger.Logger.Error("Failed to find/create document for embed",
					"doc_id", docID,
					"error", err.Error(),
					"ip", ip)
				// Continue to SPA anyway - it will handle the error
				next.ServeHTTP(w, r)
				return
			}

			if isNew {
				logger.Logger.Info("Document auto-created via embed view",
					"doc_id", docID,
					"ip", ip)

				// Publish webhook event for auto-created documents
				if publisher != nil {
					_ = publisher.Publish(ctx, "document.created", map[string]interface{}{
						"doc_id": doc.GetDocID(),
						"title":  doc.GetTitle(),
						"url":    doc.GetURL(),
						"source": "embed_view",
					})
				}
			}

			// Continue to SPA
			next.ServeHTTP(w, r)
		})
	}
}

// embedRateLimiter implements a simple IP-based rate limiter
type embedRateLimiter struct {
	attempts *sync.Map
	limit    int
	window   time.Duration
}

func newEmbedRateLimiter(limit int, window time.Duration) *embedRateLimiter {
	return &embedRateLimiter{
		attempts: &sync.Map{},
		limit:    limit,
		window:   window,
	}
}

func (rl *embedRateLimiter) Allow(ip string) bool {
	now := time.Now()

	// Check current attempts
	if val, ok := rl.attempts.Load(ip); ok {
		attempts := val.([]time.Time)

		// Filter out old attempts
		var valid []time.Time
		for _, t := range attempts {
			if now.Sub(t) < rl.window {
				valid = append(valid, t)
			}
		}

		if len(valid) >= rl.limit {
			return false
		}

		valid = append(valid, now)
		rl.attempts.Store(ip, valid)
	} else {
		rl.attempts.Store(ip, []time.Time{now})
	}

	return true
}

func getClientIP(r *http.Request) string {
	// Try X-Forwarded-For first (for proxies)
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		ips := strings.Split(forwarded, ",")
		return strings.TrimSpace(ips[0])
	}

	// Try X-Real-IP
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}

	// Fallback to RemoteAddr
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}
