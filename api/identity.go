package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// UserResolver provides user lookup capabilities for identity resolution.
// Implemented by auth.Service.
type UserResolver interface {
	GetUserByID(ctx context.Context, id string) (auth.User, error)
	GetUserByProviderID(ctx context.Context, provider, providerUserID string) (auth.User, error)
	GetUserByProviderAndEmail(ctx context.Context, provider, email string) (auth.User, error)
	GetUserByEmail(ctx context.Context, email string) (auth.User, error)
	GetUserByAnyProviderID(ctx context.Context, providerUserID string) (auth.User, error)
}

// resolvedUserFromAuthUser converts an auth.User to a ResolvedUser.
func resolvedUserFromAuthUser(u auth.User) ResolvedUser {
	return ResolvedUser{
		InternalUUID: u.InternalUUID,
		Provider:     u.Provider,
		ProviderID:   u.ProviderUserID,
		Email:        u.Email,
		DisplayName:  u.Name,
	}
}

// isUserNotFound returns true if the error indicates a user was not found.
// Uses errors.Is for typed errors from repositories, with Classify fallback
// for errors that may carry "not found" only as a string (e.g., from auth service wrappers).
func isUserNotFound(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(dberrors.Classify(err), dberrors.ErrNotFound)
}

// ResolveUser resolves a partially-known identity to a fully-resolved user via database lookup.
//
// Algorithm priority:
//  1. If InternalUUID is set: lookup by UUID (hard error if not found, no fallthrough)
//  2. If Provider and ProviderID are set: lookup by (provider, provider_id)
//  3. If ProviderID and Email are set (no Provider): lookup by any provider ID, verify email
//  4. If Provider and Email are set (no ProviderID): lookup by (provider, email)
//  5. If only Email is set: lookup by email
//
// After a successful match, if partial.Email is non-empty it is substituted into the result
// to reflect the most current email asserted by the IdP.
func ResolveUser(ctx context.Context, partial ResolvedUser, resolver UserResolver) (ResolvedUser, error) {
	// Input validation: at least one of InternalUUID, ProviderID, or Email must be non-empty
	if partial.InternalUUID == "" && partial.ProviderID == "" && partial.Email == "" {
		return ResolvedUser{}, fmt.Errorf("ResolveUser: at least one of InternalUUID, ProviderID, or Email must be provided")
	}

	// Strategy 0: UUID lookup (highest priority, hard error if not found)
	if partial.InternalUUID != "" {
		return resolveByUUID(ctx, partial, resolver)
	}

	return resolveWithoutUUID(ctx, partial, resolver)
}

// resolveByUUID handles UUID-based user resolution (strategy 0).
// This is a hard lookup: if the UUID is not found, it returns an error with no fallthrough.
// Provider and ProviderID mismatches are errors; email mismatch is tolerated.
func resolveByUUID(ctx context.Context, partial ResolvedUser, resolver UserResolver) (ResolvedUser, error) {
	logger := slogging.Get()

	u, err := resolver.GetUserByID(ctx, partial.InternalUUID)
	if err != nil {
		if isUserNotFound(err) {
			return ResolvedUser{}, fmt.Errorf("ResolveUser: user with UUID %s not found", partial.InternalUUID)
		}
		return ResolvedUser{}, fmt.Errorf("ResolveUser: error looking up user by UUID: %w", err)
	}

	// Verify no provided fields conflict
	if partial.Provider != "" && u.Provider != partial.Provider {
		return ResolvedUser{}, fmt.Errorf("ResolveUser: provider conflict for UUID %s: expected %q, found %q",
			partial.InternalUUID, partial.Provider, u.Provider)
	}
	if partial.ProviderID != "" && u.ProviderUserID != partial.ProviderID {
		return ResolvedUser{}, fmt.Errorf("ResolveUser: provider ID conflict for UUID %s: expected %q, found %q",
			partial.InternalUUID, partial.ProviderID, u.ProviderUserID)
	}
	if partial.Email != "" && u.Email != partial.Email {
		logger.Debug("ResolveUser: email mismatch for UUID %s: partial has %q, DB has %q (tolerated, email is mutable)",
			partial.InternalUUID, partial.Email, u.Email)
	}

	return reflectEmail(resolvedUserFromAuthUser(u), partial.Email), nil
}

// resolveWithoutUUID tries strategies 1-4 in priority order when no UUID is available.
func resolveWithoutUUID(ctx context.Context, partial ResolvedUser, resolver UserResolver) (ResolvedUser, error) {
	// Strategy 1: Provider + ProviderID (ignore email for lookup)
	if partial.Provider != "" && partial.ProviderID != "" {
		return resolveByProviderID(ctx, partial, resolver)
	}

	// Strategy 2: ProviderID + Email (no Provider) - lookup by any provider ID, verify email
	if partial.ProviderID != "" && partial.Email != "" {
		result, err := resolveByAnyProviderID(ctx, partial, resolver)
		if err == nil {
			return result, nil
		}
		// If it was a non-"not found" error, propagate it
		if !isUserNotFound(err) {
			return ResolvedUser{}, err
		}
		// Fall through to email-based strategies
	}

	// Strategy 3: Provider + Email (no ProviderID)
	if partial.Provider != "" && partial.Email != "" {
		result, err := resolveByProviderAndEmail(ctx, partial, resolver)
		if err == nil {
			return result, nil
		}
		if !isUserNotFound(err) {
			return ResolvedUser{}, err
		}
		// Fall through to email-only
	}

	// Strategy 4: Email only
	if partial.Email != "" {
		return resolveByEmail(ctx, partial, resolver)
	}

	return ResolvedUser{}, fmt.Errorf("ResolveUser: user not found by any strategy")
}

