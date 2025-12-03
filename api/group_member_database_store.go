package api

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// GroupMemberDatabaseStore implements group membership operations using PostgreSQL
type GroupMemberDatabaseStore struct {
	db *sql.DB
}

// NewGroupMemberDatabaseStore creates a new database-backed group member store
func NewGroupMemberDatabaseStore(db *sql.DB) *GroupMemberDatabaseStore {
	return &GroupMemberDatabaseStore{
		db: db,
	}
}

// GroupMemberFilter defines filtering and pagination for group membership queries
type GroupMemberFilter struct {
	GroupInternalUUID uuid.UUID
	Limit             int
	Offset            int
}

// ListMembers returns all members of a group with pagination
func (s *GroupMemberDatabaseStore) ListMembers(ctx context.Context, filter GroupMemberFilter) ([]GroupMember, error) {
	logger := slogging.Get()

	// Query joins group_members with users to get user details
	query := `
		SELECT
			gm.id,
			gm.group_internal_uuid,
			gm.user_internal_uuid,
			u.email,
			u.name,
			u.provider,
			u.provider_user_id,
			gm.added_by_internal_uuid,
			adder.email as added_by_email,
			gm.added_at,
			gm.notes
		FROM group_members gm
		JOIN users u ON gm.user_internal_uuid = u.internal_uuid
		LEFT JOIN users adder ON gm.added_by_internal_uuid = adder.internal_uuid
		WHERE gm.group_internal_uuid = $1
		ORDER BY gm.added_at DESC`

	args := []interface{}{filter.GroupInternalUUID}
	argPos := 2

	// Apply pagination
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argPos)
		args = append(args, filter.Limit)
		argPos++
	}

	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argPos)
		args = append(args, filter.Offset)
	}

	// Execute query
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query group members: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			logger.Error("Failed to close rows: %v", cerr)
		}
	}()

	// Scan results
	members := []GroupMember{}
	for rows.Next() {
		var member GroupMember
		var idUUID, groupUUID, userUUID uuid.UUID
		var userEmail, userProvider, userProviderUserID, userName string
		var addedByUUID sql.NullString
		var addedByEmail sql.NullString
		var notes sql.NullString
		var addedAt time.Time

		err := rows.Scan(
			&idUUID,
			&groupUUID,
			&userUUID,
			&userEmail,
			&userName,
			&userProvider,
			&userProviderUserID,
			&addedByUUID,
			&addedByEmail,
			&addedAt,
			&notes,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan group member: %w", err)
		}

		// Convert to openapi_types
		member.Id = idUUID
		member.GroupInternalUuid = groupUUID
		member.UserInternalUuid = userUUID
		member.UserEmail = openapi_types.Email(userEmail)
		member.UserName = userName
		member.UserProvider = userProvider
		member.UserProviderUserId = userProviderUserID
		member.AddedAt = addedAt

		if addedByUUID.Valid {
			if parsedUUID, err := uuid.Parse(addedByUUID.String); err == nil {
				member.AddedByInternalUuid = &parsedUUID
			}
		}
		if addedByEmail.Valid {
			email := openapi_types.Email(addedByEmail.String)
			member.AddedByEmail = &email
		}
		if notes.Valid {
			member.Notes = &notes.String
		}

		members = append(members, member)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating group members: %w", err)
	}

	return members, nil
}

// CountMembers returns the total number of members in a group
func (s *GroupMemberDatabaseStore) CountMembers(ctx context.Context, groupInternalUUID uuid.UUID) (int, error) {
	query := `SELECT COUNT(*) FROM group_members WHERE group_internal_uuid = $1`

	var count int
	err := s.db.QueryRowContext(ctx, query, groupInternalUUID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count group members: %w", err)
	}

	return count, nil
}

