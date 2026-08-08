package dbschema

import (
	"testing"
	"time"

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

type dedupeTestGroup struct {
	InternalUUID string `gorm:"column:internal_uuid;primaryKey"`
	Provider     string `gorm:"column:provider"`
	GroupName    string `gorm:"column:group_name"`
	FirstUsed    time.Time
	LastUsed     time.Time
	UsageCount   int
}

func (dedupeTestGroup) TableName() string { return "groups" }

type dedupeTestGroupMember struct {
	ID                      string  `gorm:"column:id;primaryKey"`
	GroupInternalUUID       string  `gorm:"column:group_internal_uuid"`
	UserInternalUUID        *string `gorm:"column:user_internal_uuid"`
	MemberGroupInternalUUID *string `gorm:"column:member_group_internal_uuid"`
	SubjectType             string  `gorm:"column:subject_type"`
	AddedAt                 time.Time
}

func (dedupeTestGroupMember) TableName() string { return "group_members" }

type dedupeTestThreatModelAccess struct {
	ID                string  `gorm:"column:id;primaryKey"`
	GroupInternalUUID *string `gorm:"column:group_internal_uuid"`
}

func (dedupeTestThreatModelAccess) TableName() string { return "threat_model_access" }

func newDedupeTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&dedupeTestGroup{}, &dedupeTestGroupMember{}, &dedupeTestThreatModelAccess{}))
	return db
}

func strPtr(s string) *string { return &s }

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
