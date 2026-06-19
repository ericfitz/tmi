// Package dbschema: PostgreSQL default-isolation defense-in-depth (#450).
//
// #448/#449 thread &sql.TxOptions{Isolation: LevelSerializable} through the
// retry wrapper so every write transaction runs SERIALIZABLE. That is the
// primary, cross-database, leak-proof mechanism. This installer adds a
// belt-and-suspenders backstop on PostgreSQL only: it pins the connecting
// role's default_transaction_isolation to 'serializable', so any connection
// that somehow bypasses the wrapper still starts its transactions at the safe
// level rather than READ COMMITTED.
//
// Scope and limits:
//   - PostgreSQL only. Oracle ADB has no ALTER DATABASE/ALTER ROLE
//     default-isolation knob and blocks the logon-trigger workaround;
//     enforcement there is per-transaction via the wrapper's TxOptions, which
//     this package cannot and does not change. The Oracle side of #450 is
//     verification, not configuration.
//   - ALTER ROLE ... SET takes effect for sessions opened AFTER it runs.
//     Connections already in the pool keep their session default until they
//     are recycled (or the server restarts). Because this is only a backstop
//     behind the per-transaction wrapper, that lag is acceptable.
package dbschema

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// InstallPostgresDefaultIsolation pins the connecting role's
// default_transaction_isolation to 'serializable' on PostgreSQL, as
// defense-in-depth behind the per-transaction SERIALIZABLE wrapper. It is a
// no-op on every other dialect. Non-fatal by contract: callers log and
// continue on error (the wrapper remains the authoritative mechanism).
//
// ALTER ROLE CURRENT_USER is used rather than ALTER DATABASE because it is the
// portable form — it works on managed PostgreSQL (e.g. Heroku) where the
// connecting role owns its own settings but may not own the database.
// SEM@080eef4c36738f6b82a5dddaff40f2580081b8bc: pin the current PostgreSQL role default transaction isolation to serializable; no-op on other dialects (mutates shared state)
func InstallPostgresDefaultIsolation(ctx context.Context, db *gorm.DB) error {
	logger := slogging.Get()
	if db.Name() != "postgres" {
		logger.Info("InstallPostgresDefaultIsolation: skipping on dialect %q (per-transaction wrapper is the only isolation lever)", db.Name())
		return nil
	}

	const stmt = `ALTER ROLE CURRENT_USER SET default_transaction_isolation = 'serializable'`
	if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
		return fmt.Errorf("postgres default isolation install: %w", err)
	}
	logger.Info("InstallPostgresDefaultIsolation: role default_transaction_isolation pinned to 'serializable' (effective for sessions opened after this point)")
	return nil
}
