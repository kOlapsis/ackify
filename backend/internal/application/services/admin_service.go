// SPDX-License-Identifier: AGPL-3.0-or-later
package services

import (
	"context"

	"github.com/kolapsis/ackify/backend/pkg/models"
)

// adminDocumentRepository defines admin-specific document operations
type adminDocumentRepository interface {
	GetByDocID(ctx context.Context, docID string) (*models.Document, error)
	List(ctx context.Context, limit, offset int) ([]*models.Document, error)
	Search(ctx context.Context, query string, limit, offset int) ([]*models.Document, error)
	Count(ctx context.Context, searchQuery string) (int, error)
	CreateOrUpdate(ctx context.Context, docID string, input models.DocumentInput, createdBy string) (*models.Document, error)
	Delete(ctx context.Context, docID string) error
}

// adminSignerRepository defines admin-specific expected signer operations
type adminSignerRepository interface {
	ListByDocID(ctx context.Context, docID string) ([]*models.ExpectedSigner, error)
	ListWithStatusByDocID(ctx context.Context, docID string) ([]*models.ExpectedSignerWithStatus, error)
	AddExpected(ctx context.Context, docID string, contacts []models.ContactInfo, addedBy string) error
	Remove(ctx context.Context, docID, email string) error
	GetStats(ctx context.Context, docID string) (*models.DocCompletionStats, error)
	GetAggregateDocumentStats(ctx context.Context, createdBy string) (pending, completed int, err error)
}

// AdminService handles all admin-specific operations on documents and signers
type AdminService struct {
	docRepo    adminDocumentRepository
	signerRepo adminSignerRepository
}

// NewAdminService creates a new admin service
func NewAdminService(docRepo adminDocumentRepository, signerRepo adminSignerRepository) *AdminService {
	return &AdminService{
		docRepo:    docRepo,
		signerRepo: signerRepo,
	}
}

// Document operations
func (s *AdminService) GetDocument(ctx context.Context, docID string) (*models.Document, error) {
	return s.docRepo.GetByDocID(ctx, docID)
}

func (s *AdminService) ListDocuments(ctx context.Context, limit, offset int) ([]*models.Document, error) {
	return s.docRepo.List(ctx, limit, offset)
}

func (s *AdminService) SearchDocuments(ctx context.Context, query string, limit, offset int) ([]*models.Document, error) {
	return s.docRepo.Search(ctx, query, limit, offset)
}

func (s *AdminService) CountDocuments(ctx context.Context, searchQuery string) (int, error) {
	return s.docRepo.Count(ctx, searchQuery)
}

func (s *AdminService) UpdateDocumentMetadata(ctx context.Context, docID string, input models.DocumentInput, updatedBy string) (*models.Document, error) {
	return s.docRepo.CreateOrUpdate(ctx, docID, input, updatedBy)
}

func (s *AdminService) DeleteDocument(ctx context.Context, docID string) error {
	return s.docRepo.Delete(ctx, docID)
}

// Expected signer operations
func (s *AdminService) ListExpectedSigners(ctx context.Context, docID string) ([]*models.ExpectedSigner, error) {
	return s.signerRepo.ListByDocID(ctx, docID)
}

func (s *AdminService) ListExpectedSignersWithStatus(ctx context.Context, docID string) ([]*models.ExpectedSignerWithStatus, error) {
	return s.signerRepo.ListWithStatusByDocID(ctx, docID)
}

func (s *AdminService) AddExpectedSigners(ctx context.Context, docID string, contacts []models.ContactInfo, addedBy string) error {
	return s.signerRepo.AddExpected(ctx, docID, contacts, addedBy)
}

func (s *AdminService) RemoveExpectedSigner(ctx context.Context, docID, email string) error {
	return s.signerRepo.Remove(ctx, docID, email)
}

func (s *AdminService) GetSignerStats(ctx context.Context, docID string) (*models.DocCompletionStats, error) {
	return s.signerRepo.GetStats(ctx, docID)
}

func (s *AdminService) GetAggregateDocumentStats(ctx context.Context) (pending, completed int, err error) {
	return s.signerRepo.GetAggregateDocumentStats(ctx, "")
}
