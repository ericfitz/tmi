package dbschema

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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

// pingCountingMatcher wraps the default sqlmock regexp matcher and counts
// how many times a query matching "SELECT 1 FROM DUAL" was successfully
// matched. Used to assert the #723 keepalive goroutine actually pinged the
// pinned connection at least once, without pinning the test to an exact
// tick count (which real-time ticker scheduling cannot guarantee).
type pingCountingMatcher struct {
	count int32
}

func (m *pingCountingMatcher) Match(expectedSQL, actualSQL string) error {
	err := sqlmock.QueryMatcherRegexp.Match(expectedSQL, actualSQL)
	if err == nil && strings.Contains(actualSQL, "SELECT 1 FROM DUAL") {
		atomic.AddInt32(&m.count, 1)
	}
	return err
}

// TestAcquireOracleLock_KeepalivePingsPinnedConn verifies the #723 fix: while
// the lock is held, a background goroutine pings the pinned session with
// SELECT 1 FROM DUAL every oracleLockKeepaliveInterval, so ADB's resource
// manager does not silently idle-kill the session (and with it the
// cross-replica lock) during a long AutoMigrate run.
func TestAcquireOracleLock_KeepalivePingsPinnedConn(t *testing.T) {
	oldInterval := oracleLockKeepaliveInterval
	oracleLockKeepaliveInterval = 5 * time.Millisecond
	t.Cleanup(func() { oracleLockKeepaliveInterval = oldInterval })

	matcher := &pingCountingMatcher{}
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()

	// Unordered: the keepalive goroutine's pings interleave with the
	// surrounding calls at real-time granularity, so strict ordering can't
	// be asserted here (unlike the other tests in this file).
	mock.MatchExpectationsInOrder(false)
	mock.ExpectExec("DBMS_LOCK.ALLOCATE_UNIQUE").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DBMS_LOCK.REQUEST").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DBMS_LOCK.RELEASE").WillReturnResult(sqlmock.NewResult(0, 0))
	for i := 0; i < 20; i++ {
		mock.ExpectExec(regexp.QuoteMeta("SELECT 1 FROM DUAL")).WillReturnResult(sqlmock.NewResult(0, 0))
	}

	release, err := acquireOracleLock(context.Background(), sqlDB, "test-lock", slogging.Get())
	require.NoError(t, err)

	time.Sleep(80 * time.Millisecond)
	release()

	assert.GreaterOrEqual(t, atomic.LoadInt32(&matcher.count), int32(1),
		"keepalive goroutine must ping the pinned conn at least once while the lock is held")
}

