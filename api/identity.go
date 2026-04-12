package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ericfitz/tmi/internal/slogging"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ResolvedUser is the internal canonical representation of an authenticated user identity.
// It is the ONLY type that should be passed between functions for identity operations.
// It is never serialized to wire format directly — convert to/from API types (User, Principal)
// at system boundaries.
type ResolvedUser struct {
	InternalUUID string // System-assigned UUID from users table (may be empty if unresolved)
	Provider     string // Identity provider name (e.g., "google", "github", "tmi")
	ProviderID   string // Provider-assigned unique identifier (OAuth sub / SAML NameID)
	Email        string // User's email address (mutable contact attribute, not identity)
	DisplayName  string // Human-readable display name
}

// IsEmpty returns true if the ResolvedUser has no identity fields set.
func (u ResolvedUser) IsEmpty() bool {
	return u.InternalUUID == "" && u.Provider == "" && u.ProviderID == "" && u.Email == "" && u.DisplayName == ""
}

// ToUser converts a ResolvedUser to the API User type for wire format serialization.
func (u ResolvedUser) ToUser() User {
	return User{
		PrincipalType: UserPrincipalTypeUser,
		Provider:      u.Provider,
		ProviderId:    u.ProviderID,
		Email:         openapi_types.Email(u.Email),
		DisplayName:   u.DisplayName,
	}
}

// ToPrincipal converts a ResolvedUser to the API Principal type for wire format serialization.
func (u ResolvedUser) ToPrincipal() Principal {
	email := openapi_types.Email(u.Email)
	displayName := u.DisplayName
	return Principal{
		PrincipalType: PrincipalPrincipalTypeUser,
		Provider:      u.Provider,
		ProviderId:    u.ProviderID,
		Email:         &email,
		DisplayName:   &displayName,
	}
}

// ResolvedUserFromUser creates a ResolvedUser from an API User.
// InternalUUID will be empty since the API User type does not carry it.
func ResolvedUserFromUser(u User) ResolvedUser {
	return ResolvedUser{
		Provider:    u.Provider,
		ProviderID:  u.ProviderId,
		Email:       string(u.Email),
		DisplayName: u.DisplayName,
	}
}

// ResolvedUserFromPrincipal creates a ResolvedUser from an API Principal.
// InternalUUID will be empty since the API Principal type does not carry it.
func ResolvedUserFromPrincipal(p Principal) ResolvedUser {
	ru := ResolvedUser{
		Provider:   p.Provider,
		ProviderID: p.ProviderId,
	}
	if p.Email != nil {
		ru.Email = string(*p.Email)
	}
	if p.DisplayName != nil {
		ru.DisplayName = *p.DisplayName
	}
	return ru
}

// GetAuthenticatedUser extracts the authenticated user identity from the Gin context.
// Returns a ResolvedUser populated from JWT claims set by auth middleware.
// Requires userID (provider ID) and userEmail to be present; returns 401 if missing.
// Provider, InternalUUID, and DisplayName are populated if available.
//
// This replaces ValidateAuthenticatedUser. Role is NOT included — use GetResourceRole separately.
func GetAuthenticatedUser(c *gin.Context) (ResolvedUser, error) {
	// Get user email from JWT claim (required)
	userEmailInterface, _ := c.Get("userEmail")
	userEmail, ok := userEmailInterface.(string)
	if !ok || userEmail == "" {
		return ResolvedUser{}, &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Authentication required",
		}
	}

	// Get provider user ID from JWT "sub" claim (required)
	providerIDInterface, _ := c.Get("userID")
	providerID, ok := providerIDInterface.(string)
	if !ok || providerID == "" {
		return ResolvedUser{}, &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Authentication required - missing provider ID",
		}
	}

	// Get provider name (optional — set by JWT middleware)
	provider := ""
	if p, exists := c.Get("userProvider"); exists {
		if pStr, ok := p.(string); ok {
			provider = pStr
		}
	}

	// Get internal UUID (optional — may not be set if middleware hasn't done DB lookup)
	internalUUID := ""
	if uuid, exists := c.Get("userInternalUUID"); exists {
		if uStr, ok := uuid.(string); ok {
			internalUUID = uStr
		}
	}

	// Get display name (optional)
	displayName := ""
	if name, exists := c.Get("userDisplayName"); exists {
		if nStr, ok := name.(string); ok {
			displayName = nStr
		}
	}

	return ResolvedUser{
		InternalUUID: internalUUID,
		Provider:     provider,
		ProviderID:   providerID,
		Email:        userEmail,
		DisplayName:  displayName,
	}, nil
}

// GetResourceRole extracts the resource-scoped role from the Gin context.
// Returns empty role if not set (some endpoints don't have resource middleware).
// Errors only on type assertion failure, not on absence.
func GetResourceRole(c *gin.Context) (Role, error) {
	roleValue, exists := c.Get("userRole")
	if !exists {
		return "", nil
	}

	userRole, ok := roleValue.(Role)
	if !ok {
		return "", &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to determine user role",
		}
	}

	return userRole, nil
}

// SamePrincipal returns true if two ResolvedUser values represent the same person.
// Pure in-memory comparison, no DB access. Both arguments should ideally be fully
// resolved (via GetAuthenticatedUser or ResolveUser) before calling.
//
// Algorithm:
// 1. If both have InternalUUID: match on UUID (warn if provider fields conflict)
// 2. If both have Provider AND ProviderID: match on (provider, provider_id)
// 3. Otherwise: return false (insufficient information)
//
// Email is NEVER used for identity comparison.
func SamePrincipal(a, b ResolvedUser) bool {
	logger := slogging.Get()

	// Step 1: UUID comparison (highest priority)
	if a.InternalUUID != "" && b.InternalUUID != "" {
		if a.InternalUUID == b.InternalUUID {
			// UUID matches — warn if provider fields are populated and inconsistent
			if a.Provider != "" && b.Provider != "" && a.ProviderID != "" && b.ProviderID != "" {
				if a.Provider != b.Provider || a.ProviderID != b.ProviderID {
					logger.Warn("SamePrincipal: UUID match (%s) but provider fields differ: (%s, %s) vs (%s, %s)",
						a.InternalUUID, a.Provider, a.ProviderID, b.Provider, b.ProviderID)
				}
			}
			return true
		}
		return false
	}

	// Step 2: Provider + ProviderID comparison
	if a.Provider != "" && a.ProviderID != "" && b.Provider != "" && b.ProviderID != "" {
		return a.Provider == b.Provider && a.ProviderID == b.ProviderID
	}

	// Step 3: Insufficient information
	return false
}
