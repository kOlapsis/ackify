// SPDX-License-Identifier: AGPL-3.0-or-later
package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kolapsis/ackify/backend/pkg/checksum"
	"github.com/kolapsis/ackify/backend/pkg/config"
	"github.com/kolapsis/ackify/backend/pkg/crypto"
	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/models"
)

type repository interface {
	Create(ctx context.Context, signature *models.Signature) error
	GetByDocAndUser(ctx context.Context, docID, userSub string) (*models.Signature, error)
	GetByDoc(ctx context.Context, docID string) ([]*models.Signature, error)
	GetByUserEmail(ctx context.Context, userEmail string) ([]*models.Signature, error)
	ExistsByDocAndUser(ctx context.Context, docID, userSub string) (bool, error)
	CheckUserSignatureStatus(ctx context.Context, docID, userIdentifier string) (bool, error)
	GetLastSignature(ctx context.Context, docID string) (*models.Signature, error)
	GetAllSignaturesOrdered(ctx context.Context) ([]*models.Signature, error)
	UpdatePrevHash(ctx context.Context, id int64, prevHash *string) error
	Count(ctx context.Context) (int, error)
}

type cryptoSigner interface {
	CreateSignature(ctx context.Context, docID string, user *models.User, timestamp time.Time, nonce string, docChecksum string) (string, string, error)
}

// SignatureService orchestrates signature creation with Ed25519 cryptography and hash chain linking
type SignatureService struct {
	repo           repository
	docRepo        documentRepository
	signer         cryptoSigner
	checksumConfig *config.ChecksumConfig
}

// NewSignatureService initializes the signature service with repository and cryptographic signer dependencies
func NewSignatureService(repo repository, docRepo documentRepository, signer cryptoSigner) *SignatureService {
	return &SignatureService{
		repo:    repo,
		docRepo: docRepo,
		signer:  signer,
	}
}

// SetChecksumConfig sets the checksum configuration for document verification
func (s *SignatureService) SetChecksumConfig(cfg *config.ChecksumConfig) {
	s.checksumConfig = cfg
}

