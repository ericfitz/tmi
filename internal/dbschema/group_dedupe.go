// Package dbschema: pre-migration dedupe of groups(provider, group_name)
// (#704).
//
// ensureGroupExists (api/database_store_gorm.go) and UpsertGroup
// (api/group_repository.go) both upsert a group keyed on
// (provider, group_name) via an ON CONFLICT / MERGE clause, but until #704
// that pair was backed only by two separate non-unique indexes
// (idx_groups_provider, idx_groups_group_name) — the conflict target itself
// was unbacked. On Oracle, MERGE INTO does not require a unique index on its
// match condition, so two concurrent first-time upserts for the same
// (provider, group_name) could each fall through to their INSERT branch and
// silently create duplicate group rows. api/models/models.go now backs the
// pair with uniq_groups_provider_group_name, but a database that already
// carries such duplicates would fail AutoMigrate's CREATE UNIQUE INDEX
// (ORA-01452 / PostgreSQL 23505). DeduplicateGroups runs ahead of
// AutoMigrate to clear any existing duplicates first.
package dbschema

import (
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// dedupeMaxAttempts bounds how many times dedupeGroupKey's transaction is
// retried after a transient error (Oracle ORA-00060 deadlock, ORA-08177
// serialization failure — see #712) before giving up. DeduplicateGroups runs
// during startup schema migration while holding the cross-replica advisory
// lock (cmd/server/main.go runMigrationsLocked), so every retry here delays
// every other replica waiting on that lock: keep the count and backoff small
// and fixed rather than configurable.
const dedupeMaxAttempts = 3

// dedupeRetryDelay is the fixed backoff between retry attempts. Deliberately
// short (see dedupeMaxAttempts) — this is not a general-purpose transaction
// retry policy, just enough to let a concurrent replica's conflicting
// transaction clear before trying again.
const dedupeRetryDelay = 20 * time.Millisecond

// groupDupKey is a raw scan target for the duplicate-key query. Deliberately
// untagged (see cmd/dedup-group-members/main.go): result-set labels come back
// UPPERCASE from Oracle and lowercase from Postgres, and an untagged field's
// DBName is derived from the active dialect's NamingStrategy, so the same
// struct matches both. The "cnt" alias is unquoted so it folds identically.
// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: hold a duplicate groups(provider, group_name) key and its row count (pure)
type groupDupKey struct {
	Provider  string
	GroupName string
	Cnt       int64
}

// groupIDRow is a raw scan target for a group row's identity/ordering columns.
// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: hold a group's identity and ordering columns for dedupe scans (pure)
type groupIDRow struct {
	InternalUUID string
	FirstUsed    time.Time
}

// groupMemberRow is a raw scan target for the group_members columns dedupe
// needs. UserInternalUUID is a pointer because the column is nullable
// (subject_type = "group" rows carry no user).
// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: hold group_members identity, user, and subject-type columns for dedupe scans (pure)
type groupMemberRow struct {
	ID               string
	UserInternalUUID *string
	SubjectType      string
}

// maxInListSize caps a single SQL IN (...) list so it stays under Oracle's
// 1000-expression limit (ORA-01795). In practice a duplicate-key's loser set
// is small (the first successful create for a (provider, group_name) stops
// any further duplicate creates via resolveGroupUUID's fast path), but
// chunking removes the failure mode entirely rather than relying on that.
const maxInListSize = 500

// chunkStrings splits ids into batches of at most maxInListSize, preserving
// order. Returns a single-element slice (containing ids itself) when no
// chunking is needed.
// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: split a string slice into fixed-size batches for SQL IN-list safety (pure)
func chunkStrings(ids []string) [][]string {
	if len(ids) <= maxInListSize {
		return [][]string{ids}
	}
	chunks := make([][]string, 0, (len(ids)+maxInListSize-1)/maxInListSize)
	for i := 0; i < len(ids); i += maxInListSize {
		end := i + maxInListSize
		if end > len(ids) {
			end = len(ids)
		}
		chunks = append(chunks, ids[i:end])
	}
	return chunks
}

// membershipKey identifies a group_members row within the scope of
// idx_gm_group_user_type (group_internal_uuid, user_internal_uuid,
// subject_type) for a single group_internal_uuid. Rows with a NULL
// user_internal_uuid (subject_type = "group") never populate this map: SQL
// unique indexes never treat two NULLs as equal, so those rows can't collide.
// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: identify a group_members row by user and subject type within a group (pure)
type membershipKey struct {
	userInternalUUID string
	subjectType      string
}

// DeduplicateGroups finds groups(provider, group_name) keys with more than
// one row, keeps the earliest row per key (by first_used, tie-broken by
// internal_uuid for determinism), repoints referencing rows in
// group_members, threat_model_access, and survey_response_access from the
// removed duplicates onto the survivor, and deletes the duplicates. Each key
// is resolved in its own transaction. It is idempotent — a concurrent
// replica that already resolved a key leaves fewer than two rows for it, and
// the key is skipped — and cheap (one query) when there are no duplicates,
// so it is safe to run on every boot immediately before AutoMigrate creates
// uniq_groups_provider_group_name.
// SEM@bb40881560ec43c848a818a906635c7d26b0b603: dedupe groups(provider, group_name) and repoint child rows before the unique index is created (writes DB)
func DeduplicateGroups(db *gorm.DB) (int64, error) {
	logger := slogging.Get()

	groupsTable := (&models.Group{}).TableName()
	if !db.Migrator().HasTable(groupsTable) {
		return 0, nil
	}

	var dups []groupDupKey
	err := withMigrationRetry("groups duplicate-key discovery", func() error {
		dups = dups[:0]
		return db.Table(groupsTable).
			Select("provider, group_name, COUNT(*) AS cnt").
			Group("provider, group_name").
			Having("COUNT(*) > 1").
			Scan(&dups).Error
	})
	if err != nil {
		return 0, fmt.Errorf("failed to find duplicate groups: %w", err)
	}
	if len(dups) == 0 {
		return 0, nil
	}

	var totalRemoved int64
	for _, dup := range dups {
		removed, err := dedupeGroupKey(db, dup.Provider, dup.GroupName)
		if err != nil {
			return totalRemoved, fmt.Errorf("failed to dedupe group %q@%q: %w", dup.GroupName, dup.Provider, err)
		}
		totalRemoved += removed
		if removed > 0 {
			logger.Info("groups dedupe: provider=%s group_name=%s removed=%d duplicate rows", dup.Provider, dup.GroupName, removed)
		}
	}
	return totalRemoved, nil
}

// dedupeGroupKey resolves one duplicate (provider, group_name) key inside a
// single transaction: pick the survivor, repoint children, delete losers.
// The transaction is wrapped in withMigrationRetry so a transient
// cross-replica conflict (#712) is retried a bounded number of times instead
// of aborting the whole migration.
// SEM@bb40881560ec43c848a818a906635c7d26b0b603: resolve one duplicate groups key by repointing children and deleting the losers, retrying on transient errors (writes DB)
func dedupeGroupKey(db *gorm.DB, provider, groupName string) (int64, error) {
	groupsTable := (&models.Group{}).TableName()
	tmaTable := (&models.ThreatModelAccess{}).TableName()
	sraTable := (&models.SurveyResponseAccess{}).TableName()

	var removed int64
	label := fmt.Sprintf("groups dedupe %s@%s", groupName, provider)
	err := withMigrationRetry(label, func() error {
		removed = 0
		return db.Transaction(func(tx *gorm.DB) error {
			var rows []groupIDRow
			if err := tx.Table(groupsTable).
				Select("internal_uuid, first_used").
				Where("provider = ? AND group_name = ?", provider, groupName).
				Order("first_used ASC, internal_uuid ASC").
				Scan(&rows).Error; err != nil {
				// Deliberately avoid the word "duplicate" in every error wrapped
				// inside this retried transaction: without the oracle build tag,
				// dberrors.Classify falls back to classifyByString, which matches
				// "duplicate" -> ErrDuplicate (non-retryable) before any other
				// pattern, which would silently defeat withDedupeRetry's transient
				// retry (1.8.3 oracle-db-admin review).
				return fmt.Errorf("failed to load redundant group rows: %w", err)
			}
			if len(rows) < 2 {
				// A concurrent replica already resolved this key.
				return nil
			}

			survivor := rows[0].InternalUUID
			losers := make([]string, 0, len(rows)-1)
			for _, r := range rows[1:] {
				losers = append(losers, r.InternalUUID)
			}

			if err := repointGroupMembers(tx, survivor, losers); err != nil {
				return err
			}
			// A loser nested under the survivor (or vice versa) before this
			// dedupe ran can, after the repoint above, leave a group_members row
			// where the survivor is a member of itself. This bypasses
			// GroupMember's BeforeSave hooks (writes here go through .Table(),
			// not the model), so clean it up explicitly.
			if err := dropSelfMembership(tx, survivor); err != nil {
				return err
			}
			if err := repointGroupReference(tx, tmaTable, survivor, losers); err != nil {
				return err
			}
			if err := repointGroupReference(tx, sraTable, survivor, losers); err != nil {
				return err
			}

			var deleted int64
			for _, chunk := range chunkStrings(losers) {
				result := tx.Table(groupsTable).Where("internal_uuid IN ?", chunk).Delete(nil)
				if result.Error != nil {
					return fmt.Errorf("failed to delete redundant group rows: %w", result.Error)
				}
				deleted += result.RowsAffected
			}
			removed = deleted
			return nil
		})
	})
	return removed, err
}

// withMigrationRetry runs fn (a single pre-AutoMigrate schema-check or
// dedupe attempt) up to dedupeMaxAttempts times. It retries only when the
// returned error classifies as dberrors.IsRetryable (Oracle
// deadlock/serialization failures, connection blips); any other error —
// including dberrors.ErrDuplicate, which is never retryable — is returned to
// the caller unchanged on the first attempt. label identifies the step in
// the retry log line only. Generalized from the original groups-only
// withDedupeRetry (#712) to also cover the duplicate-key discovery queries
// (#725 item e) and the users duplicate-identity check (#724).
// SEM@bb40881560ec43c848a818a906635c7d26b0b603: retry a migration-phase attempt a bounded number of times on transient errors only (pure control flow)
func withMigrationRetry(label string, fn func() error) error {
	var err error
	for attempt := 1; attempt <= dedupeMaxAttempts; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		if !dberrors.IsRetryable(dberrors.Classify(err)) || attempt == dedupeMaxAttempts {
			return err
		}
		slogging.Get().Warn("migration step %q: transient error (attempt %d/%d), retrying: %v",
			label, attempt, dedupeMaxAttempts, err)
		time.Sleep(dedupeRetryDelay)
	}
	return err
}

