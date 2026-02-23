package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"gorm.io/gorm"
)

// GormUserStore implements UserStore using GORM for cross-database support
type GormUserStore struct {
	db          *gorm.DB
	authService *auth.Service
	logger      *slogging.Logger
}

// NewGormUserStore creates a new GORM-backed user store
func NewGormUserStore(db *gorm.DB, authService *auth.Service) *GormUserStore {
	return &GormUserStore{
		db:          db,
		authService: authService,
		logger:      slogging.Get(),
	}
}

// List returns users with optional filtering and pagination
func (s *GormUserStore) List(ctx context.Context, filter UserFilter) ([]AdminUser, error) {
	query := s.db.WithContext(ctx).Model(&models.User{})

	// Apply filters
	if filter.Provider != "" {
		query = query.Where("provider = ?", filter.Provider)
	}

	if filter.Email != "" {
		// Use LOWER() for cross-database case-insensitive search
		query = query.Where("LOWER(email) LIKE LOWER(?)", "%"+filter.Email+"%")
	}

	if filter.CreatedAfter != nil {
		query = query.Where("created_at >= ?", *filter.CreatedAfter)
	}

	if filter.CreatedBefore != nil {
		query = query.Where("created_at <= ?", *filter.CreatedBefore)
	}

	if filter.LastLoginAfter != nil {
		query = query.Where("last_login >= ?", *filter.LastLoginAfter)
	}

	if filter.LastLoginBefore != nil {
		query = query.Where("last_login <= ?", *filter.LastLoginBefore)
	}

	// Apply sorting
	sortBy := "created_at"
	if filter.SortBy != "" {
		switch filter.SortBy {
		case "created_at", "last_login", "email":
			sortBy = filter.SortBy
		default:
			s.logger.Warn("Invalid sort_by value: %s, using default: created_at", filter.SortBy)
		}
	}

	sortOrder := SortDirectionDESC
	if filter.SortOrder != "" {
		switch strings.ToUpper(filter.SortOrder) {
		case SortDirectionASC:
			sortOrder = SortDirectionASC
		case SortDirectionDESC:
			sortOrder = SortDirectionDESC
		default:
			s.logger.Warn("Invalid sort_order value: %s, using default: DESC", filter.SortOrder)
		}
	}

	query = query.Order(fmt.Sprintf("%s %s", sortBy, sortOrder))

	// Apply pagination
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}

	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	// Execute query
	var gormUsers []models.User
	if err := query.Find(&gormUsers).Error; err != nil {
		return nil, fmt.Errorf("failed to query users: %w", err)
	}

	// Convert to API type
	users := make([]AdminUser, 0, len(gormUsers))
	for _, gu := range gormUsers {
		users = append(users, s.convertToAdminUser(&gu))
	}

	return users, nil
}

// Get retrieves a user by internal UUID
func (s *GormUserStore) Get(ctx context.Context, internalUUID openapi_types.UUID) (*AdminUser, error) {
	var gormUser models.User
	result := s.db.WithContext(ctx).Where("internal_uuid = ?", internalUUID.String()).First(&gormUser)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, errors.New(ErrMsgUserNotFound)
		}
		return nil, fmt.Errorf("failed to get user: %w", result.Error)
	}

	user := s.convertToAdminUser(&gormUser)
	return &user, nil
}

// GetByProviderAndID retrieves a user by provider and provider_user_id
func (s *GormUserStore) GetByProviderAndID(ctx context.Context, provider string, providerUserID string) (*AdminUser, error) {
	var gormUser models.User
	// Use map-based query for cross-database compatibility (Oracle requires quoted lowercase column names)
	result := s.db.WithContext(ctx).
		Where(map[string]any{"provider": provider, "provider_user_id": providerUserID}).
		First(&gormUser)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, errors.New(ErrMsgUserNotFound)
		}
		return nil, fmt.Errorf("failed to get user: %w", result.Error)
	}

	user := s.convertToAdminUser(&gormUser)
	return &user, nil
}

