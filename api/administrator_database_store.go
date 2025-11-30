package api

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
)

// AdministratorDatabaseStore implements AdministratorStore using PostgreSQL
type AdministratorDatabaseStore struct {
	db *sql.DB
}

// NewAdministratorDatabaseStore creates a new database-backed administrator store
func NewAdministratorDatabaseStore(db *sql.DB) *AdministratorDatabaseStore {
	return &AdministratorDatabaseStore{db: db}
}

// Create adds a new administrator entry
func (s *AdministratorDatabaseStore) Create(ctx context.Context, admin Administrator) error {
	logger := slogging.Get()

	// Generate ID if not set
	id := admin.ID
	if id == uuid.Nil {
		id = uuid.New()
	}

	// Use different queries based on subject_type to handle different unique constraints
	var query string
	if admin.SubjectType == "user" {
		query = `
			INSERT INTO administrators (id, user_internal_uuid, group_internal_uuid, subject_type, provider, granted_at, granted_by_internal_uuid, notes)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (user_internal_uuid, subject_type) DO UPDATE
			SET granted_at = EXCLUDED.granted_at,
				granted_by_internal_uuid = EXCLUDED.granted_by_internal_uuid,
				notes = EXCLUDED.notes,
				provider = EXCLUDED.provider
		`
	} else {
		query = `
			INSERT INTO administrators (id, user_internal_uuid, group_internal_uuid, subject_type, provider, granted_at, granted_by_internal_uuid, notes)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (group_internal_uuid, subject_type, provider) DO UPDATE
			SET granted_at = EXCLUDED.granted_at,
				granted_by_internal_uuid = EXCLUDED.granted_by_internal_uuid,
				notes = EXCLUDED.notes
		`
	}

	_, err := s.db.ExecContext(ctx, query,
		id,
		admin.UserInternalUUID,
		admin.GroupInternalUUID,
		admin.SubjectType,
		admin.Provider,
		admin.GrantedAt,
		admin.GrantedBy,
		admin.Notes,
	)

	if err != nil {
		logger.Error("Failed to create administrator entry: type=%s, provider=%s, user_uuid=%v, group_uuid=%v, error=%v",
			admin.SubjectType, admin.Provider, admin.UserInternalUUID, admin.GroupInternalUUID, err)
		return fmt.Errorf("failed to create administrator: %w", err)
	}

	logger.Info("Administrator created: type=%s, provider=%s, user_uuid=%v, group_uuid=%v",
		admin.SubjectType, admin.Provider, admin.UserInternalUUID, admin.GroupInternalUUID)

	return nil
}

