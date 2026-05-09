// optimistic_locking_test.go — concurrent-update acceptance test for the
// If-Match / Version contract introduced for T14 (#385).
package api

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newGinCtxWithHeader builds a minimal gin.Context carrying a single header
// for ParseIfMatchHeader unit tests.
func newGinCtxWithHeader(name, value string) *gin.Context {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	if value != "" {
		c.Request.Header.Set(name, value)
	}
	return c
}

// TestCheckAndBumpVersion_Concurrent verifies that two simultaneous CAS-style
// version bumps with the same expected value never both succeed: exactly one
// returns the new version, the other returns ErrVersionMismatch. This is the
// acceptance criterion called out in the issue: "concurrent-update test in
// api/threat_model_handlers_test.go ... two goroutines PUT, exactly one 200 +
// one 409."
//
// We exercise the helper directly against an in-memory SQLite DB rather than
// going through the full HTTP stack, because the handler-layer wrapper is a
// thin shell over CheckAndBumpVersion (parse If-Match, call helper, map error)
// and SQLite's serialized writer captures the same contention shape that
// PostgreSQL/Oracle expose for a single-row UPDATE WHERE id=? AND version=?.
func TestCheckAndBumpVersion_Concurrent(t *testing.T) {
	db, _ := setupThreatModelAliasTestDB(t)

	// Pin the connection pool to a single connection so all goroutines see the
	// same in-memory SQLite database. The default pool would spawn fresh
	// connections, each with its own empty :memory: schema.
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	// Seed a threat model row with version=1.
	id := uuid.New().String()
	tm := &models.ThreatModel{
		ID:                    id,
		Name:                  "Concurrent Lock Test",
		OwnerInternalUUID:     uuid.New().String(),
		CreatedByInternalUUID: uuid.New().String(),
		ThreatModelFramework:  "STRIDE",
		Status:                "not_started",
		Version:               1,
	}
	require.NoError(t, db.Create(tm).Error)

	const goroutines = 8
	var wg sync.WaitGroup
	wg.Add(goroutines)

	results := make([]error, goroutines)
	versions := make([]int, goroutines)

	// Barrier so all goroutines start the CAS at once.
	start := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start
			v, err := CheckAndBumpVersion(context.Background(), db, "threat_models", id, 1)
			results[idx] = err
			versions[idx] = v
		}(i)
	}
	close(start)
	wg.Wait()

	// Exactly one goroutine should win.
	successCount := 0
	mismatchCount := 0
	for i, err := range results {
		switch {
		case err == nil:
			successCount++
			assert.Equal(t, 2, versions[i], "winner must observe new version=2")
		case errors.Is(err, ErrVersionMismatch):
			mismatchCount++
		default:
			t.Fatalf("unexpected error from goroutine %d: %v", i, err)
		}
	}
	assert.Equal(t, 1, successCount, "exactly one CAS must succeed")
	assert.Equal(t, goroutines-1, mismatchCount, "all other CAS attempts must report version mismatch")

	// Final row state: version=2.
	var fresh models.ThreatModel
	require.NoError(t, db.First(&fresh, "id = ?", id).Error)
	assert.Equal(t, 2, fresh.Version)
}

// TestCheckAndBumpVersion_NotFound verifies the helper distinguishes a missing
// row (ErrNotFound) from a version mismatch (ErrVersionMismatch).
func TestCheckAndBumpVersion_NotFound(t *testing.T) {
	db, _ := setupThreatModelAliasTestDB(t)
	_, err := CheckAndBumpVersion(context.Background(), db, "threat_models", uuid.New().String(), 1)
	require.Error(t, err)
	assert.True(t, errors.Is(err, dberrors.ErrNotFound))
	assert.False(t, errors.Is(err, ErrVersionMismatch))
}

// TestCheckAndBumpVersion_VersionMismatch verifies that a stale expected
// version against an existing row returns ErrVersionMismatch (not ErrNotFound).
func TestCheckAndBumpVersion_VersionMismatch(t *testing.T) {
	db, _ := setupThreatModelAliasTestDB(t)
	id := uuid.New().String()
	tm := &models.ThreatModel{
		ID:                    id,
		Name:                  "Stale Version Test",
		OwnerInternalUUID:     uuid.New().String(),
		CreatedByInternalUUID: uuid.New().String(),
		ThreatModelFramework:  "STRIDE",
		Status:                "not_started",
		Version:               5,
	}
	require.NoError(t, db.Create(tm).Error)

	_, err := CheckAndBumpVersion(context.Background(), db, "threat_models", id, 1) // expected=1, actual=5
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrVersionMismatch))
	assert.False(t, errors.Is(err, dberrors.ErrNotFound))

	// Row version unchanged.
	var fresh models.ThreatModel
	require.NoError(t, db.First(&fresh, "id = ?", id).Error)
	assert.Equal(t, 5, fresh.Version)
}

// TestParseIfMatchHeader covers the header parsing surface: missing, bare
// integer, quoted ETag form, weak prefix, wildcard rejection, malformed.
func TestParseIfMatchHeader_Variants(t *testing.T) {
	cases := []struct {
		name      string
		header    string
		want      int
		present   bool
		expectErr bool
	}{
		{"absent", "", 0, false, false},
		{"bare integer", "5", 5, true, false},
		{"quoted etag", `"7"`, 7, true, false},
		{"weak prefix", `W/"3"`, 3, true, false},
		{"wildcard rejected", "*", 0, true, true},
		{"malformed", "not-a-number", 0, true, true},
		{"negative", "-1", 0, true, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			c := newGinCtxWithHeader("If-Match", tc.header)
			got, present, err := ParseIfMatchHeader(c)
			assert.Equal(t, tc.present, present)
			if tc.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.want, got)
			}
		})
	}
}
