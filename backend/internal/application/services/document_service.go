// SPDX-License-Identifier: AGPL-3.0-or-later
package services

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/kolapsis/ackify/backend/pkg/checksum"
	"github.com/kolapsis/ackify/backend/pkg/config"
	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/models"
)

var (
	// ErrDocumentAlreadyExists is returned when trying to create a document with a doc_id that already exists (e.g. soft-deleted)
	ErrDocumentAlreadyExists = errors.New("document already exists")
	ErrHTTPSRequired         = errors.New("HTTPS is required for document URLs")
	ErrDomainNotAllowed      = errors.New("domain not in allowed list")
)

type documentRepository interface {
	Create(ctx context.Context, docID string, input models.DocumentInput, createdBy string) (*models.Document, error)
	GetByDocID(ctx context.Context, docID string) (*models.Document, error)
	FindByReference(ctx context.Context, ref string, refType string) (*models.Document, error)
	List(ctx context.Context, limit, offset int) ([]*models.Document, error)
	Search(ctx context.Context, query string, limit, offset int) ([]*models.Document, error)
	Count(ctx context.Context, searchQuery string) (int, error)
	ListByCreatedBy(ctx context.Context, createdBy string, limit, offset int) ([]*models.Document, error)
	SearchByCreatedBy(ctx context.Context, createdBy, searchQuery string, limit, offset int) ([]*models.Document, error)
	CountByCreatedBy(ctx context.Context, createdBy, searchQuery string) (int, error)
}

type docExpectedSignerRepository interface {
	ListByDocID(ctx context.Context, docID string) ([]*models.ExpectedSigner, error)
	GetStats(ctx context.Context, docID string) (*models.DocCompletionStats, error)
	GetAggregateDocumentStats(ctx context.Context, createdBy string) (pending, completed int, err error)
	FindPendingForEmail(ctx context.Context, email string) ([]*models.PendingDocument, error)
}

type docConfigProvider interface {
	GetConfig() *models.MutableConfig
}

// DocumentService handles document metadata operations and unique ID generation
type DocumentService struct {
	repo               documentRepository
	expectedSignerRepo docExpectedSignerRepository
	checksumConfig     *config.ChecksumConfig
	configProvider     docConfigProvider
}

// NewDocumentService initializes the document service with its repository dependency
func NewDocumentService(repo documentRepository, expectedSignerRepo docExpectedSignerRepository, checksumConfig *config.ChecksumConfig) *DocumentService {
	return &DocumentService{
		repo:               repo,
		expectedSignerRepo: expectedSignerRepo,
		checksumConfig:     checksumConfig,
	}
}

// WithConfigProvider sets the config provider for hot-reloaded settings (e.g. domain whitelist)
func (s *DocumentService) WithConfigProvider(cp docConfigProvider) *DocumentService {
	s.configProvider = cp
	return s
}

// CreateDocumentRequest represents the request to create a document
type CreateDocumentRequest struct {
	Reference string `json:"reference" validate:"required,min=1"`
	Title     string `json:"title"`
	CreatedBy string `json:"created_by,omitempty"`

	// Reader options
	ReadMode        string `json:"read_mode,omitempty"`
	AllowDownload   *bool  `json:"allow_download,omitempty"`
	RequireFullRead *bool  `json:"require_full_read,omitempty"`
	VerifyChecksum  *bool  `json:"verify_checksum,omitempty"`

	// Storage fields for uploaded files
	StorageKey        string `json:"storage_key,omitempty"`
	StorageProvider   string `json:"storage_provider,omitempty"`
	FileSize          int64  `json:"file_size,omitempty"`
	MimeType          string `json:"mime_type,omitempty"`
	Checksum          string `json:"checksum,omitempty"`
	ChecksumAlgorithm string `json:"checksum_algorithm,omitempty"`
	OriginalFilename  string `json:"original_filename,omitempty"`
}

// ValidateDocumentURL enforces HTTPS and optional domain whitelist on document URLs.
// HTTPS is always required. Domain whitelist is only enforced when allowedDomains is non-empty.
func ValidateDocumentURL(urlStr string, allowedDomains []string) error {
	if !strings.HasPrefix(urlStr, "https://") {
		return ErrHTTPSRequired
	}

	if len(allowedDomains) == 0 {
		return nil
	}

	// Extract hostname from URL
	hostname := urlStr[len("https://"):]
	if idx := strings.IndexAny(hostname, "/:?#"); idx >= 0 {
		hostname = hostname[:idx]
	}
	hostname = strings.ToLower(hostname)

	for _, pattern := range allowedDomains {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if matchDomain(hostname, pattern) {
			return nil
		}
	}

	return fmt.Errorf("%w: %s", ErrDomainNotAllowed, hostname)
}

