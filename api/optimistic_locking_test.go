// optimistic_locking_test.go — concurrent-update acceptance test for the
// If-Match / Version contract introduced for T14 (#385).
package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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
		ID:                    models.DBVarchar(id),
		Name:                  "Concurrent Lock Test",
		OwnerInternalUUID:     models.DBVarchar(uuid.New().String()),
		CreatedByInternalUUID: models.DBVarchar(uuid.New().String()),
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

// fakeVersionStore is a VersionedStore whose CheckAndBumpVersion always returns
// a fixed error, used to exercise ApplyOptimisticLock's error mapping without a DB.
type fakeVersionStore struct{ err error }

func (f fakeVersionStore) CheckAndBumpVersion(_ context.Context, _ string, _ int) (int, error) {
	return 0, f.err
}

// TestApplyOptimisticLock_NotFoundReturns404 pins the Zero-500 fix (#495 B2):
// when the CAS finds no row, ApplyOptimisticLock must surface a 404 RequestError
// rather than a bare store error that HandleRequestError would turn into a 500.
func TestApplyOptimisticLock_NotFoundReturns404(t *testing.T) {
	c := newGinCtxWithHeader("If-Match", `"5"`)
	_, present, err := ApplyOptimisticLock(c, fakeVersionStore{err: dberrors.ErrNotFound}, uuid.New().String(), nil)
	require.Error(t, err)
	var reqErr *RequestError
	require.True(t, errors.As(err, &reqErr), "expected *RequestError, got %T", err)
	assert.Equal(t, http.StatusNotFound, reqErr.Status)
	assert.False(t, present)
}

// TestApplyOptimisticLock_VersionMismatchReturns409 pins the sibling mapping:
// a version mismatch surfaces as a 409 RequestError.
func TestApplyOptimisticLock_VersionMismatchReturns409(t *testing.T) {
	c := newGinCtxWithHeader("If-Match", `"5"`)
	_, _, err := ApplyOptimisticLock(c, fakeVersionStore{err: ErrVersionMismatch}, uuid.New().String(), nil)
	require.Error(t, err)
	var reqErr *RequestError
	require.True(t, errors.As(err, &reqErr), "expected *RequestError, got %T", err)
	assert.Equal(t, http.StatusConflict, reqErr.Status)
}

// TestCheckAndBumpVersion_VersionMismatch verifies that a stale expected
// version against an existing row returns ErrVersionMismatch (not ErrNotFound).
func TestCheckAndBumpVersion_VersionMismatch(t *testing.T) {
	db, _ := setupThreatModelAliasTestDB(t)
	id := uuid.New().String()
	tm := &models.ThreatModel{
		ID:                    models.DBVarchar(id),
		Name:                  "Stale Version Test",
		OwnerInternalUUID:     models.DBVarchar(uuid.New().String()),
		CreatedByInternalUUID: models.DBVarchar(uuid.New().String()),
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

// TestIsOracleSpuriousNoRowsErr pins the error-string match used by the
// gorm-oracle "WHERE conditions required" workaround (#392). The matcher is
// shared with api/tombstone_store.go's cascade-update path; if the gorm-oracle
// driver ever changes its synthetic message we want the failure to surface in
// unit tests rather than on the next ADB rollout.
func TestIsOracleSpuriousNoRowsErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unrelated", errors.New("connection refused"), false},
		{"exact synthetic message", errors.New("WHERE conditions required"), true},
		{"wrapped synthetic message", fmt.Errorf("ORA-XYZ: %s", "WHERE conditions required"), true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isOracleSpuriousNoRowsErr(tc.err))
		})
	}
}

// TestCheckAndBumpVersion_SwallowsOracleSyntheticError simulates the
// gorm-oracle "WHERE conditions required" pseudo-error on the version-mismatch
// path and confirms CheckAndBumpVersion returns ErrVersionMismatch (or
// ErrNotFound when the row is absent) rather than propagating the synthetic
// driver error to the handler. This is the regression guard for #392 —
// without the swallow in CheckAndBumpVersion, the version-mismatch CAS path
// on Oracle would surface a confusing 500 instead of a clean 409.
func TestCheckAndBumpVersion_SwallowsOracleSyntheticError(t *testing.T) {
	db, _ := setupThreatModelAliasTestDB(t)

	// Seed a row so the existence-probe distinguishes 409 from 404.
	id := uuid.New().String()
	tm := &models.ThreatModel{
		ID:                    models.DBVarchar(id),
		Name:                  "Synthetic Error Test",
		OwnerInternalUUID:     models.DBVarchar(uuid.New().String()),
		CreatedByInternalUUID: models.DBVarchar(uuid.New().String()),
		ThreatModelFramework:  "STRIDE",
		Status:                "not_started",
		Version:               1,
	}
	require.NoError(t, db.Create(tm).Error)

	// Inject the synthetic error on every UPDATE statement, mimicking the
	// gorm-oracle driver's behavior when an UpdateColumn matches zero rows.
	// The callback also forces RowsAffected=0 so the helper sees the same
	// shape it would on Oracle.
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:inject_oracle_synth", func(tx *gorm.DB) {
		tx.Error = errors.New("WHERE conditions required")
		tx.RowsAffected = 0
	}))
	t.Cleanup(func() {
		_ = db.Callback().Update().Remove("test:inject_oracle_synth")
	})

	// Version-mismatch case: row exists, expected version stale → 409.
	_, err := CheckAndBumpVersion(context.Background(), db, "threat_models", id, 99)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrVersionMismatch),
		"synthetic gorm-oracle error must map to ErrVersionMismatch, got: %v", err)

	// Not-found case: row absent → ErrNotFound, not ErrVersionMismatch.
	_, err = CheckAndBumpVersion(context.Background(), db, "threat_models", uuid.New().String(), 1)
	require.Error(t, err)
	assert.True(t, errors.Is(err, dberrors.ErrNotFound),
		"synthetic gorm-oracle error on a missing row must map to ErrNotFound, got: %v", err)
}