// Delete removes an administrator entry by ID
func (s *AdministratorDatabaseStore) Delete(ctx context.Context, id uuid.UUID) error {
	logger := slogging.Get()

	query := `
		DELETE FROM administrators
		WHERE id = $1
	`

	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		logger.Error("Failed to delete administrator entry: id=%s, error=%v", id, err)
		return fmt.Errorf("failed to delete administrator: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logger.Error("Failed to get rows affected for delete: %v", err)
		return fmt.Errorf("failed to verify delete: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("administrator not found: id=%s", id)
	}

	logger.Info("Administrator deleted: id=%s", id)

	return nil
}

// List returns all administrator entries
func (s *AdministratorDatabaseStore) List(ctx context.Context) ([]Administrator, error) {
	logger := slogging.Get()

	query := `
		SELECT id, user_internal_uuid, group_internal_uuid, subject_type, provider, granted_at, granted_by_internal_uuid, notes
		FROM administrators
		ORDER BY granted_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		logger.Error("Failed to list administrators: %v", err)
		return nil, fmt.Errorf("failed to list administrators: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Error("Failed to close rows: %v", closeErr)
		}
	}()

	var administrators []Administrator
	for rows.Next() {
		var admin Administrator
		var userUUID sql.NullString
		var groupUUID sql.NullString
		var grantedBy sql.NullString
		var notes sql.NullString

		err := rows.Scan(
			&admin.ID,
			&userUUID,
			&groupUUID,
			&admin.SubjectType,
			&admin.Provider,
			&admin.GrantedAt,
			&grantedBy,
			&notes,
		)
		if err != nil {
			logger.Error("Failed to scan administrator row: %v", err)
			return nil, fmt.Errorf("failed to scan administrator: %w", err)
		}

		// Handle nullable UUIDs
		if userUUID.Valid {
			parsedUUID, err := uuid.Parse(userUUID.String)
			if err == nil {
				admin.UserInternalUUID = &parsedUUID
			}
		}
		if groupUUID.Valid {
			parsedUUID, err := uuid.Parse(groupUUID.String)
			if err == nil {
				admin.GroupInternalUUID = &parsedUUID
			}
		}
		if grantedBy.Valid {
			parsedUUID, err := uuid.Parse(grantedBy.String)
			if err == nil {
				admin.GrantedBy = &parsedUUID
			}
		}
		if notes.Valid {
			admin.Notes = notes.String
		}

		administrators = append(administrators, admin)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Error iterating administrator rows: %v", err)
		return nil, fmt.Errorf("error iterating administrators: %w", err)
	}

	logger.Debug("Listed %d administrators", len(administrators))

	return administrators, nil
}

// IsAdmin checks if a user or any of their groups is an administrator
// Checks by user UUID and provider, or by group UUIDs and provider
func (s *AdministratorDatabaseStore) IsAdmin(ctx context.Context, userUUID *uuid.UUID, provider string, groupUUIDs []uuid.UUID) (bool, error) {
	logger := slogging.Get()

	// Build query to check:
	// 1. User by UUID and provider (subject_type='user' AND user_internal_uuid=uuid AND provider=provider)
	// 2. Any group by UUID and provider (subject_type='group' AND group_internal_uuid IN groupUUIDs AND provider=provider)

	query := `
		SELECT EXISTS (
			SELECT 1 FROM administrators
			WHERE provider = $1
			AND (
				(subject_type = 'user' AND $2::uuid IS NOT NULL AND user_internal_uuid = $2)
				OR (subject_type = 'group' AND group_internal_uuid = ANY($3))
			)
		)
	`

	var isAdmin bool
	err := s.db.QueryRowContext(ctx, query, provider, userUUID, pqUUIDArray(groupUUIDs)).Scan(&isAdmin)
	if err != nil {
		logger.Error("Failed to check admin status for user_uuid=%v, provider=%s, groups=%v: %v",
			userUUID, provider, groupUUIDs, err)
		return false, fmt.Errorf("failed to check admin status: %w", err)
	}

	logger.Debug("Admin check: user_uuid=%v, provider=%s, groups=%v, is_admin=%t",
		userUUID, provider, groupUUIDs, isAdmin)

	return isAdmin, nil
}

// GetByPrincipal retrieves administrator entries by user or group UUID
func (s *AdministratorDatabaseStore) GetByPrincipal(ctx context.Context, userUUID *uuid.UUID, groupUUID *uuid.UUID, provider string) ([]Administrator, error) {
	logger := slogging.Get()

	query := `
		SELECT id, user_internal_uuid, group_internal_uuid, subject_type, provider, granted_at, granted_by_internal_uuid, notes
		FROM administrators
		WHERE provider = $1
		AND (
			($2::uuid IS NOT NULL AND user_internal_uuid = $2)
			OR ($3::uuid IS NOT NULL AND group_internal_uuid = $3)
		)
		ORDER BY granted_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, provider, userUUID, groupUUID)
	if err != nil {
		logger.Error("Failed to get administrators by principal: user_uuid=%v, group_uuid=%v, provider=%s, error=%v",
			userUUID, groupUUID, provider, err)
		return nil, fmt.Errorf("failed to get administrators by principal: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Error("Failed to close rows: %v", closeErr)
		}
	}()

	var administrators []Administrator
	for rows.Next() {
		var admin Administrator
		var userUUIDCol sql.NullString
		var groupUUIDCol sql.NullString
		var grantedBy sql.NullString
		var notes sql.NullString

		err := rows.Scan(
			&admin.ID,
			&userUUIDCol,
			&groupUUIDCol,
			&admin.SubjectType,
			&admin.Provider,
			&admin.GrantedAt,
			&grantedBy,
			&notes,
		)
		if err != nil {
			logger.Error("Failed to scan administrator row: %v", err)
			return nil, fmt.Errorf("failed to scan administrator: %w", err)
		}

		// Handle nullable UUIDs
		if userUUIDCol.Valid {
			parsedUUID, err := uuid.Parse(userUUIDCol.String)
			if err == nil {
				admin.UserInternalUUID = &parsedUUID
			}
		}
		if groupUUIDCol.Valid {
			parsedUUID, err := uuid.Parse(groupUUIDCol.String)
			if err == nil {
				admin.GroupInternalUUID = &parsedUUID
			}
		}
		if grantedBy.Valid {
			parsedUUID, err := uuid.Parse(grantedBy.String)
			if err == nil {
				admin.GrantedBy = &parsedUUID
			}
		}
		if notes.Valid {
			admin.Notes = notes.String
		}

		administrators = append(administrators, admin)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Error iterating administrator rows: %v", err)
		return nil, fmt.Errorf("error iterating administrators: %w", err)
	}

	logger.Debug("Found %d administrators for user_uuid=%v, group_uuid=%v, provider=%s",
		len(administrators), userUUID, groupUUID, provider)

	return administrators, nil
}

