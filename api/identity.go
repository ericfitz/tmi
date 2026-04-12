package api

import (
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
