package auth

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ClientCredential represents an OAuth 2.0 client credential for machine-to-machine authentication
type ClientCredential struct {
	ID               uuid.UUID
	OwnerUUID        uuid.UUID
	ClientID         string
	ClientSecretHash string
	Name             string
	Description      string
	IsActive         bool
	LastUsedAt       *time.Time
	CreatedAt        time.Time
	ModifiedAt       time.Time
	ExpiresAt        *time.Time
}

// ClientCredentialCreateParams contains parameters for creating a new client credential
type ClientCredentialCreateParams struct {
	OwnerUUID        uuid.UUID
	ClientID         string
	ClientSecretHash string
	Name             string
	Description      string
	ExpiresAt        *time.Time
}

// CreateClientCredential creates a new client credential in the database
func (s *Service) CreateClientCredential(ctx context.Context, params ClientCredentialCreateParams) (*ClientCredential, error) {
	db := s.dbManager.Postgres().GetDB()

	now := time.Now()
	cred := &ClientCredential{
		ID:               uuid.New(),
		OwnerUUID:        params.OwnerUUID,
		ClientID:         params.ClientID,
		ClientSecretHash: params.ClientSecretHash,
		Name:             params.Name,
		Description:      params.Description,
		IsActive:         true,
		CreatedAt:        now,
		ModifiedAt:       now,
		ExpiresAt:        params.ExpiresAt,
	}

	query := `
		INSERT INTO client_credentials (id, owner_uuid, client_id, client_secret_hash, name, description, is_active, created_at, modified_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id
	`

	err := db.QueryRowContext(ctx, query,
		cred.ID,
		cred.OwnerUUID,
		cred.ClientID,
		cred.ClientSecretHash,
		cred.Name,
		cred.Description,
		cred.IsActive,
		cred.CreatedAt,
		cred.ModifiedAt,
		cred.ExpiresAt,
	).Scan(&cred.ID)

	if err != nil {
		return nil, fmt.Errorf("failed to create client credential: %w", err)
	}

	return cred, nil
}

// GetClientCredentialByClientID retrieves a client credential by its client_id
func (s *Service) GetClientCredentialByClientID(ctx context.Context, clientID string) (*ClientCredential, error) {
	db := s.dbManager.Postgres().GetDB()

	cred := &ClientCredential{}
	query := `
		SELECT id, owner_uuid, client_id, client_secret_hash, name, description, is_active, last_used_at, created_at, modified_at, expires_at
		FROM client_credentials
		WHERE client_id = $1 AND is_active = true
	`

	var lastUsedAt sql.NullTime
	var expiresAt sql.NullTime

	err := db.QueryRowContext(ctx, query, clientID).Scan(
		&cred.ID,
		&cred.OwnerUUID,
		&cred.ClientID,
		&cred.ClientSecretHash,
		&cred.Name,
		&cred.Description,
		&cred.IsActive,
		&lastUsedAt,
		&cred.CreatedAt,
		&cred.ModifiedAt,
		&expiresAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("client credential not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get client credential: %w", err)
	}

	if lastUsedAt.Valid {
		cred.LastUsedAt = &lastUsedAt.Time
	}
	if expiresAt.Valid {
		cred.ExpiresAt = &expiresAt.Time
	}

	return cred, nil
}

// ListClientCredentialsByOwner retrieves all client credentials for a given owner
func (s *Service) ListClientCredentialsByOwner(ctx context.Context, ownerUUID uuid.UUID) ([]*ClientCredential, error) {
	db := s.dbManager.Postgres().GetDB()

	query := `
		SELECT id, owner_uuid, client_id, client_secret_hash, name, description, is_active, last_used_at, created_at, modified_at, expires_at
		FROM client_credentials
		WHERE owner_uuid = $1
		ORDER BY created_at DESC
	`

	rows, err := db.QueryContext(ctx, query, ownerUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to list client credentials: %w", err)
	}
	defer func() {
		_ = rows.Close() // Error is intentionally ignored; closing is best-effort
	}()

	var credentials []*ClientCredential
	for rows.Next() {
		cred := &ClientCredential{}
		var lastUsedAt sql.NullTime
		var expiresAt sql.NullTime

		err := rows.Scan(
			&cred.ID,
			&cred.OwnerUUID,
			&cred.ClientID,
			&cred.ClientSecretHash,
			&cred.Name,
			&cred.Description,
			&cred.IsActive,
			&lastUsedAt,
			&cred.CreatedAt,
			&cred.ModifiedAt,
			&expiresAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan client credential: %w", err)
		}

		if lastUsedAt.Valid {
			cred.LastUsedAt = &lastUsedAt.Time
		}
		if expiresAt.Valid {
			cred.ExpiresAt = &expiresAt.Time
		}

		credentials = append(credentials, cred)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating client credentials: %w", err)
	}

	return credentials, nil
}

// UpdateClientCredentialLastUsed updates the last_used_at timestamp for a client credential
func (s *Service) UpdateClientCredentialLastUsed(ctx context.Context, id uuid.UUID) error {
	db := s.dbManager.Postgres().GetDB()

	query := `
		UPDATE client_credentials
		SET last_used_at = $2
		WHERE id = $1
	`

	result, err := db.ExecContext(ctx, query, id, time.Now())
	if err != nil {
		return fmt.Errorf("failed to update last_used_at: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("client credential not found")
	}

	return nil
}

// DeactivateClientCredential deactivates a client credential (soft delete)
func (s *Service) DeactivateClientCredential(ctx context.Context, id uuid.UUID, ownerUUID uuid.UUID) error {
	db := s.dbManager.Postgres().GetDB()

	query := `
		UPDATE client_credentials
		SET is_active = false, modified_at = $3
		WHERE id = $1 AND owner_uuid = $2
	`

	result, err := db.ExecContext(ctx, query, id, ownerUUID, time.Now())
	if err != nil {
		return fmt.Errorf("failed to deactivate client credential: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("client credential not found or unauthorized")
	}

	return nil
}

// DeleteClientCredential permanently deletes a client credential
func (s *Service) DeleteClientCredential(ctx context.Context, id uuid.UUID, ownerUUID uuid.UUID) error {
	db := s.dbManager.Postgres().GetDB()

	query := `
		DELETE FROM client_credentials
		WHERE id = $1 AND owner_uuid = $2
	`

	result, err := db.ExecContext(ctx, query, id, ownerUUID)
	if err != nil {
		return fmt.Errorf("failed to delete client credential: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("client credential not found or unauthorized")
	}

	return nil
}
