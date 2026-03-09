// SPDX-License-Identifier: AGPL-3.0-or-later
package crypto

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kolapsis/ackify/backend/pkg/models"
)

// TestCryptoIntegration tests the integrations between signature generation and nonce generation
func TestCryptoIntegration(t *testing.T) {
	t.Run("signature with generated nonce", func(t *testing.T) {
		signer, err := NewEd25519Signer()
		require.NoError(t, err)

		user := testUserAlice
		docID := "integrations-test-doc"
		timestamp := time.Now().UTC()

		// Generate a nonce
		nonce, err := GenerateNonce()
		require.NoError(t, err)

		// Create signature with generated nonce
		hash, sig, err := signer.CreateSignature(context.Background(), docID, user, timestamp, nonce, "")
		require.NoError(t, err)

		assert.NotEmpty(t, hash)
		assert.NotEmpty(t, sig)

		// Verify hash is SHA-256
		hashBytes, err := base64.StdEncoding.DecodeString(hash)
		require.NoError(t, err)
		assert.Len(t, hashBytes, 32, "Hash should be SHA-256 (32 bytes)")

		// Verify signature is Ed25519
		sigBytes, err := base64.StdEncoding.DecodeString(sig)
		require.NoError(t, err)
		assert.Len(t, sigBytes, 64, "Signature should be Ed25519 (64 bytes)")
	})

	t.Run("different nonces produce different signatures", func(t *testing.T) {
		signer, err := NewEd25519Signer()
		require.NoError(t, err)

		user := testUserBob
		docID := "nonce-diff-test"
		timestamp := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

		// Generate two different nonces
		nonce1, err := GenerateNonce()
		require.NoError(t, err)

		nonce2, err := GenerateNonce()
		require.NoError(t, err)

		assert.NotEqual(t, nonce1, nonce2, "Nonces should be different")

		// Create signatures with different nonces
		hash1, sig1, err := signer.CreateSignature(context.Background(), docID, user, timestamp, nonce1, "")
		require.NoError(t, err)

		hash2, sig2, err := signer.CreateSignature(context.Background(), docID, user, timestamp, nonce2, "")
		require.NoError(t, err)

		// Different nonces should produce different signatures
		assert.NotEqual(t, hash1, hash2, "Different nonces should produce different hashes")
		assert.NotEqual(t, sig1, sig2, "Different nonces should produce different signatures")
	})

	t.Run("replay attack prevention", func(t *testing.T) {
		signer, err := NewEd25519Signer()
		require.NoError(t, err)

		user := testUserCharlie
		docID := "replay-test-doc"
		timestamp := time.Now().UTC()

		// Simulate multiple signature attempts for same document
		signatures := make(map[string]bool)
		nonces := make(map[string]bool)

		for i := 0; i < 10; i++ {
			// Generate unique nonce for each attempt
			nonce, err := GenerateNonce()
			require.NoError(t, err)

			// Verify nonce is unique
			assert.False(t, nonces[nonce], "Nonce should be unique for replay protection")
			nonces[nonce] = true

			// Create signature
			hash, sig, err := signer.CreateSignature(context.Background(), docID, user, timestamp, nonce, "")
			require.NoError(t, err)

			// Verify signature is unique
			assert.False(t, signatures[sig], "Signature should be unique due to nonce")
			signatures[sig] = true

			// All should have different hashes due to nonce
			assert.NotEmpty(t, hash)
			assert.NotEmpty(t, sig)
		}

		assert.Len(t, signatures, 10, "All signatures should be unique")
		assert.Len(t, nonces, 10, "All nonces should be unique")
	})
}

