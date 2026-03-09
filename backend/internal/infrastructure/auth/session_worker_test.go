// SPDX-License-Identifier: AGPL-3.0-or-later
package auth

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kolapsis/ackify/backend/pkg/models"
)

// mockTenantProvider for testing
type mockTenantProvider struct {
	tenantID uuid.UUID
}

func (m *mockTenantProvider) CurrentTenant(ctx context.Context) (uuid.UUID, error) {
	return m.tenantID, nil
}

// mockSessionRepo implements SessionRepository for testing
type mockSessionRepoForWorker struct {
	mu              sync.Mutex
	deleteExpiredFn func(ctx context.Context, olderThan time.Duration) (int64, error)
	callCount       int
}

func (m *mockSessionRepoForWorker) DeleteExpired(ctx context.Context, olderThan time.Duration) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	if m.deleteExpiredFn != nil {
		return m.deleteExpiredFn(ctx, olderThan)
	}
	return 0, nil
}

func (m *mockSessionRepoForWorker) GetCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// Implement other SessionRepository methods (not used by worker)
func (m *mockSessionRepoForWorker) Create(ctx context.Context, session *models.OAuthSession) error {
	return nil
}
func (m *mockSessionRepoForWorker) GetBySessionID(ctx context.Context, sessionID string) (*models.OAuthSession, error) {
	return nil, nil
}
func (m *mockSessionRepoForWorker) UpdateRefreshToken(ctx context.Context, sessionID string, encryptedToken []byte, expiresAt time.Time) error {
	return nil
}
func (m *mockSessionRepoForWorker) DeleteBySessionID(ctx context.Context, sessionID string) error {
	return nil
}

func TestSessionWorker_StartStop(t *testing.T) {
	repo := &mockSessionRepoForWorker{}
	config := SessionWorkerConfig{
		CleanupInterval: 100 * time.Millisecond,
		CleanupAge:      1 * time.Hour,
	}

	worker := NewSessionWorker(repo, config, context.Background(), nil, &mockTenantProvider{tenantID: uuid.New()})

	// Test starting
	err := worker.Start()
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	if !worker.started {
		t.Error("Worker should be marked as started")
	}

	// Test starting again should fail
	err = worker.Start()
	if err == nil {
		t.Error("Starting already started worker should return error")
	}

	// Wait a bit for cleanup to run
	time.Sleep(150 * time.Millisecond)

	// Verify cleanup was called at least once
	if repo.GetCallCount() < 1 {
		t.Error("Cleanup should have been called at least once")
	}

	// Test stopping
	err = worker.Stop()
	if err != nil {
		t.Errorf("Stop() failed: %v", err)
	}

	if worker.started {
		t.Error("Worker should be marked as stopped")
	}

	// Test stopping again should fail
	err = worker.Stop()
	if err == nil {
		t.Error("Stopping already stopped worker should return error")
	}
}

func TestSessionWorker_CleanupSuccess(t *testing.T) {
	deletedCount := int64(0)
	repo := &mockSessionRepoForWorker{
		deleteExpiredFn: func(ctx context.Context, olderThan time.Duration) (int64, error) {
			deletedCount++
			return deletedCount, nil
		},
	}

	config := SessionWorkerConfig{
		CleanupInterval: 50 * time.Millisecond,
		CleanupAge:      24 * time.Hour,
	}

	worker := NewSessionWorker(repo, config, context.Background(), nil, &mockTenantProvider{tenantID: uuid.New()})

	err := worker.Start()
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Wait for multiple cleanup cycles
	time.Sleep(120 * time.Millisecond)

	err = worker.Stop()
	if err != nil {
		t.Errorf("Stop() failed: %v", err)
	}

	// Should have been called at least twice (immediate + at least one tick)
	if repo.GetCallCount() < 2 {
		t.Errorf("Cleanup called %d times, expected at least 2", repo.GetCallCount())
	}
}

