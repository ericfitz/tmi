package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"gorm.io/gorm"
)

// GormGroupMemberStore implements group membership operations using GORM
type GormGroupMemberStore struct {
	db *gorm.DB
}

// NewGormGroupMemberStore creates a new GORM-backed group member store
func NewGormGroupMemberStore(db *gorm.DB) *GormGroupMemberStore {
	return &GormGroupMemberStore{db: db}
}

// ListMembers returns all members of a group with pagination
func (s *GormGroupMemberStore) ListMembers(ctx context.Context, filter GroupMemberFilter) ([]GroupMember, error) {
	logger := slogging.Get()

	// Use a raw query with joins to get user and group member details
	type memberRow struct {
		ID                      string
		GroupInternalUUID       string
		UserInternalUUID        *string
		MemberGroupInternalUUID *string
		SubjectType             string
		UserEmail               *string
		UserName                *string
		UserProvider            *string
		UserProviderUserID      *string
		MemberGroupName         *string
		MemberGroupProvider     *string
		AddedByInternalUUID     *string
		AddedByEmail            *string
		AddedAt                 time.Time
		Notes                   *string
	}

	query := s.db.WithContext(ctx).Table("group_members gm").
		Select(`
			gm.id,
			gm.group_internal_uuid,
			gm.user_internal_uuid,
			gm.member_group_internal_uuid,
			gm.subject_type,
			u.email as user_email,
			u.name as user_name,
			u.provider as user_provider,
			u.provider_user_id as user_provider_user_id,
			mg.group_name as member_group_name,
			mg.provider as member_group_provider,
			gm.added_by_internal_uuid,
			adder.email as added_by_email,
			gm.added_at,
			gm.notes
		`).
		Joins("LEFT JOIN users u ON gm.user_internal_uuid = u.internal_uuid").
		Joins("LEFT JOIN groups mg ON gm.member_group_internal_uuid = mg.internal_uuid").
		Joins("LEFT JOIN users adder ON gm.added_by_internal_uuid = adder.internal_uuid").
		Where("gm.group_internal_uuid = ?", filter.GroupInternalUUID.String()).
		Order("gm.added_at DESC")

	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	var rows []memberRow
	if err := query.Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("failed to query group members: %w", err)
	}

	members := make([]GroupMember, len(rows))
	for i, row := range rows {
		idUUID, _ := uuid.Parse(row.ID)
		groupUUID, _ := uuid.Parse(row.GroupInternalUUID)

		members[i] = GroupMember{
			Id:                idUUID,
			GroupInternalUuid: groupUUID,
			SubjectType:       GroupMemberSubjectType(row.SubjectType),
			AddedAt:           row.AddedAt,
		}

		// Populate user fields for user-type members
		if row.UserInternalUUID != nil {
			if userUUID, err := uuid.Parse(*row.UserInternalUUID); err == nil {
				members[i].UserInternalUuid = &userUUID
			}
		}
		if row.UserEmail != nil {
			email := openapi_types.Email(*row.UserEmail)
			members[i].UserEmail = &email
		}
		if row.UserName != nil {
			members[i].UserName = row.UserName
		}
		if row.UserProvider != nil {
			members[i].UserProvider = row.UserProvider
		}
		if row.UserProviderUserID != nil {
			members[i].UserProviderUserId = row.UserProviderUserID
		}

		// Populate group fields for group-type members
		if row.MemberGroupInternalUUID != nil {
			if mgUUID, err := uuid.Parse(*row.MemberGroupInternalUUID); err == nil {
				members[i].MemberGroupInternalUuid = &mgUUID
			}
		}
		if row.MemberGroupName != nil {
			members[i].MemberGroupName = row.MemberGroupName
		}

		if row.AddedByInternalUUID != nil {
			if addedByUUID, err := uuid.Parse(*row.AddedByInternalUUID); err == nil {
				members[i].AddedByInternalUuid = &addedByUUID
			}
		}
		if row.AddedByEmail != nil {
			email := openapi_types.Email(*row.AddedByEmail)
			members[i].AddedByEmail = &email
		}
		if row.Notes != nil {
			members[i].Notes = row.Notes
		}
	}

	logger.Debug("Listed %d members for group %s", len(members), filter.GroupInternalUUID)

	return members, nil
}

