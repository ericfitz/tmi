package repository

import (
	"context"
	"errors"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GormClientCredentialRepository implements ClientCredentialRepository using GORM
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: GORM-backed store for client credential CRUD operations (reads DB)
type GormClientCredentialRepository struct {
	db     *gorm.DB
	logger *slogging.Logger
}

// NewGormClientCredentialRepository creates a new GORM-backed client credential repository
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: build a GORM client credential repository wrapping the given database connection (pure)
func NewGormClientCredentialRepository(db *gorm.DB) *GormClientCredentialRepository {
	return &GormClientCredentialRepository{
		db:     db,
		logger: slogging.Get(),
	}
}

// Create creates a new client credential
// SEM@5dfa9dcf64aa0662920dbbab3bca200db1b22c73: store a new client credential record and return the persisted entity (reads DB)
func (r *GormClientCredentialRepository) Create(ctx context.Context, params ClientCredentialCreateParams) (*ClientCredential, error) {
	now := time.Now()

	gormCred := &models.ClientCredential{
		ID:               models.DBVarchar(uuid.New().String()),
		OwnerUUID:        models.DBVarchar(params.OwnerUUID.String()),
		ClientID:         models.DBVarchar(params.ClientID),
		ClientSecretHash: models.DBText(params.ClientSecretHash),
		Name:             models.DBVarchar(params.Name),
		Description:      models.NewNullableDBText(&params.Description),
		IsActive:         models.DBBool(true),
		CreatedAt:        now,
		ModifiedAt:       now,
		ExpiresAt:        params.ExpiresAt,
	}

	result := r.db.WithContext(ctx).Create(gormCred)
	if result.Error != nil {
		return nil, dberrors.Classify(result.Error)
	}

	return convertModelToClientCredential(gormCred), nil
}

// GetByClientID retrieves an active client credential by client ID
// SEM@8077d4387088ee7e6e22cce2171ad54ee850e10b: fetch an active client credential by client ID; error if not found (reads DB)
func (r *GormClientCredentialRepository) GetByClientID(ctx context.Context, clientID string) (*ClientCredential, error) {
	var gormCred models.ClientCredential
	result := r.db.WithContext(ctx).
		Where("client_id = ? AND is_active = ?", clientID, true).
		First(&gormCred)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrClientCredentialNotFound
		}
		return nil, dberrors.Classify(result.Error)
	}

	return convertModelToClientCredential(&gormCred), nil
}

// ListByOwner retrieves all client credentials owned by a user
// SEM@8077d4387088ee7e6e22cce2171ad54ee850e10b: list all client credentials owned by a user, ordered by creation time (reads DB)
func (r *GormClientCredentialRepository) ListByOwner(ctx context.Context, ownerUUID uuid.UUID) ([]*ClientCredential, error) {
	var gormCreds []models.ClientCredential
	result := r.db.WithContext(ctx).
		Where("owner_uuid = ?", ownerUUID.String()).
		Order("created_at DESC").
		Find(&gormCreds)

	if result.Error != nil {
		return nil, dberrors.Classify(result.Error)
	}

	credentials := make([]*ClientCredential, 0, len(gormCreds))
	for i := range gormCreds {
		credentials = append(credentials, convertModelToClientCredential(&gormCreds[i]))
	}

	return credentials, nil
}

// UpdateLastUsed updates the last_used_at timestamp for a client credential
// SEM@8077d4387088ee7e6e22cce2171ad54ee850e10b: update the last-used timestamp for a client credential by ID (reads DB)
func (r *GormClientCredentialRepository) UpdateLastUsed(ctx context.Context, id uuid.UUID) error {
	result := r.db.WithContext(ctx).Model(&models.ClientCredential{}).
		Where("id = ?", id.String()).
		Update("last_used_at", time.Now())

	if result.Error != nil {
		return dberrors.Classify(result.Error)
	}

	if result.RowsAffected == 0 {
		return ErrClientCredentialNotFound
	}

	return nil
}

// Deactivate deactivates a client credential (soft delete)
// SEM@8077d4387088ee7e6e22cce2171ad54ee850e10b: soft-delete a client credential by marking it inactive, scoped to owner (reads DB)
func (r *GormClientCredentialRepository) Deactivate(ctx context.Context, id, ownerUUID uuid.UUID) error {
	result := r.db.WithContext(ctx).Model(&models.ClientCredential{}).
		Where("id = ? AND owner_uuid = ?", id.String(), ownerUUID.String()).
		Updates(map[string]any{
			"is_active":   false,
			"modified_at": time.Now(),
		})

	if result.Error != nil {
		return dberrors.Classify(result.Error)
	}

	if result.RowsAffected == 0 {
		return ErrClientCredentialNotFound
	}

	return nil
}

// Delete permanently deletes a client credential
// SEM@8077d4387088ee7e6e22cce2171ad54ee850e10b: permanently delete a client credential record scoped to its owner (reads DB)
func (r *GormClientCredentialRepository) Delete(ctx context.Context, id, ownerUUID uuid.UUID) error {
	result := r.db.WithContext(ctx).
		Where("id = ? AND owner_uuid = ?", id.String(), ownerUUID.String()).
		Delete(&models.ClientCredential{})

	if result.Error != nil {
		return dberrors.Classify(result.Error)
	}

	if result.RowsAffected == 0 {
		return ErrClientCredentialNotFound
	}

	return nil
}

// convertModelToClientCredential converts a GORM ClientCredential model to a repository ClientCredential
// SEM@5dfa9dcf64aa0662920dbbab3bca200db1b22c73: convert a GORM ClientCredential model to the repository domain type (pure)
func convertModelToClientCredential(m *models.ClientCredential) *ClientCredential {
	id, _ := uuid.Parse(string(m.ID))
	ownerUUID, _ := uuid.Parse(string(m.OwnerUUID))

	description := m.Description.String

	return &ClientCredential{
		ID:               id,
		OwnerUUID:        ownerUUID,
		ClientID:         string(m.ClientID),
		ClientSecretHash: string(m.ClientSecretHash), // Convert DBText to string
		Name:             string(m.Name),
		Description:      description,
		IsActive:         m.IsActive.Bool(), // Convert DBBool to bool
		LastUsedAt:       m.LastUsedAt,
		CreatedAt:        m.CreatedAt,
		ModifiedAt:       m.ModifiedAt,
		ExpiresAt:        m.ExpiresAt,
	}
}
