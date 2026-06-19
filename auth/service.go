package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/auth/repository"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// redisNilError is the error message returned by Redis when a key is not found
const redisNilError = "redis: nil"

// ClaimsEnricher enriches JWT claims with application-specific data (e.g., group membership)
// that cannot be directly accessed from the auth package without creating circular dependencies.
// SEM@18f87a010aa0bba84d6fa6221cfb289094caf982: interface for enriching JWT claims with TMI group membership and role flags
type ClaimsEnricher interface {
	// EnrichClaims checks built-in group membership and resolves TMI-managed group names for a user.
	// Returns whether the user is an administrator, security reviewer, and the user's TMI group names.
	EnrichClaims(ctx context.Context, userInternalUUID string, provider string, groupNames []string) (isAdmin bool, isSecurityReviewer bool, tmiGroupNames []string, err error)
}

// UserContentTokenRevoker is called before a user is deleted to sweep any
// per-user OAuth tokens at the provider side. Implementations must be
// best-effort: revocation failures must be logged but must never block user
// deletion. The interface lives in the auth package to avoid a circular
// import with the api package (which provides the concrete implementation).
// SEM@18f87a010aa0bba84d6fa6221cfb289094caf982: interface for revoking all provider-side OAuth tokens before a user is deleted
type UserContentTokenRevoker interface {
	// RevokeUserTokens attempts to revoke all content tokens belonging to
	// userID at their respective providers. It never returns an error.
	RevokeUserTokens(ctx context.Context, userID string)
}

// Service provides authentication and authorization functionality
// SEM@1eb7997add7b39214eac29d20050d7968745a98d: core auth service struct holding JWT, SAML, user repo, and caching dependencies
type Service struct {
	dbManager           *db.Manager
	config              Config
	keyManager          *JWTKeyManager
	samlManager         *SAMLManager
	stateStore          StateStore
	userRepo            repository.UserRepository
	credRepo            repository.ClientCredentialRepository
	deletionRepo        repository.DeletionRepository
	claimsEnricher      ClaimsEnricher
	registry            ProviderRegistry
	preUserDeleteHook   UserContentTokenRevoker
	linkedIdentityStore LinkedIdentityStore
}

// SetClaimsEnricher sets the claims enricher for JWT token generation
// SEM@a0040890dd7b1940f542d4211d4338cd0e713cbc: register the claims enricher used during JWT token generation (mutates shared state)
func (s *Service) SetClaimsEnricher(enricher ClaimsEnricher) {
	s.claimsEnricher = enricher
}

// SetProviderRegistry sets the provider registry for unified provider lookup.
// SEM@d526a06f3040d3424d4deb08071cd87ae770937f: register the provider registry for unified OAuth provider lookup (mutates shared state)
func (s *Service) SetProviderRegistry(registry ProviderRegistry) {
	s.registry = registry
}

// SetPreUserDeleteHook registers a hook that is called before each user deletion
// to perform best-effort content-token revocations at the provider side.
// The hook is called with the user's internal UUID before the DB row (and its
// FK-cascaded child rows) is removed, giving the implementation access to the
// token data. Pass nil to clear the hook.
// SEM@18f87a010aa0bba84d6fa6221cfb289094caf982: register a hook to revoke provider tokens before user deletion (mutates shared state)
func (s *Service) SetPreUserDeleteHook(h UserContentTokenRevoker) {
	s.preUserDeleteHook = h
}

// SetLinkedIdentityStore wires a LinkedIdentityStore into the service, enabling
// Tier 1b linked-identity resolution during OAuth login.
// SEM@1eb7997add7b39214eac29d20050d7968745a98d: wire a linked identity store enabling Tier 1b login resolution (mutates shared state)
func (s *Service) SetLinkedIdentityStore(store LinkedIdentityStore) {
	s.linkedIdentityStore = store
}

// NewService creates a new authentication service
// SEM@8af03cfea628820f921f3922831bbb27c7aa2b02: build and initialize the auth service with JWT key manager, SAML, and user repos (reads DB)
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
// SEM@41fea1c48a3526015f75a5e401ec4970c6c9dfcf: return the JWT key manager (pure)
func (s *Service) GetKeyManager() *JWTKeyManager {
	return s.keyManager
}

