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
	rows, total, _, err := repo.List(ctx, SystemAuditFilter{HTTPMethod: &method, Limit: 50})
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, rows, 2)

	prefix := "/admin/settings"
	rows, total, _, err = repo.List(ctx, SystemAuditFilter{PathPrefix: &prefix, Limit: 50})
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, rows, 2)

	// LIKE metacharacters in the prefix must be treated literally
	weird := "/admin/100%_done"
	_, total, _, err = repo.List(ctx, SystemAuditFilter{PathPrefix: &weird, Limit: 50})
	require.NoError(t, err)
	assert.Equal(t, 0, total)

	// cursor iteration: page size 2 over 3 rows
	page1, total, next, err := repo.List(ctx, SystemAuditFilter{Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	require.Len(t, page1, 2)
	require.NotNil(t, next)
	cur, err := decodeAuditCursor(*next)
	require.NoError(t, err)
	page2, _, next2, err := repo.List(ctx, SystemAuditFilter{Limit: 2, Cursor: cur})
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
