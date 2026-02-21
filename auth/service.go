package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/auth/repository"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// redisNilError is the error message returned by Redis when a key is not found
const redisNilError = "redis: nil"

// ClaimsEnricher enriches JWT claims with application-specific data (e.g., group membership)
// that cannot be directly accessed from the auth package without creating circular dependencies.
type ClaimsEnricher interface {
	// EnrichClaims checks built-in group membership for a user.
	// Returns whether the user is an administrator and/or security reviewer.
	EnrichClaims(ctx context.Context, userInternalUUID string, provider string, groupNames []string) (isAdmin bool, isSecurityReviewer bool, err error)
}

// Service provides authentication and authorization functionality
type Service struct {
	dbManager      *db.Manager
	config         Config
	keyManager     *JWTKeyManager
	samlManager    *SAMLManager
	stateStore     StateStore
	userRepo       repository.UserRepository
	credRepo       repository.ClientCredentialRepository
	deletionRepo   repository.DeletionRepository
	claimsEnricher ClaimsEnricher
}

// SetClaimsEnricher sets the claims enricher for JWT token generation
func (s *Service) SetClaimsEnricher(enricher ClaimsEnricher) {
	s.claimsEnricher = enricher
}

// NewService creates a new authentication service
func NewService(dbManager *db.Manager, config Config) (*Service, error) {
	if dbManager == nil {
		return nil, errors.New("database manager is required")
	}

	if err := config.ValidateConfig(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Set TMI_BUILD_MODE from config if specified (ensures config overrides environment)
	if config.BuildMode != "" {
		if err := os.Setenv("TMI_BUILD_MODE", config.BuildMode); err != nil {
			return nil, fmt.Errorf("failed to set TMI_BUILD_MODE: %w", err)
		}
	}

	// Initialize JWT key manager
	keyManager, err := NewJWTKeyManager(config.JWT)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize JWT key manager: %w", err)
	}

	// Initialize state store (in-memory for now, can be Redis later)
	stateStore := NewInMemoryStateStore()

	// Initialize GORM repositories
	gormDB := dbManager.Gorm().DB()
	userRepo := repository.NewGormUserRepository(gormDB)
	credRepo := repository.NewGormClientCredentialRepository(gormDB)
	deletionRepo := repository.NewGormDeletionRepository(gormDB)

	// Create service instance
	service := &Service{
		dbManager:    dbManager,
		config:       config,
		keyManager:   keyManager,
		stateStore:   stateStore,
		userRepo:     userRepo,
		credRepo:     credRepo,
		deletionRepo: deletionRepo,
	}

	// Initialize SAML manager if configured
	if config.SAML.Enabled {
		samlManager := NewSAMLManager(service)
		if err := samlManager.InitializeProviders(config.SAML, stateStore); err != nil {
			// Log the error but don't fail - SAML is optional
			slogging.Get().Warn("SAML provider initialization had issues (continuing without SAML): %v", err)
		}
		service.samlManager = samlManager
	}

	return service, nil
}

// GetKeyManager returns the JWT key manager (getter for unexported field)
func (s *Service) GetKeyManager() *JWTKeyManager {
	return s.keyManager
}

// GetSAMLManager returns the SAML manager (getter for unexported field)
func (s *Service) GetSAMLManager() *SAMLManager {
	return s.samlManager
}

// BlacklistToken adds a JWT token to the blacklist so it can no longer be used.
// This should be called when a user is deleted or logs out to invalidate their tokens.
func (s *Service) BlacklistToken(ctx context.Context, tokenString string) error {
	if s.dbManager == nil || s.dbManager.Redis() == nil {
		slogging.Get().Warn("Token blacklisting skipped: Redis not available")
		return nil
	}

	blacklist := NewTokenBlacklist(s.dbManager.Redis().GetClient(), s.keyManager)
	return blacklist.BlacklistToken(ctx, tokenString)
}