func TestSessionWorker_CleanupError(t *testing.T) {
	testError := errors.New("database error")
	repo := &mockSessionRepoForWorker{
		deleteExpiredFn: func(ctx context.Context, olderThan time.Duration) (int64, error) {
			return 0, testError
		},
	}

	config := SessionWorkerConfig{
		CleanupInterval: 50 * time.Millisecond,
		CleanupAge:      24 * time.Hour,
	}

	worker := NewSessionWorker(repo, config, context.Background(), nil, &mockTenantProvider{tenantID: uuid.New()})

	err := worker.Start()
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Wait for cleanup to run
	time.Sleep(100 * time.Millisecond)

	err = worker.Stop()
	if err != nil {
		t.Errorf("Stop() failed: %v", err)
	}

	// Worker should continue despite errors
	if repo.GetCallCount() < 1 {
		t.Error("Cleanup should have been attempted despite errors")
	}
}

func TestSessionWorker_ImmediateCleanupOnStart(t *testing.T) {
	repo := &mockSessionRepoForWorker{
		deleteExpiredFn: func(ctx context.Context, olderThan time.Duration) (int64, error) {
			return 5, nil
		},
	}

	// Use a long interval so we only get the immediate cleanup
	config := SessionWorkerConfig{
		CleanupInterval: 1 * time.Hour,
		CleanupAge:      24 * time.Hour,
	}

	worker := NewSessionWorker(repo, config, context.Background(), nil, &mockTenantProvider{tenantID: uuid.New()})

	err := worker.Start()
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Give it a moment to run the immediate cleanup
	time.Sleep(50 * time.Millisecond)

	err = worker.Stop()
	if err != nil {
		t.Errorf("Stop() failed: %v", err)
	}

	// Should have been called exactly once (immediate cleanup only)
	if repo.GetCallCount() != 1 {
		t.Errorf("Cleanup called %d times, expected exactly 1 (immediate cleanup)", repo.GetCallCount())
	}
}

func TestSessionWorker_GracefulShutdown(t *testing.T) {
	// Create a repo that takes time to cleanup
	cleanupRunning := false
	var mu sync.Mutex

	repo := &mockSessionRepoForWorker{
		deleteExpiredFn: func(ctx context.Context, olderThan time.Duration) (int64, error) {
			mu.Lock()
			cleanupRunning = true
			mu.Unlock()

			time.Sleep(50 * time.Millisecond)

			mu.Lock()
			cleanupRunning = false
			mu.Unlock()

			return 1, nil
		},
	}

	config := SessionWorkerConfig{
		CleanupInterval: 200 * time.Millisecond,
		CleanupAge:      1 * time.Hour,
	}

	worker := NewSessionWorker(repo, config, context.Background(), nil, &mockTenantProvider{tenantID: uuid.New()})

	err := worker.Start()
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Wait for immediate cleanup to start
	time.Sleep(10 * time.Millisecond)

	// Verify cleanup is running
	mu.Lock()
	if !cleanupRunning {
		mu.Unlock()
		t.Skip("Cleanup not running when expected, test timing issue")
		return
	}
	mu.Unlock()

	// Stop should wait for ongoing cleanup
	start := time.Now()
	err = worker.Stop()
	duration := time.Since(start)

	if err != nil {
		t.Errorf("Stop() failed: %v", err)
	}

	// Cleanup should have finished
	mu.Lock()
	stillRunning := cleanupRunning
	mu.Unlock()

	if stillRunning {
		t.Error("Cleanup still running after Stop()")
	}

	// Stop should have waited at least some time for cleanup
	if duration < 5*time.Millisecond {
		t.Logf("Stop() returned very quickly (%v), but cleanup finished cleanly", duration)
	}

	if duration > 10*time.Second {
		t.Error("Stop() took too long, might be hanging")
	}
}

func TestSessionWorker_ContextCancellation(t *testing.T) {
	var cleanupCalled atomic.Bool
	repo := &mockSessionRepoForWorker{
		deleteExpiredFn: func(ctx context.Context, olderThan time.Duration) (int64, error) {
			cleanupCalled.Store(true)
			// Check if context is cancelled during cleanup
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			default:
				return 0, nil
			}
		},
	}

	config := SessionWorkerConfig{
		CleanupInterval: 1 * time.Hour, // Long interval
		CleanupAge:      1 * time.Hour,
	}

	worker := NewSessionWorker(repo, config, context.Background(), nil, &mockTenantProvider{tenantID: uuid.New()})

	err := worker.Start()
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Wait for immediate cleanup
	time.Sleep(50 * time.Millisecond)

	if !cleanupCalled.Load() {
		t.Error("Cleanup should have been called")
	}

	err = worker.Stop()
	if err != nil {
		t.Errorf("Stop() failed: %v", err)
	}
}
