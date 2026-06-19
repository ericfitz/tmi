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

// GormUserRepository implements UserRepository using GORM
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: GORM-backed implementation of the user repository interface
type GormUserRepository struct {
	db     *gorm.DB
	logger *slogging.Logger
}

// NewGormUserRepository creates a new GORM-backed user repository
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: build a GORM user repository bound to the given database connection (pure)
func NewGormUserRepository(db *gorm.DB) *GormUserRepository {
	return &GormUserRepository{
		db:     db,
		logger: slogging.Get(),
	}
}

// GetByEmail retrieves a user by email address
// SEM@8077d4387088ee7e6e22cce2171ad54ee850e10b: fetch a user by email address (reads DB)
func (r *GormUserRepository) GetByEmail(ctx context.Context, email string) (*User, error) {
	var gormUser models.User
	result := r.db.WithContext(ctx).Where("email = ?", email).First(&gormUser)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		r.logger.Error("GetByEmail: database query failed for email=%s: %v", email, result.Error)
		return nil, dberrors.Classify(result.Error)
	}

	return convertModelToUser(&gormUser), nil
}

// GetByID retrieves a user by internal UUID
// SEM@8077d4387088ee7e6e22cce2171ad54ee850e10b: fetch a user by internal UUID (reads DB)
func (r *GormUserRepository) GetByID(ctx context.Context, id string) (*User, error) {
	var gormUser models.User
	result := r.db.WithContext(ctx).Where("internal_uuid = ?", id).First(&gormUser)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, dberrors.Classify(result.Error)
	}

	return convertModelToUser(&gormUser), nil
}

// GetByProviderID retrieves a user by provider and provider user ID
// SEM@8077d4387088ee7e6e22cce2171ad54ee850e10b: fetch a user by OAuth provider and provider user ID (reads DB)
func (r *GormUserRepository) GetByProviderID(ctx context.Context, provider, providerUserID string) (*User, error) {
	var gormUser models.User
	// Use map-based query for cross-database compatibility (Oracle requires quoted lowercase column names)
	result := r.db.WithContext(ctx).
		Where(map[string]any{"provider": provider, "provider_user_id": providerUserID}).
		First(&gormUser)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, dberrors.Classify(result.Error)
	}

	return convertModelToUser(&gormUser), nil
}

// GetByProviderAndEmail retrieves a user by provider and email address
// SEM@8077d4387088ee7e6e22cce2171ad54ee850e10b: fetch a user matching both provider name and email address (reads DB)
func (r *GormUserRepository) GetByProviderAndEmail(ctx context.Context, provider, email string) (*User, error) {
	var gormUser models.User
	result := r.db.WithContext(ctx).
		Where("provider = ? AND email = ?", provider, email).
		First(&gormUser)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, dberrors.Classify(result.Error)
	}

	return convertModelToUser(&gormUser), nil
}

// GetByAnyProviderID retrieves a user by provider user ID across all providers
// SEM@8077d4387088ee7e6e22cce2171ad54ee850e10b: fetch a user by provider user ID across all OAuth providers (reads DB)
func (r *GormUserRepository) GetByAnyProviderID(ctx context.Context, providerUserID string) (*User, error) {
	var gormUser models.User
	// Use map-based query for cross-database compatibility (Oracle requires quoted lowercase column names)
	result := r.db.WithContext(ctx).
		Where(map[string]any{"provider_user_id": providerUserID}).
		First(&gormUser)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, dberrors.Classify(result.Error)
	}

	return convertModelToUser(&gormUser), nil
}

// GetProviders returns the OAuth providers for a user
// Note: In the current architecture, each user has exactly one provider
// SEM@2dccb03396c9b3e288e2242edb54c418635c3e08: list the OAuth providers associated with a user (reads DB)
func (r *GormUserRepository) GetProviders(ctx context.Context, userID string) ([]UserProvider, error) {
	var gormUser models.User
	result := r.db.WithContext(ctx).
		Select("internal_uuid, provider, provider_user_id, email, created_at, last_login").
		Where("internal_uuid = ?", userID).
		First(&gormUser)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return []UserProvider{}, nil // User not found, return empty array
		}
		return nil, dberrors.Classify(result.Error)
	}

	// Convert to UserProvider format (single provider)
	lastLogin := time.Time{} // Zero value
	if gormUser.LastLogin != nil {
		lastLogin = *gormUser.LastLogin
	}

	providerUserID := ""
	if gormUser.ProviderUserID.Valid {
		providerUserID = gormUser.ProviderUserID.String
	}

	providers := []UserProvider{
		{
			ID:             string(gormUser.InternalUUID),
			UserID:         string(gormUser.InternalUUID),
			Provider:       string(gormUser.Provider),
			ProviderUserID: providerUserID,
			Email:          string(gormUser.Email),
			IsPrimary:      true, // Always true since there's only one provider per user
			CreatedAt:      gormUser.CreatedAt,
			LastLogin:      lastLogin,
		},
	}

	return providers, nil
}

// GetPrimaryProviderID returns the provider user ID for a user
// SEM@8077d4387088ee7e6e22cce2171ad54ee850e10b: fetch the primary OAuth provider user ID for a given user (reads DB)
func (r *GormUserRepository) GetPrimaryProviderID(ctx context.Context, userID string) (string, error) {
	// Use a struct to scan the result - this works reliably across all databases
	// (PostgreSQL, Oracle, SQLite) unlike scanning directly into a *string pointer
	var result struct {
		ProviderUserID *string
	}
	err := r.db.WithContext(ctx).
		Model(&models.User{}).
		Select("provider_user_id").
		Where("internal_uuid = ?", userID).
		First(&result).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil // User not found
		}
		return "", dberrors.Classify(err)
	}

	if result.ProviderUserID == nil {
		return "", nil
	}
	return *result.ProviderUserID, nil
}