// User represents a user in the system
type User struct {
	InternalUUID       string     `json:"internal_uuid"`    // Internal system UUID (cached but excluded from API responses via convertUserToAPIResponse)
	Provider           string     `json:"provider"`         // OAuth provider: "tmi", "google", "github", "microsoft", "azure"
	ProviderUserID     string     `json:"provider_user_id"` // Provider's user ID (from JWT sub claim)
	Email              string     `json:"email"`
	Name               string     `json:"name"` // Display name for UI presentation
	EmailVerified      bool       `json:"email_verified"`
	AccessToken        *string    `json:"-"`                    // OAuth access token (not exposed in JSON) - nullable
	RefreshToken       *string    `json:"-"`                    // OAuth refresh token (not exposed in JSON) - nullable
	TokenExpiry        *time.Time `json:"-"`                    // Token expiration time (not exposed in JSON) - nullable
	Groups             []string   `json:"groups,omitempty"`     // Groups from identity provider (not stored in DB)
	IsAdmin            bool       `json:"is_admin"`             // Whether user has administrator privileges
	IsSecurityReviewer bool       `json:"is_security_reviewer"` // Whether user is a security reviewer
	CreatedAt          time.Time  `json:"created_at"`
	ModifiedAt         time.Time  `json:"modified_at"`
	LastLogin          *time.Time `json:"last_login,omitempty"` // nullable - may be NULL for auto-created admin users
}

// TokenPair contains an access token and a refresh token
type TokenPair struct {
	AccessToken  string `json:"access_token"`  //nolint:gosec // G117 - OAuth token pair field
	RefreshToken string `json:"refresh_token"` //nolint:gosec // G117 - OAuth token pair field
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// Claims represents the JWT claims
type Claims struct {
	Email              string   `json:"email"`
	EmailVerified      bool     `json:"email_verified,omitempty"`
	Name               string   `json:"name"`
	IdentityProvider   string   `json:"idp,omitempty"`                      // Identity provider
	Groups             []string `json:"groups,omitempty"`                   // User's groups from IdP
	IsAdministrator    *bool    `json:"tmi_is_administrator,omitempty"`     // TMI Administrators group membership
	IsSecurityReviewer *bool    `json:"tmi_is_security_reviewer,omitempty"` // TMI Security Reviewers group membership
	jwt.RegisteredClaims
}

// GenerateTokens generates a new JWT token pair for a user
func (s *Service) GenerateTokens(ctx context.Context, user User) (TokenPair, error) {
	return s.GenerateTokensWithUserInfo(ctx, user, nil)
}

// GenerateTokensWithUserInfo generates a new JWT token pair for a user with optional provider UserInfo
func (s *Service) GenerateTokensWithUserInfo(ctx context.Context, user User, userInfo *UserInfo) (TokenPair, error) {
	// If UserInfo is provided, update the user with fresh provider data
	if userInfo != nil {
		user.EmailVerified = userInfo.EmailVerified
		// Note: GivenName, FamilyName, Picture, Locale are ignored per schema requirements

		// Set IdP and groups from the fresh UserInfo
		if userInfo.IdP != "" {
			// Only set provider if user doesn't already have one (sparse record).
			// This matches the OAuth flow's tiered matching behavior and prevents
			// a SAML login from overwriting an existing OAuth provider (or vice versa)
			// for the same user.
			if user.Provider == "" {
				user.Provider = userInfo.IdP
			}
			// Cache groups in Redis if available
			if len(userInfo.Groups) > 0 {
				if err := s.CacheUserGroups(ctx, user.Email, userInfo.IdP, userInfo.Groups); err != nil {
					// Log error but continue - caching failure shouldn't block login
					slogging.Get().Warn("Failed to cache user groups: %v", err)
				}
			}
		}
		user.Groups = userInfo.Groups

		// Update the user in the database with fresh provider data (except groups)
		if err := s.UpdateUser(ctx, user); err != nil {
			// Log error but continue - token generation shouldn't fail due to update issues
			slogging.Get().Error("Failed to update user provider data: %v", err)
		}
	}

	// The provider user ID is now directly stored on the User struct
	// JWT sub claim should contain the provider's user ID, NOT our internal UUID
	if user.ProviderUserID == "" {
		return TokenPair{}, fmt.Errorf("no provider user ID found for user %s", user.InternalUUID)
	}

	// Derive the issuer from the OAuth callback URL
	issuer := s.deriveIssuer()

	// Create the JWT claims using the user's stored data
	expirationTime := time.Now().Add(s.config.GetJWTDuration())
	claims := &Claims{
		Email:            user.Email,
		EmailVerified:    user.EmailVerified,
		Name:             user.Name,
		IdentityProvider: user.Provider,
		Groups:           user.Groups,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   user.ProviderUserID,      // JWT sub contains provider's user ID, NOT internal UUID
			Audience:  jwt.ClaimStrings{issuer}, // The audience is the issuer itself for self-issued tokens
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			ID:        uuid.New().String(),
		},
	}

	// Enrich claims with TMI group membership data (admin, security reviewer)
	if s.claimsEnricher != nil && user.InternalUUID != "" {
		isAdmin, isSecReviewer, enrichErr := s.claimsEnricher.EnrichClaims(ctx, user.InternalUUID, user.Provider, user.Groups)
		if enrichErr != nil {
			slogging.Get().Warn("Failed to enrich JWT claims with group membership: %v", enrichErr)
			// Don't fail token generation if enrichment fails - just omit the claims
		} else {
			claims.IsAdministrator = &isAdmin
			claims.IsSecurityReviewer = &isSecReviewer
		}
	}

	// Create the JWT token using the key manager
	tokenString, err := s.keyManager.CreateToken(claims)
	if err != nil {
		return TokenPair{}, fmt.Errorf("failed to create token: %w", err)
	}

	// Generate a refresh token
	refreshToken := uuid.New().String()
	refreshDuration := 30 * 24 * time.Hour // 30 days

	// Store the refresh token in Redis (map to internal UUID)
	refreshKey := fmt.Sprintf("refresh_token:%s", refreshToken)
	err = s.dbManager.Redis().Set(ctx, refreshKey, user.InternalUUID, refreshDuration)
	if err != nil {
		return TokenPair{}, fmt.Errorf("failed to store refresh token: %w", err)
	}

	// Return the token pair
	return TokenPair{
		AccessToken:  tokenString,
		RefreshToken: refreshToken,
		ExpiresIn:    s.config.JWT.ExpirationSeconds,
		TokenType:    "Bearer",
	}, nil
}

