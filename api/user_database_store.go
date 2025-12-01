package api

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
)

// UserDatabaseStore implements UserStore using PostgreSQL
type UserDatabaseStore struct {
	db          *sql.DB
	authService *auth.Service
}

// NewUserDatabaseStore creates a new database-backed user store
func NewUserDatabaseStore(db *sql.DB, authService *auth.Service) *UserDatabaseStore {
	return &UserDatabaseStore{
		db:          db,
		authService: authService,
	}
}

// List returns users with optional filtering and pagination
func (s *UserDatabaseStore) List(ctx context.Context, filter UserFilter) ([]AdminUser, error) {
	logger := slogging.Get()

	// Build query with filters
	query := `SELECT internal_uuid, provider, provider_user_id, email, name, email_verified,
	          created_at, modified_at, last_login FROM users WHERE 1=1`
	args := []interface{}{}
	argPos := 1

	// Apply filters
	if filter.Provider != "" {
		query += fmt.Sprintf(" AND provider = $%d", argPos)
		args = append(args, filter.Provider)
		argPos++
	}

	if filter.Email != "" {
		query += fmt.Sprintf(" AND LOWER(email) LIKE LOWER($%d)", argPos)
		args = append(args, "%"+filter.Email+"%")
		argPos++
	}

	if filter.CreatedAfter != nil {
		query += fmt.Sprintf(" AND created_at >= $%d", argPos)
		args = append(args, *filter.CreatedAfter)
		argPos++
	}

	if filter.CreatedBefore != nil {
		query += fmt.Sprintf(" AND created_at <= $%d", argPos)
		args = append(args, *filter.CreatedBefore)
		argPos++
	}

	if filter.LastLoginAfter != nil {
		query += fmt.Sprintf(" AND last_login >= $%d", argPos)
		args = append(args, *filter.LastLoginAfter)
		argPos++
	}

	if filter.LastLoginBefore != nil {
		query += fmt.Sprintf(" AND last_login <= $%d", argPos)
		args = append(args, *filter.LastLoginBefore)
		argPos++
	}

	// Apply sorting
	sortBy := "created_at"
	if filter.SortBy != "" {
		switch filter.SortBy {
		case "created_at", "last_login", "email":
			sortBy = filter.SortBy
		default:
			logger.Warn("Invalid sort_by value: %s, using default: created_at", filter.SortBy)
		}
	}

	sortOrder := "DESC"
	if filter.SortOrder != "" {
		switch strings.ToUpper(filter.SortOrder) {
		case "ASC":
			sortOrder = "ASC"
		case "DESC":
			sortOrder = "DESC"
		default:
			logger.Warn("Invalid sort_order value: %s, using default: DESC", filter.SortOrder)
		}
	}

	query += fmt.Sprintf(" ORDER BY %s %s", sortBy, sortOrder)

	// Apply pagination
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argPos)
		args = append(args, filter.Limit)
		argPos++
	}

	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argPos)
		args = append(args, filter.Offset)
		// argPos++ // Last use of argPos, no need to increment
	}

	// Execute query
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query users: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			logger.Error("Failed to close rows: %v", cerr)
		}
	}()

	// Scan results
	users := []AdminUser{}
	for rows.Next() {
		var user AdminUser
		var lastLogin sql.NullTime

		err := rows.Scan(
			&user.InternalUuid,
			&user.Provider,
			&user.ProviderUserId,
			&user.Email,
			&user.Name,
			&user.EmailVerified,
			&user.CreatedAt,
			&user.ModifiedAt,
			&lastLogin,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user: %w", err)
		}

		if lastLogin.Valid {
			user.LastLogin = &lastLogin.Time
		}

		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating users: %w", err)
	}

	return users, nil
}

