// SPDX-License-Identifier: AGPL-3.0-or-later
package tenant

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/kolapsis/ackify/backend/internal/infrastructure/dbctx"
	"github.com/kolapsis/ackify/backend/pkg/providers"
)

// WithTenantContext executes the given function within a transactional context
// configured with RLS tenant isolation. It:
// 1. Begins a new transaction
// 2. Sets the app.tenant_id session variable for RLS policies
// 3. Stores the transaction in the context for use by repositories
// 4. Commits on success, rolls back on error or panic
//
// This is the primary mechanism for ensuring RLS isolation in workers,
// background jobs, and tests. HTTP handlers should use the RLS middleware instead.
//
// Example usage:
//
//	err := tenant.WithTenantContext(ctx, db, tenantID, func(ctx context.Context) error {
//	    // All repository calls here will use RLS isolation
//	    doc, err := docRepo.GetByDocID(ctx, docID)
//	    return err
//	})
func WithTenantContext(ctx context.Context, db *sql.DB, tenantID uuid.UUID, fn func(ctx context.Context) error) (err error) {
	// Begin transaction
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Ensure cleanup on panic or error
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p) // Re-throw panic after rollback
		} else if err != nil {
			_ = tx.Rollback()
		}
	}()

	// Set tenant_id for RLS policies (LOCAL = transaction scope only)
	_, err = tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID.String())
	if err != nil {
		return fmt.Errorf("failed to set tenant context: %w", err)
	}

	// Store transaction in context for GetQuerier
	txCtx := dbctx.WithTx(ctx, tx)

	// Execute the function
	if err = fn(txCtx); err != nil {
		return err
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// WithTenantContextFromProvider is like WithTenantContext but obtains the tenant ID
// from a TenantProvider. This is useful when the tenant ID is not known upfront.
func WithTenantContextFromProvider(ctx context.Context, db *sql.DB, provider providers.TenantProvider, fn func(ctx context.Context) error) error {
	tenantID, err := provider.CurrentTenant(ctx)
	if err != nil {
		return fmt.Errorf("failed to get tenant ID: %w", err)
	}
	return WithTenantContext(ctx, db, tenantID, fn)
}
