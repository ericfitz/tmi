//go:build oracle

package api

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// openWebhookStoreOracleDB opens a direct GORM connection to the Oracle ADB
// backend used by `make test-integration-oci`. Reuses authdb.ParseDatabaseURL +
// authdb.NewGormDB so the oracle-samples/gorm-oracle dialector is configured
// exactly as in production (OracleNamingStrategy uppercasing +
// SkipQuoteIdentifiers). Reads TMI_DATABASE_URL (oracle://…), ORACLE_PASSWORD,
// and the wallet directory from TMI_ORACLE_WALLET_LOCATION (falling back to
// TNS_ADMIN). When TMI_DATABASE_URL is unset the test skips.
func openWebhookStoreOracleDB(t *testing.T) *gorm.DB {
	t.Helper()

	dbURL := os.Getenv("TMI_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TMI_DATABASE_URL not set; run under `make test-integration-oci` with scripts/oci-env.sh sourced")
	}

	cfg, err := authdb.ParseDatabaseURL(dbURL)
	require.NoError(t, err, "parse TMI_DATABASE_URL")
	require.Equal(t, authdb.DatabaseTypeOracle, cfg.Type,
		"this test requires an oracle:// TMI_DATABASE_URL (got %q)", cfg.Type)

	if cfg.OracleWalletLocation == "" {
		if w := os.Getenv("TMI_ORACLE_WALLET_LOCATION"); w != "" {
			cfg.OracleWalletLocation = w
		} else if w := os.Getenv("TNS_ADMIN"); w != "" {
			cfg.OracleWalletLocation = w
		}
	}

	gormDB, err := authdb.NewGormDB(*cfg)
	require.NoError(t, err, "open Oracle ADB connection")
	t.Cleanup(func() { _ = gormDB.Close() })

	// Migrate the tables needed for this test. User must come before
	// WebhookSubscription because of the FK on owner_internal_uuid.
	require.NoError(t, gormDB.AutoMigrate(&models.User{}, &models.WebhookSubscription{}))
	return gormDB.DB()
}

// TestWebhookStore_ListIdle_BoolWhere_OracleIntegration exercises the
// boolean-column WHERE clause on the Oracle SELECT path. It verifies that
// ListIdle returns only the non-pinned subscription when both rows are idle,
// confirming that the models.DBBool(false) bind parameter resolves correctly
// against an Oracle NUMBER(1) column (fixes oracle-db-admin BLOCKING finding
// on #395: raw `operator_pinned = false` is invalid on Oracle 19c).
//
// Run via `make test-integration-oci`.
func TestWebhookStore_ListIdle_BoolWhere_OracleIntegration(t *testing.T) {
	db := openWebhookStoreOracleDB(t)
	ctx := context.Background()

	// Create a synthetic owner user so the FK on owner_internal_uuid is satisfied.
	ownerUUID := uuid.New().String()
	owner := models.User{
		InternalUUID: models.DBVarchar(ownerUUID),
		Provider:     models.DBVarchar("tmi"),
		Email:        models.DBVarchar("oracle-bool-test@tmi.local"),
		Name:         models.DBVarchar("Oracle Bool Test User"),
	}
	require.NoError(t, db.Create(&owner).Error, "create synthetic owner user")
	t.Cleanup(func() {
		_ = db.Where("INTERNAL_UUID = ?", ownerUUID).Delete(&models.User{}).Error
	})

	// Helper to generate a minimal active subscription with a created_at
	// backdated far enough to appear idle for any reasonable daysIdle value.
	const daysIdle = 30
	makeWebhook := func(name string, pinned bool) *models.WebhookSubscription {
		id := uuid.New().String()
		return &models.WebhookSubscription{
			ID:                models.DBVarchar(id),
			OwnerInternalUUID: models.DBVarchar(ownerUUID),
			Name:              models.DBVarchar(name),
			URL:               models.DBText("https://example.com/" + name),
			Events:            models.StringArray{"*"},
			Status:            models.DBVarchar("active"),
			OperatorPinned:    models.DBBool(pinned),
		}
	}

	pinnedSub := makeWebhook("oracle-bool-pinned", true)
	unpinnedSub := makeWebhook("oracle-bool-unpinned", false)

	require.NoError(t, db.Create(pinnedSub).Error, "create pinned subscription")
	require.NoError(t, db.Create(unpinnedSub).Error, "create unpinned subscription")

	// Backdate created_at so both rows appear idle (Oracle uppercases identifiers).
	backdated := time.Now().UTC().AddDate(0, 0, -(daysIdle + 5))
	for _, id := range []string{string(pinnedSub.ID), string(unpinnedSub.ID)} {
		require.NoError(t,
			db.Exec("UPDATE WEBHOOK_SUBSCRIPTIONS SET CREATED_AT = ? WHERE ID = ?", backdated, id).Error,
			"backdate subscription %s", id,
		)
	}

	t.Cleanup(func() {
		for _, id := range []string{string(pinnedSub.ID), string(unpinnedSub.ID)} {
			_ = db.Where("ID = ?", id).Delete(&models.WebhookSubscription{}).Error
		}
	})

	store := NewGormWebhookSubscriptionStore(db)
	results, err := store.ListIdle(ctx, daysIdle)
	require.NoError(t, err, "ListIdle must not error on Oracle with DBBool(false) bind")

	// Filter to only the rows seeded by this test (Oracle ADB is a shared
	// persistent schema; other rows may be present).
	seededIDs := map[string]bool{
		string(pinnedSub.ID):   true,
		string(unpinnedSub.ID): true,
	}
	var seededResults []DBWebhookSubscription
	for _, r := range results {
		if seededIDs[r.Id.String()] {
			seededResults = append(seededResults, r)
		}
	}

	require.Len(t, seededResults, 1, "ListIdle must return exactly 1 of the 2 seeded rows (the non-pinned one)")
	assert.Equal(t, string(unpinnedSub.ID), seededResults[0].Id.String(),
		"the returned row must be the non-pinned subscription")
	assert.False(t, seededResults[0].OperatorPinned,
		"returned row must not be operator-pinned")
}

