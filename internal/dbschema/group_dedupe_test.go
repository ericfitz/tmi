package dbschema

import (
	"errors"
	"testing"
	"time"

	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// The structs below mirror the column shape of api/models.Group,
// api/models.GroupMember, and api/models.ThreatModelAccess closely enough to
// exercise DeduplicateGroups in isolation, rather than importing the real
// models here (matching internal/dbschema/audit_append_only_test.go's
// convention). group_dedupe.go itself does import api/models -- unlike
// audit_append_only.go, it needs (&models.X{}).TableName() for every
// .Table() call per scripts/check-oracle-table-names.py -- so this is a
// test-only decoupling choice, not a package-level dependency boundary.

// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: test double mirroring the Group model's schema columns for dedupe tests
type dedupeTestGroup struct {
	InternalUUID string `gorm:"column:internal_uuid;primaryKey"`
	Provider     string `gorm:"column:provider"`
	GroupName    string `gorm:"column:group_name"`
	FirstUsed    time.Time
	LastUsed     time.Time
	UsageCount   int
}

// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: return the groups table name for the dedupe test group double (pure)
func (dedupeTestGroup) TableName() string { return "groups" }

// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: test double mirroring the GroupMember model's schema columns for dedupe tests
type dedupeTestGroupMember struct {
	ID                      string  `gorm:"column:id;primaryKey"`
	GroupInternalUUID       string  `gorm:"column:group_internal_uuid"`
	UserInternalUUID        *string `gorm:"column:user_internal_uuid"`
	MemberGroupInternalUUID *string `gorm:"column:member_group_internal_uuid"`
	SubjectType             string  `gorm:"column:subject_type"`
	AddedAt                 time.Time
}

// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: return the group_members table name for the dedupe test group-member double (pure)
func (dedupeTestGroupMember) TableName() string { return "group_members" }

// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: test double mirroring ThreatModelAccess's group reference column for dedupe tests
type dedupeTestThreatModelAccess struct {
	ID                string  `gorm:"column:id;primaryKey"`
	GroupInternalUUID *string `gorm:"column:group_internal_uuid"`
}

// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: return the threat_model_access table name for the dedupe test access double (pure)
func (dedupeTestThreatModelAccess) TableName() string { return "threat_model_access" }

// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: build an in-memory SQLite database migrated with the group-dedupe test schema
func newDedupeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&dedupeTestGroup{}, &dedupeTestGroupMember{}, &dedupeTestThreatModelAccess{}))
	return db
}

// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: build a pointer to a string value for optional-field test fixtures (pure)
func strPtr(s string) *string { return &s }

// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: validate group dedupe is a no-op when no duplicate groups exist
func TestDeduplicateGroups_NoDuplicates_NoOp(t *testing.T) {
	db := newDedupeTestDB(t)
	require.NoError(t, db.Create(&dedupeTestGroup{
		InternalUUID: uuid.NewString(), Provider: "tmi", GroupName: "Everyone",
		FirstUsed: time.Now(), LastUsed: time.Now(), UsageCount: 1,
	}).Error)

	removed, err := DeduplicateGroups(db)
	require.NoError(t, err)
	assert.Equal(t, int64(0), removed)
}

