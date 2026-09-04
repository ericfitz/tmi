// optimistic_locking_test.go — concurrent-update acceptance test for the
// If-Match / Version contract introduced for T14 (#385).
package api

import (
	"context"
	"database/sql"
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

// TestMapOptimisticLockError_NotFoundReturns404 pins the Zero-500 fix (#495 B2):
// when the CAS finds no row, MapOptimisticLockError must surface a 404
// RequestError rather than a bare store error that HandleRequestError would
// turn into a 500. This is the same contract ApplyOptimisticLock used to
// enforce; it now lives on the store-error side of the split (#594).
func TestMapOptimisticLockError_NotFoundReturns404(t *testing.T) {
	err := MapOptimisticLockError(dberrors.ErrNotFound, "Project not found")
	require.Error(t, err)
	var reqErr *RequestError
	require.True(t, errors.As(err, &reqErr), "expected *RequestError, got %T", err)
	assert.Equal(t, http.StatusNotFound, reqErr.Status)
}

// TestMapOptimisticLockError_VersionMismatchReturns409 pins the sibling
// mapping: a version mismatch surfaces as a 409 RequestError.
func TestMapOptimisticLockError_VersionMismatchReturns409(t *testing.T) {
	err := MapOptimisticLockError(ErrVersionMismatch, "Project not found")
	require.Error(t, err)
	var reqErr *RequestError
	require.True(t, errors.As(err, &reqErr), "expected *RequestError, got %T", err)
	assert.Equal(t, http.StatusConflict, reqErr.Status)
}

// TestMapOptimisticLockError_OtherErrorReturnsNil verifies the passthrough
// contract: nil or a non-versioning error returns nil so callers fall
// through to their existing error mapping instead of a false 409/404.
func TestMapOptimisticLockError_OtherErrorReturnsNil(t *testing.T) {
	assert.Nil(t, MapOptimisticLockError(nil, "Project not found"))
	assert.Nil(t, MapOptimisticLockError(errors.New("some other store error"), "Project not found"))
}

// TestResolveOptimisticLock_HeaderPresent verifies If-Match header parsing
// flows through to the (expected, true, nil) success case.
func TestResolveOptimisticLock_HeaderPresent(t *testing.T) {
	c := newGinCtxWithHeader("If-Match", `"5"`)
	expected, present, err := ResolveOptimisticLock(c, nil)
	require.NoError(t, err)
	assert.True(t, present)
	assert.Equal(t, 5, expected)
}

// TestResolveOptimisticLock_BodyVersionFallback verifies the body 'version'
// fallback is used when If-Match is absent.
func TestResolveOptimisticLock_BodyVersionFallback(t *testing.T) {
	c := newGinCtxWithHeader("If-Match", "")
	bodyVersion := 3
	expected, present, err := ResolveOptimisticLock(c, &bodyVersion)
	require.NoError(t, err)
	assert.True(t, present)
	assert.Equal(t, 3, expected)
}

// TestResolveOptimisticLock_MissingSetsWarnHeader verifies the lenient
// rollout path: no version supplied and RequireIfMatch() false sets the
// Deprecation/Warning headers and returns (0, false, nil).
func TestResolveOptimisticLock_MissingSetsWarnHeader(t *testing.T) {
	SetRequireIfMatch(false)
	c := newGinCtxWithHeader("If-Match", "")
	expected, present, err := ResolveOptimisticLock(c, nil)
	require.NoError(t, err)
	assert.False(t, present)
	assert.Equal(t, 0, expected)
	assert.Equal(t, VersionDeprecationLink, c.Writer.Header().Get("Deprecation"))
	assert.NotEmpty(t, c.Writer.Header().Get("Warning"))
}

// TestResolveOptimisticLock_MissingReturns428WhenRequired verifies the
// strict enforcement path: no version supplied and RequireIfMatch() true
// returns a 428 RequestError.
func TestResolveOptimisticLock_MissingReturns428WhenRequired(t *testing.T) {
	SetRequireIfMatch(true)
	defer SetRequireIfMatch(false)
	c := newGinCtxWithHeader("If-Match", "")
	_, present, err := ResolveOptimisticLock(c, nil)
	require.Error(t, err)
	var reqErr *RequestError
	require.True(t, errors.As(err, &reqErr), "expected *RequestError, got %T", err)
	assert.Equal(t, http.StatusPreconditionRequired, reqErr.Status)
	assert.False(t, present)
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

// TestCheckAndBumpVersion_Wildcard verifies that an expected value of
// VersionWildcard (RFC 7232 "If-Match: *") bumps unconditionally regardless
// of the row's current version, and returns the resulting new version.
func TestCheckAndBumpVersion_Wildcard(t *testing.T) {
	db, _ := setupThreatModelAliasTestDB(t)
	id := uuid.New().String()
	tm := &models.ThreatModel{
		ID:                    models.DBVarchar(id),
		Name:                  "Wildcard Bump Test",
		OwnerInternalUUID:     models.DBVarchar(uuid.New().String()),
		CreatedByInternalUUID: models.DBVarchar(uuid.New().String()),
		ThreatModelFramework:  "STRIDE",
		Status:                "not_started",
		Version:               7,
	}
	require.NoError(t, db.Create(tm).Error)

	newVersion, err := CheckAndBumpVersion(context.Background(), db, "threat_models", id, VersionWildcard)
	require.NoError(t, err)
	assert.Equal(t, 8, newVersion, "wildcard bump must return current+1")

	var fresh models.ThreatModel
	require.NoError(t, db.First(&fresh, "id = ?", id).Error)
	assert.Equal(t, 8, fresh.Version)
}

// TestCheckAndBumpVersion_WildcardNotFound verifies that a wildcard CAS
// against a missing row returns ErrNotFound rather than ErrVersionMismatch —
// there is no version predicate under the wildcard, so a zero-rows-affected
// UPDATE can only mean the row does not exist.
func TestCheckAndBumpVersion_WildcardNotFound(t *testing.T) {
	db, _ := setupThreatModelAliasTestDB(t)
	_, err := CheckAndBumpVersion(context.Background(), db, "threat_models", uuid.New().String(), VersionWildcard)
	require.Error(t, err)
	assert.True(t, errors.Is(err, dberrors.ErrNotFound))
	assert.False(t, errors.Is(err, ErrVersionMismatch))
}

// TestCheckAndBumpVersion_WildcardReadBackNoRows is the regression guard for
// #581 finding 1: on the wildcard path, the version bump commits via
// UpdateColumn and the resulting version is then read back with a separate
// SELECT. If that read-back races a hard delete (or, on Oracle, hits a
// transient connectivity blip classified by dberrors), the old
// `.Row().Scan(&int)` implementation surfaced a bare, unclassified error
// (sql.ErrNoRows was not recognized by dberrors.Classify, and a nil *sql.Row
// receiver could even panic) that HandleRequestError would turn into an
// undocumented 500 — violating the Zero-500 policy. This test forces the
// read-back to fail with sql.ErrNoRows after a successful bump and asserts
// the helper now reports dberrors.ErrNotFound, which MapOptimisticLockError
// already maps to a documented 404 (see TestMapOptimisticLockError_NotFoundReturns404).
func TestCheckAndBumpVersion_WildcardReadBackNoRows(t *testing.T) {
	db, _ := setupThreatModelAliasTestDB(t)
	id := uuid.New().String()
	tm := &models.ThreatModel{
		ID:                    models.DBVarchar(id),
		Name:                  "Wildcard Read-Back Failure Test",
		OwnerInternalUUID:     models.DBVarchar(uuid.New().String()),
		CreatedByInternalUUID: models.DBVarchar(uuid.New().String()),
		ThreatModelFramework:  "STRIDE",
		Status:                "not_started",
		Version:               3,
	}
	require.NoError(t, db.Create(tm).Error)

	// Force the read-back SELECT (identified by its sql.NullInt64 dest,
	// which only the wildcard read-back in CheckAndBumpVersion uses) to fail
	// with sql.ErrNoRows, simulating a hard delete racing the SELECT that
	// follows the already-committed UPDATE.
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:inject_readback_norows", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*sql.NullInt64); ok {
			tx.Error = sql.ErrNoRows
		}
	}))
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove("test:inject_readback_norows")
	})

	_, err := CheckAndBumpVersion(context.Background(), db, "threat_models", id, VersionWildcard)
	require.Error(t, err)
	assert.True(t, errors.Is(err, dberrors.ErrNotFound),
		"read-back sql.ErrNoRows must classify to dberrors.ErrNotFound (404), not surface bare (500), got: %v", err)

	// The version must be UNCHANGED. Under read-then-CAS (#593) the SELECT comes
	// FIRST, so a failure here happens before anything is written — there is no
	// orphaned bump to reconcile. The old bump-then-read-back shape committed the
	// increment and only then discovered it could not report the result, which
	// left the row advanced for a request that returned an error.
	require.NoError(t, db.Callback().Query().Remove("test:inject_readback_norows"))
	var fresh models.ThreatModel
	require.NoError(t, db.First(&fresh, "id = ?", id).Error)
	assert.Equal(t, 3, fresh.Version,
		"a failed read must leave the version untouched; nothing is written until the CAS")
}

