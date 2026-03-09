// SPDX-License-Identifier: AGPL-3.0-or-later
package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/kolapsis/ackify/backend/internal/infrastructure/dbctx"
	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/models"
	"github.com/kolapsis/ackify/backend/pkg/providers"
)

// DocumentRepository handles document metadata persistence
type DocumentRepository struct {
	db      *sql.DB
	tenants providers.TenantProvider
}

// NewDocumentRepository creates a new DocumentRepository
func NewDocumentRepository(db *sql.DB, tenants providers.TenantProvider) *DocumentRepository {
	return &DocumentRepository{db: db, tenants: tenants}
}

// Create persists a new document with metadata including optional checksum validation data
func (r *DocumentRepository) Create(ctx context.Context, docID string, input models.DocumentInput, createdBy string) (*models.Document, error) {
	tenantID, err := r.tenants.CurrentTenant(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}

	query := `
		INSERT INTO documents (tenant_id, doc_id, title, url, checksum, checksum_algorithm, description, read_mode, allow_download, require_full_read, verify_checksum, created_by, storage_key, storage_provider, file_size, mime_type, original_filename)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		RETURNING doc_id, tenant_id, title, url, checksum, checksum_algorithm, description, read_mode, allow_download, require_full_read, verify_checksum, created_at, updated_at, created_by, deleted_at, storage_key, storage_provider, file_size, mime_type, original_filename
	`

	// Use NULL for empty checksum fields to avoid constraint violation
	var checksum, checksumAlgorithm interface{}
	if input.Checksum != "" {
		checksum = input.Checksum
		checksumAlgorithm = input.ChecksumAlgorithm
	} else {
		checksum = ""
		checksumAlgorithm = "SHA-256"
	}

	// Handle read_mode with default
	readMode := input.ReadMode
	if readMode == "" {
		readMode = "integrated"
	}

	// Handle boolean defaults
	allowDownload := true
	if input.AllowDownload != nil {
		allowDownload = *input.AllowDownload
	}
	requireFullRead := false
	if input.RequireFullRead != nil {
		requireFullRead = *input.RequireFullRead
	}
	verifyChecksum := true
	if input.VerifyChecksum != nil {
		verifyChecksum = *input.VerifyChecksum
	}

	// Handle storage fields - use sql.NullString/NullInt64 for nullable columns
	var storageKey, storageProvider, mimeType, originalFilename sql.NullString
	var fileSize sql.NullInt64

	if input.StorageKey != "" {
		storageKey = sql.NullString{String: input.StorageKey, Valid: true}
	}
	if input.StorageProvider != "" {
		storageProvider = sql.NullString{String: input.StorageProvider, Valid: true}
	}
	if input.FileSize > 0 {
		fileSize = sql.NullInt64{Int64: input.FileSize, Valid: true}
	}
	if input.MimeType != "" {
		mimeType = sql.NullString{String: input.MimeType, Valid: true}
	}
	if input.OriginalFilename != "" {
		originalFilename = sql.NullString{String: input.OriginalFilename, Valid: true}
	}

	doc := &models.Document{}
	var scanStorageKey, scanStorageProvider, scanMimeType, scanOriginalFilename sql.NullString
	var scanFileSize sql.NullInt64

	err = dbctx.GetQuerier(ctx, r.db).QueryRowContext(
		ctx,
		query,
		tenantID,
		docID,
		input.Title,
		input.URL,
		checksum,
		checksumAlgorithm,
		input.Description,
		readMode,
		allowDownload,
		requireFullRead,
		verifyChecksum,
		createdBy,
		storageKey,
		storageProvider,
		fileSize,
		mimeType,
		originalFilename,
	).Scan(
		&doc.DocID,
		&doc.TenantID,
		&doc.Title,
		&doc.URL,
		&doc.Checksum,
		&doc.ChecksumAlgorithm,
		&doc.Description,
		&doc.ReadMode,
		&doc.AllowDownload,
		&doc.RequireFullRead,
		&doc.VerifyChecksum,
		&doc.CreatedAt,
		&doc.UpdatedAt,
		&doc.CreatedBy,
		&doc.DeletedAt,
		&scanStorageKey,
		&scanStorageProvider,
		&scanFileSize,
		&scanMimeType,
		&scanOriginalFilename,
	)

	if err != nil {
		logger.Logger.Error("Failed to create document", "error", err.Error(), "doc_id", docID)
		return nil, fmt.Errorf("failed to create document: %w", err)
	}

	// Convert nullable fields to model
	doc.StorageKey = scanStorageKey.String
	doc.StorageProvider = scanStorageProvider.String
	doc.FileSize = scanFileSize.Int64
	doc.MimeType = scanMimeType.String
	doc.OriginalFilename = scanOriginalFilename.String

	return doc, nil
}

