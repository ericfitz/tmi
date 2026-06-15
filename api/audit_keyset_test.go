package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func keysetTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.SystemAuditEntry{}))
	return db
}

// seedKS inserts a row aged ageMinutes in the past and returns its id.
func seedKS(t *testing.T, db *gorm.DB, ageMinutes int) string {
	t.Helper()
	e := models.SystemAuditEntry{
		ID:               models.DBVarchar(uuid.New().String()),
		ActorEmail:       models.DBVarchar("a@b.c"),
		ActorProvider:    models.DBVarchar("tmi"),
		ActorProviderID:  models.DBVarchar("a"),
		ActorDisplayName: models.DBVarchar("A"),
		HTTPMethod:       models.DBVarchar("PUT"),
		HTTPPath:         models.DBText("/admin/x"),
		FieldPath:        models.DBVarchar("f"),
	}
	require.NoError(t, db.Create(&e).Error)
	ts := time.Now().UTC().Add(-time.Duration(ageMinutes) * time.Minute)
	require.NoError(t, db.Exec("UPDATE system_audit_entries SET created_at = ? WHERE id = ?", ts, e.ID).Error)
	return string(e.ID)
}

func sysKeyOf(e models.SystemAuditEntry) (time.Time, string) { return e.CreatedAt, string(e.ID) }

func newSysQuery(db *gorm.DB) func() *gorm.DB {
	return func() *gorm.DB { return db.WithContext(context.Background()).Model(&models.SystemAuditEntry{}) }
}

func TestFetchKeysetPage_ForwardThenBackward(t *testing.T) {
	db := keysetTestDB(t)
	for age := 10; age <= 50; age += 10 {
		seedKS(t, db, age)
	}
	nq := newSysQuery(db)

	page1, prev1, next1, err := fetchKeysetPage(nq, nil, 2, sysKeyOf)
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.Nil(t, prev1, "first page has nothing newer")
	require.NotNil(t, next1)

	c2, err := decodeAuditCursor(*next1)
	require.NoError(t, err)
	page2, prev2, next2, err := fetchKeysetPage(nq, c2, 2, sysKeyOf)
	require.NoError(t, err)
	require.Len(t, page2, 2)
	require.NotNil(t, prev2)
	require.NotNil(t, next2)
	require.NotEqual(t, string(page1[1].ID), string(page2[0].ID))

	cb, err := decodeAuditCursor(*prev2)
	require.NoError(t, err)
	back, _, _, err := fetchKeysetPage(nq, cb, 2, sysKeyOf)
	require.NoError(t, err)
	require.Len(t, back, 2)
	require.Equal(t, string(page1[0].ID), string(back[0].ID))
	require.Equal(t, string(page1[1].ID), string(back[1].ID))
}

func TestFetchKeysetPage_LastPageNextNil(t *testing.T) {
	db := keysetTestDB(t)
	for age := 10; age <= 30; age += 10 {
		seedKS(t, db, age)
	}
	nq := newSysQuery(db)
	page1, _, next1, err := fetchKeysetPage(nq, nil, 2, sysKeyOf)
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.NotNil(t, next1)
	c2, _ := decodeAuditCursor(*next1)
	last, prev2, next2, err := fetchKeysetPage(nq, c2, 2, sysKeyOf)
	require.NoError(t, err)
	require.Len(t, last, 1)
	require.NotNil(t, prev2)
	require.Nil(t, next2, "last page has nothing older")
}

func TestFetchAroundPage_Centers(t *testing.T) {
	db := keysetTestDB(t)
	ids := make([]string, 0, 7)
	for age := 70; age >= 10; age -= 10 {
		ids = append(ids, seedKS(t, db, age))
	}
	anchorID := ids[3]
	nq := newSysQuery(db)
	fetchAnchor := func() (*models.SystemAuditEntry, error) {
		var row models.SystemAuditEntry
		if err := db.Where("id = ?", anchorID).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, err
		}
		return &row, nil
	}
	page, prev, next, err := fetchAroundPage(nq, fetchAnchor, 5, sysKeyOf)
	require.NoError(t, err)
	require.Len(t, page, 5)
	require.Equal(t, anchorID, string(page[2].ID))
	require.NotNil(t, prev)
	require.NotNil(t, next)
	require.True(t, page[0].CreatedAt.After(page[4].CreatedAt))
}

func TestFetchAroundPage_NotFound(t *testing.T) {
	db := keysetTestDB(t)
	nq := newSysQuery(db)
	fetchAnchor := func() (*models.SystemAuditEntry, error) { return nil, nil }
	_, _, _, err := fetchAroundPage(nq, fetchAnchor, 5, sysKeyOf)
	require.ErrorIs(t, err, errAuditAnchorNotFound)
}
