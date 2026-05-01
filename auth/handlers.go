package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

// PKCE code challenge method constants
const (
	pkceMethodS256 = "S256"
)

// URL scheme constants
const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

// Context key type for context values
type contextKey string

const (
	// userHintContextKey is the key for login_hint in context
	userHintContextKey contextKey = "login_hint"
	// UserContextKey is the key for the user in the Gin context
	UserContextKey contextKey = "user"
)

// wwwAuthenticateRealm identifies the protection space for Bearer token authentication.
const wwwAuthenticateRealm = "tmi"

// tmiProviderID is the identifier for the built-in TMI OAuth provider
const tmiProviderID = wwwAuthenticateRealm

// setWWWAuthenticateHeader sets a RFC 6750 compliant WWW-Authenticate header.
// This is a package-local helper to avoid circular dependencies with the api package.
func setWWWAuthenticateHeader(c *gin.Context, errType, description string) {
	header := fmt.Sprintf(`Bearer realm="%s"`, wwwAuthenticateRealm)
	if errType != "" {
		header += fmt.Sprintf(`, error="%s"`, errType)
		if description != "" {
			escapedDesc := strings.ReplaceAll(description, `"`, `\"`)
			header += fmt.Sprintf(`, error_description="%s"`, escapedDesc)
		}
	}
	c.Header("WWW-Authenticate", header)
}

// AdminChecker is an interface for checking if a user is an administrator or security reviewer
type AdminChecker interface {
	IsAdmin(ctx context.Context, userInternalUUID *string, provider string, groupUUIDs []string) (bool, error)
	IsSecurityReviewer(ctx context.Context, userInternalUUID *string, provider string, groupUUIDs []string) (bool, error)
	GetGroupUUIDsByNames(ctx context.Context, provider string, groupNames []string) ([]string, error)
}

// UserGroupInfo represents a TMI-managed group that a user belongs to
type UserGroupInfo struct {
	InternalUUID string `json:"internal_uuid"`
	GroupName    string `json:"group_name"`
	Name         string `json:"name,omitempty"`
}

// UserGroupsFetcher retrieves TMI-managed group memberships for a user
type UserGroupsFetcher interface {
	GetUserGroups(ctx context.Context, userInternalUUID string) ([]UserGroupInfo, error)
}

// Handlers provides HTTP handlers for authentication
type Handlers struct {
	service           *Service
	config            Config
	adminChecker      AdminChecker
	userGroupsFetcher UserGroupsFetcher
	cookieOpts        CookieOptions
	registry          ProviderRegistry
	tokenLockoutImpl  *OAuthTokenLockout
}

// tokenLockout returns the per-client_id /oauth2/token brute-force lockout.
// Lazily constructed on first use so the Handlers literal in tests can
// remain minimal. Returns a no-op lockout when Redis is unavailable.
func (h *Handlers) tokenLockout() *OAuthTokenLockout {
	if h.tokenLockoutImpl != nil {
		return h.tokenLockoutImpl
	}
	if h.service == nil || h.service.dbManager == nil || h.service.dbManager.Redis() == nil {
		h.tokenLockoutImpl = NewOAuthTokenLockout(nil)
		return h.tokenLockoutImpl
	}
	h.tokenLockoutImpl = NewOAuthTokenLockout(h.service.dbManager.Redis().GetClient())
	return h.tokenLockoutImpl
}

// SetTokenLockout overrides the per-client_id lockout. Used in tests.
func (h *Handlers) SetTokenLockout(l *OAuthTokenLockout) {
	h.tokenLockoutImpl = l
}

// NewHandlers creates new authentication handlers
func NewHandlers(service *Service, config Config) *Handlers {
	return &Handlers{
		service: service,
		config:  config,
	}
}

// SetAdminChecker sets the admin checker for the handlers
func (h *Handlers) SetAdminChecker(checker AdminChecker) {
	h.adminChecker = checker
}

// SetUserGroupsFetcher sets the user groups fetcher for the handlers
func (h *Handlers) SetUserGroupsFetcher(fetcher UserGroupsFetcher) {
	h.userGroupsFetcher = fetcher
}

// SetCookieOptions sets the cookie configuration for session cookie management
func (h *Handlers) SetCookieOptions(opts CookieOptions) {
	h.cookieOpts = opts
}

// SetProviderRegistry sets the provider registry for unified provider lookup.
func (h *Handlers) SetProviderRegistry(registry ProviderRegistry) {
	h.registry = registry
}

// Service returns the auth service (getter for unexported field)
func (h *Handlers) Service() *Service {
	return h.service
}

// Config returns the auth config (getter for unexported field)
func (h *Handlers) Config() Config {
	return h.config
}

// Note: Route registration has been removed. All routes are now registered via OpenAPI
// specification in api/api.go. The auth handlers are called through the Server's
// AuthService adapter.