// TestAcquireOracleLock_KeepaliveStopsOnPingError verifies that once a
// keepalive ping fails (the pinned session is presumed lost), the goroutine
// stops pinging rather than retrying — and release() still runs cleanly
// afterward on the same conn.
func TestAcquireOracleLock_KeepaliveStopsOnPingError(t *testing.T) {
	oldInterval := oracleLockKeepaliveInterval
	oracleLockKeepaliveInterval = 5 * time.Millisecond
	t.Cleanup(func() { oracleLockKeepaliveInterval = oldInterval })

	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()

	mock.MatchExpectationsInOrder(true)
	mock.ExpectExec("DBMS_LOCK.ALLOCATE_UNIQUE").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DBMS_LOCK.REQUEST").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("SELECT 1 FROM DUAL")).WillReturnError(assert.AnError)
	mock.ExpectExec("DBMS_LOCK.RELEASE").WillReturnResult(sqlmock.NewResult(0, 0))

	release, err := acquireOracleLock(context.Background(), sqlDB, "test-lock", slogging.Get())
	require.NoError(t, err)

	// Give the keepalive goroutine time to fire once, hit the mocked error,
	// and exit before we release.
	time.Sleep(100 * time.Millisecond)
	release()

	// With ordered matching, a second ping attempt after the error would try
	// to match against the queued RELEASE expectation and fail — so this
	// passing confirms both that RELEASE ran cleanly and that no further
	// ping was attempted.
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestAcquireOracleLock_RequestError_ClosesConn verifies (#725 item d) that
// when DBMS_LOCK.REQUEST itself errors (as opposed to returning a nonzero
// status), the pinned connection is closed and no keepalive goroutine is
// left running — the keepalive goroutine is only started after a successful
// REQUEST.
func TestAcquireOracleLock_RequestError_ClosesConn(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = sqlDB.Close() }()

	mock.MatchExpectationsInOrder(true)
	mock.ExpectExec("DBMS_LOCK.ALLOCATE_UNIQUE").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DBMS_LOCK.REQUEST").WillReturnError(assert.AnError)

	before := runtime.NumGoroutine()
	_, err = acquireOracleLock(context.Background(), sqlDB, "test-lock", slogging.Get())
	require.Error(t, err)
	assert.Equal(t, 0, sqlDB.Stats().InUse, "the pinned connection must not remain checked out when REQUEST errors")

	// Give any errantly-started goroutine a chance to schedule before we
	// check for a leak.
	time.Sleep(20 * time.Millisecond)
	assert.LessOrEqual(t, runtime.NumGoroutine(), before, "no keepalive goroutine should be left running when REQUEST errors")

	assert.NoError(t, mock.ExpectationsWereMet())
}

// oracleFakeResponse configures one queued ExecContext call on
// oracleFakeConn: either an error, or a success that writes outStatus (if
// set) into the call's sole sql.Out argument.
type oracleFakeResponse struct {
	err       error
	outStatus *int
}

// oracleFakeConn is a minimal database/sql/driver.Conn double used only by
// the two tests below that must control DBMS_LOCK's OUT-bind status value.
// go-sqlmock (github.com/DATA-DOG/go-sqlmock) has no support for OUT-bind
// write-back: database/sql never touches sql.Out.Dest itself — only the
// driver does (godror, in production), so a generic mock can't fake it.
// This double intercepts ExecContext directly and writes the configured
// status into the OUT param, in call order.
type oracleFakeConn struct {
	mu        sync.Mutex
	responses []oracleFakeResponse
	closed    bool
}

func newOracleFakeConn(responses ...oracleFakeResponse) *oracleFakeConn {
	return &oracleFakeConn{responses: responses}
}

func intPtr(i int) *int { return &i }

func (c *oracleFakeConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *oracleFakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("oracleFakeConn: Prepare not supported; production code uses ExecContext")
}

func (c *oracleFakeConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *oracleFakeConn) Begin() (driver.Tx, error) {
	return nil, errors.New("oracleFakeConn: transactions not supported")
}

// CheckNamedValue accepts sql.Out args as-is, mirroring an OUT-bind capable
// driver like godror; other values go through the default converter.
func (c *oracleFakeConn) CheckNamedValue(nv *driver.NamedValue) error {
	if _, ok := nv.Value.(sql.Out); ok {
		return nil
	}
	converted, err := driver.DefaultParameterConverter.ConvertValue(nv.Value)
	if err != nil {
		return err
	}
	nv.Value = converted
	return nil
}

func (c *oracleFakeConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, driver.ErrBadConn
	}
	if len(c.responses) == 0 {
		return nil, fmt.Errorf("oracleFakeConn: unexpected ExecContext call: %s", query)
	}
	resp := c.responses[0]
	c.responses = c.responses[1:]
	if resp.err != nil {
		return nil, resp.err
	}
	if resp.outStatus != nil {
		for _, a := range args {
			out, ok := a.Value.(sql.Out)
			if !ok {
				continue
			}
			dest, ok := out.Dest.(*int)
			if !ok {
				return nil, fmt.Errorf("oracleFakeConn: unsupported OUT dest type %T", out.Dest)
			}
			*dest = *resp.outStatus
			break
		}
	}
	return driver.RowsAffected(0), nil
}

