package auth

import (
	"context"
	"testing"

	"github.com/ericfitz/tmi/auth/saml"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSAMLManager_EnsureProvider_Idempotent(t *testing.T) {
	manager := NewSAMLManager(nil)

	config := SAMLProviderConfig{
		ID:             "test",
		Name:           "Test IDP",
		EntityID:       "https://tmi.example.com",
		IDPMetadataURL: "https://idp.example.com/metadata",
		ACSURL:         "https://tmi.example.com/saml/test/acs",
	}

	// First call attempts initialization (will fail without real IDP metadata, but shouldn't panic)
	err := manager.EnsureProvider("test", config)
	if err != nil {
		assert.Contains(t, err.Error(), "test")
	}
}

func TestSAMLManager_IsProviderInitialized_NotInitialized(t *testing.T) {
	manager := NewSAMLManager(nil)
	assert.False(t, manager.IsProviderInitialized("nonexistent"))
}

// fakeSAMLUserResolver extends the OAuth-path fakeUserResolver (see
// handlers_oauth_user_test.go) with the UpdateUser method required by the
// samlUserResolver interface.
type fakeSAMLUserResolver struct {
	fakeUserResolver
	updateCalls  int
	updatedUsers []User
}

func (f *fakeSAMLUserResolver) UpdateUser(_ context.Context, u User) error {
	f.updateCalls++
	f.updatedUsers = append(f.updatedUsers, u)
	return nil
}

// Regression test for the SAML variant of #290: an assertion whose email
// matches an existing user bound to a DIFFERENT provider must be rejected
// with errCrossProviderConflict instead of returning the victim's record
// (which would mint the victim's tokens via GenerateTokensWithUserInfo).
func TestProcessSAMLUser_CrossProviderEmailMatchRejected(t *testing.T) {
	victim := User{
		InternalUUID:   uuid.NewString(),
		Provider:       "google",
		ProviderUserID: "google-sub-123",
		Email:          "victim@example.com",
		Name:           "Victim",
		EmailVerified:  true,
	}
	resolver := &fakeSAMLUserResolver{}
	resolver.byEmail = &victim

	userInfo := &saml.UserInfo{
		ID:    "attacker-nameid",
		Email: "victim@example.com",
		Name:  "Attacker",
	}

	user, err := processSAMLUser(context.Background(), resolver, userInfo, "evil-corp-saml")
	require.ErrorIs(t, err, errCrossProviderConflict)
	assert.Nil(t, user)
	assert.Equal(t, 0, resolver.updateCalls, "victim record must not be updated")
	assert.Equal(t, 0, resolver.createCalls, "no new user should be created on conflict")
}

// TestUpdateSAMLUserOnLogin_GuardsAndTiers verifies that updateSAMLUserOnLogin
// (a) never persists a synthesized or empty name/email over a real stored
// value, and (b) only updates email on the strongest match tiers, mirroring
// the OAuth path's tier-aware guarded updates (auth/handlers_oauth_user.go).
func TestUpdateSAMLUserOnLogin_GuardsAndTiers(t *testing.T) {
	stored := User{InternalUUID: "u-1", Provider: "okta", ProviderUserID: "sub-1",
		Email: "alice@corp.example", Name: "Alice Real"}

	tests := []struct {
		name      string
		userInfo  *saml.UserInfo
		match     samlMatchType
		wantName  string
		wantEmail string
	}{
		{"synthetic name never overwrites", &saml.UserInfo{ID: "sub-1",
			Email: "sub-1@okta.saml.tmi", EmailSynthesized: true,
			Name: "sub-1", NameSynthesized: true},
			samlMatchProviderID, "Alice Real", "alice@corp.example"},
		{"real name updates on provider-id match", &saml.UserInfo{ID: "sub-1",
			Email: "alice@corp.example", Name: "Alice B. Real"},
			samlMatchProviderID, "Alice B. Real", "alice@corp.example"},
		{"email updates on provider-id match", &saml.UserInfo{ID: "sub-1",
			Email: "alice.new@corp.example", Name: "Alice Real"},
			samlMatchProviderID, "Alice Real", "alice.new@corp.example"},
		{"email NOT updated on email-tier match", &saml.UserInfo{ID: "sub-1",
			Email: "alice@corp.example", Name: "Alice B. Real"},
			samlMatchProviderEmail, "Alice B. Real", "alice@corp.example"},
		{"empty name never overwrites", &saml.UserInfo{ID: "sub-1",
			Email: "alice@corp.example", Name: ""},
			samlMatchProviderID, "Alice Real", "alice@corp.example"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolver := &fakeSAMLUserResolver{}
			got, err := updateSAMLUserOnLogin(context.Background(), resolver, stored, tc.userInfo, tc.match)
			require.NoError(t, err)
			assert.Equal(t, tc.wantName, got.Name)
			assert.Equal(t, tc.wantEmail, got.Email)
			assert.NotNil(t, got.LastLogin)
			require.Len(t, resolver.updatedUsers, 1)
			assert.Equal(t, tc.wantName, resolver.updatedUsers[0].Name)
		})
	}

	// Sparse, admin-precreated record (Tier 3: samlMatchEmailOnly): stored
	// Name is empty, so even a synthesized name is better than none — the
	// guard that protects a real name against synthetic overwrite must not
	// also block the record's only chance to ever get a name.
	t.Run("synthetic name fills an empty stored name", func(t *testing.T) {
		sparse := User{InternalUUID: "u-2", Email: "bob@corp.example", Name: ""}
		userInfo := &saml.UserInfo{ID: "sub-2",
			Email: "sub-2@okta.saml.tmi", EmailSynthesized: true,
			Name: "sub-2", NameSynthesized: true}

		resolver := &fakeSAMLUserResolver{}
		got, err := updateSAMLUserOnLogin(context.Background(), resolver, sparse, userInfo, samlMatchEmailOnly)
		require.NoError(t, err)
		assert.Equal(t, "sub-2", got.Name)
		require.Len(t, resolver.updatedUsers, 1)
		assert.Equal(t, "sub-2", resolver.updatedUsers[0].Name)
	})

	// A linked (secondary) identity must never overwrite the primary record's
	// email, even when the asserted email is real (not synthesized) and
	// different from the stored one. A linked identity's email is
	// display-cache only per the #383 identity-link design; treating it as
	// authoritative would let an attacker link a secondary IdP asserting a
	// victim's email and hijack the primary account (users.email has no
	// unique index, so a later email-addressed grant could bind to the
	// attacker). Name updates on this tier are unchanged and intended, so
	// this pins both halves of the behavior.
	t.Run("linked identity never updates email, but does update name", func(t *testing.T) {
		resolver := &fakeSAMLUserResolver{}
		userInfo := &saml.UserInfo{ID: "sub-1",
			Email: "attacker-controlled@evil.example", Name: "Alice B. Real"}

		got, err := updateSAMLUserOnLogin(context.Background(), resolver, stored, userInfo, samlMatchLinkedIdentity)
		require.NoError(t, err)
		assert.Equal(t, "alice@corp.example", got.Email, "linked identity must not overwrite primary email")
		assert.Equal(t, "Alice B. Real", got.Name, "name updates are still allowed on a linked-identity match")
		require.Len(t, resolver.updatedUsers, 1)
		assert.Equal(t, "alice@corp.example", resolver.updatedUsers[0].Email)
		assert.Equal(t, "Alice B. Real", resolver.updatedUsers[0].Name)
	})
}
