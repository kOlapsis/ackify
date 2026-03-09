// SPDX-License-Identifier: AGPL-3.0-or-later
package shared

import (
	"database/sql"
	"net/http"

	"github.com/kolapsis/ackify/backend/internal/infrastructure/dbctx"
	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/providers"
)

// RLSMiddleware provides Row Level Security context for database queries.
// It wraps each request in a transaction with app.tenant_id set via set_config.
// RLS is always active - this is a security feature that cannot be disabled.
type RLSMiddleware struct {
	db      *sql.DB
	tenants providers.TenantProvider
}

// NewRLSMiddleware creates a new RLS middleware.
func NewRLSMiddleware(db *sql.DB, tenants providers.TenantProvider) *RLSMiddleware {
	return &RLSMiddleware{
		db:      db,
		tenants: tenants,
	}
}

// Handler wraps HTTP requests with RLS transaction context.
// For each request:
// 1. Gets the current tenant ID from the provider
// 2. Starts a database transaction
// 3. Sets app.tenant_id in the session via set_config
// 4. Stores the transaction in the request context
// 5. Calls the next handler
// 6. Commits on success (2xx-3xx status) or rolls back on error/panic
func (m *RLSMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		requestID := getRequestID(ctx)

		// Get current tenant from provider
		tenantID, err := m.tenants.CurrentTenant(ctx)
		if err != nil {
			logger.Logger.Error("rls_middleware: failed to get tenant",
				"request_id", requestID,
				"error", err.Error())
			WriteError(w, http.StatusInternalServerError, "RLS_ERROR", "Failed to establish tenant context", nil)
			return
		}

		// Start transaction
		tx, err := m.db.BeginTx(ctx, nil)
		if err != nil {
			logger.Logger.Error("rls_middleware: failed to begin transaction",
				"request_id", requestID,
				"error", err.Error())
			WriteError(w, http.StatusInternalServerError, "RLS_ERROR", "Failed to start database transaction", nil)
			return
		}

		// Set tenant context in session
		// The 'true' makes it local to this transaction only
		_, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID.String())
		if err != nil {
			tx.Rollback()
			logger.Logger.Error("rls_middleware: failed to set tenant context",
				"request_id", requestID,
				"tenant_id", tenantID.String(),
				"error", err.Error())
			WriteError(w, http.StatusInternalServerError, "RLS_ERROR", "Failed to set tenant context", nil)
			return
		}

		logger.Logger.Debug("rls_middleware: tenant context set",
			"request_id", requestID,
			"tenant_id", tenantID.String())

		// Store transaction in context for repositories to use
		ctxWithTx := dbctx.WithTx(ctx, tx)

		// Wrap response writer to capture status code
		wrapped := &statusCapturingResponseWriter{ResponseWriter: w, status: http.StatusOK}

		// Handle panics - rollback on panic
		defer func() {
			if rec := recover(); rec != nil {
				tx.Rollback()
				logger.Logger.Error("rls_middleware: panic recovered, transaction rolled back",
					"request_id", requestID,
					"panic", rec)
				panic(rec) // re-panic after rollback to let recovery middleware handle it
			}
		}()

		// Call next handler with transaction context
		next.ServeHTTP(wrapped, r.WithContext(ctxWithTx))

		// Commit or rollback based on response status
		if wrapped.status >= 200 && wrapped.status < 400 {
			if err := tx.Commit(); err != nil {
				logger.Logger.Error("rls_middleware: failed to commit transaction",
					"request_id", requestID,
					"status", wrapped.status,
					"error", err.Error())
				// Transaction already used, can't send error response
			} else {
				logger.Logger.Debug("rls_middleware: transaction committed",
					"request_id", requestID,
					"status", wrapped.status)
			}
		} else {
			if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
				logger.Logger.Error("rls_middleware: failed to rollback transaction",
					"request_id", requestID,
					"status", wrapped.status,
					"error", err.Error())
			} else {
				logger.Logger.Debug("rls_middleware: transaction rolled back",
					"request_id", requestID,
					"status", wrapped.status)
			}
		}
	})
}

// statusCapturingResponseWriter captures the HTTP status code for decision making
type statusCapturingResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusCapturingResponseWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusCapturingResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}
