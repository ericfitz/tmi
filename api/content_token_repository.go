package api

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ContentToken is the domain representation of a per-user OAuth token used
// by delegated content providers. Access/refresh tokens are plaintext here;
// the repository handles encryption at rest.
type ContentToken struct {
	ID                   string
	UserID               string
	ProviderID           string
	AccessToken          string //nolint:gosec // G117 - domain type holds plaintext after repository decryption; not persisted directly
	RefreshToken         string //nolint:gosec // G117 - domain type holds plaintext after repository decryption; not persisted directly
	Scopes               string
	ExpiresAt            *time.Time
	Status               string // ContentTokenStatus*
	LastRefreshAt        *time.Time
	LastError            string
	ProviderAccountID    string
	ProviderAccountLabel string
	CreatedAt            time.Time
	ModifiedAt           time.Time
}

// ContentTokenStatus constants for the Status field of ContentToken.
const (
	ContentTokenStatusActive        = "active"
	ContentTokenStatusFailedRefresh = "failed_refresh"
)

// Typed errors for content-token repository operations.
// Each wraps the corresponding dberrors sentinel so handlers can check
// either the entity-specific error or the generic category.
var (
	ErrContentTokenNotFound  = fmt.Errorf("content token: %w", dberrors.ErrNotFound)
	ErrContentTokenDuplicate = fmt.Errorf("content token: %w", dberrors.ErrDuplicate)
)

// ContentTokenRepository is the repository abstraction over user_content_tokens.
// All methods return typed errors from internal/dberrors.
type ContentTokenRepository interface {
	// GetByUserAndProvider retrieves a token by user ID and provider ID.
	// Returns ErrContentTokenNotFound if no matching token exists.
	GetByUserAndProvider(ctx context.Context, userID, providerID string) (*ContentToken, error)

	// ListByUser returns all tokens for the given user ID.
	ListByUser(ctx context.Context, userID string) ([]ContentToken, error)

	// Upsert creates or updates a token for the (UserID, ProviderID) pair.
	// Returns ErrContentTokenDuplicate on an unexpected unique-key conflict.
	Upsert(ctx context.Context, token *ContentToken) error

	// UpdateStatus updates the status and last_error fields for the token with the given ID.
	// Returns ErrContentTokenNotFound if the token does not exist.
	UpdateStatus(ctx context.Context, id, status, lastError string) error

	// Delete removes the token with the given ID.
	// Returns ErrContentTokenNotFound if the token does not exist.
	Delete(ctx context.Context, id string) error

	// DeleteByUserAndProvider removes the token for the given user/provider pair and
	// returns the deleted token. Returns ErrContentTokenNotFound if it did not exist.
	DeleteByUserAndProvider(ctx context.Context, userID, providerID string) (*ContentToken, error)

	// RefreshWithLock opens a transaction, SELECT ... FOR UPDATE on the row,
	// invokes fn with the current decrypted token, and persists the returned
	// token. Returns the updated token or the fn error.
	RefreshWithLock(ctx context.Context, id string, fn func(current *ContentToken) (*ContentToken, error)) (*ContentToken, error)
}

// GormContentTokenRepository persists ContentToken records via GORM.
// AccessToken and RefreshToken are AES-256-GCM encrypted at rest.
type GormContentTokenRepository struct {
	db  *gorm.DB
	enc *ContentTokenEncryptor
}

// NewGormContentTokenRepository creates a new GORM-backed content-token repository.
func NewGormContentTokenRepository(db *gorm.DB, enc *ContentTokenEncryptor) *GormContentTokenRepository {
	return &GormContentTokenRepository{db: db, enc: enc}
}

// GetByUserAndProvider retrieves a token by user ID and provider ID.
func (r *GormContentTokenRepository) GetByUserAndProvider(ctx context.Context, userID, providerID string) (*ContentToken, error) {
	var row models.UserContentToken
	err := r.db.WithContext(ctx).Where("user_id = ? AND provider_id = ?", userID, providerID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrContentTokenNotFound
	}
	if err != nil {
		return nil, dberrors.Classify(err)
	}
	return r.decode(&row)
}

// ListByUser returns all tokens for the given user ID.
func (r *GormContentTokenRepository) ListByUser(ctx context.Context, userID string) ([]ContentToken, error) {
	var rows []models.UserContentToken
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&rows).Error; err != nil {
		return nil, dberrors.Classify(err)
	}
	out := make([]ContentToken, 0, len(rows))
	for i := range rows {
		t, err := r.decode(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, nil
}

// Upsert creates or updates the token for the (UserID, ProviderID) pair using
// ON CONFLICT DO UPDATE so the operation is idempotent.
func (r *GormContentTokenRepository) Upsert(ctx context.Context, token *ContentToken) error {
	row, err := r.encode(token)
	if err != nil {
		return err
	}
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "provider_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"access_token", "refresh_token", "scopes", "expires_at",
			"status", "last_refresh_at", "last_error",
			"provider_account_id", "provider_account_label", "modified_at",
		}),
	}).Create(row)
	if res.Error != nil {
		classified := dberrors.Classify(res.Error)
		if errors.Is(classified, dberrors.ErrDuplicate) {
			return ErrContentTokenDuplicate
		}
		return classified
	}
	// Back-fill the generated ID so the caller can use it immediately.
	token.ID = row.ID
	return nil
}

