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

func setupSystemAuditPruneDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SystemAuditEntry{}))
	return db
}

func seedSystemAuditEntry(t *testing.T, db *gorm.DB, ageDays int) string {
	t.Helper()
	entry := models.SystemAuditEntry{
		ID:               models.DBVarchar(uuid.New().String()),
		ActorEmail:       models.DBVarchar("charlie@tmi.local"),
		ActorProvider:    models.DBVarchar("tmi"),
		ActorProviderID:  models.DBVarchar("charlie"),
		ActorDisplayName: models.DBVarchar("Charlie"),
		HTTPMethod:       models.DBVarchar("PUT"),
		HTTPPath:         models.DBText("/admin/settings/test"),
		FieldPath:        models.DBVarchar("test"),
	}
	require.NoError(t, db.Create(&entry).Error)
	backdated := time.Now().UTC().AddDate(0, 0, -ageDays)
	require.NoError(t, db.Exec("UPDATE system_audit_entries SET created_at = ? WHERE id = ?", backdated, entry.ID).Error)
	return string(entry.ID)
}

func TestPruneSystemAuditEntries(t *testing.T) {
	db := setupSystemAuditPruneDB(t)
	t.Setenv("SYSTEM_AUDIT_RETENTION_DAYS", "100")

	oldID := seedSystemAuditEntry(t, db, 150)  // past retention
	youngID := seedSystemAuditEntry(t, db, 50) // within retention

	svc := NewGormAuditService(db)
	pruned, err := svc.PruneSystemAuditEntries(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, pruned)

	var count int64
	require.NoError(t, db.Model(&models.SystemAuditEntry{}).Where("id = ?", oldID).Count(&count).Error)
	assert.Equal(t, int64(0), count, "aged entry should be pruned")
	require.NoError(t, db.Model(&models.SystemAuditEntry{}).Where("id = ?", youngID).Count(&count).Error)
	assert.Equal(t, int64(1), count, "young entry must survive")
}

func TestPruneSystemAuditEntries_NothingToPrune(t *testing.T) {
	db := setupSystemAuditPruneDB(t)
	t.Setenv("SYSTEM_AUDIT_RETENTION_DAYS", "100")
	seedSystemAuditEntry(t, db, 10)

	svc := NewGormAuditService(db)
	pruned, err := svc.PruneSystemAuditEntries(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, pruned)
}