// TestGormOracleAddColumnEmitsSingleStatement is the load-bearing guard for
// #391. Adding a NOT NULL DEFAULT 1 column on Oracle 12c+ / 19c ADB is fast
// (metadata-only) IF the dialector emits a single ALTER TABLE statement of
// the form:
//
//	ALTER TABLE <t> ADD (<col> <type> DEFAULT <val> NOT NULL)
//
// The two-statement form (ADD ...; MODIFY ... NOT NULL) re-scans every row
// and takes a TM lock long enough to stall writers on large tables. We
// verified gorm-oracle v1.1.1 emits the single-statement form via source
// inspection; this test pins that shape against the on-disk source so any
// dependency bump that changes it fails CI rather than first surfacing on a
// production rollout.
//
// We read the source from GOMODCACHE because the dependency is not vendored
// (TMI does not commit a vendor/ tree). The test is skipped if the cache is
// unavailable or the source path is missing — a CI environment that has
// just built will have the dep populated by `go mod download`.
func TestGormOracleAddColumnEmitsSingleStatement(t *testing.T) {
	cache := os.Getenv("GOMODCACHE")
	if cache == "" {
		out, err := exec.Command("go", "env", "GOMODCACHE").Output()
		if err != nil {
			t.Skipf("cannot determine GOMODCACHE: %v", err)
		}
		cache = strings.TrimSpace(string(out))
	}
	if cache == "" {
		t.Skip("GOMODCACHE not set")
	}

	// `cache` comes from `go env GOMODCACHE`; the remainder of the path is
	// a fixed dependency identifier. Test-only file read of the dependency
	// source.
	migratorPath := filepath.Clean(filepath.Join(cache, "github.com", "oracle-samples", "gorm-oracle@v1.1.1", "oracle", "migrator.go"))
	src, err := os.ReadFile(migratorPath) // #nosec G304 G703
	if err != nil {
		t.Skipf("gorm-oracle source not available at %s: %v", migratorPath, err)
	}
	body := string(src)

	// AddColumn must emit "ALTER TABLE ? ADD (? ?)" in a single Exec.
	require.Contains(t, body, `"ALTER TABLE ? ADD (? ?)"`,
		"gorm-oracle Migrator.AddColumn no longer emits the single-statement form; #391 metadata-only-default property may have regressed")

	// FullDataTypeOf must concatenate DEFAULT and NOT NULL into one expression
	// rather than emitting them as separate ALTER statements.
	require.Contains(t, body, `expr.SQL += " NOT NULL"`,
		"gorm-oracle FullDataTypeOf no longer appends NOT NULL inline; the migrator may have switched to the two-statement form")
	require.Contains(t, body, `expr.SQL += " " + defaultSQL`,
		"gorm-oracle FullDataTypeOf no longer appends DEFAULT inline; the migrator may have switched to the two-statement form")

	// Sanity: the migrator file must NOT contain "MODIFY ? NOT NULL" inside
	// the AddColumn-adjacent code path. (AlterColumn legitimately uses
	// MODIFY; we only care that AddColumn does not.) Locate the AddColumn
	// function and check that no ALTER ... MODIFY appears before the next
	// top-level "func" declaration.
	addIdx := strings.Index(body, "func (m Migrator) AddColumn(")
	require.NotEqual(t, -1, addIdx, "could not locate Migrator.AddColumn in gorm-oracle source")
	tail := body[addIdx:]
	nextFunc := strings.Index(tail[1:], "\nfunc ")
	if nextFunc > 0 {
		tail = tail[:nextFunc+1]
	}
	require.NotContains(t, tail, "MODIFY",
		"Migrator.AddColumn now references MODIFY; the migrator may have switched to the two-statement form, breaking the metadata-only-default rollout property")
}