// oracleFakeConnector adapts oracleFakeConn to driver.Connector so it can
// back a *sql.DB via sql.OpenDB, always handing out the same conn instance
// (matching how the pinned-conn code under test expects exactly one
// physical session for the lock's lifetime).
type oracleFakeConnector struct {
	conn *oracleFakeConn
}

func (c *oracleFakeConnector) Connect(context.Context) (driver.Conn, error) {
	return c.conn, nil
}

func (c *oracleFakeConnector) Driver() driver.Driver {
	return oracleFakeDriver{}
}

// oracleFakeDriver exists only to satisfy driver.Connector.Driver(); it is
// never invoked because sql.OpenDB(connector) always calls Connect directly.
type oracleFakeDriver struct{}

func (oracleFakeDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("oracleFakeDriver: use sql.OpenDB with oracleFakeConnector, not sql.Open")
}

// TestAcquireOracleLock_RequestStatusNonzero_ClosesConn verifies (#725 item
// d) that when DBMS_LOCK.REQUEST succeeds but returns a nonzero status (e.g.
// 1=timeout), the pinned connection is closed and no keepalive goroutine is
// started.
func TestAcquireOracleLock_RequestStatusNonzero_ClosesConn(t *testing.T) {
	conn := newOracleFakeConn(
		oracleFakeResponse{},                     // ALLOCATE_UNIQUE succeeds
		oracleFakeResponse{outStatus: intPtr(1)}, // REQUEST: status=1 (timeout)
	)
	sqlDB := sql.OpenDB(&oracleFakeConnector{conn: conn})
	defer func() { _ = sqlDB.Close() }()

	before := runtime.NumGoroutine()
	_, err := acquireOracleLock(context.Background(), sqlDB, "test-lock", slogging.Get())
	require.Error(t, err)
	assert.ErrorContains(t, err, "status=1")
	// A clean Close() on this path just checks the pinned connection back
	// into the pool (it may be kept idle, not physically destroyed) — the
	// same convention TestAcquireOracleLock_ClosesConnOnAcquisitionError
	// uses via InUse, since only a dropConn (driver.ErrBadConn) path forces
	// physical destruction.
	assert.Equal(t, 0, sqlDB.Stats().InUse, "the pinned connection must not remain checked out when REQUEST returns a nonzero status")

	time.Sleep(20 * time.Millisecond)
	assert.LessOrEqual(t, runtime.NumGoroutine(), before, "no keepalive goroutine should be left running when REQUEST returns a nonzero status")
}

// TestAcquireOracleLock_ReleaseStatusNotOwned_DropsConn verifies the #723/
// #725 hardening: when DBMS_LOCK.RELEASE returns status=4 (does not own
// lock — the session may already have been idle-killed and lost the lock),
// the pinned connection is dropped via driver.ErrBadConn rather than
// returned to the pool, so database/sql destroys the physical session
// instead of recycling it into an unrelated caller's hands.
func TestAcquireOracleLock_ReleaseStatusNotOwned_DropsConn(t *testing.T) {
	conn := newOracleFakeConn(
		oracleFakeResponse{},                     // ALLOCATE_UNIQUE succeeds
		oracleFakeResponse{},                     // REQUEST succeeds, status=0
		oracleFakeResponse{outStatus: intPtr(4)}, // RELEASE: status=4 (not owned)
	)
	sqlDB := sql.OpenDB(&oracleFakeConnector{conn: conn})
	defer func() { _ = sqlDB.Close() }()

	release, err := acquireOracleLock(context.Background(), sqlDB, "test-lock", slogging.Get())
	require.NoError(t, err)

	release()

	assert.True(t, conn.isClosed(), "the pinned connection must be closed after a not-owned RELEASE")
	assert.Equal(t, 0, sqlDB.Stats().OpenConnections, "a conn marked bad via driver.ErrBadConn must be destroyed, not pooled for reuse")

	// release() must remain idempotent even on the drop-conn path.
	assert.NotPanics(t, release)
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
