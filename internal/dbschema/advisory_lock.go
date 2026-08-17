package dbschema

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// oracleLockTimeoutSeconds is the maximum time DBMS_LOCK.REQUEST will wait for
// the named lock before returning status=1 (timeout). Replaces DBMS_LOCK.MAXWAIT
// (effectively infinite) so a stuck replica cannot block another replica's
// startup indefinitely. 5 minutes is generous for schema migration steps.
const oracleLockTimeoutSeconds = 300

// oracleLockKeepaliveInterval is how often the pinned lock session is pinged
// while the lock is held (#723). A var so tests can shrink it. 60s sits far
// under any plausible ADB IDLE_TIME floor (resource-manager minimums are
// minutes) while adding negligible load.
var oracleLockKeepaliveInterval = 60 * time.Second

// MigrationLockName is the one cross-replica lock every schema-evolution
// entry point serializes on: the server's startup migration
// (runMigrationsLocked), `tmi-dbtool --schema`, and the deprecated
// InitAuthWithConfig path. They all run the same DDL against the same schema,
// so they must contend for the same name -- a second name would serialize
// each entry point against itself and nothing else (#737).
const MigrationLockName = "tmi_schema_migration"

// WithMigrationLock runs fn while holding the named cross-replica advisory
// lock, releasing it on every exit path.
//
// SQLite (used by narrow unit tests and some tooling) has no advisory locks;
// AcquireMigrationLock reports that as "unsupported dialect", and fn runs
// unlocked -- a single-process in-memory SQLite is inherently single-writer.
// Any other acquisition failure is returned without running fn: proceeding
// unserialized is exactly the concurrent-DDL exposure the lock exists to
// prevent.
func WithMigrationLock(ctx context.Context, db *gorm.DB, name string, fn func() error) error {
	release, err := AcquireMigrationLock(ctx, db, name)
	if err != nil {
		if !strings.Contains(err.Error(), "unsupported dialect") {
			return fmt.Errorf("failed to acquire schema-migration advisory lock: %w", err)
		}
		slogging.Get().Warn("schema migration: skipping advisory lock for dialect %q: %v", db.Name(), err)
		release = func() {}
	}
	defer release()

	return fn()
}

// AcquireMigrationLock takes an exclusive, server-wide named lock that is
// released by calling the returned function. Used to serialize startup-time
// migrations across multiple replicas. The function blocks until the lock
// is acquired (subject to context cancellation).
//
// On PostgreSQL: uses pg_advisory_lock with a deterministic int64 derived
// from sha256(name). On Oracle: uses DBMS_LOCK.ALLOCATE_UNIQUE +
// DBMS_LOCK.REQUEST. Other dialects return an error.
//
// The release function is idempotent and safe to defer.
// SEM@dea6869d1be777f8e297ccf30db5f7c272b226f9: lock a named schema-migration advisory lock, dispatching to the correct dialect implementation (reads DB)
func AcquireMigrationLock(ctx context.Context, db *gorm.DB, name string) (release func(), err error) {
	logger := slogging.Get()
	dialect := db.Name()

	switch dialect {
	case "postgres":
		sqlDB, err := db.DB()
		if err != nil {
			return nil, fmt.Errorf("get sql.DB for advisory lock: %w", err)
		}
		return acquirePGLock(ctx, sqlDB, name, logger)
	case "oracle":
		sqlDB, err := db.DB()
		if err != nil {
			return nil, fmt.Errorf("get sql.DB for advisory lock: %w", err)
		}
		return acquireOracleLock(ctx, sqlDB, name, logger)
	default:
		return nil, fmt.Errorf("AcquireMigrationLock: unsupported dialect %q", dialect)
	}
}