// TestCheckAndBumpVersion_WildcardReadBackTransient covers the sibling case:
// a transient (non-not-found) error on the read-back — e.g. a dropped
// connection — must not surface bare either (#581 finding 1b). The bump
// already committed, so CheckAndBumpVersion cannot honestly report the
// resulting version; it maps this to ErrVersionMismatch, reusing the
// already-documented 409 refetch-and-retry response rather than an
// undocumented status code.
func TestCheckAndBumpVersion_WildcardReadBackTransient(t *testing.T) {
	db, _ := setupThreatModelAliasTestDB(t)
	id := uuid.New().String()
	tm := &models.ThreatModel{
		ID:                    models.DBVarchar(id),
		Name:                  "Wildcard Read-Back Transient Test",
		OwnerInternalUUID:     models.DBVarchar(uuid.New().String()),
		CreatedByInternalUUID: models.DBVarchar(uuid.New().String()),
		ThreatModelFramework:  "STRIDE",
		Status:                "not_started",
		Version:               9,
	}
	require.NoError(t, db.Create(tm).Error)

	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:inject_readback_transient", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*sql.NullInt64); ok {
			tx.Error = errors.New("connection reset by peer")
		}
	}))
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove("test:inject_readback_transient")
	})

	_, err := CheckAndBumpVersion(context.Background(), db, "threat_models", id, VersionWildcard)
	require.Error(t, err)
	assert.True(t, dberrors.IsRetryable(err),
		"transient read-back error must stay classified ErrTransient so the enclosing retry wrapper retries it (#775), got: %v", err)
	assert.False(t, errors.Is(err, ErrVersionMismatch),
		"a transient blip must not be reported as a version conflict (#775)")
	assert.False(t, errors.Is(err, dberrors.ErrNotFound))

	require.NoError(t, db.Callback().Query().Remove("test:inject_readback_transient"))
	var fresh models.ThreatModel
	require.NoError(t, db.First(&fresh, "id = ?", id).Error)
	assert.Equal(t, 9, fresh.Version,
		"a failed read must leave the version untouched; nothing is written until the CAS (#593)")
}

