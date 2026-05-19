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