// ValidateToken validates a JWT token
func (s *Service) ValidateToken(tokenString string) (*Claims, error) {
	// Use the key manager to verify the token
	claims := &Claims{}
	token, err := s.keyManager.VerifyToken(tokenString, claims)
	if err != nil {
		return nil, fmt.Errorf("failed to verify token: %w", err)
	}

	// Validate the token
	if !token.Valid {
		return nil, errors.New("invalid token")
	}

	// Extract the claims
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, errors.New("invalid token claims")
	}

	// Validate issuer
	expectedIssuer := s.deriveIssuer()
	if claims.Issuer != expectedIssuer {
		return nil, fmt.Errorf("invalid token issuer: expected %s, got %s", expectedIssuer, claims.Issuer)
	}

	// Validate audience
	audienceValid := slices.Contains(claims.Audience, expectedIssuer)
	if !audienceValid {
		return nil, fmt.Errorf("invalid token audience: expected %s", expectedIssuer)
	}

	return claims, nil
}

// RefreshToken refreshes an access token using a refresh token
func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (TokenPair, error) {
	// Get the user ID from Redis
	refreshKey := fmt.Sprintf("refresh_token:%s", refreshToken)
	userID, err := s.dbManager.Redis().Get(ctx, refreshKey)
	if err != nil {
		return TokenPair{}, fmt.Errorf("invalid refresh token: %w", err)
	}

	// Delete the old refresh token
	if err := s.dbManager.Redis().Del(ctx, refreshKey); err != nil {
		return TokenPair{}, fmt.Errorf("failed to delete refresh token: %w", err)
	}

	// Get the user from the database
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return TokenPair{}, fmt.Errorf("failed to get user: %w", err)
	}

	// Update the last login time
	now := time.Now()
	user.LastLogin = &now
	if err := s.UpdateUser(ctx, user); err != nil {
		// Log the error but continue
		slogging.Get().Error("Failed to update user last login: %v", err)
	}

	// Generate new tokens
	return s.GenerateTokens(ctx, user)
}

// RevokeToken revokes a refresh token
func (s *Service) RevokeToken(ctx context.Context, refreshToken string) error {
	refreshKey := fmt.Sprintf("refresh_token:%s", refreshToken)
	return s.dbManager.Redis().Del(ctx, refreshKey)
}

