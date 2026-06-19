package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/auth"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"gorm.io/gorm"
)

// GormUserStore implements UserStore using GORM for cross-database support
// SEM@75d52ab3d1f4f71b22b1cef7144254cfdb837491: GORM-backed store for user records with auth service integration
type GormUserStore struct {
	db          *gorm.DB
	authService *auth.Service
	logger      *slogging.Logger
}

// NewGormUserStore creates a new GORM-backed user store
// SEM@75d52ab3d1f4f71b22b1cef7144254cfdb837491: build a GORM-backed user store wired to the auth service
func NewGormUserStore(db *gorm.DB, authService *auth.Service) *GormUserStore {
	return &GormUserStore{
		db:          db,
		authService: authService,
		logger:      slogging.Get(),
	}
}

// List returns users with optional filtering and pagination
// SEM@6a6c15749391c2817c30c64c8b54f8e0a4082a91: list users with optional filter, sort, and pagination applied (reads DB)
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

	if filter.Name != "" {
		// Use LOWER() for cross-database case-insensitive search
		query = query.Where("LOWER(name) LIKE LOWER(?)", "%"+filter.Name+"%")
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

	if filter.Automation != nil {
		if *filter.Automation {
			query = query.Where("automation = ?", true)
		} else {
			query = query.Where("automation IS NULL OR automation = ?", false)
		}
	}

	// Apply sorting
	sortBy := "created_at"
	if filter.SortBy != "" {
		switch filter.SortBy {
		case "created_at", "last_login", "email", string(SortByQueryParamName):
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
		return nil, dberrors.Classify(err)
	}

	// Convert to API type
	users := make([]AdminUser, 0, len(gormUsers))
	for _, gu := range gormUsers {
		users = append(users, s.convertToAdminUser(&gu))
	}

	return users, nil
}

// Get retrieves a user by internal UUID
// SEM@6a6c15749391c2817c30c64c8b54f8e0a4082a91: fetch a user by internal UUID (reads DB)
func (s *GormUserStore) Get(ctx context.Context, internalUUID openapi_types.UUID) (*AdminUser, error) {
	var gormUser models.User
	result := s.db.WithContext(ctx).Where("internal_uuid = ?", internalUUID.String()).First(&gormUser)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, dberrors.Classify(result.Error)
	}

	user := s.convertToAdminUser(&gormUser)
	return &user, nil
}

// GetByProviderAndID retrieves a user by provider and provider_user_id
// SEM@6a6c15749391c2817c30c64c8b54f8e0a4082a91: fetch a user by identity provider and provider user ID (reads DB)
func (s *GormUserStore) GetByProviderAndID(ctx context.Context, provider string, providerUserID string) (*AdminUser, error) {
	var gormUser models.User
	// Use map-based query for cross-database compatibility (Oracle requires quoted lowercase column names)
	result := s.db.WithContext(ctx).
		Where(map[string]any{"provider": provider, "provider_user_id": providerUserID}).
		First(&gormUser)

	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, dberrors.Classify(result.Error)
	}

	user := s.convertToAdminUser(&gormUser)
	return &user, nil
}

// Update updates user metadata (email, name, email_verified)
// SEM@6a6c15749391c2817c30c64c8b54f8e0a4082a91: update a user's email, name, and email verification status (writes DB)
func (s *GormUserStore) Update(ctx context.Context, user AdminUser) error {
	// Note: modified_at is handled automatically by GORM's autoUpdateTime tag
	return authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.Model(&models.User{}).
			Where("internal_uuid = ?", user.InternalUuid.String()).
			Updates(map[string]any{
				"email":          string(user.Email),
				"name":           user.Name,
				"email_verified": user.EmailVerified,
			})
		if result.Error != nil {
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrUserNotFound
		}
		return nil
	})
}

// Delete deletes a user by internal UUID, using the auth service's direct UUID-based
// deletion to avoid multi-hop identity resolution bugs.
// SEM@6a6c15749391c2817c30c64c8b54f8e0a4082a91: delete a user by internal UUID and transfer or remove owned threat models (writes DB)
func (s *GormUserStore) Delete(ctx context.Context, internalUUID uuid.UUID) (*DeletionStats, error) {
	result, err := s.authService.DeleteUserByInternalUUID(ctx, internalUUID.String())
	if err != nil {
		return nil, err
	}

	s.logger.Info("[AUDIT] User deletion: internal_uuid=%s, email=%s, transferred=%d, deleted=%d",
		internalUUID, result.UserEmail, result.ThreatModelsTransferred, result.ThreatModelsDeleted)

	return &DeletionStats{
		ThreatModelsTransferred: result.ThreatModelsTransferred,
		ThreatModelsDeleted:     result.ThreatModelsDeleted,
		UserEmail:               result.UserEmail,
	}, nil
}

// Count returns total count of users matching the filter
// SEM@6a6c15749391c2817c30c64c8b54f8e0a4082a91: count users matching the given filter criteria (reads DB)
func (s *GormUserStore) Count(ctx context.Context, filter UserFilter) (int, error) {
	query := s.db.WithContext(ctx).Model(&models.User{})

	// Apply same filters as List (excluding pagination and sorting)
	if filter.Provider != "" {
		query = query.Where("provider = ?", filter.Provider)
	}

	if filter.Email != "" {
		query = query.Where("LOWER(email) LIKE LOWER(?)", "%"+filter.Email+"%")
	}

	if filter.Name != "" {
		query = query.Where("LOWER(name) LIKE LOWER(?)", "%"+filter.Name+"%")
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

	if filter.Automation != nil {
		if *filter.Automation {
			query = query.Where("automation = ?", true)
		} else {
			query = query.Where("automation IS NULL OR automation = ?", false)
		}
	}

	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, dberrors.Classify(err)
	}

	return int(count), nil
}

// EnrichUsers adds related data to users (admin status, groups, threat model counts)
// SEM@1aa36c06c7b700d3f00bf6f4b22125d673b1070a: attach admin status and threat model counts to a list of users (reads DB)
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
		isAdmin, err := GlobalGroupMemberRepository.IsEffectiveMember(ctx, adminsGroupUUID, userUUID, nil)
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
// SEM@2dccb03396c9b3e288e2242edb54c418635c3e08: convert a GORM user model to an AdminUser API struct (pure)
func (s *GormUserStore) convertToAdminUser(gu *models.User) AdminUser {
	internalUUID, _ := uuid.Parse(string(gu.InternalUUID))

	// Sanitize email: openapi_types.Email.MarshalJSON calls mail.ParseAddress
	// which fails on empty strings, causing c.JSON to produce an empty 200 response.
	email := gu.Email
	if email == "" {
		email = "unknown@invalid"
	}

	user := AdminUser{
		InternalUuid:   internalUUID,
		Provider:       string(gu.Provider),
		ProviderUserId: strFromPtr(gu.ProviderUserID.Ptr()),
		Email:          openapi_types.Email(email),
		Name:           string(gu.Name),
		EmailVerified:  gu.EmailVerified.Bool(),
		CreatedAt:      gu.CreatedAt,
		ModifiedAt:     gu.ModifiedAt,
	}

	if gu.LastLogin != nil {
		user.LastLogin = gu.LastLogin
	}

	user.Automation = gu.Automation

	return user
}
