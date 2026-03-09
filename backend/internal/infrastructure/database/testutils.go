//go:build integration

// SPDX-License-Identifier: AGPL-3.0-or-later
package database

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/kolapsis/ackify/backend/internal/infrastructure/tenant"
	"github.com/kolapsis/ackify/backend/pkg/models"
	"github.com/kolapsis/ackify/backend/pkg/providers"
	_ "github.com/lib/pq"
)

type TestDB struct {
	DB             *sql.DB
	DSN            string
	dbName         string
	TenantProvider providers.TenantProvider
}

func SetupTestDB(t *testing.T) *TestDB {
	t.Helper()

	if os.Getenv("INTEGRATION_TESTS") == "" {
		t.Skip("Skipping integrations test (INTEGRATION_TESTS not set)")
	}

	dsn := os.Getenv("ACKIFY_DB_DSN")
	if dsn == "" {
		dsn = "postgres://postgres:testpassword@localhost:5432/ackify_test?sslmode=disable"
	}

	// Create unique test database name to enable parallel test execution
	// Format: testdb_{random}_{testname}
	// Use random bytes to ensure uniqueness even when tests start at the same nanosecond
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		t.Fatalf("Failed to generate random database name: %v", err)
	}
	randomHex := hex.EncodeToString(randomBytes)

	// PostgreSQL converts unquoted identifiers to lowercase, so we normalize to lowercase
	testName := strings.ReplaceAll(t.Name(), "/", "_")
	testName = strings.ReplaceAll(testName, " ", "_")
	testName = strings.ToLower(testName)
	// Limit testName to avoid exceeding PostgreSQL's 63-character limit
	if len(testName) > 30 {
		testName = testName[:30]
	}
	dbName := fmt.Sprintf("testdb_%s_%s", randomHex, testName)

	// Truncate database name to PostgreSQL's 63-character limit
	if len(dbName) > 63 {
		dbName = dbName[:63]
	}

	// Connect to default postgres database to create test database
	mainDSN := strings.Replace(dsn, "/ackify_test?", "/postgres?", 1)
	mainDB, err := sql.Open("postgres", mainDSN)
	if err != nil {
		t.Fatalf("Failed to connect to postgres database: %v", err)
	}
	defer mainDB.Close()

	// Create unique test database
	_, err = mainDB.Exec(fmt.Sprintf("CREATE DATABASE %s", quoteIdentifier(dbName)))
	if err != nil {
		t.Fatalf("Failed to create test database %s: %v", dbName, err)
	}

	// Connect to the new test database
	testDSN := strings.Replace(dsn, "/ackify_test?", fmt.Sprintf("/%s?", dbName), 1)
	db, err := sql.Open("postgres", testDSN)
	if err != nil {
		t.Fatalf("Failed to connect to test database %s: %v", dbName, err)
	}

	if err := db.Ping(); err != nil {
		t.Fatalf("Failed to ping test database %s: %v", dbName, err)
	}

	testDB := &TestDB{
		DB:     db,
		DSN:    testDSN,
		dbName: dbName,
	}

	if err := testDB.createSchema(); err != nil {
		t.Fatalf("Failed to create test schema in %s: %v", dbName, err)
	}

	// Initialize tenant provider after migrations (instance_metadata table exists)
	tenantProvider, err := tenant.NewSingleTenantProviderWithContext(context.Background(), db)
	if err != nil {
		t.Fatalf("Failed to create tenant provider in %s: %v", dbName, err)
	}
	testDB.TenantProvider = tenantProvider

	t.Cleanup(func() {
		testDB.Cleanup()

		// Drop the test database after cleanup
		mainDB, err := sql.Open("postgres", mainDSN)
		if err == nil {
			defer mainDB.Close()
			// Force close any remaining connections
			_, _ = mainDB.Exec(`
				SELECT pg_terminate_backend(pg_stat_activity.pid)
				FROM pg_stat_activity
				WHERE pg_stat_activity.datname = $1
				AND pid <> pg_backend_pid()
			`, dbName)
			// Drop the database
			_, _ = mainDB.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s", quoteIdentifier(dbName)))
		}
	})

	return testDB
}

