package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync/atomic"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrAliasSequenceMissing indicates the global ThreatModel alias sequence does
// not exist at NEXTVAL time — it was installed at startup (so useAliasSequence
// is on) but has since been dropped (schema drift / a DB reset under a running
// server). It is recoverable: GormThreatModelStore.Create reinstalls the
// sequence (idempotent, seeded above the current max alias) and retries the
// create once. The error chain also satisfies errors.Is(err,
// dberrors.ErrUndefinedObject).
var ErrAliasSequenceMissing = errors.New("threat_model alias sequence missing")

const (
	// globalAliasParent / threatModelAliasObjectType identify the single
	// global ThreatModel alias scope. It is the only scope every create
	// contends on, so on PostgreSQL and Oracle it is allocated from a
	// non-transactional sequence instead of the per-scope FOR UPDATE row,
	// eliminating the SERIALIZABLE serialization hot spot (#452).
	globalAliasParent          = "__global__"
	threatModelAliasObjectType = "threat_model"
	// threatModelAliasSeqName must match dbschema.ThreatModelAliasSequenceName.
	threatModelAliasSeqName = "tmi_threat_model_alias_seq"
)

// useAliasSequence gates the sequence-backed global ThreatModel alias path. It
// is turned on by EnableThreatModelAliasSequence only after the server has
// successfully installed the sequence; until then (and in unit tests, which
// never install it) AllocateNextAlias uses the row-counter path. Without this
// gate, a failed or absent sequence install would make every threat-model
// create fail on a missing sequence.
var useAliasSequence atomic.Bool

// EnableThreatModelAliasSequence signals that the global ThreatModel alias
// sequence exists and AllocateNextAlias may draw the global alias from it. The
// server calls this once, after dbschema.InstallThreatModelAliasSequence
// succeeds on PostgreSQL or Oracle.
// SEM@15d1523404ac67830fbe68f72a41c9683aa564b6: enable DB-sequence-backed alias allocation for the global threat model scope (mutates shared state)
func EnableThreatModelAliasSequence() { useAliasSequence.Store(true) }

// AllocateNextAlias atomically reserves the next alias value for the given
// (parentID, objectType) scope. MUST be called inside a transaction. The
// returned value is guaranteed unique within the scope so long as the calling
// transaction commits successfully.
//
// For the ThreatModel global counter, pass parentID="__global__" and
// objectType="threat_model". For sub-objects, parentID is the parent
// ThreatModel UUID and objectType is one of "diagram", "threat", "asset",
// "repository", "note", "document".
//
// The global ThreatModel scope is served from a DB sequence on PostgreSQL and
// Oracle (see #452); every other scope — and SQLite — uses the row-counter
// path below. Per-scope sub-object counters are naturally partitioned by their
// parent threat model and never became a global hot spot.
//
// Note: on the row-counter path, if the calling transaction rolls back the
// counter UPDATE rolls back too — the alias is "released" and reused. On the
// sequence path the drawn value is gone (gap) whether or not the create
// commits. Both paths only ever yield committed, unique aliases.
// SEM@15d1523404ac67830fbe68f72a41c9683aa564b6: atomically reserve the next alias integer for a given parent/object-type scope within a transaction (reads DB)
func AllocateNextAlias(ctx context.Context, tx *gorm.DB, parentID, objectType string) (int32, error) {
	if useAliasSequence.Load() && parentID == globalAliasParent && objectType == threatModelAliasObjectType {
		switch tx.Name() {
		case "postgres", "oracle":
			return allocateAliasFromSequence(ctx, tx)
		}
	}
	return allocateNextAliasRowLocked(ctx, tx, parentID, objectType)
}

// allocateAliasFromSequence draws the next global ThreatModel alias from the DB
// sequence. NEXTVAL is non-transactional: it never participates in the caller's
// serializable snapshot (so it cannot raise ORA-08177 / 40001) and is not
// rolled back.
// SEM@178dbd0418cfb7e057d4297c7a88c5879cb64c7f: fetch the next value from the DB sequence for global threat model alias allocation (reads DB)
func allocateAliasFromSequence(ctx context.Context, tx *gorm.DB) (int32, error) {
	var query string
	switch tx.Name() {
	case "postgres":
		query = fmt.Sprintf("SELECT nextval('%s')", threatModelAliasSeqName)
	case "oracle":
		query = fmt.Sprintf("SELECT %s.NEXTVAL FROM dual", threatModelAliasSeqName)
	default:
		return 0, fmt.Errorf("sequence alias allocation unsupported on dialect %q", tx.Name())
	}

	var next int64
	if err := tx.WithContext(ctx).Raw(query).Scan(&next).Error; err != nil {
		// A dropped sequence surfaces as PG 42P01 / ORA-02289. Classify it so the
		// caller can distinguish recoverable schema drift (reinstall + retry) from
		// a genuine failure, instead of returning a 500 either way.
		classified := dberrors.Classify(err)
		if errors.Is(classified, dberrors.ErrUndefinedObject) {
			return 0, fmt.Errorf("%w: %w", ErrAliasSequenceMissing, classified)
		}
		return 0, fmt.Errorf("threat_model alias sequence nextval: %w", classified)
	}
	if next < 1 || next > math.MaxInt32 {
		return 0, fmt.Errorf("threat_model alias sequence value %d out of int32 range", next)
	}
	return int32(next), nil
}

// allocateNextAliasRowLocked is the original SELECT ... FOR UPDATE row-counter
// allocator, retained for per-scope sub-object aliases and for SQLite.
// SEM@ebf201816c3638ec74fc8483a2a649af3ccddfc9: reserve the next alias by row-locking and incrementing a counter row in a transaction (reads DB)
func allocateNextAliasRowLocked(ctx context.Context, tx *gorm.DB, parentID, objectType string) (int32, error) {
	logger := slogging.Get()

	// Insert counter row if missing. ON CONFLICT DO NOTHING is idempotent.
	row := models.AliasCounter{ParentID: models.DBVarchar(parentID), ObjectType: models.DBVarchar(objectType), NextAlias: 1}
	if err := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		logger.Error("alias_counters upsert failed: parent=%s type=%s err=%v", parentID, objectType, err)
		return 0, fmt.Errorf("alias_counters upsert: %w", err)
	}

	// Lock the row and read the current value.
	var counter models.AliasCounter
	err := tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("parent_id = ? AND object_type = ?", parentID, objectType).
		First(&counter).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Should be impossible after the upsert above.
		return 0, fmt.Errorf("alias_counters row missing after upsert: parent=%s type=%s", parentID, objectType)
	}
	if err != nil {
		logger.Error("alias_counters lock failed: parent=%s type=%s err=%v", parentID, objectType, err)
		return 0, fmt.Errorf("alias_counters lock: %w", err)
	}

	allocated := counter.NextAlias

	// Bump the counter atomically (still inside the same transaction & lock).
	if err := tx.WithContext(ctx).
		Model(&models.AliasCounter{}).
		Where("parent_id = ? AND object_type = ?", parentID, objectType).
		Update("next_alias", counter.NextAlias+1).Error; err != nil {
		logger.Error("alias_counters bump failed: parent=%s type=%s err=%v", parentID, objectType, err)
		return 0, fmt.Errorf("alias_counters bump: %w", err)
	}

	return allocated, nil
}