// resolveByProviderID looks up a user by (provider, providerID). No fallthrough on not-found.
func resolveByProviderID(ctx context.Context, partial ResolvedUser, resolver UserResolver) (ResolvedUser, error) {
	u, err := resolver.GetUserByProviderID(ctx, partial.Provider, partial.ProviderID)
	if err != nil {
		if isUserNotFound(err) {
			return ResolvedUser{}, fmt.Errorf("ResolveUser: user not found for provider %q, provider ID %q", partial.Provider, partial.ProviderID)
		}
		return ResolvedUser{}, fmt.Errorf("ResolveUser: error looking up user by provider ID: %w", err)
	}
	return reflectEmail(resolvedUserFromAuthUser(u), partial.Email), nil
}

// resolveByAnyProviderID looks up a user by providerID across all providers.
// Email mismatch is tolerated but logged.
func resolveByAnyProviderID(ctx context.Context, partial ResolvedUser, resolver UserResolver) (ResolvedUser, error) {
	u, err := resolver.GetUserByAnyProviderID(ctx, partial.ProviderID)
	if err != nil {
		if isUserNotFound(err) {
			return ResolvedUser{}, fmt.Errorf("ResolveUser: user not found by any provider ID %q", partial.ProviderID)
		}
		return ResolvedUser{}, fmt.Errorf("ResolveUser: error looking up user by any provider ID: %w", err)
	}
	if u.Email != partial.Email {
		slogging.Get().Debug("ResolveUser: email mismatch for provider ID %q: partial has %q, DB has %q (tolerated, email is mutable)",
			partial.ProviderID, partial.Email, u.Email)
	}
	return reflectEmail(resolvedUserFromAuthUser(u), partial.Email), nil
}

// resolveByProviderAndEmail looks up a user by (provider, email).
func resolveByProviderAndEmail(ctx context.Context, partial ResolvedUser, resolver UserResolver) (ResolvedUser, error) {
	u, err := resolver.GetUserByProviderAndEmail(ctx, partial.Provider, partial.Email)
	if err != nil {
		if isUserNotFound(err) {
			return ResolvedUser{}, fmt.Errorf("ResolveUser: user not found for provider %q, email %q", partial.Provider, partial.Email)
		}
		return ResolvedUser{}, fmt.Errorf("ResolveUser: error looking up user by provider and email: %w", err)
	}
	return reflectEmail(resolvedUserFromAuthUser(u), partial.Email), nil
}

// resolveByEmail looks up a user by email address only (weakest strategy).
func resolveByEmail(ctx context.Context, partial ResolvedUser, resolver UserResolver) (ResolvedUser, error) {
	u, err := resolver.GetUserByEmail(ctx, partial.Email)
	if err != nil {
		if isUserNotFound(err) {
			return ResolvedUser{}, fmt.Errorf("ResolveUser: user not found by any strategy")
		}
		return ResolvedUser{}, fmt.Errorf("ResolveUser: error looking up user by email: %w", err)
	}
	return reflectEmail(resolvedUserFromAuthUser(u), partial.Email), nil
}

// reflectEmail substitutes the provided email into the result if non-empty,
// reflecting the most current email asserted by the IdP without persisting it.
func reflectEmail(result ResolvedUser, email string) ResolvedUser {
	if email != "" {
		result.Email = email
	}
	return result
}

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

// ResolvedUserFromWebSocketClient creates a ResolvedUser from a WebSocketClient.
func ResolvedUserFromWebSocketClient(client *WebSocketClient) ResolvedUser {
	return ResolvedUser{
		InternalUUID: client.InternalUUID,
		Provider:     client.UserProvider,
		ProviderID:   client.UserID,
		Email:        client.UserEmail,
		DisplayName:  client.UserName,
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

// ResolvedUserFromAuthorization creates a ResolvedUser from an Authorization entry.
// InternalUUID will be empty since the API Authorization type does not carry it.
func ResolvedUserFromAuthorization(auth Authorization) ResolvedUser {
	ru := ResolvedUser{
		Provider:   auth.Provider,
		ProviderID: auth.ProviderId,
	}
	if auth.Email != nil {
		ru.Email = string(*auth.Email)
	}
	if auth.DisplayName != nil {
		ru.DisplayName = *auth.DisplayName
	}
	return ru
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
			Message: "Authentication required",
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
	// Step 1: UUID comparison (highest priority)
	if a.InternalUUID != "" && b.InternalUUID != "" {
		if a.InternalUUID == b.InternalUUID {
			// UUID matches — warn if provider fields are populated and inconsistent
			if a.Provider != "" && b.Provider != "" && a.ProviderID != "" && b.ProviderID != "" {
				if a.Provider != b.Provider || a.ProviderID != b.ProviderID {
					slogging.Get().Warn("SamePrincipal: UUID match (%s) but provider fields differ: (%s, %s) vs (%s, %s)",
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
