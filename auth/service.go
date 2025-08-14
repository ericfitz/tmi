package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/logging"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Service provides authentication and authorization functionality
type Service struct {
	dbManager *db.Manager
	config    Config
}

// NewService creates a new authentication service
func NewService(dbManager *db.Manager, config Config) (*Service, error) {
	if dbManager == nil {
		return nil, errors.New("database manager is required")
	}

	if err := config.ValidateConfig(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Service{
		dbManager: dbManager,
		config:    config,
	}, nil
}

// User represents a user in the system
type User struct {
	ID         string    `json:"id"`
	Email      string    `json:"email"`
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"created_at"`
	ModifiedAt time.Time `json:"modified_at"`
	LastLogin  time.Time `json:"last_login,omitempty"`
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
	Email string `json:"email"`
	Name  string `json:"name"`
	jwt.RegisteredClaims
}

// GenerateTokens generates a new JWT token pair for a user
func (s *Service) GenerateTokens(ctx context.Context, user User) (TokenPair, error) {
	// Create the JWT claims
	expirationTime := time.Now().Add(s.config.GetJWTDuration())
	claims := &Claims{
		Email: user.Email,
		Name:  user.Name,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.Email,
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			ID:        uuid.New().String(),
		},
	}

	// Create the JWT token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(s.config.JWT.Secret))
	if err != nil {
		return TokenPair{}, fmt.Errorf("failed to sign token: %w", err)
	}

	// Generate a refresh token
	refreshToken := uuid.New().String()
	refreshDuration := 30 * 24 * time.Hour // 30 days

	// Store the refresh token in Redis
	refreshKey := fmt.Sprintf("refresh_token:%s", refreshToken)
	err = s.dbManager.Redis().Set(ctx, refreshKey, user.Email, refreshDuration)
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
	// Parse the token
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate the signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.config.JWT.Secret), nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
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

	return claims, nil
}

// RefreshToken refreshes an access token using a refresh token
func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (TokenPair, error) {
	// Get the user email from Redis
	refreshKey := fmt.Sprintf("refresh_token:%s", refreshToken)
	email, err := s.dbManager.Redis().Get(ctx, refreshKey)
	if err != nil {
		return TokenPair{}, fmt.Errorf("invalid refresh token: %w", err)
	}

	// Delete the old refresh token
	if err := s.dbManager.Redis().Del(ctx, refreshKey); err != nil {
		return TokenPair{}, fmt.Errorf("failed to delete refresh token: %w", err)
	}

	// Get the user from the database
	user, err := s.GetUserByEmail(ctx, email)
	if err != nil {
		return TokenPair{}, fmt.Errorf("failed to get user: %w", err)
	}

	// Update the last login time
	user.LastLogin = time.Now()
	if err := s.UpdateUser(ctx, user); err != nil {
		// Log the error but continue
		fmt.Printf("Failed to update user last login: %v\n", err)
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
	query := `SELECT id, email, name, created_at, modified_at, last_login FROM users WHERE email = $1`
	err := db.QueryRowContext(ctx, query, email).Scan(
		&user.ID,
		&user.Email,
		&user.Name,
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
		INSERT INTO users (id, email, name, created_at, modified_at, last_login)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`

	err := db.QueryRowContext(ctx, query,
		user.ID,
		user.Email,
		user.Name,
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
		SET email = $2, name = $3, modified_at = $4, last_login = $5
		WHERE id = $1
	`

	result, err := db.ExecContext(ctx, query,
		user.ID,
		user.Email,
		user.Name,
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
			logging.Get().Error("Error closing rows: %v", err)
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
		SELECT id, email, name, created_at, modified_at, last_login
		FROM users
		WHERE id = $1
	`, userID).Scan(
		&user.ID,
		&user.Email,
		&user.Name,
		&user.CreatedAt,
		&user.ModifiedAt,
		&user.LastLogin,
	)

	if err != nil {
		return User{}, fmt.Errorf("failed to get user: %w", err)
	}

	return user, nil
}
