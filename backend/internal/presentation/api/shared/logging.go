// SPDX-License-Identifier: AGPL-3.0-or-later
package shared

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/kolapsis/ackify/backend/pkg/logger"
)

// responseWriter is a wrapper around http.ResponseWriter that captures the status code
type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func wrapResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w}
}

func (rw *responseWriter) Status() int {
	return rw.status
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}

	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
	rw.wroteHeader = true
}

// RequestLogger middleware logs all API requests with structured logging
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := getRequestID(r.Context())

		// Log request start in DEBUG
		logger.Logger.Debug("api_request_start",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
			"user_agent", r.UserAgent())

		wrapped := wrapResponseWriter(w)
		next.ServeHTTP(wrapped, r)

		// Log request completion
		duration := time.Since(start)
		status := wrapped.status
		if status == 0 {
			status = 200
		}

		fields := []interface{}{
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration_ms", duration.Milliseconds(),
		}

		// Add user email if available
		if user, ok := GetUserFromContext(r.Context()); ok {
			fields = append(fields, "user_email", user.Email)
		}

		// Log at appropriate level based on status
		if status >= 500 {
			logger.Logger.Error("api_request_error", fields...)
		} else if status >= 400 {
			logger.Logger.Warn("api_request_client_error", fields...)
		} else {
			logger.Logger.Info("api_request_complete", fields...)
		}
	})
}

// Helper functions

func getRequestID(ctx context.Context) string {
	if requestID, ok := ctx.Value(ContextKeyRequestID).(string); ok {
		return requestID
	}
	return ""
}

func errToString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// AddRequestIDToContext middleware adds the request ID from chi middleware to our context
func AddRequestIDToContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := middleware.GetReqID(r.Context())
		ctx := context.WithValue(r.Context(), ContextKeyRequestID, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
