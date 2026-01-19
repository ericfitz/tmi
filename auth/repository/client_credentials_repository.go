package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GormClientCredentialRepository implements ClientCredentialRepository using GORM
type GormClientCredentialRepository struct {
	db     *gorm.DB
	logger *slogging.Logger
}

// NewGormClientCredentialRepository creates a new GORM-backed client credential repository
func NewGormClientCredentialRepository(db *gorm.DB) *GormClientCredentialRepository {
	return &GormClientCredentialRepository{
		db:     db,
		logger: slogging.Get(),
	}
}

// Create creates a new client credential
func (r *GormClientCredentialRepository) Create(ctx context.Context, params ClientCredentialCreateParams) (*ClientCredential, error) {
	now := time.Now()

	gormCred := &models.ClientCredential{
		ID:               uuid.New().String(),
		OwnerUUID:        params.OwnerUUID.String(),
		ClientID:         params.ClientID,
		ClientSecretHash: models.DBText(params.ClientSecretHash),
		Name:             params.Name,
		Description:      &params.Description,
		IsActive:         models.DBBool(true),
		CreatedAt:        now,
		ModifiedAt:       now,
		ExpiresAt:        params.ExpiresAt,
	}

	result := r.db.WithContext(ctx).Create(gormCred)
	if result.Error != nil {
		return nil, fmt.Errorf("failed to create client credential: %w", result.Error)
	}

	return convertModelToClientCredential(gormCred), nil
}

// GetByClientID retrieves an active client credential by client ID
func (r *GormClientCredentialRepository) GetByClientID(ctx context.Context, clientID string) (*ClientCredential, error) {
	var gormCred models.ClientCredential
	result := r.db.WithContext(ctx).
		Where("client_id = ? AND is_active = ?", clientID, true).
		First(&gormCred)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, ErrClientCredentialNotFound
		}
		return nil, fmt.Errorf("failed to get client credential: %w", result.Error)
	}

	return convertModelToClientCredential(&gormCred), nil
}

// ListByOwner retrieves all client credentials owned by a user
func (r *GormClientCredentialRepository) ListByOwner(ctx context.Context, ownerUUID uuid.UUID) ([]*ClientCredential, error) {
	var gormCreds []models.ClientCredential
	result := r.db.WithContext(ctx).
		Where("owner_uuid = ?", ownerUUID.String()).
		Order("created_at DESC").
		Find(&gormCreds)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to list client credentials: %w", result.Error)
	}

	credentials := make([]*ClientCredential, 0, len(gormCreds))
	for i := range gormCreds {
		credentials = append(credentials, convertModelToClientCredential(&gormCreds[i]))
	}

	return credentials, nil
}

// UpdateLastUsed updates the last_used_at timestamp for a client credential
func (r *GormClientCredentialRepository) UpdateLastUsed(ctx context.Context, id uuid.UUID) error {
	result := r.db.WithContext(ctx).Model(&models.ClientCredential{}).
		Where("id = ?", id.String()).
		Update("last_used_at", time.Now())

	if result.Error != nil {
		return fmt.Errorf("failed to update last_used_at: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return ErrClientCredentialNotFound
	}

	return nil
}

// Deactivate deactivates a client credential (soft delete)
func (r *GormClientCredentialRepository) Deactivate(ctx context.Context, id, ownerUUID uuid.UUID) error {
	result := r.db.WithContext(ctx).Model(&models.ClientCredential{}).
		Where("id = ? AND owner_uuid = ?", id.String(), ownerUUID.String()).
		Updates(map[string]interface{}{
			"is_active":   false,
			"modified_at": time.Now(),
		})

	if result.Error != nil {
		return fmt.Errorf("failed to deactivate client credential: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("client credential not found or unauthorized")
	}

	return nil
}

// Delete permanently deletes a client credential
func (r *GormClientCredentialRepository) Delete(ctx context.Context, id, ownerUUID uuid.UUID) error {
	result := r.db.WithContext(ctx).
		Where("id = ? AND owner_uuid = ?", id.String(), ownerUUID.String()).
		Delete(&models.ClientCredential{})

	if result.Error != nil {
		return fmt.Errorf("failed to delete client credential: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("client credential not found or unauthorized")
	}

	return nil
}

// convertModelToClientCredential converts a GORM ClientCredential model to a repository ClientCredential
func convertModelToClientCredential(m *models.ClientCredential) *ClientCredential {
	id, _ := uuid.Parse(m.ID)
	ownerUUID, _ := uuid.Parse(m.OwnerUUID)

	description := ""
	if m.Description != nil {
		description = *m.Description
	}

	return &ClientCredential{
		ID:               id,
		OwnerUUID:        ownerUUID,
		ClientID:         m.ClientID,
		ClientSecretHash: string(m.ClientSecretHash), // Convert DBText to string
		Name:             m.Name,
		Description:      description,
		IsActive:         m.IsActive.Bool(), // Convert DBBool to bool
		LastUsedAt:       m.LastUsedAt,
		CreatedAt:        m.CreatedAt,
		ModifiedAt:       m.ModifiedAt,
		ExpiresAt:        m.ExpiresAt,
	}
}
