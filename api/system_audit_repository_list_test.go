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

func setupSysAuditListDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SystemAuditEntry{}))
	return db
}

func seedSysAuditRow(t *testing.T, db *gorm.DB, actor, provider, method, path, field string, ageMinutes int) string {
	t.Helper()
	e := models.SystemAuditEntry{
		ID:               models.DBVarchar(uuid.New().String()),
		ActorEmail:       models.DBVarchar(actor),
		ActorProvider:    models.DBVarchar(provider),
		ActorProviderID:  models.DBVarchar(actor),
		ActorDisplayName: models.DBVarchar("Test"),
		HTTPMethod:       models.DBVarchar(method),
		HTTPPath:         models.DBText(path),
		FieldPath:        models.DBVarchar(field),
	}
	require.NoError(t, db.Create(&e).Error)
	ts := time.Now().UTC().Add(-time.Duration(ageMinutes) * time.Minute)
	require.NoError(t, db.Exec("UPDATE system_audit_entries SET created_at = ? WHERE id = ?", ts, e.ID).Error)
	return string(e.ID)
}

func TestSystemAuditList_FiltersAndCursor(t *testing.T) {
	db := setupSysAuditListDB(t)
	repo := NewSystemAuditRepository(db)
	ctx := context.Background()

	seedSysAuditRow(t, db, "charlie@tmi.local", "tmi", "PUT", "/admin/settings/a", "a", 10)
	seedSysAuditRow(t, db, "charlie@tmi.local", "tmi", "DELETE", "/admin/settings/b", "b", 20)
	seedSysAuditRow(t, db, "dave@tmi.local", "google", "PUT", "/admin/quotas/users/x", "quota", 30)

	method := "PUT"
	rows, total, _, _, err := repo.List(ctx, SystemAuditFilter{HTTPMethod: &method, Limit: 50})
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, rows, 2)

	prefix := "/admin/settings"
	rows, total, _, _, err = repo.List(ctx, SystemAuditFilter{PathPrefix: &prefix, Limit: 50})
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, rows, 2)

	// LIKE metacharacters in the prefix must be treated literally
	weird := "/admin/100%_done"
	_, total, _, _, err = repo.List(ctx, SystemAuditFilter{PathPrefix: &weird, Limit: 50})
	require.NoError(t, err)
	assert.Equal(t, 0, total)

	// cursor iteration: page size 2 over 3 rows
	page1, total, _, next, err := repo.List(ctx, SystemAuditFilter{Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	require.Len(t, page1, 2)
	require.NotNil(t, next)
	cur, err := decodeAuditCursor(*next)
	require.NoError(t, err)
	page2, _, _, next2, err := repo.List(ctx, SystemAuditFilter{Limit: 2, Cursor: cur})
	require.NoError(t, err)
	require.Len(t, page2, 1)
	assert.Nil(t, next2)
}

func TestSystemAuditGetByID(t *testing.T) {
	db := setupSysAuditListDB(t)
	repo := NewSystemAuditRepository(db)
	ctx := context.Background()

	id := seedSysAuditRow(t, db, "charlie@tmi.local", "tmi", "PUT", "/admin/settings/a", "a", 1)

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "charlie@tmi.local", string(got.ActorEmail))

	got, err = repo.GetByID(ctx, uuid.New().String())
	require.NoError(t, err, "unknown id is not an error")
	assert.Nil(t, got, "unknown id returns nil entry")
}

func TestSystemAuditList_Bidirectional(t *testing.T) {
	db := setupSysAuditListDB(t)
	repo := NewSystemAuditRepository(db)
	ctx := context.Background()
	for age := 10; age <= 50; age += 10 {
		seedSysAuditRow(t, db, "c@tmi.local", "tmi", "PUT", "/admin/x", "f", age)
	}
	p1, _, prev1, next1, err := repo.List(ctx, SystemAuditFilter{Limit: 2})
	require.NoError(t, err)
	require.Len(t, p1, 2)
	require.Nil(t, prev1)
	require.NotNil(t, next1)

	c2, _ := decodeAuditCursor(*next1)
	_, _, prev2, _, err := repo.List(ctx, SystemAuditFilter{Limit: 2, Cursor: c2})
	require.NoError(t, err)
	require.NotNil(t, prev2)

	cb, _ := decodeAuditCursor(*prev2)
	back, _, _, _, err := repo.List(ctx, SystemAuditFilter{Limit: 2, Cursor: cb})
	require.NoError(t, err)
	require.Equal(t, string(p1[0].ID), string(back[0].ID))
}