// InvalidateUserSessions invalidates all sessions for a user
func (s *Service) InvalidateUserSessions(ctx context.Context, userID string) error {
	logger := slogging.Get()
	redisDB := s.dbManager.Redis()
	client := redisDB.GetClient()

	// Find all session keys for this user using pattern matching
	pattern := fmt.Sprintf("session:%s:*", userID)
	keys, err := client.Keys(ctx, pattern).Result()
	if err != nil {
		logger.Error("Failed to find session keys for user %s: %v", userID, err)
		return fmt.Errorf("failed to find user sessions: %w", err)
	}

	// Delete all session keys
	if len(keys) > 0 {
		if err := client.Del(ctx, keys...).Err(); err != nil {
			logger.Error("Failed to delete sessions for user %s: %v", userID, err)
			return fmt.Errorf("failed to delete user sessions: %w", err)
		}
		logger.Info("Invalidated %d sessions for user %s", len(keys), userID)
	}

	return nil
}

// GetUserByEmail gets a user by email
func (s *Service) GetUserByEmail(ctx context.Context, email string) (User, error) {
	// Try cache first
	cachedUser, err := s.GetCachedUserByEmail(ctx, email)
	if err == nil && cachedUser != nil {
		return *cachedUser, nil
	}

	repoUser, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return User{}, errors.New("user not found")
		}
		return User{}, err
	}

	user := convertRepoUserToServiceUser(repoUser)

	// Cache the user for future lookups
	if cacheErr := s.CacheUser(ctx, user); cacheErr != nil {
		logger := slogging.Get()
		logger.Warn("Failed to cache user after lookup: %v", cacheErr)
		// Don't fail the request, just log the cache error
	}

	return user, nil
}

// GetUserByID gets a user by internal UUID
func (s *Service) GetUserByID(ctx context.Context, id string) (User, error) {
	// Try cache first
	cachedUser, err := s.GetCachedUserByID(ctx, id)
	if err == nil && cachedUser != nil {
		return *cachedUser, nil
	}

	repoUser, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return User{}, errors.New("user not found")
		}
		return User{}, fmt.Errorf("failed to get user: %w", err)
	}

	user := convertRepoUserToServiceUser(repoUser)

	// Cache the user for future lookups
	if cacheErr := s.CacheUser(ctx, user); cacheErr != nil {
		logger := slogging.Get()
		logger.Warn("Failed to cache user after lookup: %v", cacheErr)
		// Don't fail the request, just log the cache error
	}

	return user, nil
}

// CreateUser creates a new user
func (s *Service) CreateUser(ctx context.Context, user User) (User, error) {
	// Provider and ProviderUserID must be set by caller
	if user.Provider == "" || user.ProviderUserID == "" {
		return User{}, errors.New("provider and provider_user_id are required")
	}

	repoUser := convertServiceUserToRepoUser(&user)
	createdUser, err := s.userRepo.Create(ctx, repoUser)
	if err != nil {
		return User{}, fmt.Errorf("failed to create user: %w", err)
	}

	result := convertRepoUserToServiceUser(createdUser)

	// Cache the newly created user
	if cacheErr := s.CacheUser(ctx, result); cacheErr != nil {
		logger := slogging.Get()
		logger.Warn("Failed to cache newly created user: %v", cacheErr)
		// Don't fail the request, just log the cache error
	}

	return result, nil
}

// UpdateUser updates an existing user
func (s *Service) UpdateUser(ctx context.Context, user User) error {
	repoUser := convertServiceUserToRepoUser(&user)
	err := s.userRepo.Update(ctx, repoUser)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return errors.New("user not found")
		}
		return fmt.Errorf("failed to update user: %w", err)
	}

	// Invalidate cache after update
	if cacheErr := s.InvalidateUserCache(ctx, user); cacheErr != nil {
		logger := slogging.Get()
		logger.Warn("Failed to invalidate user cache after update: %v", cacheErr)
		// Don't fail the request, just log the cache error
	}

	return nil
}

// DeleteUser deletes a user by internal UUID
func (s *Service) DeleteUser(ctx context.Context, id string) error {
	// Get user before deletion for cache invalidation
	user, err := s.GetUserByID(ctx, id)
	if err != nil {
		return err
	}

	err = s.userRepo.Delete(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return errors.New("user not found")
		}
		return fmt.Errorf("failed to delete user: %w", err)
	}

	// Invalidate cache after deletion
	if cacheErr := s.InvalidateUserCache(ctx, user); cacheErr != nil {
		logger := slogging.Get()
		logger.Warn("Failed to invalidate user cache after deletion: %v", cacheErr)
		// Don't fail the request, just log the cache error
	}

	return nil
}

