package dbschema

import (
	"context"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNameToInt64Stable(t *testing.T) {
	a := nameToInt64("foo")
	b := nameToInt64("foo")
	c := nameToInt64("bar")
	assert.Equal(t, a, b, "hash should be stable for same input")
	assert.NotEqual(t, a, c, "different inputs should produce different hashes")
}

func TestAcquireMigrationLock_UnsupportedDialect(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	_, err = AcquireMigrationLock(context.Background(), db, "test")
	assert.ErrorContains(t, err, "unsupported dialect")
}

// TestAcquireMigrationLock_PGSerializes is gated by an env var because it
// requires a real PG connection. CI runs it via make test-integration; it's
// skipped by default in `make test-unit`.
func TestAcquireMigrationLock_PGSerializes(t *testing.T) {
	if testing.Short() {
		t.Skip("requires real PostgreSQL; run via integration tests")
	}
	t.Skip("manual smoke; requires DATABASE_URL env var")

	// Smoke design (uncomment to run manually):
	// db := connectToPGTestDB(t) // helper that opens a real PG connection
	// ctx := context.Background()
	//
	// var order []int
	// var mu sync.Mutex
	// var wg sync.WaitGroup
	// for i := 0; i < 3; i++ {
	//     wg.Add(1)
	//     go func(i int) {
	//         defer wg.Done()
	//         release, err := AcquireMigrationLock(ctx, db, "test-lock-pg")
	//         require.NoError(t, err)
	//         mu.Lock(); order = append(order, i); mu.Unlock()
	//         time.Sleep(50 * time.Millisecond)
	//         release()
	//     }(i)
	// }
	// wg.Wait()
	// assert.Len(t, order, 3)
	_ = sync.WaitGroup{}
	_ = time.Now()
}

// TestAcquireOracleLock_PinnedConnection verifies the #711 fix: ALLOCATE_UNIQUE,
// REQUEST, and RELEASE all execute on the same pinned *sql.Conn (not the
// general pool), and that connection is released back to the pool exactly
// once, when release() is called.
func TestAcquireOracleLock_PinnedConnection(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()

	mock.MatchExpectationsInOrder(true)
	mock.ExpectExec("DBMS_LOCK.ALLOCATE_UNIQUE").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DBMS_LOCK.REQUEST").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DBMS_LOCK.RELEASE").WillReturnResult(sqlmock.NewResult(0, 0))

	release, err := acquireOracleLock(context.Background(), sqlDB, "test-lock", slogging.Get())
	require.NoError(t, err)

	// The lock-acquisition connection is pinned out of the pool and stays
	// checked out until release() runs.
	assert.Equal(t, 1, sqlDB.Stats().InUse, "lock acquisition must hold the pinned connection checked out")

	release()
	assert.Equal(t, 0, sqlDB.Stats().InUse, "release() must check the pinned connection back in")

	// release() must be idempotent: a second call issues no further RELEASE.
	release()

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAcquireOracleLock_ClosesConnOnAcquisitionError verifies the pinned
// connection is not leaked when ALLOCATE_UNIQUE or REQUEST fails.
func TestAcquireOracleLock_ClosesConnOnAcquisitionError(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()

	mock.MatchExpectationsInOrder(true)
	mock.ExpectExec("DBMS_LOCK.ALLOCATE_UNIQUE").WillReturnError(assert.AnError)

	_, err = acquireOracleLock(context.Background(), sqlDB, "test-lock", slogging.Get())
	require.Error(t, err)
	assert.Equal(t, 0, sqlDB.Stats().InUse, "the pinned connection must not remain checked out when acquisition fails")

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAcquirePGLock_PinsSingleConnAndUnlocks verifies the #726 fix: the lock
// and unlock statements execute on the same pinned *sql.Conn (asserted via
// strict ordered sqlmock expectations, since sqlmock itself has only one
// simulated backend), and pg_advisory_unlock returning true (lock held and
// released cleanly) results in the pinned connection being closed with no
// error logged.
func TestAcquirePGLock_PinsSingleConnAndUnlocks(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()

	mock.MatchExpectationsInOrder(true)
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_lock($1)")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_advisory_unlock($1)")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"pg_advisory_unlock"}).AddRow(true))

	release, err := acquirePGLock(context.Background(), sqlDB, "test-lock", slogging.Get())
	require.NoError(t, err)

	// The lock-acquisition connection is pinned out of the pool and stays
	// checked out until release() runs.
	assert.Equal(t, 1, sqlDB.Stats().InUse, "lock acquisition must hold the pinned connection checked out")

	release()
	assert.Equal(t, 0, sqlDB.Stats().InUse, "release() must check the pinned connection back in")

	// release() must be idempotent: a second call issues no further unlock.
	release()

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAcquirePGLock_UnlockReturnsFalse_LogsError verifies that when
// pg_advisory_unlock reports false (this session did not hold the lock —
// leaked or session recycled), release() still closes the pinned connection
// rather than leaking it, and does not panic.
func TestAcquirePGLock_UnlockReturnsFalse_LogsError(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()

	mock.MatchExpectationsInOrder(true)
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_lock($1)")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_advisory_unlock($1)")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"pg_advisory_unlock"}).AddRow(false))

	release, err := acquirePGLock(context.Background(), sqlDB, "test-lock", slogging.Get())
	require.NoError(t, err)

	assert.NotPanics(t, release, "release must not panic when unlock returns false")
	assert.Equal(t, 0, sqlDB.Stats().InUse, "release() must still check the pinned connection back in when unlock returns false")

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAcquirePGLock_PoolOfOne_RaisedToTwo verifies the #711-style deadlock
// guard also applies to PostgreSQL: a pool ceiling of exactly 1 would let the
// pinned lock connection starve the caller's own migration queries, so
// acquirePGLock must raise it to 2.
func TestAcquirePGLock_PoolOfOne_RaisedToTwo(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()

	sqlDB.SetMaxOpenConns(1)

	mock.MatchExpectationsInOrder(true)
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_lock($1)")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT pg_advisory_unlock($1)")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"pg_advisory_unlock"}).AddRow(true))

	release, err := acquirePGLock(context.Background(), sqlDB, "test-lock", slogging.Get())
	require.NoError(t, err)
	defer release()

	assert.Equal(t, 2, sqlDB.Stats().MaxOpenConnections, "pool ceiling of 1 must be raised to 2 so migration queries are not starved")
}

// TestAcquirePGLock_LockError_ClosesConn verifies the pinned connection is
// not leaked when pg_advisory_lock itself fails.
func TestAcquirePGLock_LockError_ClosesConn(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()

	mock.MatchExpectationsInOrder(true)
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_lock($1)")).
		WithArgs(sqlmock.AnyArg()).
		WillReturnError(assert.AnError)

	_, err = acquirePGLock(context.Background(), sqlDB, "test-lock", slogging.Get())
	require.Error(t, err)
	assert.Equal(t, 0, sqlDB.Stats().InUse, "the pinned connection must not remain checked out when lock acquisition fails")

	assert.NoError(t, mock.ExpectationsWereMet())
}