// CountMembers returns the total number of members in a group
func (s *GormGroupMemberStore) CountMembers(ctx context.Context, groupInternalUUID uuid.UUID) (int, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Model(&models.GroupMember{}).
		Where("group_internal_uuid = ?", groupInternalUUID.String()).
		Count(&count).Error

	if err != nil {
		return 0, fmt.Errorf("failed to count group members: %w", err)
	}

	return int(count), nil
}

// AddMember adds a user to a group
func (s *GormGroupMemberStore) AddMember(ctx context.Context, groupInternalUUID, userInternalUUID uuid.UUID, addedByInternalUUID *uuid.UUID, notes *string) (*GroupMember, error) {
	logger := slogging.Get()

	// Check if group is the "everyone" pseudo-group
	if groupInternalUUID == uuid.MustParse("00000000-0000-0000-0000-000000000000") {
		return nil, fmt.Errorf("cannot add members to the 'everyone' pseudo-group")
	}

	// Verify group exists
	var groupCount int64
	if err := s.db.WithContext(ctx).Model(&models.Group{}).Where("internal_uuid = ?", groupInternalUUID.String()).Count(&groupCount).Error; err != nil {
		return nil, fmt.Errorf("failed to verify group existence: %w", err)
	}
	if groupCount == 0 {
		return nil, errors.New(ErrMsgGroupNotFound)
	}

	// Verify user exists
	var userCount int64
	if err := s.db.WithContext(ctx).Model(&models.User{}).Where("internal_uuid = ?", userInternalUUID.String()).Count(&userCount).Error; err != nil {
		return nil, fmt.Errorf("failed to verify user existence: %w", err)
	}
	if userCount == 0 {
		return nil, errors.New(ErrMsgUserNotFound)
	}

	// Create membership record
	memberID := uuid.New()
	addedAt := time.Now().UTC()
	userUUIDStr := userInternalUUID.String()

	model := models.GroupMember{
		ID:                memberID.String(),
		GroupInternalUUID: groupInternalUUID.String(),
		UserInternalUUID:  &userUUIDStr,
		SubjectType:       "user",
		AddedAt:           addedAt,
	}

	if addedByInternalUUID != nil {
		addedByStr := addedByInternalUUID.String()
		model.AddedByInternalUUID = &addedByStr
	}
	if notes != nil {
		model.Notes = notes
	}

	if err := s.db.WithContext(ctx).Create(&model).Error; err != nil {
		if s.isDuplicateKeyError(err) {
			return nil, fmt.Errorf("user is already a member of this group")
		}
		return nil, fmt.Errorf("failed to add group member: %w", err)
	}

	// Fetch the complete member record with user details
	type addMemberRow struct {
		ID                  string
		GroupInternalUUID   string
		UserInternalUUID    string
		UserEmail           string
		UserName            string
		UserProvider        string
		UserProviderUserID  string
		AddedByInternalUUID *string
		AddedByEmail        *string
		AddedAt             time.Time
		Notes               *string
	}

	var row addMemberRow
	err := s.db.WithContext(ctx).Table("group_members gm").
		Select(`
			gm.id,
			gm.group_internal_uuid,
			gm.user_internal_uuid,
			u.email as user_email,
			u.name as user_name,
			u.provider as user_provider,
			u.provider_user_id as user_provider_user_id,
			gm.added_by_internal_uuid,
			adder.email as added_by_email,
			gm.added_at,
			gm.notes
		`).
		Joins("JOIN users u ON gm.user_internal_uuid = u.internal_uuid").
		Joins("LEFT JOIN users adder ON gm.added_by_internal_uuid = adder.internal_uuid").
		Where("gm.id = ?", memberID.String()).
		Scan(&row).Error

	if err != nil {
		logger.Error("Failed to fetch created member record: %v", err)
		return nil, fmt.Errorf("failed to fetch created member record: %w", err)
	}

	userEmail := openapi_types.Email(row.UserEmail)
	member := &GroupMember{
		Id:                 memberID,
		GroupInternalUuid:  groupInternalUUID,
		UserInternalUuid:   &userInternalUUID,
		SubjectType:        GroupMemberSubjectTypeUser,
		UserEmail:          &userEmail,
		UserName:           &row.UserName,
		UserProvider:       &row.UserProvider,
		UserProviderUserId: &row.UserProviderUserID,
		AddedAt:            row.AddedAt,
	}

	if row.AddedByInternalUUID != nil {
		if addedByUUID, err := uuid.Parse(*row.AddedByInternalUUID); err == nil {
			member.AddedByInternalUuid = &addedByUUID
		}
	}
	if row.AddedByEmail != nil {
		email := openapi_types.Email(*row.AddedByEmail)
		member.AddedByEmail = &email
	}
	if row.Notes != nil {
		member.Notes = row.Notes
	}

	logger.Info("Added member %s to group %s", userInternalUUID, groupInternalUUID)

	return member, nil
}

