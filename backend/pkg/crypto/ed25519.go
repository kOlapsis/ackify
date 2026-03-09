// SPDX-License-Identifier: AGPL-3.0-or-later
package crypto

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/models"
)

// Ed25519Signer provides cryptographic signature operations using Ed25519 elliptic curve algorithm
type Ed25519Signer struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
}

// NewEd25519Signer initializes signer with persistent or ephemeral keypair from environment
func NewEd25519Signer() (*Ed25519Signer, error) {
	privKey, pubKey, err := loadOrGenerateKeys()
	if err != nil {
		return nil, fmt.Errorf("failed to load or generate keys: %w", err)
	}

	return &Ed25519Signer{
		privateKey: privKey,
		publicKey:  pubKey,
	}, nil
}

// CreateSignature generates SHA-256 payload hash and Ed25519 signature for non-repudiation proof
// The context is used for tracing and cancellation propagation.
func (s *Ed25519Signer) CreateSignature(ctx context.Context, docID string, user *models.User, timestamp time.Time, nonce string, docChecksum string) (string, string, error) {
	// Check if context has been cancelled before performing cryptographic operations
	if err := ctx.Err(); err != nil {
		return "", "", fmt.Errorf("context cancelled before signature creation: %w", err)
	}

	payload := canonicalPayload(docID, user, timestamp, nonce, docChecksum)
	hash := sha256.Sum256(payload)
	signature := ed25519.Sign(s.privateKey, hash[:])

	return base64.StdEncoding.EncodeToString(hash[:]), base64.StdEncoding.EncodeToString(signature), nil
}

// GetPublicKey exports the base64-encoded public key for signature verification by external parties
func (s *Ed25519Signer) GetPublicKey() string {
	return base64.StdEncoding.EncodeToString(s.publicKey)
}

func canonicalPayload(docID string, user *models.User, timestamp time.Time, nonce string, docChecksum string) []byte {
	payload := fmt.Sprintf(
		"doc_id=%s\nuser_sub=%s\nuser_email=%s\nsigned_at=%s\nnonce=%s\n",
		docID,
		user.Sub,
		user.NormalizedEmail(),
		timestamp.UTC().Format(time.RFC3339Nano),
		nonce,
	)

	// Include document checksum if provided (ensures signature ties to specific document version)
	if docChecksum != "" {
		payload += fmt.Sprintf("doc_checksum=%s\n", docChecksum)
	}

	return []byte(payload)
}

func loadOrGenerateKeys() (ed25519.PrivateKey, ed25519.PublicKey, error) {
	b64Key := strings.TrimSpace(os.Getenv("ACKIFY_ED25519_PRIVATE_KEY"))

	if b64Key != "" {
		keyBytes, err := base64.StdEncoding.DecodeString(b64Key)
		if err != nil || len(keyBytes) != ed25519.PrivateKeySize {
			return nil, nil, fmt.Errorf("invalid ACKIFY_ED25519_PRIVATE_KEY: %v", err)
		}

		privateKey := ed25519.PrivateKey(keyBytes)
		publicKey := privateKey.Public().(ed25519.PublicKey)

		return privateKey, publicKey, nil
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate keys: %w", err)
	}

	logger.Logger.Warn("Ed25519 private key not set, signatures will be different on restart")

	return privateKey, publicKey, nil
}