// documentColumns is the standard column list for document queries
const documentColumns = `doc_id, tenant_id, title, url, checksum, checksum_algorithm, description, read_mode, allow_download, require_full_read, verify_checksum, created_at, updated_at, created_by, deleted_at, storage_key, storage_provider, file_size, mime_type, original_filename`

// scanDocument scans a row into a Document model with nullable storage fields
func scanDocument(row interface{ Scan(dest ...any) error }) (*models.Document, error) {
	doc := &models.Document{}
	var storageKey, storageProvider, mimeType, originalFilename sql.NullString
	var fileSize sql.NullInt64

	err := row.Scan(
		&doc.DocID,
		&doc.TenantID,
		&doc.Title,
		&doc.URL,
		&doc.Checksum,
		&doc.ChecksumAlgorithm,
		&doc.Description,
		&doc.ReadMode,
		&doc.AllowDownload,
		&doc.RequireFullRead,
		&doc.VerifyChecksum,
		&doc.CreatedAt,
		&doc.UpdatedAt,
		&doc.CreatedBy,
		&doc.DeletedAt,
		&storageKey,
		&storageProvider,
		&fileSize,
		&mimeType,
		&originalFilename,
	)
	if err != nil {
		return nil, err
	}

	// Convert nullable fields to model
	doc.StorageKey = storageKey.String
	doc.StorageProvider = storageProvider.String
	doc.FileSize = fileSize.Int64
	doc.MimeType = mimeType.String
	doc.OriginalFilename = originalFilename.String

	return doc, nil
}

// GetByDocID retrieves document metadata by document ID (excluding soft-deleted documents)
// RLS policy automatically filters by tenant_id
func (r *DocumentRepository) GetByDocID(ctx context.Context, docID string) (*models.Document, error) {
	query := `SELECT ` + documentColumns + ` FROM documents WHERE doc_id = $1 AND deleted_at IS NULL`

	row := dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, query, docID)
	doc, err := scanDocument(row)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		logger.Logger.Error("Failed to get document", "error", err.Error(), "doc_id", docID)
		return nil, fmt.Errorf("failed to get document: %w", err)
	}

	return doc, nil
}

// FindByReference searches for a document by reference (URL, path, or doc_id)
// RLS policy automatically filters by tenant_id
func (r *DocumentRepository) FindByReference(ctx context.Context, ref string, refType string) (*models.Document, error) {
	var query string
	var args []interface{}

	switch refType {
	case "url", "path":
		query = `SELECT ` + documentColumns + ` FROM documents WHERE url = $1 AND deleted_at IS NULL LIMIT 1`
		args = []interface{}{ref}
	case "reference":
		query = `SELECT ` + documentColumns + ` FROM documents WHERE doc_id = $1 AND deleted_at IS NULL LIMIT 1`
		args = []interface{}{ref}
	default:
		return nil, fmt.Errorf("unknown reference type: %s", refType)
	}

	row := dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, query, args...)
	doc, err := scanDocument(row)

	if err == sql.ErrNoRows {
		logger.Logger.Debug("Document not found by reference", "reference", ref, "type", refType)
		return nil, nil
	}

	if err != nil {
		logger.Logger.Error("Failed to find document by reference", "error", err.Error(), "reference", ref, "type", refType)
		return nil, fmt.Errorf("failed to find document: %w", err)
	}

	logger.Logger.Debug("Document found by reference", "doc_id", doc.DocID, "reference", ref, "type", refType)
	return doc, nil
}