// RemoveMember removes a user from a group
func (s *GormGroupMemberStore) RemoveMember(ctx context.Context, groupInternalUUID, userInternalUUID uuid.UUID) error {
	logger := slogging.Get()

	// Check if group is the "everyone" pseudo-group
	if groupInternalUUID == uuid.MustParse("00000000-0000-0000-0000-000000000000") {
		return fmt.Errorf("cannot remove members from the 'everyone' pseudo-group")
	}

	result := s.db.WithContext(ctx).
		Where("group_internal_uuid = ? AND user_internal_uuid = ? AND subject_type = ?", groupInternalUUID.String(), userInternalUUID.String(), "user").
		Delete(&models.GroupMember{})

	if result.Error != nil {
		return fmt.Errorf("failed to remove group member: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("membership not found")
	}

	logger.Info("Removed member %s from group %s", userInternalUUID, groupInternalUUID)

	return nil
}

// IsMember checks if a user is a direct member of a group
func (s *GormGroupMemberStore) IsMember(ctx context.Context, groupInternalUUID, userInternalUUID uuid.UUID) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Model(&models.GroupMember{}).
		Where("group_internal_uuid = ? AND user_internal_uuid = ? AND subject_type = ?", groupInternalUUID.String(), userInternalUUID.String(), "user").
		Count(&count).Error

	if err != nil {
		return false, fmt.Errorf("failed to check group membership: %w", err)
	}

	return count > 0, nil
}

// isDuplicateKeyError checks if the error is a duplicate key violation
// This works across different databases via GORM's error handling
func (s *GormGroupMemberStore) isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}

	// Check for GORM's duplicate key error
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}

	// Also check error message for database-specific duplicate key errors
	errMsg := err.Error()

	// PostgreSQL
	if strings.Contains(errMsg, "duplicate key value violates unique constraint") {
		return true
	}
	// MySQL
	if strings.Contains(errMsg, "Duplicate entry") {
		return true
	}
	// SQLite
	if strings.Contains(errMsg, "UNIQUE constraint failed") {
		return true
	}
	// SQL Server
	if strings.Contains(errMsg, "Cannot insert duplicate key") {
		return true
	}
	// Oracle
	if strings.Contains(errMsg, "unique constraint") && strings.Contains(errMsg, "violated") {
		return true
	}

	return false
}