// GetGroupUUIDsByNames looks up group UUIDs from group names for a given provider
// This is a helper function for middleware/handlers that receive group names from JWT
func (s *AdministratorDatabaseStore) GetGroupUUIDsByNames(ctx context.Context, provider string, groupNames []string) ([]uuid.UUID, error) {
	if len(groupNames) == 0 {
		return []uuid.UUID{}, nil
	}

	logger := slogging.Get()

	query := `
		SELECT internal_uuid
		FROM groups
		WHERE provider = $1
		AND group_name = ANY($2)
	`

	rows, err := s.db.QueryContext(ctx, query, provider, pqStringArray(groupNames))
	if err != nil {
		logger.Error("Failed to look up group UUIDs: provider=%s, group_names=%v, error=%v",
			provider, groupNames, err)
		return nil, fmt.Errorf("failed to look up group UUIDs: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Error("Failed to close rows: %v", closeErr)
		}
	}()

	var groupUUIDs []uuid.UUID
	for rows.Next() {
		var groupUUID uuid.UUID
		err := rows.Scan(&groupUUID)
		if err != nil {
			logger.Error("Failed to scan group UUID: %v", err)
			return nil, fmt.Errorf("failed to scan group UUID: %w", err)
		}
		groupUUIDs = append(groupUUIDs, groupUUID)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Error iterating group UUID rows: %v", err)
		return nil, fmt.Errorf("error iterating group UUIDs: %w", err)
	}

	logger.Debug("Looked up %d group UUIDs from %d group names for provider %s",
		len(groupUUIDs), len(groupNames), provider)

	return groupUUIDs, nil
}

// pqStringArray converts a Go string slice to a PostgreSQL-compatible array format
func pqStringArray(arr []string) interface{} {
	if arr == nil {
		return nil
	}
	if len(arr) == 0 {
		return "{}"
	}
	return arr
}

// pqUUIDArray converts a Go UUID slice to a PostgreSQL-compatible array format
func pqUUIDArray(arr []uuid.UUID) interface{} {
	if arr == nil {
		return nil
	}
	if len(arr) == 0 {
		return "{}"
	}
	return arr
}

// Get retrieves a single administrator grant by ID
func (s *AdministratorDatabaseStore) Get(ctx context.Context, id uuid.UUID) (*Administrator, error) {
	logger := slogging.Get()

	query := `
		SELECT id, user_internal_uuid, group_internal_uuid, subject_type, provider, granted_at, granted_by_internal_uuid, notes
		FROM administrators
		WHERE id = $1
	`

	var admin Administrator
	var userUUID sql.NullString
	var groupUUID sql.NullString
	var grantedBy sql.NullString
	var notes sql.NullString

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&admin.ID,
		&userUUID,
		&groupUUID,
		&admin.SubjectType,
		&admin.Provider,
		&admin.GrantedAt,
		&grantedBy,
		&notes,
	)

	if err == sql.ErrNoRows {
		logger.Debug("Administrator not found: id=%s", id)
		return nil, fmt.Errorf("administrator not found: id=%s", id)
	}
	if err != nil {
		logger.Error("Failed to get administrator: id=%s, error=%v", id, err)
		return nil, fmt.Errorf("failed to get administrator: %w", err)
	}

	// Handle nullable UUIDs
	if userUUID.Valid {
		parsedUUID, err := uuid.Parse(userUUID.String)
		if err == nil {
			admin.UserInternalUUID = &parsedUUID
		}
	}
	if groupUUID.Valid {
		parsedUUID, err := uuid.Parse(groupUUID.String)
		if err == nil {
			admin.GroupInternalUUID = &parsedUUID
		}
	}
	if grantedBy.Valid {
		parsedUUID, err := uuid.Parse(grantedBy.String)
		if err == nil {
			admin.GrantedBy = &parsedUUID
		}
	}
	if notes.Valid {
		admin.Notes = notes.String
	}

	logger.Debug("Retrieved administrator: id=%s", id)
	return &admin, nil
}

