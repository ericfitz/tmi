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
	dbManager  *db.Manager
	config     Config
	keyManager *JWTKeyManager
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

	return &Service{
		dbManager:  dbManager,
		config:     config,
		keyManager: keyManager,
	}, nil
}

// GetKeyManager returns the JWT key manager (getter for unexported field)
func (s *Service) GetKeyManager() *JWTKeyManager {
	return s.keyManager
}

// User represents a user in the system
type User struct {
	ID               string    `json:"id"`
	Email            string    `json:"email"`
	Name             string    `json:"name"`
	EmailVerified    bool      `json:"email_verified"`
	GivenName        string    `json:"given_name,omitempty"`
	FamilyName       string    `json:"family_name,omitempty"`
	Picture          string    `json:"picture,omitempty"`
	Locale           string    `json:"locale,omitempty"`
	IdentityProvider string    `json:"idp,omitempty"`       // Current identity provider
	Groups           []string  `json:"groups,omitempty"`    // Groups from identity provider (not stored in DB)
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
	GivenName        string   `json:"given_name,omitempty"`
	FamilyName       string   `json:"family_name,omitempty"`
	Picture          string   `json:"picture,omitempty"`
	Locale           string   `json:"locale,omitempty"`
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
		user.GivenName = userInfo.GivenName
		user.FamilyName = userInfo.FamilyName
		user.Picture = userInfo.Picture
		if userInfo.Locale != "" {
			user.Locale = userInfo.Locale
		}
		// Set IdP and groups from the fresh UserInfo
		if userInfo.IdP != "" {
			user.IdentityProvider = userInfo.IdP
			// Cache groups in Redis if available
			if len(userInfo.Groups) > 0 {
				s.CacheUserGroups(ctx, user.Email, userInfo.IdP, userInfo.Groups)
			}
		}
		user.Groups = userInfo.Groups

		// Update the user in the database with fresh provider data (except groups)
		if err := s.UpdateUser(ctx, user); err != nil {
			// Log error but continue - token generation shouldn't fail due to update issues
			slogging.Get().Error("Failed to update user provider data: %v", err)
		}
	}

	// Get the primary provider ID for the user
	providerID, err := s.GetPrimaryProviderID(ctx, user.ID)
	if err != nil {
		return TokenPair{}, fmt.Errorf("failed to get primary provider ID: %w", err)
	}

	if providerID == "" {
		return TokenPair{}, fmt.Errorf("no primary provider ID found for user %s", user.ID)
	}

	// Derive the issuer from the OAuth callback URL
	issuer := s.deriveIssuer()

	// Create the JWT claims using the user's stored data
	expirationTime := time.Now().Add(s.config.GetJWTDuration())
	claims := &Claims{
		Email:            user.Email,
		EmailVerified:    user.EmailVerified,
		Name:             user.Name,
		GivenName:        user.GivenName,
		FamilyName:       user.FamilyName,
		Picture:          user.Picture,
		Locale:           user.Locale,
		IdentityProvider: user.IdentityProvider,
		Groups:           user.Groups,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   providerID,
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

	// Store the refresh token in Redis
	refreshKey := fmt.Sprintf("refresh_token:%s", refreshToken)
	err = s.dbManager.Redis().Set(ctx, refreshKey, user.ID, refreshDuration)
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
	db := s.dbManager.Postgres().GetDB()

	var user User
	query := `SELECT id, email, name, email_verified, given_name, family_name, picture, locale, identity_provider, created_at, modified_at, last_login FROM users WHERE email = $1`
	err := db.QueryRowContext(ctx, query, email).Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.EmailVerified,
		&user.GivenName,
		&user.FamilyName,
		&user.Picture,
		&user.Locale,
		&user.IdentityProvider,
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

	return user, nil
}