// Update updates user metadata (email, name, email_verified)
func (s *GormUserStore) Update(ctx context.Context, user AdminUser) error {
	// Note: modified_at is handled automatically by GORM's autoUpdateTime tag
	result := s.db.WithContext(ctx).Model(&models.User{}).
		Where("internal_uuid = ?", user.InternalUuid.String()).
		Updates(map[string]any{
			"email":          string(user.Email),
			"name":           user.Name,
			"email_verified": user.EmailVerified,
		})

	if result.Error != nil {
		return fmt.Errorf("failed to update user: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return errors.New(ErrMsgUserNotFound)
	}

	return nil
}

// Delete deletes a user by provider and provider_user_id
func (s *GormUserStore) Delete(ctx context.Context, provider string, providerUserID string) (*DeletionStats, error) {
	// First, get the user to find their email
	user, err := s.GetByProviderAndID(ctx, provider, providerUserID)
	if err != nil {
		return nil, fmt.Errorf("failed to find user: %w", err)
	}

	// Delegate to auth service DeleteUserAndData (same as DELETE /me)
	result, err := s.authService.DeleteUserAndData(ctx, string(user.Email))
	if err != nil {
		return nil, fmt.Errorf("failed to delete user: %w", err)
	}

	s.logger.Info("[AUDIT] Admin user deletion: provider=%s, provider_user_id=%s, email=%s, transferred=%d, deleted=%d",
		provider, providerUserID, string(user.Email), result.ThreatModelsTransferred, result.ThreatModelsDeleted)

	return &DeletionStats{
		ThreatModelsTransferred: result.ThreatModelsTransferred,
		ThreatModelsDeleted:     result.ThreatModelsDeleted,
		UserEmail:               result.UserEmail,
	}, nil
}

// Count returns total count of users matching the filter
func (s *GormUserStore) Count(ctx context.Context, filter UserFilter) (int, error) {
	query := s.db.WithContext(ctx).Model(&models.User{})

	// Apply same filters as List (excluding pagination and sorting)
	if filter.Provider != "" {
		query = query.Where("provider = ?", filter.Provider)
	}

	if filter.Email != "" {
		query = query.Where("LOWER(email) LIKE LOWER(?)", "%"+filter.Email+"%")
	}

	if filter.CreatedAfter != nil {
		query = query.Where("created_at >= ?", *filter.CreatedAfter)
	}

	if filter.CreatedBefore != nil {
		query = query.Where("created_at <= ?", *filter.CreatedBefore)
	}

	if filter.LastLoginAfter != nil {
		query = query.Where("last_login >= ?", *filter.LastLoginAfter)
	}

	if filter.LastLoginBefore != nil {
		query = query.Where("last_login <= ?", *filter.LastLoginBefore)
	}

	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to count users: %w", err)
	}

	return int(count), nil
}

// EnrichUsers adds related data to users (admin status, groups, threat model counts)
func (s *GormUserStore) EnrichUsers(ctx context.Context, users []AdminUser) ([]AdminUser, error) {
	if len(users) == 0 {
		return users, nil
	}

	enriched := make([]AdminUser, len(users))
	copy(enriched, users)

	for i := range enriched {
		user := &enriched[i]

		// Check admin status via Administrators group membership
		userUUID := user.InternalUuid
		adminsGroupUUID := uuid.MustParse(AdministratorsGroupUUID)
		isAdmin, err := GlobalGroupMemberStore.IsEffectiveMember(ctx, adminsGroupUUID, userUUID, nil)
		if err != nil {
			s.logger.Warn("Failed to check admin status for user %s: %v", user.InternalUuid, err)
		} else {
			user.IsAdmin = &isAdmin
		}

		// Count active threat models owned by user
		var count int64
		err = s.db.WithContext(ctx).Model(&models.ThreatModel{}).
			Where("owner_internal_uuid = ?", userUUID.String()).
			Count(&count).Error
		if err != nil {
			s.logger.Warn("Failed to count threat models for user %s: %v", user.InternalUuid, err)
		} else {
			countInt := int(count)
			user.ActiveThreatModels = &countInt
		}

		// Note: Groups are not stored in database, they come from JWT claims
		// For enrichment, we would need to query the IdP or use cached data
		// Leave groups empty for now - they can be populated from JWT in handlers
	}

	return enriched, nil
}

// convertToAdminUser converts a GORM User model to AdminUser
func (s *GormUserStore) convertToAdminUser(gu *models.User) AdminUser {
	internalUUID, _ := uuid.Parse(gu.InternalUUID)

	user := AdminUser{
		InternalUuid:   internalUUID,
		Provider:       gu.Provider,
		ProviderUserId: strFromPtr(gu.ProviderUserID),
		Email:          openapi_types.Email(gu.Email),
		Name:           gu.Name,
		EmailVerified:  gu.EmailVerified.Bool(),
		CreatedAt:      gu.CreatedAt,
		ModifiedAt:     gu.ModifiedAt,
	}

	if gu.LastLogin != nil {
		user.LastLogin = gu.LastLogin
	}

	return user
}
