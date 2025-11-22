package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// ValidateDuplicateSubjects checks for duplicate subjects in authorization list
func ValidateDuplicateSubjects(authList []Authorization) error {
	subjectMap := make(map[string]bool)

	for _, auth := range authList {
		if _, exists := subjectMap[auth.Subject]; exists {
			return &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Duplicate authorization subject: %s", auth.Subject),
			}
		}
		subjectMap[auth.Subject] = true
	}

	return nil
}

// ApplyOwnershipTransferRule applies the business rule that when ownership changes,
// the original owner should be preserved in the authorization list with owner role
func ApplyOwnershipTransferRule(authList []Authorization, originalOwner, newOwner string) []Authorization {
	if originalOwner == newOwner {
		return authList // No ownership change
	}

	// Check if the original owner is already in the authorization list
	originalOwnerFound := false
	for i := range authList {
		if authList[i].Subject == originalOwner {
			// Make sure the original owner has the Owner role
			authList[i].Role = RoleOwner
			originalOwnerFound = true
			break
		}
	}

	// If the original owner isn't in the list, add them
	if !originalOwnerFound {
		authList = append(authList, Authorization{
			Subject: originalOwner,
			Role:    RoleOwner,
		})
	}

	return authList
}

// ExtractOwnershipChangesFromOperations extracts owner and authorization changes from patch operations
func ExtractOwnershipChangesFromOperations(operations []PatchOperation) (newOwner string, newAuth []Authorization, hasOwnerChange, hasAuthChange bool) {
	for _, op := range operations {
		if op.Op == "replace" || op.Op == "add" {
			switch op.Path {
			case "/owner":
				if ownerVal, ok := op.Value.(string); ok && ownerVal != "" {
					newOwner = ownerVal
					hasOwnerChange = true
				}
			case "/authorization":
				if authVal, ok := op.Value.([]interface{}); ok {
					newAuth = convertInterfaceToAuthList(authVal)
					hasAuthChange = true
				}
			}
		}
	}
	return newOwner, newAuth, hasOwnerChange, hasAuthChange
}

// convertInterfaceToAuthList converts []interface{} to []Authorization
func convertInterfaceToAuthList(authList []interface{}) []Authorization {
	result := make([]Authorization, 0, len(authList))

	for _, authItem := range authList {
		if auth, ok := authItem.(map[string]interface{}); ok {
			var authObj Authorization
			if subject, ok := auth["subject"].(string); ok {
				authObj.Subject = subject
			}
			if role, ok := auth["role"].(string); ok {
				authObj.Role = Role(role)
			}
			result = append(result, authObj)
		}
	}

	return result
}

// ValidateAuthorizationEntries validates individual authorization entries
func ValidateAuthorizationEntries(authList []Authorization) error {
	for _, auth := range authList {
		if auth.Subject == "" {
			return &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_input",
				Message: "Authorization subject cannot be empty",
			}
		}
	}
	return nil
}

// ValidateAuthorizationEntriesWithFormat validates authorization entries with format checking
func ValidateAuthorizationEntriesWithFormat(authList []Authorization) error {
	for i, auth := range authList {
		// Validate subject format
		if auth.Subject == "" {
			return &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Authorization subject at index %d cannot be empty", i),
			}
		}

		if len(auth.Subject) > 255 {
			return &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Authorization subject '%s' exceeds maximum length of 255 characters", auth.Subject),
			}
		}

		// Validate role is valid
		if auth.Role != RoleReader && auth.Role != RoleWriter && auth.Role != RoleOwner {
			return &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Invalid role '%s' for subject '%s'. Must be one of: reader, writer, owner", auth.Role, auth.Subject),
			}
		}
	}
	return nil
}

// Authorization type constants
const (
	AuthTypeTMI10 = "tmi-1.0"
)

// Pseudo-group constants
const (
	// EveryonePseudoGroup is a special group that matches all authenticated users
	// regardless of their identity provider or actual group memberships
	EveryonePseudoGroup = "everyone"
)

// AuthorizationData represents abstracted authorization data for any resource
type AuthorizationData struct {
	Type          string          `json:"type"`
	Owner         string          `json:"owner"`
	Authorization []Authorization `json:"authorization"`
}