// UserProvider represents a user's OAuth provider
type UserProvider struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	Provider       string    `json:"provider"`
	ProviderUserID string    `json:"provider_user_id"`
	Email          string    `json:"email"`
	IsPrimary      bool      `json:"is_primary"`
	CreatedAt      time.Time `json:"created_at"`
	LastLogin      time.Time `json:"last_login"`
}

// GetUserProviders gets the OAuth provider for a user
// Note: In the new architecture, each user has exactly one provider
func (s *Service) GetUserProviders(ctx context.Context, userID string) ([]UserProvider, error) {
	repoProviders, err := s.userRepo.GetProviders(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user provider: %w", err)
	}

	// Convert repository providers to service providers
	providers := make([]UserProvider, 0, len(repoProviders))
	for _, rp := range repoProviders {
		providers = append(providers, UserProvider{
			ID:             rp.ID,
			UserID:         rp.UserID,
			Provider:       rp.Provider,
			ProviderUserID: rp.ProviderUserID,
			Email:          rp.Email,
			IsPrimary:      rp.IsPrimary,
			CreatedAt:      rp.CreatedAt,
			LastLogin:      rp.LastLogin,
		})
	}

	return providers, nil
}

// GetPrimaryProviderID gets the provider user ID for a user
// Note: In the new architecture, each user has exactly one provider stored directly on the users table
func (s *Service) GetPrimaryProviderID(ctx context.Context, userID string) (string, error) {
	return s.userRepo.GetPrimaryProviderID(ctx, userID)
}

// GetUserByProviderID gets a user by provider and provider user ID
func (s *Service) GetUserByProviderID(ctx context.Context, provider, providerUserID string) (User, error) {
	// Try cache first
	cachedUser, err := s.GetCachedUserByProvider(ctx, provider, providerUserID)
	if err == nil && cachedUser != nil {
		return *cachedUser, nil
	}

	repoUser, err := s.userRepo.GetByProviderID(ctx, provider, providerUserID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return User{}, errors.New("user not found")
		}
		return User{}, fmt.Errorf("failed to get user by provider ID: %w", err)
	}

	user := convertRepoUserToServiceUser(repoUser)

	// Cache the user for future lookups
	if cacheErr := s.CacheUser(ctx, user); cacheErr != nil {
		logger := slogging.Get()
		logger.Warn("Failed to cache user after lookup: %v", cacheErr)
		// Don't fail the request, just log the cache error
	}

	return user, nil
}

// GetUserByProviderAndEmail gets a user by provider and email address
// This is used as a fallback when provider_user_id doesn't match but same provider + email does
func (s *Service) GetUserByProviderAndEmail(ctx context.Context, provider, email string) (User, error) {
	repoUser, err := s.userRepo.GetByProviderAndEmail(ctx, provider, email)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return User{}, errors.New("user not found")
		}
		return User{}, fmt.Errorf("failed to get user by provider and email: %w", err)
	}

	return convertRepoUserToServiceUser(repoUser), nil
}

// GetUserByAnyProviderID gets a user by provider ID across all providers
// This allows provider-independent authorization using IdP user IDs
// NOTE: This can return ambiguous results if the same provider_user_id exists for multiple providers
func (s *Service) GetUserByAnyProviderID(ctx context.Context, providerUserID string) (User, error) {
	repoUser, err := s.userRepo.GetByAnyProviderID(ctx, providerUserID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return User{}, errors.New("user not found")
		}
		return User{}, fmt.Errorf("failed to get user by provider ID: %w", err)
	}

	return convertRepoUserToServiceUser(repoUser), nil
}

// User Caching Methods

const (
	// UserCacheTTL defines how long user data is cached
	UserCacheTTL = 15 * time.Minute
)