// GetSAMLManager returns the SAML manager (getter for unexported field)
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: return the SAML manager (pure)
func (s *Service) GetSAMLManager() *SAMLManager {
	return s.samlManager
}

// GormDB returns the underlying GORM database connection.
// Used by services that need to wrap operations in retryable transactions.
// SEM@18f87a010aa0bba84d6fa6221cfb289094caf982: return the underlying GORM database connection (pure)
func (s *Service) GormDB() *gorm.DB {
	return s.dbManager.Gorm().DB()
}

// BlacklistToken adds a JWT token to the blacklist so it can no longer be used.
// This should be called when a user is deleted or logs out to invalidate their tokens.
// SEM@0538436fe19e71299239f10214d737a09cf94961: add a JWT to the Redis token blacklist to invalidate it immediately (reads DB)
func (s *Service) BlacklistToken(ctx context.Context, tokenString string) error {
	if s.dbManager == nil || s.dbManager.Redis() == nil {
		slogging.Get().Warn("Token blacklisting skipped: Redis not available")
		return nil
	}

	blacklist := NewTokenBlacklist(s.dbManager.Redis().GetClient(), s.keyManager)
	return blacklist.BlacklistToken(ctx, tokenString)
}

// User represents a user in the system
// SEM@24dcbaf59ea6bfe4e66c3f1fbc4863c809cfdc0e: domain user struct with provider identity, roles, and OAuth token fields
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
	Automation         *bool      `json:"automation,omitempty"` // Whether this is an automation/service account (server-managed, nullable)
	CreatedAt          time.Time  `json:"created_at"`
	ModifiedAt         time.Time  `json:"modified_at"`
	LastLogin          *time.Time `json:"last_login,omitempty"` // nullable - may be NULL for auto-created admin users
}

// TokenPair contains an access token and a refresh token
// SEM@65af9b7db2850b6e18076df15ed522c8df4bb64c: access and refresh token response returned after successful authentication (pure)
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// Claims represents the JWT claims
// SEM@18f87a010aa0bba84d6fa6221cfb289094caf982: JWT claims struct carrying email, groups, role flags, delegation context, and auth_time (pure)
type Claims struct {
	Email              string             `json:"email"`
	EmailVerified      bool               `json:"email_verified,omitempty"`
	Name               string             `json:"name"`
	IdentityProvider   string             `json:"idp,omitempty"`                      // Identity provider
	Groups             []string           `json:"groups,omitempty"`                   // User's groups from IdP
	IsAdministrator    *bool              `json:"tmi_is_administrator,omitempty"`     // TMI Administrators group membership
	IsSecurityReviewer *bool              `json:"tmi_is_security_reviewer,omitempty"` // TMI Security Reviewers group membership
	Delegation         *DelegationContext `json:"delegation,omitempty"`               // T18: scoped delegation token for addon invocations
	// AuthTime is the timestamp (Unix seconds) of the user's last interactive
	// IdP authentication. OIDC-standard claim. #355 step-up middleware reads
	// this to decide whether a /admin/* write requires re-authentication.
	// Refresh-token rotation preserves this value (refresh proves possession
	// of the refresh token, not freshness of the human).
	AuthTime *jwt.NumericDate `json:"auth_time,omitempty"`
	jwt.RegisteredClaims
}

// DelegationContext is the addon-invocation scope embedded in a delegation
// JWT (T18, #358). Its presence on a token means: "this token impersonates
// the invoker for the duration of one specific addon invocation against
// one specific threat model — do not allow it to escape that scope".
//
// Routes that addons hit on the write-back path declare
// `x-tmi-authz: { subject_authority: "invoker" }` to require this token
// shape (and to reject service-account-only tokens). The token's `sub`
// is the invoker's provider_user_id; the rest of the user-identity
// claims (email, name, provider, groups, tmi_is_security_reviewer) are
// copied from the invoker so existing handler code reads the invoker's
// identity transparently.
// SEM@18f87a010aa0bba84d6fa6221cfb289094caf982: scoped addon-invocation context embedded in delegation JWTs to limit token authority (pure)
type DelegationContext struct {
	// AddonID is the addon being invoked.
	AddonID string `json:"addon_id"`
	// DeliveryID is the unique webhook delivery record this token was
	// minted for. Replays of the same delivery will mint distinct tokens
	// (one per attempt), all sharing this DeliveryID.
	DeliveryID string `json:"delivery_id"`
	// ThreatModelID is the parent threat model the invocation targets.
	// Writes to other threat models with this token are out of scope; a
	// future hardening pass can add per-resource allowlist enforcement
	// (the schema field `subject_authority: invoker` is the route-level
	// gate today; the resource-level scope check is residual scope).
	ThreatModelID string `json:"threat_model_id"`
}

