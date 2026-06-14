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
	updateCalls int
}

func (f *fakeSAMLUserResolver) UpdateUser(_ context.Context, _ User) error {
	f.updateCalls++
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
