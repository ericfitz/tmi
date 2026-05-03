package api

import (
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// AuthzMiddleware is the unified declarative authorization gate. It looks up
// the AuthzRule for the matched route in the global AuthzTable and enforces
// public/role/ownership gates declared in the OpenAPI x-tmi-authz extension.
//
//   - For routes with no rule (legacy paths not yet annotated): pass through.
//     Existing resource middleware (ThreatModelMiddleware, DiagramMiddleware,
//     etc.) takes over. This is the no-regression guarantee for paths the
//     migration has not yet covered.
//
//   - For routes with rule.Public=true: pass through regardless of identity.
//     JWT middleware separately recognizes the path as public.
//
//   - For routes with rule.Roles containing "admin": delegate to
//     RequireAdministrator (api/auth_helpers.go), which returns 401/403 with
//     consistent error format.
//
//   - For routes with rule.Ownership in {reader, writer, owner}: extract the
//     parent threat-model ID from the path, load lightweight ACL data, and
//     enforce the role. This replaces the per-method switch in
//     ThreatModelMiddleware for annotated routes (#365).
//
// On any gate failure, the middleware aborts with the appropriate status code
// and response body. On allow, it sets the context key "authzCovered" = true
// so downstream resource middleware can short-circuit duplicate work.
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

		// Ownership gate. Reader/writer/owner paths in slice 2 (#365) are all
		// nested under /threat_models/{threat_model_id}/...; ID extraction
		// targets that family. If a future slice annotates a route with a
		// non-threat-model parent, extractParentThreatModelID will return ""
		// and we fall through to legacy middleware so we don't miscount auth.
		if rule.Ownership != OwnershipNone {
			if !enforceOwnership(c, rule.Ownership) {
				c.Abort()
				return
			}
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

// enforceOwnership performs the resource-hierarchical role check for an
// annotated route whose parent is a threat model. On success it sets the
// "userRole" context key (preserving handler expectations from the legacy
// ThreatModelMiddleware) and records access for embedding idle cleanup.
//
// Returns true on allow, false after writing a 401/403/404/503 response.
func enforceOwnership(c *gin.Context, ownership Ownership) bool {
	logger := slogging.Get().WithContext(c)

	userEmailVal, exists := c.Get("userEmail")
	if !exists {
		logger.Warn("AuthzMiddleware: userEmail not in context for %s %s",
			c.Request.Method, c.Request.URL.Path)
		SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "No authentication token provided")
		c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "Authentication required",
		})
		return false
	}
	userEmail, ok := userEmailVal.(string)
	if !ok || userEmail == "" {
		logger.Warn("AuthzMiddleware: invalid userEmail in context for %s %s",
			c.Request.Method, c.Request.URL.Path)
		SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "Invalid authentication token")
		c.AbortWithStatusJSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "Invalid authentication",
		})
		return false
	}

	tmID := extractParentThreatModelID(c.Request.URL.Path)
	if tmID == "" {
		// The slice annotated a route whose parent isn't a threat model, but
		// no ID extractor is wired up. Fall through so the legacy resource
		// middleware can still enforce the rule rather than fail open.
		logger.Warn("AuthzMiddleware: ownership=%s on %s but no threat_model_id in path; falling through to legacy middleware",
			ownership, c.Request.URL.Path)
		return true
	}

	if ThreatModelStore == nil {
		logger.Error("AuthzMiddleware: ThreatModelStore not initialized")
		c.Header("Retry-After", "30")
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, Error{
			Error:            "service_unavailable",
			ErrorDescription: "Storage service temporarily unavailable - please retry",
		})
		return false
	}

	userProviderID, userInternalUUID, userIdP, userGroups := GetUserAuthFieldsForAccessCheck(c)
	user := ResolvedUser{
		InternalUUID: userInternalUUID,
		Provider:     userIdP,
		ProviderID:   userProviderID,
		Email:        userEmail,
	}

	isRestoreRoute := c.Request.Method == http.MethodPost &&
		strings.HasSuffix(strings.TrimRight(c.Request.URL.Path, "/"), "/restore")

	authorization, owner, err := loadMiddlewareAuthData(c.Request.Context(), tmID, isRestoreRoute)
	if err != nil {
		logger.Debug("AuthzMiddleware: threat model not found %s: %v", tmID, err)
		c.AbortWithStatusJSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "Threat model not found",
		})
		return false
	}

	authData := AuthorizationData{
		Type:          AuthTypeTMI10,
		Owner:         owner,
		Authorization: authorization,
	}

	requiredRole := ownershipToRole(ownership)
	if !AccessCheckWithGroups(user, userGroups, requiredRole, authData) {
		userRole := getUserRoleFromAuthData(user, userGroups, authData)
		logger.Warn("AuthzMiddleware: access denied for user %s (role=%q, required=%q) on %s %s",
			userEmail, userRole, requiredRole, c.Request.Method, c.Request.URL.Path)
		c.AbortWithStatusJSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "You don't have sufficient permissions to perform this action",
		})
		return false
	}

	userRole := getUserRoleFromAuthData(user, userGroups, authData)
	c.Set("userRole", userRole)

	if GlobalAccessTracker != nil {
		GlobalAccessTracker.RecordAccess(tmID)
	}

	logger.Debug("AuthzMiddleware: access granted for user %s (role=%q) on %s %s",
		userEmail, userRole, c.Request.Method, c.Request.URL.Path)
	return true
}

// ownershipToRole maps an x-tmi-authz ownership level to the legacy Role
// constants used by AccessCheckWithGroups.
func ownershipToRole(o Ownership) Role {
	switch o {
	case OwnershipReader:
		return RoleReader
	case OwnershipWriter:
		return RoleWriter
	case OwnershipOwner:
		return RoleOwner
	}
	return ""
}

// extractParentThreatModelID returns the threat model ID for any path under
// /threat_models/{threat_model_id}/..., or "" otherwise. The collection
// endpoint /threat_models is annotated with ownership=none and never reaches
// this code path.
func extractParentThreatModelID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[0] != "threat_models" {
		return ""
	}
	return parts[1]
}