func (tdb *TestDB) createSchema() error {
	// Create ackify_app role if it doesn't exist (required by migration 0016_add_rls_policies.up.sql)
	// This mirrors what cmd/migrate/main.go does with ensureAppRole()
	if err := tdb.ensureAppRole(); err != nil {
		return fmt.Errorf("failed to ensure ackify_app role: %w", err)
	}

	// Find migrations directory
	migrationsPath := os.Getenv("MIGRATIONS_PATH")
	if migrationsPath == "" {
		// Try to find migrations directory by walking up from current directory
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}

		// Walk up the directory tree looking for migrations directory
		found := false
		searchDir := wd
		for i := 0; i < 10; i++ {
			// Try migrations in current directory
			testPath := filepath.Join(searchDir, "migrations")
			if stat, err := os.Stat(testPath); err == nil && stat.IsDir() {
				migrationsPath = testPath
				found = true
				break
			}

			// Try backend/migrations (for root project directory)
			testPath = filepath.Join(searchDir, "backend", "migrations")
			if stat, err := os.Stat(testPath); err == nil && stat.IsDir() {
				migrationsPath = testPath
				found = true
				break
			}

			parent := filepath.Dir(searchDir)
			if parent == searchDir {
				break // Reached root
			}
			searchDir = parent
		}

		if !found {
			return fmt.Errorf("migrations directory not found (searched from %s)", wd)
		}
	}

	// Get absolute path
	absPath, err := filepath.Abs(migrationsPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for migrations: %w", err)
	}

	// Create postgres driver instance
	driver, err := postgres.WithInstance(tdb.DB, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("failed to create postgres driver: %w", err)
	}

	// Create migrator
	m, err := migrate.NewWithDatabaseInstance(
		fmt.Sprintf("file://%s", absPath),
		"postgres",
		driver,
	)
	if err != nil {
		return fmt.Errorf("failed to create migrator: %w", err)
	}

	// Apply all migrations
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	return nil
}

func (tdb *TestDB) Cleanup() {
	if tdb.DB != nil {
		// Drop all tables to ensure clean state
		// This is more reliable than running migrations down
		_, _ = tdb.DB.Exec(`
			DROP TABLE IF EXISTS signatures CASCADE;
			DROP TABLE IF EXISTS documents CASCADE;
			DROP TABLE IF EXISTS expected_signers CASCADE;
			DROP TABLE IF EXISTS reminder_logs CASCADE;
			DROP TABLE IF EXISTS checksum_verifications CASCADE;
			DROP TABLE IF EXISTS email_queue CASCADE;
			DROP TABLE IF EXISTS schema_migrations CASCADE;
		`)

		_ = tdb.DB.Close()
	}
}

func (tdb *TestDB) ClearTable(t *testing.T) {
	t.Helper()
	_, err := tdb.DB.Exec("TRUNCATE TABLE signatures RESTART IDENTITY")
	if err != nil {
		t.Fatalf("Failed to clear signatures table: %v", err)
	}
}

func (tdb *TestDB) GetTableCount(t *testing.T) int {
	t.Helper()
	var count int
	err := tdb.DB.QueryRow("SELECT COUNT(*) FROM signatures").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to get table count: %v", err)
	}
	return count
}

type SignatureFactory struct{}

func (f *SignatureFactory) CreateValidSignature() *models.Signature {
	now := time.Now().UTC()
	referer := "https://example.com/doc"

	return &models.Signature{
		DocID:       "test-doc-123",
		UserSub:     "user-123",
		UserEmail:   "test@example.com",
		UserName:    "Test User",
		SignedAtUTC: now,
		PayloadHash: "dGVzdC1wYXlsb2FkLWhhc2g=", // base64("test-payload-hash")
		Signature:   "dGVzdC1zaWduYXR1cmU=",     // base64("test-signature")
		Nonce:       "test-nonce-123",
		Referer:     &referer,
		PrevHash:    nil, // Will be set for chained signatures
	}
}

func (f *SignatureFactory) CreateSignatureWithDoc(docID string) *models.Signature {
	sig := f.CreateValidSignature()
	sig.DocID = docID
	return sig
}

func (f *SignatureFactory) CreateSignatureWithUser(userSub, userEmail string) *models.Signature {
	sig := f.CreateValidSignature()
	sig.UserSub = userSub
	sig.UserEmail = userEmail
	return sig
}

func (f *SignatureFactory) CreateSignatureWithDocAndUser(docID, userSub, userEmail string) *models.Signature {
	sig := f.CreateValidSignature()
	sig.DocID = docID
	sig.UserSub = userSub
	sig.UserEmail = userEmail
	return sig
}