func matchDomain(hostname, pattern string) bool {
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // e.g. ".company.com"
		return hostname == pattern[2:] || strings.HasSuffix(hostname, suffix)
	}
	return hostname == pattern
}

// getActiveAllowedDomains returns the current allowed domains from config provider
func (s *DocumentService) getActiveAllowedDomains() []string {
	if s.configProvider == nil {
		return nil
	}
	cfg := s.configProvider.GetConfig()
	if cfg == nil {
		return nil
	}
	return cfg.General.AllowedDomains
}

// validateURLIfNeeded validates the URL against HTTPS and domain whitelist rules
func (s *DocumentService) validateURLIfNeeded(ref string) error {
	if !strings.HasPrefix(ref, "http://") && !strings.HasPrefix(ref, "https://") {
		return nil
	}
	return ValidateDocumentURL(ref, s.getActiveAllowedDomains())
}

// CreateDocument generates a collision-resistant base36 identifier and persists document metadata
func (s *DocumentService) CreateDocument(ctx context.Context, req CreateDocumentRequest) (*models.Document, error) {
	logger.Logger.Info("Document creation attempt", "reference", req.Reference)

	if err := s.validateURLIfNeeded(req.Reference); err != nil {
		return nil, err
	}

	var docID string
	maxRetries := 5
	for i := 0; i < maxRetries; i++ {
		docID = generateDocID()

		existing, err := s.repo.GetByDocID(ctx, docID)
		if err != nil {
			return nil, fmt.Errorf("failed to check doc_id uniqueness: %w", err)
		}

		if existing == nil {
			break
		}

		logger.Logger.Debug("Generated doc_id already exists, retrying",
			"doc_id", docID, "attempt", i+1)
	}

	var url, title string
	if strings.HasPrefix(req.Reference, "http://") || strings.HasPrefix(req.Reference, "https://") {
		url = req.Reference

		if req.Title == "" {
			title = extractTitleFromURL(req.Reference)
		} else {
			title = req.Title
		}
	} else {
		if req.Title == "" {
			title = req.Reference
		} else {
			title = req.Title
		}
	}

	input := models.DocumentInput{
		Title:           title,
		URL:             url,
		ReadMode:        req.ReadMode,
		AllowDownload:   req.AllowDownload,
		RequireFullRead: req.RequireFullRead,
		VerifyChecksum:  req.VerifyChecksum,
	}

	// Handle storage fields if provided (for uploaded files)
	if req.StorageKey != "" {
		input.StorageKey = req.StorageKey
		input.StorageProvider = req.StorageProvider
		input.FileSize = req.FileSize
		input.MimeType = req.MimeType
		input.OriginalFilename = req.OriginalFilename
		// Use provided checksum for uploaded files
		if req.Checksum != "" {
			input.Checksum = req.Checksum
			input.ChecksumAlgorithm = req.ChecksumAlgorithm
			if input.ChecksumAlgorithm == "" {
				input.ChecksumAlgorithm = "SHA-256"
			}
		}
	} else if (strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")) && s.checksumConfig != nil {
		// Automatically compute checksum for remote URLs if enabled
		checksumResult := s.computeChecksumForURL(ctx, url)
		if checksumResult != nil {
			input.Checksum = checksumResult.ChecksumHex
			input.ChecksumAlgorithm = checksumResult.Algorithm
			logger.Logger.Info("Automatically computed checksum for document",
				"doc_id", docID,
				"checksum", checksumResult.ChecksumHex,
				"algorithm", checksumResult.Algorithm)
		}
	}

	// Use provided createdBy or empty string
	createdBy := req.CreatedBy

	doc, err := s.repo.Create(ctx, docID, input, createdBy)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "23505") {
			logger.Logger.Warn("Document already exists (possibly soft-deleted)",
				"doc_id", docID)
			return nil, ErrDocumentAlreadyExists
		}
		logger.Logger.Error("Failed to create document",
			"doc_id", docID,
			"error", err.Error())
		return nil, fmt.Errorf("failed to create document: %w", err)
	}

	logger.Logger.Info("Document created successfully",
		"doc_id", docID,
		"url", url,
		"title", title)

	return doc, nil
}