// CacheUser stores a user in Redis cache with multiple lookup keys
func (s *Service) CacheUser(ctx context.Context, user User) error {
	logger := slogging.Get()
	redis := s.dbManager.Redis()
	builder := db.NewRedisKeyBuilder()

	// Marshal user data
	data, err := json.Marshal(user)
	if err != nil {
		logger.Error("Failed to marshal user for cache: %v", err)
		return fmt.Errorf("failed to marshal user: %w", err)
	}

	// Cache by internal UUID (primary key)
	keyByID := builder.CacheUserKey(user.InternalUUID)
	if err := redis.Set(ctx, keyByID, string(data), UserCacheTTL); err != nil {
		logger.Error("Failed to cache user by ID %s: %v", user.InternalUUID, err)
		return fmt.Errorf("failed to cache user by ID: %w", err)
	}

	// Cache by email (secondary key)
	keyByEmail := builder.CacheUserByEmailKey(user.Email)
	if err := redis.Set(ctx, keyByEmail, string(data), UserCacheTTL); err != nil {
		logger.Error("Failed to cache user by email %s: %v", user.Email, err)
		return fmt.Errorf("failed to cache user by email: %w", err)
	}

	// Cache by provider + provider_user_id (tertiary key)
	keyByProvider := builder.CacheUserByProviderKey(user.Provider, user.ProviderUserID)
	if err := redis.Set(ctx, keyByProvider, string(data), UserCacheTTL); err != nil {
		logger.Error("Failed to cache user by provider %s:%s: %v", user.Provider, user.ProviderUserID, err)
		return fmt.Errorf("failed to cache user by provider: %w", err)
	}

	logger.Debug("Cached user %s with TTL %v", user.InternalUUID, UserCacheTTL)
	return nil
}

// GetCachedUserByID retrieves a user from cache by internal UUID
func (s *Service) GetCachedUserByID(ctx context.Context, userID string) (*User, error) {
	logger := slogging.Get()
	redis := s.dbManager.Redis()
	builder := db.NewRedisKeyBuilder()

	key := builder.CacheUserKey(userID)
	data, err := redis.Get(ctx, key)
	if err != nil {
		if err.Error() == redisNilError {
			logger.Debug("Cache miss for user ID %s", userID)
			return nil, nil // Cache miss
		}
		logger.Error("Failed to get cached user by ID %s: %v", userID, err)
		return nil, fmt.Errorf("failed to get cached user: %w", err)
	}

	var user User
	if err := json.Unmarshal([]byte(data), &user); err != nil {
		logger.Error("Failed to unmarshal cached user %s: %v", userID, err)
		return nil, fmt.Errorf("failed to unmarshal cached user: %w", err)
	}

	logger.Debug("Cache hit for user ID %s", userID)
	return &user, nil
}

// GetCachedUserByEmail retrieves a user from cache by email
func (s *Service) GetCachedUserByEmail(ctx context.Context, email string) (*User, error) {
	logger := slogging.Get()
	redis := s.dbManager.Redis()
	builder := db.NewRedisKeyBuilder()

	key := builder.CacheUserByEmailKey(email)
	data, err := redis.Get(ctx, key)
	if err != nil {
		if err.Error() == redisNilError {
			logger.Debug("Cache miss for user email %s", email)
			return nil, nil // Cache miss
		}
		logger.Error("Failed to get cached user by email %s: %v", email, err)
		return nil, fmt.Errorf("failed to get cached user: %w", err)
	}

	var user User
	if err := json.Unmarshal([]byte(data), &user); err != nil {
		logger.Error("Failed to unmarshal cached user %s: %v", email, err)
		return nil, fmt.Errorf("failed to unmarshal cached user: %w", err)
	}

	logger.Debug("Cache hit for user email %s", email)
	return &user, nil
}

// GetCachedUserByProvider retrieves a user from cache by provider and provider user ID
func (s *Service) GetCachedUserByProvider(ctx context.Context, provider, providerUserID string) (*User, error) {
	logger := slogging.Get()
	redis := s.dbManager.Redis()
	builder := db.NewRedisKeyBuilder()

	key := builder.CacheUserByProviderKey(provider, providerUserID)
	data, err := redis.Get(ctx, key)
	if err != nil {
		if err.Error() == redisNilError {
			logger.Debug("Cache miss for user provider %s:%s", provider, providerUserID)
			return nil, nil // Cache miss
		}
		logger.Error("Failed to get cached user by provider %s:%s: %v", provider, providerUserID, err)
		return nil, fmt.Errorf("failed to get cached user: %w", err)
	}

	var user User
	if err := json.Unmarshal([]byte(data), &user); err != nil {
		logger.Error("Failed to unmarshal cached user %s:%s: %v", provider, providerUserID, err)
		return nil, fmt.Errorf("failed to unmarshal cached user: %w", err)
	}

	logger.Debug("Cache hit for user provider %s:%s", provider, providerUserID)
	return &user, nil
}