// AdminFilter represents filtering criteria for listing administrators
type AdminFilter struct {
	Provider string     // Filter by provider (optional)
	UserID   *uuid.UUID // Filter by user_id (optional)
	GroupID  *uuid.UUID // Filter by group_id (optional)
	Limit    int        // Pagination limit (default 50, max 100)
	Offset   int        // Pagination offset (default 0)
}

// ListFiltered retrieves administrator grants with optional filtering
func (s *AdministratorDatabaseStore) ListFiltered(ctx context.Context, filter AdminFilter) ([]Administrator, error) {
	logger := slogging.Get()

	// Build query dynamically based on filters
	query := `
		SELECT id, user_internal_uuid, group_internal_uuid, subject_type, provider, granted_at, granted_by_internal_uuid, notes
		FROM administrators
		WHERE 1=1
	`
	args := []interface{}{}
	argIndex := 1

	// Apply filters
	if filter.Provider != "" {
		query += fmt.Sprintf(" AND provider = $%d", argIndex)
		args = append(args, filter.Provider)
		argIndex++
	}

	if filter.UserID != nil {
		query += fmt.Sprintf(" AND user_internal_uuid = $%d", argIndex)
		args = append(args, filter.UserID)
		argIndex++
	}

	if filter.GroupID != nil {
		query += fmt.Sprintf(" AND group_internal_uuid = $%d", argIndex)
		args = append(args, filter.GroupID)
		argIndex++
	}

	// Add ordering and pagination
	query += " ORDER BY granted_at DESC"

	// Apply pagination limits
	limit := filter.Limit
	if limit <= 0 {
		limit = 50 // Default limit
	}
	if limit > 100 {
		limit = 100 // Max limit
	}

	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argIndex, argIndex+1)
	args = append(args, limit, filter.Offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		logger.Error("Failed to list administrators with filter: %v", err)
		return nil, fmt.Errorf("failed to list administrators: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Error("Failed to close rows: %v", closeErr)
		}
	}()

	var administrators []Administrator
	for rows.Next() {
		var admin Administrator
		var userUUID sql.NullString
		var groupUUID sql.NullString
		var grantedBy sql.NullString
		var notes sql.NullString

		err := rows.Scan(
			&admin.ID,
			&userUUID,
			&groupUUID,
			&admin.SubjectType,
			&admin.Provider,
			&admin.GrantedAt,
			&grantedBy,
			&notes,
		)
		if err != nil {
			logger.Error("Failed to scan administrator row: %v", err)
			return nil, fmt.Errorf("failed to scan administrator: %w", err)
		}

		// Handle nullable UUIDs
		if userUUID.Valid {
			parsedUUID, err := uuid.Parse(userUUID.String)
			if err == nil {
				admin.UserInternalUUID = &parsedUUID
			}
		}
		if groupUUID.Valid {
			parsedUUID, err := uuid.Parse(groupUUID.String)
			if err == nil {
				admin.GroupInternalUUID = &parsedUUID
			}
		}
		if grantedBy.Valid {
			parsedUUID, err := uuid.Parse(grantedBy.String)
			if err == nil {
				admin.GrantedBy = &parsedUUID
			}
		}
		if notes.Valid {
			admin.Notes = notes.String
		}

		administrators = append(administrators, admin)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Error iterating administrator rows: %v", err)
		return nil, fmt.Errorf("error iterating administrators: %w", err)
	}

	logger.Debug("Listed %d administrators with filter", len(administrators))
	return administrators, nil
}

// HasAnyAdministrators returns true if at least one administrator grant exists
func (s *AdministratorDatabaseStore) HasAnyAdministrators(ctx context.Context) (bool, error) {
	logger := slogging.Get()

	query := `SELECT EXISTS(SELECT 1 FROM administrators LIMIT 1)`

	var hasAdmins bool
	err := s.db.QueryRowContext(ctx, query).Scan(&hasAdmins)
	if err != nil {
		logger.Error("Failed to check if any administrators exist: %v", err)
		return false, fmt.Errorf("failed to check administrators: %w", err)
	}

	logger.Debug("HasAnyAdministrators: %t", hasAdmins)
	return hasAdmins, nil
}