// Update modifies existing document metadata while preserving creation timestamp and creator
// RLS policy automatically filters by tenant_id
func (r *DocumentRepository) Update(ctx context.Context, docID string, input models.DocumentInput) (*models.Document, error) {
	query := `
		UPDATE documents
		SET title = $2, url = $3, checksum = $4, checksum_algorithm = $5, description = $6, read_mode = $7, allow_download = $8, require_full_read = $9, verify_checksum = $10, storage_key = $11, storage_provider = $12, file_size = $13, mime_type = $14, original_filename = $15
		WHERE doc_id = $1 AND deleted_at IS NULL
		RETURNING ` + documentColumns

	checksum := input.Checksum
	checksumAlgorithm := input.ChecksumAlgorithm
	if checksumAlgorithm == "" {
		checksumAlgorithm = "SHA-256"
	}

	readMode := input.ReadMode
	if readMode == "" {
		readMode = "integrated"
	}

	allowDownload := true
	if input.AllowDownload != nil {
		allowDownload = *input.AllowDownload
	}
	requireFullRead := false
	if input.RequireFullRead != nil {
		requireFullRead = *input.RequireFullRead
	}
	verifyChecksum := true
	if input.VerifyChecksum != nil {
		verifyChecksum = *input.VerifyChecksum
	}

	// Handle storage fields
	var storageKey, storageProvider, mimeType, originalFilename sql.NullString
	var fileSize sql.NullInt64
	if input.StorageKey != "" {
		storageKey = sql.NullString{String: input.StorageKey, Valid: true}
	}
	if input.StorageProvider != "" {
		storageProvider = sql.NullString{String: input.StorageProvider, Valid: true}
	}
	if input.FileSize > 0 {
		fileSize = sql.NullInt64{Int64: input.FileSize, Valid: true}
	}
	if input.MimeType != "" {
		mimeType = sql.NullString{String: input.MimeType, Valid: true}
	}
	if input.OriginalFilename != "" {
		originalFilename = sql.NullString{String: input.OriginalFilename, Valid: true}
	}

	row := dbctx.GetQuerier(ctx, r.db).QueryRowContext(
		ctx, query, docID, input.Title, input.URL, checksum, checksumAlgorithm,
		input.Description, readMode, allowDownload, requireFullRead, verifyChecksum,
		storageKey, storageProvider, fileSize, mimeType, originalFilename,
	)
	doc, err := scanDocument(row)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("document not found")
	}

	if err != nil {
		logger.Logger.Error("Failed to update document", "error", err.Error(), "doc_id", docID)
		return nil, fmt.Errorf("failed to update document: %w", err)
	}

	return doc, nil
}