// InvalidateUserCache removes a user from all cache keys
func (s *Service) InvalidateUserCache(ctx context.Context, user User) error {
	logger := slogging.Get()
	redis := s.dbManager.Redis()
	builder := db.NewRedisKeyBuilder()

	// Delete all three cache keys
	keys := []string{
		builder.CacheUserKey(user.InternalUUID),
		builder.CacheUserByEmailKey(user.Email),
		builder.CacheUserByProviderKey(user.Provider, user.ProviderUserID),
	}

	for _, key := range keys {
		if err := redis.Del(ctx, key); err != nil {
			logger.Error("Failed to invalidate user cache key %s: %v", key, err)
			return fmt.Errorf("failed to invalidate user cache: %w", err)
		}
	}

	logger.Debug("Invalidated user cache for %s", user.InternalUUID)
	return nil
}

// deriveIssuer derives the issuer URL from the OAuth callback URL
func (s *Service) deriveIssuer() string {
	// Parse the OAuth callback URL to extract the base URL
	callbackURL := s.config.OAuth.CallbackURL
	if callbackURL == "" {
		// Fallback to a reasonable default
		return "http://localhost:8080"
	}

	// Parse the URL
	parsedURL, err := url.Parse(callbackURL)
	if err != nil {
		// If parsing fails, return the full callback URL
		return callbackURL
	}

	// Return just the scheme and host (without path)
	return fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)
}

// CacheUserGroups caches user groups in Redis for the session duration
func (s *Service) CacheUserGroups(ctx context.Context, email, idp string, groups []string) error {
	redis := s.dbManager.Redis()
	if redis == nil {
		// No Redis available, skip caching
		return nil
	}

	// Store groups as JSON in Redis with same TTL as JWT
	key := fmt.Sprintf("user_groups:%s", email)
	data := map[string]any{
		"email":     email,
		"idp":       idp,
		"groups":    groups,
		"cached_at": time.Now().Unix(),
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal group data: %w", err)
	}

	// Cache for the same duration as the JWT token
	ttl := s.config.GetJWTDuration()
	if err := redis.Set(ctx, key, string(jsonData), ttl); err != nil {
		return fmt.Errorf("failed to cache user groups: %w", err)
	}

	return nil
}

// GetCachedGroups retrieves cached user groups from Redis
func (s *Service) GetCachedGroups(ctx context.Context, email string) (string, []string, error) {
	redis := s.dbManager.Redis()
	if redis == nil {
		// No Redis available, return empty
		return "", nil, nil
	}

	key := fmt.Sprintf("user_groups:%s", email)
	jsonData, err := redis.Get(ctx, key)
	if err != nil {
		// Check if key doesn't exist (redis returns specific error for nil)
		// This is not an error condition, just means no cached groups
		return "", nil, nil //nolint:nilerr // cache miss is not an error
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return "", nil, fmt.Errorf("failed to unmarshal group data: %w", err)
	}

	idp, _ := data["idp"].(string)
	groupsInterface, _ := data["groups"].([]any)

	var groups []string
	for _, g := range groupsInterface {
		if groupStr, ok := g.(string); ok {
			groups = append(groups, groupStr)
		}
	}

	return idp, groups, nil
}

// ClearUserGroups clears cached user groups from Redis (used on logout)
func (s *Service) ClearUserGroups(ctx context.Context, email string) error {
	redis := s.dbManager.Redis()
	if redis == nil {
		// No Redis available, skip
		return nil
	}

	key := fmt.Sprintf("user_groups:%s", email)
	if err := redis.Del(ctx, key); err != nil {
		// Ignore error if key doesn't exist
		return nil //nolint:nilerr // deleting nonexistent key is not an error
	}

	return nil
}

