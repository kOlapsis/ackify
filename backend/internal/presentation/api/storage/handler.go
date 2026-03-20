// SPDX-License-Identifier: AGPL-3.0-or-later
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kolapsis/ackify/backend/internal/application/services"
	"github.com/kolapsis/ackify/backend/internal/presentation/api/shared"
	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/models"
	"github.com/kolapsis/ackify/backend/pkg/storage"
)

// extensionMIMETypes maps file extensions to their correct MIME types
// Used to override incorrect detection from http.DetectContentType
var extensionMIMETypes = map[string]string{
	// Text formats (detected as text/plain)
	".md":       "text/markdown",
	".markdown": "text/markdown",
	".txt":      "text/plain",

	// XML-based formats (detected as text/xml)
	".html": "text/html",
	".htm":  "text/html",
	".svg":  "image/svg+xml",

	// ZIP-based formats (detected as application/zip)
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".odt":  "application/vnd.oasis.opendocument.text",
	".ods":  "application/vnd.oasis.opendocument.spreadsheet",

	// Image formats (fallback for small/malformed files detected as application/octet-stream)
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// ambiguousDetectedTypes lists MIME types from http.DetectContentType that
// should be refined using file extension
var ambiguousDetectedTypes = map[string]bool{
	"text/plain":               true, // .md, .txt detected as this
	"text/xml":                 true, // .svg, .html sometimes detected as this
	"application/octet-stream": true, // fallback type
	"application/zip":          true, // .docx, .xlsx, .odt, .ods detected as this
	"application/x-zip":        true, // alternative zip type
	"application/x-gzip":       true, // some zip variants
}

// isTextBasedMIME returns true if the MIME type is text-based and needs charset
func isTextBasedMIME(mimeType string) bool {
	return strings.HasPrefix(mimeType, "text/") ||
		mimeType == "application/json" ||
		mimeType == "application/xml" ||
		mimeType == "image/svg+xml"
}

// refineContentType improves content type detection using file extension
// when content detection returns an ambiguous type
func refineContentType(detectedType, filename string) string {
	// Extract base MIME type without parameters
	baseType := strings.Split(detectedType, ";")[0]
	baseType = strings.TrimSpace(baseType)

	// If detection gave a specific non-ambiguous type, trust it
	if !ambiguousDetectedTypes[baseType] {
		return baseType
	}

	// For ambiguous types, use file extension to determine correct type
	ext := strings.ToLower(filepath.Ext(filename))
	if mimeType, ok := extensionMIMETypes[ext]; ok {
		return mimeType
	}

	return baseType
}

type documentService interface {
	CreateDocument(ctx context.Context, req services.CreateDocumentRequest) (*models.Document, error)
	GetByDocID(ctx context.Context, docID string) (*models.Document, error)
}

// StorageQuotaChecker verifies storage quota before upload.
type StorageQuotaChecker interface {
	CheckStorageQuota(ctx context.Context, tenantID string, fileSize int64) error
}

type Handler struct {
	provider     storage.Provider
	docService   documentService
	maxSizeMB    int64
	quotaChecker StorageQuotaChecker
	tenantID     func(ctx context.Context) string
}

func NewHandler(provider storage.Provider, docService documentService, maxSizeMB int64) *Handler {
	return &Handler{
		provider:   provider,
		docService: docService,
		maxSizeMB:  maxSizeMB,
	}
}

// WithStorageQuotaChecker sets the storage quota checker.
func (h *Handler) WithStorageQuotaChecker(checker StorageQuotaChecker, getTenantID func(ctx context.Context) string) *Handler {
	h.quotaChecker = checker
	h.tenantID = getTenantID
	return h
}

func (h *Handler) IsEnabled() bool {
	return h.provider != nil
}

type UploadResponse struct {
	DocID             string    `json:"doc_id"`
	Title             string    `json:"title"`
	StorageKey        string    `json:"storage_key"`
	StorageProvider   string    `json:"storage_provider"`
	FileSize          int64     `json:"file_size"`
	MimeType          string    `json:"mime_type"`
	Checksum          string    `json:"checksum"`
	ChecksumAlgorithm string    `json:"checksum_algorithm"`
	CreatedAt         time.Time `json:"created_at"`
	IsNew             bool      `json:"is_new"`
}

func (h *Handler) HandleUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if h.provider == nil {
		shared.WriteError(w, http.StatusServiceUnavailable, shared.ErrCodeServiceUnavailable, "Storage is not configured", nil)
		return
	}

	// Get current user
	user, ok := shared.GetUserFromContext(ctx)
	if !ok || user == nil {
		shared.WriteUnauthorized(w, "Authentication required")
		return
	}

	// Parse multipart form with size limit
	maxSize := h.maxSizeMB * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxSize)

	if err := r.ParseMultipartForm(maxSize); err != nil {
		if strings.Contains(err.Error(), "too large") {
			shared.WriteError(w, http.StatusRequestEntityTooLarge, shared.ErrCodeBadRequest,
				fmt.Sprintf("File too large. Maximum size is %d MB", h.maxSizeMB), nil)
			return
		}
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Failed to parse form", nil)
		return
	}

	// Get file from form
	file, header, err := r.FormFile("file")
	if err != nil {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest, "Missing file in request", nil)
		return
	}
	defer file.Close()

	// Check storage quota before upload
	if h.quotaChecker != nil && h.tenantID != nil {
		tenantID := h.tenantID(ctx)
		if tenantID != "" {
			if err := h.quotaChecker.CheckStorageQuota(ctx, tenantID, header.Size); err != nil {
				shared.WriteError(w, http.StatusForbidden, shared.ErrCodeForbidden, "Storage quota exceeded", nil)
				return
			}
		}
	}

	// Get optional title from form
	title := r.FormValue("title")
	if title == "" {
		title = header.Filename
	}

	// Get reader options from form
	readMode := r.FormValue("readMode")
	if readMode == "" {
		readMode = "integrated"
	}
	allowDownload := r.FormValue("allowDownload") == "true"
	requireFullRead := r.FormValue("requireFullRead") == "true"
	verifyChecksum := r.FormValue("verifyChecksum") != "false" // default true

	// Detect content type from file content
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		shared.WriteInternalError(w)
		return
	}
	detectedType := http.DetectContentType(buffer[:n])

	// Refine content type using file extension for text-based formats
	contentType := refineContentType(detectedType, header.Filename)

	// Validate content type
	if !storage.IsAllowedMIMEType(contentType) {
		shared.WriteError(w, http.StatusBadRequest, shared.ErrCodeBadRequest,
			fmt.Sprintf("File type not allowed: %s", contentType), nil)
		return
	}

	// Reset file position
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		shared.WriteInternalError(w)
		return
	}

	// Generate storage key (unique per upload)
	storageKey := generateStorageKey(header.Filename)

	// Calculate checksum while reading
	hasher := sha256.New()
	teeReader := io.TeeReader(file, hasher)

	// Upload to storage
	if err := h.provider.Upload(ctx, storageKey, teeReader, header.Size, contentType); err != nil {
		logger.Logger.Error("Failed to upload file", "error", err.Error(), "key", storageKey)
		shared.WriteInternalError(w)
		return
	}

	// Calculate final checksum
	checksum := hex.EncodeToString(hasher.Sum(nil))

	// Create document with storage info
	doc, err := h.docService.CreateDocument(ctx, services.CreateDocumentRequest{
		Reference:         storageKey,
		Title:             title,
		CreatedBy:         user.Email,
		ReadMode:          readMode,
		AllowDownload:     &allowDownload,
		RequireFullRead:   &requireFullRead,
		VerifyChecksum:    &verifyChecksum,
		StorageKey:        storageKey,
		StorageProvider:   h.provider.Type(),
		FileSize:          header.Size,
		MimeType:          contentType,
		Checksum:          checksum,
		ChecksumAlgorithm: "SHA-256",
		OriginalFilename:  header.Filename,
	})
	if err != nil {
		// Cleanup uploaded file on document creation failure
		if delErr := h.provider.Delete(ctx, storageKey); delErr != nil {
			logger.Logger.Error("Failed to cleanup uploaded file after document creation failure", "error", delErr.Error(), "key", storageKey)
		}
		logger.Logger.Error("Failed to create document", "error", err.Error())
		shared.WriteInternalError(w)
		return
	}

	logger.Logger.Info("File uploaded and document created",
		"doc_id", doc.DocID,
		"storage_key", storageKey,
		"size", header.Size,
		"mime_type", contentType,
		"user", user.Email)

	shared.WriteJSON(w, http.StatusCreated, UploadResponse{
		DocID:             doc.DocID,
		Title:             doc.Title,
		StorageKey:        doc.StorageKey,
		StorageProvider:   doc.StorageProvider,
		FileSize:          doc.FileSize,
		MimeType:          doc.MimeType,
		Checksum:          doc.Checksum,
		ChecksumAlgorithm: doc.ChecksumAlgorithm,
		CreatedAt:         doc.CreatedAt,
		IsNew:             true,
	})
}