// AddGroupMember adds a group as a member of another group (one level of nesting)
func (s *GormGroupMemberStore) AddGroupMember(ctx context.Context, groupInternalUUID, memberGroupInternalUUID uuid.UUID, addedByInternalUUID *uuid.UUID, notes *string) (*GroupMember, error) {
	logger := slogging.Get()

	// Check if target group is the "everyone" pseudo-group
	if groupInternalUUID == uuid.MustParse("00000000-0000-0000-0000-000000000000") {
		return nil, fmt.Errorf("cannot add members to the 'everyone' pseudo-group")
	}

	// Prevent self-membership
	if groupInternalUUID == memberGroupInternalUUID {
		return nil, fmt.Errorf("a group cannot be a member of itself")
	}

	// Verify target group exists
	var groupCount int64
	if err := s.db.WithContext(ctx).Model(&models.Group{}).Where("internal_uuid = ?", groupInternalUUID.String()).Count(&groupCount).Error; err != nil {
		return nil, fmt.Errorf("failed to verify group existence: %w", err)
	}
	if groupCount == 0 {
		return nil, errors.New(ErrMsgGroupNotFound)
	}

	// Verify member group exists
	var memberGroupCount int64
	if err := s.db.WithContext(ctx).Model(&models.Group{}).Where("internal_uuid = ?", memberGroupInternalUUID.String()).Count(&memberGroupCount).Error; err != nil {
		return nil, fmt.Errorf("failed to verify member group existence: %w", err)
	}
	if memberGroupCount == 0 {
		return nil, fmt.Errorf("member group not found")
	}

	// Create membership record
	memberID := uuid.New()
	addedAt := time.Now().UTC()
	memberGroupStr := memberGroupInternalUUID.String()

	model := models.GroupMember{
		ID:                      memberID.String(),
		GroupInternalUUID:       groupInternalUUID.String(),
		MemberGroupInternalUUID: &memberGroupStr,
		SubjectType:             "group",
		AddedAt:                 addedAt,
	}

	if addedByInternalUUID != nil {
		addedByStr := addedByInternalUUID.String()
		model.AddedByInternalUUID = &addedByStr
	}
	if notes != nil {
		model.Notes = notes
	}

	if err := s.db.WithContext(ctx).Create(&model).Error; err != nil {
		if s.isDuplicateKeyError(err) {
			return nil, fmt.Errorf("group is already a member of this group")
		}
		return nil, fmt.Errorf("failed to add group member: %w", err)
	}

	// Fetch member group name for the response
	var memberGroupName *string
	var memberGroup models.Group
	if err := s.db.WithContext(ctx).Where("internal_uuid = ?", memberGroupInternalUUID.String()).First(&memberGroup).Error; err == nil {
		if memberGroup.Name != nil {
			memberGroupName = memberGroup.Name
		} else {
			memberGroupName = &memberGroup.GroupName
		}
	}

	// Return the GroupMember result
	member := &GroupMember{
		Id:                      memberID,
		GroupInternalUuid:       groupInternalUUID,
		SubjectType:             GroupMemberSubjectTypeGroup,
		MemberGroupInternalUuid: &memberGroupInternalUUID,
		MemberGroupName:         memberGroupName,
		AddedAt:                 addedAt,
	}
	if notes != nil {
		member.Notes = notes
	}

	logger.Info("Added group %s as member of group %s", memberGroupInternalUUID, groupInternalUUID)

	return member, nil
}

