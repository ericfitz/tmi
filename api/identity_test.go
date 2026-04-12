package api

import (
	"testing"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
)

func TestResolvedUserToUser(t *testing.T) {
	ru := ResolvedUser{
		InternalUUID: "uuid-123",
		Provider:     "tmi",
		ProviderID:   "alice",
		Email:        "alice@tmi.local",
		DisplayName:  "Alice",
	}

	u := ru.ToUser()
	assert.Equal(t, "alice", u.ProviderId)
	assert.Equal(t, "tmi", u.Provider)
	assert.Equal(t, openapi_types.Email("alice@tmi.local"), u.Email)
	assert.Equal(t, "Alice", u.DisplayName)
	assert.Equal(t, UserPrincipalTypeUser, u.PrincipalType)
}

func TestResolvedUserToPrincipal(t *testing.T) {
	ru := ResolvedUser{
		InternalUUID: "uuid-123",
		Provider:     "google",
		ProviderID:   "google-uid-alice",
		Email:        "alice@gmail.com",
		DisplayName:  "Alice",
	}

	p := ru.ToPrincipal()
	assert.Equal(t, "google-uid-alice", p.ProviderId)
	assert.Equal(t, "google", p.Provider)
	assert.NotNil(t, p.Email)
	assert.Equal(t, openapi_types.Email("alice@gmail.com"), *p.Email)
	assert.NotNil(t, p.DisplayName)
	assert.Equal(t, "Alice", *p.DisplayName)
	assert.Equal(t, PrincipalPrincipalTypeUser, p.PrincipalType)
}

func TestResolvedUserFromUser(t *testing.T) {
	u := User{
		Provider:      "tmi",
		ProviderId:    "alice",
		Email:         openapi_types.Email("alice@tmi.local"),
		DisplayName:   "Alice",
		PrincipalType: UserPrincipalTypeUser,
	}

	ru := ResolvedUserFromUser(u)
	assert.Equal(t, "", ru.InternalUUID)
	assert.Equal(t, "tmi", ru.Provider)
	assert.Equal(t, "alice", ru.ProviderID)
	assert.Equal(t, "alice@tmi.local", ru.Email)
	assert.Equal(t, "Alice", ru.DisplayName)
}

func TestResolvedUserFromPrincipal(t *testing.T) {
	email := openapi_types.Email("alice@tmi.local")
	displayName := "Alice"
	p := Principal{
		Provider:      "tmi",
		ProviderId:    "alice",
		Email:         &email,
		DisplayName:   &displayName,
		PrincipalType: PrincipalPrincipalTypeUser,
	}

	ru := ResolvedUserFromPrincipal(p)
	assert.Equal(t, "", ru.InternalUUID)
	assert.Equal(t, "tmi", ru.Provider)
	assert.Equal(t, "alice", ru.ProviderID)
	assert.Equal(t, "alice@tmi.local", ru.Email)
	assert.Equal(t, "Alice", ru.DisplayName)
}

func TestResolvedUserFromPrincipalNilOptionals(t *testing.T) {
	p := Principal{
		Provider:      "tmi",
		ProviderId:    "alice",
		Email:         nil,
		DisplayName:   nil,
		PrincipalType: PrincipalPrincipalTypeUser,
	}

	ru := ResolvedUserFromPrincipal(p)
	assert.Equal(t, "", ru.Email)
	assert.Equal(t, "", ru.DisplayName)
}

func TestResolvedUserIsEmpty(t *testing.T) {
	assert.True(t, (ResolvedUser{}).IsEmpty())
	assert.False(t, (ResolvedUser{Email: "a@b.com"}).IsEmpty())
	assert.False(t, (ResolvedUser{ProviderID: "x"}).IsEmpty())
	assert.False(t, (ResolvedUser{InternalUUID: "uuid"}).IsEmpty())
}

func TestSamePrincipalByUUID(t *testing.T) {
	a := ResolvedUser{InternalUUID: "uuid-1", Provider: "tmi", ProviderID: "alice"}
	b := ResolvedUser{InternalUUID: "uuid-1", Provider: "tmi", ProviderID: "alice"}
	assert.True(t, SamePrincipal(a, b))
}

func TestSamePrincipalByUUIDDifferent(t *testing.T) {
	a := ResolvedUser{InternalUUID: "uuid-1", Provider: "tmi", ProviderID: "alice"}
	b := ResolvedUser{InternalUUID: "uuid-2", Provider: "tmi", ProviderID: "alice"}
	assert.False(t, SamePrincipal(a, b))
}

func TestSamePrincipalByUUIDWithProviderMismatchStillMatches(t *testing.T) {
	// UUID match takes precedence, but provider mismatch should log warning
	a := ResolvedUser{InternalUUID: "uuid-1", Provider: "tmi", ProviderID: "alice"}
	b := ResolvedUser{InternalUUID: "uuid-1", Provider: "google", ProviderID: "google-uid"}
	assert.True(t, SamePrincipal(a, b))
}

func TestSamePrincipalByProviderAndProviderID(t *testing.T) {
	a := ResolvedUser{Provider: "tmi", ProviderID: "alice"}
	b := ResolvedUser{Provider: "tmi", ProviderID: "alice"}
	assert.True(t, SamePrincipal(a, b))
}

func TestSamePrincipalByProviderAndProviderIDDifferentProvider(t *testing.T) {
	a := ResolvedUser{Provider: "tmi", ProviderID: "alice"}
	b := ResolvedUser{Provider: "google", ProviderID: "alice"}
	assert.False(t, SamePrincipal(a, b))
}

func TestSamePrincipalByProviderAndProviderIDDifferentID(t *testing.T) {
	a := ResolvedUser{Provider: "tmi", ProviderID: "alice"}
	b := ResolvedUser{Provider: "tmi", ProviderID: "bob"}
	assert.False(t, SamePrincipal(a, b))
}

func TestSamePrincipalInsufficientInfo(t *testing.T) {
	// Only email — not enough for identity comparison
	a := ResolvedUser{Email: "alice@tmi.local"}
	b := ResolvedUser{Email: "alice@tmi.local"}
	assert.False(t, SamePrincipal(a, b))
}

func TestSamePrincipalOneHasUUIDOtherDoesNot(t *testing.T) {
	// Falls through to provider+providerID check
	a := ResolvedUser{InternalUUID: "uuid-1", Provider: "tmi", ProviderID: "alice"}
	b := ResolvedUser{Provider: "tmi", ProviderID: "alice"}
	assert.True(t, SamePrincipal(a, b))
}

func TestSamePrincipalProviderIDWithoutProvider(t *testing.T) {
	// ProviderID alone without provider is insufficient
	a := ResolvedUser{ProviderID: "alice"}
	b := ResolvedUser{ProviderID: "alice"}
	assert.False(t, SamePrincipal(a, b))
}

func TestSamePrincipalBothEmpty(t *testing.T) {
	assert.False(t, SamePrincipal(ResolvedUser{}, ResolvedUser{}))
}