// CreateOrUpdate performs upsert operation, creating new document or updating existing one atomically
func (r *DocumentRepository) CreateOrUpdate(ctx context.Context, docID string, input models.DocumentInput, createdBy string) (*models.Document, error) {
	tenantID, err := r.tenants.CurrentTenant(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get tenant: %w", err)
	}

	query := `
		INSERT INTO documents (tenant_id, doc_id, title, url, checksum, checksum_algorithm, description, read_mode, allow_download, require_full_read, verify_checksum, created_by, storage_key, storage_provider, file_size, mime_type, original_filename)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (doc_id) DO UPDATE SET
			title = EXCLUDED.title,
			url = EXCLUDED.url,
			checksum = EXCLUDED.checksum,
			checksum_algorithm = EXCLUDED.checksum_algorithm,
			description = EXCLUDED.description,
			read_mode = EXCLUDED.read_mode,
			allow_download = EXCLUDED.allow_download,
			require_full_read = EXCLUDED.require_full_read,
			verify_checksum = EXCLUDED.verify_checksum,
			storage_key = EXCLUDED.storage_key,
			storage_provider = EXCLUDED.storage_provider,
			file_size = EXCLUDED.file_size,
			mime_type = EXCLUDED.mime_type,
			original_filename = EXCLUDED.original_filename,
			deleted_at = NULL
		RETURNING ` + documentColumns

	checksum := input.Checksum
	checksumAlgorithm := input.ChecksumAlgorithm
	if checksumAlgorithm == "" {
		checksumAlgorithm = "SHA-256"
	}

	readMode := input.ReadMode
	if readMode == "" {
		readMode = "integrated"
	}

	allowDownload := true
	if input.AllowDownload != nil {
		allowDownload = *input.AllowDownload
	}
	requireFullRead := false
	if input.RequireFullRead != nil {
		requireFullRead = *input.RequireFullRead
	}
	verifyChecksum := true
	if input.VerifyChecksum != nil {
		verifyChecksum = *input.VerifyChecksum
	}

	// Handle storage fields
	var storageKey, storageProvider, mimeType, originalFilename sql.NullString
	var fileSize sql.NullInt64
	if input.StorageKey != "" {
		storageKey = sql.NullString{String: input.StorageKey, Valid: true}
	}
	if input.StorageProvider != "" {
		storageProvider = sql.NullString{String: input.StorageProvider, Valid: true}
	}
	if input.FileSize > 0 {
		fileSize = sql.NullInt64{Int64: input.FileSize, Valid: true}
	}
	if input.MimeType != "" {
		mimeType = sql.NullString{String: input.MimeType, Valid: true}
	}
	if input.OriginalFilename != "" {
		originalFilename = sql.NullString{String: input.OriginalFilename, Valid: true}
	}

	row := dbctx.GetQuerier(ctx, r.db).QueryRowContext(
		ctx, query, tenantID, docID, input.Title, input.URL, checksum, checksumAlgorithm,
		input.Description, readMode, allowDownload, requireFullRead, verifyChecksum, createdBy,
		storageKey, storageProvider, fileSize, mimeType, originalFilename,
	)
	doc, err := scanDocument(row)

	if err != nil {
		logger.Logger.Error("Failed to create or update document", "error", err.Error(), "doc_id", docID)
		return nil, fmt.Errorf("failed to create or update document: %w", err)
	}

	return doc, nil
}

