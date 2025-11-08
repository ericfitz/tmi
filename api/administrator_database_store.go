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

	query := `
		INSERT INTO administrators (user_id, subject, subject_type, granted_at, granted_by, notes)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (user_id, subject, subject_type) DO UPDATE
		SET granted_at = EXCLUDED.granted_at,
			granted_by = EXCLUDED.granted_by,
			notes = EXCLUDED.notes
	`

	_, err := s.db.ExecContext(ctx, query,
		admin.UserID,
		admin.Subject,
		admin.SubjectType,
		admin.GrantedAt,
		admin.GrantedBy,
		admin.Notes,
	)

	if err != nil {
		logger.Error("Failed to create administrator entry: subject=%s, type=%s, error=%v",
			admin.Subject, admin.SubjectType, err)
		return fmt.Errorf("failed to create administrator: %w", err)
	}

	logger.Info("Administrator created: subject=%s, type=%s, user_id=%s",
		admin.Subject, admin.SubjectType, admin.UserID)

	return nil
}

// Delete removes an administrator entry
func (s *AdministratorDatabaseStore) Delete(ctx context.Context, userID uuid.UUID, subject string, subjectType string) error {
	logger := slogging.Get()

	query := `
		DELETE FROM administrators
		WHERE user_id = $1 AND subject = $2 AND subject_type = $3
	`

	result, err := s.db.ExecContext(ctx, query, userID, subject, subjectType)
	if err != nil {
		logger.Error("Failed to delete administrator entry: subject=%s, type=%s, error=%v",
			subject, subjectType, err)
		return fmt.Errorf("failed to delete administrator: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		logger.Error("Failed to get rows affected for delete: %v", err)
		return fmt.Errorf("failed to verify delete: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("administrator not found: subject=%s, type=%s", subject, subjectType)
	}

	logger.Info("Administrator deleted: subject=%s, type=%s, user_id=%s",
		subject, subjectType, userID)

	return nil
}

// List returns all administrator entries
func (s *AdministratorDatabaseStore) List(ctx context.Context) ([]Administrator, error) {
	logger := slogging.Get()

	query := `
		SELECT user_id, subject, subject_type, granted_at, granted_by, notes
		FROM administrators
		ORDER BY granted_at DESC, subject ASC
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
		var grantedBy sql.NullString
		var notes sql.NullString

		err := rows.Scan(
			&admin.UserID,
			&admin.Subject,
			&admin.SubjectType,
			&admin.GrantedAt,
			&grantedBy,
			&notes,
		)
		if err != nil {
			logger.Error("Failed to scan administrator row: %v", err)
			return nil, fmt.Errorf("failed to scan administrator: %w", err)
		}

		// Handle nullable fields
		if grantedBy.Valid {
			grantedByUUID, err := uuid.Parse(grantedBy.String)
			if err == nil {
				admin.GrantedBy = &grantedByUUID
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

// IsAdmin checks if a user (by email or UUID) or any of their groups is an administrator
func (s *AdministratorDatabaseStore) IsAdmin(ctx context.Context, userID *uuid.UUID, email string, groups []string) (bool, error) {
	logger := slogging.Get()

	// Build query to check:
	// 1. User by email (subject_type='user' AND subject=email)
	// 2. User by UUID (subject_type='user' AND user_id=uuid)
	// 3. Any group membership (subject_type='group' AND subject IN groups)

	query := `
		SELECT EXISTS (
			SELECT 1 FROM administrators
			WHERE (subject_type = 'user' AND (subject = $1 OR ($2::uuid IS NOT NULL AND user_id = $2)))
			   OR (subject_type = 'group' AND subject = ANY($3))
		)
	`

	var isAdmin bool
	err := s.db.QueryRowContext(ctx, query, email, userID, pqStringArray(groups)).Scan(&isAdmin)
	if err != nil {
		logger.Error("Failed to check admin status for email=%s, user_id=%v, groups=%v: %v",
			email, userID, groups, err)
		return false, fmt.Errorf("failed to check admin status: %w", err)
	}

	logger.Debug("Admin check: email=%s, user_id=%v, groups=%v, is_admin=%t",
		email, userID, groups, isAdmin)

	return isAdmin, nil
}

// GetBySubject retrieves administrator entries by subject (email or group)
func (s *AdministratorDatabaseStore) GetBySubject(ctx context.Context, subject string) ([]Administrator, error) {
	logger := slogging.Get()

	query := `
		SELECT user_id, subject, subject_type, granted_at, granted_by, notes
		FROM administrators
		WHERE subject = $1
		ORDER BY granted_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, subject)
	if err != nil {
		logger.Error("Failed to get administrators by subject=%s: %v", subject, err)
		return nil, fmt.Errorf("failed to get administrators by subject: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Error("Failed to close rows: %v", closeErr)
		}
	}()

	var administrators []Administrator
	for rows.Next() {
		var admin Administrator
		var grantedBy sql.NullString
		var notes sql.NullString

		err := rows.Scan(
			&admin.UserID,
			&admin.Subject,
			&admin.SubjectType,
			&admin.GrantedAt,
			&grantedBy,
			&notes,
		)
		if err != nil {
			logger.Error("Failed to scan administrator row: %v", err)
			return nil, fmt.Errorf("failed to scan administrator: %w", err)
		}

		// Handle nullable fields
		if grantedBy.Valid {
			grantedByUUID, err := uuid.Parse(grantedBy.String)
			if err == nil {
				admin.GrantedBy = &grantedByUUID
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

	logger.Debug("Found %d administrators for subject=%s", len(administrators), subject)

	return administrators, nil
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