// TestParseIfMatchHeader covers the header parsing surface: missing, bare
// integer, quoted ETag form, weak prefix, wildcard (bare and quoted), malformed.
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
		{"wildcard", "*", VersionWildcard, true, false},
		{"quoted wildcard", `"*"`, VersionWildcard, true, false},
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

// TestCheckAndBumpVersion_WildcardRetriesInsteadOfAdoptingAnotherVersion pins
// the defect #593 describes.
//
// The old wildcard path bumped unconditionally and then SELECTed the result for
// the ETag — two statements in autocommit. With two concurrent wildcard writers
// that interleaves: A bumps 5->6 and commits, B bumps 6->7, and A's read-back
// returns 7. A is handed an ETag describing B's representation, and A's next
// `If-Match: 7` then succeeds where it should have conflicted — a lost update
// beyond what opting out of version checking asked for.
//
// Read-then-CAS makes that impossible: the UPDATE carries `version = <what we
// read>`, so if the value we read is not the value in the row, it matches zero
// rows and we re-read rather than adopting anything. Here the first read is
// forced to return a STALE version, which is exactly the state a racing writer
// would leave us in; the helper must retry and still return the version it
// actually produced.
func TestCheckAndBumpVersion_WildcardRetriesInsteadOfAdoptingAnotherVersion(t *testing.T) {
	db, _ := setupThreatModelAliasTestDB(t)
	id := uuid.New().String()
	tm := &models.ThreatModel{
		ID:                    models.DBVarchar(id),
		Name:                  "Wildcard CAS Retry Test",
		OwnerInternalUUID:     models.DBVarchar(uuid.New().String()),
		CreatedByInternalUUID: models.DBVarchar(uuid.New().String()),
		ThreatModelFramework:  "STRIDE",
		Status:                "not_started",
		Version:               5,
	}
	require.NoError(t, db.Create(tm).Error)

	// Corrupt only the FIRST version read, standing in for a racing writer that
	// moved the row between our SELECT and our UPDATE. The CAS then matches no
	// rows, and the loop must re-read rather than trusting the stale value.
	var poisoned bool
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:stale_read", func(tx *gorm.DB) {
		dest, ok := tx.Statement.Dest.(*sql.NullInt64)
		if !ok || poisoned || !dest.Valid {
			return
		}
		poisoned = true
		dest.Int64-- // report a version that is no longer current
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove("test:stale_read") })

	got, err := CheckAndBumpVersion(context.Background(), db, "threat_models", id, VersionWildcard)
	require.NoError(t, err, "one stale read must be absorbed by the retry loop")

	var fresh models.ThreatModel
	require.NoError(t, db.First(&fresh, "id = ?", id).Error)

	assert.Equal(t, fresh.Version, got,
		"the returned ETag version must be the row's ACTUAL version — the old "+
			"read-back could hand back a value produced by a different writer")
	assert.Equal(t, 6, got, "5 -> 6, exactly one bump by this writer")
	assert.True(t, poisoned, "the stale-read injection must actually have fired")
}