// Delete soft-deletes document by setting deleted_at timestamp, preserving metadata and signature history
// RLS policy automatically filters by tenant_id
func (r *DocumentRepository) Delete(ctx context.Context, docID string) error {
	query := `UPDATE documents SET deleted_at = now() WHERE doc_id = $1 AND deleted_at IS NULL`

	result, err := dbctx.GetQuerier(ctx, r.db).ExecContext(ctx, query, docID)
	if err != nil {
		logger.Logger.Error("Failed to delete document", "error", err.Error(), "doc_id", docID)
		return fmt.Errorf("failed to delete document: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("document not found or already deleted")
	}

	return nil
}

// scanDocumentRows scans multiple rows into Document models
func scanDocumentRows(rows *sql.Rows) ([]*models.Document, error) {
	documents := []*models.Document{}
	for rows.Next() {
		doc := &models.Document{}
		var storageKey, storageProvider, mimeType, originalFilename sql.NullString
		var fileSize sql.NullInt64

		err := rows.Scan(
			&doc.DocID, &doc.TenantID, &doc.Title, &doc.URL,
			&doc.Checksum, &doc.ChecksumAlgorithm, &doc.Description, &doc.ReadMode,
			&doc.AllowDownload, &doc.RequireFullRead, &doc.VerifyChecksum,
			&doc.CreatedAt, &doc.UpdatedAt, &doc.CreatedBy, &doc.DeletedAt,
			&storageKey, &storageProvider, &fileSize, &mimeType, &originalFilename,
		)
		if err != nil {
			return nil, err
		}

		doc.StorageKey = storageKey.String
		doc.StorageProvider = storageProvider.String
		doc.FileSize = fileSize.Int64
		doc.MimeType = mimeType.String
		doc.OriginalFilename = originalFilename.String
		documents = append(documents, doc)
	}
	return documents, rows.Err()
}

// List retrieves paginated documents ordered by creation date, newest first (excluding soft-deleted)
// RLS policy automatically filters by tenant_id
func (r *DocumentRepository) List(ctx context.Context, limit, offset int) ([]*models.Document, error) {
	query := `SELECT ` + documentColumns + ` FROM documents WHERE deleted_at IS NULL ORDER BY created_at DESC LIMIT $1 OFFSET $2`

	rows, err := dbctx.GetQuerier(ctx, r.db).QueryContext(ctx, query, limit, offset)
	if err != nil {
		logger.Logger.Error("Failed to list documents", "error", err.Error())
		return nil, fmt.Errorf("failed to list documents: %w", err)
	}
	defer rows.Close()

	documents, err := scanDocumentRows(rows)
	if err != nil {
		logger.Logger.Error("Failed to scan document rows", "error", err.Error())
		return nil, fmt.Errorf("failed to scan documents: %w", err)
	}

	return documents, nil
}

// Search retrieves paginated documents matching the search query (excluding soft-deleted)
// Searches in doc_id, title, url, and description fields using case-insensitive pattern matching
// RLS policy automatically filters by tenant_id
func (r *DocumentRepository) Search(ctx context.Context, query string, limit, offset int) ([]*models.Document, error) {
	searchQuery := `SELECT ` + documentColumns + ` FROM documents WHERE deleted_at IS NULL AND (doc_id ILIKE $1 OR title ILIKE $1 OR url ILIKE $1 OR description ILIKE $1) ORDER BY created_at DESC LIMIT $2 OFFSET $3`

	searchPattern := "%" + query + "%"
	rows, err := dbctx.GetQuerier(ctx, r.db).QueryContext(ctx, searchQuery, searchPattern, limit, offset)
	if err != nil {
		logger.Logger.Error("Failed to search documents", "error", err.Error(), "query", query)
		return nil, fmt.Errorf("failed to search documents: %w", err)
	}
	defer rows.Close()

	documents, err := scanDocumentRows(rows)
	if err != nil {
		logger.Logger.Error("Failed to scan document rows", "error", err.Error())
		return nil, fmt.Errorf("failed to scan documents: %w", err)
	}

	logger.Logger.Debug("Document search completed", "query", query, "results", len(documents), "limit", limit, "offset", offset)
	return documents, nil
}

// Count returns the total number of documents matching the optional search query (excluding soft-deleted)
func (r *DocumentRepository) Count(ctx context.Context, searchQuery string) (int, error) {
	var query string
	var args []interface{}

	if searchQuery != "" {
		// Count with search filter
		query = `
			SELECT COUNT(*)
			FROM documents
			WHERE deleted_at IS NULL
			AND (
				doc_id ILIKE $1
				OR title ILIKE $1
				OR url ILIKE $1
				OR description ILIKE $1
			)
		`
		searchPattern := "%" + searchQuery + "%"
		args = []interface{}{searchPattern}
	} else {
		// Count all documents
		query = `
			SELECT COUNT(*)
			FROM documents
			WHERE deleted_at IS NULL
		`
		args = []interface{}{}
	}

	var count int
	err := dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		logger.Logger.Error("Failed to count documents", "error", err.Error(), "search", searchQuery)
		return 0, fmt.Errorf("failed to count documents: %w", err)
	}

	logger.Logger.Debug("Document count completed", "count", count, "search", searchQuery)
	return count, nil
}