// repointGroupReference repoints a nullable group_internal_uuid column that
// carries no uniqueness constraint of its own onto the survivor. A missing
// table (a database mid-upgrade may not have created every referencing table
// yet) is skipped rather than treated as an error. table must already be the
// dialect-correct name (a model's TableName(), not a bare literal — see
// scripts/check-oracle-table-names.py / #504).
// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: repoint a nullable group_internal_uuid FK column from losing groups to the survivor (writes DB)
func repointGroupReference(tx *gorm.DB, table, survivor string, losers []string) error {
	if !tx.Migrator().HasTable(table) {
		return nil
	}
	for _, chunk := range chunkStrings(losers) {
		if err := tx.Table(table).
			Where("group_internal_uuid IN ?", chunk).
			Update("group_internal_uuid", survivor).Error; err != nil {
			return fmt.Errorf("failed to repoint %s.group_internal_uuid: %w", table, err)
		}
	}
	return nil
}

// dropSelfMembership removes a group_members row left pointing a group at
// itself (group_internal_uuid == member_group_internal_uuid == survivor).
// See the comment at its call site in dedupeGroupKey for how this arises.
// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: delete a group_members row where the survivor group is listed as its own member (writes DB)
func dropSelfMembership(tx *gorm.DB, survivor string) error {
	groupMembersTable := (&models.GroupMember{}).TableName()
	if !tx.Migrator().HasTable(groupMembersTable) {
		return nil
	}
	if err := tx.Table(groupMembersTable).
		Where("group_internal_uuid = ? AND member_group_internal_uuid = ?", survivor, survivor).
		Delete(nil).Error; err != nil {
		return fmt.Errorf("failed to drop self-referential group_members row for %s: %w", survivor, err)
	}
	return nil
}