// GenerateTokens generates a new JWT token pair for a user with auth_time = now.
// Use this for fresh interactive logins; use GenerateTokensWithAuthTime to
// preserve auth_time across refresh-token rotation.
// SEM@18f87a010aa0bba84d6fa6221cfb289094caf982: mint a JWT token pair for a fresh interactive login with auth_time = now (reads DB)
func (s *Service) GenerateTokens(ctx context.Context, user User) (TokenPair, error) {
	return s.GenerateTokensWithAuthTime(ctx, user, nil, time.Now())
}

// GenerateTokensWithUserInfo generates a new JWT token pair for a user with
// optional provider UserInfo and auth_time = now.
// SEM@18f87a010aa0bba84d6fa6221cfb289094caf982: mint a JWT token pair enriched with provider UserInfo, auth_time = now (reads DB)
func (s *Service) GenerateTokensWithUserInfo(ctx context.Context, user User, userInfo *UserInfo) (TokenPair, error) {
	return s.GenerateTokensWithAuthTime(ctx, user, userInfo, time.Now())
}

// GenerateTokensWithAuthTime is the canonical token-mint entry point. The
// authTime parameter is the timestamp of the user's last interactive IdP
// authentication. For fresh logins, pass time.Now(). For refresh-token
// rotation, pass the preserved auth_time from the previous JWT.
// SEM@18f87a010aa0bba84d6fa6221cfb289094caf982: mint a JWT access/refresh token pair preserving a caller-supplied auth_time (reads DB)
func (s *Service) GenerateTokensWithAuthTime(ctx context.Context, user User, userInfo *UserInfo, authTime time.Time) (TokenPair, error) {
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
		AuthTime:         jwt.NewNumericDate(authTime),
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

	// Enrich claims with TMI group membership data (admin, security reviewer, group names)
	if s.claimsEnricher != nil && user.InternalUUID != "" {
		isAdmin, isSecReviewer, tmiGroups, enrichErr := s.claimsEnricher.EnrichClaims(ctx, user.InternalUUID, user.Provider, user.Groups)
		if enrichErr != nil {
			slogging.Get().Warn("Failed to enrich JWT claims with group membership: %v", enrichErr)
			// Don't fail token generation if enrichment fails - just omit the claims
		} else {
			claims.IsAdministrator = &isAdmin
			claims.IsSecurityReviewer = &isSecReviewer
			// Merge TMI-managed group names into the groups claim alongside any IdP groups
			if len(tmiGroups) > 0 {
				claims.Groups = mergeGroups(claims.Groups, tmiGroups)
			}
		}
	}

	// Create the JWT token using the key manager
	tokenString, err := s.keyManager.CreateToken(claims)
	if err != nil {
		return TokenPair{}, fmt.Errorf("failed to create token: %w", err)
	}

	// Generate a refresh token
	refreshToken := uuid.New().String()
	refreshTokenDays := s.config.JWT.RefreshTokenDays
	if refreshTokenDays <= 0 {
		refreshTokenDays = 7
	}
	refreshDuration := time.Duration(refreshTokenDays) * 24 * time.Hour

	// Store the refresh token in Redis with session creation timestamp and auth time.
	// Value format: "userID|sessionCreatedAtUnix|authTimeUnix" to support absolute session
	// expiration and step-up authentication (auth_time must survive refresh-token rotation).
	sessionCreatedAt := time.Now().Unix()
	refreshValue := fmt.Sprintf("%s|%d|%d", user.InternalUUID, sessionCreatedAt, authTime.Unix())
	refreshKey := fmt.Sprintf("refresh_token:%s", refreshToken)
	err = s.dbManager.Redis().Set(ctx, refreshKey, refreshValue, refreshDuration)
	if err != nil {
		return TokenPair{}, fmt.Errorf("failed to store refresh token: %w", err)
	}

	// Return the token pair
	return TokenPair{
		AccessToken:  tokenString,
		RefreshToken: refreshToken,
		ExpiresIn:    s.config.JWT.ExpirationSeconds,
		TokenType:    "bearer",
	}, nil
}