// CreateSignature validates user authorization, generates cryptographic proof, and chains to previous signature
func (s *SignatureService) CreateSignature(ctx context.Context, request *models.SignatureRequest) error {
	logger.Logger.Info("Signature creation attempt",
		"doc_id", request.DocID,
		"user_email", func() string {
			if request.User != nil {
				return request.User.NormalizedEmail()
			}
			return ""
		}())

	if request.User == nil || !request.User.IsValid() {
		logger.Logger.Warn("Signature creation failed: invalid user",
			"doc_id", request.DocID,
			"user_nil", request.User == nil)
		return models.ErrInvalidUser
	}

	if request.DocID == "" {
		logger.Logger.Warn("Signature creation failed: invalid document",
			"user_email", request.User.NormalizedEmail())
		return models.ErrInvalidDocument
	}

	exists, err := s.repo.ExistsByDocAndUser(ctx, request.DocID, request.User.Sub)
	if err != nil {
		logger.Logger.Error("Signature creation failed: database check error",
			"doc_id", request.DocID,
			"user_email", request.User.NormalizedEmail(),
			"error", err.Error())
		return fmt.Errorf("failed to check existing signature: %w", err)
	}

	if exists {
		logger.Logger.Warn("Signature creation failed: already exists",
			"doc_id", request.DocID,
			"user_email", request.User.NormalizedEmail())
		return models.ErrSignatureAlreadyExists
	}

	nonce, err := crypto.GenerateNonce()
	if err != nil {
		logger.Logger.Error("Signature creation failed: nonce generation error",
			"doc_id", request.DocID,
			"user_email", request.User.NormalizedEmail(),
			"error", err.Error())
		return fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Fetch document metadata to get checksum (if available)
	var docChecksum string
	doc, err := s.docRepo.GetByDocID(ctx, request.DocID)
	if err != nil {
		logger.Logger.Debug("Document metadata not found, signing without checksum",
			"doc_id", request.DocID,
			"error", err.Error())
		// Continue without checksum - document metadata is optional
	} else if doc != nil && doc.Checksum != "" {
		// Verify document hasn't been modified before signing
		if err := s.verifyDocumentIntegrity(ctx, doc); err != nil {
			logger.Logger.Warn("Document integrity check failed",
				"doc_id", request.DocID,
				"error", err.Error())
			return err
		}

		docChecksum = doc.Checksum
		checksumPreview := docChecksum
		if len(docChecksum) > 16 {
			checksumPreview = docChecksum[:16] + "..."
		}
		logger.Logger.Debug("Including document checksum in signature",
			"doc_id", request.DocID,
			"checksum", checksumPreview)
	}

	timestamp := time.Now().UTC()
	payloadHash, signatureB64, err := s.signer.CreateSignature(ctx, request.DocID, request.User, timestamp, nonce, docChecksum)
	if err != nil {
		logger.Logger.Error("Signature creation failed: cryptographic signature error",
			"doc_id", request.DocID,
			"user_email", request.User.NormalizedEmail(),
			"error", err.Error())
		return fmt.Errorf("failed to create cryptographic signature: %w", err)
	}

	lastSignature, err := s.repo.GetLastSignature(ctx, request.DocID)
	if err != nil {
		logger.Logger.Error("Signature creation failed: chain lookup error",
			"doc_id", request.DocID,
			"user_email", request.User.NormalizedEmail(),
			"error", err.Error())
		return fmt.Errorf("failed to get last signature for chaining: %w", err)
	}

	var prevHashB64 *string
	if lastSignature != nil {
		hash := lastSignature.ComputeRecordHash()
		prevHashB64 = &hash
		logger.Logger.Debug("Chaining to previous signature",
			"doc_id", request.DocID,
			"prev_signature_id", lastSignature.ID,
			"prev_hash", hash[:16]+"...")
	} else {
		logger.Logger.Debug("Creating genesis signature (no previous signature)",
			"doc_id", request.DocID)
	}

	signature := &models.Signature{
		DocID:       request.DocID,
		UserSub:     request.User.Sub,
		UserEmail:   request.User.NormalizedEmail(),
		UserName:    request.User.Name,
		SignedAtUTC: timestamp,
		DocChecksum: docChecksum,
		PayloadHash: payloadHash,
		Signature:   signatureB64,
		Nonce:       nonce,
		Referer:     request.Referer,
		PrevHash:    prevHashB64,
	}

	if err := s.repo.Create(ctx, signature); err != nil {
		logger.Logger.Error("Signature creation failed: database save error",
			"doc_id", request.DocID,
			"user_email", request.User.NormalizedEmail(),
			"error", err.Error())
		return fmt.Errorf("failed to save signature: %w", err)
	}

	logger.Logger.Info("Signature created successfully",
		"signature_id", signature.ID,
		"doc_id", request.DocID,
		"user_email", request.User.NormalizedEmail(),
		"has_prev_hash", prevHashB64 != nil)

	return nil
}

// GetSignatureStatus checks if a user has already signed a document and returns signature timestamp if exists
func (s *SignatureService) GetSignatureStatus(ctx context.Context, docID string, user *models.User) (*models.SignatureStatus, error) {
	if user == nil || !user.IsValid() {
		return nil, models.ErrInvalidUser
	}

	signature, err := s.repo.GetByDocAndUser(ctx, docID, user.Sub)
	if err != nil {
		if errors.Is(err, models.ErrSignatureNotFound) {
			return &models.SignatureStatus{
				DocID:     docID,
				UserEmail: user.Email,
				IsSigned:  false,
				SignedAt:  nil,
			}, nil
		}
		return nil, fmt.Errorf("failed to get signature: %w", err)
	}

	return &models.SignatureStatus{
		DocID:     docID,
		UserEmail: user.Email,
		IsSigned:  true,
		SignedAt:  &signature.SignedAtUTC,
	}, nil
}

// GetDocumentSignatures retrieves all cryptographic signatures associated with a document for public verification
func (s *SignatureService) GetDocumentSignatures(ctx context.Context, docID string) ([]*models.Signature, error) {
	logger.Logger.Debug("Retrieving document signatures",
		"doc_id", docID)

	signatures, err := s.repo.GetByDoc(ctx, docID)
	if err != nil {
		logger.Logger.Error("Failed to retrieve document signatures",
			"doc_id", docID,
			"error", err.Error())
		return nil, fmt.Errorf("failed to get document signatures: %w", err)
	}

	logger.Logger.Debug("Document signatures retrieved",
		"doc_id", docID,
		"count", len(signatures))

	return signatures, nil
}

// GetUserSignatures retrieves all documents signed by a specific user for personal dashboard display
func (s *SignatureService) GetUserSignatures(ctx context.Context, user *models.User) ([]*models.Signature, error) {
	if user == nil || !user.IsValid() {
		return nil, models.ErrInvalidUser
	}

	signatures, err := s.repo.GetByUserEmail(ctx, user.Email)
	if err != nil {
		return nil, fmt.Errorf("failed to get user signatures: %w", err)
	}

	return signatures, nil
}

// GetSignatureByDocAndUser retrieves a specific signature record for verification or display purposes
func (s *SignatureService) GetSignatureByDocAndUser(ctx context.Context, docID string, user *models.User) (*models.Signature, error) {
	if user == nil || !user.IsValid() {
		return nil, models.ErrInvalidUser
	}

	signature, err := s.repo.GetByDocAndUser(ctx, docID, user.Sub)
	if err != nil {
		return nil, fmt.Errorf("failed to get signature: %w", err)
	}

	return signature, nil
}

// CheckUserSignature verifies signature existence using flexible identifier matching (email or OAuth subject)
func (s *SignatureService) CheckUserSignature(ctx context.Context, docID, userIdentifier string) (bool, error) {
	exists, err := s.repo.CheckUserSignatureStatus(ctx, docID, userIdentifier)
	if err != nil {
		return false, fmt.Errorf("failed to check user signature: %w", err)
	}

	return exists, nil
}

type ChainIntegrityResult struct {
	IsValid      bool
	TotalRecords int
	BreakAtID    *int64
	Details      string
}

// VerifyChainIntegrity validates the cryptographic hash chain across all signatures for tamper detection
func (s *SignatureService) VerifyChainIntegrity(ctx context.Context) (*ChainIntegrityResult, error) {
	signatures, err := s.repo.GetAllSignaturesOrdered(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get signatures for chain verification: %w", err)
	}

	result := &ChainIntegrityResult{
		IsValid:      true,
		TotalRecords: len(signatures),
	}

	if len(signatures) == 0 {
		result.Details = "No signatures found"
		return result, nil
	}

	if signatures[0].PrevHash != nil {
		result.IsValid = false
		result.BreakAtID = &signatures[0].ID
		result.Details = "Genesis signature has non-null previous hash"
		return result, nil
	}

	for i := 1; i < len(signatures); i++ {
		current := signatures[i]
		previous := signatures[i-1]

		expectedHash := previous.ComputeRecordHash()

		if current.PrevHash == nil {
			result.IsValid = false
			result.BreakAtID = &current.ID
			result.Details = fmt.Sprintf("Signature %d has null previous hash, expected: %s...", current.ID, expectedHash[:16])
			return result, nil
		}

		if *current.PrevHash != expectedHash {
			result.IsValid = false
			result.BreakAtID = &current.ID
			result.Details = fmt.Sprintf("Hash mismatch at signature %d: expected %s..., got %s...",
				current.ID, expectedHash[:16], (*current.PrevHash)[:16])
			return result, nil
		}
	}

	result.Details = "Chain integrity verified successfully"
	return result, nil
}

// RebuildChain recalculates and updates prev_hash pointers for existing signatures during migration
func (s *SignatureService) RebuildChain(ctx context.Context) error {
	signatures, err := s.repo.GetAllSignaturesOrdered(ctx)
	if err != nil {
		return fmt.Errorf("failed to get signatures for chain rebuild: %w", err)
	}

	if len(signatures) == 0 {
		logger.Logger.Info("No signatures found, nothing to rebuild")
		return nil
	}

	logger.Logger.Info("Starting chain rebuild", "totalSignatures", len(signatures))

	if signatures[0].PrevHash != nil {
		if err := s.repo.UpdatePrevHash(ctx, signatures[0].ID, nil); err != nil {
			logger.Logger.Warn("Failed to nullify genesis prev_hash", "id", signatures[0].ID, "error", err)
		}
	}

	for i := 1; i < len(signatures); i++ {
		current := signatures[i]
		previous := signatures[i-1]

		expectedHash := previous.ComputeRecordHash()

		if current.PrevHash == nil || *current.PrevHash != expectedHash {
			logger.Logger.Info("Chain rebuild: updating prev_hash",
				"id", current.ID,
				"expectedHash", expectedHash[:16]+"...",
				"hadPrevHash", current.PrevHash != nil)
			if err := s.repo.UpdatePrevHash(ctx, current.ID, &expectedHash); err != nil {
				logger.Logger.Warn("Failed to update prev_hash", "id", current.ID, "error", err)
			}
		}
	}

	logger.Logger.Info("Chain rebuild completed", "processedSignatures", len(signatures))
	return nil
}

// verifyDocumentIntegrity checks if the document at the URL hasn't been modified since the checksum was stored
func (s *SignatureService) verifyDocumentIntegrity(ctx context.Context, doc *models.Document) error {
	// Only verify if document has URL and checksum, and checksum config is available
	if doc.URL == "" || doc.Checksum == "" || s.checksumConfig == nil {
		logger.Logger.Debug("Skipping document integrity check",
			"doc_id", doc.DocID,
			"has_url", doc.URL != "",
			"has_checksum", doc.Checksum != "",
			"has_config", s.checksumConfig != nil)
		return nil
	}

	storedChecksumPreview := doc.Checksum
	if len(doc.Checksum) > 16 {
		storedChecksumPreview = doc.Checksum[:16] + "..."
	}
	logger.Logger.Info("Verifying document integrity before signature",
		"doc_id", doc.DocID,
		"url", doc.URL,
		"stored_checksum", storedChecksumPreview)

	// Configure checksum computation options
	opts := checksum.ComputeOptions{
		MaxBytes:           s.checksumConfig.MaxBytes,
		TimeoutMs:          s.checksumConfig.TimeoutMs,
		MaxRedirects:       s.checksumConfig.MaxRedirects,
		AllowedContentType: s.checksumConfig.AllowedContentType,
		SkipSSRFCheck:      s.checksumConfig.SkipSSRFCheck,
		InsecureSkipVerify: s.checksumConfig.InsecureSkipVerify,
	}

	// Compute current checksum
	result, err := checksum.ComputeRemoteChecksum(ctx, doc.URL, opts)
	if err != nil {
		logger.Logger.Error("Failed to compute checksum for integrity check",
			"doc_id", doc.DocID,
			"url", doc.URL,
			"error", err.Error())
		// If we can't verify, we can't be sure it's modified, so we continue
		// but log the issue
		return nil
	}

	// If checksum computation returned nil (too large, wrong type, network error, etc.)
	// we can't verify integrity, so we continue but log a warning
	if result == nil {
		logger.Logger.Warn("Could not verify document integrity - unable to compute checksum",
			"doc_id", doc.DocID,
			"url", doc.URL)
		return nil
	}

	// Compare checksums
	if result.ChecksumHex != doc.Checksum {
		logger.Logger.Error("Document integrity check FAILED - checksums do not match",
			"doc_id", doc.DocID,
			"url", doc.URL,
			"stored_checksum", doc.Checksum,
			"current_checksum", result.ChecksumHex)
		return models.ErrDocumentModified
	}

	logger.Logger.Info("Document integrity verified successfully",
		"doc_id", doc.DocID,
		"checksum", result.ChecksumHex[:16]+"...")

	return nil
}

// CountSigns returns the current count of signatures in the database
func (s *SignatureService) CountSigns(ctx context.Context) int {
	c, err := s.repo.Count(ctx)
	if err != nil {
		c = 0
	}
	return c
}
