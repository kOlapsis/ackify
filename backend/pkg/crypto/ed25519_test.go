// SPDX-License-Identifier: AGPL-3.0-or-later
package crypto

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kolapsis/ackify/backend/pkg/models"
)

func TestEd25519Signer_NewEd25519Signer(t *testing.T) {
	t.Run("creates new signer successfully", func(t *testing.T) {

		originalKey := os.Getenv("ACKIFY_ED25519_PRIVATE_KEY")
		_ = os.Unsetenv("ACKIFY_ED25519_PRIVATE_KEY")
		defer func() {
			if originalKey != "" {
				_ = os.Setenv("ACKIFY_ED25519_PRIVATE_KEY", originalKey)
			}
		}()

		signer, err := NewEd25519Signer()
		require.NoError(t, err)
		require.NotNil(t, signer)

		pubKey := signer.GetPublicKey()
		assert.NotEmpty(t, pubKey)

		_, err = base64.StdEncoding.DecodeString(pubKey)
		assert.NoError(t, err)
	})

	t.Run("loads signer from environment variable", func(t *testing.T) {

		publicKey, privateKey, err := ed25519.GenerateKey(nil)
		require.NoError(t, err)

		b64Key := base64.StdEncoding.EncodeToString(privateKey)
		_ = os.Setenv("ACKIFY_ED25519_PRIVATE_KEY", b64Key)
		defer func() {
			_ = os.Unsetenv("ACKIFY_ED25519_PRIVATE_KEY")
		}()

		signer, err := NewEd25519Signer()
		require.NoError(t, err)
		require.NotNil(t, signer)

		expectedPubKey := base64.StdEncoding.EncodeToString(publicKey)
		actualPubKey := signer.GetPublicKey()
		assert.Equal(t, expectedPubKey, actualPubKey)
	})

	t.Run("fails with invalid environment variable", func(t *testing.T) {
		testCases := []struct {
			name  string
			value string
		}{
			{"invalid base64", "invalid!@#$"},
			{"wrong length", base64.StdEncoding.EncodeToString([]byte("short"))},
			{"empty string", ""},
			{"whitespace only", "   "},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				_ = os.Setenv("ACKIFY_ED25519_PRIVATE_KEY", tc.value)
				defer func() {
					_ = os.Unsetenv("ACKIFY_ED25519_PRIVATE_KEY")
				}()

				if tc.value == "" || tc.value == "   " {

					signer, err := NewEd25519Signer()
					require.NoError(t, err)
					assert.NotNil(t, signer)
				} else {

					signer, err := NewEd25519Signer()
					assert.Error(t, err)
					assert.Nil(t, signer)
					assert.Contains(t, err.Error(), "invalid ACKIFY_ED25519_PRIVATE_KEY")
				}
			})
		}
	})
}