// TestWebhookStore_TogglePinned_SelectUpdates_OracleIntegration exercises the
// UPDATE path the CATS seeder uses to clear and restore operator_pinned
// (cmd/dbtool/data_api.go setWebhookPinnedViaDB, #742).
//
// Two things are under test that the SELECT-side test above does not cover:
// that Select("OperatorPinned") resolves to the uppercase Oracle column, and
// that Updates() with a zero-valued (false) struct field actually writes rather
// than being skipped as a zero value. A silent no-op here would leave the CATS
// anchor subscription permanently pinned and un-triggerable — the failure #742
// was filed for — or, on the restore leg, permanently unpinned and mortal.
//
// Run via `make test-integration-oci`.
func TestWebhookStore_TogglePinned_SelectUpdates_OracleIntegration(t *testing.T) {
	db := openWebhookStoreOracleDB(t)

	ownerUUID := uuid.New().String()
	owner := models.User{
		InternalUUID: models.DBVarchar(ownerUUID),
		Provider:     models.DBVarchar("tmi"),
		Email:        models.DBVarchar("oracle-pin-toggle-test@tmi.local"),
		Name:         models.DBVarchar("Oracle Pin Toggle Test User"),
	}
	require.NoError(t, db.Create(&owner).Error, "create synthetic owner user")
	t.Cleanup(func() {
		_ = db.Where("INTERNAL_UUID = ?", ownerUUID).Delete(&models.User{}).Error
	})

	subID := uuid.New().String()
	sub := &models.WebhookSubscription{
		ID:                models.DBVarchar(subID),
		OwnerInternalUUID: models.DBVarchar(ownerUUID),
		Name:              models.DBVarchar("oracle-pin-toggle"),
		URL:               models.DBText("https://example.com/oracle-pin-toggle"),
		Events:            models.StringArray{"*"},
		Status:            models.DBVarchar("active"),
		OperatorPinned:    models.DBBool(true),
	}
	require.NoError(t, db.Create(sub).Error, "create pinned subscription")
	t.Cleanup(func() {
		_ = db.Where("ID = ?", subID).Delete(&models.WebhookSubscription{}).Error
	})

	// setPinned mirrors the seeder's statement exactly.
	setPinned := func(pinned bool) {
		t.Helper()
		result := db.
			Model(&models.WebhookSubscription{}).
			Where(&models.WebhookSubscription{ID: models.DBVarchar(subID)}).
			Select("OperatorPinned").
			Updates(&models.WebhookSubscription{OperatorPinned: models.DBBool(pinned)})
		require.NoError(t, result.Error, "set operator_pinned=%t on Oracle", pinned)
		require.EqualValues(t, 1, result.RowsAffected,
			"set operator_pinned=%t must affect exactly 1 row", pinned)
	}

	readPinned := func() bool {
		t.Helper()
		var got models.WebhookSubscription
		require.NoError(t, db.First(&got, "id = ?", subID).Error, "read back subscription")
		return bool(got.OperatorPinned)
	}

	// Clearing the flag is the leg that a zero-value skip would silently break.
	setPinned(false)
	assert.False(t, readPinned(), "operator_pinned must be false after clearing it")

	setPinned(true)
	assert.True(t, readPinned(), "operator_pinned must be true after restoring it")
}
