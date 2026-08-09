package dbschema

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// oracleLockTimeoutSeconds is the maximum time DBMS_LOCK.REQUEST will wait for
// the named lock before returning status=1 (timeout). Replaces DBMS_LOCK.MAXWAIT
// (effectively infinite) so a stuck replica cannot block another replica's
// startup indefinitely. 5 minutes is generous for schema migration steps.
const oracleLockTimeoutSeconds = 300

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
// SEM@3d0932f1: lock a named schema-migration advisory lock, dispatching to the correct dialect implementation (reads DB)
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
// against nothing.
// SEM@0000000: acquire a PostgreSQL advisory lock on a pinned session connection and return a release function (reads DB)
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

	released := false
	return func() {
		if released {
			return
		}
		released = true
		defer func() {
			if err := conn.Close(); err != nil {
				logger.Warn("closing pinned advisory-lock connection failed: %v", err)
			}
		}()
		var unlocked bool
		if err := conn.QueryRowContext(ctx, "SELECT pg_advisory_unlock($1)", key).Scan(&unlocked); err != nil {
			logger.Error("pg_advisory_unlock(%d) failed: %v", key, err)
			return
		}
		if !unlocked {
			logger.Error("pg_advisory_unlock(%d) returned false — this session did not hold the lock (lock leaked or session was recycled)", key)
		}
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
// SEM@3d0932f18cc283406a531ef6fac4204f8f61d66a: acquire an Oracle DBMS_LOCK exclusive lock on a pinned session connection, raising a single-slot pool to two first, and return a release function (reads DB)
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
	// survives implicit DDL commits during migration.
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

	released := false
	return func() {
		if released {
			return
		}
		released = true
		defer func() {
			if err := conn.Close(); err != nil {
				logger.Warn("closing pinned advisory-lock connection failed: %v", err)
			}
		}()
		var rstatus int
		if _, err := conn.ExecContext(ctx,
			`BEGIN :1 := DBMS_LOCK.RELEASE(lockhandle => :2); END;`,
			sql.Out{Dest: &rstatus}, handle,
		); err != nil {
			logger.Warn("DBMS_LOCK.RELEASE failed: %v", err)
			return
		}
		if rstatus != 0 {
			logger.Warn("DBMS_LOCK.RELEASE returned status=%d (3=parameter error, 4=not owned, 5=illegal handle)", rstatus)
		}
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
