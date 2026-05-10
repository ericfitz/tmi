package api

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func newTestSystemAuditDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Discard,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SystemAuditEntry{}))
	return db
}

func TestSystemAuditRepository_CreateAndRead(t *testing.T) {
	db := newTestSystemAuditDB(t)
	repo := NewSystemAuditRepository(db)
	ctx := context.Background()

	entry := models.SystemAuditEntry{
		ActorEmail:       "alice@example.com",
		ActorProvider:    "google",
		ActorProviderID:  "google-sub-1",
		ActorDisplayName: "Alice",
		HTTPMethod:       "PUT",
		HTTPPath:         "/admin/settings/foo",
		FieldPath:        "system_settings.foo",
		ChangeSummary:    models.NewNullableDBText(strPtr("PUT system_settings.foo")),
	}

	err := repo.Create(ctx, entry)
	require.NoError(t, err)

	rows, err := repo.ListByActor(ctx, "alice@example.com", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "system_settings.foo", rows[0].FieldPath)
	assert.NotEmpty(t, rows[0].ID, "BeforeCreate should have generated UUID")
}

func TestSystemAuditRepository_ListByActor_Filters(t *testing.T) {
	db := newTestSystemAuditDB(t)
	repo := NewSystemAuditRepository(db)
	ctx := context.Background()

	// Insert two rows: one for alice, one for bob.
	require.NoError(t, repo.Create(ctx, models.SystemAuditEntry{
		ActorEmail: "alice@example.com", FieldPath: "x.alice",
		ActorProvider: "google", ActorProviderID: "a", ActorDisplayName: "Alice",
		HTTPMethod: "PUT", HTTPPath: "/admin/x",
	}))
	require.NoError(t, repo.Create(ctx, models.SystemAuditEntry{
		ActorEmail: "bob@example.com", FieldPath: "x.bob",
		ActorProvider: "google", ActorProviderID: "b", ActorDisplayName: "Bob",
		HTTPMethod: "PUT", HTTPPath: "/admin/x",
	}))

	rows, err := repo.ListByActor(ctx, "alice@example.com", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	require.NoError(t, err)
	assert.Len(t, rows, 1)
	assert.Equal(t, "alice@example.com", rows[0].ActorEmail)
}