func generateDocID() string {
	timestamp := time.Now().Unix()
	timestampB36 := strconv.FormatInt(timestamp, 36)

	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	const suffixLen = 4

	suffix := make([]byte, suffixLen)
	for i := range suffix {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			suffix[i] = charset[(int(timestamp)+i)%len(charset)]
		} else {
			suffix[i] = charset[n.Int64()]
		}
	}

	return timestampB36 + string(suffix)
}

func extractTitleFromURL(urlStr string) string {
	urlStr = strings.TrimRight(urlStr, "/")

	urlStr = strings.TrimPrefix(urlStr, "http://")
	urlStr = strings.TrimPrefix(urlStr, "https://")

	parts := strings.Split(urlStr, "/")

	if len(parts) == 0 {
		return urlStr
	}

	var lastSegment string
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			lastSegment = parts[i]
			break
		}
	}

	if lastSegment == "" {
		if len(parts) > 0 && parts[0] != "" {
			return parts[0]
		}
		return urlStr
	}

	// Remove query parameters
	if idx := strings.Index(lastSegment, "?"); idx >= 0 {
		lastSegment = lastSegment[:idx]
	}

	// Remove fragment
	if idx := strings.Index(lastSegment, "#"); idx >= 0 {
		lastSegment = lastSegment[:idx]
	}

	// Remove file extension
	if idx := strings.LastIndex(lastSegment, "."); idx > 0 {
		lastSegment = lastSegment[:idx]
	}

	// Clean up hash/ID suffixes (Notion, GitHub, GitLab, etc.)
	lastSegment = cleanHashSuffix(lastSegment)

	return lastSegment
}

// cleanHashSuffix removes common hash/ID patterns appended by various platforms
// Examples:
//   - "Introduction-to-Cybersecurity-26b2915834718093a062f54c798d63c5" -> "Introduction-to-Cybersecurity"
//   - "My-Document-abc123def456" -> "My-Document"
//   - "Report-2024-1a2b3c4d5e6f" -> "Report-2024"
func cleanHashSuffix(title string) string {
	// Pattern 1: Remove UUID-like suffixes (with dashes) - check this first before splitting
	// Example: "Title-a1b2c3d4-e5f6-7890-abcd-ef1234567890" -> "Title"
	// UUID format: 8-4-4-4-12 = 36 chars total with dashes
	parts := strings.Split(title, "-")
	if len(parts) >= 6 {
		// Check if last 5 segments form a UUID pattern
		potentialUUID := strings.Join(parts[len(parts)-5:], "-")
		cleanUUID := strings.ReplaceAll(potentialUUID, "-", "")
		if len(cleanUUID) == 32 && isHexString(cleanUUID) {
			return strings.Join(parts[:len(parts)-5], "-")
		}
	}

	// Pattern 2: Remove long hexadecimal suffixes (24+ chars) - Notion style
	// Example: "Title-26b2915834718093a062f54c798d63c5" -> "Title"
	if idx := strings.LastIndex(title, "-"); idx > 0 {
		suffix := title[idx+1:]
		if len(suffix) >= 24 && isHexString(suffix) {
			return title[:idx]
		}
	}

	// Pattern 3: Remove short hash suffixes (8-16 chars) only if alphanumeric
	// Example: "Document-abc123def" -> "Document"
	if idx := strings.LastIndex(title, "-"); idx > 0 {
		suffix := title[idx+1:]
		if len(suffix) >= 8 && len(suffix) <= 16 && isAlphanumeric(suffix) && hasLettersAndNumbers(suffix) {
			return title[:idx]
		}
	}

	// Pattern 4: Remove numeric-only suffixes (timestamps, IDs) 8+ digits
	// Example: "Article-1234567890" -> "Article"
	if idx := strings.LastIndex(title, "-"); idx > 0 {
		suffix := title[idx+1:]
		if len(suffix) >= 8 && isNumericString(suffix) {
			return title[:idx]
		}
	}

	// Pattern 5: Remove base64-like suffixes (URL-safe base64)
	// Example: "Page-aGVsbG93b3JsZA" -> "Page"
	if idx := strings.LastIndex(title, "-"); idx > 0 {
		suffix := title[idx+1:]
		if len(suffix) >= 12 && isBase64Like(suffix) {
			return title[:idx]
		}
	}

	return title
}

// isHexString checks if a string contains only hexadecimal characters (0-9, a-f, A-F)
func isHexString(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, ch := range s {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
			return false
		}
	}
	return true
}

// isAlphanumeric checks if string contains only letters and numbers
func isAlphanumeric(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, ch := range s {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')) {
			return false
		}
	}
	return true
}