func (h *Handler) HandleContent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	docID := chi.URLParam(r, "docId")

	if h.provider == nil {
		shared.WriteError(w, http.StatusServiceUnavailable, shared.ErrCodeServiceUnavailable, "Storage is not configured", nil)
		return
	}

	// Get document
	doc, err := h.docService.GetByDocID(ctx, docID)
	if err != nil {
		logger.Logger.Error("Failed to get document", "error", err.Error(), "doc_id", docID)
		shared.WriteInternalError(w)
		return
	}

	if doc == nil {
		shared.WriteNotFound(w, "Document")
		return
	}

	// Check if document has stored content
	if !doc.IsStored() {
		shared.WriteNotFound(w, "Document content")
		return
	}

	// Check if storage provider matches
	if doc.StorageProvider != h.provider.Type() {
		shared.WriteError(w, http.StatusNotFound, shared.ErrCodeNotFound, "Document stored in different storage provider", nil)
		return
	}

	// Download from storage
	reader, size, contentType, err := h.provider.Download(ctx, doc.StorageKey)
	if err != nil {
		logger.Logger.Error("Failed to download file", "error", err.Error(), "key", doc.StorageKey)
		shared.WriteInternalError(w)
		return
	}
	defer reader.Close()

	// Use stored mime type if available
	if doc.MimeType != "" {
		contentType = doc.MimeType
	}

	// Set security headers for iframe embedding (same origin only)
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")

	// For PDFs, don't set restrictive CSP as the browser's PDF viewer needs full control
	// For other content types, use restrictive CSP
	if contentType != "application/pdf" {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:")
	}

	// Set content headers with charset for text-based formats
	finalContentType := contentType
	if isTextBasedMIME(contentType) {
		finalContentType = contentType + "; charset=utf-8"
	}
	w.Header().Set("Content-Type", finalContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))

	// Set content disposition based on query param
	disposition := "inline"
	if r.URL.Query().Get("download") == "true" {
		disposition = "attachment"
	}

	// Use original filename if available, otherwise fallback to title or storage key
	filename := doc.OriginalFilename
	if filename == "" {
		filename = doc.Title
		if filename == "" {
			parts := strings.Split(doc.StorageKey, "/")
			filename = parts[len(parts)-1]
		}
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, filename))

	// Stream content
	if _, err := io.Copy(w, reader); err != nil {
		logger.Logger.Error("Failed to stream file", "error", err.Error(), "key", doc.StorageKey)
	}
}

func (h *Handler) HandleStorageConfig(w http.ResponseWriter, r *http.Request) {
	response := map[string]interface{}{
		"enabled":     h.IsEnabled(),
		"max_size_mb": h.maxSizeMB,
	}

	if h.IsEnabled() {
		response["provider"] = h.provider.Type()
		response["allowed_types"] = getAllowedMIMETypes()
	}

	shared.WriteJSON(w, http.StatusOK, response)
}

func generateStorageKey(filename string) string {
	timestamp := time.Now().UnixNano()
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d-%s", timestamp, filename)))
	shortHash := hex.EncodeToString(hash[:8])

	// Extract extension
	ext := ""
	if idx := strings.LastIndex(filename, "."); idx != -1 {
		ext = filename[idx:]
	}

	// Format: YYYY/MM/DD/hash.ext
	now := time.Now()
	return fmt.Sprintf("%d/%02d/%02d/%s%s", now.Year(), now.Month(), now.Day(), shortHash, ext)
}

func getAllowedMIMETypes() []string {
	types := make([]string, 0, len(storage.AllowedMIMETypes))
	for t := range storage.AllowedMIMETypes {
		types = append(types, t)
	}
	return types
}