// TestGormThreatModelStore_UpdateWithVersion_SameTxAtomicity is the
// regression pin for #594: the version CAS and the content write must
// commit or roll back together as a single transaction, not as two
// autocommit statements that another writer's CAS could interleave with.
func TestGormThreatModelStore_UpdateWithVersion_SameTxAtomicity(t *testing.T) {
	db, user := setupThreatModelAliasTestDB(t)
	store := NewGormThreatModelStore(db)

	providerID := user.ProviderUserID.String
	owner := User{
		PrincipalType: UserPrincipalTypeUser,
		Provider:      string(user.Provider),
		ProviderId:    providerID,
	}
	emptyAuth := []Authorization{}
	tm := ThreatModel{
		Name:          "UpdateWithVersion Atomicity Test",
		Owner:         owner,
		CreatedBy:     &owner,
		Authorization: &emptyAuth,
	}
	idSetter := func(item ThreatModel, id string) ThreatModel {
		uid, _ := uuid.Parse(id)
		item.Id = &uid
		return item
	}
	created, err := store.Create(tm, idSetter)
	require.NoError(t, err)
	id := created.Id.String()

	var seeded models.ThreatModel
	require.NoError(t, db.First(&seeded, "id = ?", id).Error)

	t.Run("correct expected version persists content and bumps version", func(t *testing.T) {
		updated := created
		updated.Name = "Renamed via UpdateWithVersion"
		newVersion, err := store.UpdateWithVersion(context.Background(), id, updated, seeded.Version)
		require.NoError(t, err)
		assert.Equal(t, seeded.Version+1, newVersion)

		var row models.ThreatModel
		require.NoError(t, db.First(&row, "id = ?", id).Error)
		assert.Equal(t, "Renamed via UpdateWithVersion", string(row.Name))
		assert.Equal(t, seeded.Version+1, row.Version)
	})

	t.Run("wrong expected version leaves content and version unchanged", func(t *testing.T) {
		var before models.ThreatModel
		require.NoError(t, db.First(&before, "id = ?", id).Error)

		attempted := created
		attempted.Name = "Should Not Persist"
		_, err := store.UpdateWithVersion(context.Background(), id, attempted, before.Version+99)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrVersionMismatch))

		var after models.ThreatModel
		require.NoError(t, db.First(&after, "id = ?", id).Error)
		assert.Equal(t, before.Version, after.Version, "version must not change on a mismatched CAS")
		assert.Equal(t, string(before.Name), string(after.Name), "content must not change on a mismatched CAS")
	})

	t.Run("in-tx content-write failure rolls back the version bump", func(t *testing.T) {
		var before models.ThreatModel
		require.NoError(t, db.First(&before, "id = ?", id).Error)

		// An owner identifier that resolves to no user makes
		// resolveUserIdentifierToUUID fail inside the transaction, after the
		// CAS has already bumped the version. If the CAS and the content
		// write are not one transaction, the version bump survives this
		// failure — that was #594.
		badOwner := created
		badOwner.Owner = User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      string(user.Provider),
			ProviderId:    "no-such-user-" + uuid.New().String(),
		}
		_, err := store.UpdateWithVersion(context.Background(), id, badOwner, before.Version)
		require.Error(t, err, "content write must fail: owner identifier does not resolve to any user")
		assert.False(t, errors.Is(err, ErrVersionMismatch), "this is a content-write failure, not a CAS mismatch")

		var after models.ThreatModel
		require.NoError(t, db.First(&after, "id = ?", id).Error)
		assert.Equal(t, before.Version, after.Version,
			"#594 regression pin: a failed content write must roll back the version CAS that guarded it")
	})
}

