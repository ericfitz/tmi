//go:build dev || test || integration

package api

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// openSortPaginationIntegrationDB opens the real PostgreSQL integration
// database, skipping when TEST_DB_* is unset (see alias_sequence_integration_test.go
// for the same pattern). PostgreSQL, unlike SQLite, does not guarantee any
// stable order among rows tied on a sort key across separate queries, which
// is exactly the property this test needs a real backend to exercise.
func openSortPaginationIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()

	host := os.Getenv("TEST_DB_HOST")
	port := os.Getenv("TEST_DB_PORT")
	user := os.Getenv("TEST_DB_USER")
	password := os.Getenv("TEST_DB_PASSWORD")
	dbname := os.Getenv("TEST_DB_NAME")
	if host == "" || port == "" || user == "" || dbname == "" {
		t.Skip("TEST_DB_* not set; sort pagination stability test requires PostgreSQL")
	}

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=UTC",
		host, port, user, password, dbname,
	)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err, "open PostgreSQL integration DB")
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.ThreatModelAccess{}, &models.ThreatModel{}, &models.Threat{}, &models.AliasCounter{}))
	return db
}

// TestSortPaginationStability_Integration is the behavioral regression guard
// for the #645 ORDER BY tiebreaker: it reproduces TestSortPaginationStability
// (api/threat_store_semantic_sort_e2e_test.go) against PostgreSQL instead of
// SQLite. SQLite happens to break ties on rowid deterministically, so that
// test passes even without a tiebreaker; PostgreSQL does not make that
// promise, so a regression here (e.g. reverting buildOrderBy to drop the
// ", id ASC" suffix) can surface as duplicate or missing rows across
// LIMIT/OFFSET pages.
// SEM@78155d54: verify LIMIT/OFFSET pagination never drops or duplicates PostgreSQL rows tied on the sort key (reads DB)
func TestSortPaginationStability_Integration(t *testing.T) {
	ctx := context.Background()
	db := openSortPaginationIntegrationDB(t)

	// A real owning user and threat model are required: threats.threat_model_id
	// and threat_models.owner_internal_uuid are enforced foreign keys on
	// PostgreSQL (unlike the SQLite unit test, which never creates a backing
	// ThreatModel/User row because SQLite does not enforce the constraint).
	providerID := "sort-pagination-" + uuid.New().String()[:8]
	owner := &models.User{
		InternalUUID:   models.DBVarchar(uuid.New().String()),
		Provider:       "test",
		ProviderUserID: models.NewNullableDBVarchar(&providerID),
		Email:          models.DBVarchar(providerID + "@example.com"),
		Name:           models.DBVarchar("Sort Pagination Owner"),
	}
	require.NoError(t, db.Create(owner).Error)
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM users WHERE internal_uuid = ?", string(owner.InternalUUID)).Error
	})

	tmStore := NewGormThreatModelStore(db)
	emptyAuth := []Authorization{}
	idSetter := func(item ThreatModel, id string) ThreatModel {
		uid, _ := uuid.Parse(id)
		item.Id = &uid
		return item
	}
	tm, err := tmStore.Create(ThreatModel{
		Name:          "Sort pagination stability (integration)",
		Owner:         User{PrincipalType: UserPrincipalTypeUser, Provider: "test", ProviderId: providerID},
		CreatedBy:     &User{PrincipalType: UserPrincipalTypeUser, Provider: "test", ProviderId: providerID},
		Authorization: &emptyAuth,
	}, idSetter)
	require.NoError(t, err)
	require.NotNil(t, tm.Id)
	tmID := tm.Id.String()
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM threat_models WHERE id = ?", tmID).Error
	})

	store := &GormThreatRepository{db: db}
	const total = 25
	allIDs := make(map[string]bool, total)
	createdIDs := make([]string, 0, total)
	t.Cleanup(func() {
		for _, id := range createdIDs {
			_ = db.Exec("DELETE FROM threats WHERE id = ?", id).Error
		}
	})

	for i := 0; i < total; i++ {
		tid := uuid.New()
		desc := "desc"
		severity := "high"
		th := &Threat{
			Id:            &tid,
			ThreatModelId: tm.Id,
			Name:          fmt.Sprintf("pg-tiebreak-%d", i),
			Description:   &desc,
			ThreatType:    []string{"test"},
			Severity:      &severity,
		}
		require.NoError(t, store.Create(ctx, th))
		allIDs[tid.String()] = true
		createdIDs = append(createdIDs, tid.String())
	}

	sort := "severity:asc"
	seen := make(map[string]bool, total)
	var duplicates []string
	for offset := 0; offset < total; offset += 7 {
		filter := ThreatFilter{Sort: &sort, Offset: offset, Limit: 7}
		results, _, err := store.List(ctx, tmID, filter)
		require.NoError(t, err)
		for _, r := range results {
			id := r.Id.String()
			if seen[id] {
				duplicates = append(duplicates, id)
			}
			seen[id] = true
		}
	}

	assert.Empty(t, duplicates, "no threat ID should appear across more than one page")
	assert.Equal(t, allIDs, seen, "union of all pages should equal the full inserted set")
}
