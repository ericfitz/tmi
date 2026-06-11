package auth

import (
	"context"
	"errors"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"gorm.io/gorm"
)

// ErrLinkedIdentityNotFound is returned when a linked identity row is not found
// or the caller is not the owner.
var ErrLinkedIdentityNotFound = errors.New("linked identity not found")

// LinkedIdentityInput holds the fields needed to create a new linked identity.
type LinkedIdentityInput struct {
	UserInternalUUID string
	Provider         string
	ProviderUserID   string
	Email            string
	Name             string
}

// LinkedIdentityStore is the persistence interface for the linked_identities table.
type LinkedIdentityStore interface {
	// Create inserts a new linked identity row. Returns a dberrors.ErrDuplicate-
	// wrapped error if the (provider, provider_user_id) pair already exists.
	Create(ctx context.Context, input LinkedIdentityInput) (models.LinkedIdentity, error)

	// GetByProviderSub looks up a linked identity by provider and provider-user-id.
	// Returns ErrLinkedIdentityNotFound if no row matches.
	GetByProviderSub(ctx context.Context, provider, providerUserID string) (models.LinkedIdentity, error)

	// ListByUser returns all linked identities owned by userInternalUUID.
	// Returns an empty slice (not an error) when none exist.
	ListByUser(ctx context.Context, userInternalUUID string) ([]models.LinkedIdentity, error)

	// TouchLastUsed updates last_used_at to now for the given identity id.
	TouchLastUsed(ctx context.Context, id string) error

	// Delete removes the linked identity identified by id, scoped to ownerUUID.
	// Returns ErrLinkedIdentityNotFound if no row matches both id and ownerUUID.
	Delete(ctx context.Context, id, ownerUUID string) error
}

// GormLinkedIdentityStore is the GORM-backed implementation of LinkedIdentityStore.
type GormLinkedIdentityStore struct {
	db *gorm.DB
}

// NewGormLinkedIdentityStore returns a new GormLinkedIdentityStore.
func NewGormLinkedIdentityStore(db *gorm.DB) *GormLinkedIdentityStore {
	return &GormLinkedIdentityStore{db: db}
}

// Create inserts a new linked identity row.
func (s *GormLinkedIdentityStore) Create(ctx context.Context, input LinkedIdentityInput) (models.LinkedIdentity, error) {
	row := models.LinkedIdentity{
		UserInternalUUID: models.DBVarchar(input.UserInternalUUID),
		Provider:         models.DBVarchar(input.Provider),
		ProviderUserID:   models.DBVarchar(input.ProviderUserID),
		Email:            models.DBVarchar(input.Email),
		Name:             models.DBVarchar(input.Name),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return models.LinkedIdentity{}, dberrors.Classify(err)
	}
	return row, nil
}

// GetByProviderSub looks up a linked identity by provider and provider-user-id.
func (s *GormLinkedIdentityStore) GetByProviderSub(ctx context.Context, provider, providerUserID string) (models.LinkedIdentity, error) {
	var row models.LinkedIdentity
	err := s.db.WithContext(ctx).
		Where("provider = ? AND provider_user_id = ?", provider, providerUserID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.LinkedIdentity{}, ErrLinkedIdentityNotFound
		}
		return models.LinkedIdentity{}, dberrors.Classify(err)
	}
	return row, nil
}

// ListByUser returns all linked identities owned by userInternalUUID.
func (s *GormLinkedIdentityStore) ListByUser(ctx context.Context, userInternalUUID string) ([]models.LinkedIdentity, error) {
	var rows []models.LinkedIdentity
	if err := s.db.WithContext(ctx).
		Where("user_internal_uuid = ?", userInternalUUID).
		Find(&rows).Error; err != nil {
		return nil, dberrors.Classify(err)
	}
	return rows, nil
}

// TouchLastUsed updates last_used_at to now for the given identity id.
func (s *GormLinkedIdentityStore) TouchLastUsed(ctx context.Context, id string) error {
	now := time.Now().UTC()
	if err := s.db.WithContext(ctx).
		Model(&models.LinkedIdentity{}).
		Where("id = ?", id).
		Update("last_used_at", now).Error; err != nil {
		return dberrors.Classify(err)
	}
	return nil
}

// Delete removes the linked identity identified by id, scoped to ownerUUID.
func (s *GormLinkedIdentityStore) Delete(ctx context.Context, id, ownerUUID string) error {
	result := s.db.WithContext(ctx).
		Where("id = ? AND user_internal_uuid = ?", id, ownerUUID).
		Delete(&models.LinkedIdentity{})
	if result.Error != nil {
		return dberrors.Classify(result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrLinkedIdentityNotFound
	}
	return nil
}