// TestCheckAndBumpVersion_WildcardReadBackNonTransientStays409 pins the
// #581 1b half that #775 keeps: a read failure that is neither not-found nor
// transient still maps to ErrVersionMismatch (409), never a bare 500.
func TestCheckAndBumpVersion_WildcardReadBackNonTransientStays409(t *testing.T) {
	db, _ := setupThreatModelAliasTestDB(t)
	id := uuid.New().String()
	tm := &models.ThreatModel{
		ID:                    models.DBVarchar(id),
		Name:                  "Wildcard Read-Back Non-Transient Test",
		OwnerInternalUUID:     models.DBVarchar(uuid.New().String()),
		CreatedByInternalUUID: models.DBVarchar(uuid.New().String()),
		ThreatModelFramework:  "STRIDE",
		Status:                "not_started",
		Version:               3,
	}
	require.NoError(t, db.Create(tm).Error)

	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:inject_readback_odd", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*sql.NullInt64); ok {
			tx.Error = errors.New("ORA-00942: table or view does not exist")
		}
	}))
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove("test:inject_readback_odd")
	})

	_, err := CheckAndBumpVersion(context.Background(), db, "threat_models", id, VersionWildcard)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrVersionMismatch), "got: %v", err)
	assert.False(t, dberrors.IsRetryable(err))
}
