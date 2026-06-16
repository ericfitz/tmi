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

func setupTMAuditKeysetDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.AuditEntry{}))
	return db
}

// seedTMAuditEntry inserts an audit entry for the given threat model with a
// deterministic created_at offset (older ageMinutes => older row).
func seedTMAuditEntry(t *testing.T, db *gorm.DB, tmID, objectType, actorEmail string, ageMinutes int) string {
	t.Helper()
	v := 1
	e := models.AuditEntry{
		ThreatModelID: models.DBVarchar(tmID),
		ObjectType:    models.DBVarchar(objectType),
		ObjectID:      models.DBVarchar(uuid.New().String()),
		Version:       &v,
		ChangeType:    models.DBVarchar("created"),
		ActorEmail:    models.DBVarchar(actorEmail),
		ActorProvider: models.DBVarchar("tmi"),
	}
	require.NoError(t, db.Create(&e).Error)
	ts := time.Now().UTC().Add(-time.Duration(ageMinutes) * time.Minute)
	require.NoError(t, db.Exec("UPDATE audit_entries SET created_at = ? WHERE id = ?", ts, e.ID).Error)
	return string(e.ID)
}

func TestGetThreatModelAuditTrailKeyset_CursorIteration(t *testing.T) {
	db := setupTMAuditKeysetDB(t)
	tm := uuid.New().String()
	other := uuid.New().String()
	// 5 entries for tm, plus 2 for an unrelated TM that must never appear.
	for i := 0; i < 5; i++ {
		seedTMAuditEntry(t, db, tm, "threat_model", "alice@tmi.local", i+1)
	}
	seedTMAuditEntry(t, db, other, "threat_model", "bob@tmi.local", 1)
	seedTMAuditEntry(t, db, other, "threat_model", "bob@tmi.local", 2)

	svc := NewGormAuditService(db)
	ctx := context.Background()

	page1, total, prev1, next1, err := svc.GetThreatModelAuditTrailKeyset(ctx, tm, 2, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 5, total, "total must be scoped to the threat model and ignore the cursor")
	require.Len(t, page1, 2)
	require.Nil(t, prev1, "first page must not yield a prev cursor")
	require.NotNil(t, next1, "full first page must yield a next cursor")

	cur1, err := decodeAuditCursor(*next1)
	require.NoError(t, err)
	page2, _, _, next2, err := svc.GetThreatModelAuditTrailKeyset(ctx, tm, 2, cur1, nil)
	require.NoError(t, err)
	require.Len(t, page2, 2)
	require.NotNil(t, next2)

	cur2, err := decodeAuditCursor(*next2)
	require.NoError(t, err)
	page3, _, _, next3, err := svc.GetThreatModelAuditTrailKeyset(ctx, tm, 2, cur2, nil)
	require.NoError(t, err)
	require.Len(t, page3, 1, "last page holds the remaining row")
	assert.Nil(t, next3, "short last page must not yield a next cursor")

	// No overlap, no gaps, and only rows for the scoped TM.
	seen := map[string]bool{}
	for _, p := range [][]AuditEntryResponse{page1, page2, page3} {
		for _, e := range p {
			assert.False(t, seen[e.ID], "duplicate entry %s across pages", e.ID)
			seen[e.ID] = true
			assert.Equal(t, tm, e.ThreatModelID, "entry must belong to scoped threat model")
		}
	}
	assert.Len(t, seen, 5)
}

// TestGetThreatModelAuditTrailKeyset_BackwardCursor verifies that the prev_cursor
// emitted on a forward page can be used to navigate backward, and that backward
// traversal returns exactly the prior page's entries (correct order) while staying
// scoped to the threat model — it must never leak rows belonging to a different TM.
func TestGetThreatModelAuditTrailKeyset_BackwardCursor(t *testing.T) {
	db := setupTMAuditKeysetDB(t)
	tm := uuid.New().String()
	other := uuid.New().String()

	// 5 entries for the scoped TM (ages 1..5; smaller age == newer).
	for i := 0; i < 5; i++ {
		seedTMAuditEntry(t, db, tm, "threat_model", "alice@tmi.local", i+1)
	}
	// Negative controls: rows for an unrelated TM that must never appear, including
	// rows whose timestamps interleave with the scoped TM's window.
	seedTMAuditEntry(t, db, other, "threat_model", "bob@tmi.local", 1)
	seedTMAuditEntry(t, db, other, "threat_model", "bob@tmi.local", 3)
	seedTMAuditEntry(t, db, other, "threat_model", "bob@tmi.local", 5)

	svc := NewGormAuditService(db)
	ctx := context.Background()

	// Page forward to page 1, then page 2 to obtain a prev cursor.
	page1, _, prev1, next1, err := svc.GetThreatModelAuditTrailKeyset(ctx, tm, 2, nil, nil)
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.Nil(t, prev1, "first page has nothing newer")
	require.NotNil(t, next1)

	cur1, err := decodeAuditCursor(*next1)
	require.NoError(t, err)
	page2, _, prev2, _, err := svc.GetThreatModelAuditTrailKeyset(ctx, tm, 2, cur1, nil)
	require.NoError(t, err)
	require.Len(t, page2, 2)
	require.NotNil(t, prev2, "page 2 must yield a prev cursor pointing back to page 1")

	// Navigate backward using page 2's prev cursor: must reproduce page 1 exactly.
	curBack, err := decodeAuditCursor(*prev2)
	require.NoError(t, err)
	back, _, _, _, err := svc.GetThreatModelAuditTrailKeyset(ctx, tm, 2, curBack, nil)
	require.NoError(t, err)
	require.Len(t, back, 2)
	assert.Equal(t, page1[0].ID, back[0].ID, "backward page must match page 1 order")
	assert.Equal(t, page1[1].ID, back[1].ID, "backward page must match page 1 order")

	// Backward traversal must remain scoped to the threat model.
	for _, e := range back {
		assert.Equal(t, tm, e.ThreatModelID, "backward traversal must not leak another TM's rows")
	}
}

func TestGetThreatModelAuditTrailKeyset_Filters(t *testing.T) {
	db := setupTMAuditKeysetDB(t)
	tm := uuid.New().String()
	seedTMAuditEntry(t, db, tm, "threat_model", "alice@tmi.local", 10)
	seedTMAuditEntry(t, db, tm, "diagram", "alice@tmi.local", 20)
	seedTMAuditEntry(t, db, tm, "diagram", "alice@tmi.local", 30)

	svc := NewGormAuditService(db)
	ctx := context.Background()

	objectType := "diagram"
	rows, total, _, _, err := svc.GetThreatModelAuditTrailKeyset(ctx, tm, 50, nil,
		&AuditFilters{ObjectType: &objectType})
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	require.Len(t, rows, 2)
	for _, r := range rows {
		assert.Equal(t, "diagram", r.ObjectType)
	}
}

func TestGetThreatModelAuditTrailKeyset_EmptyResult(t *testing.T) {
	db := setupTMAuditKeysetDB(t)
	svc := NewGormAuditService(db)
	rows, total, prev, next, err := svc.GetThreatModelAuditTrailKeyset(context.Background(), uuid.New().String(), 50, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.Empty(t, rows)
	assert.Nil(t, prev)
	assert.Nil(t, next)
}
