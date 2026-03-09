// SPDX-License-Identifier: AGPL-3.0-or-later
package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/kolapsis/ackify/backend/pkg/logger"
)

// SecureHeaders enforces baseline security headers (CSP, XFO, etc.)
func SecureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")

		isEmbedRoute := strings.HasPrefix(r.URL.Path, "/embed/") || strings.HasPrefix(r.URL.Path, "/embed")

		// OAuth provider avatar domains
		imgSrc := "img-src 'self' data: https://cdn.simpleicons.org https://*.googleusercontent.com https://avatars.githubusercontent.com https://secure.gravatar.com https://gitlab.com"

		if isEmbedRoute {
			w.Header().Set("Content-Security-Policy",
				"default-src 'self'; "+
					"style-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com https://fonts.googleapis.com; "+
					"font-src 'self' https://fonts.gstatic.com; "+
					"script-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com; "+
					imgSrc+"; "+
					"connect-src 'self'; "+
					"frame-ancestors *")
		} else {
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Content-Security-Policy",
				"default-src 'self'; "+
					"style-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com https://fonts.googleapis.com; "+
					"font-src 'self' https://fonts.gstatic.com; "+
					"script-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com; "+
					imgSrc+"; "+
					"connect-src 'self'; "+
					"frame-ancestors 'self'")
		}

		next.ServeHTTP(w, r)
	})
}

func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sr := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(sr, r)
		duration := time.Since(start)
		logger.Logger.Info("http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sr.status,
			"duration_ms", duration.Milliseconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}