// Get retrieves a user by internal UUID
func (s *UserDatabaseStore) Get(ctx context.Context, internalUUID uuid.UUID) (*AdminUser, error) {
	query := `SELECT internal_uuid, provider, provider_user_id, email, name, email_verified,
	          created_at, modified_at, last_login FROM users WHERE internal_uuid = $1`

	var user AdminUser
	var lastLogin sql.NullTime

	err := s.db.QueryRowContext(ctx, query, internalUUID).Scan(
		&user.InternalUuid,
		&user.Provider,
		&user.ProviderUserId,
		&user.Email,
		&user.Name,
		&user.EmailVerified,
		&user.CreatedAt,
		&user.ModifiedAt,
		&lastLogin,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	if lastLogin.Valid {
		user.LastLogin = &lastLogin.Time
	}

	return &user, nil
}

// GetByProviderAndID retrieves a user by provider and provider_user_id
func (s *UserDatabaseStore) GetByProviderAndID(ctx context.Context, provider string, providerUserID string) (*AdminUser, error) {
	query := `SELECT internal_uuid, provider, provider_user_id, email, name, email_verified,
	          created_at, modified_at, last_login FROM users WHERE provider = $1 AND provider_user_id = $2`

	var user AdminUser
	var lastLogin sql.NullTime

	err := s.db.QueryRowContext(ctx, query, provider, providerUserID).Scan(
		&user.InternalUuid,
		&user.Provider,
		&user.ProviderUserId,
		&user.Email,
		&user.Name,
		&user.EmailVerified,
		&user.CreatedAt,
		&user.ModifiedAt,
		&lastLogin,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	if lastLogin.Valid {
		user.LastLogin = &lastLogin.Time
	}

	return &user, nil
}

// Update updates user metadata (email, name, email_verified)
func (s *UserDatabaseStore) Update(ctx context.Context, user AdminUser) error {
	query := `UPDATE users SET email = $1, name = $2, email_verified = $3, modified_at = $4
	          WHERE internal_uuid = $5`

	result, err := s.db.ExecContext(ctx, query,
		user.Email,
		user.Name,
		user.EmailVerified,
		time.Now().UTC(),
		user.InternalUuid,
	)
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("user not found")
	}

	return nil
}

// Delete deletes a user by provider and provider_user_id
func (s *UserDatabaseStore) Delete(ctx context.Context, provider string, providerUserID string) (*DeletionStats, error) {
	logger := slogging.Get()

	// First, get the user to find their email
	user, err := s.GetByProviderAndID(ctx, provider, providerUserID)
	if err != nil {
		return nil, fmt.Errorf("failed to find user: %w", err)
	}

	// Delegate to auth service DeleteUserAndData (same as DELETE /users/me)
	result, err := s.authService.DeleteUserAndData(ctx, string(user.Email))
	if err != nil {
		return nil, fmt.Errorf("failed to delete user: %w", err)
	}

	logger.Info("[AUDIT] Admin user deletion: provider=%s, provider_user_id=%s, email=%s, transferred=%d, deleted=%d",
		provider, providerUserID, string(user.Email), result.ThreatModelsTransferred, result.ThreatModelsDeleted)

	return &DeletionStats{
		ThreatModelsTransferred: result.ThreatModelsTransferred,
		ThreatModelsDeleted:     result.ThreatModelsDeleted,
		UserEmail:               result.UserEmail,
	}, nil
}

// Count returns total count of users matching the filter
func (s *UserDatabaseStore) Count(ctx context.Context, filter UserFilter) (int, error) {
	query := `SELECT COUNT(*) FROM users WHERE 1=1`
	args := []interface{}{}
	argPos := 1

	// Apply same filters as List (excluding pagination)
	if filter.Provider != "" {
		query += fmt.Sprintf(" AND provider = $%d", argPos)
		args = append(args, filter.Provider)
		argPos++
	}

	if filter.Email != "" {
		query += fmt.Sprintf(" AND LOWER(email) LIKE LOWER($%d)", argPos)
		args = append(args, "%"+filter.Email+"%")
		argPos++
	}

	if filter.CreatedAfter != nil {
		query += fmt.Sprintf(" AND created_at >= $%d", argPos)
		args = append(args, *filter.CreatedAfter)
		argPos++
	}

	if filter.CreatedBefore != nil {
		query += fmt.Sprintf(" AND created_at <= $%d", argPos)
		args = append(args, *filter.CreatedBefore)
		argPos++
	}

	if filter.LastLoginAfter != nil {
		query += fmt.Sprintf(" AND last_login >= $%d", argPos)
		args = append(args, *filter.LastLoginAfter)
		argPos++
	}

	if filter.LastLoginBefore != nil {
		query += fmt.Sprintf(" AND last_login <= $%d", argPos)
		args = append(args, *filter.LastLoginBefore)
		// argPos++ // Last use of argPos before non-parameterized query
	}

	var count int
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count users: %w", err)
	}

	return count, nil
}

// EnrichUsers adds related data to users (admin status, groups, threat model counts)
func (s *UserDatabaseStore) EnrichUsers(ctx context.Context, users []AdminUser) ([]AdminUser, error) {
	logger := slogging.Get()

	if len(users) == 0 {
		return users, nil
	}

	enriched := make([]AdminUser, len(users))
	copy(enriched, users)

	for i := range enriched {
		user := &enriched[i]

		// Check admin status
		isAdmin, err := GlobalAdministratorStore.IsAdmin(ctx, &user.InternalUuid, user.Provider, nil)
		if err != nil {
			logger.Warn("Failed to check admin status for user %s: %v", user.InternalUuid, err)
		} else {
			user.IsAdmin = &isAdmin
		}

		// Count active threat models owned by user
		countQuery := `SELECT COUNT(*) FROM threat_models WHERE owner_internal_uuid = $1`
		var count int
		err = s.db.QueryRowContext(ctx, countQuery, user.InternalUuid).Scan(&count)
		if err != nil {
			logger.Warn("Failed to count threat models for user %s: %v", user.InternalUuid, err)
		} else {
			user.ActiveThreatModels = &count
		}

		// Note: Groups are not stored in database, they come from JWT claims
		// For enrichment, we would need to query the IdP or use cached data
		// Leave groups empty for now - they can be populated from JWT in handlers
	}

	return enriched, nil
}