// Create creates a new user
// SEM@8077d4387088ee7e6e22cce2171ad54ee850e10b: store a new user record, generating UUID and timestamps if absent (mutates shared state)
func (r *GormUserRepository) Create(ctx context.Context, user *User) (*User, error) {
	// Generate a new internal UUID if not provided
	if user.InternalUUID == "" {
		user.InternalUUID = uuid.New().String()
	}

	// Set timestamps if not provided
	now := time.Now()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	if user.ModifiedAt.IsZero() {
		user.ModifiedAt = now
	}

	gormUser := convertUserToModel(user)

	result := r.db.WithContext(ctx).Create(gormUser)
	if result.Error != nil {
		return nil, dberrors.Classify(result.Error)
	}

	// Return the created user with the generated UUID
	return convertModelToUser(gormUser), nil
}

// Update updates an existing user
// SEM@2dccb03396c9b3e288e2242edb54c418635c3e08: update a user's mutable fields, preserving existing provider identity (mutates shared state)
func (r *GormUserRepository) Update(ctx context.Context, user *User) error {
	// Note: modified_at is handled automatically by GORM's autoUpdateTime tag
	// Do not include it in the Updates map to avoid duplicate column errors on Oracle

	// Build the base updates map
	updates := map[string]any{
		"email":          user.Email,
		"name":           user.Name,
		"email_verified": user.EmailVerified,
		"access_token":   user.AccessToken,
		"refresh_token":  user.RefreshToken,
		"token_expiry":   user.TokenExpiry,
		"last_login":     user.LastLogin,
	}

	// Conditionally update provider and provider_user_id only if they're currently blank/null
	// This allows sparse user records (created from admin config) to be completed on first login
	// but prevents overwriting valid provider info with different values
	if user.Provider != "" || user.ProviderUserID != "" {
		// First, check current values in the database
		var existing models.User
		if err := r.db.WithContext(ctx).
			Select("provider", "provider_user_id").
			Where("internal_uuid = ?", user.InternalUUID).
			First(&existing).Error; err == nil {
			// Update provider only if current value is blank and new value is provided
			if existing.Provider == "" && user.Provider != "" {
				updates["provider"] = user.Provider
			}
			// Update provider_user_id only if current value is blank/null and new value is provided
			if (!existing.ProviderUserID.Valid || existing.ProviderUserID.String == "") && user.ProviderUserID != "" {
				updates["provider_user_id"] = user.ProviderUserID
			}
		}
	}

	result := r.db.WithContext(ctx).Model(&models.User{}).
		Where("internal_uuid = ?", user.InternalUUID).
		Updates(updates)

	if result.Error != nil {
		return dberrors.Classify(result.Error)
	}

	if result.RowsAffected == 0 {
		return ErrUserNotFound
	}

	return nil
}

// Delete deletes a user by internal UUID
// SEM@8077d4387088ee7e6e22cce2171ad54ee850e10b: delete a user by internal UUID, returning an error if not found (mutates shared state)
func (r *GormUserRepository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).
		Where("internal_uuid = ?", id).
		Delete(&models.User{})

	if result.Error != nil {
		return dberrors.Classify(result.Error)
	}

	if result.RowsAffected == 0 {
		return ErrUserNotFound
	}

	return nil
}

// convertModelToUser converts a GORM User model to a repository User
// SEM@2dccb03396c9b3e288e2242edb54c418635c3e08: convert a GORM user model to the repository user domain type (pure)
func convertModelToUser(m *models.User) *User {
	providerUserID := ""
	if m.ProviderUserID.Valid {
		providerUserID = m.ProviderUserID.String
	}

	return &User{
		InternalUUID:   string(m.InternalUUID),
		Provider:       string(m.Provider),
		ProviderUserID: providerUserID,
		Email:          string(m.Email),
		Name:           string(m.Name),
		EmailVerified:  m.EmailVerified.Bool(), // Convert DBBool to bool
		AccessToken:    m.AccessToken.Ptr(),    // Convert NullableDBText to *string
		RefreshToken:   m.RefreshToken.Ptr(),   // Convert NullableDBText to *string
		TokenExpiry:    m.TokenExpiry,
		CreatedAt:      m.CreatedAt,
		ModifiedAt:     m.ModifiedAt,
		LastLogin:      m.LastLogin,
		Automation:     m.Automation,
	}
}

// convertUserToModel converts a repository User to a GORM User model
// SEM@2dccb03396c9b3e288e2242edb54c418635c3e08: convert a repository user domain type to a GORM user model (pure)
func convertUserToModel(u *User) *models.User {
	var providerUserID *string
	if u.ProviderUserID != "" {
		providerUserID = &u.ProviderUserID
	}

	return &models.User{
		InternalUUID:   models.DBVarchar(u.InternalUUID),
		Provider:       models.DBVarchar(u.Provider),
		ProviderUserID: models.NewNullableDBVarchar(providerUserID),
		Email:          models.DBVarchar(u.Email),
		Name:           models.DBVarchar(u.Name),
		EmailVerified:  models.DBBool(u.EmailVerified),           // Convert bool to DBBool
		AccessToken:    models.NewNullableDBText(u.AccessToken),  // Convert *string to NullableDBText
		RefreshToken:   models.NewNullableDBText(u.RefreshToken), // Convert *string to NullableDBText
		TokenExpiry:    u.TokenExpiry,
		CreatedAt:      u.CreatedAt,
		ModifiedAt:     u.ModifiedAt,
		LastLogin:      u.LastLogin,
		Automation:     u.Automation,
	}
}
