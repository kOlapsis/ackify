// SPDX-License-Identifier: AGPL-3.0-or-later
//go:build integration

package database

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/kolapsis/ackify/backend/pkg/models"
)

func TestMagicLinkRepository_CreateToken(t *testing.T) {
	testDB := SetupTestDB(t)

	repo := NewMagicLinkRepository(testDB.DB)
	ctx := context.Background()

	token := &models.MagicLinkToken{
		Token:              "test-token-123",
		Email:              "test@example.com",
		ExpiresAt:          time.Now().Add(15 * time.Minute),
		RedirectTo:         "/dashboard",
		CreatedByIP:        "192.168.1.1",
		CreatedByUserAgent: "Mozilla/5.0",
	}

	err := repo.CreateToken(ctx, token)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	if token.ID == 0 {
		t.Error("Expected token ID to be set")
	}
	if token.CreatedAt.IsZero() {
		t.Error("Expected created_at to be set")
	}
}

func TestMagicLinkRepository_GetByToken(t *testing.T) {
	testDB := SetupTestDB(t)

	repo := NewMagicLinkRepository(testDB.DB)
	ctx := context.Background()

	// Créer un token
	original := &models.MagicLinkToken{
		Token:              "test-token-456",
		Email:              "user@example.com",
		ExpiresAt:          time.Now().Add(15 * time.Minute),
		RedirectTo:         "/",
		CreatedByIP:        "10.0.0.1",
		CreatedByUserAgent: "Chrome",
	}

	err := repo.CreateToken(ctx, original)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	// Récupérer le token
	retrieved, err := repo.GetByToken(ctx, "test-token-456")
	if err != nil {
		t.Fatalf("Failed to get token: %v", err)
	}

	if retrieved.Email != original.Email {
		t.Errorf("Expected email %s, got %s", original.Email, retrieved.Email)
	}
	if retrieved.UsedAt != nil {
		t.Error("Expected token to not be used")
	}
	if !retrieved.IsValid() {
		t.Error("Expected token to be valid")
	}
}

func TestMagicLinkRepository_MarkAsUsed(t *testing.T) {
	testDB := SetupTestDB(t)

	repo := NewMagicLinkRepository(testDB.DB)
	ctx := context.Background()

	// Créer un token
	token := &models.MagicLinkToken{
		Token:              "test-token-789",
		Email:              "mark@example.com",
		ExpiresAt:          time.Now().Add(15 * time.Minute),
		RedirectTo:         "/",
		CreatedByIP:        "10.0.0.2",
		CreatedByUserAgent: "Firefox",
	}

	err := repo.CreateToken(ctx, token)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	// Marquer comme utilisé
	err = repo.MarkAsUsed(ctx, token.Token, "10.0.0.3", "Safari")
	if err != nil {
		t.Fatalf("Failed to mark token as used: %v", err)
	}

	// Vérifier que c'est bien marqué
	retrieved, err := repo.GetByToken(ctx, token.Token)
	if err != nil {
		t.Fatalf("Failed to get token: %v", err)
	}

	if retrieved.UsedAt == nil {
		t.Error("Expected token to be marked as used")
	}
	if retrieved.IsValid() {
		t.Error("Expected token to be invalid after use")
	}

	// Tenter de marquer à nouveau (devrait échouer)
	err = repo.MarkAsUsed(ctx, token.Token, "10.0.0.4", "Edge")
	if err != sql.ErrNoRows {
		t.Errorf("Expected ErrNoRows when marking already used token, got %v", err)
	}
}

func TestMagicLinkRepository_DeleteExpired(t *testing.T) {
	testDB := SetupTestDB(t)

	repo := NewMagicLinkRepository(testDB.DB)
	ctx := context.Background()

	// Créer un token expiré
	expiredToken := &models.MagicLinkToken{
		Token:              "expired-token",
		Email:              "expired@example.com",
		ExpiresAt:          time.Now().Add(-1 * time.Hour), // Expiré
		RedirectTo:         "/",
		CreatedByIP:        "10.0.0.1",
		CreatedByUserAgent: "Test",
	}

	err := repo.CreateToken(ctx, expiredToken)
	if err != nil {
		t.Fatalf("Failed to create expired token: %v", err)
	}

	// Créer un token valide
	validToken := &models.MagicLinkToken{
		Token:              "valid-token",
		Email:              "valid@example.com",
		ExpiresAt:          time.Now().Add(15 * time.Minute),
		RedirectTo:         "/",
		CreatedByIP:        "10.0.0.2",
		CreatedByUserAgent: "Test",
	}

	err = repo.CreateToken(ctx, validToken)
	if err != nil {
		t.Fatalf("Failed to create valid token: %v", err)
	}

	// Supprimer les tokens expirés
	deleted, err := repo.DeleteExpired(ctx)
	if err != nil {
		t.Fatalf("Failed to delete expired tokens: %v", err)
	}

	if deleted == 0 {
		t.Error("Expected at least one token to be deleted")
	}

	// Vérifier que le token expiré est supprimé
	_, err = repo.GetByToken(ctx, "expired-token")
	if err != sql.ErrNoRows {
		t.Error("Expected expired token to be deleted")
	}

	// Vérifier que le token valide existe toujours
	_, err = repo.GetByToken(ctx, "valid-token")
	if err != nil {
		t.Error("Expected valid token to still exist")
	}
}