// UpdateStatus updates the status and last_error fields for the given token ID.
func (r *GormContentTokenRepository) UpdateStatus(ctx context.Context, id, status, lastError string) error {
	res := r.db.WithContext(ctx).Model(&models.UserContentToken{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": status, "last_error": lastError})
	if res.Error != nil {
		return dberrors.Classify(res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrContentTokenNotFound
	}
	return nil
}

// Delete removes the token with the given ID.
func (r *GormContentTokenRepository) Delete(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Delete(&models.UserContentToken{ID: id})
	if res.Error != nil {
		return dberrors.Classify(res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrContentTokenNotFound
	}
	return nil
}

// DeleteByUserAndProvider removes the token for the given user/provider pair and
// returns the deleted token.
func (r *GormContentTokenRepository) DeleteByUserAndProvider(ctx context.Context, userID, providerID string) (*ContentToken, error) {
	var deleted *ContentToken
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row models.UserContentToken
		if err := tx.Where("user_id = ? AND provider_id = ?", userID, providerID).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrContentTokenNotFound
			}
			return dberrors.Classify(err)
		}
		dec, err := r.decode(&row)
		if err != nil {
			return err
		}
		deleted = dec
		if err := tx.Delete(&models.UserContentToken{ID: row.ID}).Error; err != nil {
			return dberrors.Classify(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return deleted, nil
}

// RefreshWithLock opens a transaction, locks the row with SELECT … FOR UPDATE,
// invokes fn with the decrypted token, and persists the token returned by fn.
// On SQLite (unit tests) the locking clause is a no-op; real serialization
// is verified against PostgreSQL in integration tests.
func (r *GormContentTokenRepository) RefreshWithLock(ctx context.Context, id string, fn func(current *ContentToken) (*ContentToken, error)) (*ContentToken, error) {
	var updated *ContentToken
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row models.UserContentToken
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrContentTokenNotFound
			}
			return dberrors.Classify(err)
		}
		current, err := r.decode(&row)
		if err != nil {
			return err
		}
		next, err := fn(current)
		if err != nil {
			return err
		}
		nextRow, err := r.encode(next)
		if err != nil {
			return err
		}
		// Preserve the primary key so Save updates the existing row.
		nextRow.ID = row.ID
		if err := tx.Save(nextRow).Error; err != nil {
			return dberrors.Classify(err)
		}
		decoded, err := r.decode(nextRow)
		if err != nil {
			return err
		}
		updated = decoded
		return nil
	})
	return updated, err
}

// encode converts a plaintext ContentToken to the GORM model with encrypted tokens.
func (r *GormContentTokenRepository) encode(t *ContentToken) (*models.UserContentToken, error) {
	atCipher, err := r.enc.Encrypt([]byte(t.AccessToken))
	if err != nil {
		return nil, err
	}
	var rtCipher []byte
	if t.RefreshToken != "" {
		rtCipher, err = r.enc.Encrypt([]byte(t.RefreshToken))
		if err != nil {
			return nil, err
		}
	}
	status := t.Status
	if status == "" {
		status = ContentTokenStatusActive
	}
	row := &models.UserContentToken{
		ID:            t.ID,
		UserID:        t.UserID,
		ProviderID:    t.ProviderID,
		AccessToken:   atCipher,
		RefreshToken:  rtCipher,
		Scopes:        t.Scopes,
		ExpiresAt:     t.ExpiresAt,
		Status:        status,
		LastRefreshAt: t.LastRefreshAt,
		LastError:     t.LastError,
	}
	if t.ProviderAccountID != "" {
		v := t.ProviderAccountID
		row.ProviderAccountID = &v
	}
	if t.ProviderAccountLabel != "" {
		v := t.ProviderAccountLabel
		row.ProviderAccountLabel = &v
	}
	return row, nil
}

// decode converts the GORM model (with encrypted tokens) to a plaintext ContentToken.
func (r *GormContentTokenRepository) decode(row *models.UserContentToken) (*ContentToken, error) {
	at, err := r.enc.Decrypt(row.AccessToken)
	if err != nil {
		return nil, err
	}
	var rt []byte
	if len(row.RefreshToken) > 0 {
		rt, err = r.enc.Decrypt(row.RefreshToken)
		if err != nil {
			return nil, err
		}
	}
	out := &ContentToken{
		ID:            row.ID,
		UserID:        row.UserID,
		ProviderID:    row.ProviderID,
		AccessToken:   string(at),
		RefreshToken:  string(rt),
		Scopes:        row.Scopes,
		ExpiresAt:     row.ExpiresAt,
		Status:        row.Status,
		LastRefreshAt: row.LastRefreshAt,
		LastError:     row.LastError,
		CreatedAt:     row.CreatedAt,
		ModifiedAt:    row.ModifiedAt,
	}
	if row.ProviderAccountID != nil {
		out.ProviderAccountID = *row.ProviderAccountID
	}
	if row.ProviderAccountLabel != nil {
		out.ProviderAccountLabel = *row.ProviderAccountLabel
	}
	return out, nil
}
