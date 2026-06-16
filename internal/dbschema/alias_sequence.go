// Package dbschema: global ThreatModel alias sequence (#452).
//
// The ThreatModel alias is a globally-unique, monotonically-increasing
// integer. It was originally allocated with a single-row
// SELECT ... FOR UPDATE against the alias_counters '__global__'/'threat_model'
// row, taken on every threat-model create. Once #451 made every write
// transaction SERIALIZABLE that one hot row became a serialization
// bottleneck: on Oracle (snapshot serializable) a waiter that sees the
// counter row committed-modified since its snapshot raises ORA-08177,
// converting lock contention into abort-and-retry churn on a single row.
//
// A database sequence removes the contention entirely. NEXTVAL is
// non-transactional — it does not participate in the caller's snapshot, so it
// can never raise ORA-08177 / 40001, and concurrent creates each get a
// distinct value without serializing. The trade-off is gaps on rollback,
// which the alias contract already tolerated (a rolled-back create previously
// "released" its number; now it simply leaves a gap).
package dbschema

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// ThreatModelAliasSequenceName is the sequence that allocates the global
// ThreatModel alias on PostgreSQL and Oracle. Unquoted, Oracle folds the
// identifier to upper case; api.AllocateNextAlias references the same literal
// when issuing NEXTVAL, and must be kept in sync.
const ThreatModelAliasSequenceName = "tmi_threat_model_alias_seq"

// aliasSeedDeployBuffer is added to the seed so the sequence's range starts
// well above the legacy row counter's next value. During a rolling deploy,
// old pods keep allocating from the alias_counters '__global__' row (their
// per-process gate never flips) while new pods allocate from the sequence; a
// zero buffer would make both hand out the same values in lockstep and collide
// on the unique threat_models.alias index (a hard, non-retryable
// ORA-00001/23505 create failure). The buffer must exceed the number of
// threat-model creates a fully-drained set of old pods could perform during
// the deploy window. Aliases are opaque and already gap-tolerant (rollback
// leaves gaps), so the one-time wasted range is harmless.
const aliasSeedDeployBuffer = 10000

// InstallThreatModelAliasSequence creates the global ThreatModel alias
// sequence if it does not already exist, seeded above every alias currently in
// use so its first NEXTVAL can never collide with the unique
// threat_models.alias index.
//
// It is idempotent and safe to run on every boot: once the sequence exists the
// seed step is skipped, so an already-advanced sequence is never moved
// backwards (which could otherwise hand out a value an in-flight create
// already holds). SQLite and any other dialect keep the row-counter allocator
// and are no-ops here.
func InstallThreatModelAliasSequence(ctx context.Context, db *gorm.DB) error {
	logger := slogging.Get()
	switch db.Name() {
	case "postgres":
		return installPostgresAliasSequence(ctx, db, logger)
	case "oracle":
		return installOracleAliasSequence(ctx, db, logger)
	case "sqlite":
		logger.Info("InstallThreatModelAliasSequence: skipping on dialect %q (row-counter allocator retained)", db.Name())
		return nil
	default:
		logger.Warn("InstallThreatModelAliasSequence: unsupported dialect %q, skipping; row-counter allocator retained", db.Name())
		return nil
	}
}

// aliasSeedStart returns the value the sequence should START WITH so its first
// NEXTVAL is safely above every alias currently in use AND above the range the
// legacy row counter would still hand out from old pods during a rolling
// deploy: the greatest of the max committed threat_models.alias and the legacy
// alias_counters high-water mark, plus one, plus aliasSeedDeployBuffer
// (minimum 1 + buffer on a fresh database). COALESCE keeps the result non-NULL
// when either source is empty; the legacy counter stores the NEXT value, so
// one is subtracted to recover the highest already used.
func aliasSeedStart(ctx context.Context, db *gorm.DB) (int64, error) {
	var maxInUse int64
	q := `SELECT GREATEST(
	        COALESCE((SELECT MAX(alias) FROM threat_models), 0),
	        COALESCE((SELECT MAX(next_alias) - 1 FROM alias_counters WHERE parent_id = '__global__' AND object_type = 'threat_model'), 0)
	      )`
	if err := db.WithContext(ctx).Raw(q).Scan(&maxInUse).Error; err != nil {
		return 0, fmt.Errorf("compute alias seed start: %w", err)
	}
	return maxInUse + 1 + aliasSeedDeployBuffer, nil
}

func installPostgresAliasSequence(ctx context.Context, db *gorm.DB, logger *slogging.Logger) error {
	start, err := aliasSeedStart(ctx, db)
	if err != nil {
		return err
	}
	// IF NOT EXISTS makes this a no-op after first creation, so START WITH
	// only applies once and an advanced sequence is preserved across boots.
	stmt := fmt.Sprintf(`CREATE SEQUENCE IF NOT EXISTS %s AS integer START WITH %d INCREMENT BY 1 MINVALUE 1`,
		ThreatModelAliasSequenceName, start)
	if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
		return fmt.Errorf("postgres alias sequence install: %w", err)
	}
	logger.Info("InstallThreatModelAliasSequence: postgres sequence %s ensured (seed start=%d)", ThreatModelAliasSequenceName, start)
	return nil
}

func installOracleAliasSequence(ctx context.Context, db *gorm.DB, logger *slogging.Logger) error {
	// Oracle 19c has no CREATE SEQUENCE IF NOT EXISTS, so guard with a PL/SQL
	// existence check and compute START WITH inside the block, applying the
	// seed atomically with creation. user_sequences stores the folded
	// upper-case identifier.
	//
	// The sequence is left at Oracle's default CACHE 20 NOORDER. NOORDER is
	// intentional: on RAC/ADB each instance draws from a disjoint cached range,
	// so values stay unique (the only contractual requirement on the alias)
	// without the cross-instance SGA coordination ORDER would impose. The alias
	// is therefore not strictly globally monotonic across instances, which is
	// fine — it already tolerates gaps from rollbacks.
	// The + aliasSeedDeployBuffer term must match aliasSeedStart on the PG side
	// so both dialects keep the sequence range clear of the legacy row counter
	// during a rolling deploy.
	block := fmt.Sprintf(`DECLARE
	  v_count NUMBER;
	  v_start NUMBER;
	BEGIN
	  SELECT COUNT(*) INTO v_count FROM user_sequences WHERE sequence_name = 'TMI_THREAT_MODEL_ALIAS_SEQ';
	  IF v_count = 0 THEN
	    SELECT GREATEST(
	             NVL((SELECT MAX(alias) FROM threat_models), 0),
	             NVL((SELECT MAX(next_alias) - 1 FROM alias_counters WHERE parent_id = '__global__' AND object_type = 'threat_model'), 0)
	           ) + 1 + %d
	      INTO v_start FROM dual;
	    EXECUTE IMMEDIATE 'CREATE SEQUENCE tmi_threat_model_alias_seq START WITH ' || v_start || ' INCREMENT BY 1 NOCYCLE';
	  END IF;
	END;`, aliasSeedDeployBuffer)
	if err := db.WithContext(ctx).Exec(block).Error; err != nil {
		return fmt.Errorf("oracle alias sequence install: %w", err)
	}
	logger.Info("InstallThreatModelAliasSequence: oracle sequence %s ensured", ThreatModelAliasSequenceName)
	return nil
}