// memberGroupPairDupKey is a raw scan target for the duplicate
// (group_internal_uuid, member_group_internal_uuid) pair query.
// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: hold a duplicate parent-group subgroup-membership key and its row count (pure)
type memberGroupPairDupKey struct {
	GroupInternalUUID string
	Cnt               int64
}

// dedupeMemberGroupPairs collapses group_members rows that became duplicate
// (group_internal_uuid, member_group_internal_uuid = survivor) pairs after
// repointing member_group_internal_uuid onto the survivor, keeping the
// earliest row (by added_at) per parent group and deleting the rest. See the
// comment at its call site in repointGroupMembers for how this arises.
// SEM@8ea37221e3186b49d52e78d8834a4e6dd35d2b93: collapse duplicate parent-group subgroup-membership rows left by a member repoint (writes DB)
func dedupeMemberGroupPairs(tx *gorm.DB, groupMembersTable, survivor string) error {
	var dups []memberGroupPairDupKey
	if err := tx.Table(groupMembersTable).
		Select("group_internal_uuid, COUNT(*) AS cnt").
		Where("member_group_internal_uuid = ? AND subject_type = ?", survivor, "group").
		Group("group_internal_uuid").
		Having("COUNT(*) > 1").
		Scan(&dups).Error; err != nil {
		return fmt.Errorf("failed to find redundant subgroup-membership pairs for %s: %w", survivor, err)
	}

	for _, dup := range dups {
		var keepID string
		if err := tx.Table(groupMembersTable).
			Select("id").
			Where("group_internal_uuid = ? AND member_group_internal_uuid = ? AND subject_type = ?",
				dup.GroupInternalUUID, survivor, "group").
			Order("added_at ASC").
			Limit(1).
			Scan(&keepID).Error; err != nil {
			return fmt.Errorf("failed to find earliest subgroup-membership row for parent %s: %w", dup.GroupInternalUUID, err)
		}
		if err := tx.Table(groupMembersTable).
			Where("group_internal_uuid = ? AND member_group_internal_uuid = ? AND subject_type = ? AND id <> ?",
				dup.GroupInternalUUID, survivor, "group", keepID).
			Delete(nil).Error; err != nil {
			return fmt.Errorf("failed to drop redundant subgroup-membership rows for parent %s: %w", dup.GroupInternalUUID, err)
		}
	}
	return nil
}