func TestMagicLinkRepository_RateLimit(t *testing.T) {
	testDB := SetupTestDB(t)

	repo := NewMagicLinkRepository(testDB.DB)
	ctx := context.Background()

	email := "ratelimit@example.com"
	ip := "192.168.1.100"

	// Créer 5 tentatives
	for i := 0; i < 5; i++ {
		attempt := &models.MagicLinkAuthAttempt{
			Email:     email,
			Success:   true,
			IPAddress: ip,
			UserAgent: "Test",
		}
		err := repo.LogAttempt(ctx, attempt)
		if err != nil {
			t.Fatalf("Failed to log attempt: %v", err)
		}
	}

	// Compter les tentatives récentes (dernière heure)
	since := time.Now().Add(-1 * time.Hour)
	count, err := repo.CountRecentAttempts(ctx, email, since)
	if err != nil {
		t.Fatalf("Failed to count attempts: %v", err)
	}

	if count != 5 {
		t.Errorf("Expected 5 attempts, got %d", count)
	}

	// Compter par IP
	countIP, err := repo.CountRecentAttemptsByIP(ctx, ip, since)
	if err != nil {
		t.Fatalf("Failed to count attempts by IP: %v", err)
	}

	if countIP != 5 {
		t.Errorf("Expected 5 attempts by IP, got %d", countIP)
	}

	// Compter les tentatives anciennes (devrait être 0)
	oldSince := time.Now().Add(-2 * time.Hour)
	oldCount, err := repo.CountRecentAttempts(ctx, email, oldSince)
	if err != nil {
		t.Fatalf("Failed to count old attempts: %v", err)
	}

	if oldCount != 5 {
		t.Errorf("Expected 5 old attempts, got %d", oldCount)
	}
}

func TestMagicLinkRepository_LogAttempt(t *testing.T) {
	testDB := SetupTestDB(t)

	repo := NewMagicLinkRepository(testDB.DB)
	ctx := context.Background()

	attempt := &models.MagicLinkAuthAttempt{
		Email:         "test@example.com",
		Success:       true,
		FailureReason: "",
		IPAddress:     "192.168.1.1",
		UserAgent:     "Mozilla/5.0",
	}

	err := repo.LogAttempt(ctx, attempt)
	if err != nil {
		t.Fatalf("Failed to log attempt: %v", err)
	}

	if attempt.ID == 0 {
		t.Error("Expected attempt ID to be set")
	}
	if attempt.AttemptedAt.IsZero() {
		t.Error("Expected attempted_at to be set")
	}
}

func TestMagicLinkRepository_TokenExpiration(t *testing.T) {
	testDB := SetupTestDB(t)

	repo := NewMagicLinkRepository(testDB.DB)
	ctx := context.Background()

	// Créer un token qui expire dans 1 seconde
	token := &models.MagicLinkToken{
		Token:              "expiring-token",
		Email:              "expiring@example.com",
		ExpiresAt:          time.Now().Add(1 * time.Second),
		RedirectTo:         "/",
		CreatedByIP:        "10.0.0.1",
		CreatedByUserAgent: "Test",
	}

	err := repo.CreateToken(ctx, token)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	// Vérifier que le token est valide
	retrieved, err := repo.GetByToken(ctx, "expiring-token")
	if err != nil {
		t.Fatalf("Failed to get token: %v", err)
	}

	if !retrieved.IsValid() {
		t.Error("Expected token to be valid")
	}

	// Attendre 2 secondes
	time.Sleep(2 * time.Second)

	// Récupérer à nouveau le token
	retrieved, err = repo.GetByToken(ctx, "expiring-token")
	if err != nil {
		t.Fatalf("Failed to get token: %v", err)
	}

	// Vérifier que le token est maintenant invalide
	if retrieved.IsValid() {
		t.Error("Expected token to be invalid after expiration")
	}
}
