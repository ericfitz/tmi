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

// GormUserRepository implements UserRepository using GORM
type GormUserRepository struct {
	db     *gorm.DB
	logger *slogging.Logger
}

// NewGormUserRepository creates a new GORM-backed user repository
func NewGormUserRepository(db *gorm.DB) *GormUserRepository {
	return &GormUserRepository{
		db:     db,
		logger: slogging.Get(),
	}
}

// GetByEmail retrieves a user by email address
func (r *GormUserRepository) GetByEmail(ctx context.Context, email string) (*User, error) {
	var gormUser models.User
	result := r.db.WithContext(ctx).Where("email = ?", email).First(&gormUser)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, ErrUserNotFound
		}
		r.logger.Error("GetByEmail: database query failed for email=%s: %v", email, result.Error)
		return nil, fmt.Errorf("failed to get user: %w", result.Error)
	}

	return convertModelToUser(&gormUser), nil
}

// GetByID retrieves a user by internal UUID
func (r *GormUserRepository) GetByID(ctx context.Context, id string) (*User, error) {
	var gormUser models.User
	result := r.db.WithContext(ctx).Where("internal_uuid = ?", id).First(&gormUser)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user: %w", result.Error)
	}

	return convertModelToUser(&gormUser), nil
}

// GetByProviderID retrieves a user by provider and provider user ID
func (r *GormUserRepository) GetByProviderID(ctx context.Context, provider, providerUserID string) (*User, error) {
	var gormUser models.User
	// Use map-based query for cross-database compatibility (Oracle requires quoted lowercase column names)
	result := r.db.WithContext(ctx).
		Where(map[string]interface{}{"provider": provider, "provider_user_id": providerUserID}).
		First(&gormUser)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user by provider ID: %w", result.Error)
	}

	return convertModelToUser(&gormUser), nil
}

// GetByProviderAndEmail retrieves a user by provider and email address
func (r *GormUserRepository) GetByProviderAndEmail(ctx context.Context, provider, email string) (*User, error) {
	var gormUser models.User
	result := r.db.WithContext(ctx).
		Where("provider = ? AND email = ?", provider, email).
		First(&gormUser)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user by provider and email: %w", result.Error)
	}

	return convertModelToUser(&gormUser), nil
}

// GetByAnyProviderID retrieves a user by provider user ID across all providers
func (r *GormUserRepository) GetByAnyProviderID(ctx context.Context, providerUserID string) (*User, error) {
	var gormUser models.User
	// Use map-based query for cross-database compatibility (Oracle requires quoted lowercase column names)
	result := r.db.WithContext(ctx).
		Where(map[string]interface{}{"provider_user_id": providerUserID}).
		First(&gormUser)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user by provider ID: %w", result.Error)
	}

	return convertModelToUser(&gormUser), nil
}

// GetProviders returns the OAuth providers for a user
// Note: In the current architecture, each user has exactly one provider
func (r *GormUserRepository) GetProviders(ctx context.Context, userID string) ([]UserProvider, error) {
	var gormUser models.User
	result := r.db.WithContext(ctx).
		Select("internal_uuid, provider, provider_user_id, email, created_at, last_login").
		Where("internal_uuid = ?", userID).
		First(&gormUser)

	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return []UserProvider{}, nil // User not found, return empty array
		}
		return nil, fmt.Errorf("failed to get user provider: %w", result.Error)
	}

	// Convert to UserProvider format (single provider)
	lastLogin := time.Time{} // Zero value
	if gormUser.LastLogin != nil {
		lastLogin = *gormUser.LastLogin
	}

	providerUserID := ""
	if gormUser.ProviderUserID != nil {
		providerUserID = *gormUser.ProviderUserID
	}

	providers := []UserProvider{
		{
			ID:             gormUser.InternalUUID,
			UserID:         gormUser.InternalUUID,
			Provider:       gormUser.Provider,
			ProviderUserID: providerUserID,
			Email:          gormUser.Email,
			IsPrimary:      true, // Always true since there's only one provider per user
			CreatedAt:      gormUser.CreatedAt,
			LastLogin:      lastLogin,
		},
	}

	return providers, nil
}

// GetPrimaryProviderID returns the provider user ID for a user
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
		if err == gorm.ErrRecordNotFound {
			return "", nil // User not found
		}
		return "", fmt.Errorf("failed to get provider user ID: %w", err)
	}

	if result.ProviderUserID == nil {
		return "", nil
	}
	return *result.ProviderUserID, nil
}

// Create creates a new user
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
		return nil, fmt.Errorf("failed to create user: %w", result.Error)
	}

	// Return the created user with the generated UUID
	return convertModelToUser(gormUser), nil
}

// Update updates an existing user
func (r *GormUserRepository) Update(ctx context.Context, user *User) error {
	// Note: modified_at is handled automatically by GORM's autoUpdateTime tag
	// Do not include it in the Updates map to avoid duplicate column errors on Oracle

	result := r.db.WithContext(ctx).Model(&models.User{}).
		Where("internal_uuid = ?", user.InternalUUID).
		Updates(map[string]interface{}{
			"email":          user.Email,
			"name":           user.Name,
			"email_verified": user.EmailVerified,
			"access_token":   user.AccessToken,
			"refresh_token":  user.RefreshToken,
			"token_expiry":   user.TokenExpiry,
			"last_login":     user.LastLogin,
		})

	if result.Error != nil {
		return fmt.Errorf("failed to update user: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return ErrUserNotFound
	}

	return nil
}

// Delete deletes a user by internal UUID
func (r *GormUserRepository) Delete(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).
		Where("internal_uuid = ?", id).
		Delete(&models.User{})

	if result.Error != nil {
		return fmt.Errorf("failed to delete user: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return ErrUserNotFound
	}

	return nil
}

// convertModelToUser converts a GORM User model to a repository User
func convertModelToUser(m *models.User) *User {
	providerUserID := ""
	if m.ProviderUserID != nil {
		providerUserID = *m.ProviderUserID
	}

	return &User{
		InternalUUID:   m.InternalUUID,
		Provider:       m.Provider,
		ProviderUserID: providerUserID,
		Email:          m.Email,
		Name:           m.Name,
		EmailVerified:  m.EmailVerified.Bool(), // Convert DBBool to bool
		AccessToken:    m.AccessToken.Ptr(),    // Convert NullableDBText to *string
		RefreshToken:   m.RefreshToken.Ptr(),   // Convert NullableDBText to *string
		TokenExpiry:    m.TokenExpiry,
		CreatedAt:      m.CreatedAt,
		ModifiedAt:     m.ModifiedAt,
		LastLogin:      m.LastLogin,
	}
}

// convertUserToModel converts a repository User to a GORM User model
func convertUserToModel(u *User) *models.User {
	var providerUserID *string
	if u.ProviderUserID != "" {
		providerUserID = &u.ProviderUserID
	}

	return &models.User{
		InternalUUID:   u.InternalUUID,
		Provider:       u.Provider,
		ProviderUserID: providerUserID,
		Email:          u.Email,
		Name:           u.Name,
		EmailVerified:  models.DBBool(u.EmailVerified),           // Convert bool to DBBool
		AccessToken:    models.NewNullableDBText(u.AccessToken),  // Convert *string to NullableDBText
		RefreshToken:   models.NewNullableDBText(u.RefreshToken), // Convert *string to NullableDBText
		TokenExpiry:    u.TokenExpiry,
		CreatedAt:      u.CreatedAt,
		ModifiedAt:     u.ModifiedAt,
		LastLogin:      u.LastLogin,
	}
}