// repointGroupMembers repoints group_members rows from the losing group ids
// onto the survivor. member_group_internal_uuid (subgroup nesting) carries no
// uniqueness constraint and is repointed unconditionally. group_internal_uuid
// (the owning group) is part of idx_gm_group_user_type
// (group_internal_uuid, user_internal_uuid, subject_type); repointing a
// loser's row onto the survivor can collide with a row the survivor (or an
// already-repointed loser) already has for the same
// (user_internal_uuid, subject_type), so those rows are resolved one at a
// time: repoint if the key is free, drop the now-redundant membership
// otherwise. Rows with a NULL user_internal_uuid (subject_type = "group")
// never collide — SQL unique indexes never treat two NULLs as equal — so
// they are always repointed unconditionally onto the survivor's owning side.
// That unconditional repoint has its own collision case: two duplicate
// groups (or a loser and the survivor) can each have already listed the same
// child subgroup as a member, which leaves the survivor listing that child
// twice once both owning-side rows land on it. dedupeOwnedSubgroupRows
// collapses those at the end, mirroring dedupeMemberGroupPairs above (#715).
// SEM@0000000000000000000000000000000000000000: repoint group_members rows from losing groups to the survivor, dropping colliding rows (writes DB)
func repointGroupMembers(tx *gorm.DB, survivor string, losers []string) error {
	groupMembersTable := (&models.GroupMember{}).TableName()
	if !tx.Migrator().HasTable(groupMembersTable) {
		return nil
	}

	for _, chunk := range chunkStrings(losers) {
		if err := tx.Table(groupMembersTable).
			Where("member_group_internal_uuid IN ?", chunk).
			Update("member_group_internal_uuid", survivor).Error; err != nil {
			return fmt.Errorf("failed to repoint group_members.member_group_internal_uuid: %w", err)
		}
	}
	// A parent group that already listed two now-merged duplicates as
	// separate subgroup members (two rows, each subject_type = "group", one
	// per duplicate) ends up, after the repoint above, listing the survivor
	// twice under the same parent. idx_gm_group_user_type doesn't catch this
	// (user_internal_uuid is NULL on both rows, and NULLs never collide), so
	// it's collapsed explicitly.
	if err := dedupeMemberGroupPairs(tx, groupMembersTable, survivor); err != nil {
		return err
	}

	existing, err := loadMembershipKeys(tx, survivor)
	if err != nil {
		return err
	}

	// Order is preserved within each chunk (added_at ASC) but not globally
	// across chunk boundaries; chunking only engages far beyond any
	// realistic loser count (see maxInListSize), where exact ordering no
	// longer matters for the collision resolution below.
	var loserRows []groupMemberRow
	for _, chunk := range chunkStrings(losers) {
		var batch []groupMemberRow
		if err := tx.Table(groupMembersTable).
			Select("id, user_internal_uuid, subject_type").
			Where("group_internal_uuid IN ?", chunk).
			Order("added_at ASC").
			Scan(&batch).Error; err != nil {
			return fmt.Errorf("failed to load group_members rows for redundant groups: %w", err)
		}
		loserRows = append(loserRows, batch...)
	}

	for _, row := range loserRows {
		if row.UserInternalUUID == nil {
			if err := tx.Table(groupMembersTable).Where("id = ?", row.ID).
				Update("group_internal_uuid", survivor).Error; err != nil {
				return fmt.Errorf("failed to repoint group_members row %s: %w", row.ID, err)
			}
			continue
		}

		key := membershipKey{userInternalUUID: *row.UserInternalUUID, subjectType: row.SubjectType}
		if existing[key] {
			if err := tx.Table(groupMembersTable).Where("id = ?", row.ID).
				Delete(nil).Error; err != nil {
				return fmt.Errorf("failed to drop redundant group_members row %s: %w", row.ID, err)
			}
			continue
		}
		if err := tx.Table(groupMembersTable).Where("id = ?", row.ID).
			Update("group_internal_uuid", survivor).Error; err != nil {
			return fmt.Errorf("failed to repoint group_members row %s: %w", row.ID, err)
		}
		existing[key] = true
	}

	return dedupeOwnedSubgroupRows(tx, groupMembersTable, survivor)
}

