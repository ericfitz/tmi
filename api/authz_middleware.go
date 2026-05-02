package api

import (
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// AuthzMiddleware is the unified declarative authorization gate. It looks up
// the AuthzRule for the matched route in the global AuthzTable and:
//
//   - For routes with no rule (legacy paths in slice 1): pass through. Existing
//     resource middleware (ThreatModelMiddleware, DiagramMiddleware, etc.) takes
//     over. This is the no-regression guarantee for paths the slice has not
//     yet annotated.
//
//   - For routes with rule.Public=true: pass through regardless of identity.
//     JWT middleware separately recognizes the path as public.
//
//   - For routes with rule.Roles containing "admin": delegate to
//     RequireAdministrator (api/auth_helpers.go), which returns 401/403 with
//     consistent error format.
//
//   - Ownership values reader/writer/owner are not enforced in slice 1 (they
//     are out of scope until #365 lands). If the spec ever reaches a route
//     with ownership!=none in this slice, the middleware logs and falls
//     through — the resource middleware will catch it.
//
// On any role-gate failure, the middleware aborts with the appropriate status
// (RequireAdministrator already writes the response). On allow, it sets the
// context key "authzCovered" = true so downstream middleware can skip
// duplicate checks (used in slice 2+).
func AuthzMiddleware() gin.HandlerFunc {
	tbl, err := LoadGlobalAuthzTable()
	if err != nil {
		// Failing to load the spec at startup is fatal — return a middleware
		// that 500s on every request rather than starting in an inconsistent
		// state. main.go logs the error during the first request.
		slogging.Get().Error("AuthzMiddleware: failed to load AuthzTable: %v", err)
		return func(c *gin.Context) {
			c.AbortWithStatusJSON(http.StatusInternalServerError, Error{
				Error:            "server_error",
				ErrorDescription: "Authorization table not initialized",
			})
		}
	}
	return authzMiddlewareWithTable(tbl)
}

func authzMiddlewareWithTable(tbl *AuthzTable) gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := slogging.GetContextLogger(c)
		rule, ok := tbl.Lookup(c.Request.Method, c.Request.URL.Path)
		if !ok {
			logger.Debug("AuthzMiddleware: no x-tmi-authz rule for %s %s; falling through to legacy middleware",
				c.Request.Method, c.Request.URL.Path)
			c.Next()
			return
		}

		if rule.Public {
			c.Set("authzCovered", true)
			c.Next()
			return
		}

		// Roles gate (OR-list).
		if len(rule.Roles) > 0 {
			if !checkAuthzRoles(c, rule.Roles) {
				// checkAuthzRoles writes the 401/403 response.
				c.Abort()
				return
			}
		}

		// Ownership enforcement is added in slice 2 (#365). For slice 1,
		// every annotated route has ownership=none, so we record that and
		// move on. If a future commit annotates a route with ownership!=none
		// before slice 2 lands, log and continue — the existing resource
		// middleware will still enforce it.
		if rule.Ownership != OwnershipNone {
			logger.Debug("AuthzMiddleware: ownership=%s on %s %s deferred to resource middleware (slice 2)",
				rule.Ownership, c.Request.Method, c.Request.URL.Path)
		}

		c.Set("authzCovered", true)
		c.Next()
	}
}

// checkAuthzRoles enforces an OR-list of role gates. Returns true on allow,
// false on deny (after writing the response). Slice 1 supports only "admin";
// other role kinds short-circuit to deny with a 500 (would indicate a slice
// 4/5/6 annotation landed without the matching enforcement).
func checkAuthzRoles(c *gin.Context, roles []AuthzRoleName) bool {
	for _, r := range roles {
		switch r {
		case RoleAuthzAdmin:
			if _, err := RequireAdministrator(c); err != nil {
				return false
			}
			return true
		default:
			slogging.Get().WithContext(c).Error(
				"AuthzMiddleware: unsupported role gate %q (slice 1 supports only 'admin')", r)
			c.AbortWithStatusJSON(http.StatusInternalServerError, Error{
				Error:            "server_error",
				ErrorDescription: "Unsupported authz role gate",
			})
			return false
		}
	}
	// Empty roles list with ownership=none and public=false: authenticated
	// users only. JWT middleware has already enforced authentication.
	return true
}