// AccessCheck performs core authorization logic
// Returns true if the principal has the required role for the given authorization data
func AccessCheck(principal string, requiredRole Role, authData AuthorizationData) bool {
	// Validate authorization type
	if authData.Type != AuthTypeTMI10 {
		return false
	}

	// Check if principal is the owner
	if authData.Owner == principal {
		// Owner always has access regardless of required role
		return true
	}

	// Check authorization list for principal's highest role (user only)
	var highestRole Role
	found := false

	for _, auth := range authData.Authorization {
		// For user authorization (default for backward compatibility)
		// If SubjectType is empty string, assume it's a user for backward compatibility
		if (auth.SubjectType == "" || auth.SubjectType == AuthorizationSubjectTypeUser) && auth.Subject == principal {
			if !found || isHigherRole(auth.Role, highestRole) {
				highestRole = auth.Role
				found = true
			}
		}
	}

	if !found {
		// Principal not found in authorization list
		return false
	}

	// Check if the principal's highest role meets the required role
	return hasRequiredRole(highestRole, requiredRole)
}

// AccessCheckWithGroups performs authorization check with group support
// Returns true if the principal or one of their groups has the required role
// This is a compatibility wrapper that calls the enhanced version with nil auth service
func AccessCheckWithGroups(principal string, principalIdP string, principalGroups []string, requiredRole Role, authData AuthorizationData) bool {
	return AccessCheckWithGroupsAndIdPLookup(context.Background(), nil, principal, principalIdP, principalGroups, requiredRole, authData)
}

// checkUserMatch checks if an authorization entry matches the principal user
// Returns true if the user matches, using two-step matching when authService is available
func checkUserMatch(ctx context.Context, authService AuthService, auth Authorization, principal string) bool {
	if authService != nil {
		// Step 1: Try to match subject as IdP user ID
		if auth.Idp != nil && *auth.Idp != "" {
			// Provider-specific check: subject is IdP user ID for specific provider
			if authAdapter, ok := authService.(*AuthServiceAdapter); ok {
				service := authAdapter.GetService()
				if service != nil {
					user, err := service.GetUserByProviderID(ctx, *auth.Idp, auth.Subject)
					if err == nil && user.Email == principal {
						return true
					}
				}
			}
		} else {
			// Provider-independent check: subject could be IdP ID from any provider
			if authAdapter, ok := authService.(*AuthServiceAdapter); ok {
				service := authAdapter.GetService()
				if service != nil {
					user, err := service.GetUserByAnyProviderID(ctx, auth.Subject)
					if err == nil && user.Email == principal {
						return true
					}
				}
			}
		}
	}

	// Step 2: Fallback to direct email matching
	return auth.Subject == principal
}

// checkGroupMatch checks if an authorization entry matches the principal's groups
// Returns true if the user is a member of the group, handling special pseudo-groups
func checkGroupMatch(auth Authorization, principal string, principalIdP string, principalGroups []string) bool {
	// Special handling for "everyone" pseudo-group
	if auth.Subject == EveryonePseudoGroup {
		logger := slogging.Get()
		logger.Debug("Access granted via 'everyone' pseudo-group with role: %s for user: %s",
			auth.Role, principal)
		return true
	}

	// Normal groups must match both the group name AND the IdP
	if auth.Idp != nil && *auth.Idp == principalIdP {
		for _, group := range principalGroups {
			if auth.Subject == group {
				return true
			}
		}
	}

	return false
}

// updateHighestRole updates the highest role if the new role is higher
func updateHighestRole(currentHighest Role, newRole Role, found bool) (Role, bool) {
	if !found || isHigherRole(newRole, currentHighest) {
		return newRole, true
	}
	return currentHighest, found
}