func TestEd25519Signer_CreateSignature(t *testing.T) {

	signer, err := NewEd25519Signer()
	require.NoError(t, err)

	t.Run("creates valid signature", func(t *testing.T) {
		user := testUserAlice
		docID := "test-document"
		timestamp := time.Date(2024, 1, 15, 12, 30, 0, 0, time.UTC)
		nonce := "test-nonce-123"

		hashB64, sigB64, err := signer.CreateSignature(context.Background(), docID, user, timestamp, nonce, "")

		require.NoError(t, err)
		assert.NotEmpty(t, hashB64)
		assert.NotEmpty(t, sigB64)

		hashBytes, err := base64.StdEncoding.DecodeString(hashB64)
		require.NoError(t, err)
		assert.Len(t, hashBytes, 32) // SHA-256 hash length

		sigBytes, err := base64.StdEncoding.DecodeString(sigB64)
		require.NoError(t, err)
		assert.Len(t, sigBytes, ed25519.SignatureSize) // Ed25519 signature length
	})

	t.Run("creates consistent signatures", func(t *testing.T) {
		user := testUserBob
		docID := "consistent-doc"
		timestamp := time.Date(2024, 2, 1, 10, 0, 0, 0, time.UTC)
		nonce := "consistent-nonce"

		// Create signature twice with same parameters
		hash1, sig1, err1 := signer.CreateSignature(context.Background(), docID, user, timestamp, nonce, "")
		require.NoError(t, err1)

		hash2, sig2, err2 := signer.CreateSignature(context.Background(), docID, user, timestamp, nonce, "")
		require.NoError(t, err2)

		// Should produce identical results
		assert.Equal(t, hash1, hash2)
		assert.Equal(t, sig1, sig2)
	})

	t.Run("creates different signatures for different inputs", func(t *testing.T) {
		user := testUserCharlie
		timestamp := time.Now().UTC()
		nonce := "test-nonce"

		// Same user, different documents
		hash1, sig1, err := signer.CreateSignature(context.Background(), "doc1", user, timestamp, nonce, "")
		require.NoError(t, err)

		hash2, sig2, err := signer.CreateSignature(context.Background(), "doc2", user, timestamp, nonce, "")
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
		assert.NotEqual(t, sig1, sig2)

		// Same document, different users
		hash3, sig3, err := signer.CreateSignature(context.Background(), "doc1", testUserAlice, timestamp, nonce, "")
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash3)
		assert.NotEqual(t, sig1, sig3)

		// Same everything, different nonces
		hash4, sig4, err := signer.CreateSignature(context.Background(), "doc1", user, timestamp, "different-nonce", "")
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash4)
		assert.NotEqual(t, sig1, sig4)
	})

	t.Run("handles different timestamp formats", func(t *testing.T) {
		user := testUserAlice
		docID := "timestamp-test"
		nonce := "timestamp-nonce"

		testCases := []struct {
			name      string
			timestamp time.Time
		}{
			{"UTC time", time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC)},
			{"Local time", time.Date(2024, 6, 15, 14, 30, 0, 0, time.FixedZone("EST", -5*3600))},
			{"Nanoseconds", time.Date(2024, 6, 15, 14, 30, 0, 123456789, time.UTC)},
			{"Zero time", time.Time{}},
			{"Unix epoch", time.Unix(0, 0)},
		}

		signatures := make(map[string]string)

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				hash, sig, err := signer.CreateSignature(context.Background(), docID, user, tc.timestamp, nonce, "")
				require.NoError(t, err)

				// Each timestamp should produce unique signature
				assert.NotContains(t, signatures, sig, "Signature should be unique for different timestamps")
				signatures[sig] = tc.name

				assert.NotEmpty(t, hash)
				assert.NotEmpty(t, sig)
			})
		}
	})

	t.Run("handles edge case inputs", func(t *testing.T) {
		timestamp := time.Now().UTC()

		testCases := []struct {
			name  string
			docID string
			user  *models.User
			nonce string
		}{
			{"empty docID", "", testUserAlice, "nonce"},
			{"empty nonce", "doc", testUserAlice, ""},
			{"special chars in docID", "doc/with:special#chars", testUserAlice, "nonce"},
			{"unicode in docID", "文档-测试", testUserAlice, "nonce"},
			{"long docID", string(make([]byte, 1000)), testUserAlice, "nonce"},
			{"long nonce", string(make([]byte, 1000)), testUserAlice, "nonce"},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				// Fill long strings with valid data
				if tc.docID == string(make([]byte, 1000)) {
					tc.docID = "long-doc-" + string(make([]rune, 990))
					for i := range tc.docID[9:] {
						tc.docID = tc.docID[:9+i] + "a" + tc.docID[9+i+1:]
					}
				}
				if tc.nonce == string(make([]byte, 1000)) {
					tc.nonce = "long-nonce-" + string(make([]rune, 985))
					for i := range tc.nonce[11:] {
						tc.nonce = tc.nonce[:11+i] + "b" + tc.nonce[11+i+1:]
					}
				}

				hash, sig, err := signer.CreateSignature(context.Background(), tc.docID, testUserAlice, timestamp, tc.nonce, "")

				// Should not fail on edge case inputs
				require.NoError(t, err)
				assert.NotEmpty(t, hash)
				assert.NotEmpty(t, sig)
			})
		}
	})
}

func TestEd25519Signer_SignatureVerification(t *testing.T) {
	signer, err := NewEd25519Signer()
	require.NoError(t, err)

	t.Run("signature can be verified", func(t *testing.T) {
		user := testUserAlice
		docID := "verify-test"
		timestamp := time.Date(2024, 3, 1, 9, 15, 30, 0, time.UTC)
		nonce := "verify-nonce"

		hashB64, sigB64, err := signer.CreateSignature(context.Background(), docID, user, timestamp, nonce, "")
		require.NoError(t, err)

		// Decode signature and hash
		sigBytes, err := base64.StdEncoding.DecodeString(sigB64)
		require.NoError(t, err)

		hashBytes, err := base64.StdEncoding.DecodeString(hashB64)
		require.NoError(t, err)

		// Get public key
		pubKeyB64 := signer.GetPublicKey()
		pubKeyBytes, err := base64.StdEncoding.DecodeString(pubKeyB64)
		require.NoError(t, err)

		pubKey := ed25519.PublicKey(pubKeyBytes)

		// Verify signature against hash
		isValid := ed25519.Verify(pubKey, hashBytes, sigBytes)
		assert.True(t, isValid, "Generated signature should be valid")
	})

	t.Run("corrupted signature fails verification", func(t *testing.T) {
		user := testUserBob
		docID := "corrupt-test"
		timestamp := time.Now().UTC()
		nonce := "corrupt-nonce"

		hashB64, sigB64, err := signer.CreateSignature(context.Background(), docID, user, timestamp, nonce, "")
		require.NoError(t, err)

		// Corrupt the signature
		sigBytes, err := base64.StdEncoding.DecodeString(sigB64)
		require.NoError(t, err)

		sigBytes[0] ^= 0xFF // Flip bits in first byte
		corruptedSig := base64.StdEncoding.EncodeToString(sigBytes)

		// Try to verify corrupted signature
		hashBytes, err := base64.StdEncoding.DecodeString(hashB64)
		require.NoError(t, err)

		pubKeyB64 := signer.GetPublicKey()
		pubKeyBytes, err := base64.StdEncoding.DecodeString(pubKeyB64)
		require.NoError(t, err)

		pubKey := ed25519.PublicKey(pubKeyBytes)
		corruptedSigBytes, err := base64.StdEncoding.DecodeString(corruptedSig)
		require.NoError(t, err)

		isValid := ed25519.Verify(pubKey, hashBytes, corruptedSigBytes)
		assert.False(t, isValid, "Corrupted signature should not be valid")
	})
}