// GetUserByID gets a user by ID
func (s *Service) GetUserByID(ctx context.Context, id string) (User, error) {
	db := s.dbManager.Postgres().GetDB()

	var user User
	query := `SELECT id, email, name, email_verified, given_name, family_name, picture, locale, identity_provider, created_at, modified_at, last_login FROM users WHERE id = $1`
	err := db.QueryRowContext(ctx, query, id).Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.EmailVerified,
		&user.GivenName,
		&user.FamilyName,
		&user.Picture,
		&user.Locale,
		&user.IdentityProvider,
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

	return user, nil
}

// GetUserWithProviderID gets a user by email with their primary provider ID
func (s *Service) GetUserWithProviderID(ctx context.Context, email string) (User, error) {
	db := s.dbManager.Postgres().GetDB()

	var user User
	var providerUserID sql.NullString
	query := `
		SELECT u.id, u.email, u.name, u.email_verified, u.given_name, u.family_name, 
		       u.picture, u.locale, u.created_at, u.modified_at, u.last_login,
		       up.provider_user_id
		FROM users u
		LEFT JOIN user_providers up ON u.id = up.user_id AND up.is_primary = true
		WHERE u.email = $1
	`
	err := db.QueryRowContext(ctx, query, email).Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.EmailVerified,
		&user.GivenName,
		&user.FamilyName,
		&user.Picture,
		&user.Locale,
		&user.CreatedAt,
		&user.ModifiedAt,
		&user.LastLogin,
		&providerUserID,
	)

	if err == sql.ErrNoRows {
		return User{}, errors.New("user not found")
	}

	if err != nil {
		return User{}, fmt.Errorf("failed to get user: %w", err)
	}

	return user, nil
}

// CreateUser creates a new user
func (s *Service) CreateUser(ctx context.Context, user User) (User, error) {
	db := s.dbManager.Postgres().GetDB()

	// Generate a new ID if not provided
	if user.ID == "" {
		user.ID = uuid.New().String()
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
		INSERT INTO users (id, email, name, email_verified, given_name, family_name, picture, locale, identity_provider, created_at, modified_at, last_login)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id
	`

	err := db.QueryRowContext(ctx, query,
		user.ID,
		user.Email,
		user.Name,
		user.EmailVerified,
		user.GivenName,
		user.FamilyName,
		user.Picture,
		user.Locale,
		user.IdentityProvider,
		user.CreatedAt,
		user.ModifiedAt,
		user.LastLogin,
	).Scan(&user.ID)

	if err != nil {
		return User{}, fmt.Errorf("failed to create user: %w", err)
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
		SET email = $2, name = $3, email_verified = $4, given_name = $5, family_name = $6,
		    picture = $7, locale = $8, identity_provider = $9, modified_at = $10, last_login = $11
		WHERE id = $1
	`

	result, err := db.ExecContext(ctx, query,
		user.ID,
		user.Email,
		user.Name,
		user.EmailVerified,
		user.GivenName,
		user.FamilyName,
		user.Picture,
		user.Locale,
		user.IdentityProvider,
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

	return nil
}

// DeleteUser deletes a user
func (s *Service) DeleteUser(ctx context.Context, id string) error {
	db := s.dbManager.Postgres().GetDB()

	query := `DELETE FROM users WHERE id = $1`

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

// GetUserProviders gets the OAuth providers for a user
func (s *Service) GetUserProviders(ctx context.Context, userID string) ([]UserProvider, error) {
	db := s.dbManager.Postgres().GetDB()

	query := `
		SELECT id, user_id, provider, provider_user_id, email, is_primary, created_at, last_login
		FROM user_providers
		WHERE user_id = $1
	`

	rows, err := db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user providers: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slogging.Get().Error("Error closing rows: %v", err)
		}
	}()

	var providers []UserProvider
	for rows.Next() {
		var provider UserProvider
		err := rows.Scan(
			&provider.ID,
			&provider.UserID,
			&provider.Provider,
			&provider.ProviderUserID,
			&provider.Email,
			&provider.IsPrimary,
			&provider.CreatedAt,
			&provider.LastLogin,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan user provider: %w", err)
		}
		providers = append(providers, provider)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating user providers: %w", err)
	}

	return providers, nil
}

// LinkUserProvider links an OAuth provider to a user
func (s *Service) LinkUserProvider(ctx context.Context, userID, provider, providerUserID, email string) error {
	db := s.dbManager.Postgres().GetDB()

	// Check if the provider is already linked to the user
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM user_providers
		WHERE user_id = $1 AND provider = $2
	`, userID, provider).Scan(&count)

	if err != nil {
		return fmt.Errorf("failed to check existing provider: %w", err)
	}

	if count > 0 {
		// Provider already linked, update it
		_, err := db.ExecContext(ctx, `
			UPDATE user_providers
			SET provider_user_id = $3, email = $4, last_login = $5
			WHERE user_id = $1 AND provider = $2
		`, userID, provider, providerUserID, email, time.Now())

		if err != nil {
			return fmt.Errorf("failed to update provider: %w", err)
		}
	} else {
		// Provider not linked, insert it
		_, err := db.ExecContext(ctx, `
			INSERT INTO user_providers (id, user_id, provider, provider_user_id, email, is_primary, created_at, last_login)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, uuid.New().String(), userID, provider, providerUserID, email, count == 0, time.Now(), time.Now())

		if err != nil {
			return fmt.Errorf("failed to link provider: %w", err)
		}
	}

	return nil
}