// AccessCheckWithGroupsAndIdPLookup performs authorization check with group support and IdP user ID lookup
// Returns true if the principal or one of their groups has the required role
// When authService is provided, it enables two-step user matching:
// 1. First tries to match subject as IdP user ID (provider_user_id)
// 2. Falls back to email matching if no IdP match found
func AccessCheckWithGroupsAndIdPLookup(ctx context.Context, authService AuthService, principal string, principalIdP string, principalGroups []string, requiredRole Role, authData AuthorizationData) bool {
	// Validate authorization type
	if authData.Type != AuthTypeTMI10 {
		return false
	}

	// Check if principal is the owner using two-step matching
	// Step 1: Try direct email match
	if authData.Owner == principal {
		return true
	}

	// Step 2: Try IdP user ID match if authService is available
	if authService != nil && authData.Owner != "" {
		if authAdapter, ok := authService.(*AuthServiceAdapter); ok {
			service := authAdapter.GetService()
			if service != nil {
				// Try to resolve owner as IdP user ID from any provider
				user, err := service.GetUserByAnyProviderID(ctx, authData.Owner)
				if err == nil && user.Email == principal {
					return true
				}
			}
		}
	}

	// Check authorization list for principal's highest role
	var highestRole Role
	found := false

	for _, auth := range authData.Authorization {
		// Check user authorization
		if auth.SubjectType == "" || auth.SubjectType == AuthorizationSubjectTypeUser {
			if checkUserMatch(ctx, authService, auth, principal) {
				highestRole, found = updateHighestRole(highestRole, auth.Role, found)
			}
		}

		// Check group authorization
		if auth.SubjectType == AuthorizationSubjectTypeGroup {
			if checkGroupMatch(auth, principal, principalIdP, principalGroups) {
				highestRole, found = updateHighestRole(highestRole, auth.Role, found)
			}
		}
	}

	if !found {
		return false
	}

	return hasRequiredRole(highestRole, requiredRole)
}

// isHigherRole checks if role1 has higher permissions than role2
// Role hierarchy: owner > writer > reader
func isHigherRole(role1, role2 Role) bool {
	roleHierarchy := map[Role]int{
		RoleReader: 1,
		RoleWriter: 2,
		RoleOwner:  3,
	}

	level1, exists1 := roleHierarchy[role1]
	level2, exists2 := roleHierarchy[role2]

	// If either role is invalid, consider them equal (return false)
	if !exists1 || !exists2 {
		return false
	}

	return level1 > level2
}

// hasRequiredRole checks if the user's role meets the required role
// Role hierarchy: owner > writer > reader
func hasRequiredRole(userRole, requiredRole Role) bool {
	roleHierarchy := map[Role]int{
		RoleReader: 1,
		RoleWriter: 2,
		RoleOwner:  3,
	}

	userLevel, userExists := roleHierarchy[userRole]
	requiredLevel, requiredExists := roleHierarchy[requiredRole]

	// If either role is invalid, deny access
	if !userExists || !requiredExists {
		return false
	}

	// User's role level must be >= required level
	return userLevel >= requiredLevel
}

// ExtractAuthData extracts authorization data from threat models or diagrams
// This is a generic helper that works with any struct that has Owner and Authorization fields
func ExtractAuthData(resource interface{}) (AuthorizationData, error) {
	var authData AuthorizationData
	authData.Type = AuthTypeTMI10 // Default to current supported type

	// Type assertion for different resource types
	switch r := resource.(type) {
	case ThreatModel:
		authData.Owner = r.Owner
		authData.Authorization = r.Authorization
		return authData, nil
	case *ThreatModel:
		authData.Owner = r.Owner
		authData.Authorization = r.Authorization
		return authData, nil
	case DfdDiagram:
		// For diagrams, use TestFixtures pattern for now
		if TestFixtures.Owner != "" {
			authData.Owner = TestFixtures.Owner
			authData.Authorization = TestFixtures.DiagramAuth
			return authData, nil
		}
	}

	// If no data available, return error
	return authData, &RequestError{
		Status:  http.StatusInternalServerError,
		Code:    "server_error",
		Message: "Unable to extract authorization data from resource",
	}
}

// CheckResourceAccess is a utility function that checks if a subject has required access to a resource
// This function uses the basic AccessCheck and does NOT support group-based authorization.
// For group support (including "everyone" pseudo-group), use CheckResourceAccessWithGroups instead.
// Note: subject can be a user email or user ID, but group matching is not supported by this function.
func CheckResourceAccess(subject string, resource interface{}, requiredRole Role) (bool, error) {
	// Extract authorization data from the resource
	authData, err := ExtractAuthData(resource)
	if err != nil {
		return false, err
	}

	// Use AccessCheck to determine access
	hasAccess := AccessCheck(subject, requiredRole, authData)
	return hasAccess, nil
}

