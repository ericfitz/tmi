package api

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupAdminAuditListDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.AuditEntry{}))
	return db
}

func seedAdminAuditEntry(t *testing.T, db *gorm.DB, actorEmail, provider, tmID string, ageMinutes int) string {
	t.Helper()
	v := 1
	e := models.AuditEntry{
		ThreatModelID: models.DBVarchar(tmID),
		ObjectType:    models.DBVarchar("threat_model"),
		ObjectID:      models.DBVarchar(uuid.New().String()),
		Version:       &v,
		ChangeType:    models.DBVarchar("created"),
		ActorEmail:    models.DBVarchar(actorEmail),
		ActorProvider: models.DBVarchar(provider),
	}
	require.NoError(t, db.Create(&e).Error)
	ts := time.Now().UTC().Add(-time.Duration(ageMinutes) * time.Minute)
	require.NoError(t, db.Exec("UPDATE audit_entries SET created_at = ? WHERE id = ?", ts, e.ID).Error)
	return string(e.ID)
}

func TestListAuditEntriesAdmin_CursorIteration(t *testing.T) {
	db := setupAdminAuditListDB(t)
	tmA, tmB := uuid.New().String(), uuid.New().String()
	// 5 entries across two TMs, distinct timestamps
	for i := 0; i < 5; i++ {
		tm := tmA
		if i%2 == 1 {
			tm = tmB
		}
		seedAdminAuditEntry(t, db, "alice@tmi.local", "tmi", tm, i+1)
	}

	svc := NewGormAuditService(db)
	page1, total, _, next, err := svc.ListAuditEntriesAdmin(context.Background(), 2, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	require.Len(t, page1, 2)
	require.NotNil(t, next, "full page must yield a next cursor")

	cur, err := decodeAuditCursor(*next)
	require.NoError(t, err)
	page2, _, _, next2, err := svc.ListAuditEntriesAdmin(context.Background(), 2, cur, nil)
	require.NoError(t, err)
	require.Len(t, page2, 2)
	require.NotNil(t, next2)

	cur2, err := decodeAuditCursor(*next2)
	require.NoError(t, err)
	page3, _, _, next3, err := svc.ListAuditEntriesAdmin(context.Background(), 2, cur2, nil)
	require.NoError(t, err)
	require.Len(t, page3, 1)
	assert.Nil(t, next3, "short page must not yield a next cursor")

	// no duplicates, no gaps across pages
	seen := map[string]bool{}
	for _, p := range [][]AuditEntryResponse{page1, page2, page3} {
		for _, e := range p {
			assert.False(t, seen[e.ID], "duplicate entry %s across pages", e.ID)
			seen[e.ID] = true
		}
	}
	assert.Len(t, seen, 5)
}

func TestListAuditEntriesAdmin_Filters(t *testing.T) {
	db := setupAdminAuditListDB(t)
	tmA, tmB := uuid.New().String(), uuid.New().String()
	seedAdminAuditEntry(t, db, "alice@tmi.local", "tmi", tmA, 10)
	seedAdminAuditEntry(t, db, "bob@tmi.local", "google", tmB, 20)

	svc := NewGormAuditService(db)

	provider := "google"
	rows, total, _, _, err := svc.ListAuditEntriesAdmin(context.Background(), 50, nil,
		&AuditFilters{ActorProvider: &provider})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	require.Len(t, rows, 1)

	rows, total, _, _, err = svc.ListAuditEntriesAdmin(context.Background(), 50, nil,
		&AuditFilters{ThreatModelID: &tmA})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	require.Len(t, rows, 1)
}

func TestListAuditEntriesAdmin_Bidirectional(t *testing.T) {
	db := setupAdminAuditListDB(t)
	svc := NewGormAuditService(db)
	ctx := context.Background()
	tm := uuid.New().String()
	for i := 0; i < 5; i++ {
		seedAdminAuditEntry(t, db, "c@tmi.local", "tmi", tm, (i+1)*10)
	}
	p1, _, prev1, next1, err := svc.ListAuditEntriesAdmin(ctx, 2, nil, nil)
	require.NoError(t, err)
	require.Len(t, p1, 2)
	require.Nil(t, prev1)
	require.NotNil(t, next1)
	c2, _ := decodeAuditCursor(*next1)
	_, _, prev2, _, err := svc.ListAuditEntriesAdmin(ctx, 2, c2, nil)
	require.NoError(t, err)
	require.NotNil(t, prev2)
}

func TestAroundAuditEntriesAdmin(t *testing.T) {
	db := setupAdminAuditListDB(t)
	svc := NewGormAuditService(db)
	ctx := context.Background()
	tm := uuid.New().String()
	var mid string
	for i := 0; i < 7; i++ {
		id := seedAdminAuditEntry(t, db, "c@tmi.local", "tmi", tm, (7-i)*10) // newest..oldest
		if i == 3 {
			mid = id
		}
	}
	page, total, prev, next, err := svc.AroundAuditEntriesAdmin(ctx, 5, mid, nil)
	require.NoError(t, err)
	require.Equal(t, 7, total)
	require.Len(t, page, 5)
	require.Equal(t, mid, page[2].ID)
	require.NotNil(t, prev)
	require.NotNil(t, next)

	_, _, _, _, err = svc.AroundAuditEntriesAdmin(ctx, 5, uuid.New().String(), nil)
	require.ErrorIs(t, err, errAuditAnchorNotFound)
}
