package config

import "testing"

func TestClassificationFor_KnownBootstrapKey(t *testing.T) {
	c := classificationFor("database.url")
	if c.Category != CategoryBootstrap {
		t.Errorf("database.url Category = %v, want CategoryBootstrap", c.Category)
	}
}

func TestClassificationFor_KnownOperationalKey(t *testing.T) {
	c := classificationFor("websocket.inactivity_timeout_seconds")
	if c.Category != CategoryOperational {
		t.Errorf("websocket.inactivity_timeout_seconds Category = %v, want CategoryOperational", c.Category)
	}
}

func TestClassificationFor_SharedEmbeddingKey(t *testing.T) {
	c := classificationFor("timmy.text_embedding_model")
	if c.Delivery == nil || !c.Delivery.SharedInvariant {
		t.Errorf("timmy.text_embedding_model should be a SharedInvariant setting, got %+v", c.Delivery)
	}
}

func TestClassificationFor_ProviderPrefixKey(t *testing.T) {
	// classificationFor is purely prefix-based and cannot distinguish a
	// provider's secret sub-keys (.client_secret) from its non-secret ones
	// (.enabled, .client_id). The provider prefix is therefore classified
	// operational and NOT blanket-secret — a blanket Class.Secret:true would
	// cause GetMigratableSettings' Class.Secret->Secret sync to mask the
	// non-secret sub-keys. Per-setting Secret flags are the precise masking
	// source (see TestGetMigratableSettings_OAuthProviderSecretMasking).
	c := classificationFor("auth.oauth.providers.google.client_secret")
	if c.Category != CategoryOperational {
		t.Errorf("oauth provider key Category = %v, want CategoryOperational", c.Category)
	}
	if c.Secret {
		t.Error("oauth provider prefix must not be blanket-classified Secret — provider subtrees contain a mix of secret and non-secret keys")
	}
}

func TestClassificationFor_UnknownKeyIsUnclassified(t *testing.T) {
	c := classificationFor("totally.unknown.key")
	if c.Category != CategoryUnclassified {
		t.Errorf("unknown key Category = %v, want CategoryUnclassified", c.Category)
	}
}

// TestClassificationFor_TimmyAPIKeysAreOperationalSecrets pins #422: the four
// Timmy API keys must be CategoryOperational + Secret + monolith-only, NOT
// bootstrap. Bootstrap classification means file/env only — never DB-backed,
// never imported by dbtool --import-legacy, never visible in /admin/settings
// for rotation. The keys are operational by every meaningful criterion
// (rotatable at runtime, no restart required); the Secret flag drives masking
// and audit-log redaction.
func TestClassificationFor_TimmyAPIKeysAreOperationalSecrets(t *testing.T) {
	keys := []string{
		"timmy.llm_api_key",
		"timmy.text_embedding_api_key",
		"timmy.code_embedding_api_key",
		"timmy.rerank_api_key",
	}
	for _, k := range keys {
		c := classificationFor(k)
		if c.Category != CategoryOperational {
			t.Errorf("%s Category = %v, want CategoryOperational", k, c.Category)
		}
		if !c.Secret {
			t.Errorf("%s Secret = false, want true", k)
		}
		if c.Visibility != VisibilityAdminOnly {
			t.Errorf("%s Visibility = %v, want VisibilityAdminOnly", k, c.Visibility)
		}
		if c.Delivery == nil {
			t.Errorf("%s Delivery is nil, want non-nil for operational", k)
			continue
		}
		if c.Delivery.StampedIntoEnvelope {
			t.Errorf("%s StampedIntoEnvelope = true, want false — workers read keys from their own SecretMounts, not from job envelopes", k)
		}
		if len(c.Consumers) != 1 || c.Consumers[0] != ConsumerMonolith {
			t.Errorf("%s Consumers = %v, want [ConsumerMonolith]", k, c.Consumers)
		}
	}
}

// TestClassificationFor_ClientConfigKeysArePublic pins that the DB-only keys
// surfaced on the public /config endpoint are classified VisibilityPublic
// operational. Without a classification entry they default to the zero
// ConfigClass (VisibilityInternal), which makes GET/DELETE
// /admin/settings/{key} 404 even though the LIST endpoint shows them.
func TestClassificationFor_ClientConfigKeysArePublic(t *testing.T) {
	keys := []string{
		"features.saml_enabled",
		"features.webhooks_enabled",
		"features.websocket_enabled",
		"websocket.max_participants",
		"upload.max_file_size_mb",
		"ui.default_theme",
	}
	for _, k := range keys {
		c := classificationFor(k)
		if c.Category != CategoryOperational {
			t.Errorf("%s Category = %v, want CategoryOperational", k, c.Category)
		}
		if c.Visibility != VisibilityPublic {
			t.Errorf("%s Visibility = %v, want VisibilityPublic", k, c.Visibility)
		}
		if c.Secret {
			t.Errorf("%s Secret = true, want false", k)
		}
	}
}
