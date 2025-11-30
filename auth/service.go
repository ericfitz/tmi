package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Service provides authentication and authorization functionality
type Service struct {
	dbManager   *db.Manager
	config      Config
	keyManager  *JWTKeyManager
	samlManager *SAMLManager
	stateStore  StateStore
}

// NewService creates a new authentication service
func NewService(dbManager *db.Manager, config Config) (*Service, error) {
	if dbManager == nil {
		return nil, errors.New("database manager is required")
	}

	if err := config.ValidateConfig(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Initialize JWT key manager
	keyManager, err := NewJWTKeyManager(config.JWT)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize JWT key manager: %w", err)
	}

	// Initialize state store (in-memory for now, can be Redis later)
	stateStore := NewInMemoryStateStore()

	// Create service instance
	service := &Service{
		dbManager:  dbManager,
		config:     config,
		keyManager: keyManager,
		stateStore: stateStore,
	}

	// Initialize SAML manager if configured
	if config.SAML.Enabled {
		samlManager := NewSAMLManager(service)
		if err := samlManager.InitializeProviders(config.SAML, stateStore); err != nil {
			return nil, fmt.Errorf("failed to initialize SAML providers: %w", err)
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

// User represents a user in the system
type User struct {
	InternalUUID     string    `json:"-"`                // Internal system UUID (NEVER exposed in API responses or JWT)
	Provider         string    `json:"provider"`         // OAuth provider: "test", "google", "github", "microsoft", "azure"
	ProviderUserID   string    `json:"provider_user_id"` // Provider's user ID (from JWT sub claim)
	Email            string    `json:"email"`
	Name             string    `json:"name"` // Display name for UI presentation
	EmailVerified    bool      `json:"email_verified"`
	AccessToken      string    `json:"-"`                // OAuth access token (not exposed in JSON)
	RefreshToken     string    `json:"-"`                // OAuth refresh token (not exposed in JSON)
	TokenExpiry      time.Time `json:"-"`                // Token expiration time (not exposed in JSON)
	IdentityProvider string    `json:"idp,omitempty"`    // DEPRECATED: Use Provider instead (kept for backward compatibility)
	Groups           []string  `json:"groups,omitempty"` // Groups from identity provider (not stored in DB)
	IsAdmin          bool      `json:"is_admin"`         // Whether user has administrator privileges
	CreatedAt        time.Time `json:"created_at"`
	ModifiedAt       time.Time `json:"modified_at"`
	LastLogin        time.Time `json:"last_login,omitempty"`
}

// TokenPair contains an access token and a refresh token
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// Claims represents the JWT claims
type Claims struct {
	Email            string   `json:"email"`
	EmailVerified    bool     `json:"email_verified,omitempty"`
	Name             string   `json:"name"`
	IdentityProvider string   `json:"idp,omitempty"`    // Identity provider
	Groups           []string `json:"groups,omitempty"` // User's groups from IdP
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
			user.IdentityProvider = userInfo.IdP
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
		IdentityProvider: user.IdentityProvider,
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
	audienceValid := false
	for _, aud := range claims.Audience {
		if aud == expectedIssuer {
			audienceValid = true
			break
		}
	}
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
	user.LastLogin = time.Now()
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

// GetUserByEmail gets a user by email
func (s *Service) GetUserByEmail(ctx context.Context, email string) (User, error) {
	// Try cache first
	cachedUser, err := s.GetCachedUserByEmail(ctx, email)
	if err == nil && cachedUser != nil {
		return *cachedUser, nil
	}

	db := s.dbManager.Postgres().GetDB()

	var user User
	query := `SELECT internal_uuid, provider, provider_user_id, email, name, email_verified, access_token, refresh_token, token_expiry, created_at, modified_at, last_login FROM users WHERE email = $1`
	err = db.QueryRowContext(ctx, query, email).Scan(
		&user.InternalUUID,
		&user.Provider,
		&user.ProviderUserID,
		&user.Email,
		&user.Name,
		&user.EmailVerified,
		&user.AccessToken,
		&user.RefreshToken,
		&user.TokenExpiry,
		&user.CreatedAt,
		&user.ModifiedAt,
		&user.LastLogin,
	)

	if err == sql.ErrNoRows {
		return User{}, errors.New("user not found")
	}

	if err != nil {
		return User{}, fmt.Errorf("failed to get user: %w", err)
	}

	// Set IdentityProvider for backward compatibility
	user.IdentityProvider = user.Provider

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

	db := s.dbManager.Postgres().GetDB()

	var user User
	query := `SELECT internal_uuid, provider, provider_user_id, email, name, email_verified, access_token, refresh_token, token_expiry, created_at, modified_at, last_login FROM users WHERE internal_uuid = $1`
	err = db.QueryRowContext(ctx, query, id).Scan(
		&user.InternalUUID,
		&user.Provider,
		&user.ProviderUserID,
		&user.Email,
		&user.Name,
		&user.EmailVerified,
		&user.AccessToken,
		&user.RefreshToken,
		&user.TokenExpiry,
		&user.CreatedAt,
		&user.ModifiedAt,
		&user.LastLogin,
	)

	if err == sql.ErrNoRows {
		return User{}, errors.New("user not found")
	}

	if err != nil {
		return User{}, fmt.Errorf("failed to get user: %w", err)
	}

	// Set IdentityProvider for backward compatibility
	user.IdentityProvider = user.Provider

	// Cache the user for future lookups
	if cacheErr := s.CacheUser(ctx, user); cacheErr != nil {
		logger := slogging.Get()
		logger.Warn("Failed to cache user after lookup: %v", cacheErr)
		// Don't fail the request, just log the cache error
	}

	return user, nil
}

// GetUserWithProviderID gets a user by email - DEPRECATED: provider ID is now on User struct
func (s *Service) GetUserWithProviderID(ctx context.Context, email string) (User, error) {
	// This function is now just an alias for GetUserByEmail since provider info is on User
	return s.GetUserByEmail(ctx, email)
}

// CreateUser creates a new user
func (s *Service) CreateUser(ctx context.Context, user User) (User, error) {
	db := s.dbManager.Postgres().GetDB()

	// Generate a new internal UUID if not provided
	if user.InternalUUID == "" {
		user.InternalUUID = uuid.New().String()
	}

	// Provider and ProviderUserID must be set by caller
	if user.Provider == "" || user.ProviderUserID == "" {
		return User{}, errors.New("provider and provider_user_id are required")
	}

	// Set timestamps if not provided
	now := time.Now()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	if user.ModifiedAt.IsZero() {
		user.ModifiedAt = now
	}

	query := `
		INSERT INTO users (internal_uuid, provider, provider_user_id, email, name, email_verified, access_token, refresh_token, token_expiry, created_at, modified_at, last_login)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING internal_uuid
	`

	err := db.QueryRowContext(ctx, query,
		user.InternalUUID,
		user.Provider,
		user.ProviderUserID,
		user.Email,
		user.Name,
		user.EmailVerified,
		user.AccessToken,
		user.RefreshToken,
		user.TokenExpiry,
		user.CreatedAt,
		user.ModifiedAt,
		user.LastLogin,
	).Scan(&user.InternalUUID)

	if err != nil {
		return User{}, fmt.Errorf("failed to create user: %w", err)
	}

	// Set IdentityProvider for backward compatibility
	user.IdentityProvider = user.Provider

	// Cache the newly created user
	if cacheErr := s.CacheUser(ctx, user); cacheErr != nil {
		logger := slogging.Get()
		logger.Warn("Failed to cache newly created user: %v", cacheErr)
		// Don't fail the request, just log the cache error
	}

	return user, nil
}

// UpdateUser updates an existing user
func (s *Service) UpdateUser(ctx context.Context, user User) error {
	db := s.dbManager.Postgres().GetDB()

	// Update the modified_at timestamp
	user.ModifiedAt = time.Now()

	query := `
		UPDATE users
		SET email = $2, name = $3, email_verified = $4, access_token = $5, refresh_token = $6, token_expiry = $7,
		    modified_at = $8, last_login = $9
		WHERE internal_uuid = $1
	`

	result, err := db.ExecContext(ctx, query,
		user.InternalUUID,
		user.Email,
		user.Name,
		user.EmailVerified,
		user.AccessToken,
		user.RefreshToken,
		user.TokenExpiry,
		user.ModifiedAt,
		user.LastLogin,
	)

	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return errors.New("user not found")
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

	db := s.dbManager.Postgres().GetDB()

	query := `DELETE FROM users WHERE internal_uuid = $1`

	result, err := db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return errors.New("user not found")
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
	LastLogin      time.Time `json:"last_login,omitempty"`
}

// GetUserProviders gets the OAuth provider for a user
// Note: In the new architecture, each user has exactly one provider
func (s *Service) GetUserProviders(ctx context.Context, userID string) ([]UserProvider, error) {
	db := s.dbManager.Postgres().GetDB()

	query := `
		SELECT internal_uuid, provider, provider_user_id, email, created_at, last_login
		FROM users
		WHERE internal_uuid = $1
	`

	var user struct {
		InternalUUID   string
		Provider       string
		ProviderUserID string
		Email          string
		CreatedAt      time.Time
		LastLogin      *time.Time
	}

	err := db.QueryRowContext(ctx, query, userID).Scan(
		&user.InternalUUID,
		&user.Provider,
		&user.ProviderUserID,
		&user.Email,
		&user.CreatedAt,
		&user.LastLogin,
	)

	if err == sql.ErrNoRows {
		return []UserProvider{}, nil // User not found, return empty array
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get user provider: %w", err)
	}

	// Convert to UserProvider format (single provider)
	lastLogin := time.Time{} // Zero value
	if user.LastLogin != nil {
		lastLogin = *user.LastLogin
	}

	providers := []UserProvider{
		{
			ID:             user.InternalUUID, // Use internal UUID as provider record ID
			UserID:         user.InternalUUID,
			Provider:       user.Provider,
			ProviderUserID: user.ProviderUserID,
			Email:          user.Email,
			IsPrimary:      true, // Always true since there's only one provider per user now
			CreatedAt:      user.CreatedAt,
			LastLogin:      lastLogin,
		},
	}

	return providers, nil
}

// LinkUserProvider links an OAuth provider to a user
// LinkUserProvider is deprecated - provider information is now stored directly on the User struct
// This function is kept for backward compatibility but is now a no-op.
// Provider linking happens automatically during user creation via the provider, provider_user_id fields.
//
// Deprecated: Use CreateUser or UpdateUser with provider fields instead.
func (s *Service) LinkUserProvider(ctx context.Context, userID, provider, providerUserID, email string) error {
	// DEPRECATED: user_providers table has been eliminated
	// Provider information is now stored directly on users table (provider, provider_user_id fields)
	// This function is maintained for backward compatibility but performs no operation
	logger := slogging.Get()
	logger.Debug("LinkUserProvider called (deprecated no-op): userID=%s, provider=%s", userID, provider)
	return nil
}

// UnlinkUserProvider is deprecated - provider information is now stored directly on the User struct
// With the new architecture, each user has exactly one provider (stored in provider, provider_user_id fields).
// Unlinking a provider would require deleting the user entirely.
//
// Deprecated: Provider unlinking is not supported in the new architecture.
// Each user is tied to exactly one OAuth provider.
func (s *Service) UnlinkUserProvider(ctx context.Context, userID, provider string) error {
	// DEPRECATED: user_providers table has been eliminated
	// In the new architecture, users have a single provider (provider, provider_user_id fields)
	// Unlinking a provider is not supported - the user would need to be deleted instead
	logger := slogging.Get()
	logger.Warn("UnlinkUserProvider called (deprecated, not supported): userID=%s, provider=%s", userID, provider)
	return errors.New("unlinking providers is not supported in the current architecture - each user is tied to one provider")
}

// GetPrimaryProviderID gets the provider user ID for a user
// Note: In the new architecture, each user has exactly one provider stored directly on the users table
func (s *Service) GetPrimaryProviderID(ctx context.Context, userID string) (string, error) {
	db := s.dbManager.Postgres().GetDB()

	var providerUserID string
	query := `
		SELECT provider_user_id
		FROM users
		WHERE internal_uuid = $1
		LIMIT 1
	`
	err := db.QueryRowContext(ctx, query, userID).Scan(&providerUserID)
	if err == sql.ErrNoRows {
		return "", nil // User not found
	}
	if err != nil {
		return "", fmt.Errorf("failed to get provider user ID: %w", err)
	}
	return providerUserID, nil
}

// GetUserByProviderID gets a user by provider and provider user ID
func (s *Service) GetUserByProviderID(ctx context.Context, provider, providerUserID string) (User, error) {
	// Try cache first
	cachedUser, err := s.GetCachedUserByProvider(ctx, provider, providerUserID)
	if err == nil && cachedUser != nil {
		return *cachedUser, nil
	}

	db := s.dbManager.Postgres().GetDB()

	var user User
	query := `
		SELECT internal_uuid, provider, provider_user_id, email, name, email_verified, access_token, refresh_token, token_expiry, created_at, modified_at, last_login
		FROM users
		WHERE provider = $1 AND provider_user_id = $2
	`
	err = db.QueryRowContext(ctx, query, provider, providerUserID).Scan(
		&user.InternalUUID,
		&user.Provider,
		&user.ProviderUserID,
		&user.Email,
		&user.Name,
		&user.EmailVerified,
		&user.AccessToken,
		&user.RefreshToken,
		&user.TokenExpiry,
		&user.CreatedAt,
		&user.ModifiedAt,
		&user.LastLogin,
	)

	if err == sql.ErrNoRows {
		return User{}, errors.New("user not found")
	}

	if err != nil {
		return User{}, fmt.Errorf("failed to get user by provider ID: %w", err)
	}

	// Set IdentityProvider for backward compatibility
	user.IdentityProvider = user.Provider

	// Cache the user for future lookups
	if cacheErr := s.CacheUser(ctx, user); cacheErr != nil {
		logger := slogging.Get()
		logger.Warn("Failed to cache user after lookup: %v", cacheErr)
		// Don't fail the request, just log the cache error
	}

	return user, nil
}

// GetUserByAnyProviderID gets a user by provider ID across all providers
// This allows provider-independent authorization using IdP user IDs
// NOTE: This can return ambiguous results if the same provider_user_id exists for multiple providers
func (s *Service) GetUserByAnyProviderID(ctx context.Context, providerUserID string) (User, error) {
	db := s.dbManager.Postgres().GetDB()

	var user User
	query := `
		SELECT internal_uuid, provider, provider_user_id, email, name, email_verified, access_token, refresh_token, token_expiry, created_at, modified_at, last_login
		FROM users
		WHERE provider_user_id = $1
		LIMIT 1
	`
	err := db.QueryRowContext(ctx, query, providerUserID).Scan(
		&user.InternalUUID,
		&user.Provider,
		&user.ProviderUserID,
		&user.Email,
		&user.Name,
		&user.EmailVerified,
		&user.AccessToken,
		&user.RefreshToken,
		&user.TokenExpiry,
		&user.CreatedAt,
		&user.ModifiedAt,
		&user.LastLogin,
	)

	if err == sql.ErrNoRows {
		return User{}, errors.New("user not found")
	}

	if err != nil {
		return User{}, fmt.Errorf("failed to get user by provider ID: %w", err)
	}

	// Set IdentityProvider for backward compatibility
	user.IdentityProvider = user.Provider

	return user, nil
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
		if err.Error() == "redis: nil" {
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
		if err.Error() == "redis: nil" {
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
		if err.Error() == "redis: nil" {
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
	data := map[string]interface{}{
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
		return "", nil, nil
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return "", nil, fmt.Errorf("failed to unmarshal group data: %w", err)
	}

	idp, _ := data["idp"].(string)
	groupsInterface, _ := data["groups"].([]interface{})

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
		return nil
	}

	return nil
}