func (f *SignatureFactory) CreateChainedSignature(prevHashB64 string) *models.Signature {
	sig := f.CreateValidSignature()
	sig.PrevHash = &prevHashB64
	return sig
}

func (f *SignatureFactory) CreateMinimalSignature() *models.Signature {
	now := time.Now().UTC()

	return &models.Signature{
		DocID:       "minimal-doc",
		UserSub:     "minimal-user",
		UserEmail:   "minimal@example.com",
		UserName:    "", // Empty string
		SignedAtUTC: now,
		PayloadHash: "bWluaW1hbA==", // base64("minimal")
		Signature:   "bWluaW1hbA==", // base64("minimal")
		Nonce:       "minimal-nonce",
		Referer:     nil, // NULL
		PrevHash:    nil, // NULL
	}
}

// AssertSignatureEqual compares two signatures for testing
func AssertSignatureEqual(t *testing.T, expected, actual *models.Signature) {
	t.Helper()

	if actual.DocID != expected.DocID {
		t.Errorf("DocID mismatch: got %s, want %s", actual.DocID, expected.DocID)
	}

	if actual.UserSub != expected.UserSub {
		t.Errorf("UserSub mismatch: got %s, want %s", actual.UserSub, expected.UserSub)
	}

	if actual.UserEmail != expected.UserEmail {
		t.Errorf("UserEmail mismatch: got %s, want %s", actual.UserEmail, expected.UserEmail)
	}

	if actual.UserName != expected.UserName {
		t.Errorf("UserName mismatch: got %s, want %s", actual.UserName, expected.UserName)
	}

	if actual.PayloadHash != expected.PayloadHash {
		t.Errorf("PayloadHash mismatch: got %s, want %s", actual.PayloadHash, expected.PayloadHash)
	}

	if actual.Signature != expected.Signature {
		t.Errorf("Signature mismatch: got %s, want %s", actual.Signature, expected.Signature)
	}

	if actual.Nonce != expected.Nonce {
		t.Errorf("Nonce mismatch: got %s, want %s", actual.Nonce, expected.Nonce)
	}

	if !isStringPtrEqual(actual.Referer, expected.Referer) {
		t.Errorf("Referer mismatch: got %v, want %v", actual.Referer, expected.Referer)
	}

	if !isStringPtrEqual(actual.PrevHash, expected.PrevHash) {
		t.Errorf("PrevHash mismatch: got %v, want %v", actual.PrevHash, expected.PrevHash)
	}
}

func isStringPtrEqual(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func NewSignatureFactory() *SignatureFactory {
	return &SignatureFactory{}
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// ensureAppRole creates the ackify_app role if it doesn't exist.
// This mirrors cmd/migrate/main.go ensureAppRole() behavior for tests.
// The role is created with NOLOGIN (no password) - sufficient for running migrations.
func (tdb *TestDB) ensureAppRole() error {
	// Check if role exists
	var exists bool
	err := tdb.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = 'ackify_app')").Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check if ackify_app role exists: %w", err)
	}

	if exists {
		return nil // Role already exists
	}

	// Create the role with NOLOGIN (no password needed for tests)
	createSQL := `
		CREATE ROLE ackify_app WITH
			NOLOGIN
			NOCREATEDB
			NOCREATEROLE
			NOINHERIT
			NOREPLICATION
			CONNECTION LIMIT -1
	`
	_, err = tdb.DB.Exec(createSQL)
	if err != nil {
		return fmt.Errorf("failed to create ackify_app role: %w", err)
	}

	// Grant CONNECT on database
	var dbName string
	err = tdb.DB.QueryRow("SELECT current_database()").Scan(&dbName)
	if err != nil {
		return fmt.Errorf("failed to get current database name: %w", err)
	}

	_, err = tdb.DB.Exec(fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO ackify_app", quoteIdentifier(dbName)))
	if err != nil {
		return fmt.Errorf("failed to grant CONNECT to ackify_app: %w", err)
	}

	// Grant USAGE on public schema
	_, err = tdb.DB.Exec("GRANT USAGE ON SCHEMA public TO ackify_app")
	if err != nil {
		return fmt.Errorf("failed to grant USAGE on public schema: %w", err)
	}

	return nil
}