// ownedSubgroupDupKey is a raw scan target for the duplicate
// member_group_internal_uuid key query under one owning survivor group.
// SEM@0000000000000000000000000000000000000000: hold a duplicate owned-subgroup membership key and its row count (pure)
type ownedSubgroupDupKey struct {
	MemberGroupInternalUUID string
	Cnt                     int64
}

// dedupeOwnedSubgroupRows collapses group_members rows that became duplicate
// (group_internal_uuid = survivor, member_group_internal_uuid) pairs after
// the unconditional owning-side repoint in repointGroupMembers — the
// owning-side mirror of dedupeMemberGroupPairs. Two duplicate groups can each
// have already listed the same child subgroup M as a member (one row each,
// subject_type = "group"); once both owning-side rows land on the survivor,
// it lists M twice. idx_gm_group_user_type doesn't catch this
// (user_internal_uuid is NULL on both rows, and NULLs never collide), so it's
// collapsed explicitly, keeping the earliest row (by added_at) per member and
// deleting the rest (#715). member_group_internal_uuid is nullable (unlike
// dedupeMemberGroupPairs's grouping column), so the query excludes NULL
// explicitly rather than relying on it dropping out incidentally: an empty
// string IS NULL on Oracle but not on PostgreSQL (oracle-db-admin review), so
// a NULL bucket left in would collapse inconsistently across dialects
// instead of just being skipped on both.
// SEM@0000000000000000000000000000000000000000: collapse duplicate owned-subgroup membership rows left by an owning-side repoint (writes DB)
func dedupeOwnedSubgroupRows(tx *gorm.DB, groupMembersTable, survivor string) error {
	var dups []ownedSubgroupDupKey
	if err := tx.Table(groupMembersTable).
		Select("member_group_internal_uuid, COUNT(*) AS cnt").
		Where("group_internal_uuid = ? AND subject_type = ? AND member_group_internal_uuid IS NOT NULL", survivor, "group").
		Group("member_group_internal_uuid").
		Having("COUNT(*) > 1").
		Scan(&dups).Error; err != nil {
		return fmt.Errorf("failed to find redundant owned-subgroup membership rows for %s: %w", survivor, err)
	}

	for _, dup := range dups {
		var keepID string
		if err := tx.Table(groupMembersTable).
			Select("id").
			Where("group_internal_uuid = ? AND member_group_internal_uuid = ? AND subject_type = ?",
				survivor, dup.MemberGroupInternalUUID, "group").
			Order("added_at ASC").
			Limit(1).
			Scan(&keepID).Error; err != nil {
			return fmt.Errorf("failed to find earliest owned-subgroup membership row for member %s: %w", dup.MemberGroupInternalUUID, err)
		}
		if keepID == "" {
			// Defensive: the IS NOT NULL guard above should make this
			// unreachable, but an empty keepID must never reach the delete
			// below unguarded — "id <> ''" is UNKNOWN (deletes nothing) on
			// Oracle but true for every row (deletes everything in the
			// group) on PostgreSQL (oracle-db-admin review).
			continue
		}
		if err := tx.Table(groupMembersTable).
			Where("group_internal_uuid = ? AND member_group_internal_uuid = ? AND subject_type = ? AND id <> ?",
				survivor, dup.MemberGroupInternalUUID, "group", keepID).
			Delete(nil).Error; err != nil {
			return fmt.Errorf("failed to drop redundant owned-subgroup membership rows for member %s: %w", dup.MemberGroupInternalUUID, err)
		}
	}
	return nil
}

// loadMembershipKeys returns the (user_internal_uuid, subject_type) keys
// already present under groupInternalUUID, restricted to rows with a
// non-NULL user_internal_uuid (see repointGroupMembers).
// SEM@8dfef8f6c12df5ee0b3e4e320e4cb780a50506b0: load existing group_members (user, subject_type) keys for a group (reads DB)
func loadMembershipKeys(tx *gorm.DB, groupInternalUUID string) (map[membershipKey]bool, error) {
	groupMembersTable := (&models.GroupMember{}).TableName()
	var rows []groupMemberRow
	if err := tx.Table(groupMembersTable).
		Select("id, user_internal_uuid, subject_type").
		Where("group_internal_uuid = ?", groupInternalUUID).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to load existing group_members for %s: %w", groupInternalUUID, err)
	}
	keys := make(map[membershipKey]bool, len(rows))
	for _, r := range rows {
		if r.UserInternalUUID == nil {
			continue
		}
		keys[membershipKey{userInternalUUID: *r.UserInternalUUID, subjectType: r.SubjectType}] = true
	}
	return keys, nil
}