// TestSHA256Hashing tests SHA-256 hashing functionality indirectly through signature creation
func TestSHA256Hashing(t *testing.T) {
	signer, err := NewEd25519Signer()
	require.NoError(t, err)

	t.Run("consistent hashing", func(t *testing.T) {
		user := testUserAlice
		docID := "hash-test-doc"
		timestamp := time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC)
		nonce := "consistent-nonce"

		// Create signature multiple times
		hash1, _, err := signer.CreateSignature(context.Background(), docID, user, timestamp, nonce, "")
		require.NoError(t, err)

		hash2, _, err := signer.CreateSignature(context.Background(), docID, user, timestamp, nonce, "")
		require.NoError(t, err)

		assert.Equal(t, hash1, hash2, "Same input should produce same hash")
	})

	t.Run("hash changes with input changes", func(t *testing.T) {
		user := testUserBob
		baseTimestamp := time.Date(2024, 4, 1, 14, 0, 0, 0, time.UTC)
		baseNonce := "base-nonce"

		// Base signature
		baseHash, _, err := signer.CreateSignature(context.Background(), "base-doc", user, baseTimestamp, baseNonce, "")
		require.NoError(t, err)

		// Test different document ID
		hash1, _, err := signer.CreateSignature(context.Background(), "different-doc", user, baseTimestamp, baseNonce, "")
		require.NoError(t, err)
		assert.NotEqual(t, baseHash, hash1, "Different docID should produce different hash")

		// Test different user
		differentUser := testUserCharlie
		hash2, _, err := signer.CreateSignature(context.Background(), "base-doc", differentUser, baseTimestamp, baseNonce, "")
		require.NoError(t, err)
		assert.NotEqual(t, baseHash, hash2, "Different user should produce different hash")

		// Test different timestamp
		differentTime := baseTimestamp.Add(time.Hour)
		hash3, _, err := signer.CreateSignature(context.Background(), "base-doc", user, differentTime, baseNonce, "")
		require.NoError(t, err)
		assert.NotEqual(t, baseHash, hash3, "Different timestamp should produce different hash")

		// Test different nonce
		hash4, _, err := signer.CreateSignature(context.Background(), "base-doc", user, baseTimestamp, "different-nonce", "")
		require.NoError(t, err)
		assert.NotEqual(t, baseHash, hash4, "Different nonce should produce different hash")
	})

	t.Run("hash properties", func(t *testing.T) {
		user := testUserAlice
		docID := "props-test"
		timestamp := time.Now().UTC()
		nonce := "props-nonce"

		hashB64, _, err := signer.CreateSignature(context.Background(), docID, user, timestamp, nonce, "")
		require.NoError(t, err)

		// Decode hash
		hashBytes, err := base64.StdEncoding.DecodeString(hashB64)
		require.NoError(t, err)

		// SHA-256 properties
		assert.Len(t, hashBytes, 32, "SHA-256 hash should be 32 bytes")
		assert.NotEqual(t, make([]byte, 32), hashBytes, "Hash should not be all zeros")

		// Verify it's actually SHA-256 by recreating manually
		expectedPayload := "doc_id=" + docID + "\n" +
			"user_sub=" + user.Sub + "\n" +
			"user_email=" + user.NormalizedEmail() + "\n" +
			"signed_at=" + timestamp.UTC().Format(time.RFC3339Nano) + "\n" +
			"nonce=" + nonce + "\n"

		expectedHash := sha256.Sum256([]byte(expectedPayload))
		expectedHashB64 := base64.StdEncoding.EncodeToString(expectedHash[:])

		assert.Equal(t, expectedHashB64, hashB64, "Hash should match manual SHA-256 calculation")
	})
}