// UnlinkUserProvider unlinks an OAuth provider from a user
func (s *Service) UnlinkUserProvider(ctx context.Context, userID, provider string) error {
	db := s.dbManager.Postgres().GetDB()

	// Check if this is the only provider for the user
	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM user_providers
		WHERE user_id = $1
	`, userID).Scan(&count)

	if err != nil {
		return fmt.Errorf("failed to count user providers: %w", err)
	}

	if count <= 1 {
		return errors.New("cannot unlink the only provider for a user")
	}

	// Delete the provider
	result, err := db.ExecContext(ctx, `
		DELETE FROM user_providers
		WHERE user_id = $1 AND provider = $2
	`, userID, provider)

	if err != nil {
		return fmt.Errorf("failed to unlink provider: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return errors.New("provider not found")
	}

	return nil
}

// GetPrimaryProviderID gets the primary provider ID for a user
func (s *Service) GetPrimaryProviderID(ctx context.Context, userID string) (string, error) {
	db := s.dbManager.Postgres().GetDB()

	var providerUserID string
	query := `
		SELECT provider_user_id 
		FROM user_providers 
		WHERE user_id = $1 AND is_primary = true
		LIMIT 1
	`
	err := db.QueryRowContext(ctx, query, userID).Scan(&providerUserID)
	if err == sql.ErrNoRows {
		return "", nil // No primary provider
	}
	if err != nil {
		return "", fmt.Errorf("failed to get primary provider ID: %w", err)
	}
	return providerUserID, nil
}

// GetUserByProviderID gets a user by provider ID
func (s *Service) GetUserByProviderID(ctx context.Context, provider, providerUserID string) (User, error) {
	db := s.dbManager.Postgres().GetDB()

	var userID string
	err := db.QueryRowContext(ctx, `
		SELECT user_id FROM user_providers
		WHERE provider = $1 AND provider_user_id = $2
	`, provider, providerUserID).Scan(&userID)

	if err == sql.ErrNoRows {
		return User{}, errors.New("user not found")
	}

	if err != nil {
		return User{}, fmt.Errorf("failed to get user by provider ID: %w", err)
	}

	// Get the user
	var user User
	err = db.QueryRowContext(ctx, `
		SELECT id, email, name, email_verified, given_name, family_name, picture, locale, created_at, modified_at, last_login
		FROM users
		WHERE id = $1
	`, userID).Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.EmailVerified,
		&user.GivenName,
		&user.FamilyName,
		&user.Picture,
		&user.Locale,
		&user.CreatedAt,
		&user.ModifiedAt,
		&user.LastLogin,
	)

	if err != nil {
		return User{}, fmt.Errorf("failed to get user: %w", err)
	}

	return user, nil
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