func TestSystemAuditList_Around(t *testing.T) {
	db := setupSysAuditListDB(t)
	repo := NewSystemAuditRepository(db)
	ctx := context.Background()
	var mid string
	for age := 70; age >= 10; age -= 10 {
		id := seedSysAuditRow(t, db, "c@tmi.local", "tmi", "PUT", "/admin/x", "f", age)
		if age == 40 {
			mid = id
		}
	}
	page, total, prev, next, err := repo.Around(ctx, SystemAuditFilter{Limit: 5}, mid)
	require.NoError(t, err)
	require.Equal(t, 7, total)
	require.Len(t, page, 5)
	require.Equal(t, mid, string(page[2].ID))
	require.NotNil(t, prev)
	require.NotNil(t, next)

	_, _, _, _, err = repo.Around(ctx, SystemAuditFilter{Limit: 5}, uuid.New().String())
	require.ErrorIs(t, err, errAuditAnchorNotFound)
}

// TestSystemAuditList_AroundBackfill covers the simpler one-sided case: anchor
// is the NEWEST row, so the newer side is empty, olderWant absorbs the full
// budget, and the page fills entirely from older rows. The backfill re-fetch
// branch in fetchAroundPage is NOT triggered here because the older side
// already satisfies the limit on its own. See TestSystemAuditList_AroundBackfillNewer
// for the branch that does trigger the backfill re-fetch.
func TestSystemAuditList_AroundBackfill(t *testing.T) {
	db := setupSysAuditListDB(t)
	repo := NewSystemAuditRepository(db)
	ctx := context.Background()
	var newest string
	for age := 60; age >= 10; age -= 10 {
		id := seedSysAuditRow(t, db, "c@tmi.local", "tmi", "PUT", "/admin/x", "f", age)
		if age == 10 {
			newest = id // smallest age == newest
		}
	}
	page, _, prev, next, err := repo.Around(ctx, SystemAuditFilter{Limit: 5}, newest)
	require.NoError(t, err)
	require.Len(t, page, 5)                      // full page despite no newer rows
	require.Equal(t, newest, string(page[0].ID)) // anchor at the top (newest)
	require.Nil(t, prev)                         // nothing newer than the newest row
	require.NotNil(t, next)
}

// TestSystemAuditList_AroundBackfillNewer exercises the backfill re-fetch branch
// in fetchAroundPage (audit_keyset.go ~lines 98-105): the anchor is the OLDEST
// row, so the older side is empty and total=newer+0+1 < limit, causing the
// code to re-fetch the newer side with a larger limit to fill the page.
//
// Math with limit=5, 6 rows (ages 10..60), anchor=oldest (age 60):
//
//	newerWant  = (5-1)/2 = 2   → newer = [age50, age40]  (2 rows)
//	olderWant  = 5-1-2   = 2   → older = []              (0 rows, none exist)
//	total      = 2+0+1   = 3 < 5  → backfill triggers
//	newerWant2 = 5-1-0   = 4   → newer re-fetched = [age50, age40, age30, age20]
//	page       = [age20, age30, age40, age50, anchor(age60)]
//	anchor sits at page[4] (bottom); prev is not nil; next is nil.
func TestSystemAuditList_AroundBackfillNewer(t *testing.T) {
	db := setupSysAuditListDB(t)
	repo := NewSystemAuditRepository(db)
	ctx := context.Background()
	var oldest string
	for age := 10; age <= 60; age += 10 { // 6 rows, age 60 = oldest
		id := seedSysAuditRow(t, db, "c@tmi.local", "tmi", "PUT", "/admin/x", "f", age)
		if age == 60 {
			oldest = id
		}
	}
	page, _, prev, next, err := repo.Around(ctx, SystemAuditFilter{Limit: 5}, oldest)
	require.NoError(t, err)
	require.Len(t, page, 5)                                // full page despite no older rows
	require.Equal(t, oldest, string(page[len(page)-1].ID)) // anchor at the bottom (oldest)
	require.NotNil(t, prev)                                // newer rows exist above the page
	require.Nil(t, next)                                   // nothing older than the oldest row
}

func TestSystemAuditStreamFiltered_Batches(t *testing.T) {
	db := setupSysAuditListDB(t)
	repo := NewSystemAuditRepository(db)
	ctx := context.Background()
	for age := 1; age <= 5; age++ {
		seedSysAuditRow(t, db, "c@tmi.local", "tmi", "PUT", "/admin/x", "f", age)
	}
	var seen, batches int
	err := repo.StreamFiltered(ctx, SystemAuditFilter{}, 2, func(rows []models.SystemAuditEntry) error {
		batches++
		seen += len(rows)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 5, seen)
	require.Equal(t, 3, batches) // 2 + 2 + 1
}