// RemoveGroupMember removes a group from membership in another group
func (s *GormGroupMemberStore) RemoveGroupMember(ctx context.Context, groupInternalUUID, memberGroupInternalUUID uuid.UUID) error {
	logger := slogging.Get()

	// Check if target group is the "everyone" pseudo-group
	if groupInternalUUID == uuid.MustParse("00000000-0000-0000-0000-000000000000") {
		return fmt.Errorf("cannot remove members from the 'everyone' pseudo-group")
	}

	result := s.db.WithContext(ctx).
		Where("group_internal_uuid = ? AND member_group_internal_uuid = ? AND subject_type = ?",
			groupInternalUUID.String(), memberGroupInternalUUID.String(), "group").
		Delete(&models.GroupMember{})

	if result.Error != nil {
		return fmt.Errorf("failed to remove group member: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("group membership not found")
	}

	logger.Info("Removed group %s from group %s", memberGroupInternalUUID, groupInternalUUID)

	return nil
}

// IsEffectiveMember checks if a user is an effective member of a group, either
// through direct user membership or because one of the user's IdP groups is a
// group member (one level of nesting).
func (s *GormGroupMemberStore) IsEffectiveMember(ctx context.Context, groupInternalUUID uuid.UUID, userInternalUUID uuid.UUID, userGroupUUIDs []uuid.UUID) (bool, error) {
	groupStr := groupInternalUUID.String()
	userStr := userInternalUUID.String()

	// Build a single query checking both direct membership and group membership
	query := s.db.WithContext(ctx).Model(&models.GroupMember{}).
		Where("group_internal_uuid = ?", groupStr)

	if len(userGroupUUIDs) == 0 {
		// Only check direct user membership
		query = query.Where("subject_type = ? AND user_internal_uuid = ?", "user", userStr)
	} else {
		// Check direct user membership OR group membership via user's IdP groups
		groupUUIDStrs := make([]string, len(userGroupUUIDs))
		for i, g := range userGroupUUIDs {
			groupUUIDStrs[i] = g.String()
		}
		query = query.Where(
			"(subject_type = ? AND user_internal_uuid = ?) OR (subject_type = ? AND member_group_internal_uuid IN ?)",
			"user", userStr, "group", groupUUIDStrs,
		)
	}

	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, fmt.Errorf("failed to check effective membership: %w", err)
	}

	return count > 0, nil
}

// HasAnyMembers checks if a group has any members (user or group)
func (s *GormGroupMemberStore) HasAnyMembers(ctx context.Context, groupInternalUUID uuid.UUID) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Model(&models.GroupMember{}).
		Where("group_internal_uuid = ?", groupInternalUUID.String()).
		Count(&count).Error

	if err != nil {
		return false, fmt.Errorf("failed to check for group members: %w", err)
	}

	return count > 0, nil
}

// GetGroupsForUser returns all TMI-managed groups that a user has direct membership in.
// This queries the group_members table for user-type memberships and joins the groups table
// to return group metadata. The "everyone" pseudo-group is excluded since it has no
// membership records (all authenticated users are implicitly members).
func (s *GormGroupMemberStore) GetGroupsForUser(ctx context.Context, userInternalUUID uuid.UUID) ([]Group, error) {
	logger := slogging.Get()

	type groupRow struct {
		InternalUUID string  `gorm:"column:internal_uuid"`
		GroupName    string  `gorm:"column:group_name"`
		Name         *string `gorm:"column:name"`
	}

	var rows []groupRow
	err := s.db.WithContext(ctx).
		Table("group_members").
		Distinct("groups.internal_uuid, groups.group_name, groups.name").
		Joins("JOIN groups ON groups.internal_uuid = group_members.group_internal_uuid").
		Where("group_members.subject_type = ? AND group_members.user_internal_uuid = ?", "user", userInternalUUID.String()).
		Scan(&rows).Error

	if err != nil {
		return nil, fmt.Errorf("failed to query groups for user: %w", err)
	}

	groups := make([]Group, len(rows))
	for i, row := range rows {
		groupUUID, _ := uuid.Parse(row.InternalUUID)
		groups[i] = Group{
			InternalUUID: groupUUID,
			GroupName:    row.GroupName,
		}
		if row.Name != nil {
			groups[i].Name = *row.Name
		}
	}

	logger.Debug("Found %d groups for user %s", len(groups), userInternalUUID.String())
	return groups, nil
}