// hasLettersAndNumbers checks if string contains both letters AND numbers (likely a hash)
func hasLettersAndNumbers(s string) bool {
	hasLetter := false
	hasNumber := false
	for _, ch := range s {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
			hasLetter = true
		}
		if ch >= '0' && ch <= '9' {
			hasNumber = true
		}
	}
	return hasLetter && hasNumber
}

// isNumericString checks if string contains only digits
func isNumericString(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

// isBase64Like checks if string looks like base64 encoding
func isBase64Like(s string) bool {
	if len(s) == 0 {
		return false
	}
	base64Chars := 0
	for _, ch := range s {
		if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_' || ch == '-' {
			base64Chars++
		}
	}
	// If 90%+ of chars are base64-compatible, likely base64
	return float64(base64Chars)/float64(len(s)) >= 0.9
}

// computeChecksumForURL attempts to compute the checksum for a remote URL
// Returns nil if the checksum cannot be computed (error, too large, etc.)
func (s *DocumentService) computeChecksumForURL(ctx context.Context, url string) *checksum.Result {
	if s.checksumConfig == nil {
		return nil
	}

	opts := checksum.ComputeOptions{
		MaxBytes:           s.checksumConfig.MaxBytes,
		TimeoutMs:          s.checksumConfig.TimeoutMs,
		MaxRedirects:       s.checksumConfig.MaxRedirects,
		AllowedContentType: s.checksumConfig.AllowedContentType,
		SkipSSRFCheck:      s.checksumConfig.SkipSSRFCheck,
		InsecureSkipVerify: s.checksumConfig.InsecureSkipVerify,
	}

	result, err := checksum.ComputeRemoteChecksum(ctx, url, opts)
	if err != nil {
		logger.Logger.Warn("Failed to compute checksum for URL",
			"url", url,
			"error", err.Error())
		return nil
	}

	return result
}

type ReferenceType string

const (
	ReferenceTypeURL       ReferenceType = "url"
	ReferenceTypePath      ReferenceType = "path"
	ReferenceTypeReference ReferenceType = "reference"
)

func detectReferenceType(ref string) ReferenceType {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ReferenceTypeURL
	}

	if strings.Contains(ref, "/") || strings.Contains(ref, "\\") {
		return ReferenceTypePath
	}

	return ReferenceTypeReference
}

// FindByReference finds a document by its reference without creating it
func (s *DocumentService) FindByReference(ctx context.Context, ref string, refType string) (*models.Document, error) {
	doc, err := s.repo.FindByReference(ctx, ref, refType)
	if err != nil {
		return nil, err
	}
	return doc, nil
}

// FindOrCreateDocument performs smart lookup by URL/path/reference or creates new document if not found
func (s *DocumentService) FindOrCreateDocument(ctx context.Context, ref string, createdBy string) (*models.Document, bool, error) {
	logger.Logger.Info("Find or create document", "reference", ref, "created_by", createdBy)

	refType := detectReferenceType(ref)
	logger.Logger.Debug("Reference type detected", "type", refType, "reference", ref)

	doc, err := s.repo.FindByReference(ctx, ref, string(refType))
	if err != nil {
		logger.Logger.Error("Error searching for document", "reference", ref, "error", err.Error())
		return nil, false, fmt.Errorf("failed to search for document: %w", err)
	}

	if doc != nil {
		logger.Logger.Info("Document found", "doc_id", doc.DocID, "reference", ref)
		return doc, false, nil
	}

	logger.Logger.Info("Document not found, creating new one", "reference", ref, "created_by", createdBy)

	if err := s.validateURLIfNeeded(ref); err != nil {
		return nil, false, err
	}

	var title string
	switch refType {
	case ReferenceTypeURL:
		title = extractTitleFromURL(ref)
	case ReferenceTypePath:
		title = extractTitleFromURL(ref)
	case ReferenceTypeReference:
		title = ref
	}

	createReq := CreateDocumentRequest{
		Reference: ref,
		Title:     title,
		CreatedBy: createdBy,
	}

	if refType == ReferenceTypeReference {
		input := models.DocumentInput{
			Title: title,
		}

		doc, err := s.repo.Create(ctx, ref, input, createdBy)
		if err != nil {
			// Detect PostgreSQL duplicate key (e.g. soft-deleted document with same doc_id)
			if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "23505") {
				logger.Logger.Warn("Document already exists (possibly soft-deleted)",
					"doc_id", ref)
				return nil, false, ErrDocumentAlreadyExists
			}
			logger.Logger.Error("Failed to create document with custom doc_id",
				"doc_id", ref,
				"error", err.Error())
			return nil, false, fmt.Errorf("failed to create document: %w", err)
		}

		logger.Logger.Info("Document created with custom doc_id",
			"doc_id", ref,
			"title", title,
			"created_by", createdBy)

		return doc, true, nil
	}

	// For URL references, compute checksum before creating
	if refType == ReferenceTypeURL && s.checksumConfig != nil {
		logger.Logger.Debug("Computing checksum for URL reference", "url", ref)
		checksumResult := s.computeChecksumForURL(ctx, ref)
		if checksumResult != nil {
			logger.Logger.Info("Automatically computed checksum for URL reference",
				"url", ref,
				"checksum", checksumResult.ChecksumHex,
				"algorithm", checksumResult.Algorithm)
		}
	}

	doc, err = s.CreateDocument(ctx, createReq)
	if err != nil {
		return nil, false, err
	}

	return doc, true, nil
}

