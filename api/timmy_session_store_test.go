package api

import (
	"context"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimmySessionStore_CreateAndGet(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmySessionStore(db)
	ctx := context.Background()

	session := &models.TimmySession{
		ThreatModelID: "tm-session-001",
		UserID:        "user-001",
		Title:         "Test Session",
		Status:        "active",
	}

	err := store.Create(ctx, session)
	require.NoError(t, err)
	assert.NotEmpty(t, session.ID)

	got, err := store.Get(ctx, session.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, session.ID, got.ID)
	assert.Equal(t, "tm-session-001", got.ThreatModelID)
	assert.Equal(t, "user-001", got.UserID)
	assert.Equal(t, "Test Session", got.Title)
	assert.Equal(t, "active", got.Status)
}

func TestTimmySessionStore_GetNotFound(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmySessionStore(db)
	ctx := context.Background()

	_, err := store.Get(ctx, "nonexistent-id")
	require.Error(t, err)
}

func TestTimmySessionStore_ListByUserAndThreatModel(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmySessionStore(db)
	ctx := context.Background()

	tmID := "tm-session-002"
	aliceID := "user-alice"
	bobID := "user-bob"

	// Create sessions for alice with this TM
	for i := range 3 {
		err := store.Create(ctx, &models.TimmySession{
			ThreatModelID: tmID,
			UserID:        aliceID,
			Title:         "Alice session",
			Status:        "active",
		})
		require.NoError(t, err, "failed creating session %d", i)
	}

	// Create session for bob with the same TM
	err := store.Create(ctx, &models.TimmySession{
		ThreatModelID: tmID,
		UserID:        bobID,
		Title:         "Bob session",
		Status:        "active",
	})
	require.NoError(t, err)

	// Alice's sessions for this TM
	sessions, total, err := store.ListByUserAndThreatModel(ctx, aliceID, tmID, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Len(t, sessions, 3)

	// Bob's sessions for this TM
	bobSessions, bobTotal, err := store.ListByUserAndThreatModel(ctx, bobID, tmID, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, bobTotal)
	assert.Len(t, bobSessions, 1)

	// Pagination
	paginated, pageTotal, err := store.ListByUserAndThreatModel(ctx, aliceID, tmID, 0, 2)
	require.NoError(t, err)
	assert.Equal(t, 3, pageTotal)
	assert.Len(t, paginated, 2)
}

func TestTimmySessionStore_SoftDelete(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmySessionStore(db)
	ctx := context.Background()

	session := &models.TimmySession{
		ThreatModelID: "tm-session-003",
		UserID:        "user-001",
		Title:         "To be deleted",
		Status:        "active",
	}
	err := store.Create(ctx, session)
	require.NoError(t, err)

	// Confirm it exists
	got, err := store.Get(ctx, session.ID)
	require.NoError(t, err)
	require.NotNil(t, got)

	// Soft delete
	err = store.SoftDelete(ctx, session.ID)
	require.NoError(t, err)

	// Get should now return not found
	_, err = store.Get(ctx, session.ID)
	require.Error(t, err)

	// Deleting again should return an error
	err = store.SoftDelete(ctx, session.ID)
	require.Error(t, err)
}

func TestTimmySessionStore_CountActiveByThreatModel(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmySessionStore(db)
	ctx := context.Background()

	tmID := "tm-session-004"

	// Create active and inactive sessions
	err := store.Create(ctx, &models.TimmySession{
		ThreatModelID: tmID,
		UserID:        "user-001",
		Status:        "active",
	})
	require.NoError(t, err)

	err = store.Create(ctx, &models.TimmySession{
		ThreatModelID: tmID,
		UserID:        "user-002",
		Status:        "active",
	})
	require.NoError(t, err)

	// Create one with "closed" status
	err = store.Create(ctx, &models.TimmySession{
		ThreatModelID: tmID,
		UserID:        "user-003",
		Status:        "closed",
	})
	require.NoError(t, err)

	count, err := store.CountActiveByThreatModel(ctx, tmID)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestTimmyMessageStore_CreateAndList(t *testing.T) {
	db := setupTimmyTestDB(t)
	// Create session first for FK satisfaction
	sessionStore := NewGormTimmySessionStore(db)
	msgStore := NewGormTimmyMessageStore(db)
	ctx := context.Background()

	session := &models.TimmySession{
		ThreatModelID: "tm-msg-001",
		UserID:        "user-001",
		Status:        "active",
	}
	err := sessionStore.Create(ctx, session)
	require.NoError(t, err)

	messages := []*models.TimmyMessage{
		{
			SessionID:  session.ID,
			Role:       "user",
			Content:    "Hello, Timmy",
			TokenCount: 5,
			Sequence:   1,
		},
		{
			SessionID:  session.ID,
			Role:       "assistant",
			Content:    "Hello! How can I help you?",
			TokenCount: 8,
			Sequence:   2,
		},
		{
			SessionID:  session.ID,
			Role:       "user",
			Content:    "Tell me about threats",
			TokenCount: 6,
			Sequence:   3,
		},
	}

	for _, msg := range messages {
		err := msgStore.Create(ctx, msg)
		require.NoError(t, err)
		assert.NotEmpty(t, msg.ID)
	}

	results, total, err := msgStore.ListBySession(ctx, session.ID, 0, 10)
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Len(t, results, 3)

	// Verify ordering by sequence ASC
	assert.Equal(t, 1, results[0].Sequence)
	assert.Equal(t, "user", results[0].Role)
	assert.Equal(t, 2, results[1].Sequence)
	assert.Equal(t, "assistant", results[1].Role)
	assert.Equal(t, 3, results[2].Sequence)

	// Pagination
	paged, pageTotal, err := msgStore.ListBySession(ctx, session.ID, 0, 2)
	require.NoError(t, err)
	assert.Equal(t, 3, pageTotal)
	assert.Len(t, paged, 2)
}

func TestTimmyMessageStore_GetNextSequence(t *testing.T) {
	db := setupTimmyTestDB(t)
	sessionStore := NewGormTimmySessionStore(db)
	msgStore := NewGormTimmyMessageStore(db)
	ctx := context.Background()

	session := &models.TimmySession{
		ThreatModelID: "tm-msg-002",
		UserID:        "user-001",
		Status:        "active",
	}
	err := sessionStore.Create(ctx, session)
	require.NoError(t, err)

	// No messages yet: should start at 1
	seq, err := msgStore.GetNextSequence(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, seq)

	// Add a message with sequence 1
	err = msgStore.Create(ctx, &models.TimmyMessage{
		SessionID: session.ID,
		Role:      "user",
		Content:   "First message",
		Sequence:  seq,
	})
	require.NoError(t, err)

	// Next should be 2
	seq, err = msgStore.GetNextSequence(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, seq)

	// Add another message
	err = msgStore.Create(ctx, &models.TimmyMessage{
		SessionID: session.ID,
		Role:      "assistant",
		Content:   "Second message",
		Sequence:  seq,
	})
	require.NoError(t, err)

	// Next should be 3
	seq, err = msgStore.GetNextSequence(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, 3, seq)
}