// acquirePGLock acquires a named pg_advisory_lock. Advisory locks are
// session-scoped exactly like Oracle DBMS_LOCK, and database/sql gives no
// guarantee that separate calls through the pool land on the same backend, so
// lock and unlock must run on one pinned *sql.Conn held open for the lock's
// lifetime (#726, mirroring #711). pg_advisory_unlock reports failure via its
// BOOLEAN result (false = this session did not hold the lock), not via an
// error, so the release checks the scanned value and logs ERROR on false —
// a false here means the lock leaked or another session was serialized
// against nothing. Either case means this session's lock state is unknown,
// so — mirroring acquireOracleLock's dropConn — the connection is discarded
// via driver.ErrBadConn rather than returned to the pool: a plain Close()
// would hand a possibly-still-locked backend back to an unrelated query.
// SEM@dea6869d1be777f8e297ccf30db5f7c272b226f9: acquire a PostgreSQL advisory lock on a pinned session connection and return a release function (reads DB)
func acquirePGLock(ctx context.Context, sqlDB *sql.DB, name string, logger *slogging.Logger) (func(), error) {
	key := nameToInt64(name)

	// Same single-slot-pool deadlock as Oracle (#711 follow-up): the pinned
	// conn would consume the only slot while AutoMigrate waits for one.
	if stats := sqlDB.Stats(); stats.MaxOpenConnections == 1 {
		logger.Warn("PG advisory lock: connection pool max_open_conns=1 would deadlock AutoMigrate (pinned lock connection would leave no slot for migration queries); raising pool ceiling to 2")
		sqlDB.SetMaxOpenConns(2)
	}

	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("get pinned sql.Conn for advisory lock: %w", err)
	}

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", key); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("pg_advisory_lock: %w", err)
	}
	logger.Debug("Acquired pg_advisory_lock(%d) for %q", key, name)

	var releaseOnce sync.Once
	return func() {
		releaseOnce.Do(func() {
			dropConn := false
			defer func() {
				if dropConn {
					// Lock state is unknown (unlock errored or reported this
					// session didn't hold it). Conn.Raw's callback returning
					// driver.ErrBadConn makes database/sql discard the
					// connection synchronously, so a further conn.Close()
					// call would be redundant — see the identical pattern
					// (and its rationale) in acquireOracleLock's release.
					_ = conn.Raw(func(any) error { return driver.ErrBadConn })
					return
				}
				if err := conn.Close(); err != nil {
					logger.Warn("closing pinned advisory-lock connection failed: %v", err)
				}
			}()
			var unlocked bool
			if err := conn.QueryRowContext(ctx, "SELECT pg_advisory_unlock($1)", key).Scan(&unlocked); err != nil {
				logger.Error("pg_advisory_unlock(%d) failed: %v", key, err)
				dropConn = true
				return
			}
			if !unlocked {
				logger.Error("pg_advisory_unlock(%d) returned false — this session did not hold the lock (lock leaked or session was recycled)", key)
				dropConn = true
			}
		})
	}, nil
}

