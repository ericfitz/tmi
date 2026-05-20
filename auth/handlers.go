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

// OAuth response_type values (RFC 6749 §3.1.1).
const (
	oauthResponseTypeCode = "code"
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
	stepUpAuditor     *StepUpAuditor      // #397
	runtimeCfg        RuntimeConfigReader // #419 — DB-backed operational config
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

// SetStepUpAuditor wires the step-up audit writer. Safe to call multiple
// times; nil disables step-up auditing (used in tests AND in production
// when admin-audit middleware is disabled — see cmd/server/main.go). #397.
func (h *Handlers) SetStepUpAuditor(a *StepUpAuditor) {
	h.stepUpAuditor = a
}

// stepUpAud returns the wired auditor or a no-op auditor if none is set.
func (h *Handlers) stepUpAud() *StepUpAuditor {
	if h.stepUpAuditor != nil {
		return h.stepUpAuditor
	}
	return NewStepUpAuditor(nil)
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

// SetRuntimeConfigReader wires the DB-backed operational config reader used
// by handlers at request time. Safe to call multiple times. A nil reader
// makes handlers fall back to the YAML snapshot in h.config — see
// RuntimeConfigReader for the contract. (#419)
func (h *Handlers) SetRuntimeConfigReader(r RuntimeConfigReader) {
	h.runtimeCfg = r
}

// clientCallbackAllowList returns the allowlist read from the runtime
// config (DB-backed) when available, otherwise the YAML snapshot.
//
// Fail-closed semantics: a DB row that exists but is unusable (corrupt
// JSON, read error, decryption failure) returns an empty list, NOT the
// YAML fallback. Silently reverting to the YAML snapshot would defeat
// the open-redirect mitigation when an operator's allowlist has been
// corrupted in storage.
func (h *Handlers) clientCallbackAllowList(ctx context.Context) []string {
	if h.runtimeCfg != nil {
		list, exists, err := h.runtimeCfg.GetClientCallbackAllowList(ctx)
		switch {
		case err != nil:
			// DB row exists but unusable — fail-closed.
			return nil
		case exists:
			return list
		}
		// exists == false → no DB row; fall through to YAML.
	}
	return h.config.OAuth.ClientCallbackAllowList
}

// samlEnabled reports whether SAML auth is enabled, preferring the runtime
// config (DB-backed) over the YAML snapshot.
func (h *Handlers) samlEnabled(ctx context.Context) bool {
	if h.runtimeCfg != nil {
		return h.runtimeCfg.IsSAMLEnabled(ctx)
	}
	return h.config.SAML.Enabled
}

// oauthCallbackURL returns the OAuth callback URL, preferring the runtime
// config (DB-backed) over the YAML snapshot. An empty value from the
// runtime reader falls through to the YAML snapshot so the YAML can still
// supply the value in development before the DB row exists.
func (h *Handlers) oauthCallbackURL(ctx context.Context) string {
	if h.runtimeCfg != nil {
		if v := h.runtimeCfg.GetOAuthCallbackURL(ctx); v != "" {
			return v
		}
	}
	return h.config.OAuth.CallbackURL
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
