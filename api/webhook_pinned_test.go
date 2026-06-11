package api

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gormlogger "gorm.io/gorm/logger"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupWebhookPinnedTestDB creates an in-memory SQLite DB for webhook pinned tests.
// FK constraints are disabled because WebhookSubscription has User and ThreatModel FKs
// that we don't need to satisfy for the pinned-flag test.
func setupWebhookPinnedTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.WebhookSubscription{}))
	return db
}

// TestWebhookSubscription_OperatorPinnedRoundTrip verifies that the
// OperatorPinned flag persists through the GORM store create → get cycle.
func TestWebhookSubscription_OperatorPinnedRoundTrip(t *testing.T) {
	db := setupWebhookPinnedTestDB(t)
	store := NewGormWebhookSubscriptionStore(db)
	ctx := context.Background()

	ownerID := uuid.New()
	sub := DBWebhookSubscription{
		OwnerId:        ownerID,
		Name:           "test-pinned-sub",
		Url:            "https://alerts.example.com/hook",
		Events:         []string{"system_audit.admin_write"},
		Status:         "active",
		CreatedAt:      time.Now().UTC(),
		ModifiedAt:     time.Now().UTC(),
		OperatorPinned: true,
	}

	idSetter := func(s DBWebhookSubscription, id string) DBWebhookSubscription {
		s.Id = uuid.MustParse(id)
		return s
	}

	created, err := store.Create(ctx, sub, idSetter)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, created.Id)

	got, err := store.Get(ctx, created.Id.String())
	require.NoError(t, err)
	assert.True(t, got.OperatorPinned, "OperatorPinned flag must round-trip through the GORM store")
}
