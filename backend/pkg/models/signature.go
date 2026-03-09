// SPDX-License-Identifier: AGPL-3.0-or-later
package models

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/kolapsis/ackify/backend/pkg/services"
)

type Signature struct {
	ID           int64      `json:"id" db:"id"`
	TenantID     uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	DocID        string     `json:"doc_id" db:"doc_id"`
	UserSub      string     `json:"user_sub" db:"user_sub"`
	UserEmail    string     `json:"user_email" db:"user_email"`
	UserName     string     `json:"user_name,omitempty" db:"user_name"`
	SignedAtUTC  time.Time  `json:"signed_at" db:"signed_at"`
	DocChecksum  string     `json:"doc_checksum,omitempty" db:"doc_checksum"`
	PayloadHash  string     `json:"payload_hash" db:"payload_hash"`
	Signature    string     `json:"signature" db:"signature"`
	Nonce        string     `json:"nonce" db:"nonce"`
	Referer      *string    `json:"referer,omitempty" db:"referer"`
	PrevHash     *string    `json:"prev_hash,omitempty" db:"prev_hash"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
	HashVersion  int        `json:"hash_version" db:"hash_version"`
	DocDeletedAt *time.Time `json:"doc_deleted_at,omitempty" db:"doc_deleted_at"`
	// Document metadata enriched from LEFT JOIN (not stored in signatures table)
	DocTitle string `json:"doc_title,omitempty"`
	DocURL   string `json:"doc_url,omitempty"`
}

func (s *Signature) GetServiceInfo() *services.ServiceInfo {
	if s.Referer == nil {
		return nil
	}
	return services.DetectServiceFromReferrer(*s.Referer)
}

type SignatureRequest struct {
	DocID   string
	User    *User
	Referer *string
}

type SignatureStatus struct {
	DocID     string
	UserEmail string
	IsSigned  bool
	SignedAt  *time.Time
}

// ComputeRecordHash computes the hash of the signature record for blockchain integrity
// Uses versioned hash algorithms for backward compatibility
func (s *Signature) ComputeRecordHash() string {
	switch s.HashVersion {
	case 2:
		return s.computeHashV2()
	default:
		// Version 1 or unset (backward compatibility)
		return s.computeHashV1()
	}
}

// computeHashV1 computes hash using legacy pipe-separated format
// Used for existing signatures to maintain backward compatibility
func (s *Signature) computeHashV1() string {
	data := fmt.Sprintf("%d|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s|%s",
		s.ID,
		s.DocID,
		s.UserSub,
		s.UserEmail,
		s.UserName,
		s.SignedAtUTC.Format(time.RFC3339Nano),
		s.DocChecksum,
		s.PayloadHash,
		s.Signature,
		s.Nonce,
		s.CreatedAt.Format(time.RFC3339Nano),
		func() string {
			if s.Referer != nil {
				return *s.Referer
			}
			return ""
		}(),
	)

	hash := sha256.Sum256([]byte(data))
	return base64.StdEncoding.EncodeToString(hash[:])
}

// computeHashV2 computes hash using JSON canonical format
// Recommended for new signatures - eliminates ambiguity and is more extensible
func (s *Signature) computeHashV2() string {
	// Create canonical representation with keys sorted alphabetically
	canonical := map[string]interface{}{
		"created_at":   s.CreatedAt.Unix(),
		"doc_checksum": s.DocChecksum,
		"doc_id":       s.DocID,
		"id":           s.ID,
		"nonce":        s.Nonce,
		"payload_hash": s.PayloadHash,
		"referer": func() string {
			if s.Referer != nil {
				return *s.Referer
			}
			return ""
		}(),
		"signature":  s.Signature,
		"signed_at":  s.SignedAtUTC.Unix(),
		"user_email": s.UserEmail,
		"user_name":  s.UserName,
		"user_sub":   s.UserSub,
	}

	// Marshal to JSON with sorted keys (Go's json.Marshal sorts keys automatically)
	data, err := json.Marshal(canonical)
	if err != nil {
		// Fallback to V1 if JSON marshaling fails (should never happen)
		return s.computeHashV1()
	}

	hash := sha256.Sum256(data)
	return base64.StdEncoding.EncodeToString(hash[:])
}