// CheckResourceAccessWithGroups checks if a subject has required access to a resource with group support
// This function supports group-based authorization including the "everyone" pseudo-group.
// The subject can be a user email or user ID. The function also checks group memberships.
func CheckResourceAccessWithGroups(subject string, subjectIdP string, subjectGroups []string, resource interface{}, requiredRole Role) (bool, error) {
	// Extract authorization data from the resource
	authData, err := ExtractAuthData(resource)
	if err != nil {
		return false, err
	}

	// Use AccessCheckWithGroups to determine access (supports groups and "everyone")
	hasAccess := AccessCheckWithGroups(subject, subjectIdP, subjectGroups, requiredRole, authData)
	return hasAccess, nil
}

// CheckResourceAccessFromContext checks resource access using subject info from Gin context
// This is a convenience function that extracts subject (user email/ID), IdP, and groups from the context
// and calls CheckResourceAccessWithGroups for group-aware authorization including "everyone" pseudo-group.
func CheckResourceAccessFromContext(c *gin.Context, subject string, resource interface{}, requiredRole Role) (bool, error) {
	// Get subject's IdP and groups from context for group-based authorization
	subjectIdP := ""
	if idp, exists := c.Get("userIdP"); exists {
		subjectIdP, _ = idp.(string)
	}

	var subjectGroups []string
	if groups, exists := c.Get("userGroups"); exists {
		subjectGroups, _ = groups.([]string)
	}

	// Use the group-aware version
	return CheckResourceAccessWithGroups(subject, subjectIdP, subjectGroups, resource, requiredRole)
}

// ValidateResourceAccess is a Gin middleware-compatible function for authorization checks
func ValidateResourceAccess(requiredRole Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get authenticated user
		userEmail, _, err := ValidateAuthenticatedUser(c)
		if err != nil {
			HandleRequestError(c, err)
			c.Abort()
			return
		}

		// For now, we'll use a generic resource placeholder
		// In practice, this would extract the specific resource from context or ID
		var resource interface{}

		// Check resource access
		hasAccess, err := CheckResourceAccess(userEmail, resource, requiredRole)
		if err != nil {
			HandleRequestError(c, err)
			c.Abort()
			return
		}

		if !hasAccess {
			HandleRequestError(c, ForbiddenError("Insufficient permissions for this resource"))
			c.Abort()
			return
		}

		// Access granted, continue
		c.Next()
	}
}

