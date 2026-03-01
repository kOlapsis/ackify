// SPDX-License-Identifier: AGPL-3.0-or-later
package shared

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/btouchard/ackify-ce/backend/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockAuditLogger struct {
	events []AuditEvent
	err    error
}

func (m *mockAuditLogger) Log(_ context.Context, event AuditEvent) error {
	m.events = append(m.events, event)
	return m.err
}

func TestEmitAudit_WithLogger(t *testing.T) {
	t.Parallel()

	logger := &mockAuditLogger{}
	ctx := context.WithValue(context.Background(), ContextKeyUser, &models.User{Email: "alice@test.com"})
	req := httptest.NewRequest("POST", "/api/v1/documents", nil)
	req.Header.Set("User-Agent", "TestAgent/1.0")

	EmitAudit(ctx, logger, req, "tenant-123", "document.create", "document", "doc-abc", map[string]any{"title": "Test"})

	require.Len(t, logger.events, 1)
	event := logger.events[0]
	assert.Equal(t, "tenant-123", event.TenantID)
	assert.Equal(t, "alice@test.com", event.UserEmail)
	assert.Equal(t, "document.create", event.Action)
	assert.Equal(t, "document", event.Resource)
	assert.Equal(t, "doc-abc", event.ResourceID)
	assert.Equal(t, "Test", event.Details["title"])
	assert.Equal(t, "TestAgent/1.0", event.UserAgent)
	assert.False(t, event.Timestamp.IsZero())
}

func TestEmitAudit_NilLogger(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("POST", "/api/v1/documents", nil)

	// Should not panic with nil logger
	EmitAudit(context.Background(), nil, req, "tenant-123", "document.create", "document", "doc-abc", nil)
}

func TestEmitAudit_NoUserInContext(t *testing.T) {
	t.Parallel()

	logger := &mockAuditLogger{}
	req := httptest.NewRequest("POST", "/api/v1/documents", nil)

	EmitAudit(context.Background(), logger, req, "tenant-123", "document.create", "document", "doc-abc", nil)

	require.Len(t, logger.events, 1)
	assert.Equal(t, "", logger.events[0].UserEmail)
}

func TestEmitAudit_LoggerError(t *testing.T) {
	t.Parallel()

	logger := &mockAuditLogger{err: errors.New("db failure")}
	req := httptest.NewRequest("POST", "/api/v1/documents", nil)

	// Should not panic even if logger returns error
	EmitAudit(context.Background(), logger, req, "tenant-123", "document.create", "document", "doc-abc", nil)

	// Event was still attempted
	require.Len(t, logger.events, 1)
}