// acquireOracleLock acquires a named DBMS_LOCK using godror's driver-level OUT
// bind support. PL/SQL anonymous blocks do NOT produce result sets — they
// return values via OUT bind variables, which require sql.Out{Dest: &x}. Using
// db.Raw(...).Row().Scan(...) on a PL/SQL block fails (no result set), so we
// call ExecContext directly against the underlying *sql.DB rather than
// through GORM.
//
// DBMS_LOCK ownership is session-scoped in Oracle, but database/sql gives no
// guarantee that separate ExecContext calls land on the same pooled
// connection. ALLOCATE_UNIQUE, REQUEST, and RELEASE must therefore run on one
// pinned *sql.Conn (sqlDB.Conn(ctx)) rather than through the general pool, or
// the lock can be requested and released against different Oracle sessions
// (issue #711). That pinned session must stay open — the conn held, not
// returned to the pool — for the whole duration the caller holds the lock,
// so the session-scoped lock survives; the caller's own locked work (e.g.
// AutoMigrate) runs through its normal pooled connections, not this one. The
// pinned conn is closed on every exit path: immediately on any acquisition
// error, and inside the returned release function otherwise. Taking *sql.DB
// (rather than *gorm.DB) also keeps this function testable with a plain
// database/sql mock driver, independent of GORM's dialector.
//
// All binds are positional (:1, :2, ...). Mixing ? and named binds (:h, :s)
// is unreliable on godror.
// SEM@2bc5bf8f3e1be695fa3f274458939777e390e85b: acquire an Oracle DBMS_LOCK exclusive lock on a pinned session connection, raising a single-slot pool to two first, and return a release function (reads DB)
func acquireOracleLock(ctx context.Context, sqlDB *sql.DB, name string, logger *slogging.Logger) (func(), error) {
	// A pool sized to exactly one connection (TMI_DB_MAX_OPEN_CONNS=1) cannot
	// survive pinning a connection for the lock's lifetime: sqlDB.Conn below
	// takes the pool's only slot and holds it for the whole migration, so the
	// caller's own locked work (e.g. AutoMigrate), which runs through the
	// normal pool rather than this pinned conn, has no slot left and blocks
	// forever on context.Background() (#711 follow-up). Bump the ceiling to 2
	// so the pinned lock connection and the migration's own queries each get
	// a slot; pools already sized >=2 (including the default of 10) are left
	// untouched.
	if stats := sqlDB.Stats(); stats.MaxOpenConnections == 1 {
		logger.Warn("Oracle advisory lock: connection pool max_open_conns=1 would deadlock AutoMigrate (pinned lock connection would leave no slot for migration queries); raising pool ceiling to 2")
		sqlDB.SetMaxOpenConns(2)
	}

	// Pin a single physical connection for the lifetime of the lock so
	// ALLOCATE_UNIQUE, REQUEST, and RELEASE all execute on the same Oracle
	// session. This conn is held open (not returned to the pool) for as long
	// as the caller holds the lock, so the session-scoped lock stays valid —
	// the caller's own locked work runs on other pooled connections, not
	// this one.
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("get pinned sql.Conn for advisory lock: %w", err)
	}

	// DBMS_LOCK.ALLOCATE_UNIQUE returns a handle for a named lock via OUT bind.
	var handle string
	if _, err := conn.ExecContext(ctx,
		`BEGIN DBMS_LOCK.ALLOCATE_UNIQUE(lockname => :1, lockhandle => :2); END;`,
		name, sql.Out{Dest: &handle},
	); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("DBMS_LOCK.ALLOCATE_UNIQUE: %w", err)
	}

	// DBMS_LOCK.REQUEST returns its status as the function value (OUT bind on :1).
	// lockmode=6 (X_MODE / EXCLUSIVE), finite timeout (replaces MAXWAIT so a stuck
	// replica cannot block startup forever), release_on_commit=FALSE so the lock
	// survives implicit DDL commits during migration — this also has to survive
	// the keepalive ping below: godror commits on every Exec issued outside an
	// explicit transaction, so each keepalive ping commits on this session too;
	// if release_on_commit is ever flipped to TRUE, the keepalive itself would
	// silently release the lock roughly once a minute.
	var status int
	if _, err := conn.ExecContext(ctx,
		`BEGIN :1 := DBMS_LOCK.REQUEST(lockhandle => :2, lockmode => 6, timeout => :3, release_on_commit => FALSE); END;`,
		sql.Out{Dest: &status}, handle, oracleLockTimeoutSeconds,
	); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("DBMS_LOCK.REQUEST: %w", err)
	}
	if status != 0 {
		_ = conn.Close()
		return nil, fmt.Errorf(
			"DBMS_LOCK.REQUEST status=%d (1=timeout, 2=deadlock, 3=parameter error, 4=already owned, 5=illegal handle)",
			status,
		)
	}
	logger.Debug("Acquired DBMS_LOCK for %q (handle=%s)", name, handle)

	// #723: ADB resource-manager IDLE_TIME limits can kill a session that
	// issues no SQL — exactly what this pinned conn does while AutoMigrate
	// runs on other connections. A killed session silently releases the
	// DBMS_LOCK, letting two replicas migrate concurrently. Ping the pinned
	// session periodically while the lock is held. A failed ping means the
	// session (and with it the lock) is likely gone: log ERROR and stop.
	keepaliveStop := make(chan struct{})
	var keepaliveWG sync.WaitGroup
	keepaliveWG.Add(1)
	go func() {
		defer keepaliveWG.Done()
		ticker := time.NewTicker(oracleLockKeepaliveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-keepaliveStop:
				return
			case <-ticker.C:
				if _, err := conn.ExecContext(ctx, "SELECT 1 FROM DUAL"); err != nil {
					logger.Error("advisory-lock keepalive ping failed — pinned session may have been killed and the cross-replica migration lock lost: %v", err)
					return
				}
			}
		}
	}()

	var releaseOnce sync.Once
	return func() {
		releaseOnce.Do(func() {
			close(keepaliveStop)
			keepaliveWG.Wait() // conn must never be used by two goroutines at once

			dropConn := false
			defer func() {
				if dropConn {
					// The session's lock state is unknown (RELEASE failed or
					// was not owned). Returning this conn to the pool would
					// hand a possibly-lock-holding session to an unrelated
					// query. Mark it bad so database/sql destroys the
					// physical session — Oracle frees session-scoped locks
					// when the session ends. This assumes each *sql.Conn maps
					// to a dedicated Oracle session (godror's default
					// standalone-connection mode, which is what TMI's Oracle
					// dialector uses — see auth/db/gorm_oracle.go); if that
					// ever changes to a pooled/DRCP connect string, closing
					// the Go-level conn would only return the session to
					// Oracle's own pool with the lock state intact, not end
					// it.
					//
					// Conn.Raw's callback returning driver.ErrBadConn makes Raw
					// itself synchronously discard the connection (database/sql's
					// putConn sees errors.Is(err, driver.ErrBadConn) and calls
					// dc.Close() inline, rather than merely flagging the conn for
					// a later Close() to notice), so a further conn.Close() call
					// here would be redundant — the *sql.Conn is already done,
					// and Close() would just return sql.ErrConnDone. Skip it.
					_ = conn.Raw(func(any) error { return driver.ErrBadConn })
					return
				}
				if err := conn.Close(); err != nil {
					logger.Warn("closing pinned advisory-lock connection failed: %v", err)
				}
			}()

			var rstatus int
			if _, err := conn.ExecContext(ctx,
				`BEGIN :1 := DBMS_LOCK.RELEASE(lockhandle => :2); END;`,
				sql.Out{Dest: &rstatus}, handle,
			); err != nil {
				logger.Error("DBMS_LOCK.RELEASE failed (session will be discarded, not pooled): %v", err)
				dropConn = true
				return
			}
			switch rstatus {
			case 0:
				// released cleanly
			case 4:
				logger.Error("DBMS_LOCK.RELEASE returned status=4 (does not own lock) — the lock was already lost (idle-killed session?); another replica may have migrated concurrently")
				dropConn = true
			default:
				logger.Warn("DBMS_LOCK.RELEASE returned status=%d (3=parameter error, 5=illegal handle)", rstatus)
				dropConn = true
			}
		})
	}, nil
}

// nameToInt64 hashes a name string to a deterministic int64 for use as a
// pg_advisory_lock key. Two different names will produce different keys
// with overwhelming probability.
// SEM@73235c4c5e292c1a307c5bb6d625a4cb06eb57f2: convert a lock name to a stable int64 key via SHA-256 for use with pg_advisory_lock (pure)
func nameToInt64(name string) int64 {
	h := sha256.Sum256([]byte(name))
	return int64(binary.BigEndian.Uint64(h[:8])) // #nosec G115 -- deterministic hash; signed wrap is fine
}