// mergeGroups combines two group name slices, deduplicating entries.
// SEM@18f87a010aa0bba84d6fa6221cfb289094caf982: combine two group name slices, deduplicating entries (pure)
func mergeGroups(existing, additional []string) []string {
	merged := make([]string, len(existing))
	copy(merged, existing)
	for _, g := range additional {
		if !slices.Contains(merged, g) {
			merged = append(merged, g)
		}
	}
	return merged
}

// ValidateToken validates a JWT token
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: validate a JWT signature, issuer, and audience; return its claims (pure)
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

// RefreshToken refreshes an access token using a refresh token.
// Implements single-use rotation (old token deleted) and absolute session expiration.
// SEM@18f87a010aa0bba84d6fa6221cfb289094caf982: rotate a refresh token and issue a new token pair, enforcing absolute session expiration (reads DB)
func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (TokenPair, error) {
	logger := slogging.Get()

	// Get the refresh token value from Redis
	refreshKey := fmt.Sprintf("refresh_token:%s", refreshToken)
	storedValue, err := s.dbManager.Redis().Get(ctx, refreshKey)
	if err != nil {
		return TokenPair{}, fmt.Errorf("invalid refresh token: %w", err)
	}

	// Delete the old refresh token (single-use rotation)
	if err := s.dbManager.Redis().Del(ctx, refreshKey); err != nil {
		return TokenPair{}, fmt.Errorf("failed to delete refresh token: %w", err)
	}

	// Parse stored value: "userID|sessionCreatedAtUnix|authTimeUnix" (current),
	// "userID|sessionCreatedAtUnix" (legacy 2-field), or "userID" (oldest legacy).
	userID := storedValue
	var sessionCreatedAt int64
	var authTimeUnix int64
	parts := strings.Split(storedValue, "|")
	switch len(parts) {
	case 3:
		userID = parts[0]
		sessionCreatedAt, _ = strconv.ParseInt(parts[1], 10, 64)
		authTimeUnix, _ = strconv.ParseInt(parts[2], 10, 64)
	case 2:
		// Legacy 2-field token: no auth_time was preserved at mint time.
		// authTimeUnix stays 0 here; the rotation path below converts that
		// to time.Unix(0, 0), which step-up middleware treats as stale.
		userID = parts[0]
		sessionCreatedAt, _ = strconv.ParseInt(parts[1], 10, 64)
	}

	// Enforce absolute session expiration
	sessionLifetimeDays := s.config.JWT.SessionLifetimeDays
	if sessionLifetimeDays <= 0 {
		sessionLifetimeDays = 7
	}
	if sessionCreatedAt > 0 {
		sessionAge := time.Since(time.Unix(sessionCreatedAt, 0))
		maxLifetime := time.Duration(sessionLifetimeDays) * 24 * time.Hour
		if sessionAge > maxLifetime {
			logger.Info("Refresh token rejected: absolute session lifetime exceeded session_age=%v max_lifetime=%v", sessionAge, maxLifetime)
			return TokenPair{}, fmt.Errorf("session expired: absolute session lifetime of %d days exceeded, please re-authenticate", sessionLifetimeDays)
		}
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
		logger.Error("Failed to update user last login: %v", err)
	}

	// Generate new tokens, preserving original auth_time and session creation time.
	// Legacy 2-field (and oldest 1-field) tokens have authTimeUnix == 0:
	// no auth_time was preserved at mint time. Per the spec's "missing
	// auth_time = stale" policy, mint a sentinel auth_time of epoch 0 so
	// step-up middleware forces re-authentication on the next admin write.
	// Self-corrects on next interactive IdP login.
	authTime := time.Unix(0, 0)
	if authTimeUnix > 0 {
		authTime = time.Unix(authTimeUnix, 0)
	}
	tokenPair, err := s.GenerateTokensWithAuthTime(ctx, user, nil, authTime)
	if err != nil {
		return TokenPair{}, err
	}

	// Overwrite the new refresh token's session timestamp and auth_time with the
	// original values so absolute expiration and step-up invariants are preserved
	// across every rotation in the chain.
	if sessionCreatedAt > 0 {
		newRefreshKey := fmt.Sprintf("refresh_token:%s", tokenPair.RefreshToken)
		refreshTokenDays := s.config.JWT.RefreshTokenDays
		if refreshTokenDays <= 0 {
			refreshTokenDays = 7
		}
		refreshDuration := time.Duration(refreshTokenDays) * 24 * time.Hour
		refreshValue := fmt.Sprintf("%s|%d|%d", user.InternalUUID, sessionCreatedAt, authTime.Unix())
		if err := s.dbManager.Redis().Set(ctx, newRefreshKey, refreshValue, refreshDuration); err != nil {
			logger.Error("Failed to preserve session creation time on refresh: %v", err)
		}
	}

	return tokenPair, nil
}

