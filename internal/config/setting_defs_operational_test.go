package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOperationalDefs_KnownKeysAreDeclaredOperational(t *testing.T) {
	// A representative sample across the operational sections. Full coverage
	// is proven by the bijection tests in setting_defs_bijection_test.go.
	for _, key := range []string{
		"auth.everyone_is_a_reviewer",
		"auth.oauth.client_callback_allowlist",
		"features.saml_enabled",
		"websocket.inactivity_timeout_seconds",
		"operator.name",
		"administrators",
		"observability.enabled",
		"ssrf.webhook.allowlist",
		"webhooks.allow_http_targets",
		"timmy.embedding_dimension",
	} {
		d, ok := DefFor(key)
		require.True(t, ok, "%s must be declared in the registry", key)
		assert.Equal(t, CategoryOperational, d.Class.Category, "%s must be operational", key)
		// Controller ruling (fix round 1, finding 3): the brief's blanket
		// assert.NotEmpty(d.Default) does not hold across this sample —
		// operator.name and ssrf.webhook.allowlist both have a genuinely
		// empty compiled-in Default (Type "string", exempted by
		// setting_def_validation.go's operational-Default rule). Default is
		// only load-bearing when the key is Seeded (written into
		// system_settings at database init), so check it only then —
		// consistent with the Seeded-requires-Default rule added in the
		// same fix round.
		if d.Seeded {
			assert.NotEmpty(t, d.Default, "%s is Seeded and must declare a Default", key)
		}
	}
}

func TestOperationalDefs_AreTransitionalWhileConfigDelivered(t *testing.T) {
	d, ok := DefFor("auth.everyone_is_a_reviewer")
	require.True(t, ok)
	assert.True(t, d.Transitional, "operational settings still have a config path in Phase A")
	assert.NotEmpty(t, d.EnvVar)
	assert.NotNil(t, d.Get)
}

func TestOperationalDefs_DeclareMutability(t *testing.T) {
	// features.saml_enabled gates SAML manager construction at startup, so a
	// database edit cannot take effect without a restart. The spec makes
	// Mutability load-bearing; this pins the known case.
	d, ok := DefFor("features.saml_enabled")
	require.True(t, ok)
	assert.Equal(t, MutabilityStatic, d.Class.Mutability,
		"features.saml_enabled gates startup wiring and is restart-required")
}