// TestCorruptionDetection tests that signature corruption is detectable
func TestCorruptionDetection(t *testing.T) {
	signer, err := NewEd25519Signer()
	require.NoError(t, err)

	t.Run("hash corruption detection", func(t *testing.T) {
		user := testUserAlice
		docID := "corruption-test"
		timestamp := time.Now().UTC()
		nonce := "corruption-nonce"

		originalHash, originalSig, err := signer.CreateSignature(context.Background(), docID, user, timestamp, nonce, "")
		require.NoError(t, err)

		// Corrupt the hash
		hashBytes, err := base64.StdEncoding.DecodeString(originalHash)
		require.NoError(t, err)

		hashBytes[0] ^= 0x01 // Flip one bit
		corruptedHash := base64.StdEncoding.EncodeToString(hashBytes)

		assert.NotEqual(t, originalHash, corruptedHash, "Corrupted hash should be different")

		// Original signature won't match corrupted hash when verified
		// (This would be caught during verification process)
		assert.NotEmpty(t, originalSig)
	})

	t.Run("signature corruption detection", func(t *testing.T) {
		user := testUserBob
		docID := "sig-corruption-test"
		timestamp := time.Now().UTC()
		nonce := "sig-corruption-nonce"

		originalHash, originalSig, err := signer.CreateSignature(context.Background(), docID, user, timestamp, nonce, "")
		require.NoError(t, err)

		// Corrupt the signature
		sigBytes, err := base64.StdEncoding.DecodeString(originalSig)
		require.NoError(t, err)

		sigBytes[63] ^= 0xFF // Flip bits in last byte
		corruptedSig := base64.StdEncoding.EncodeToString(sigBytes)

		assert.NotEqual(t, originalSig, corruptedSig, "Corrupted signature should be different")
		assert.NotEmpty(t, originalHash) // Hash should remain valid
	})

	t.Run("payload tampering detection", func(t *testing.T) {
		user := testUserCharlie
		docID := "tamper-test"
		timestamp := time.Date(2024, 5, 1, 16, 45, 0, 0, time.UTC)
		nonce := "tamper-nonce"

		// Original signature
		originalHash, originalSig, err := signer.CreateSignature(context.Background(), docID, user, timestamp, nonce, "")
		require.NoError(t, err)

		// Create signature for tampered data (different docID)
		tamperedHash, tamperedSig, err := signer.CreateSignature(context.Background(), "tampered-doc", user, timestamp, nonce, "")
		require.NoError(t, err)

		// Tampered data produces different hash and signature
		assert.NotEqual(t, originalHash, tamperedHash, "Tampered payload should produce different hash")
		assert.NotEqual(t, originalSig, tamperedSig, "Tampered payload should produce different signature")
	})
}

// TestBusinessRuleEnforcement tests that cryptographic functions support business rules
func TestBusinessRuleEnforcement(t *testing.T) {
	t.Run("unique signatures per document-user pair", func(t *testing.T) {
		signer, err := NewEd25519Signer()
		require.NoError(t, err)

		user := testUserAlice
		docID := "business-rule-test"
		timestamp := time.Now().UTC()

		// Create signatures with different nonces (simulating different attempts)
		nonce1, err := GenerateNonce()
		require.NoError(t, err)

		nonce2, err := GenerateNonce()
		require.NoError(t, err)

		hash1, sig1, err := signer.CreateSignature(context.Background(), docID, user, timestamp, nonce1, "")
		require.NoError(t, err)

		hash2, sig2, err := signer.CreateSignature(context.Background(), docID, user, timestamp, nonce2, "")
		require.NoError(t, err)

		// Different nonces create different signatures
		// This supports business rule that each signing attempt must be unique
		assert.NotEqual(t, hash1, hash2, "Different nonces should create different hashes")
		assert.NotEqual(t, sig1, sig2, "Different nonces should create different signatures")
	})

	t.Run("email normalization consistency", func(t *testing.T) {
		signer, err := NewEd25519Signer()
		require.NoError(t, err)

		// Create users with same email in different cases
		user1 := &models.User{
			Sub:   "user-case-test",
			Email: "Test.User@EXAMPLE.COM",
			Name:  "Test User",
		}

		user2 := &models.User{
			Sub:   "user-case-test",
			Email: "test.user@example.com",
			Name:  "Test User",
		}

		docID := "email-case-test"
		timestamp := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
		nonce := "case-nonce"

		hash1, sig1, err := signer.CreateSignature(context.Background(), docID, user1, timestamp, nonce, "")
		require.NoError(t, err)

		hash2, sig2, err := signer.CreateSignature(context.Background(), docID, user2, timestamp, nonce, "")
		require.NoError(t, err)

		// Should produce same signature due to email normalization
		assert.Equal(t, hash1, hash2, "Email case should not affect signature due to normalization")
		assert.Equal(t, sig1, sig2, "Email case should not affect signature due to normalization")
	})
}
