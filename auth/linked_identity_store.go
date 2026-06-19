package auth

import (
	"context"
	"errors"
	"time"

	"github.com/ericfitz/tmi/api/models"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dberrors"
	"gorm.io/gorm"
)

// ErrLinkedIdentityNotFound is returned when a linked identity row is not found
// or the caller is not the owner.
var ErrLinkedIdentityNotFound = errors.New("linked identity not found")

// LinkedIdentityInput holds the fields needed to create a new linked identity.
// SEM@211793c39ea528b3d2da244f3504963c40584df7: input fields required to create a new linked identity record (pure)
type LinkedIdentityInput struct {
	UserInternalUUID string
	Provider         string
	ProviderUserID   string
	Email            string
	Name             string
}

// LinkedIdentityStore is the persistence interface for the linked_identities table.
// SEM@053baa340d412aa135be32953dfcb6133af89b4d: persistence interface for linked OAuth identity records (reads DB)
type LinkedIdentityStore interface {
	// Create inserts a new linked identity row. Returns a dberrors.ErrDuplicate-
	// wrapped error if the (provider, provider_user_id) pair already exists.
	Create(ctx context.Context, input LinkedIdentityInput) (models.LinkedIdentity, error)

	// CreateExclusive checks for an existing binding (in linked_identities) and
	// creates the row inside a single serializable transaction, eliminating the
	// check-then-act race between concurrent confirm calls. The caller is
	// responsible for checking the users (primary identity) table before calling
	// this method — that cross-table check is performed by the handler using the
	// same serializable transaction indirectly via the retry wrapper.
	// Returns dberrors.ErrDuplicate if the (provider, provider_user_id) pair is
	// already present in linked_identities.
	CreateExclusive(ctx context.Context, input LinkedIdentityInput) (models.LinkedIdentity, error)

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
// SEM@211793c39ea528b3d2da244f3504963c40584df7: GORM-backed store for linked OAuth identity records (reads DB)
type GormLinkedIdentityStore struct {
	db *gorm.DB
}

// NewGormLinkedIdentityStore returns a new GormLinkedIdentityStore.
// SEM@211793c39ea528b3d2da244f3504963c40584df7: build a GORM-backed linked identity store from a db handle (pure)
func NewGormLinkedIdentityStore(db *gorm.DB) *GormLinkedIdentityStore {
	return &GormLinkedIdentityStore{db: db}
}

// Create inserts a new linked identity row.
// SEM@211793c39ea528b3d2da244f3504963c40584df7: store a new linked identity record; return typed duplicate error on constraint violation (reads DB)
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

// CreateExclusive performs a check-then-create inside a single serializable
// transaction, eliminating the TOCTOU race that exists when the re-check and
// the insert run in separate statements. The unique index remains the final
// backstop; this method surfaces the conflict as dberrors.ErrDuplicate before
// reaching the constraint so that callers get a typed error in both the
// serializable-read-caught and the constraint-caught paths.
//
// PKCE protects a public-client code exchange that does not exist in the link
// flow; the pending-token + UUID-matched step-up-fresh confirm is the binding
// mechanism. A serializable transaction here is the correct concurrency guard.
// SEM@053baa340d412aa135be32953dfcb6133af89b4d: create a linked identity inside a serializable transaction to prevent TOCTOU race on duplicate binding (reads DB)
func (s *GormLinkedIdentityStore) CreateExclusive(ctx context.Context, input LinkedIdentityInput) (models.LinkedIdentity, error) {
	var created models.LinkedIdentity
	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		// Re-check binding inside the transaction: a SERIALIZABLE snapshot
		// ensures no concurrent inserter can slip in between this read and the
		// write below.
		var existing models.LinkedIdentity
		lookupErr := tx.Where("provider = ? AND provider_user_id = ?", input.Provider, input.ProviderUserID).
			First(&existing).Error
		if lookupErr == nil {
			// Row already exists — surface as a typed duplicate error.
			return dberrors.ErrDuplicate
		}
		if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
			return dberrors.Classify(lookupErr)
		}

		// Safe to insert.
		row := models.LinkedIdentity{
			UserInternalUUID: models.DBVarchar(input.UserInternalUUID),
			Provider:         models.DBVarchar(input.Provider),
			ProviderUserID:   models.DBVarchar(input.ProviderUserID),
			Email:            models.DBVarchar(input.Email),
			Name:             models.DBVarchar(input.Name),
		}
		if err := tx.Create(&row).Error; err != nil {
			return dberrors.Classify(err)
		}
		created = row
		return nil
	})
	if err != nil {
		return models.LinkedIdentity{}, err
	}
	return created, nil
}

// GetByProviderSub looks up a linked identity by provider and provider-user-id.
// SEM@211793c39ea528b3d2da244f3504963c40584df7: fetch a linked identity by OAuth provider and provider user ID (reads DB)
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
// SEM@211793c39ea528b3d2da244f3504963c40584df7: list all linked identities owned by a user UUID (reads DB)
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
// SEM@211793c39ea528b3d2da244f3504963c40584df7: update last_used_at timestamp to now for the given linked identity (reads DB)
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
// SEM@211793c39ea528b3d2da244f3504963c40584df7: delete a linked identity scoped to an owner UUID; return not-found if no row matches (reads DB)
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