// List retrieves a paginated list of documents
func (s *DocumentService) List(ctx context.Context, limit, offset int) ([]*models.Document, error) {
	return s.repo.List(ctx, limit, offset)
}

// Search performs a search query across documents
func (s *DocumentService) Search(ctx context.Context, query string, limit, offset int) ([]*models.Document, error) {
	return s.repo.Search(ctx, query, limit, offset)
}

// Count returns the total number of documents matching the search query
func (s *DocumentService) Count(ctx context.Context, searchQuery string) (int, error) {
	return s.repo.Count(ctx, searchQuery)
}

// CountDocs returns the current count of documents
func (s *DocumentService) CountDocs(ctx context.Context) int {
	c, err := s.repo.Count(ctx, "")
	if err != nil {
		c = 0
	}
	return c
}

// GetByDocID retrieves a document by its ID
func (s *DocumentService) GetByDocID(ctx context.Context, docID string) (*models.Document, error) {
	return s.repo.GetByDocID(ctx, docID)
}

// GetExpectedSignerStats retrieves completion statistics for expected signers
func (s *DocumentService) GetExpectedSignerStats(ctx context.Context, docID string) (*models.DocCompletionStats, error) {
	if s.expectedSignerRepo == nil {
		return nil, fmt.Errorf("expected signer repository not configured")
	}
	return s.expectedSignerRepo.GetStats(ctx, docID)
}

// GetAggregateDocumentStats returns pending and completed document counts.
func (s *DocumentService) GetAggregateDocumentStats(ctx context.Context, createdBy string) (pending, completed int, err error) {
	if s.expectedSignerRepo == nil {
		return 0, 0, fmt.Errorf("expected signer repository not configured")
	}
	return s.expectedSignerRepo.GetAggregateDocumentStats(ctx, createdBy)
}

// ListExpectedSigners retrieves all expected signers for a document
func (s *DocumentService) ListExpectedSigners(ctx context.Context, docID string) ([]*models.ExpectedSigner, error) {
	if s.expectedSignerRepo == nil {
		return nil, fmt.Errorf("expected signer repository not configured")
	}
	return s.expectedSignerRepo.ListByDocID(ctx, docID)
}

// FindPendingDocumentsForEmail returns documents awaiting confirmation by the given email
func (s *DocumentService) FindPendingDocumentsForEmail(ctx context.Context, email string) ([]*models.PendingDocument, error) {
	if s.expectedSignerRepo == nil {
		return nil, fmt.Errorf("expected signer repository not configured")
	}
	return s.expectedSignerRepo.FindPendingForEmail(ctx, email)
}

// ListByCreatedBy retrieves a paginated list of documents created by a specific user
func (s *DocumentService) ListByCreatedBy(ctx context.Context, createdBy string, limit, offset int) ([]*models.Document, error) {
	return s.repo.ListByCreatedBy(ctx, createdBy, limit, offset)
}

// SearchByCreatedBy performs a search query across documents created by a specific user
func (s *DocumentService) SearchByCreatedBy(ctx context.Context, createdBy, query string, limit, offset int) ([]*models.Document, error) {
	return s.repo.SearchByCreatedBy(ctx, createdBy, query, limit, offset)
}

// CountByCreatedBy returns the total number of documents created by a specific user
func (s *DocumentService) CountByCreatedBy(ctx context.Context, createdBy, searchQuery string) (int, error) {
	return s.repo.CountByCreatedBy(ctx, createdBy, searchQuery)
}
