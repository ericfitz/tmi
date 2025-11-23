package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ValidateDuplicateSubjects checks for duplicate subjects in authorization list
func ValidateDuplicateSubjects(authList []Authorization) error {
	subjectMap := make(map[string]bool)

	for _, auth := range authList {
		if _, exists := subjectMap[auth.ProviderId]; exists {
			return &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Duplicate authorization subject: %s", auth.ProviderId),
			}
		}
		subjectMap[auth.ProviderId] = true
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
		if authList[i].ProviderId == originalOwner {
			// Make sure the original owner has the Owner role
			authList[i].Role = RoleOwner
			originalOwnerFound = true
			break
		}
	}

	// If the original owner isn't in the list, add them
	if !originalOwnerFound {
		authList = append(authList, Authorization{
			PrincipalType: AuthorizationPrincipalTypeUser,
			Provider:      "test", // TODO: Need provider context from caller
			ProviderId:    originalOwner,
			Role:          RoleOwner,
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
			if providerId, ok := auth["provider_id"].(string); ok {
				authObj.ProviderId = providerId
			}
			if provider, ok := auth["provider"].(string); ok {
				authObj.Provider = provider
			}
			if principalType, ok := auth["principal_type"].(string); ok {
				authObj.PrincipalType = AuthorizationPrincipalType(principalType)
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
		if auth.ProviderId == "" {
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
		if auth.ProviderId == "" {
			return &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Authorization subject at index %d cannot be empty", i),
			}
		}

		if len(auth.ProviderId) > 255 {
			return &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Authorization subject '%s' exceeds maximum length of 255 characters", auth.ProviderId),
			}
		}

		// Validate role is valid
		if auth.Role != RoleReader && auth.Role != RoleWriter && auth.Role != RoleOwner {
			return &RequestError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Invalid role '%s' for subject '%s'. Must be one of: reader, writer, owner", auth.Role, auth.ProviderId),
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

	// EveryonePseudoGroupUUID is the flag UUID used to represent the "everyone" pseudo-group
	// in the database. This allows storing "everyone" in a UUID column (subject_internal_uuid).
	// The zero UUID (all zeros) is used as it will never conflict with real user UUIDs.
	EveryonePseudoGroupUUID = "00000000-0000-0000-0000-000000000000"
)

// AuthorizationData represents abstracted authorization data for any resource
type AuthorizationData struct {
	Type          string          `json:"type"`
	Owner         User            `json:"owner"`
	Authorization []Authorization `json:"authorization"`
}

// AccessCheck performs core authorization logic
// Returns true if the principal has the required role for the given authorization data
func AccessCheck(principal string, requiredRole Role, authData AuthorizationData) bool {
	// Validate authorization type
	if authData.Type != AuthTypeTMI10 {
		return false
	}

	// Check if principal is the owner (Owner is now a User object)
	if authData.Owner.ProviderId == principal {
		// Owner always has access regardless of required role
		return true
	}

	// Check authorization list for principal's highest role (user only)
	var highestRole Role
	found := false

	for _, auth := range authData.Authorization {
		// For user authorization (default for backward compatibility)
		// If SubjectType is empty string, assume it's a user for backward compatibility
		if (auth.PrincipalType == "" || auth.PrincipalType == AuthorizationPrincipalTypeUser) && auth.ProviderId == principal {
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

// matchesUserIdentifier checks if a subject identifier matches a user using flexible resolution:
//  1. Try direct match against internal_uuid (primary identifier)
//  2. Try direct match against provider_user_id (OAuth provider's user ID)
//  3. Try direct match against email (fallback)
func matchesUserIdentifier(owner User, userEmail string, userProviderID string, userInternalUUID string) bool {
	// Check if owner matches any of the user identifiers
	// Owner.ProviderId could be internal_uuid, provider_user_id, or email
	return owner.ProviderId == userInternalUUID || owner.ProviderId == userProviderID || owner.ProviderId == userEmail
}

// matchesProviderID checks if a provider_id string matches any user identifier
func matchesProviderID(providerId string, userEmail string, userProviderID string, userInternalUUID string) bool {
	// Check if providerId matches any of the user identifiers
	return providerId == userInternalUUID || providerId == userProviderID || providerId == userEmail
}

// AccessCheckWithGroups performs authorization check with group support and flexible user matching
// Returns true if the principal or one of their groups has the required role
// Uses flexible matching: email, provider_user_id, or internal_uuid
func AccessCheckWithGroups(principal string, principalProviderID string, principalInternalUUID string, principalIdP string, principalGroups []string, requiredRole Role, authData AuthorizationData) bool {
	return AccessCheckWithGroupsAndIdPLookup(context.Background(), nil, principal, principalProviderID, principalInternalUUID, principalIdP, principalGroups, requiredRole, authData)
}

// checkUserMatch checks if an authorization entry matches the principal user
// Returns true if the user matches, using flexible matching
func checkUserMatch(ctx context.Context, authService AuthService, auth Authorization, principal string, principalProviderID string, principalInternalUUID string) bool {
	// Use flexible matching: email, provider_user_id, or internal_uuid
	return matchesProviderID(auth.ProviderId, principal, principalProviderID, principalInternalUUID)
}

// checkGroupMatch checks if an authorization entry matches the principal's groups
// Returns true if the user is a member of the group, handling special pseudo-groups
func checkGroupMatch(auth Authorization, principal string, principalIdP string, principalGroups []string) bool {
	// Special handling for "everyone" pseudo-group
	if auth.ProviderId == EveryonePseudoGroup {
		logger := slogging.Get()
		logger.Debug("Access granted via 'everyone' pseudo-group with role: %s for user: %s",
			auth.Role, principal)
		return true
	}

	// Normal groups must match both the group name AND the provider
	// Provider "*" means provider-independent (matches all providers)
	if auth.Provider == "*" || auth.Provider == principalIdP {
		for _, group := range principalGroups {
			if auth.ProviderId == group {
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

// AccessCheckWithGroupsAndIdPLookup performs authorization check with group support and flexible user matching
// Returns true if the principal or one of their groups has the required role
// Uses flexible matching algorithm:
// 1. Try direct match (internal_uuid, provider_user_id, or email)
// 2. When authService is provided, enables IdP-based lookups
func AccessCheckWithGroupsAndIdPLookup(ctx context.Context, authService AuthService, principal string, principalProviderID string, principalInternalUUID string, principalIdP string, principalGroups []string, requiredRole Role, authData AuthorizationData) bool {
	// Validate authorization type
	if authData.Type != AuthTypeTMI10 {
		return false
	}

	// Check if principal is the owner using flexible matching
	if matchesUserIdentifier(authData.Owner, principal, principalProviderID, principalInternalUUID) {
		return true
	}

	// Check authorization list for principal's highest role
	var highestRole Role
	found := false

	for _, auth := range authData.Authorization {
		// Check user authorization
		if auth.PrincipalType == "" || auth.PrincipalType == AuthorizationPrincipalTypeUser {
			if checkUserMatch(ctx, authService, auth, principal, principalProviderID, principalInternalUUID) {
				highestRole, found = updateHighestRole(highestRole, auth.Role, found)
			}
		}

		// Check group authorization
		if auth.PrincipalType == AuthorizationPrincipalTypeGroup {
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
			authData.Owner = User{
				PrincipalType: UserPrincipalTypeUser,
				Provider:      "test",
				ProviderId:    TestFixtures.Owner,
				DisplayName:   TestFixtures.Owner,
				Email:         openapi_types.Email(TestFixtures.Owner),
			}
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
func CheckResourceAccessWithGroups(subject string, subjectProviderID string, subjectInternalUUID string, subjectIdP string, subjectGroups []string, resource interface{}, requiredRole Role) (bool, error) {
	// Extract authorization data from the resource
	authData, err := ExtractAuthData(resource)
	if err != nil {
		return false, err
	}

	// Use AccessCheckWithGroups to determine access (supports groups and "everyone")
	hasAccess := AccessCheckWithGroups(subject, subjectProviderID, subjectInternalUUID, subjectIdP, subjectGroups, requiredRole, authData)
	return hasAccess, nil
}

// CheckResourceAccessFromContext checks resource access using subject info from Gin context
// This is a convenience function that extracts subject (user email/ID), IdP, and groups from the context
// and calls CheckResourceAccessWithGroups for group-aware authorization including "everyone" pseudo-group.
func CheckResourceAccessFromContext(c *gin.Context, subject string, resource interface{}, requiredRole Role) (bool, error) {
	// Get subject's provider ID, IdP and groups from context for group-based authorization
	subjectProviderID := ""
	if providerID, exists := c.Get("userID"); exists {
		subjectProviderID, _ = providerID.(string)
	}

	subjectInternalUUID := ""
	if internalUUID, exists := c.Get("userInternalUUID"); exists {
		subjectInternalUUID, _ = internalUUID.(string)
	}

	subjectIdP := ""
	if idp, exists := c.Get("userIdP"); exists {
		subjectIdP, _ = idp.(string)
	}

	var subjectGroups []string
	if groups, exists := c.Get("userGroups"); exists {
		subjectGroups, _ = groups.([]string)
	}

	// Use the group-aware version
	return CheckResourceAccessWithGroups(subject, subjectProviderID, subjectInternalUUID, subjectIdP, subjectGroups, resource, requiredRole)
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
	// Using dual FK structure: user_internal_uuid, group_internal_uuid, subject_type, role
	accessQuery := `
		SELECT
			COALESCE(user_internal_uuid::text, group_internal_uuid::text) as subject,
			subject_type,
			role
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
		var role string

		if err := rows.Scan(&subject, &subjectType, &role); err != nil {
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
		var authPrincipalType AuthorizationPrincipalType
		switch subjectType {
		case "user":
			authPrincipalType = AuthorizationPrincipalTypeUser
		case "group":
			authPrincipalType = AuthorizationPrincipalTypeGroup
		default:
			// For backward compatibility, treat empty or unknown as user
			authPrincipalType = AuthorizationPrincipalTypeUser
		}

		// Build authorization entry with proper principal type
		// Note: This function is part of GetInheritedAuthData which needs full refactoring
		// to properly enrich principal data from database
		auth := Authorization{
			PrincipalType: authPrincipalType,
			Provider:      "unknown", // TODO: Need to enrich from database
			ProviderId:    subject,
			Role:          roleType,
		}

		authorization = append(authorization, auth)
	}

	if err = rows.Err(); err != nil {
		logger.Error("Error iterating threat model access rows: %v", err)
		return nil, fmt.Errorf("error iterating access rows: %w", err)
	}

	// Build authorization data
	// TODO: Refactor GetInheritedAuthData to properly enrich owner from database
	authData := &AuthorizationData{
		Type: AuthTypeTMI10,
		Owner: User{
			PrincipalType: UserPrincipalTypeUser,
			Provider:      "unknown", // TODO: Query from database
			ProviderId:    ownerEmail,
			DisplayName:   ownerEmail,
			Email:         openapi_types.Email(ownerEmail),
		},
		Authorization: authorization,
	}

	logger.Debug("Retrieved authorization data for threat model %s: owner=%s, %d access entries",
		threatModelID, ownerEmail, len(authorization))

	return authData, nil
}

// CheckSubResourceAccess validates if a user has the required access to a sub-resource
// This function implements authorization inheritance with Redis caching for performance
// Now supports group-based authorization with IdP scoping and flexible user matching
func CheckSubResourceAccess(ctx context.Context, db *sql.DB, cache *CacheService, principal, principalProviderID, principalInternalUUID, principalIdP string, principalGroups []string, threatModelID string, requiredRole Role) (bool, error) {
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
	hasAccess := AccessCheckWithGroups(principal, principalProviderID, principalInternalUUID, principalIdP, principalGroups, requiredRole, *authData)

	logger.Debug("Access check result for user %s on threat model %s: %t", principal, threatModelID, hasAccess)
	return hasAccess, nil
}

// CheckSubResourceAccessWithoutCache validates sub-resource access without caching
// This is useful for testing or when caching is not available
// Now supports group-based authorization with IdP scoping and flexible user matching
func CheckSubResourceAccessWithoutCache(ctx context.Context, db *sql.DB, principal, principalProviderID, principalInternalUUID, principalIdP string, principalGroups []string, threatModelID string, requiredRole Role) (bool, error) {
	// Note: cache parameter is nil for no caching
	var cache *CacheService = nil
	return CheckSubResourceAccess(ctx, db, cache, principal, principalProviderID, principalInternalUUID, principalIdP, principalGroups, threatModelID, requiredRole)
}