// TestDeduplicateGroups_KeepsEarliestAndRepointsChildren seeds two groups
// sharing a (provider, group_name) key plus child rows in group_members and
// threat_model_access, runs the dedupe, and asserts: one survivor (the
// earliest by first_used), all child rows repointed to it, a redundant
// group_members row (would collide with idx_gm_group_user_type) dropped
// rather than repointed, and that creating the unique index afterward
// succeeds -- the scenario the real migration is guarding against (#704).
// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: validate group dedupe keeps the earliest group and repoints child rows
func TestDeduplicateGroups_KeepsEarliestAndRepointsChildren(t *testing.T) {
	db := newDedupeTestDB(t)

	base := time.Now().UTC().Truncate(time.Second)
	survivorID := uuid.NewString()
	loserID := uuid.NewString()

	require.NoError(t, db.Create(&dedupeTestGroup{
		InternalUUID: survivorID, Provider: "okta", GroupName: "engineering",
		FirstUsed: base, LastUsed: base, UsageCount: 1,
	}).Error)
	require.NoError(t, db.Create(&dedupeTestGroup{
		InternalUUID: loserID, Provider: "okta", GroupName: "engineering",
		FirstUsed: base.Add(time.Minute), LastUsed: base.Add(time.Minute), UsageCount: 1,
	}).Error)

	// Distinct users: repoint without collision.
	survivorUser := uuid.NewString()
	loserUser := uuid.NewString()
	require.NoError(t, db.Create(&dedupeTestGroupMember{
		ID: uuid.NewString(), GroupInternalUUID: survivorID, UserInternalUUID: strPtr(survivorUser),
		SubjectType: "user", AddedAt: base,
	}).Error)
	require.NoError(t, db.Create(&dedupeTestGroupMember{
		ID: uuid.NewString(), GroupInternalUUID: loserID, UserInternalUUID: strPtr(loserUser),
		SubjectType: "user", AddedAt: base,
	}).Error)

	// Colliding user: both the survivor and the loser have a membership row
	// for the same user -- repointing the loser's row onto the survivor would
	// violate idx_gm_group_user_type, so it must be dropped instead.
	collidingUser := uuid.NewString()
	require.NoError(t, db.Create(&dedupeTestGroupMember{
		ID: uuid.NewString(), GroupInternalUUID: survivorID, UserInternalUUID: strPtr(collidingUser),
		SubjectType: "user", AddedAt: base,
	}).Error)
	loserCollidingRowID := uuid.NewString()
	require.NoError(t, db.Create(&dedupeTestGroupMember{
		ID: loserCollidingRowID, GroupInternalUUID: loserID, UserInternalUUID: strPtr(collidingUser),
		SubjectType: "user", AddedAt: base.Add(time.Second),
	}).Error)

	// threat_model_access referencing the loser must be repointed too.
	tmaID := uuid.NewString()
	require.NoError(t, db.Create(&dedupeTestThreatModelAccess{
		ID: tmaID, GroupInternalUUID: strPtr(loserID),
	}).Error)

	removed, err := DeduplicateGroups(db)
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed)

	var remaining []dedupeTestGroup
	require.NoError(t, db.Where("provider = ? AND group_name = ?", "okta", "engineering").Find(&remaining).Error)
	require.Len(t, remaining, 1)
	assert.Equal(t, survivorID, remaining[0].InternalUUID)

	var members []dedupeTestGroupMember
	require.NoError(t, db.Find(&members).Error)
	require.Len(t, members, 3, "the distinct-user rows survive; the colliding loser row is dropped")
	for _, m := range members {
		assert.Equal(t, survivorID, m.GroupInternalUUID, "every surviving membership must point at the survivor")
	}

	var droppedCount int64
	require.NoError(t, db.Model(&dedupeTestGroupMember{}).Where("id = ?", loserCollidingRowID).Count(&droppedCount).Error)
	assert.Equal(t, int64(0), droppedCount, "the redundant colliding membership must be deleted, not repointed")

	var tma dedupeTestThreatModelAccess
	require.NoError(t, db.Where("id = ?", tmaID).First(&tma).Error)
	require.NotNil(t, tma.GroupInternalUUID)
	assert.Equal(t, survivorID, *tma.GroupInternalUUID)

	// The scenario this dedupe exists for: AutoMigrate's CREATE UNIQUE INDEX
	// must now succeed against the deduped table.
	require.NoError(t, db.Exec("CREATE UNIQUE INDEX uniq_groups_provider_group_name ON groups(provider, group_name)").Error)
}

// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: validate a second group-dedupe pass removes nothing further
func TestDeduplicateGroups_Idempotent(t *testing.T) {
	db := newDedupeTestDB(t)
	base := time.Now().UTC()
	require.NoError(t, db.Create(&dedupeTestGroup{
		InternalUUID: uuid.NewString(), Provider: "tmi", GroupName: "dup",
		FirstUsed: base, LastUsed: base, UsageCount: 1,
	}).Error)
	require.NoError(t, db.Create(&dedupeTestGroup{
		InternalUUID: uuid.NewString(), Provider: "tmi", GroupName: "dup",
		FirstUsed: base.Add(time.Minute), LastUsed: base.Add(time.Minute), UsageCount: 1,
	}).Error)

	removed1, err := DeduplicateGroups(db)
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed1)

	removed2, err := DeduplicateGroups(db)
	require.NoError(t, err)
	assert.Equal(t, int64(0), removed2, "a second pass must be a no-op")
}

// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: validate group dedupe is a no-op when the groups table is absent
func TestDeduplicateGroups_NoGroupsTable_NoOp(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	removed, err := DeduplicateGroups(db)
	require.NoError(t, err)
	assert.Equal(t, int64(0), removed)
}

