package api

import (
	"context"
	"errors"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
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

// TestAlertingRepo_EntryIDGeneratedWhenEmpty verifies Fix 2: when the caller
// does not pre-populate entry.ID, the decorator assigns a UUID before
// delegating, so the emitted payload's entry_id and ObjectID are a non-empty
// parseable UUID that matches what the inner repo received.
func TestAlertingRepo_EntryIDGeneratedWhenEmpty(t *testing.T) {
	em := &captureEmitter{}
	inner := &stubSysAuditRepo{}
	repo := NewAlertingSystemAuditRepository(inner, em, "op")

	entry := models.SystemAuditEntry{
		// ID intentionally empty — decorator must assign one
		ActorEmail: models.DBVarchar("alice@tmi.local"),
		HTTPMethod: models.DBVarchar("PATCH"),
		HTTPPath:   models.DBText("/admin/settings/y"),
		FieldPath:  models.DBVarchar("y"),
	}
	require.NoError(t, repo.Create(context.Background(), entry))

	require.Len(t, em.events, 1)
	ev := em.events[0]

	// entry_id in Data must be a valid, non-empty UUID.
	entryID, ok := ev.Data["entry_id"].(string)
	require.True(t, ok, "entry_id must be a string")
	parsedID, err := uuid.Parse(entryID)
	require.NoError(t, err, "entry_id must be a valid UUID; got %q", entryID)

	// ObjectID on the payload must match entry_id.
	assert.Equal(t, entryID, ev.ObjectID, "ObjectID must equal entry_id")

	// The inner repo must have received the same ID that was emitted.
	require.Len(t, inner.created, 1)
	assert.Equal(t, entryID, string(inner.created[0].ID),
		"inner repo must receive the same ID as the emitted payload")

	_ = parsedID // used implicitly via uuid.Parse
}

// TestAlertingRepo_EntryIDPreservedWhenSet verifies that a caller-supplied
// ID is not overwritten by the decorator.
func TestAlertingRepo_EntryIDPreservedWhenSet(t *testing.T) {
	em := &captureEmitter{}
	inner := &stubSysAuditRepo{}
	repo := NewAlertingSystemAuditRepository(inner, em, "op")

	const callerID = "aaaabbbb-cccc-dddd-eeee-ffffffffffff"
	entry := models.SystemAuditEntry{
		ID:         models.DBVarchar(callerID),
		ActorEmail: models.DBVarchar("bob@tmi.local"),
		HTTPMethod: models.DBVarchar("DELETE"),
		HTTPPath:   models.DBText("/admin/users/1"),
		FieldPath:  models.DBVarchar("users"),
	}
	require.NoError(t, repo.Create(context.Background(), entry))

	require.Len(t, em.events, 1)
	assert.Equal(t, callerID, em.events[0].Data["entry_id"])
	assert.Equal(t, callerID, em.events[0].ObjectID)
	require.Len(t, inner.created, 1)
	assert.Equal(t, callerID, string(inner.created[0].ID))
}

func TestAlertingRepo_NoEmitOnCreateFailure(t *testing.T) {
	em := &captureEmitter{}
	inner := &stubSysAuditRepo{createErr: errors.New("db down")}
	repo := NewAlertingSystemAuditRepository(inner, em, "test-operator")

	err := repo.Create(context.Background(), models.SystemAuditEntry{})
	require.Error(t, err)
	assert.Empty(t, em.events, "no alert must be emitted when the inner Create fails")
}
