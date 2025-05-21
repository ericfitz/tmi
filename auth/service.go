package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	LastLogin time.Time `json:"last_login,omitempty"`
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
	refreshExpiration := time.Now().Add(30 * 24 * time.Hour) // 30 days

	// Store the refresh token in Redis
	refreshKey := fmt.Sprintf("refresh_token:%s", refreshToken)
	err = s.dbManager.Redis().Set(ctx, refreshKey, user.Email, refreshExpiration.Sub(time.Now()))
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
	// TODO: Implement user retrieval from PostgreSQL
	// For now, we'll create a dummy user
	user := User{
		ID:        uuid.New().String(),
		Email:     email,
		Name:      "User",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		LastLogin: time.Now(),
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
	// TODO: Implement user retrieval from PostgreSQL
	// For now, we'll return an error
	return User{}, errors.New("user not found")
}

// CreateUser creates a new user
func (s *Service) CreateUser(ctx context.Context, user User) (User, error) {
	// TODO: Implement user creation in PostgreSQL
	// For now, we'll return the user with a generated ID
	user.ID = uuid.New().String()
	user.CreatedAt = time.Now()
	user.UpdatedAt = time.Now()
	return user, nil
}

// UpdateUser updates an existing user
func (s *Service) UpdateUser(ctx context.Context, user User) error {
	// TODO: Implement user update in PostgreSQL
	return errors.New("not implemented")
}

// DeleteUser deletes a user
func (s *Service) DeleteUser(ctx context.Context, id string) error {
	// TODO: Implement user deletion in PostgreSQL
	return errors.New("not implemented")
}

// GetUserProviders gets the OAuth providers for a user
func (s *Service) GetUserProviders(ctx context.Context, userID string) ([]string, error) {
	// TODO: Implement provider retrieval from PostgreSQL
	return []string{}, errors.New("not implemented")
}

// LinkUserProvider links an OAuth provider to a user
func (s *Service) LinkUserProvider(ctx context.Context, userID, provider, providerUserID, email string) error {
	// TODO: Implement provider linking in PostgreSQL
	return errors.New("not implemented")
}

// UnlinkUserProvider unlinks an OAuth provider from a user
func (s *Service) UnlinkUserProvider(ctx context.Context, userID, provider string) error {
	// TODO: Implement provider unlinking in PostgreSQL
	return errors.New("not implemented")
}