func TestEd25519Signer_PayloadGeneration(t *testing.T) {
	signer, err := NewEd25519Signer()
	require.NoError(t, err)

	t.Run("canonical payload format", func(t *testing.T) {
		user := testUserAlice
		docID := "payload-test"
		timestamp := time.Date(2024, 4, 1, 12, 0, 0, 0, time.UTC)
		nonce := "payload-nonce"

		hash1, _, err := signer.CreateSignature(context.Background(), docID, user, timestamp, nonce, "")
		require.NoError(t, err)

		expectedPayload := []byte("doc_id=payload-test\nuser_sub=user-123-alice\nuser_email=alice@example.com\nsigned_at=2024-04-01T12:00:00Z\nnonce=payload-nonce\n")
		expectedHash := sha256.Sum256(expectedPayload)
		expectedHashB64 := base64.StdEncoding.EncodeToString(expectedHash[:])

		assert.Equal(t, expectedHashB64, hash1, "Hash should match canonical payload format")
	})

	t.Run("email normalization in payload", func(t *testing.T) {

		user := &models.User{
			Sub:   "user-email-test",
			Email: "Test.User@EXAMPLE.COM",
			Name:  "Test User",
		}

		docID := "email-test"
		timestamp := time.Date(2024, 5, 1, 10, 0, 0, 0, time.UTC)
		nonce := "email-nonce"

		hash, _, err := signer.CreateSignature(context.Background(), docID, user, timestamp, nonce, "")
		require.NoError(t, err)

		expectedPayload := []byte("doc_id=email-test\nuser_sub=user-email-test\nuser_email=test.user@example.com\nsigned_at=2024-05-01T10:00:00Z\nnonce=email-nonce\n")
		expectedHash := sha256.Sum256(expectedPayload)
		expectedHashB64 := base64.StdEncoding.EncodeToString(expectedHash[:])

		assert.Equal(t, expectedHashB64, hash, "Payload should use normalized lowercase email")
	})

	t.Run("timestamp format consistency", func(t *testing.T) {
		user := testUserCharlie
		docID := "time-format-test"
		nonce := "time-nonce"

		utcTime := time.Date(2024, 6, 1, 15, 30, 45, 123456789, time.UTC)
		localTime := utcTime.In(time.Local)

		hash1, _, err := signer.CreateSignature(context.Background(), docID, user, utcTime, nonce, "")
		require.NoError(t, err)

		hash2, _, err := signer.CreateSignature(context.Background(), docID, user, localTime, nonce, "")
		require.NoError(t, err)

		assert.Equal(t, hash1, hash2, "Different timezone representations of same moment should produce same hash")
	})
}

func TestEd25519Signer_GetPublicKey(t *testing.T) {
	t.Run("returns consistent public key", func(t *testing.T) {
		signer, err := NewEd25519Signer()
		require.NoError(t, err)

		pubKey1 := signer.GetPublicKey()
		pubKey2 := signer.GetPublicKey()

		assert.Equal(t, pubKey1, pubKey2, "Public key should be consistent across calls")
		assert.NotEmpty(t, pubKey1, "Public key should not be empty")
	})

	t.Run("different signers have different public keys", func(t *testing.T) {

		originalKey := os.Getenv("ACKIFY_ED25519_PRIVATE_KEY")
		_ = os.Unsetenv("ACKIFY_ED25519_PRIVATE_KEY")
		defer func() {
			if originalKey != "" {
				_ = os.Setenv("ACKIFY_ED25519_PRIVATE_KEY", originalKey)
			}
		}()

		signer1, err := NewEd25519Signer()
		require.NoError(t, err)

		signer2, err := NewEd25519Signer()
		require.NoError(t, err)

		pubKey1 := signer1.GetPublicKey()
		pubKey2 := signer2.GetPublicKey()

		assert.NotEqual(t, pubKey1, pubKey2, "Different signers should have different public keys")
	})
}

func TestEd25519Signer_InterfaceCompliance(t *testing.T) {
	t.Run("concrete type methods work", func(t *testing.T) {
		signer, err := NewEd25519Signer()
		require.NoError(t, err)

		// Test methods are accessible directly on concrete type
		pubKey := signer.GetPublicKey()
		assert.NotEmpty(t, pubKey)

		user := testUserAlice
		hash, sig, err := signer.CreateSignature(context.Background(), "test", user, time.Now(), "nonce", "")
		assert.NoError(t, err)
		assert.NotEmpty(t, hash)
		assert.NotEmpty(t, sig)
	})
}
