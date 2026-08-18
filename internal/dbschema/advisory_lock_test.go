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

// SEM@73235c4c5e292c1a307c5bb6d625a4cb06eb57f2: validate a name hash is stable and input-sensitive (pure)
func TestNameToInt64Stable(t *testing.T) {
	a := nameToInt64("foo")
	b := nameToInt64("foo")
	c := nameToInt64("bar")
	assert.Equal(t, a, b, "hash should be stable for same input")
	assert.NotEqual(t, a, c, "different inputs should produce different hashes")
}

// SEM@73235c4c5e292c1a307c5bb6d625a4cb06eb57f2: validate migration lock acquisition rejects an unsupported DB dialect
func TestAcquireMigrationLock_UnsupportedDialect(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	_, err = AcquireMigrationLock(context.Background(), db, "test")
	assert.ErrorContains(t, err, "unsupported dialect")
}

// TestAcquireMigrationLock_PGSerializes is gated by an env var because it
// requires a real PG connection. CI runs it via make test-integration; it's
// skipped by default in `make test-unit`.
// SEM@73235c4c5e292c1a307c5bb6d625a4cb06eb57f2: validate the migration lock serializes concurrent PostgreSQL acquirers (manual/skipped)
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
// SEM@8ea37221e3186b49d52e78d8834a4e6dd35d2b93: validate the Oracle advisory lock's acquire/release run on one pinned connection
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
// SEM@8ea37221e3186b49d52e78d8834a4e6dd35d2b93: validate the pinned connection isn't leaked when Oracle lock acquisition fails
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
// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: count sqlmock keepalive pings matching the Oracle DUAL query (test double)
type pingCountingMatcher struct {
	count int32
}

// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: match a SQL query and tally matching keepalive pings (mutates shared state)
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
// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: validate the Oracle lock's keepalive goroutine pings the pinned connection
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
// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: validate the Oracle keepalive goroutine stops pinging after a failed ping
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
// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: validate no keepalive goroutine leaks when Oracle lock REQUEST errors
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
// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: hold a queued fake response for the Oracle connection test double
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
// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: fake an Oracle driver connection that emulates OUT-bind status writes (test double)
type oracleFakeConn struct {
	mu        sync.Mutex
	responses []oracleFakeResponse
	closed    bool
}

// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: build a fake Oracle connection preloaded with queued responses
func newOracleFakeConn(responses ...oracleFakeResponse) *oracleFakeConn {
	return &oracleFakeConn{responses: responses}
}

// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: return a pointer to an int value (pure)
func intPtr(i int) *int { return &i }

// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: check whether the fake Oracle connection has been closed (pure)
func (c *oracleFakeConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: reject Prepare calls on the fake Oracle connection (test double)
func (c *oracleFakeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("oracleFakeConn: Prepare not supported; production code uses ExecContext")
}

// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: mark the fake Oracle connection closed (mutates shared state)
func (c *oracleFakeConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: reject transactions on the fake Oracle connection (test double)
func (c *oracleFakeConn) Begin() (driver.Tx, error) {
	return nil, errors.New("oracleFakeConn: transactions not supported")
}

// CheckNamedValue accepts sql.Out args as-is, mirroring an OUT-bind capable
// driver like godror; other values go through the default converter.
// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: validate a named parameter value, passing sql.Out args through unchanged (pure)
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

// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: execute the next queued fake response, writing status into an OUT param
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
// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: adapt a fake Oracle connection into a driver.Connector for sql.OpenDB (test double)
type oracleFakeConnector struct {
	conn *oracleFakeConn
}

// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: hand out the single pinned fake connection instance for every Connect call (test double)
func (c *oracleFakeConnector) Connect(context.Context) (driver.Conn, error) {
	return c.conn, nil
}

// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: return the fake Oracle driver instance backing this connector (pure)
func (c *oracleFakeConnector) Driver() driver.Driver {
	return oracleFakeDriver{}
}

// oracleFakeDriver exists only to satisfy driver.Connector.Driver(); it is
// never invoked because sql.OpenDB(connector) always calls Connect directly.
// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: stub driver.Driver satisfying the driver.Connector interface, never actually invoked (test double)
type oracleFakeDriver struct{}

// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: reject direct sql.Open calls, since the fake driver is only usable via sql.OpenDB (pure)
func (oracleFakeDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("oracleFakeDriver: use sql.OpenDB with oracleFakeConnector, not sql.Open")
}

// TestAcquireOracleLock_RequestStatusNonzero_ClosesConn verifies (#725 item
// d) that when DBMS_LOCK.REQUEST succeeds but returns a nonzero status (e.g.
// 1=timeout), the pinned connection is closed and no keepalive goroutine is
// started.
// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: verify an Oracle advisory lock request returning a nonzero status closes the connection and starts no keepalive
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
// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: verify releasing an Oracle advisory lock with a not-owned status drops the pinned connection
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
// SEM@dea6869d1be777f8e297ccf30db5f7c272b226f9: verify acquiring a Postgres advisory lock pins a single connection and unlocks cleanly on release
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
// leaked or session recycled), release() still discards the pinned
// connection (marking it bad rather than leaking it) and does not panic.
// SEM@dea6869d1be777f8e297ccf30db5f7c272b226f9: verify a failed Postgres advisory unlock still discards the pinned connection without panicking
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
// SEM@dea6869d1be777f8e297ccf30db5f7c272b226f9: verify acquiring a Postgres advisory lock raises a pool ceiling of one connection to two
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
// SEM@dea6869d1be777f8e297ccf30db5f7c272b226f9: verify a failed Postgres advisory lock acquisition closes the pinned connection without leaking it
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

// TestWithMigrationLock_UnsupportedDialectRunsUnlocked covers #737's SQLite
// path: no advisory locks exist there, so fn must still run (a single-process
// in-memory SQLite is inherently single-writer) rather than the whole
// migration aborting.
func TestWithMigrationLock_UnsupportedDialectRunsUnlocked(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	ran := false
	require.NoError(t, WithMigrationLock(context.Background(), db, MigrationLockName, func() error {
		ran = true
		return nil
	}))
	assert.True(t, ran, "fn must run unlocked on a dialect without advisory locks")
}

// TestWithMigrationLock_PropagatesCallbackError covers the plumbing: a
// migration step's own failure must reach the caller unchanged, not be masked
// by the lock wrapper.
func TestWithMigrationLock_PropagatesCallbackError(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	sentinel := errors.New("migration step failed")
	assert.ErrorIs(t, WithMigrationLock(context.Background(), db, MigrationLockName, func() error {
		return sentinel
	}), sentinel)
}

// TestMigrationLockName_IsSharedByEveryEntryPoint pins the one thing that
// makes #737 work: the server, dbtool, and the deprecated config-adapter path
// must contend for the SAME name, since a per-entry-point name would
// serialize each against itself and nothing else.
func TestMigrationLockName_IsSharedByEveryEntryPoint(t *testing.T) {
	assert.Equal(t, "tmi_schema_migration", MigrationLockName)
}
