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

	// Use a raw query with joins to get user details
	type memberRow struct {
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

	query := s.db.WithContext(ctx).Table("group_members gm").
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
		userUUID, _ := uuid.Parse(row.UserInternalUUID)

		members[i] = GroupMember{
			Id:                 idUUID,
			GroupInternalUuid:  groupUUID,
			UserInternalUuid:   userUUID,
			UserEmail:          openapi_types.Email(row.UserEmail),
			UserName:           row.UserName,
			UserProvider:       row.UserProvider,
			UserProviderUserId: row.UserProviderUserID,
			AddedAt:            row.AddedAt,
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
		return nil, fmt.Errorf("group not found")
	}

	// Verify user exists
	var userCount int64
	if err := s.db.WithContext(ctx).Model(&models.User{}).Where("internal_uuid = ?", userInternalUUID.String()).Count(&userCount).Error; err != nil {
		return nil, fmt.Errorf("failed to verify user existence: %w", err)
	}
	if userCount == 0 {
		return nil, fmt.Errorf("user not found")
	}

	// Create membership record
	memberID := uuid.New()
	addedAt := time.Now().UTC()

	model := models.GroupMember{
		ID:                memberID.String(),
		GroupInternalUUID: groupInternalUUID.String(),
		UserInternalUUID:  userInternalUUID.String(),
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
	filter := GroupMemberFilter{
		GroupInternalUUID: groupInternalUUID,
		Limit:             1,
	}

	// Use a targeted query to get just this member
	type memberRow struct {
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

	var row memberRow
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

	member := &GroupMember{
		Id:                 memberID,
		GroupInternalUuid:  groupInternalUUID,
		UserInternalUuid:   userInternalUUID,
		UserEmail:          openapi_types.Email(row.UserEmail),
		UserName:           row.UserName,
		UserProvider:       row.UserProvider,
		UserProviderUserId: row.UserProviderUserID,
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
	_ = filter // unused but kept for reference

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
		Where("group_internal_uuid = ? AND user_internal_uuid = ?", groupInternalUUID.String(), userInternalUUID.String()).
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

// IsMember checks if a user is a member of a group
func (s *GormGroupMemberStore) IsMember(ctx context.Context, groupInternalUUID, userInternalUUID uuid.UUID) (bool, error) {
	var count int64
	err := s.db.WithContext(ctx).
		Model(&models.GroupMember{}).
		Where("group_internal_uuid = ? AND user_internal_uuid = ?", groupInternalUUID.String(), userInternalUUID.String()).
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
