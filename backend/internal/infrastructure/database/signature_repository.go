// SPDX-License-Identifier: AGPL-3.0-or-later
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/kolapsis/ackify/backend/internal/infrastructure/dbctx"
	"github.com/kolapsis/ackify/backend/pkg/models"
	"github.com/kolapsis/ackify/backend/pkg/providers"
)

// SignatureRepository handles PostgreSQL persistence for cryptographic signatures
type SignatureRepository struct {
	db      *sql.DB
	tenants providers.TenantProvider
}

// NewSignatureRepository initializes a signature repository with the given database connection
func NewSignatureRepository(db *sql.DB, tenants providers.TenantProvider) *SignatureRepository {
	return &SignatureRepository{db: db, tenants: tenants}
}

func scanSignature(scanner interface {
	Scan(dest ...interface{}) error
}, signature *models.Signature) error {
	var userName sql.NullString
	var docChecksum sql.NullString
	var hashVersion sql.NullInt64
	var docDeletedAt sql.NullTime
	var docTitle sql.NullString
	var docURL sql.NullString
	err := scanner.Scan(
		&signature.ID,
		&signature.TenantID,
		&signature.DocID,
		&signature.UserSub,
		&signature.UserEmail,
		&userName,
		&signature.SignedAtUTC,
		&docChecksum,
		&signature.PayloadHash,
		&signature.Signature,
		&signature.Nonce,
		&signature.CreatedAt,
		&signature.Referer,
		&signature.PrevHash,
		&hashVersion,
		&docDeletedAt,
		&docTitle,
		&docURL,
	)
	if err != nil {
		return err
	}
	if userName.Valid {
		signature.UserName = userName.String
	} else {
		signature.UserName = ""
	}
	if docChecksum.Valid {
		signature.DocChecksum = docChecksum.String
	} else {
		signature.DocChecksum = ""
	}
	if hashVersion.Valid {
		signature.HashVersion = int(hashVersion.Int64)
	} else {
		signature.HashVersion = 1 // Default to version 1
	}
	if docDeletedAt.Valid {
		signature.DocDeletedAt = &docDeletedAt.Time
	}
	if docTitle.Valid {
		signature.DocTitle = docTitle.String
	}
	if docURL.Valid {
		signature.DocURL = docURL.String
	}
	return nil
}

// Create persists a new signature record to PostgreSQL with UNIQUE constraint enforcement on (doc_id, user_sub)
func (r *SignatureRepository) Create(ctx context.Context, signature *models.Signature) error {
	tenantID, err := r.tenants.CurrentTenant(ctx)
	if err != nil {
		return fmt.Errorf("failed to get tenant: %w", err)
	}

	query := `
		INSERT INTO signatures (tenant_id, doc_id, user_sub, user_email, user_name, signed_at, doc_checksum, payload_hash, signature, nonce, referer, prev_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, created_at
	`

	var userName sql.NullString
	if signature.UserName != "" {
		userName = sql.NullString{String: signature.UserName, Valid: true}
	}

	var docChecksum sql.NullString
	if signature.DocChecksum != "" {
		docChecksum = sql.NullString{String: signature.DocChecksum, Valid: true}
	}

	err = dbctx.GetQuerier(ctx, r.db).QueryRowContext(
		ctx, query,
		tenantID,
		signature.DocID,
		signature.UserSub,
		signature.UserEmail,
		userName,
		signature.SignedAtUTC,
		docChecksum,
		signature.PayloadHash,
		signature.Signature,
		signature.Nonce,
		signature.Referer,
		signature.PrevHash,
	).Scan(&signature.ID, &signature.CreatedAt)

	if err != nil {
		return fmt.Errorf("failed to create signature: %w", err)
	}

	signature.TenantID = tenantID
	return nil
}

// GetByDocAndUser retrieves a specific signature by document ID and user OAuth subject identifier
// RLS policy automatically filters by tenant_id
func (r *SignatureRepository) GetByDocAndUser(ctx context.Context, docID, userSub string) (*models.Signature, error) {
	query := `
		SELECT s.id, s.tenant_id, s.doc_id, s.user_sub, s.user_email, s.user_name, s.signed_at, s.doc_checksum,
		       s.payload_hash, s.signature, s.nonce, s.created_at, s.referer, s.prev_hash,
		       s.hash_version, s.doc_deleted_at, d.title, d.url
		FROM signatures s
		LEFT JOIN documents d ON s.doc_id = d.doc_id AND s.tenant_id = d.tenant_id
		WHERE s.doc_id = $1 AND s.user_sub = $2
	`

	signature := &models.Signature{}
	err := scanSignature(dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, query, docID, userSub), signature)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, models.ErrSignatureNotFound
		}
		return nil, fmt.Errorf("failed to get signature: %w", err)
	}

	return signature, nil
}

// GetByDoc retrieves all signatures for a specific document, ordered by creation timestamp descending
// RLS policy automatically filters by tenant_id
func (r *SignatureRepository) GetByDoc(ctx context.Context, docID string) ([]*models.Signature, error) {
	query := `
		SELECT s.id, s.tenant_id, s.doc_id, s.user_sub, s.user_email, s.user_name, s.signed_at, s.doc_checksum,
		       s.payload_hash, s.signature, s.nonce, s.created_at, s.referer, s.prev_hash,
		       s.hash_version, s.doc_deleted_at, d.title, d.url
		FROM signatures s
		LEFT JOIN documents d ON s.doc_id = d.doc_id AND s.tenant_id = d.tenant_id
		WHERE s.doc_id = $1
		ORDER BY s.created_at DESC
	`

	rows, err := dbctx.GetQuerier(ctx, r.db).QueryContext(ctx, query, docID)
	if err != nil {
		return nil, fmt.Errorf("failed to query signatures: %w", err)
	}
	defer func(rows *sql.Rows) {
		_ = rows.Close()
	}(rows)

	var signatures []*models.Signature
	for rows.Next() {
		signature := &models.Signature{}
		if err := scanSignature(rows, signature); err != nil {
			continue
		}
		signatures = append(signatures, signature)
	}

	return signatures, nil
}

