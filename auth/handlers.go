package auth

import (
	"context"

	"github.com/ericfitz/tmi/internal/wwwauth"
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
// SEM@d9b7276f43106db4f65f8333db6fdedae15b56bd: typed string for safe context value keying to prevent collisions (pure)
type contextKey string

const (
	// userHintContextKey is the key for login_hint in context
	userHintContextKey contextKey = "login_hint"
	// UserContextKey is the key for the user in the Gin context
	UserContextKey contextKey = "user"
)

// tmiProviderID is the identifier for the built-in TMI OAuth provider
const tmiProviderID = wwwauth.Realm

// setWWWAuthenticateHeader sets a RFC 6750 compliant WWW-Authenticate header.
// The header value is built by the shared internal/wwwauth package so the RFC
// 6750 format lives in one place (the auth package must not import api, hence
// the shared internal package).
// SEM@212287c6c02d99be7f8071b21a50666223646bec: set RFC 6750 Bearer WWW-Authenticate response header with error details
func setWWWAuthenticateHeader(c *gin.Context, errType, description string) {
	c.Header("WWW-Authenticate", wwwauth.BuildHeader(errType, description))
}

// AdminChecker is an interface for checking if a user is an administrator or security reviewer
// SEM@a0040890dd7b1940f542d4211d4338cd0e713cbc: contract for checking admin and security-reviewer roles for a user (pure)
type AdminChecker interface {
	IsAdmin(ctx context.Context, userInternalUUID *string, provider string, groupUUIDs []string) (bool, error)
	IsSecurityReviewer(ctx context.Context, userInternalUUID *string, provider string, groupUUIDs []string) (bool, error)
	GetGroupUUIDsByNames(ctx context.Context, provider string, groupNames []string) ([]string, error)
}

// UserGroupInfo represents a TMI-managed group that a user belongs to
// SEM@a0040890dd7b1940f542d4211d4338cd0e713cbc: group membership record linking a user to a TMI-managed group (pure)
type UserGroupInfo struct {
	InternalUUID string `json:"internal_uuid"`
	GroupName    string `json:"group_name"`
	Name         string `json:"name,omitempty"`
}

// UserGroupsFetcher retrieves TMI-managed group memberships for a user
// SEM@a0040890dd7b1940f542d4211d4338cd0e713cbc: contract for fetching TMI-managed group memberships for a user (pure)
type UserGroupsFetcher interface {
	GetUserGroups(ctx context.Context, userInternalUUID string) ([]UserGroupInfo, error)
}

// Handlers provides HTTP handlers for authentication
// SEM@d89a562535e2240eeb7f556a3f619d28fe9c5613: aggregate of auth HTTP handler dependencies including service, config, and auditors (pure)
type Handlers struct {
	service             *Service
	config              Config
	adminChecker        AdminChecker
	userGroupsFetcher   UserGroupsFetcher
	cookieOpts          CookieOptions
	registry            ProviderRegistry
	tokenLockoutImpl    *OAuthTokenLockout
	stepUpAuditor       *StepUpAuditor       // #397
	runtimeCfg          RuntimeConfigReader  // #419 — DB-backed operational config
	identityLinkStore   LinkedIdentityStore  // #383 — linked identity store for link handlers
	identityLinkAuditor *IdentityLinkAuditor // #383
}

// tokenLockout returns the per-client_id /oauth2/token brute-force lockout.
// Lazily constructed on first use so the Handlers literal in tests can
// remain minimal. Returns a no-op lockout when Redis is unavailable.
// SEM@a3245d875ac2cfb50e40e8e8ffcceb6c913a13f0: fetch or lazily build the per-client_id OAuth token brute-force lockout (mutates shared state)
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
// SEM@a3245d875ac2cfb50e40e8e8ffcceb6c913a13f0: override the OAuth token lockout implementation, used in tests (mutates shared state)
func (h *Handlers) SetTokenLockout(l *OAuthTokenLockout) {
	h.tokenLockoutImpl = l
}

// SetStepUpAuditor wires the step-up audit writer. Safe to call multiple
// times; nil disables step-up auditing (used in tests AND in production
// when admin-audit middleware is disabled — see cmd/server/main.go). #397.
// SEM@dd66d35bda6952fa6d623976b1adb6177685fe6d: register the step-up audit writer on the handler (mutates shared state)
func (h *Handlers) SetStepUpAuditor(a *StepUpAuditor) {
	h.stepUpAuditor = a
}

// stepUpAud returns the wired auditor or a no-op auditor if none is set.
// SEM@dd66d35bda6952fa6d623976b1adb6177685fe6d: return the registered step-up auditor or a no-op fallback (pure)
func (h *Handlers) stepUpAud() *StepUpAuditor {
	if h.stepUpAuditor != nil {
		return h.stepUpAuditor
	}
	return NewStepUpAuditor(nil)
}

// SetIdentityLinkStore wires the linked identity store. Safe to call multiple
// times; nil disables server-side identity lookups during link operations. #383.
// SEM@d89a562535e2240eeb7f556a3f619d28fe9c5613: register the linked identity store on the handler (mutates shared state)
func (h *Handlers) SetIdentityLinkStore(store LinkedIdentityStore) {
	h.identityLinkStore = store
}

// SetIdentityLinkAuditor wires the identity-link audit writer. Safe to call
// multiple times; nil disables identity-link auditing. #383.
// SEM@d89a562535e2240eeb7f556a3f619d28fe9c5613: register the identity-link audit writer on the handler (mutates shared state)
func (h *Handlers) SetIdentityLinkAuditor(a *IdentityLinkAuditor) {
	h.identityLinkAuditor = a
}

// identityLinkAud returns the wired auditor or a no-op auditor if none is set.
// SEM@d89a562535e2240eeb7f556a3f619d28fe9c5613: return the registered identity-link auditor or a no-op fallback (pure)
func (h *Handlers) identityLinkAud() *IdentityLinkAuditor {
	if h.identityLinkAuditor != nil {
		return h.identityLinkAuditor
	}
	return NewIdentityLinkAuditor(nil)
}

// NewHandlers creates new authentication handlers
// SEM@d885c7955d5a30affb8ddde84ee1cf757aab2a6b: build an auth Handlers instance bound to a service and config (pure)
func NewHandlers(service *Service, config Config) *Handlers {
	return &Handlers{
		service: service,
		config:  config,
	}
}

// SetAdminChecker sets the admin checker for the handlers
// SEM@ccfd74278ac51e8904765cbf4218077a55750258: register the admin-role checker on the handler (mutates shared state)
func (h *Handlers) SetAdminChecker(checker AdminChecker) {
	h.adminChecker = checker
}

// SetUserGroupsFetcher sets the user groups fetcher for the handlers
// SEM@a0040890dd7b1940f542d4211d4338cd0e713cbc: register the user-groups fetcher on the handler (mutates shared state)
func (h *Handlers) SetUserGroupsFetcher(fetcher UserGroupsFetcher) {
	h.userGroupsFetcher = fetcher
}

// SetCookieOptions sets the cookie configuration for session cookie management
// SEM@314b7ae8fe586a75ecee2e8fa7103d3193f15f7c: configure session cookie options on the handler (mutates shared state)
func (h *Handlers) SetCookieOptions(opts CookieOptions) {
	h.cookieOpts = opts
}

// SetProviderRegistry sets the provider registry for unified provider lookup.
// SEM@d526a06f3040d3424d4deb08071cd87ae770937f: register the OAuth provider registry on the handler (mutates shared state)
func (h *Handlers) SetProviderRegistry(registry ProviderRegistry) {
	h.registry = registry
}

// SetRuntimeConfigReader wires the DB-backed operational config reader used
// by handlers at request time. Safe to call multiple times. A nil reader
// makes handlers fall back to the YAML snapshot in h.config — see
// RuntimeConfigReader for the contract. (#419)
// SEM@08e19a77d4d2c499f116e1a1ee3c875c06407335: register the DB-backed runtime config reader on the handler (mutates shared state)
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
// SEM@08e19a77d4d2c499f116e1a1ee3c875c06407335: fetch the OAuth client callback allowlist from DB config, failing closed on error (reads DB)
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
// SEM@08e19a77d4d2c499f116e1a1ee3c875c06407335: report whether SAML authentication is enabled, preferring DB config over YAML (reads DB)
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
// SEM@08e19a77d4d2c499f116e1a1ee3c875c06407335: fetch the OAuth callback URL from DB config with YAML fallback (reads DB)
func (h *Handlers) oauthCallbackURL(ctx context.Context) string {
	if h.runtimeCfg != nil {
		if v := h.runtimeCfg.GetOAuthCallbackURL(ctx); v != "" {
			return v
		}
	}
	return h.config.OAuth.CallbackURL
}

// Service returns the auth service (getter for unexported field)
// SEM@41fea1c48a3526015f75a5e401ec4970c6c9dfcf: return the auth service from the handler (pure)
func (h *Handlers) Service() *Service {
	return h.service
}

// Config returns the auth config (getter for unexported field)
// SEM@0eb4bf778ed84abb8fa3d433bf42cc7928258257: return the auth config from the handler (pure)
func (h *Handlers) Config() Config {
	return h.config
}

// Note: Route registration has been removed. All routes are now registered via OpenAPI
// specification in api/api.go. The auth handlers are called through the Server's
// AuthService adapter.
