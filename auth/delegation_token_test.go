package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIssueAddonDelegationToken_Claims(t *testing.T) {
	svc, cleanup := setupTestServiceWithRepos(t, &stubUserRepo{}, &stubCredRepo{})
	defer cleanup()

	invoker := &User{
		InternalUUID:   uuid.New().String(),
		Email:          "alice@example.com",
		EmailVerified:  true,
		Name:           "Alice",
		Provider:       "google",
		ProviderUserID: "google-uid-12345",
		Groups:         []string{"engineering"},
	}
	addonID := uuid.New()
	deliveryID := uuid.New()
	tmID := uuid.New()

	tokenStr, err := svc.IssueAddonDelegationToken(context.Background(), invoker, addonID, deliveryID, tmID)
	require.NoError(t, err)
	require.NotEmpty(t, tokenStr)

	claims := &Claims{}
	_, err = svc.keyManager.VerifyToken(tokenStr, claims)
	require.NoError(t, err, "delegation token should verify with the same key manager")

	// Subject is the invoker's provider_user_id — handlers and ACL checks
	// resolve to the invoker, not the addon owner.
	assert.Equal(t, invoker.ProviderUserID, claims.Subject)
	assert.Equal(t, invoker.Email, claims.Email)
	assert.Equal(t, invoker.Provider, claims.IdentityProvider)

	// Display name is decorated so audit/log lines clearly distinguish
	// delegation calls from native invoker calls.
	assert.True(t, strings.HasPrefix(claims.Name, "[Addon Delegation:"),
		"name should be prefixed for audit clarity, got %q", claims.Name)
	assert.Contains(t, claims.Name, addonID.String())

	// is_administrator is FORCED to false on every delegation token,
	// regardless of the invoker's actual administrator membership.
	require.NotNil(t, claims.IsAdministrator)
	assert.False(t, *claims.IsAdministrator,
		"delegation tokens never carry administrator authority")

	// Delegation context must be present and scoped to the invocation.
	require.NotNil(t, claims.Delegation)
	assert.Equal(t, addonID.String(), claims.Delegation.AddonID)
	assert.Equal(t, deliveryID.String(), claims.Delegation.DeliveryID)
	assert.Equal(t, tmID.String(), claims.Delegation.ThreatModelID)

	// Expiration matches the addon-invocation budget. Allow a small slack
	// for the clock between issuance and the assertion.
	require.NotNil(t, claims.ExpiresAt)
	delta := time.Until(claims.ExpiresAt.Time)
	assert.Greater(t, delta, 0*time.Second, "token must not already be expired")
	assert.LessOrEqual(t, delta, DelegationTokenTTL,
		"token must expire within DelegationTokenTTL (%v)", DelegationTokenTTL)
}

func TestIssueAddonDelegationToken_RejectsMissingProviderUserID(t *testing.T) {
	svc, cleanup := setupTestServiceWithRepos(t, &stubUserRepo{}, &stubCredRepo{})
	defer cleanup()

	invoker := &User{
		InternalUUID: uuid.New().String(),
		Email:        "no-pid@example.com",
		// ProviderUserID intentionally empty
	}

	_, err := svc.IssueAddonDelegationToken(
		context.Background(), invoker, uuid.New(), uuid.New(), uuid.New(),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider_user_id")
}

func TestIssueAddonDelegationToken_StripsAdministratorsGroup(t *testing.T) {
	// If the invoker is in the administrators group via TMI claims-enricher,
	// the delegation token MUST NOT propagate it. The token is a scoped
	// invocation impersonator; granting admin would defeat the purpose.
	svc, cleanup := setupTestServiceWithRepos(t, &stubUserRepo{}, &stubCredRepo{})
	defer cleanup()
	svc.SetClaimsEnricher(&stubClaimsEnricher{
		isAdmin:     true,
		isSecReview: false,
		tmiGroups:   []string{"administrators", "engineering"},
	})

	invoker := &User{
		InternalUUID:   uuid.New().String(),
		Email:          "admin@example.com",
		Name:           "Admin",
		Provider:       "google",
		ProviderUserID: "admin-uid",
		Groups:         []string{"administrators"},
	}

	tokenStr, err := svc.IssueAddonDelegationToken(
		context.Background(), invoker, uuid.New(), uuid.New(), uuid.New(),
	)
	require.NoError(t, err)

	claims := &Claims{}
	_, err = svc.keyManager.VerifyToken(tokenStr, claims)
	require.NoError(t, err)

	require.NotNil(t, claims.IsAdministrator)
	assert.False(t, *claims.IsAdministrator)
	for _, g := range claims.Groups {
		assert.NotEqual(t, "administrators", g,
			"administrators group must be filtered from delegation token (got groups: %v)",
			claims.Groups)
	}
}

// TestIssueAddonDelegationToken_AudienceIsIssuer asserts that the token
// is signed for the same audience as normal user tokens, so the existing
// JWT validator accepts it without special-casing.
func TestIssueAddonDelegationToken_AudienceIsIssuer(t *testing.T) {
	svc, cleanup := setupTestServiceWithRepos(t, &stubUserRepo{}, &stubCredRepo{})
	defer cleanup()

	invoker := &User{
		InternalUUID:   uuid.New().String(),
		Email:          "alice@example.com",
		Name:           "Alice",
		Provider:       "google",
		ProviderUserID: "google-uid",
	}

	tokenStr, err := svc.IssueAddonDelegationToken(
		context.Background(), invoker, uuid.New(), uuid.New(), uuid.New(),
	)
	require.NoError(t, err)

	claims := &Claims{}
	parsed, err := svc.keyManager.VerifyToken(tokenStr, claims)
	require.NoError(t, err)
	assert.True(t, parsed.Valid)

	// Audience and issuer should be the same — self-issued, same shape
	// as the regular GenerateTokens output.
	require.NotEmpty(t, claims.Audience)
	assert.Equal(t, claims.Issuer, claims.Audience[0])

	// Token method is HS256 per setupTestServiceWithRepos.
	_, ok := parsed.Method.(*jwt.SigningMethodHMAC)
	assert.True(t, ok, "delegation tokens should use the configured HMAC method")
}