// ListByCreatedBy retrieves paginated documents created by a specific user (excluding soft-deleted)
// RLS policy automatically filters by tenant_id
func (r *DocumentRepository) ListByCreatedBy(ctx context.Context, createdBy string, limit, offset int) ([]*models.Document, error) {
	query := `SELECT ` + documentColumns + ` FROM documents WHERE deleted_at IS NULL AND created_by = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`

	rows, err := dbctx.GetQuerier(ctx, r.db).QueryContext(ctx, query, createdBy, limit, offset)
	if err != nil {
		logger.Logger.Error("Failed to list documents by creator", "error", err.Error(), "created_by", createdBy)
		return nil, fmt.Errorf("failed to list documents: %w", err)
	}
	defer rows.Close()

	documents, err := scanDocumentRows(rows)
	if err != nil {
		logger.Logger.Error("Failed to scan document rows", "error", err.Error())
		return nil, fmt.Errorf("failed to scan documents: %w", err)
	}

	return documents, nil
}

// SearchByCreatedBy retrieves paginated documents matching search query created by a specific user (excluding soft-deleted)
// RLS policy automatically filters by tenant_id
func (r *DocumentRepository) SearchByCreatedBy(ctx context.Context, createdBy, searchQuery string, limit, offset int) ([]*models.Document, error) {
	query := `SELECT ` + documentColumns + ` FROM documents WHERE deleted_at IS NULL AND created_by = $1 AND (doc_id ILIKE $2 OR title ILIKE $2 OR url ILIKE $2 OR description ILIKE $2) ORDER BY created_at DESC LIMIT $3 OFFSET $4`

	searchPattern := "%" + searchQuery + "%"
	rows, err := dbctx.GetQuerier(ctx, r.db).QueryContext(ctx, query, createdBy, searchPattern, limit, offset)
	if err != nil {
		logger.Logger.Error("Failed to search documents by creator", "error", err.Error(), "created_by", createdBy, "query", searchQuery)
		return nil, fmt.Errorf("failed to search documents: %w", err)
	}
	defer rows.Close()

	documents, err := scanDocumentRows(rows)
	if err != nil {
		logger.Logger.Error("Failed to scan document rows", "error", err.Error())
		return nil, fmt.Errorf("failed to scan documents: %w", err)
	}

	logger.Logger.Debug("Document search by creator completed", "created_by", createdBy, "query", searchQuery, "results", len(documents), "limit", limit, "offset", offset)
	return documents, nil
}

// CountByCreatedBy returns the total number of documents created by a specific user (excluding soft-deleted)
func (r *DocumentRepository) CountByCreatedBy(ctx context.Context, createdBy, searchQuery string) (int, error) {
	var query string
	var args []interface{}

	if searchQuery != "" {
		query = `
			SELECT COUNT(*)
			FROM documents
			WHERE deleted_at IS NULL AND created_by = $1
			AND (
				doc_id ILIKE $2
				OR title ILIKE $2
				OR url ILIKE $2
				OR description ILIKE $2
			)
		`
		searchPattern := "%" + searchQuery + "%"
		args = []interface{}{createdBy, searchPattern}
	} else {
		query = `
			SELECT COUNT(*)
			FROM documents
			WHERE deleted_at IS NULL AND created_by = $1
		`
		args = []interface{}{createdBy}
	}

	var count int
	err := dbctx.GetQuerier(ctx, r.db).QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		logger.Logger.Error("Failed to count documents by creator", "error", err.Error(), "created_by", createdBy, "search", searchQuery)
		return 0, fmt.Errorf("failed to count documents: %w", err)
	}

	logger.Logger.Debug("Document count by creator completed", "count", count, "created_by", createdBy, "search", searchQuery)
	return count, nil
}
