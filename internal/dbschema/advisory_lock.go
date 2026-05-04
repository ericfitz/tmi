package dbschema

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

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
func AcquireMigrationLock(ctx context.Context, db *gorm.DB, name string) (release func(), err error) {
	logger := slogging.Get()
	dialect := db.Name()

	switch dialect {
	case "postgres":
		return acquirePGLock(ctx, db, name, logger)
	case "oracle":
		return acquireOracleLock(ctx, db, name, logger)
	default:
		return nil, fmt.Errorf("AcquireMigrationLock: unsupported dialect %q", dialect)
	}
}

func acquirePGLock(ctx context.Context, db *gorm.DB, name string, logger *slogging.Logger) (func(), error) {
	key := nameToInt64(name)
	if err := db.WithContext(ctx).Exec("SELECT pg_advisory_lock(?)", key).Error; err != nil {
		return nil, fmt.Errorf("pg_advisory_lock: %w", err)
	}
	logger.Debug("Acquired pg_advisory_lock(%d) for %q", key, name)
	released := false
	return func() {
		if released {
			return
		}
		released = true
		if err := db.Exec("SELECT pg_advisory_unlock(?)", key).Error; err != nil {
			logger.Warn("pg_advisory_unlock(%d) failed: %v", key, err)
		}
	}, nil
}

func acquireOracleLock(ctx context.Context, db *gorm.DB, name string, logger *slogging.Logger) (func(), error) {
	// DBMS_LOCK.ALLOCATE_UNIQUE returns a handle for a named lock.
	var handle string
	if err := db.WithContext(ctx).Raw(`
		BEGIN
			DBMS_LOCK.ALLOCATE_UNIQUE(lockname => ?, lockhandle => :h);
		END;
	`, name).Row().Scan(&handle); err != nil {
		return nil, fmt.Errorf("DBMS_LOCK.ALLOCATE_UNIQUE: %w", err)
	}

	// DBMS_LOCK.REQUEST(handle, lockmode=6 (EXCLUSIVE), timeout=MAXWAIT, release_on_commit=FALSE)
	var status int
	if err := db.WithContext(ctx).Raw(`
		BEGIN
			:s := DBMS_LOCK.REQUEST(lockhandle => ?, lockmode => 6, timeout => DBMS_LOCK.MAXWAIT, release_on_commit => FALSE);
		END;
	`, handle).Row().Scan(&status); err != nil {
		return nil, fmt.Errorf("DBMS_LOCK.REQUEST: %w", err)
	}
	if status != 0 {
		return nil, fmt.Errorf("DBMS_LOCK.REQUEST returned status %d (non-zero)", status)
	}
	logger.Debug("Acquired DBMS_LOCK for %q (handle=%s)", name, handle)

	released := false
	return func() {
		if released {
			return
		}
		released = true
		var rstatus int
		if err := db.Raw(`
			BEGIN
				:r := DBMS_LOCK.RELEASE(lockhandle => ?);
			END;
		`, handle).Row().Scan(&rstatus); err != nil {
			logger.Warn("DBMS_LOCK.RELEASE failed: %v", err)
		}
	}, nil
}

// nameToInt64 hashes a name string to a deterministic int64 for use as a
// pg_advisory_lock key. Two different names will produce different keys
// with overwhelming probability.
func nameToInt64(name string) int64 {
	h := sha256.Sum256([]byte(name))
	return int64(binary.BigEndian.Uint64(h[:8])) //nolint:gosec // deterministic-hash; signed wrap is fine
}