// RevokeToken revokes a refresh token
// SEM@d885c7955d5a30affb8ddde84ee1cf757aab2a6b: delete a refresh token from Redis to revoke the session (reads DB)
func (s *Service) RevokeToken(ctx context.Context, refreshToken string) error {
	refreshKey := fmt.Sprintf("refresh_token:%s", refreshToken)
	return s.dbManager.Redis().Del(ctx, refreshKey)
}

// InvalidateUserSessions invalidates all sessions for a user
// SEM@cdd711a5407558ac89d03c4548a877007b74e7cd: delete all Redis session keys for a user, terminating all active sessions (reads DB)
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
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: fetch a user by email, checking Redis cache before the DB (reads DB)
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
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: fetch a user by internal UUID, checking Redis cache before the DB (reads DB)
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
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: store a new user and populate the Redis cache entry (reads DB)
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
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: persist user profile changes and invalidate the Redis cache (reads DB)
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
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: delete a user by internal UUID and invalidate all Redis cache keys (reads DB)
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
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: OAuth provider binding record linking a user to a provider identity (pure)
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
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: list OAuth provider bindings for a user (reads DB)
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
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: fetch the primary provider user ID stored on the user record (reads DB)
func (s *Service) GetPrimaryProviderID(ctx context.Context, userID string) (string, error) {
	return s.userRepo.GetPrimaryProviderID(ctx, userID)
}

// GetUserByProviderID gets a user by provider and provider user ID
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: fetch a user by provider and provider user ID, checking Redis cache first (reads DB)
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
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: fetch a user by provider and email as a fallback to provider user ID lookup (reads DB)
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

// GetLinkedIdentityByProviderSub looks up a linked identity by provider and provider-user-id.
// Returns ErrLinkedIdentityNotFound if no row matches or the store is not wired.
// SEM@1eb7997add7b39214eac29d20050d7968745a98d: look up a linked identity by provider and provider user ID (reads DB)
func (s *Service) GetLinkedIdentityByProviderSub(ctx context.Context, provider, providerUserID string) (models.LinkedIdentity, error) {
	if s.linkedIdentityStore == nil {
		return models.LinkedIdentity{}, ErrLinkedIdentityNotFound
	}
	return s.linkedIdentityStore.GetByProviderSub(ctx, provider, providerUserID)
}

// GetUserByInternalUUID gets a user by their internal UUID.
// SEM@1eb7997add7b39214eac29d20050d7968745a98d: fetch a user by internal UUID; delegates to GetUserByID (reads DB)
func (s *Service) GetUserByInternalUUID(ctx context.Context, uuid string) (User, error) {
	return s.GetUserByID(ctx, uuid)
}

// TouchLinkedIdentityLastUsed updates last_used_at for the linked identity with the given id.
// SEM@1eb7997add7b39214eac29d20050d7968745a98d: update the last_used_at timestamp on a linked identity record (reads DB)
func (s *Service) TouchLinkedIdentityLastUsed(ctx context.Context, id string) error {
	if s.linkedIdentityStore == nil {
		return nil
	}
	return s.linkedIdentityStore.TouchLastUsed(ctx, id)
}