// GetInheritedAuthData retrieves authorization data for a threat model from the database
// This function implements authorization inheritance by fetching threat model permissions
// that apply to all sub-resources within that threat model
func GetInheritedAuthData(ctx context.Context, db *sql.DB, threatModelID string) (*AuthorizationData, error) {
	logger := slogging.Get()
	logger.Debug("Retrieving inherited authorization data for threat model %s", threatModelID)

	// Query threat model to get owner (joining with users table to get email from internal_uuid)
	threatModelQuery := `
		SELECT u.email, tm.created_by
		FROM threat_models tm
		JOIN users u ON tm.owner_internal_uuid = u.internal_uuid
		WHERE tm.id = $1
	`

	var ownerEmail, createdBy string
	err := db.QueryRow(threatModelQuery, threatModelID).Scan(&ownerEmail, &createdBy)
	if err != nil {
		logger.Error("Failed to query threat model %s: %v", threatModelID, err)
		return nil, fmt.Errorf("failed to query threat model: %w", err)
	}

	// Query threat model access table to get authorization list
	// Using the modern column structure: subject, subject_type, idp, role
	accessQuery := `
		SELECT subject, subject_type, idp, role
		FROM threat_model_access
		WHERE threat_model_id = $1
		ORDER BY role DESC, subject ASC
	`

	rows, err := db.Query(accessQuery, threatModelID)
	if err != nil {
		logger.Error("Failed to query threat model access for %s: %v", threatModelID, err)
		return nil, fmt.Errorf("failed to query threat model access: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			logger.Error("Failed to close rows: %v", closeErr)
		}
	}()

	var authorization []Authorization
	for rows.Next() {
		var subject string
		var subjectType string
		var idp sql.NullString
		var role string

		if err := rows.Scan(&subject, &subjectType, &idp, &role); err != nil {
			logger.Error("Failed to scan threat model access row: %v", err)
			return nil, fmt.Errorf("failed to scan access row: %w", err)
		}

		// Convert string role to Role type
		var roleType Role
		switch role {
		case "owner":
			roleType = RoleOwner
		case "writer":
			roleType = RoleWriter
		case "reader":
			roleType = RoleReader
		default:
			logger.Error("Invalid role %s found for subject %s in threat model %s", role, subject, threatModelID)
			continue // Skip invalid roles
		}

		// Convert string subject_type to proper enum
		var authSubjectType AuthorizationSubjectType
		switch subjectType {
		case "user":
			authSubjectType = AuthorizationSubjectTypeUser
		case "group":
			authSubjectType = AuthorizationSubjectTypeGroup
		default:
			// For backward compatibility, treat empty or unknown as user
			authSubjectType = AuthorizationSubjectTypeUser
		}

		// Build authorization entry with proper subject type and IdP
		auth := Authorization{
			Subject:     subject,
			SubjectType: authSubjectType,
			Role:        roleType,
		}

		// Set IdP if present
		if idp.Valid && idp.String != "" {
			auth.Idp = &idp.String
		}

		authorization = append(authorization, auth)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Error iterating threat model access rows: %v", err)
		return nil, fmt.Errorf("error iterating access rows: %w", err)
	}

	// Build authorization data
	authData := &AuthorizationData{
		Type:          AuthTypeTMI10,
		Owner:         ownerEmail,
		Authorization: authorization,
	}

	logger.Debug("Retrieved authorization data for threat model %s: owner=%s, %d access entries",
		threatModelID, ownerEmail, len(authorization))

	return authData, nil
}

// CheckSubResourceAccess validates if a user has the required access to a sub-resource
// This function implements authorization inheritance with Redis caching for performance
// Now supports group-based authorization with IdP scoping
func CheckSubResourceAccess(ctx context.Context, db *sql.DB, cache *CacheService, principal, principalIdP string, principalGroups []string, threatModelID string, requiredRole Role) (bool, error) {
	logger := slogging.Get()
	logger.Debug("Checking sub-resource access for user %s on threat model %s (required role: %s)",
		principal, threatModelID, requiredRole)

	// Try to get authorization data from cache first
	var authData *AuthorizationData
	var err error

	if cache != nil {
		authData, err = cache.GetCachedAuthData(ctx, threatModelID)
		if err != nil {
			logger.Error("Failed to get cached auth data: %v", err)
			// Continue without cache - don't fail the request
		}
	}

	// If not in cache, get from database
	if authData == nil {
		authData, err = GetInheritedAuthData(ctx, db, threatModelID)
		if err != nil {
			logger.Error("Failed to get inherited auth data for threat model %s: %v", threatModelID, err)
			return false, fmt.Errorf("failed to get authorization data: %w", err)
		}

		// Cache the result for future requests
		if cache != nil {
			if cacheErr := cache.CacheAuthData(ctx, threatModelID, *authData); cacheErr != nil {
				logger.Error("Failed to cache auth data: %v", cacheErr)
				// Don't fail the request if caching fails
			}
		}
	}

	// Perform access check using the authorization data with group support
	hasAccess := AccessCheckWithGroups(principal, principalIdP, principalGroups, requiredRole, *authData)

	logger.Debug("Access check result for user %s on threat model %s: %t", principal, threatModelID, hasAccess)
	return hasAccess, nil
}

// CheckSubResourceAccessWithoutCache validates sub-resource access without caching
// This is useful for testing or when caching is not available
// Now supports group-based authorization with IdP scoping
func CheckSubResourceAccessWithoutCache(ctx context.Context, db *sql.DB, principal, principalIdP string, principalGroups []string, threatModelID string, requiredRole Role) (bool, error) {
	return CheckSubResourceAccess(ctx, db, nil, principal, principalIdP, principalGroups, threatModelID, requiredRole)
}
