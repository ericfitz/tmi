// Package dbschema: pinned-session DDL execution for startup migrations
// (#734).
//
// CREATE/DROP INDEX takes an exclusive DDL lock on the target table. USERS is
// hot (login updates, sparse inserts), and Oracle's DDL_LOCK_TIMEOUT defaults
// to 0 -- "fail immediately" -- so a single concurrent DML holder makes the
// statement raise ORA-00054 (resource busy) rather than wait. During a rolling
// deploy the old pods are still issuing UPDATE users SET last_login..., which
// made the retry budget alone (3 attempts x 20ms, all inside the same DML
// window) effectively one shot: startup aborted and the pod crash-looped until
// the window happened to clear.
//
// DDL_LOCK_TIMEOUT is a session setting, and database/sql gives no guarantee
// that two calls through the pool land on the same Oracle session -- so the
// ALTER SESSION and the DDL it configures must run on one pinned *sql.Conn,
// the same reason and pattern as the advisory lock in advisory_lock.go.
package dbschema

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

const (
	// oracleDDLLockTimeoutSeconds is how long a migration DDL statement waits
	// for the table's DDL lock before raising ORA-00054. Replaces Oracle's
	// default of 0 (fail immediately). Long enough to outlast an in-flight
	// login UPDATE on USERS many times over, short enough that ddlMaxAttempts
	// retries stay inside the advisory lock's own 300s request timeout.
	oracleDDLLockTimeoutSeconds = 30

	// oracleDDLStatementTimeout bounds the whole pinned-session exchange
	// (ALTER SESSION + the DDL itself) so a wedged session cannot hang startup
	// indefinitely -- the same reasoning as #733. Comfortably larger than
	// oracleDDLLockTimeoutSeconds plus the index build itself.
	oracleDDLStatementTimeout = 5 * time.Minute

	// ddlMaxAttempts and ddlBaseRetryDelay widen the retry budget for DDL
	// specifically. withMigrationRetry's dedupeMaxAttempts/dedupeRetryDelay
	// (3 x 20ms) is sized for cheap catalog SELECTs and is far too tight for
	// a lock-contended CREATE INDEX: with backoff doubling from
	// ddlBaseRetryDelay, four attempts span ~3.5s of backoff on top of up to
	// oracleDDLLockTimeoutSeconds of in-statement waiting each.
	ddlMaxAttempts = 4
)

// ddlBaseRetryDelay is the first backoff interval in withDDLRetry, doubling
// each attempt. A var, not a const, so tests can shrink it (same convention as
// oracleLockKeepaliveInterval in advisory_lock.go).
var ddlBaseRetryDelay = 500 * time.Millisecond

// execMigrationDDL runs a single startup-migration DDL statement.
//
// On Oracle it pins a connection, sets DDL_LOCK_TIMEOUT on that session, and
// issues the DDL there, so the statement queues behind concurrent DML instead
// of failing instantly with ORA-00054 (#734). On every other dialect the DDL
// goes through GORM unchanged -- PostgreSQL's CREATE INDEX already waits on
// its own lock queue, and SQLite is single-writer by construction.
//
// The statement text is built by this package from constants, never from user
// input.
func execMigrationDDL(db *gorm.DB, ddl string) error {
	if db.Name() != "oracle" {
		return db.Exec(ddl).Error
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get sql.DB for migration DDL: %w", err)
	}

	// Same single-slot-pool trap as the advisory lock (#711 follow-up):
	// pinning the pool's only connection here while the advisory lock already
	// holds one would block until the context deadline instead of running.
	if stats := sqlDB.Stats(); stats.MaxOpenConnections == 1 {
		slogging.Get().Warn("migration DDL: connection pool max_open_conns=1 leaves no slot for a pinned DDL session; raising pool ceiling to 2")
		sqlDB.SetMaxOpenConns(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), oracleDDLStatementTimeout)
	defer cancel()

	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("get pinned sql.Conn for migration DDL: %w", err)
	}
	defer func() {
		// Reset DDL_LOCK_TIMEOUT before handing the session back: Close()
		// returns it to the pool with whatever session state it carries, and
		// an unrelated later statement inheriting a 30s DDL wait is
		// surprising even though it is harmless (it only affects DDL, and
		// waiting beats failing fast). Best effort -- if the reset fails the
		// session is still usable (oracle-db-admin review, #734).
		if _, rerr := conn.ExecContext(ctx, "ALTER SESSION SET DDL_LOCK_TIMEOUT = 0"); rerr != nil {
			slogging.Get().Debug("could not reset DDL_LOCK_TIMEOUT on the pinned DDL session before returning it to the pool: %v", rerr)
		}
		if cerr := conn.Close(); cerr != nil {
			slogging.Get().Warn("closing pinned migration-DDL connection failed: %v", cerr)
		}
	}()

	// ALTER SESSION does not accept a bind variable for DDL_LOCK_TIMEOUT; the
	// value is an in-package integer constant, not user input.
	if _, err := conn.ExecContext(ctx,
		fmt.Sprintf("ALTER SESSION SET DDL_LOCK_TIMEOUT = %d", oracleDDLLockTimeoutSeconds),
	); err != nil {
		// Not fatal on its own: without the setting the DDL simply reverts to
		// Oracle's fail-fast behavior, which withDDLRetry still covers.
		slogging.Get().Warn("failed to set DDL_LOCK_TIMEOUT=%d on the pinned DDL session; the DDL below will fail fast on lock contention instead of waiting: %v",
			oracleDDLLockTimeoutSeconds, err)
	}

	_, err = conn.ExecContext(ctx, ddl)
	return err
}

// withDDLRetry runs a migration DDL attempt up to ddlMaxAttempts times with
// exponential backoff, retrying only transient errors (ORA-00054 "resource
// busy" and the connection blips dberrors classifies as ErrTransient).
//
// Separate from withMigrationRetry because the two have genuinely different
// shapes: that one guards cheap catalog SELECTs where a fixed 20ms retry is
// right, this one guards lock-contended DDL where the contending window is
// measured in seconds (#734).
func withDDLRetry(label string, fn func() error) error {
	var err error
	delay := ddlBaseRetryDelay
	for attempt := 1; attempt <= ddlMaxAttempts; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		if !dberrors.IsRetryable(dberrors.Classify(err)) || attempt == ddlMaxAttempts {
			return err
		}
		slogging.Get().Warn("migration DDL %q: transient error (attempt %d/%d), retrying in %s: %v",
			label, attempt, ddlMaxAttempts, delay, err)
		time.Sleep(delay)
		delay *= 2
	}
	return err
}