// GetUserByAnyProviderID gets a user by provider ID across all providers
// This allows provider-independent authorization using IdP user IDs
// NOTE: This can return ambiguous results if the same provider_user_id exists for multiple providers
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: fetch a user by provider user ID across all providers (reads DB)
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
// SEM@89d554e793900a75b5703e1d10c9d58f57ceadc6: store a user in Redis under ID, email, and provider lookup keys (reads DB)
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
// SEM@9745b416c50726fc3ca5d4637364ba55d6ba0699: fetch a cached user from Redis by internal UUID (reads DB)
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
// SEM@9745b416c50726fc3ca5d4637364ba55d6ba0699: fetch a cached user from Redis by email address (reads DB)
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
// SEM@9745b416c50726fc3ca5d4637364ba55d6ba0699: fetch a cached user from Redis by provider and provider user ID (reads DB)
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
// SEM@89d554e793900a75b5703e1d10c9d58f57ceadc6: delete all Redis cache keys for a user (ID, email, provider) (reads DB)
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
// SEM@83248bf8b4162186950592395d4c056d02394d4c: compute the JWT issuer URL from the configured OAuth callback URL (pure)
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
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: store user group membership in Redis for the JWT session duration (reads DB)
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
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: retrieve cached group membership and IdP name for a user from Redis (reads DB)
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
// SEM@85ed60a219cd0aba38e90907408068f8235d4cc1: delete cached group membership for a user from Redis on logout (reads DB)
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
// SEM@18f87a010aa0bba84d6fa6221cfb289094caf982: validate client credentials and mint a service-account JWT without refresh token (reads DB)
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
		IdentityProvider: owner.Provider,
		Groups:           owner.Groups,
		// CC grants set auth_time = now: each token mint counts as fresh
		// authentication. This is harmless for admin step-up because
		// service-account tokens are categorically denied on /admin/* by
		// RequireAdministrator before step-up runs (#399 investigation);
		// step-up only gates /admin/* routes.
		AuthTime: jwt.NewNumericDate(time.Now()),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			Issuer:    s.getIssuer(),
			Audience:  jwt.ClaimStrings{s.getIssuer()},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(s.config.JWT.ExpirationSeconds) * time.Second)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
		},
	}

	// Enrich claims with TMI group membership data (security reviewer, group names).
	// Administrative privileges are intentionally excluded from client credential tokens
	// to enforce that admin operations require interactive (PKCE) authentication.
	if s.claimsEnricher != nil && owner.InternalUUID != "" {
		_, isSecReviewer, tmiGroups, enrichErr := s.claimsEnricher.EnrichClaims(ctx, owner.InternalUUID, owner.Provider, owner.Groups)
		if enrichErr != nil {
			logger.Warn("Failed to enrich service account claims with group membership: %v", enrichErr)
		} else {
			notAdmin := false
			claims.IsAdministrator = &notAdmin
			claims.IsSecurityReviewer = &isSecReviewer
			if len(tmiGroups) > 0 {
				// Filter out the administrators group from the merged groups
				filtered := make([]string, 0, len(tmiGroups))
				for _, g := range tmiGroups {
					if g != "administrators" {
						filtered = append(filtered, g)
					}
				}
				claims.Groups = mergeGroups(claims.Groups, filtered)
			}
		}
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
		TokenType:   "bearer",
		ExpiresIn:   s.config.JWT.ExpirationSeconds,
		// Note: RefreshToken intentionally omitted for Client Credentials Grant
	}, nil
}

// getIssuer returns the JWT issuer URL
// SEM@99c8cc4c042f4729b89e24981a18dba21b40be17: return the JWT issuer URL derived from the OAuth callback URL (pure)
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
// SEM@24dcbaf59ea6bfe4e66c3f1fbc4863c809cfdc0e: convert a repository User to the service-layer User domain type (pure)
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
		Automation:     repoUser.Automation,
	}
}

// convertServiceUserToRepoUser converts a service User to a repository User
// SEM@24dcbaf59ea6bfe4e66c3f1fbc4863c809cfdc0e: convert a service-layer User to the repository User type (pure)
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
		Automation:     user.Automation,
	}
}