// TestDeduplicateGroups_DropsSelfMembership covers the case where one
// duplicate group was nested as a subgroup member of the other before the
// dedupe ran: after repointing both onto the survivor, a naive repoint would
// leave a group_members row where the survivor is a member of itself. That
// row bypasses GroupMember's BeforeSave hooks (this dedupe writes via
// .Table(), not the model) and must be dropped explicitly.
// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: validate group dedupe drops a membership row left pointing a group at itself
func TestDeduplicateGroups_DropsSelfMembership(t *testing.T) {
	db := newDedupeTestDB(t)

	base := time.Now().UTC().Truncate(time.Second)
	survivorID := uuid.NewString()
	loserID := uuid.NewString()

	require.NoError(t, db.Create(&dedupeTestGroup{
		InternalUUID: survivorID, Provider: "okta", GroupName: "engineering",
		FirstUsed: base, LastUsed: base, UsageCount: 1,
	}).Error)
	require.NoError(t, db.Create(&dedupeTestGroup{
		InternalUUID: loserID, Provider: "okta", GroupName: "engineering",
		FirstUsed: base.Add(time.Minute), LastUsed: base.Add(time.Minute), UsageCount: 1,
	}).Error)

	// The loser was nested as a subgroup member of the survivor before
	// dedupe: repointing member_group_internal_uuid (loser -> survivor)
	// would otherwise leave survivor a member of itself.
	require.NoError(t, db.Create(&dedupeTestGroupMember{
		ID: uuid.NewString(), GroupInternalUUID: survivorID, SubjectType: "group",
		MemberGroupInternalUUID: strPtr(loserID), AddedAt: base,
	}).Error)

	removed, err := DeduplicateGroups(db)
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed)

	var selfCount int64
	require.NoError(t, db.Model(&dedupeTestGroupMember{}).
		Where("group_internal_uuid = ? AND member_group_internal_uuid = ?", survivorID, survivorID).
		Count(&selfCount).Error)
	assert.Equal(t, int64(0), selfCount, "the survivor must not end up as its own member")
}

// TestDeduplicateGroups_CollapsesDuplicateSubgroupMembershipPair covers a
// different nesting scenario than self-membership: a THIRD, unrelated parent
// group already listed both duplicates as separate subgroup members (two
// rows, same parent, one per duplicate). Repointing member_group_internal_uuid
// alone would leave the parent listing the survivor twice -- not caught by
// idx_gm_group_user_type since user_internal_uuid is NULL on both rows.
// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: validate group dedupe collapses a duplicate parent-subgroup membership pair
func TestDeduplicateGroups_CollapsesDuplicateSubgroupMembershipPair(t *testing.T) {
	db := newDedupeTestDB(t)

	base := time.Now().UTC().Truncate(time.Second)
	survivorID := uuid.NewString()
	loserID := uuid.NewString()
	parentID := uuid.NewString()

	require.NoError(t, db.Create(&dedupeTestGroup{
		InternalUUID: survivorID, Provider: "okta", GroupName: "engineering",
		FirstUsed: base, LastUsed: base, UsageCount: 1,
	}).Error)
	require.NoError(t, db.Create(&dedupeTestGroup{
		InternalUUID: loserID, Provider: "okta", GroupName: "engineering",
		FirstUsed: base.Add(time.Minute), LastUsed: base.Add(time.Minute), UsageCount: 1,
	}).Error)
	require.NoError(t, db.Create(&dedupeTestGroup{
		InternalUUID: parentID, Provider: "okta", GroupName: "all-engineering-parents",
		FirstUsed: base, LastUsed: base, UsageCount: 1,
	}).Error)

	// parent already lists the survivor as a subgroup member...
	keepRowID := uuid.NewString()
	require.NoError(t, db.Create(&dedupeTestGroupMember{
		ID: keepRowID, GroupInternalUUID: parentID, SubjectType: "group",
		MemberGroupInternalUUID: strPtr(survivorID), AddedAt: base,
	}).Error)
	// ...and separately lists the loser too, before dedupe merges them.
	require.NoError(t, db.Create(&dedupeTestGroupMember{
		ID: uuid.NewString(), GroupInternalUUID: parentID, SubjectType: "group",
		MemberGroupInternalUUID: strPtr(loserID), AddedAt: base.Add(time.Second),
	}).Error)

	removed, err := DeduplicateGroups(db)
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed)

	var pairCount int64
	require.NoError(t, db.Model(&dedupeTestGroupMember{}).
		Where("group_internal_uuid = ? AND member_group_internal_uuid = ? AND subject_type = ?", parentID, survivorID, "group").
		Count(&pairCount).Error)
	assert.Equal(t, int64(1), pairCount, "the parent must list the survivor exactly once, not twice")

	// The earlier row (by added_at) is the one that must survive.
	var remaining dedupeTestGroupMember
	require.NoError(t, db.Where("group_internal_uuid = ? AND member_group_internal_uuid = ?", parentID, survivorID).First(&remaining).Error)
	assert.Equal(t, keepRowID, remaining.ID, "the earliest row must be the survivor")
}

// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: validate splitting a string slice into size-capped chunks, preserving order
func TestChunkStrings(t *testing.T) {
	t.Run("under the limit returns a single chunk", func(t *testing.T) {
		ids := []string{"a", "b", "c"}
		chunks := chunkStrings(ids)
		require.Len(t, chunks, 1)
		assert.Equal(t, ids, chunks[0])
	})

	t.Run("exactly at the limit returns a single chunk", func(t *testing.T) {
		ids := make([]string, maxInListSize)
		chunks := chunkStrings(ids)
		require.Len(t, chunks, 1)
		assert.Len(t, chunks[0], maxInListSize)
	})

	t.Run("over the limit splits into multiple chunks, preserving order and count", func(t *testing.T) {
		total := maxInListSize + 1
		ids := make([]string, total)
		for i := range ids {
			ids[i] = uuid.NewString()
		}
		chunks := chunkStrings(ids)
		require.Len(t, chunks, 2)
		assert.Len(t, chunks[0], maxInListSize)
		assert.Len(t, chunks[1], 1)

		var reassembled []string
		for _, c := range chunks {
			reassembled = append(reassembled, c...)
		}
		assert.Equal(t, ids, reassembled)
	})

	t.Run("empty input returns a single empty chunk", func(t *testing.T) {
		chunks := chunkStrings(nil)
		require.Len(t, chunks, 1)
		assert.Empty(t, chunks[0])
	})
}

// TestWithMigrationRetry_RetryableError_RetriedThenSucceeds covers #712:
// dberrors.ErrTransient (Oracle ORA-00060 deadlock / ORA-08177 serialization
// failure territory) must be retried, not propagated on the first failure.
// SEM@bb40881560ec43c848a818a906635c7d26b0b603: validate a migration retry helper retries a transient error until success
func TestWithMigrationRetry_RetryableError_RetriedThenSucceeds(t *testing.T) {
	var calls int
	err := withMigrationRetry("groups dedupe engineering@okta", func() error {
		calls++
		if calls < dedupeMaxAttempts {
			return dberrors.Wrap(errors.New("ORA-00060: deadlock detected"), dberrors.ErrTransient)
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, dedupeMaxAttempts, calls, "must retry until success, not stop early")
}

// TestWithMigrationRetry_RetryableError_ExhaustsAttempts covers the case
// where every attempt fails: withMigrationRetry must give up after
// dedupeMaxAttempts rather than retrying forever, and return the last error.
// SEM@bb40881560ec43c848a818a906635c7d26b0b603: validate a migration retry helper gives up after its max attempts
func TestWithMigrationRetry_RetryableError_ExhaustsAttempts(t *testing.T) {
	var calls int
	sentinel := dberrors.Wrap(errors.New("ORA-08177: serialization failure"), dberrors.ErrTransient)
	err := withMigrationRetry("groups dedupe engineering@okta", func() error {
		calls++
		return sentinel
	})
	require.ErrorIs(t, err, dberrors.ErrTransient)
	assert.Equal(t, dedupeMaxAttempts, calls, "must stop after dedupeMaxAttempts")
}

// TestWithMigrationRetry_NonRetryableError_FailsImmediately covers #712's
// other requirement: a non-retryable error (e.g. a genuine constraint
// violation) must propagate on the first attempt, unretried and unchanged.
// SEM@bb40881560ec43c848a818a906635c7d26b0b603: validate a migration retry helper propagates a non-retryable error unretried
func TestWithMigrationRetry_NonRetryableError_FailsImmediately(t *testing.T) {
	var calls int
	sentinel := dberrors.Wrap(errors.New("unique constraint violated"), dberrors.ErrDuplicate)
	err := withMigrationRetry("groups dedupe engineering@okta", func() error {
		calls++
		return sentinel
	})
	require.ErrorIs(t, err, dberrors.ErrDuplicate)
	assert.Same(t, sentinel, err, "non-retryable error must propagate unchanged")
	assert.Equal(t, 1, calls, "must not retry a non-retryable error")
}