// GetUserEmail retrieves email for a user_id (for enrichment in list responses)
func (s *AdministratorDatabaseStore) GetUserEmail(ctx context.Context, userID uuid.UUID) (string, error) {
	logger := slogging.Get()

	query := `SELECT email FROM users WHERE id = $1`

	var email string
	err := s.db.QueryRowContext(ctx, query, userID).Scan(&email)
	if err == sql.ErrNoRows {
		logger.Debug("User not found for email lookup: id=%s", userID)
		return "", nil // Return empty string if user not found
	}
	if err != nil {
		logger.Error("Failed to get user email: id=%s, error=%v", userID, err)
		return "", fmt.Errorf("failed to get user email: %w", err)
	}

	return email, nil
}

// GetGroupName retrieves name for a group_id (for enrichment in list responses)
func (s *AdministratorDatabaseStore) GetGroupName(ctx context.Context, groupID uuid.UUID, provider string) (string, error) {
	logger := slogging.Get()

	query := `SELECT group_name FROM groups WHERE internal_uuid = $1 AND provider = $2`

	var groupName string
	err := s.db.QueryRowContext(ctx, query, groupID, provider).Scan(&groupName)
	if err == sql.ErrNoRows {
		logger.Debug("Group not found for name lookup: id=%s, provider=%s", groupID, provider)
		return "", nil // Return empty string if group not found
	}
	if err != nil {
		logger.Error("Failed to get group name: id=%s, provider=%s, error=%v", groupID, provider, err)
		return "", fmt.Errorf("failed to get group name: %w", err)
	}

	return groupName, nil
}

// EnrichAdministrators adds user_email and group_name to administrator records
func (s *AdministratorDatabaseStore) EnrichAdministrators(ctx context.Context, admins []Administrator) ([]Administrator, error) {
	logger := slogging.Get()

	enriched := make([]Administrator, len(admins))
	for i, admin := range admins {
		enriched[i] = admin

		// Enrich user-based grants with email
		if admin.UserInternalUUID != nil {
			email, err := s.GetUserEmail(ctx, *admin.UserInternalUUID)
			if err != nil {
				logger.Warn("Failed to enrich user email for admin %s: %v", admin.ID, err)
				// Continue with empty email rather than failing
			}
			enriched[i].UserEmail = email
		}

		// Enrich group-based grants with group name
		if admin.GroupInternalUUID != nil {
			groupName, err := s.GetGroupName(ctx, *admin.GroupInternalUUID, admin.Provider)
			if err != nil {
				logger.Warn("Failed to enrich group name for admin %s: %v", admin.ID, err)
				// Continue with empty group name rather than failing
			}
			enriched[i].GroupName = groupName
		}
	}

	logger.Debug("Enriched %d administrator records", len(enriched))
	return enriched, nil
}

// AdminCheckerAdapter adapts AdministratorDatabaseStore to the auth.AdminChecker interface
type AdminCheckerAdapter struct {
	store *AdministratorDatabaseStore
}

// NewAdminCheckerAdapter creates a new adapter for the auth.AdminChecker interface
func NewAdminCheckerAdapter(store *AdministratorDatabaseStore) *AdminCheckerAdapter {
	return &AdminCheckerAdapter{store: store}
}

// IsAdmin checks if a user is an administrator (implements auth.AdminChecker)
func (a *AdminCheckerAdapter) IsAdmin(ctx context.Context, userInternalUUID *string, provider string, groupUUIDs []string) (bool, error) {
	// Convert string UUID to uuid.UUID pointer
	var userUUID *uuid.UUID
	if userInternalUUID != nil && *userInternalUUID != "" {
		parsed, err := uuid.Parse(*userInternalUUID)
		if err != nil {
			return false, fmt.Errorf("invalid user UUID: %w", err)
		}
		userUUID = &parsed
	}

	// Convert string UUIDs to uuid.UUID slice
	uuids := make([]uuid.UUID, 0, len(groupUUIDs))
	for _, uuidStr := range groupUUIDs {
		parsed, err := uuid.Parse(uuidStr)
		if err != nil {
			return false, fmt.Errorf("invalid group UUID %s: %w", uuidStr, err)
		}
		uuids = append(uuids, parsed)
	}

	return a.store.IsAdmin(ctx, userUUID, provider, uuids)
}

// GetGroupUUIDsByNames converts group names to UUIDs (implements auth.AdminChecker)
func (a *AdminCheckerAdapter) GetGroupUUIDsByNames(ctx context.Context, provider string, groupNames []string) ([]string, error) {
	uuids, err := a.store.GetGroupUUIDsByNames(ctx, provider, groupNames)
	if err != nil {
		return nil, err
	}

	// Convert uuid.UUID slice to string slice
	result := make([]string, len(uuids))
	for i, u := range uuids {
		result[i] = u.String()
	}
	return result, nil
}
