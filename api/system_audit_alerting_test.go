package api

import (
	"context"
	"errors"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureEmitter captures EventPayloads for test assertion without a real Redis connection.
type captureEmitter struct{ events []EventPayload }

func (c *captureEmitter) EmitEvent(_ context.Context, p EventPayload) error {
	c.events = append(c.events, p)
	return nil
}

// stubSysAuditRepo is a minimal SystemAuditRepository stub for decorator tests.
type stubSysAuditRepo struct {
	SystemAuditRepository
	createErr error
	created   []models.SystemAuditEntry
}

func (s *stubSysAuditRepo) Create(_ context.Context, e models.SystemAuditEntry) error {
	if s.createErr != nil {
		return s.createErr
	}
	s.created = append(s.created, e)
	return nil
}

func TestAlertingRepo_EmitsOnCreate(t *testing.T) {
	em := &captureEmitter{}
	inner := &stubSysAuditRepo{}
	repo := NewAlertingSystemAuditRepository(inner, em, "test-operator")

	entry := models.SystemAuditEntry{
		ID:         models.DBVarchar("e-1"),
		ActorEmail: models.DBVarchar("charlie@tmi.local"),
		HTTPMethod: models.DBVarchar("PUT"),
		HTTPPath:   models.DBText("/admin/settings/x"),
		FieldPath:  models.DBVarchar("x"),
	}
	require.NoError(t, repo.Create(context.Background(), entry))

	require.Len(t, em.events, 1)
	ev := em.events[0]
	assert.Equal(t, EventSystemAuditAdminWrite, ev.EventType)
	assert.Equal(t, "e-1", ev.Data["entry_id"])
	assert.Equal(t, "charlie@tmi.local", ev.Data["actor_email"])
	assert.Equal(t, "test-operator", ev.Data["operator_name"])
}

func TestAlertingRepo_NoEmitOnCreateFailure(t *testing.T) {
	em := &captureEmitter{}
	inner := &stubSysAuditRepo{createErr: errors.New("db down")}
	repo := NewAlertingSystemAuditRepository(inner, em, "test-operator")

	err := repo.Create(context.Background(), models.SystemAuditEntry{})
	require.Error(t, err)
	assert.Empty(t, em.events, "no alert must be emitted when the inner Create fails")
}
