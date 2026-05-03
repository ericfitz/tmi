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

		// subject_authority gate (T18, #358). Enforced before role/ownership
		// because rejecting an SA token early is cheaper than loading the
		// parent ACL and matches the threat-model intent: addon write-backs
		// MUST use the delegation token, not the addon's own SA.
		if !enforceSubjectAuthority(c, rule.SubjectAuthority) {
			c.Abort()
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

// enforceSubjectAuthority checks the request's authentication kind against
// the route's `subject_authority` declaration:
//
//   - "" / "any":              no extra check.
//   - "invoker":               reject service-account-only tokens. Allowed
//     callers are interactive users and addon-invocation delegation tokens
//     (auth/delegation_token.go). This is the T18 gate (#358) for addon
//     write-back paths.
//   - "service_account":       require an SA token (sub: sa:*). Rare; for
//     SA-internal endpoints if any are introduced.
//
// Returns false after writing a 403 on rejection. Returns true on allow.
func enforceSubjectAuthority(c *gin.Context, sa SubjectAuthority) bool {
	if sa == SubjectAuthorityAny {
		return true
	}

	isSA, _ := c.Get("isServiceAccount")
	isServiceAccount, _ := isSA.(bool)
	isDelVal, hasDel := c.Get("isDelegation")
	isDelegation := hasDel && isDelVal == true

	switch sa {
	case SubjectAuthorityInvoker:
		// SA tokens are always rejected on invoker-required routes — the
		// addon must use its scoped delegation token instead. Delegation
		// tokens look like user tokens (sub is the invoker's
		// provider_user_id, isServiceAccount=false), so they pass.
		if isServiceAccount {
			slogging.Get().WithContext(c).Warn(
				"AuthzMiddleware: rejecting service-account token on invoker-only route %s %s (T18)",
				c.Request.Method, c.Request.URL.Path,
			)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusForbidden,
				Code:    "forbidden",
				Message: "service account credentials cannot be used here; addon write-backs must use the delegation token from X-TMI-Delegation-Token",
			})
			return false
		}
		_ = isDelegation // currently informational; future hardening can scope-check
		return true
	case SubjectAuthorityServiceAccount:
		if !isServiceAccount {
			slogging.Get().WithContext(c).Warn(
				"AuthzMiddleware: rejecting non-SA token on service-account-only route %s %s",
				c.Request.Method, c.Request.URL.Path,
			)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusForbidden,
				Code:    "forbidden",
				Message: "this endpoint requires service-account credentials",
			})
			return false
		}
		return true
	}
	// Unknown value (parser should have rejected, but defensive).
	return true
}

// checkAuthzRoles enforces an OR-list of role gates. Returns true on the first
// role that allows; returns false only after every role has been considered
// without a match.
//
// Supported roles:
//   - admin          (slice 1, #341): member of the global Administrators group.
//   - automation     (slice 5, #368): member of either tmi-automation or
//     embedding-automation. /automation/embeddings/* additionally narrows to
//     embedding-automation via the layered EmbeddingAutomationMiddleware.
//
// Recognized but not enforced (future slices):
//   - security_reviewer    (slice 4, #367 / future role-gates)
//   - confidential_reviewer (future)
//
// Encountering a not-yet-supported role in a rule logs at Warn and skips it
// so the rest of the OR list still gets a chance to satisfy the gate. If
// every role in the list is unsupported, a 403 is returned (better than the
// 500 the original ad-hoc default arm produced before #341 fixed it).
func checkAuthzRoles(c *gin.Context, roles []AuthzRoleName) bool {
	for _, r := range roles {
		switch r {
		case RoleAuthzAdmin:
			if _, err := RequireAdministrator(c); err != nil {
				// RequireAdministrator already wrote the response.
				return false
			}
			return true
		case RoleAuthzAutomation:
			if checkAutomationRole(c) {
				return true
			}
			// Not in any automation group — try the next role in the OR list
			// (don't return false yet; another role might satisfy the gate).
		default:
			slogging.Get().WithContext(c).Warn(
				"AuthzMiddleware: unsupported role gate %q skipped; other roles in the OR list will still be evaluated",
				r)
		}
	}
	// Loop ended without any role allowing. The response is a 403 regardless
	// of whether the failure was "every role was unsupported" or "all roles
	// were evaluable and none allowed". Admin-fail short-circuits inside the
	// case and never reaches this point because RequireAdministrator has
	// already written a 401/403.
	HandleRequestError(c, &RequestError{
		Status:  http.StatusForbidden,
		Code:    "forbidden",
		Message: "Access denied",
	})
	return false
}

// checkAutomationRole returns true if the caller is a member of either the
// tmi-automation or embedding-automation group. Mirrors the OR check in the
// legacy AutomationMiddleware (api/automation_middleware.go) so /automation/*
// routes can carry x-tmi-authz: { roles: [automation] } and have the
// AuthzMiddleware enforce the outer gate. The inner /automation/embeddings/*
// gate (embedding-automation only) is layered separately in
// EmbeddingAutomationMiddleware.
//
// On unauthenticated callers or membership-resolution failures, this returns
// false — the caller (checkAuthzRoles) writes a 403. We intentionally do NOT
// distinguish 401/403 here: the per-role decision is just yes/no for the
// OR-list reducer; the final response is shaped by the loop's outcome.
func checkAutomationRole(c *gin.Context) bool {
	logger := slogging.Get().WithContext(c)
	mc, err := ResolveMembershipContext(c)
	if err != nil {
		logger.Debug("AuthzMiddleware: automation role check — membership context unresolved: %v", err)
		return false
	}
	ctx := c.Request.Context()
	if isAuto, err := IsGroupMember(ctx, mc, GroupTMIAutomation); err == nil && isAuto {
		return true
	} else if err != nil {
		logger.Warn("AuthzMiddleware: tmi-automation membership check failed for email=%s: %v", mc.Email, err)
	}
	if isEmb, err := IsGroupMember(ctx, mc, GroupEmbeddingAutomation); err == nil && isEmb {
		return true
	} else if err != nil {
		logger.Warn("AuthzMiddleware: embedding-automation membership check failed for email=%s: %v", mc.Email, err)
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
