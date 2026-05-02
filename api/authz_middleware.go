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
		// state. The error is already logged here at construction; subsequent
		// requests just receive the 500.
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
		logger := slogging.Get().WithContext(c)
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

// checkAuthzRoles enforces an OR-list of role gates. Returns true on the first
// role that allows; returns false only after every role has been considered
// without a match.
//
// Slice 1 supports only `admin`. Other role kinds are recognized in the spec
// (security_reviewer, automation, confidential_reviewer) but their enforcement
// lands in slices 4-6 (#367-#369). Until then, encountering one in a rule
// logs at Warn level and skips it, so the rest of the OR list still gets a
// chance to satisfy the gate. The annotated rule is wrong by definition (a
// future-slice role landed without its enforcement code), but the request
// still gets a meaningful 401/403 instead of a 500.
func checkAuthzRoles(c *gin.Context, roles []AuthzRoleName) bool {
	skipped := 0
	for _, r := range roles {
		switch r {
		case RoleAuthzAdmin:
			if _, err := RequireAdministrator(c); err != nil {
				// RequireAdministrator already wrote the response.
				return false
			}
			return true
		default:
			skipped++
			slogging.Get().WithContext(c).Warn(
				"AuthzMiddleware: unsupported role gate %q skipped (slice 1 supports only 'admin'); other roles in the OR list will still be evaluated",
				r)
		}
	}
	if skipped == len(roles) {
		// Every role in the list was unsupported. Write a 403 so the abort
		// in the caller produces a clean response.
		HandleRequestError(c, &RequestError{
			Status:  http.StatusForbidden,
			Code:    "forbidden",
			Message: "Access denied",
		})
	}
	return false
}