// GetByUserEmail retrieves all signatures created by a specific user (by email), ordered by creation timestamp descending
// RLS policy automatically filters by tenant_id
func (r *SignatureRepository) GetByUserEmail(ctx context.Context, userEmail string) ([]*models.Signature, error) {
	query := `
		SELECT s.id, s.tenant_id, s.doc_id, s.user_sub, s.user_email, s.user_name, s.signed_at, s.doc_checksum,
		       s.payload_hash, s.signature, s.nonce, s.created_at, s.referer, s.prev_hash,
		       s.hash_version, s.doc_deleted_at, d.title, d.url
		FROM signatures s
		LEFT JOIN documents d ON s.doc_id = d.doc_id AND s.tenant_id = d.tenant_id
		WHERE LOWER(s.user_email) = LOWER($1)
		ORDER BY s.created_at DESC
	`

	rows, err := dbctx.GetQuerier(ctx, r.db).QueryContext(ctx, query, userEmail)
	if err != nil {
		return nil, fmt.Errorf("failed to query user signatures: %w", err)
	}
	defer func(rows *sql.Rows) {
		_ = rows.Close()
	}(rows)

	var signatures []*models.Signature
	for rows.Next() {
		signature := &models.Signature{}
		if err := scanSignature(rows, signature); err != nil {
			continue
		}
		signatures = append(signatures, signature)
	}

	return signatures, nil
}

// ExistsByDocAndUser efficiently checks if a signature already exists without retrieving full record data
// RLS policy automatically filters by tenant_id
func (r *SignatureRepository) ExistsByDocAndUser(ctx context.Context, docID, userSub string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM signatures WHERE doc_id = $1 AND user_sub = $2)`

	var exists bool
	err := dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, query, docID, userSub).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check signature existence: %w", err)
	}

	return exists, nil
}

// CheckUserSignatureStatus verifies if a user has signed, accepting either OAuth subject or email as identifier
// RLS policy automatically filters by tenant_id
func (r *SignatureRepository) CheckUserSignatureStatus(ctx context.Context, docID, userIdentifier string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM signatures
			WHERE doc_id = $1 AND (user_sub = $2 OR LOWER(user_email) = LOWER($2))
		)
	`

	var exists bool
	err := dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, query, docID, userIdentifier).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check user signature status: %w", err)
	}

	return exists, nil
}

// GetLastSignature retrieves the most recent signature for hash chain linking (returns nil if no signatures exist)
// RLS policy automatically filters by tenant_id
func (r *SignatureRepository) GetLastSignature(ctx context.Context, docID string) (*models.Signature, error) {
	query := `
		SELECT s.id, s.tenant_id, s.doc_id, s.user_sub, s.user_email, s.user_name, s.signed_at, s.doc_checksum,
		       s.payload_hash, s.signature, s.nonce, s.created_at, s.referer, s.prev_hash,
		       s.hash_version, s.doc_deleted_at, d.title, d.url
		FROM signatures s
		LEFT JOIN documents d ON s.doc_id = d.doc_id AND s.tenant_id = d.tenant_id
		WHERE s.doc_id = $1
		ORDER BY s.id DESC
		LIMIT 1
	`

	signature := &models.Signature{}
	err := scanSignature(dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, query, docID), signature)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get last signature: %w", err)
	}

	return signature, nil
}

// GetAllSignaturesOrdered retrieves all signatures in chronological order for chain integrity verification
// RLS policy automatically filters by tenant_id
func (r *SignatureRepository) GetAllSignaturesOrdered(ctx context.Context) ([]*models.Signature, error) {
	query := `
		SELECT s.id, s.tenant_id, s.doc_id, s.user_sub, s.user_email, s.user_name, s.signed_at, s.doc_checksum,
		       s.payload_hash, s.signature, s.nonce, s.created_at, s.referer, s.prev_hash,
		       s.hash_version, s.doc_deleted_at, d.title, d.url
		FROM signatures s
		LEFT JOIN documents d ON s.doc_id = d.doc_id AND s.tenant_id = d.tenant_id
		ORDER BY s.id ASC`

	rows, err := dbctx.GetQuerier(ctx, r.db).QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query all signatures: %w", err)
	}
	defer func(rows *sql.Rows) {
		_ = rows.Close()
	}(rows)

	var signatures []*models.Signature
	for rows.Next() {
		signature := &models.Signature{}
		if err := scanSignature(rows, signature); err != nil {
			continue
		}
		signatures = append(signatures, signature)
	}

	return signatures, nil
}

// UpdatePrevHash modifies the previous hash pointer for chain reconstruction operations
// RLS policy automatically filters by tenant_id
func (r *SignatureRepository) UpdatePrevHash(ctx context.Context, id int64, prevHash *string) error {
	query := `UPDATE signatures SET prev_hash = $2 WHERE id = $1`
	if _, err := dbctx.GetQuerier(ctx, r.db).ExecContext(ctx, query, id, prevHash); err != nil {
		return fmt.Errorf("failed to update prev_hash: %w", err)
	}
	return nil
}

// Count returns the total number of unique signers (distinct email addresses)
func (r *SignatureRepository) Count(ctx context.Context) (int, error) {
	query := `SELECT COUNT(DISTINCT user_email) FROM signatures`
	var count int
	err := dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count unique signers: %w", err)
	}
	return count, nil
}