// AddMember adds a user to a group
func (s *GroupMemberDatabaseStore) AddMember(ctx context.Context, groupInternalUUID, userInternalUUID uuid.UUID, addedByInternalUUID *uuid.UUID, notes *string) (*GroupMember, error) {
	logger := slogging.Get()

	// Check if group is the "everyone" pseudo-group
	if groupInternalUUID == uuid.MustParse("00000000-0000-0000-0000-000000000000") {
		return nil, fmt.Errorf("cannot add members to the 'everyone' pseudo-group")
	}

	// Verify group exists
	var groupExists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM groups WHERE internal_uuid = $1)`, groupInternalUUID).Scan(&groupExists)
	if err != nil {
		return nil, fmt.Errorf("failed to verify group existence: %w", err)
	}
	if !groupExists {
		return nil, fmt.Errorf("group not found")
	}

	// Verify user exists
	var userExists bool
	err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE internal_uuid = $1)`, userInternalUUID).Scan(&userExists)
	if err != nil {
		return nil, fmt.Errorf("failed to verify user existence: %w", err)
	}
	if !userExists {
		return nil, fmt.Errorf("user not found")
	}

	// Insert membership record
	memberID := uuid.New()
	addedAt := time.Now().UTC()

	query := `
		INSERT INTO group_members (id, group_internal_uuid, user_internal_uuid, added_by_internal_uuid, added_at, notes)
		VALUES ($1, $2, $3, $4, $5, $6)`

	_, err = s.db.ExecContext(ctx, query, memberID, groupInternalUUID, userInternalUUID, addedByInternalUUID, addedAt, notes)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, fmt.Errorf("user is already a member of this group")
		}
		return nil, fmt.Errorf("failed to add group member: %w", err)
	}

	// Fetch and return the complete member record with user details
	fetchQuery := `
		SELECT
			gm.id,
			gm.group_internal_uuid,
			gm.user_internal_uuid,
			u.email,
			u.name,
			u.provider,
			u.provider_user_id,
			gm.added_by_internal_uuid,
			adder.email as added_by_email,
			gm.added_at,
			gm.notes
		FROM group_members gm
		JOIN users u ON gm.user_internal_uuid = u.internal_uuid
		LEFT JOIN users adder ON gm.added_by_internal_uuid = adder.internal_uuid
		WHERE gm.id = $1`

	var member GroupMember
	var idUUID, groupUUID, userUUID uuid.UUID
	var userEmail, userProvider, userProviderUserID, userName string
	var addedByUUIDStr, addedByEmailStr, notesStr sql.NullString
	var addedAtDB time.Time

	err = s.db.QueryRowContext(ctx, fetchQuery, memberID).Scan(
		&idUUID,
		&groupUUID,
		&userUUID,
		&userEmail,
		&userName,
		&userProvider,
		&userProviderUserID,
		&addedByUUIDStr,
		&addedByEmailStr,
		&addedAtDB,
		&notesStr,
	)
	if err != nil {
		logger.Error("Failed to fetch created member record: %v", err)
		return nil, fmt.Errorf("failed to fetch created member record: %w", err)
	}

	// Convert to openapi_types
	member.Id = idUUID
	member.GroupInternalUuid = groupUUID
	member.UserInternalUuid = userUUID
	member.UserEmail = openapi_types.Email(userEmail)
	member.UserName = userName
	member.UserProvider = userProvider
	member.UserProviderUserId = userProviderUserID
	member.AddedAt = addedAtDB

	if addedByUUIDStr.Valid {
		if parsedUUID, err := uuid.Parse(addedByUUIDStr.String); err == nil {
			member.AddedByInternalUuid = &parsedUUID
		}
	}
	if addedByEmailStr.Valid {
		email := openapi_types.Email(addedByEmailStr.String)
		member.AddedByEmail = &email
	}
	if notesStr.Valid {
		member.Notes = &notesStr.String
	}

	return &member, nil
}

// RemoveMember removes a user from a group
func (s *GroupMemberDatabaseStore) RemoveMember(ctx context.Context, groupInternalUUID, userInternalUUID uuid.UUID) error {
	// Check if group is the "everyone" pseudo-group
	if groupInternalUUID == uuid.MustParse("00000000-0000-0000-0000-000000000000") {
		return fmt.Errorf("cannot remove members from the 'everyone' pseudo-group")
	}

	query := `DELETE FROM group_members WHERE group_internal_uuid = $1 AND user_internal_uuid = $2`

	result, err := s.db.ExecContext(ctx, query, groupInternalUUID, userInternalUUID)
	if err != nil {
		return fmt.Errorf("failed to remove group member: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("membership not found")
	}

	return nil
}

// IsMember checks if a user is a member of a group
func (s *GroupMemberDatabaseStore) IsMember(ctx context.Context, groupInternalUUID, userInternalUUID uuid.UUID) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM group_members WHERE group_internal_uuid = $1 AND user_internal_uuid = $2)`

	var exists bool
	err := s.db.QueryRowContext(ctx, query, groupInternalUUID, userInternalUUID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check group membership: %w", err)
	}

	return exists, nil
}

// isDuplicateKeyError checks if the error is a PostgreSQL duplicate key violation
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	// Check for PostgreSQL error code 23505 (unique_violation)
	errMsg := err.Error()
	return errMsg == "pq: duplicate key value violates unique constraint \"group_members_group_internal_uuid_user_internal_uuid_key\"" ||
		errMsg == "ERROR: duplicate key value violates unique constraint \"group_members_group_internal_uuid_user_internal_uuid_key\" (SQLSTATE 23505)"
}