// HandleClientCredentialsGrant processes OAuth 2.0 Client Credentials Grant (RFC 6749 Section 4.4)
// Returns an access token for machine-to-machine authentication
func (s *Service) HandleClientCredentialsGrant(ctx context.Context, clientID, clientSecret string) (*TokenPair, error) {
	logger := slogging.Get()

	// 1. Validate client credentials
	creds, err := s.GetClientCredentialByClientID(ctx, clientID)
	if err != nil {
		logger.Warn("Client credentials not found: client_id=%s", clientID)
		return nil, fmt.Errorf("invalid_client")
	}

	// 2. Verify client secret (bcrypt)
	if err := bcrypt.CompareHashAndPassword([]byte(creds.ClientSecretHash), []byte(clientSecret)); err != nil {
		logger.Warn("Invalid client secret for client_id=%s", clientID)
		return nil, fmt.Errorf("invalid_client")
	}

	// 3. Check if credential is active and not expired
	if !creds.IsActive {
		logger.Warn("Inactive client credential: client_id=%s", clientID)
		return nil, fmt.Errorf("invalid_client")
	}

	if creds.ExpiresAt != nil && time.Now().After(*creds.ExpiresAt) {
		logger.Warn("Expired client credential: client_id=%s, expires_at=%v", clientID, creds.ExpiresAt)
		return nil, fmt.Errorf("invalid_client")
	}

	// 4. Load owner user from database
	owner, err := s.GetUserByID(ctx, creds.OwnerUUID.String())
	if err != nil {
		logger.Error("Failed to load owner user: uuid=%s, error=%v", creds.OwnerUUID, err)
		return nil, fmt.Errorf("server_error")
	}

	// 5. Generate JWT with service account identity
	// Subject format: "sa:{credential_id}:{owner_provider_user_id}"
	subject := fmt.Sprintf("sa:%s:%s", creds.ID.String(), owner.ProviderUserID)

	claims := Claims{
		Email:            owner.Email,
		Name:             fmt.Sprintf("[Service Account] %s", creds.Name),
		IdentityProvider: "tmi",
		Groups:           owner.Groups,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			Issuer:    s.getIssuer(),
			Audience:  jwt.ClaimStrings{s.getIssuer()},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(s.config.JWT.ExpirationSeconds) * time.Second)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	accessToken, err := s.keyManager.CreateToken(&claims)
	if err != nil {
		logger.Error("Failed to generate JWT: error=%v", err)
		return nil, fmt.Errorf("server_error")
	}

	// 6. Update last_used_at timestamp
	if err := s.UpdateClientCredentialLastUsed(ctx, creds.ID); err != nil {
		logger.Warn("Failed to update last_used_at: client_id=%s, error=%v", clientID, err)
		// Don't fail the request if we can't update timestamp
	}

	// 7. Log service account token issuance
	logger.Info("Service account token issued: client_id=%s, name=%s, owner=%s",
		clientID, creds.Name, owner.Email)

	// 8. Return token response (no refresh token for CCG per RFC 6749 Section 4.4.3)
	return &TokenPair{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresIn:   s.config.JWT.ExpirationSeconds,
		// Note: RefreshToken intentionally omitted for Client Credentials Grant
	}, nil
}

// getIssuer returns the JWT issuer URL
func (s *Service) getIssuer() string {
	// Use the callback URL base as the issuer
	if s.config.OAuth.CallbackURL != "" {
		if parsedURL, err := url.Parse(s.config.OAuth.CallbackURL); err == nil {
			return fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)
		}
	}
	// Fallback to default
	return "http://localhost:8080"
}

// convertRepoUserToServiceUser converts a repository User to a service User
func convertRepoUserToServiceUser(repoUser *repository.User) User {
	return User{
		InternalUUID:   repoUser.InternalUUID,
		Provider:       repoUser.Provider,
		ProviderUserID: repoUser.ProviderUserID,
		Email:          repoUser.Email,
		Name:           repoUser.Name,
		EmailVerified:  repoUser.EmailVerified,
		AccessToken:    repoUser.AccessToken,
		RefreshToken:   repoUser.RefreshToken,
		TokenExpiry:    repoUser.TokenExpiry,
		CreatedAt:      repoUser.CreatedAt,
		ModifiedAt:     repoUser.ModifiedAt,
		LastLogin:      repoUser.LastLogin,
	}
}

// convertServiceUserToRepoUser converts a service User to a repository User
func convertServiceUserToRepoUser(user *User) *repository.User {
	return &repository.User{
		InternalUUID:   user.InternalUUID,
		Provider:       user.Provider,
		ProviderUserID: user.ProviderUserID,
		Email:          user.Email,
		Name:           user.Name,
		EmailVerified:  user.EmailVerified,
		AccessToken:    user.AccessToken,
		RefreshToken:   user.RefreshToken,
		TokenExpiry:    user.TokenExpiry,
		CreatedAt:      user.CreatedAt,
		ModifiedAt:     user.ModifiedAt,
		LastLogin:      user.LastLogin,
	}
}
